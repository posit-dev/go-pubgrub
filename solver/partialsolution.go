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
// Level 0 means no decisions yet. Every Decide increments, so the FIRST decision
// — including the root one — lands at level 1.
//
// # ⚠️ This numbering is off by one from the specification's, and §7 is not yet
// written
//
// §1's definition carries a parenthetical — "not counting the initial
// fact-of-existence for the root package as a 'real' decision level increment" —
// and §10's trace follows it: the root decision sits at level 0, and the first
// NON-root decision at level 1. This type cannot implement that exemption,
// because Decide does not know which package is the root; whoever owns that
// knowledge is the main loop, which lands with §8.
//
// Both schemes are internally coherent, but THE BACKTRACK FLOOR IS
// SCHEME-SPECIFIC: §10's numbering needs floor 0, this one needs floor 1. §11
// item 1 resolves the floor to 0 for §10's scheme. So when §7's conflict
// resolution is written, the floor must be chosen to match whichever scheme is
// in force here — not copied from §11 without checking.
//
// Getting that pairing wrong in one direction over-backtracks (wasteful, still
// converges, since the discarded work is re-derived and re-decided). Getting it
// wrong in the other direction UNDER-backtracks, leaving the conflict still
// fully satisfied, violating §7.1's first guarantee and letting the main loop
// spin. An earlier version of this comment claimed floor 0 "matching every
// worked example", which is §10's scheme's floor paired with this scheme's
// numbering — the over-backtracking half, and wrong for this code.
func (ps *PartialSolution[P, S]) Level() int { return ps.level }

// Len reports how many assignments have been made.
func (ps *PartialSolution[P, S]) Len() int { return len(ps.assignments) }

// Assignments returns the chronological assignments. The slice must not be
// modified.
//
// # It is INVALIDATED by BacktrackTo, not merely shortened
//
// BacktrackTo truncates by re-slicing the same backing array, so a slice
// obtained before it — and every index into one, including those from
// SatisfierOf — is stale afterwards: the next Derive appends over the discarded
// tail and silently rewrites elements the old slice still spans.
//
// This matters concretely for §7's conflict-resolution loop, which computes the
// satisfier and previous-satisfier as indices, THEN truncates, and may iterate
// again. Re-read after every BacktrackTo rather than caching across one.
func (ps *PartialSolution[P, S]) Assignments() []Assignment[P, S] { return ps.assignments }

// Decide records a decision: pkg is pinned to the single version denoted by set.
//
// This raises the decision level, because a decision is a guess that may have to
// be retracted as a unit.
//
// # PRECONDITION: set must satisfy §8's eligibility, and violating it corrupts
// the partial solution silently
//
// §8 requires the chosen version to lie "within the intersection of every term
// the partial solution has accumulated about that package so far". Decide does
// not check, and passing an ineligible version does not fail — it intersects the
// accumulated term down to Positive(∅), which is always false. term.Relation
// tests Satisfied before Contradicted, so an always-false receiver then answers
// Satisfied to EVERY term about that package, of either polarity. Classify goes
// on to report arbitrary unrelated incompatibilities as fully satisfied
// conflicts, and §7 will build a proof tree out of one.
//
// Vacuously those answers are "correct" — an inconsistent assumption set entails
// everything — which is exactly why it is dangerous: the caller cannot tell a
// real conflict from state it has already corrupted. This is a wrong derivation
// dressed as certainty.
//
// Propagation cannot cause it. AlmostSatisfied requires the open term to be
// Inconclusive, which means both accumulated ∧ t and accumulated ∧ ¬t are
// non-empty, so deriving ¬t can never empty the accumulated term. Only Decide
// can, which is why the obligation sits here. Whether to enforce it — assert,
// return an error, or leave it to the §8 candidate chooser that has to compute
// the intersection anyway — is settled when the decision strategy is written.
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
//
// # An always-false term is the one exception
//
// Positive(∅) asks for a version from an empty range, so it is false in every
// world — including one where the package is still undecided. §2.5's definition
// ("S contradicts t if t is forced false whenever every term in S is true")
// therefore yields Contradicted with nothing asserted, and §2.4 says outright
// that an incompatibility holding such a term "will never fire and never needs
// to be checked again".
//
// Without this case the inconclusive rule above swallows it: the always-false
// term looks like the single open term of an almost-satisfied incompatibility,
// and propagation derives its negation — recording an assignment of the
// vacuously-true "not ∅" for a package nothing has said anything about. That is
// not unsound, since the derived term constrains nothing, but it makes
// Accumulated report ok for an untouched package, shifts every SatisfierOf
// index, gives the junk assignment a decision level that BacktrackTo then
// honors, and leaves §9's derivation graph with a node whose cause can never
// fire. §2.5's own table is written for nonempty ranges, whose justification —
// "a version could still be decided later that lands in r" — is exactly what
// fails when r is empty.
func (ps *PartialSolution[P, S]) Relation(pkg P, t term.Term[S]) term.Relation {
	if t.IsAlwaysFalse() {
		return term.Contradicted
	}

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
// It is computed by replaying the prefix rather than by inspecting the
// accumulated term, because the question is specifically *when* satisfaction
// happened. Satisfaction is monotone under further intersection, so the earliest
// tipping point is well defined and unique.
//
// # ⚠️ This is a PER-TERM primitive and is NOT §7.2's satisfier
//
// §7.2's satisfier is "the earliest assignment such that the prefix ending at it
// already fully satisfies I" — I being the whole incompatibility. That is the
// MAXIMUM over the per-term satisfier indices, which §10 spells out: "D2
// completes the json-term on its own; the http-term was already complete
// earlier, at decision 5. The later of the two, chronologically, is D2."
//
// So a caller that iterates an incompatibility's terms and calls this for one of
// them gets a per-term answer, and Go map order decides WHICH — intermittently.
// On §10's own state the wrong pick is a decision, so §7.4's "satisfier is a
// decision" escape fires immediately, the round of resolution never happens, and
// the generalization from "http 2.0.0 is bad" to "no http in [2.0, 2.5) will ever
// work" is never derived. A silently weaker solver, not a broken one.
//
// §7.2's previousSatisfier is not expressible through this API at all: one branch
// needs the earliest prefix that satisfies I *with a designated later assignment
// injected*, the other needs the point at which every OTHER term of I became
// satisfied. Both are per-incompatibility queries. §7's implementation must add
// them over *Incompatibility rather than reach for this.
//
// The returned index is invalidated by BacktrackTo — see Assignments.
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

	// Clamp BOTH sides. Backtracking only ever moves backwards, so a target above
	// the current level is a caller error — and honoring it would raise the level
	// without any decision behind it, permanently breaking §1's definition of a
	// decision level as "the number of decisions at or before that point". §7.4's
	// correctness argument rests on that equality, and once it is false the
	// surviving assignments become unreachable by any later BacktrackTo, since
	// their levels all sit below the inflated one. The low side was already
	// clamped; the asymmetry was the whole invitation to the mistake.
	if level < 0 {
		level = 0
	}
	if level > ps.level {
		level = ps.level
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
