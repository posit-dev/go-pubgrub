// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"errors"
	"math/rand"
	"strings"
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// --- §7.2: satisfier and previous satisfier ---

// TestSatisfierOfTakesTheLatestTerm pins §7.2's satisfier as the MAXIMUM over the
// per-term satisfiers, on a case where the wrong pick is not merely different but
// changes what conflict resolution does.
//
// The incompatibility is completed by the LAST of its terms to become satisfied.
// Picking any other one names an assignment that did not complete the conflict —
// and here that assignment is a decision, so §7.4's "satisfier is a decision"
// escape would fire, resolution would stop a round early, and the generalization
// it exists to derive would never be produced. Nothing about the answer looks
// wrong afterwards; the solver is just weaker.
func TestSatisfierOfTakesTheLatestTerm(t *testing.T) {
	ps := newPS()
	ps.Decide("a", versionset.Exactly(1))             // index 0 — satisfies a's term
	ps.Derive("b", pos(versionset.AtLeast(1)), nil)   // index 1 — not yet enough for b
	ps.Derive("b", pos(versionset.LessThan(10)), nil) // index 2 — completes b's term
	ps.Derive("c", pos(versionset.Exactly(1)), nil)   // index 3 — irrelevant to inc
	inc := NewIncompatibility(KindDependency, map[string]tm{
		"a": pos(versionset.Exactly(1)),
		"b": pos(versionset.Range(1, 10)),
	})

	if satisfaction, _ := Classify(ps, inc); satisfaction != FullySatisfied {
		t.Fatalf("precondition: Classify = %v, want fully satisfied", satisfaction)
	}

	index, pkg, ok := ps.SatisfierOf(inc)
	if !ok {
		t.Fatal("a fully satisfied incompatibility has a satisfier")
	}
	if index != 2 || pkg != "b" {
		t.Errorf("SatisfierOf = index %d about %q, want index 2 about \"b\": the incompatibility "+
			"is not satisfied until the LAST of its terms is, and a's was satisfied at index 0",
			index, pkg)
	}
	if ps.Assignments()[index].Decision {
		t.Error("the satisfier here is a derivation; reading it as the decision at index 0 is " +
			"exactly the mistake that skips a round of conflict resolution")
	}
}

func TestSatisfierOfReportsFalseWhenNotSatisfied(t *testing.T) {
	ps := newPS()
	ps.Decide("a", versionset.Exactly(1))

	inc := dep("a", versionset.Exactly(1), "b", versionset.Exactly(1))
	if _, _, ok := ps.SatisfierOf(inc); ok {
		t.Error("an almost-satisfied incompatibility has no satisfier: nothing has completed it")
	}

	// Every prefix satisfies the empty incompatibility, including the empty one, so
	// no single assignment can be named its satisfier. §7.4 checks for it first.
	if _, _, ok := ps.SatisfierOf(NewIncompatibility(KindDerived, map[string]tm{})); ok {
		t.Error("the empty incompatibility has no satisfier")
	}
}

// TestPreviousSatisfierOfOtherTerms covers §7.2's second branch: the satisfier
// completed its own term alone, so what the prefix has to supply is every OTHER
// term — and the assignment that did so need not be about the satisfier's package.
func TestPreviousSatisfierOfOtherTerms(t *testing.T) {
	ps := newPS()
	ps.Derive("x", pos(versionset.Exactly(1)), nil) // index 0 — nothing to do with inc
	ps.Decide("a", versionset.Exactly(1))           // index 1 — supplies a's term
	ps.Derive("b", pos(versionset.Exactly(5)), nil) // index 2 — completes inc

	inc := NewIncompatibility(KindDependency, map[string]tm{
		"a": pos(versionset.Exactly(1)),
		"b": pos(versionset.AtLeast(1)),
	})

	satisfier, _, ok := ps.SatisfierOf(inc)
	if !ok || satisfier != 2 {
		t.Fatalf("precondition: satisfier = %d (ok=%v), want 2", satisfier, ok)
	}

	previous, exists := ps.PreviousSatisfierOf(inc, satisfier)
	if !exists {
		t.Fatal("the satisfier completed only its own term, so an earlier assignment supplied " +
			"the other one and is the previous satisfier")
	}
	if previous != 1 {
		t.Errorf("previous satisfier = %d, want 1 — the earliest prefix that, with the "+
			"satisfier, satisfies the whole incompatibility, not merely the assignment before it",
			previous)
	}
}

// TestPreviousSatisfierOfJointlySatisfied covers §7.2's first branch: the
// satisfier's own term required a range no single assignment established, so it
// only completed it jointly with an earlier assignment about the SAME package.
func TestPreviousSatisfierOfJointlySatisfied(t *testing.T) {
	ps := newPS()
	ps.Derive("a", pos(versionset.AtLeast(1)), nil)   // index 0 — half of a's term
	ps.Derive("z", pos(versionset.Exactly(1)), nil)   // index 1 — unrelated
	ps.Derive("a", pos(versionset.LessThan(10)), nil) // index 2 — completes it, jointly
	inc := NewIncompatibility(KindUnavailable, map[string]tm{
		"a": pos(versionset.Range(1, 10)),
	})

	satisfier, _, ok := ps.SatisfierOf(inc)
	if !ok || satisfier != 2 {
		t.Fatalf("precondition: satisfier = %d (ok=%v), want 2", satisfier, ok)
	}

	previous, exists := ps.PreviousSatisfierOf(inc, satisfier)
	if !exists {
		t.Fatal("the satisfier did not establish a's term on its own, so the earlier " +
			"assignment about a is the previous satisfier")
	}
	if previous != 0 {
		t.Errorf("previous satisfier = %d, want 0 — the earlier assignment about the "+
			"satisfier's OWN package, which it needed to complete the term", previous)
	}
}

func TestPreviousSatisfierOfNoneWhenSatisfierAloneSuffices(t *testing.T) {
	ps := newPS()
	ps.Derive("a", pos(versionset.AtLeast(1)), nil)
	ps.Decide("a", versionset.Exactly(3)) // index 1 — satisfies the term by itself

	inc := NewIncompatibility(KindUnavailable, map[string]tm{
		"a": pos(versionset.Range(3, 4)),
	})

	satisfier, _, ok := ps.SatisfierOf(inc)
	if !ok {
		t.Fatal("precondition: expected a satisfier")
	}
	if _, exists := ps.PreviousSatisfierOf(inc, satisfier); exists {
		t.Error("the satisfier satisfies the whole incompatibility on its own, so there is no " +
			"previous satisfier and §7.4 must fall back to the base level")
	}
}

// --- §7.3: the resolution step ---

// TestPriorCauseCombinedTermIdentity pins the algebraic identity §7.3 step 3 rests
// on, which §11 item 3 records as demonstrated by one worked case rather than
// proven for every shape of overlap.
//
// The specification writes the combined term as ¬(satisfier \ term), phrased that
// way so it can be computed without materializing the cause's own term. The
// generalized resolution rule says it should be term ∪ (cause's term), and the
// satisfier's assignment IS the negation of the cause's term. So the two must
// agree for every pair of ranges and every combination of polarities — if they
// ever disagree, one of the two readings of §7.3 is wrong and the worked example
// does not say which.
func TestPriorCauseCombinedTermIdentity(t *testing.T) {
	rng := rand.New(rand.NewSource(18655))

	for i := 0; i < 2000; i++ {
		satisfierTerm := randomTerm(rng)
		incTerm := randomTerm(rng)

		// The spec's phrasing, and the rule's phrasing. The cause's term for the
		// package is the negation of the satisfier's assignment.
		spec := satisfierTerm.Intersect(incTerm.Negate()).Negate()
		rule := incTerm.Union(satisfierTerm.Negate())

		if !spec.Equal(rule) {
			t.Fatalf("¬(satisfier \\ term) = %v but term ∪ ¬satisfier = %v, for satisfier %v "+
				"and term %v", spec, rule, satisfierTerm, incTerm)
		}
	}
}

// TestPriorCauseStep3GuardMatchesNormalization pins why §7.3's step-3 condition is
// an optimization rather than a correctness condition, which is not obvious from
// the specification's phrasing and matters to anyone reading that branch.
//
// "The satisfier satisfies inc's term" is by definition "satisfier ∧ ¬term is
// always false", and the combined term step 3 would add is the negation of exactly
// that expression. So whenever the guard skips, the term it skipped adding would
// have been Negative(∅) — which §3's normalization drops. The two must agree for
// every pair of terms, or one of them has drifted from the algebra.
//
// Found by mutation: forcing step 3 to fire unconditionally left every test
// passing, including §10's trace. That looked like a coverage gap and is actually
// this identity.
func TestPriorCauseStep3GuardMatchesNormalization(t *testing.T) {
	rng := rand.New(rand.NewSource(719))

	for i := 0; i < 2000; i++ {
		satisfierTerm := randomTerm(rng)
		incTerm := randomTerm(rng)

		guardSkips := satisfierTerm.Satisfies(incTerm)
		combined := satisfierTerm.Intersect(incTerm.Negate()).Negate()

		if guardSkips != combined.IsAlwaysTrue() {
			t.Fatalf("guard skips = %v but the combined term %v is always-true = %v, for "+
				"satisfier %v and term %v", guardSkips, combined, combined.IsAlwaysTrue(),
				satisfierTerm, incTerm)
		}
	}
}

// TestPriorCauseIsImpliedByItsParents is §11 item 3's property test: fuzz pairs of
// incompatibilities and confirm the derived prior cause is logically implied by
// them.
//
// Implication is checked by enumerating every world over the packages involved —
// each either absent or at one of a few versions — and asserting that any world
// violating the derived incompatibility violates at least one parent. That is what
// "implied" means, and it is the property the whole algorithm's soundness rests on:
// a derived incompatibility that is not implied is a constraint the solver invented,
// and it will reject solutions that are perfectly valid.
//
// It exercises both of §7.3's branches, including the collapse where the union of
// the two terms is a tautology and the package's term drops out entirely.
func TestPriorCauseIsImpliedByItsParents(t *testing.T) {
	rng := rand.New(rand.NewSource(20260810))
	pkgs := []string{"p", "q", "r"}
	versions := []int64{1, 2, 3}
	allWorlds := worlds(pkgs, versions)

	collapsed := 0
	for i := 0; i < 400; i++ {
		// inc is the conflict, cause is what derived its satisfier. They disagree
		// about p, and each carries one other term.
		incTerm := randomTerm(rng)
		causeTerm := randomTerm(rng)

		inc := NewIncompatibilityFrom(KindDerived,
			pt("p", incTerm), pt("q", randomTerm(rng)))
		cause := NewIncompatibilityFrom(KindDerived,
			pt("p", causeTerm), pt("r", randomTerm(rng)))

		// The satisfier's assignment is the negation of the cause's term for p,
		// which is what unit propagation records when cause fires.
		derived := priorCause(inc, cause, "p", causeTerm.Negate(), incTerm)
		if _, mentioned := derived.Term("p"); !mentioned {
			collapsed++
		}

		for _, world := range allWorlds {
			if !derived.ViolatedBy(world) {
				continue
			}
			if !inc.ViolatedBy(world) && !cause.ViolatedBy(world) {
				t.Fatalf("world %v violates the derived %v but neither parent %v nor %v — "+
					"the resolution step invented a constraint", world, derived, inc, cause)
			}
		}
	}

	if collapsed == 0 {
		t.Error("no case collapsed the combined term away, so §7.3's step-3-skipped branch " +
			"went untested; the generator is not producing satisfying satisfiers")
	}
}

// TestPriorCauseAddsTheCombinedTermWhenJointlySatisfied pins §7.3 step 3 firing.
//
// When the satisfier established its package's term only jointly with an earlier
// assignment, dropping that package's term outright would throw away the part of
// the constraint the satisfier did not cover — producing an incompatibility
// stronger than its parents justify.
func TestPriorCauseAddsTheCombinedTermWhenJointlySatisfied(t *testing.T) {
	// inc needs a in [1,10); the satisfier only established a >= 1, so the two
	// together are what satisfied it.
	inc := NewIncompatibilityFrom(KindDerived,
		pt("a", pos(versionset.Range(1, 10))),
		pt("b", pos(versionset.Exactly(1))))
	cause := NewIncompatibilityFrom(KindDependency,
		pt("a", neg(versionset.AtLeast(1))),
		pt("c", pos(versionset.Exactly(1))))

	satisfierTerm := pos(versionset.AtLeast(1)) // does NOT satisfy [1,10) on its own
	incTerm, _ := inc.Term("a")
	derived := priorCause(inc, cause, "a", satisfierTerm, incTerm)

	got, ok := derived.Term("a")
	if !ok {
		t.Fatal("§7.3 step 3: a combined term for the satisfier's package must go back in, " +
			"because the satisfier did not cover the whole of inc's term by itself")
	}
	// ¬([1,∞) \ [1,10)) = ¬[10,∞), i.e. "a is not >= 10".
	if want := neg(versionset.AtLeast(10)); !got.Equal(want) {
		t.Errorf("combined term = %v, want %v", got, want)
	}
	if _, ok := derived.Term("b"); !ok {
		t.Error("inc's other terms must be kept")
	}
	if _, ok := derived.Term("c"); !ok {
		t.Error("the cause's other terms must be kept")
	}
}

// --- §7.4: the loop, the escapes, and the floor ---

// TestResolveBacktracksToTheRootDecision is the test CLAUDE.md asks for by name:
// under- and over-backtracking at the very first decision.
//
// The state is §10's step 8: the root is decided, one derivation hangs off it, and
// a second decision has just made a single-term incompatibility fully satisfied.
// The satisfier is that second decision and there is no previous satisfier, so
// §7.4 falls back to the base level — the case §11 item 1 records as the one the
// primary source contradicts itself about.
//
// Three assertions, and each fails for a different wrong floor:
//
//   - The conflicting decision is discarded. A floor equal to the satisfier's own
//     level keeps it, the conflict stays FULLY satisfied, §7.1's guarantee 1 is
//     violated, and the main loop spins on the same conflict forever. This is the
//     direction reached by adopting §10's level numbering without changing the
//     floor to match.
//   - The root decision and the derivation forced by it survive. A floor below the
//     root's level discards them, which still converges — the work is immediately
//     re-derived and re-decided — so nothing fails loudly and only the assignment
//     count reveals it.
//   - What comes back is ALMOST satisfied, which is the guarantee the floor exists
//     to deliver, stated directly rather than inferred from the two above.
func TestResolveBacktracksToTheRootDecision(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	rootFact := NewIncompatibility(KindRoot, map[string]tm{"app": neg(versionset.Exactly(v100))})
	i1 := dep("app", versionset.Exactly(v100), "http", versionset.AtLeast(v100))
	st.Add(rootFact)
	st.Add(i1)

	ps.Derive("app", pos(versionset.Exactly(v100)), rootFact) // index 0, level 0
	ps.Decide("app", versionset.Exactly(v100))                // index 1, level 1 — the root decision
	ps.Derive("http", pos(versionset.AtLeast(v100)), i1)      // index 2, level 1
	ps.Decide("http", versionset.Exactly(v200))               // index 3, level 2 — the guess that fails

	// A single-term incompatibility about http, fully satisfied by that decision:
	// §10's I4.
	i4 := NewIncompatibility(KindDerived, map[string]tm{"http": pos(versionset.Range(v200, v250))})
	st.Add(i4)
	if satisfaction, _ := Classify(ps, i4); satisfaction != FullySatisfied {
		t.Fatalf("precondition: Classify(I4) = %v, want fully satisfied", satisfaction)
	}

	resolution, err := Resolve(ps, st, "app", i4)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolution.Unsolvable {
		t.Fatal("a conflict over a non-root package is not a proof that nothing can be solved")
	}

	if _, decided := ps.DecisionFor("http"); decided {
		t.Error("UNDER-backtracked: the http decision survived, so the conflict is still fully " +
			"satisfied and propagation will report it again — the main loop cannot make progress")
	}
	if _, decided := ps.DecisionFor("app"); !decided {
		t.Error("OVER-backtracked: the root decision was discarded. Nothing below the root's " +
			"level is a retractable guess, and discarding it only forces the same work again")
	}
	if got, ok := ps.Accumulated("http"); !ok || !got.Equal(pos(versionset.AtLeast(v100))) {
		t.Errorf("OVER-backtracked: the derivation at the root's level should survive, got %v (ok=%v)",
			got, ok)
	}
	if ps.Len() != 3 {
		t.Errorf("after the backjump: %d assignments, want 3", ps.Len())
	}
	if satisfaction, open := Classify(ps, resolution.Incompatibility); satisfaction != AlmostSatisfied || open != "http" {
		t.Errorf("returned incompatibility classifies as %v (open %q), want almost satisfied "+
			"about http — §7.1's guarantee 1 is what the floor exists to deliver",
			satisfaction, open)
	}
}

// TestResolveWithNoDecisionsKeepsResolving covers the same floor before any
// decision exists, which is where a floor hard-coded to this scheme's 1 goes
// wrong.
//
// With no decisions every assignment sits at level 0. The satisfier is a
// derivation, and taking the floor to be 1 makes previousSatisfierLevel differ
// from the satisfier's level, so §7.4's second escape fires — and the truncation it
// then performs removes nothing at all, because there is nothing above level 0.
// The conflict comes back still fully satisfied and the loop spins.
//
// The floor is read off the partial solution instead, so it is 0 here, the levels
// match, and resolution correctly folds in the cause — reaching the empty
// incompatibility, which is the proof that the problem is unsolvable.
func TestResolveWithNoDecisionsKeepsResolving(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	// "a must be version 1" as a root-style negative fact, and "a version 1 is
	// unavailable". Together: unsolvable, with no decision ever made.
	required := NewIncompatibility(KindRoot, map[string]tm{"a": neg(versionset.Exactly(1))})
	forbidden := NewIncompatibility(KindUnavailable, map[string]tm{"a": pos(versionset.Exactly(1))})
	st.Add(required)
	st.Add(forbidden)

	ps.Derive("a", pos(versionset.Exactly(1)), required) // level 0, and its cause is recorded

	if satisfaction, _ := Classify(ps, forbidden); satisfaction != FullySatisfied {
		t.Fatalf("precondition: Classify = %v, want fully satisfied", satisfaction)
	}
	if ps.Level() != 0 {
		t.Fatalf("precondition: level = %d, want 0 (no decisions)", ps.Level())
	}

	resolution, err := Resolve(ps, st, "root", forbidden)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !resolution.Unsolvable {
		t.Fatalf("Resolve returned %v to resume on rather than reporting the problem "+
			"unsolvable; with no decision to retract there is nothing to backtrack past, so "+
			"resolution has to keep folding in causes", resolution.Incompatibility)
	}
	if !resolution.Incompatibility.IsEmpty() {
		t.Errorf("root cause = %v, want the empty incompatibility: both terms about a cancel, "+
			"and a conjunction over nothing is violated by everything", resolution.Incompatibility)
	}
	if a, b, derived := resolution.Incompatibility.Causes(); !derived || a != forbidden || b != required {
		t.Error("the root cause must record both facts as its causes, or §9 has no proof to walk")
	}
}

func TestResolveTerminalCases(t *testing.T) {
	t.Run("empty incompatibility", func(t *testing.T) {
		ps := newPS()
		st := NewStore[string, set]()
		empty := NewIncompatibility(KindDerived, map[string]tm{})

		resolution, err := Resolve(ps, st, "root", empty)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !resolution.Unsolvable || resolution.Incompatibility != empty {
			t.Error("an incompatibility with no terms is the unsolvable-root signal")
		}
	})

	t.Run("lone positive term about the root", func(t *testing.T) {
		ps := newPS()
		st := NewStore[string, set]()
		ps.Decide("root", versionset.Exactly(1))

		// The root's version is a fixed fact with no cause chain to unwind, so this
		// cannot be traced further. Reaching for a satisfier instead would find the
		// root's own decision and truncate to a level that keeps it.
		inc := NewIncompatibility(KindDerived, map[string]tm{"root": pos(versionset.Exactly(1))})
		resolution, err := Resolve(ps, st, "root", inc)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !resolution.Unsolvable {
			t.Error("a lone positive term about the root package is the other terminal case")
		}
	})

	t.Run("a negative term about the root is not terminal", func(t *testing.T) {
		// The root fact itself is {root: Negative(version)}. Treating that shape as
		// terminal would report every solve as unsolvable the moment it fired.
		ps := newPS()
		st := NewStore[string, set]()
		inc := NewIncompatibility(KindRoot, map[string]tm{"root": neg(versionset.Exactly(1))})
		if isRootFailure(inc, "root") {
			t.Error("only a POSITIVE term about the root is §7.4's terminal shape")
		}
		// And a positive term about some other package is not terminal either.
		other := NewIncompatibility(KindDerived, map[string]tm{"a": pos(versionset.Exactly(1))})
		if isRootFailure(other, "root") {
			t.Error("a lone positive term about a non-root package is an ordinary conflict")
		}
		_ = st
		_ = ps
	})
}

// TestResolveReturnsSomethingPropagationCanFind pins the two properties around
// §7.4's "add it to the known set", which are easy to conflate and only one of
// which is about the store's size.
//
// §7.4 phrases it as "add the incompatibility if it is not the original input", and
// the reason given is to avoid a duplicate of a fact propagation already scans. But
// Store.Add deduplicates, so that condition cannot change what the store holds —
// mutating it to add unconditionally left every test passing, which is what
// surfaced the point. What the condition CAN change is whether the returned
// incompatibility is in the store at all, and propagation has to be able to find
// it: that is the whole basis on which Resolve names a package to resume from.
func TestResolveReturnsSomethingPropagationCanFind(t *testing.T) {
	// A conflict the caller has NOT stored, resolved in the branch where no round
	// of resolution runs — so the returned incompatibility is the input itself.
	build := func(store bool) (*psol, *Store[string, set], *inc) {
		ps := newPS()
		st := NewStore[string, set]()
		i1 := dep("app", versionset.Exactly(v100), "http", versionset.AtLeast(v100))
		st.Add(i1)

		conflict := NewIncompatibility(KindDerived, map[string]tm{
			"http": pos(versionset.Range(v200, v250)),
		})
		if store {
			st.Add(conflict)
		}

		ps.Decide("app", versionset.Exactly(v100))
		ps.Derive("http", pos(versionset.AtLeast(v100)), i1)
		ps.Decide("http", versionset.Exactly(v200))
		return ps, st, conflict
	}

	t.Run("the replacement is findable by propagation", func(t *testing.T) {
		ps, st, conflict := build(false)

		resolution, err := Resolve(ps, st, "app", conflict)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		found := false
		for _, inc := range st.Mentioning("http") {
			if inc.Equal(resolution.Incompatibility) {
				found = true
			}
		}
		if !found {
			t.Error("the returned incompatibility is not in the store, so propagation resuming " +
				"on the package it names cannot see it: it would derive nothing and the " +
				"conflict would be silently dropped")
		}
	})

	t.Run("no duplicate when it was already stored", func(t *testing.T) {
		ps, st, conflict := build(true)
		before := st.Len()

		if _, err := Resolve(ps, st, "app", conflict); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if st.Len() != before {
			t.Errorf("store grew from %d to %d; a fact propagation already scans must not be "+
				"stored twice, or it would derive the same consequence repeatedly",
				before, st.Len())
		}
	})
}

func TestResolveErrorsRatherThanSpinning(t *testing.T) {
	t.Run("no satisfier", func(t *testing.T) {
		// §7 is defined only over a conflict. Handed something the partial solution
		// does not satisfy, there is no satisfier to unwind from — and a loop that
		// cannot progress must say so rather than run.
		ps := newPS()
		st := NewStore[string, set]()
		inc := NewIncompatibility(KindUnavailable, map[string]tm{"a": pos(versionset.Exactly(1))})

		_, err := Resolve(ps, st, "root", inc)
		if !errors.Is(err, ErrNoSatisfier) {
			t.Errorf("err = %v, want ErrNoSatisfier", err)
		}
	})

	t.Run("derivation with no cause", func(t *testing.T) {
		// Treating a causeless derivation as a decision would take §7.4's first
		// escape and under-backtrack.
		ps := newPS()
		st := NewStore[string, set]()
		ps.Derive("a", pos(versionset.Exactly(1)), nil)

		inc := NewIncompatibility(KindUnavailable, map[string]tm{"a": pos(versionset.Exactly(1))})
		_, err := Resolve(ps, st, "root", inc)
		if err == nil {
			t.Fatal("expected an error: a derivation with no cause cannot be resolved against")
		}
		// Specifically the causeless-derivation path, not ErrNoSatisfier: this
		// conflict IS satisfied, so a satisfier exists and the loop reaches the
		// point where it needs something to resolve against.
		if errors.Is(err, ErrNoSatisfier) {
			t.Errorf("err = %v, want the causeless-derivation error; ErrNoSatisfier here would "+
				"mean the satisfier search failed instead, which is a different defect", err)
		}
		if !strings.Contains(err.Error(), "no cause") {
			t.Errorf("err = %v, want an error naming the missing cause", err)
		}
	})
}

// --- helpers ---

// randomTerm builds a term over a small pool of sets, so that fuzzed pairs
// overlap, nest, and fall disjoint often enough to exercise every branch of the
// algebra rather than mostly producing unrelated ranges.
func randomTerm(rng *rand.Rand) tm {
	sets := []set{
		versionset.Empty(),
		versionset.All(),
		versionset.Exactly(1),
		versionset.Exactly(2),
		versionset.Range(1, 3),
		versionset.Range(2, 4),
		versionset.AtLeast(2),
		versionset.LessThan(3),
	}
	s := sets[rng.Intn(len(sets))]
	if rng.Intn(2) == 0 {
		return pos(s)
	}
	return neg(s)
}

// worlds enumerates every complete selection over pkgs in which each package is
// either absent or at one of versions.
//
// Absence is a world, not a gap: it is what makes a negative term true and a
// positive one false, and an implication that holds only for worlds where
// everything is selected is not the implication the algorithm needs.
func worlds(pkgs []string, versions []int64) []map[string]set {
	out := []map[string]set{{}}

	for _, pkg := range pkgs {
		next := make([]map[string]set, 0, len(out)*(len(versions)+1))
		for _, base := range out {
			// Absent.
			next = append(next, base)
			for _, v := range versions {
				extended := make(map[string]set, len(base)+1)
				for k, s := range base {
					extended[k] = s
				}
				extended[pkg] = versionset.Exactly(v)
				next = append(next, extended)
			}
		}
		out = next
	}
	return out
}
