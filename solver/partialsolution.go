// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"fmt"

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
	accumulated accumulation[P, S]

	// level is the current decision level, i.e. the number of decisions made.
	level int
}

// NewPartialSolution returns an empty partial solution.
func NewPartialSolution[P comparable, S versionset.Set[S]]() *PartialSolution[P, S] {
	return &PartialSolution[P, S]{accumulated: make(accumulation[P, S])}
}

// ensure lazily initializes, so the zero value works.
func (ps *PartialSolution[P, S]) ensure() {
	if ps.accumulated == nil {
		ps.accumulated = make(accumulation[P, S])
	}
}

// Level reports the current decision level: the number of decisions made.
//
// Level 0 means no decisions yet. Every Decide increments, so the FIRST decision
// — including the root one — lands at level 1.
//
// # This numbering is off by one from §10's, deliberately, and the floor matches
//
// §1's definition carries a parenthetical — "not counting the initial
// fact-of-existence for the root package as a 'real' decision level increment" —
// and §10's trace follows it: the root decision sits at level 0, and the first
// NON-root decision at level 1. This type does not implement that exemption:
// Decide does not know which package is the root, and §1's MAIN sentence
// ("equal to the number of decisions at or before that point") is the invariant
// this type upholds instead, which TestLevelAlwaysEqualsDecisionCount pins.
//
// Both schemes are internally coherent, but THE BACKTRACK FLOOR IS
// SCHEME-SPECIFIC: §10's numbering needs floor 0, this one needs floor 1, and
// §11 item 1 resolves the floor to 0 only FOR §10's SCHEME. Copying that 0 here
// over-backtracks (wasteful, still converges, since the discarded work is
// re-derived and re-decided); pairing §10's numbering with floor 1
// UNDER-backtracks, leaving the conflict still fully satisfied, violating §7.1's
// first guarantee and letting the main loop spin.
//
// baseLevel is where that pairing is resolved for this scheme, and
// TestResolveBacktracksToTheRootDecision pins both failure directions. It reads
// the floor off the partial solution rather than hard-coding this scheme's 1, so
// the two cannot drift apart, and so that the floor is still correct before any
// decision has been made — a hard-coded 1 escapes conflict resolution with a
// truncation that removes nothing when the solve is still at level 0.
func (ps *PartialSolution[P, S]) Level() int { return ps.level }

// baseLevel is §7.4's floor: the decision level to truncate to when a conflict's
// satisfier has no previous satisfier.
//
// It is the level of the FIRST decision — the root's, in a solve driven by
// Solve — or the current level when nothing has been decided yet. Everything at
// or below it is either the root's own fact or forced by it, so it holds no
// retractable guess, which is what makes it the deepest safe cut.
//
// Two ways to get this wrong, both of which the spec's §11 item 1 flags as
// unresolved and both of which have tests:
//
//   - Too low (0 under this numbering) discards the root decision and every
//     derivation forced by it. The solve still converges, because that work is
//     immediately re-derived and re-decided, so nothing fails loudly — only the
//     assignment count after a backjump reveals it.
//   - Too high (the satisfier's own level) leaves the satisfier in place, so the
//     conflict is STILL fully satisfied after the truncation. §7.1's first
//     guarantee is violated, propagation finds the same conflict again, and the
//     main loop spins forever.
func (ps *PartialSolution[P, S]) baseLevel() int {
	for _, a := range ps.assignments {
		if a.Decision {
			return a.Level
		}
	}
	return ps.level
}

// Len reports how many assignments have been made.
func (ps *PartialSolution[P, S]) Len() int { return len(ps.assignments) }

// Assignments returns the chronological assignments. The slice must not be
// modified.
//
// # It is INVALIDATED by BacktrackTo, not merely shortened
//
// BacktrackTo truncates by re-slicing the same backing array, so a slice
// obtained before it — and every index into one, including those from
// SatisfierOf, PreviousSatisfierOf and FirstIndexSatisfying — is stale
// afterwards: the next Derive appends over the discarded tail and silently
// rewrites elements the old slice still spans.
//
// This matters concretely for §7's conflict-resolution loop, which computes the
// satisfier and previous-satisfier as indices, THEN truncates, and may iterate
// again. Resolve reads every assignment it needs before the single BacktrackTo it
// performs, and returns immediately afterwards, which is what keeps that safe.
func (ps *PartialSolution[P, S]) Assignments() []Assignment[P, S] { return ps.assignments }

// Eligible reports whether deciding pkg at the version denoted by set is legal
// per §8: the version must lie within the intersection of every term accumulated
// about that package so far.
//
// A package nothing has been asserted about is eligible for any non-empty set.
// The empty set is never eligible: Positive(∅) is the always-false term, so
// "deciding" it asserts something no world can satisfy.
//
// Callers choosing a version themselves should consult this first — Decide
// PANICS on an ineligible one, for the reasons documented there.
func (ps *PartialSolution[P, S]) Eligible(pkg P, set S) bool {
	if set.IsEmpty() {
		return false
	}
	asserted, ok := ps.accumulated[pkg]
	if !ok {
		return true
	}
	return !asserted.Intersect(term.Positive(set)).IsAlwaysFalse()
}

// Decide records a decision: pkg is pinned to the single version denoted by set.
//
// This raises the decision level, because a decision is a guess that may have to
// be retracted as a unit.
//
// # It PANICS when set is ineligible per §8, because the alternative is silent
// corruption
//
// §8 requires the chosen version to lie "within the intersection of every term
// the partial solution has accumulated about that package so far". An ineligible
// version intersects the accumulated term down to Positive(∅), which is always
// false. term.Relation tests Satisfied before Contradicted, so an always-false
// receiver then answers Satisfied to EVERY term about that package, of either
// polarity. Classify goes on to report arbitrary unrelated incompatibilities as
// fully satisfied conflicts, and §7 builds a proof tree out of one.
//
// Vacuously those answers are "correct" — an inconsistent assumption set entails
// everything — which is exactly why it is dangerous: the caller cannot tell a
// real conflict from state it has already corrupted. Every answer the solver
// gives afterwards is a wrong derivation dressed as certainty, and none of it
// points back here.
//
// So this is a programming error, reported as one. MakeDecision never trips it:
// it filters candidates by the accumulated term and rejects a Provider that
// returns a version outside the set it was given, so a misbehaving Provider
// surfaces as an error from the solve rather than as a panic. A caller driving
// the partial solution by hand should call Eligible first.
//
// Propagation cannot cause this. AlmostSatisfied requires the open term to be
// Inconclusive, which means both accumulated ∧ t and accumulated ∧ ¬t are
// non-empty, so deriving ¬t can never empty the accumulated term. Only Decide
// can, which is why the obligation sits here.
func (ps *PartialSolution[P, S]) Decide(pkg P, set S) {
	ps.ensure()

	if !ps.Eligible(pkg, set) {
		detail := "the chosen set holds no version"
		if asserted, ok := ps.accumulated[pkg]; ok {
			detail = "the accumulated term is " + asserted.String()
		}
		panic(fmt.Sprintf("solver: ineligible decision %s %v (§8): %s; deciding it would make "+
			"every later relation test about %s answer Satisfied",
			formatPackage(pkg), term.Positive(set), detail, formatPackage(pkg)))
	}

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
	ps.accumulated.add(a.Package, a.Term)
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
// Unknown is Inconclusive for both polarities, and an always-false term is
// Contradicted by every state including the empty one. Both rules, and why each
// is what it is, are documented on accumulation.relation — which is where they
// live, so that §7.2's prefix replays cannot answer these questions differently
// than Classify does.
func (ps *PartialSolution[P, S]) Relation(pkg P, t term.Term[S]) term.Relation {
	return ps.accumulated.relation(pkg, t)
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

// FirstIndexSatisfying returns the index of the first assignment at which the
// partial solution, read in order, comes to satisfy t — and false if it never
// does.
//
// It is computed by replaying the prefix rather than by inspecting the
// accumulated term, because the question is specifically *when* satisfaction
// happened. Satisfaction is monotone under further intersection, so the earliest
// tipping point is well defined and unique.
//
// Only assignments about pkg are folded in. That filter is not an optimization:
// terms about other packages constrain other packages, and intersecting them
// here would let an unrelated assignment "satisfy" this term.
//
// # ⚠️ This is a PER-TERM primitive and is NOT §7.2's satisfier
//
// §7.2's satisfier is "the earliest assignment such that the prefix ending at it
// already fully satisfies I" — I being the whole incompatibility. That is the
// MAXIMUM over the per-term satisfier indices, which §10 spells out: "D2
// completes the json-term on its own; the http-term was already complete
// earlier, at decision 5. The later of the two, chronologically, is D2." Use
// SatisfierOf for that question; the name of this one deliberately no longer
// invites the confusion.
//
// The returned index is invalidated by BacktrackTo — see Assignments.
func (ps *PartialSolution[P, S]) FirstIndexSatisfying(pkg P, t term.Term[S]) (int, bool) {
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

// SatisfierOf implements §7.2's satisfier: the index of the EARLIEST assignment
// whose prefix of the partial solution — the assignments up to and including it —
// already fully satisfies the whole of inc. It also returns the package whose
// term that assignment is about, which is §7.3's `term`.
//
// It reports false when inc is not fully satisfied at all, and for the empty
// incompatibility, which every prefix satisfies including the empty one so no
// single assignment can be named the satisfier. §7.4 checks its terminal cases
// before asking, so neither arises there.
//
// # It is the MAXIMUM over the per-term satisfiers, and that is the whole point
//
// Every term of a fully satisfied incompatibility has its own tipping point; the
// incompatibility is not satisfied until the LAST of them. Taking any other one —
// for instance whichever term Go's map iteration happens to yield first — picks
// an assignment that did not complete the conflict. On §10's own state the wrong
// pick is a decision, so §7.4's "satisfier is a decision" escape fires
// immediately, the round of resolution never runs, and the generalization the
// example exists to demonstrate — from "http 2.0.0 is bad" to "no http in
// [2.0, 2.5) will ever work" — is never derived. A silently weaker solver, which
// is the hard kind to notice.
//
// The returned index is invalidated by BacktrackTo — see Assignments.
func (ps *PartialSolution[P, S]) SatisfierOf(inc *Incompatibility[P, S]) (index int, pkg P, ok bool) {
	var zero P
	if inc.IsEmpty() {
		return 0, zero, false
	}

	index = -1
	for p, t := range inc.terms {
		at, found := ps.FirstIndexSatisfying(p, t)
		if !found {
			return 0, zero, false
		}
		if at > index {
			index, pkg = at, p
		}
	}
	return index, pkg, true
}

// PreviousSatisfierOf implements §7.2's previous satisfier: the index of the
// EARLIEST assignment strictly before satisfier such that the prefix ending at
// it, PLUS the satisfier, together fully satisfy inc. It reports false when the
// satisfier needs no help at all.
//
// satisfier must be an index returned by SatisfierOf for this same inc and this
// same, untruncated, partial solution.
//
// # One query covers both of §7.2's branches
//
// §7.2 describes two situations and they are the same question asked of a prefix
// with the satisfier injected:
//
//   - The satisfier's own term needed help from an EARLIER assignment about the
//     SAME package — its term requires a range no single assignment established.
//     Then the earliest qualifying prefix is the one that includes that earlier
//     assignment.
//   - The satisfier completed its own term alone, and what the prefix still has
//     to supply is every OTHER term of inc. Then the earliest qualifying prefix
//     is the one reaching the assignment that finished those off — which need not
//     be about the satisfier's package at all. This is §10's case: the satisfier
//     is D2 and the previous satisfier is the `http 2.0.0` decision.
//
// Asking it as one query is not a shortcut. Written as two branches, each needs
// its own notion of "enough", and the case where both apply at once — the
// satisfier needs same-package help AND another term is still outstanding — has
// to take the later of the two answers, which is exactly what a single search for
// the earliest sufficient prefix computes.
func (ps *PartialSolution[P, S]) PreviousSatisfierOf(inc *Incompatibility[P, S], satisfier int) (int, bool) {
	if satisfier < 0 || satisfier >= len(ps.assignments) {
		return 0, false
	}

	// The satisfier goes in first, so every test below is "this prefix, plus the
	// satisfier". Intersection is commutative, so injecting it out of
	// chronological order changes nothing.
	replay := make(accumulation[P, S], len(inc.terms))
	sat := ps.assignments[satisfier]
	replay.add(sat.Package, sat.Term)

	if replay.satisfies(inc) {
		// The satisfier alone is enough and nothing earlier is required, so there
		// is no previous satisfier. §7.4 then falls back to the base level.
		return 0, false
	}

	for i := 0; i < satisfier; i++ {
		a := ps.assignments[i]
		replay.add(a.Package, a.Term)
		if replay.satisfies(inc) {
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

	ps.accumulated = make(accumulation[P, S], len(ps.accumulated))
	for _, a := range ps.assignments {
		ps.accumulated.add(a.Package, a.Term)
	}
}
