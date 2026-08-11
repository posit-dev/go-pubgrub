// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"fmt"

	"github.com/posit-dev/go-pubgrub/term"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// Solution is a successful solve: one version chosen for every package that
// needed one.
type Solution[P comparable, S versionset.Set[S]] struct {
	// Selected maps each chosen package to its single chosen version. A package
	// absent from it was not needed.
	Selected map[P]S

	// Order lists the packages in the order they were decided, root first. It is
	// not part of the answer — Selected is — but it makes a trace readable and a
	// test able to assert the search took the path it was supposed to.
	Order []P
}

// Unsolvable reports that no solution exists, and carries the proof.
//
// RootCause is §7.4's terminal incompatibility: either empty, or a lone positive
// term about the root package's own version. Both are the formal statement that
// the request itself cannot be satisfied. Following its causes reaches the
// external facts that forced it, and that derivation graph — not this error's
// message — is the explanation a user should see; rendering it is report's job.
type Unsolvable[P comparable, S versionset.Set[S]] struct {
	RootCause *Incompatibility[P, S]
}

// Error implements error.
func (e *Unsolvable[P, S]) Error() string {
	return fmt.Sprintf("solver: no solution exists; root cause %v", e.RootCause)
}

// Solver runs §5's main loop over one root package.
//
// Construct it with New, then call Solve. The partial solution and the
// incompatibility store are exposed afterwards because both are worth inspecting:
// the store holds the derivation graph a failure has to be explained from, and
// the partial solution holds the trace a test asserts against.
//
// A Solver is single-use and is not safe for concurrent use.
type Solver[P comparable, S versionset.Set[S]] struct {
	root        P
	rootVersion S
	provider    Provider[P, S]

	ps *PartialSolution[P, S]
	st *Store[P, S]

	// MaxRounds bounds how many times the loop may alternate between propagation
	// and decision making before giving up. Zero means no bound.
	//
	// This is a safety valve, kept deliberately separate from correctness. Neither
	// prose source proves the OUTER loop terminates in a bounded number of rounds
	// for arbitrary dependency graphs (§11 item 7) — only that conflict
	// resolution's inner loop does. Clause-learning search procedures are relied
	// on for this in general, but it is asserted rather than derived, so a caller
	// facing untrusted input has somewhere to put a bound instead of hanging.
	MaxRounds int
}

// New returns a Solver for the given root package, whose one real version is
// rootVersion.
//
// The root is seeded as the external fact "the root package must be exactly its
// one real version" (§5), phrased as the negative term §11 item 4 describes: it is
// forbidden for the root to be anything other than that version. Propagation then
// derives the root's requirement and decision making decides it like any other
// package, so the root needs no special case anywhere else — the provider is
// simply asked for its versions and its dependencies as usual.
func New[P comparable, S versionset.Set[S]](
	root P, rootVersion S, provider Provider[P, S],
) *Solver[P, S] {
	s := &Solver[P, S]{
		root:        root,
		rootVersion: rootVersion,
		provider:    provider,
		ps:          NewPartialSolution[P, S](),
		st:          NewStore[P, S](),
	}
	s.st.Add(NewIncompatibility(KindRoot, map[P]term.Term[S]{
		root: term.Negative(rootVersion),
	}))
	return s
}

// PartialSolution returns the solver's partial solution.
func (s *Solver[P, S]) PartialSolution() *PartialSolution[P, S] { return s.ps }

// Store returns the solver's incompatibility store, which holds the derivation
// graph.
func (s *Solver[P, S]) Store() *Store[P, S] { return s.st }

// Solve runs the loop to a total solution or to a proof that none exists.
//
// A failure to solve is reported as *Unsolvable, carrying the root-cause
// incompatibility. Any other error means the solve could not be carried out at
// all — a provider that failed or broke its contract, or an invariant violation —
// which is a different thing from "these requirements conflict" and should not be
// shown to a user as one.
//
// # Propagation and decision making strictly alternate
//
// Everything derivable is squeezed out before one new speculative decision is
// made, because a decision spent on a package whose range was about to collapse
// from information already on hand is wasted work — and the collapse might have
// removed the very version that was about to be chosen.
//
// # Conflict resolution runs INSIDE the propagate step, not beside it
//
// Propagation reports a conflict rather than resolving it, and the loop resolves
// it and resumes propagating from the package conflict resolution names. The
// correct response to "the thing I would derive next contradicts what is already
// true" is to repair the partial solution before deriving anything else, which is
// why no decision is made between the conflict and the resumption.
func (s *Solver[P, S]) Solve() (*Solution[P, S], error) {
	next := s.root

	for round := 0; ; round++ {
		if s.MaxRounds > 0 && round >= s.MaxRounds {
			return nil, fmt.Errorf("solver: gave up after %d rounds without settling", round)
		}

		if result := Propagate(s.ps, s.st, next); result.HasConflict() {
			resolution, err := Resolve(s.ps, s.st, s.root, result.Conflict)
			if err != nil {
				return nil, err
			}
			if resolution.Unsolvable {
				return nil, &Unsolvable[P, S]{RootCause: resolution.Incompatibility}
			}

			// Resume on the package whose term conflict resolution left open,
			// without making a decision first.
			next = resolution.Package
			continue
		}

		outcome, err := MakeDecision(s.ps, s.st, s.provider)
		if err != nil {
			return nil, err
		}
		if outcome.Done {
			return s.solution()
		}
		next = outcome.Package
	}
}

// solution reads the answer off the decisions, after checking that it really is
// one.
//
// # Why §4's criterion is not trusted on its own
//
// §4 declares success when every package with a positive derivation has a
// matching decision, and that is what the loop above tests. It is not quite
// sufficient. An incompatibility of two or more terms, ALL negative, says "a is in
// x or b is in y"; with neither package assigned, every relation test about it is
// Inconclusive forever, propagation never fires, and §4's criterion would report a
// solution that omits both packages as a success — while that solution violates
// it. The gap is in §6/§4 as specified, not in this implementation of them.
//
// So the answer is verified against the whole incompatibility set in completed-
// world semantics before being returned, which costs one pass and converts a
// silently wrong answer into a loud error. A correct solve cannot trip it: every
// incompatibility is entailed by the external facts, and a selection satisfying
// all of those satisfies all of them.
func (s *Solver[P, S]) solution() (*Solution[P, S], error) {
	decisions := s.ps.Decisions()

	sol := &Solution[P, S]{
		Selected: make(map[P]S, len(decisions)),
		Order:    make([]P, 0, len(decisions)),
	}
	for _, d := range decisions {
		sol.Selected[d.Package] = d.Term.Set()
		sol.Order = append(sol.Order, d.Package)
	}

	for _, inc := range s.st.All() {
		if inc.ViolatedBy(sol.Selected) {
			return nil, fmt.Errorf("solver: the solution violates %v, which §4's success "+
				"criterion cannot see because no term of it is ever entailed by a partial solution", inc)
		}
	}

	return sol, nil
}
