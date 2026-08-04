// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"github.com/posit-dev/go-pubgrub/term"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// Assignment is one entry in the partial solution: either a decision, which pins
// a package to one version, or a derivation, which asserts a term must hold
// because unit propagation proved it.
type Assignment[P comparable, S versionset.Set[S]] struct {
	// Package is the package this assignment concerns.
	Package P

	// Term is what the assignment asserts. For a decision this is a positive
	// term over the single chosen version.
	Term term.Term[S]

	// Decision distinguishes a speculative choice from a forced consequence.
	// Only decisions raise the decision level, and only decisions are what the
	// solver reports as the answer.
	Decision bool

	// Level is the number of decisions made at or before this assignment.
	//
	// Decision level, not position, is the unit of backtracking: undoing a
	// conflict discards every assignment above some level in one atomic step,
	// never a single assignment on its own.
	Level int

	// Cause is the incompatibility whose near-satisfaction forced a derivation.
	// Nil for a decision, which nothing forced.
	Cause *Incompatibility[P, S]
}

// PartialSolution is the chronological list of assignments made so far: the
// solver's running partial answer.
//
// It grows through unit propagation and decision making, and shrinks only from
// the end, down to some decision level, during conflict resolution. It never
// grows and shrinks in the same step — backtracking is a distinct, atomic act.
//
// # The accumulated term per package
//
// Alongside the chronological list, a per-package accumulated term is
// maintained: the intersection of every assignment about that package so far.
// That intersection is what the relation tests in unit propagation are
// evaluated against, and keeping it incrementally avoids re-intersecting a
// package's whole history on every check.
//
// The zero value is an empty partial solution at decision level 0 and is ready
// to use.
type PartialSolution[P comparable, S versionset.Set[S]] struct {
	assignments []Assignment[P, S]

	// accumulated caches the intersection of all terms per package.
	accumulated map[P]term.Term[S]

	// level is the current decision level, i.e. the number of decisions made.
	level int
}

// NewPartialSolution returns an empty partial solution.
func NewPartialSolution[P comparable, S versionset.Set[S]]() *PartialSolution[P, S] {
	return &PartialSolution[P, S]{accumulated: make(map[P]term.Term[S])}
}

// ensure lazily initializes, so the zero value works.
func (ps *PartialSolution[P, S]) ensure() {
	if ps.accumulated == nil {
		ps.accumulated = make(map[P]term.Term[S])
	}
}

// Level reports the current decision level: the number of decisions made.
//
// Level 0 means no decisions yet, which is where derivations from the root fact
// live. The specification's §11 records that the primary source is internally
// inconsistent about whether the floor is 0 or 1; this implementation uses 0
// throughout, matching every worked example in that source.
func (ps *PartialSolution[P, S]) Level() int { return ps.level }

// Len reports how many assignments have been made.
func (ps *PartialSolution[P, S]) Len() int { return len(ps.assignments) }

// Assignments returns the chronological assignments. The slice must not be
// modified.
func (ps *PartialSolution[P, S]) Assignments() []Assignment[P, S] { return ps.assignments }

// Decide records a decision: pkg is pinned to the single version denoted by set.
//
// This raises the decision level, because a decision is a guess that may have to
// be retracted as a unit.
func (ps *PartialSolution[P, S]) Decide(pkg P, set S) {
	ps.ensure()
	ps.level++
	ps.append(Assignment[P, S]{
		Package:  pkg,
		Term:     term.Positive(set),
		Decision: true,
		Level:    ps.level,
	})
}

// Derive records a derivation: t must hold for pkg, because cause was almost
// satisfied.
//
// The decision level is unchanged. A derivation is a consequence, not a guess,
// so it belongs to whatever level was current when it was forced.
func (ps *PartialSolution[P, S]) Derive(pkg P, t term.Term[S], cause *Incompatibility[P, S]) {
	ps.ensure()
	ps.append(Assignment[P, S]{
		Package: pkg,
		Term:    t,
		Level:   ps.level,
		Cause:   cause,
	})
}

// append records the assignment and folds it into the accumulated term.
func (ps *PartialSolution[P, S]) append(a Assignment[P, S]) {
	ps.assignments = append(ps.assignments, a)

	if existing, ok := ps.accumulated[a.Package]; ok {
		ps.accumulated[a.Package] = existing.Intersect(a.Term)
	} else {
		ps.accumulated[a.Package] = a.Term
	}
}

// Accumulated returns the intersection of every term asserted about pkg, and
// whether anything has been asserted at all.
//
// The distinction matters: "nothing asserted" is not the same as "asserted
// something vacuous". A package with no assignments is inconclusive for a
// positive term, because a version could still be decided later, whereas an
// accumulated always-true term would wrongly report satisfaction of negative
// terms only.
func (ps *PartialSolution[P, S]) Accumulated(pkg P) (term.Term[S], bool) {
	t, ok := ps.accumulated[pkg]
	return t, ok
}

// Relation reports what the partial solution ENTAILS about t, for t's package.
//
// # Unknown is inconclusive for both polarities
//
// With no assignment for the package, the answer is Inconclusive whatever the
// term's polarity. This is worth being precise about, because it looks like it
// contradicts the term algebra and does not.
//
// A term's truth in a COMPLETED world is one question: there, absence makes
// every negative term true, which is the asymmetry term.Term documents. What the
// partial solution entails is a different question. An unassigned package is not
// known-absent, it is merely undecided — a version could still be decided later,
// which would make a negative term false. So nothing is entailed either way.
//
// Treating unknown as Satisfied for negative terms breaks the algorithm at its
// core: a dependency incompatibility is {depender: Positive, dependee: Negative},
// so once the depender is selected the dependee's negative term would read as
// already satisfied, the incompatibility would classify as fully satisfied, and
// every dependency would be reported as a conflict instead of being DERIVED.
// Dependency resolution would never resolve anything.
func (ps *PartialSolution[P, S]) Relation(pkg P, t term.Term[S]) term.Relation {
	accumulated, ok := ps.accumulated[pkg]
	if !ok {
		return term.Inconclusive
	}
	return accumulated.Relation(t)
}

// DecisionFor returns the version decided for pkg, and whether a decision has
// been made.
func (ps *PartialSolution[P, S]) DecisionFor(pkg P) (S, bool) {
	// Scanning backwards finds the decision quickly, since a package is decided
	// at most once and decisions tend to be recent.
	for i := len(ps.assignments) - 1; i >= 0; i-- {
		a := ps.assignments[i]
		if a.Decision && a.Package == pkg {
			return a.Term.Set(), true
		}
	}
	var zero S
	return zero, false
}

// Decisions returns every decision in chronological order. When the solve
// succeeds, this is the answer.
func (ps *PartialSolution[P, S]) Decisions() []Assignment[P, S] {
	out := make([]Assignment[P, S], 0, ps.level)
	for _, a := range ps.assignments {
		if a.Decision {
			out = append(out, a)
		}
	}
	return out
}

// SatisfierOf returns the index of the first assignment at which the partial
// solution, read in order, comes to satisfy t — and false if it never does.
//
// This is the "satisfier" conflict resolution is defined in terms of: the single
// assignment that tips a term from not-yet-satisfied to satisfied. It is
// computed by replaying the prefix rather than by inspecting the accumulated
// term, because the question is specifically *when* satisfaction happened.
func (ps *PartialSolution[P, S]) SatisfierOf(pkg P, t term.Term[S]) (int, bool) {
	var running term.Term[S]
	started := false

	for i, a := range ps.assignments {
		if a.Package != pkg {
			continue
		}

		if !started {
			running = a.Term
			started = true
		} else {
			running = running.Intersect(a.Term)
		}

		if running.Satisfies(t) {
			return i, true
		}
	}
	return 0, false
}

// BacktrackTo discards every assignment above the given decision level and sets
// the current level to it.
//
// Truncation is by level rather than by count on purpose: a conflict invalidates
// a decision and everything derived under it, and those must go together. The
// accumulated terms are rebuilt from what remains, since intersection cannot be
// undone incrementally.
func (ps *PartialSolution[P, S]) BacktrackTo(level int) {
	ps.ensure()

	if level < 0 {
		level = 0
	}

	kept := ps.assignments
	for i, a := range ps.assignments {
		if a.Level > level {
			kept = ps.assignments[:i]
			break
		}
	}
	ps.assignments = kept
	ps.level = level

	ps.accumulated = make(map[P]term.Term[S], len(ps.accumulated))
	for _, a := range ps.assignments {
		if existing, ok := ps.accumulated[a.Package]; ok {
			ps.accumulated[a.Package] = existing.Intersect(a.Term)
		} else {
			ps.accumulated[a.Package] = a.Term
		}
	}
}
