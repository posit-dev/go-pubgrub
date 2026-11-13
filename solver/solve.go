// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"errors"
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

// ErrMaxConflicts is what errors.Is matches when Solver.MaxConflicts ends a
// solve. It says the solver stopped, not that no solution exists.
var ErrMaxConflicts = errors.New("solver: conflict limit reached")

// MaxConflictsError is returned when a solve reaches Solver.MaxConflicts.
type MaxConflictsError struct {
	// Limit is the MaxConflicts value that was reached.
	Limit int
}

// Error implements error.
func (e *MaxConflictsError) Error() string {
	return fmt.Sprintf("solver: gave up after %d conflicts", e.Limit)
}

// Unwrap lets errors.Is match ErrMaxConflicts.
func (e *MaxConflictsError) Unwrap() error { return ErrMaxConflicts }

// Stats counts work done by one Solve. It is filled however Solve returns,
// including Unsolvable and error returns, and counting never changes behavior.
type Stats struct {
	// Decisions counts calls of PartialSolution.Decide.
	Decisions int
	// Backtracks counts calls of PartialSolution.BacktrackTo, even when nothing
	// is removed.
	Backtracks int
	// PropagateVisits counts incompatibilities taken from Store.Mentioning that
	// reach the contradicted-cache test.
	PropagateVisits int
	// PreCommitClassifications counts calls of classifyAssuming.
	PreCommitClassifications int
	// KeyBuilds counts packages examined by chooseCandidate: one per undecided
	// package with a positive assignment, per decision round.
	KeyBuilds int
	// DependenciesCalls counts calls of Provider.Dependencies.
	DependenciesCalls int
	// LiveDependencyRecords is the number of KindDependency incompatibilities
	// Store.Mentioning can still return, at the end of Solve.
	LiveDependencyRecords int
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

	// MaxConflicts bounds how many conflicts (Stats.Backtracks) the solve may
	// resolve before giving up with a *MaxConflictsError. Zero means no bound.
	//
	// Unlike MaxRounds it does not count the decisions made again after each
	// backjump, so it suits large dependency sets. If both are set, the first
	// one reached ends the solve.
	MaxConflicts int

	// MergeDependencies (default off): when a version's dependency record names
	// the same depender package, dependency package and required range as a live
	// one, replace both by one record covering both versions. Fewer records are
	// visited, but the derivation trail can differ.
	MergeDependencies bool

	// RunningSatisfier (default off): keep a running term per assignment, so
	// satisfier queries and BacktrackTo do not re-intersect a package's history.
	// Same answers, less work.
	RunningSatisfier bool

	// Stats is filled by Solve. Read it after Solve returns, even on error.
	Stats Stats

	// ClashThreshold turns on a conflict-driven decision order when positive. Zero,
	// the default, and any negative value leave the solver exactly as it is without
	// it: same decisions, same provider calls, same learned facts.
	//
	// When positive, a package that takes part in this many conflicts, in either
	// role, is decided sooner than packages that have not. A conflict names one
	// blamed package and the others involved: a version turned down at decision
	// time, or the fact conflict analysis ends with. Reaching the count as the
	// blamed package puts a package in the first tier; reaching it as an involved
	// one puts it in the second, and a second-tier package that was already decided
	// has that decision, and all later ones, undone. Learned facts are kept, and
	// tiers last for the one solve. The order only changes which of several legal
	// answers is found first.
	ClashThreshold int

	// ClashFirst, with ClashThreshold positive, compares a package's tier before its
	// rank (version count), so a package that keeps clashing is decided before a
	// package with fewer versions. With OwnOrder, Exact-marked packages still go
	// first, then the tier. Ignored when ClashThreshold is not positive. Off by
	// default; the order only changes which of several legal answers is found.
	ClashFirst bool

	// Test hooks. Both are unexported on purpose: the order is observable only
	// through the result and the provider's calls.
	clashHook func(clash[P])
	pickHook  func(pkg P, tier int)

	// recheck makes the solver propagate every assigned package again after an
	// undo. The spec allows either; a test runs both.
	recheck bool

	// StableDiscovery breaks rank ties by the order in which the solver first met
	// each package in a returned dependency list, instead of by first derivation.
	// The zero value keeps the existing order.
	StableDiscovery bool

	// OwnOrder makes the solver order packages itself and ignore the provider's
	// rank: packages marked by Dependency.Exact first, then by first-met order.
	// It implies StableDiscovery. The zero value keeps the existing order.
	OwnOrder bool

	// StoredKeys keeps each package's decide key between rounds and rebuilds it only
	// when stale, instead of rebuilding every key every round. Changes no decision.
	// The zero value is off.
	StoredKeys bool
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
	defer s.finishStats()
	s.st.mergeDeps = s.MergeDependencies
	if s.RunningSatisfier && s.ps.rs == nil {
		s.ps.enableRunningSatisfier()
	}
	if s.StoredKeys && s.ps.keyBook == nil {
		s.ps.keyBook = newKeyCache[P, S]()
	}
	next := s.root
	ord := newPackageOrder[P](s.StableDiscovery, s.OwnOrder)
	if s.ClashFirst && s.ClashThreshold > 0 {
		if ord == nil {
			ord = &packageOrder[P]{places: map[P]int{}, marked: map[P]bool{}}
		}
		ord.tierFirst = true
	}

	var tiers *tierState[P]
	if s.ClashThreshold > 0 {
		tiers = newTierState(s.ClashThreshold, s.root)
	}
	var opts *decideOpts[P, S] // nil keeps the off path on today's code

	// report notes one clash: to the test hook first, then to the tallies.
	report := func(blamed P, involved []P, kind clashKind) {
		if s.clashHook != nil {
			s.clashHook(clash[P]{blamed: blamed, involved: involved, kind: kind})
		}
		if tiers != nil {
			tiers.record(blamed, involved)
		}
	}
	var learned func(P, *Incompatibility[P, S])
	if tiers != nil || s.clashHook != nil {
		learned = func(blamed P, inc *Incompatibility[P, S]) {
			report(blamed, incompatibilityPackages(inc, blamed), clashLearned)
		}
		opts = &decideOpts[P, S]{
			turnedDown: func(pkg P, inc *Incompatibility[P, S]) {
				report(pkg, incompatibilityPackages(inc, pkg), clashTurnedDown)
			},
		}
		if tiers != nil {
			opts.tierOf = tiers.tierOf
		}
	}

	if ord != nil {
		if opts == nil {
			opts = &decideOpts[P, S]{}
		}
		opts.ord = ord
	}

	// settle propagates from pkg and resolves any conflict. It reports whether
	// there was one, and where to resume if so.
	settle := func(pkg P) (bool, P, error) {
		result := Propagate(s.ps, s.st, pkg)
		if !result.HasConflict() {
			return false, pkg, nil
		}
		resolution, err := resolve(s.ps, s.st, s.root, result.Conflict, learned)
		if err != nil {
			return true, pkg, err
		}
		if resolution.Unsolvable {
			return true, pkg, &Unsolvable[P, S]{RootCause: resolution.Incompatibility}
		}
		return true, resolution.Package, nil
	}

	for round := 0; ; round++ {
		// Conflicts first: a conflict limit reached in the last round wins a tie.
		if s.MaxConflicts > 0 && s.ps.stats.Backtracks >= s.MaxConflicts {
			return nil, &MaxConflictsError{Limit: s.MaxConflicts}
		}
		if s.MaxRounds > 0 && round >= s.MaxRounds {
			return nil, fmt.Errorf("solver: gave up after %d rounds without settling", round)
		}

		// Resume on the package whose term conflict resolution left open,
		// without making a decision first.
		conflicted, resume, err := settle(next)
		if err != nil {
			return nil, err
		}
		if conflicted {
			next = resume
			continue
		}

		if tiers != nil && undoForPromotion(s.ps, tiers.promote()) && s.recheck {
			again := false
			for _, p := range assignedPackages(s.ps) {
				conflicted, resume, err = settle(p)
				if err != nil {
					return nil, err
				}
				if conflicted {
					next, again = resume, true
					break
				}
			}
			if again {
				continue
			}
		}

		outcome, err := makeDecision(s.ps, s.st, s.provider, opts)
		if err != nil {
			return nil, err
		}
		if outcome.Done {
			return s.solution()
		}
		if s.pickHook != nil {
			tier := tierDefault
			if tiers != nil {
				tier = tiers.tierOf(outcome.Package)
			}
			s.pickHook(outcome.Package, tier)
		}
		next = outcome.Package
	}
}

// finishStats copies the counters out of the partial solution.
func (s *Solver[P, S]) finishStats() {
	s.Stats = s.ps.stats
	s.Stats.LiveDependencyRecords = s.st.liveDependencyRecords()
}

// assignedPackages lists each package with an assignment, oldest first.
func assignedPackages[P comparable, S versionset.Set[S]](ps *PartialSolution[P, S]) []P {
	seen := map[P]bool{}
	var out []P
	for _, a := range ps.Assignments() {
		if !seen[a.Package] {
			seen[a.Package] = true
			out = append(out, a.Package)
		}
	}
	return out
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
