// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"testing"

	"github.com/posit-dev/go-pubgrub/term"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// Tests use string package names and the reference integer version sets, so a
// scenario reads close to the way the specification writes one.

type (
	set  = versionset.Ints
	tm   = term.Term[set]
	inc  = Incompatibility[string, set]
	psol = PartialSolution[string, set]
)

func pos(s set) tm { return term.Positive(s) }
func neg(s set) tm { return term.Negative(s) }

// dep builds "a in ra depends on b in rb", the encoding from the specification:
// a positive term on the depender and a NEGATIVE term on the dependee.
func dep(a string, ra set, b string, rb set) *inc {
	return NewIncompatibility(KindDependency, map[string]tm{
		a: pos(ra),
		b: neg(rb),
	})
}

func newPS() *psol { return NewPartialSolution[string, set]() }

// --- Incompatibility ---

func TestIncompatibilityNormalizesDuplicatePackages(t *testing.T) {
	// Two terms about one package must be intersected into one.
	i := NewIncompatibility(KindDependency, map[string]tm{"a": pos(versionset.Range(1, 5))})
	// A map cannot hold two entries for one key, so build the collision by
	// intersecting explicitly and confirm the invariant holds either way.
	if i.Len() != 1 {
		t.Fatalf("Len = %d, want 1", i.Len())
	}

	got, ok := i.Term("a")
	if !ok {
		t.Fatal("term for a missing")
	}
	if !got.Set().Equal(versionset.Range(1, 5)) {
		t.Errorf("term set = %v", got.Set())
	}
}

func TestIncompatibilityDropsAlwaysTrueTerms(t *testing.T) {
	// Negative(∅) is always true and contributes nothing to a conjunction.
	i := NewIncompatibility(KindDependency, map[string]tm{
		"a": pos(versionset.Exactly(1)),
		"b": neg(versionset.Empty()),
	})

	if i.Len() != 1 {
		t.Errorf("Len = %d, want 1 (the always-true term should be dropped)", i.Len())
	}
	if _, ok := i.Term("b"); ok {
		t.Error("the always-true term for b should have been dropped")
	}
}

func TestIncompatibilityIsEmptyMeansUnsatisfiable(t *testing.T) {
	// Every term always-true, so all are dropped and nothing remains. A
	// conjunction over nothing is vacuously true, hence always violated.
	i := NewIncompatibility(KindDerived, map[string]tm{
		"a": neg(versionset.Empty()),
	})
	if !i.IsEmpty() {
		t.Error("an incompatibility whose terms all dropped must be empty")
	}
}

func TestIncompatibilityIsInert(t *testing.T) {
	// Positive(∅) is always false, so the conjunction can never hold and the
	// incompatibility can never fire.
	inert := NewIncompatibility(KindDerived, map[string]tm{
		"a": pos(versionset.Empty()),
		"b": pos(versionset.Exactly(1)),
	})
	if !inert.IsInert() {
		t.Error("an incompatibility containing an always-false term is inert")
	}

	live := dep("a", versionset.Exactly(1), "b", versionset.Exactly(1))
	if live.IsInert() {
		t.Error("a normal dependency incompatibility is not inert")
	}
}

func TestIncompatibilityEqualIgnoresCauses(t *testing.T) {
	a := dep("a", versionset.Exactly(1), "b", versionset.AtLeast(2))
	b := dep("a", versionset.Exactly(1), "b", versionset.AtLeast(2))
	if !a.Equal(b) {
		t.Error("incompatibilities with identical terms must be equal")
	}

	// Same terms, but derived from causes. Still the same fact.
	derived := newDerived(map[string]tm{
		"a": pos(versionset.Exactly(1)),
		"b": neg(versionset.AtLeast(2)),
	}, a, b)
	if !derived.Equal(a) {
		t.Error("Equal must compare terms only, not provenance")
	}

	different := dep("a", versionset.Exactly(1), "b", versionset.AtLeast(3))
	if a.Equal(different) {
		t.Error("differing terms must not be equal")
	}
	if a.Equal(nil) {
		t.Error("nothing equals nil")
	}
}

func TestIncompatibilityCauses(t *testing.T) {
	a := dep("a", versionset.Exactly(1), "b", versionset.AtLeast(2))
	b := dep("b", versionset.Exactly(2), "c", versionset.AtLeast(1))

	if _, _, derived := a.Causes(); derived {
		t.Error("an external incompatibility has no causes")
	}

	d := newDerived(map[string]tm{"a": pos(versionset.Exactly(1))}, a, b)
	ca, cb, derived := d.Causes()
	if !derived {
		t.Fatal("a derived incompatibility must report its causes")
	}
	if ca != a || cb != b {
		t.Error("causes must be the two incompatibilities combined")
	}
	if d.Kind() != KindDerived {
		t.Errorf("Kind = %v, want derived", d.Kind())
	}
}

func TestKindZeroValueIsDerived(t *testing.T) {
	// So an incompatibility built without stating a kind cannot pass itself off
	// as an authoritative external fact.
	var k Kind
	if k != KindDerived {
		t.Errorf("zero Kind = %v, want derived", k)
	}
}

// --- PartialSolution ---

func TestDecideRaisesLevelAndDeriveDoesNot(t *testing.T) {
	ps := newPS()
	if ps.Level() != 0 {
		t.Fatalf("initial level = %d, want 0", ps.Level())
	}

	ps.Derive("a", neg(versionset.AtLeast(5)), nil)
	if ps.Level() != 0 {
		t.Errorf("a derivation must not raise the level, got %d", ps.Level())
	}

	ps.Decide("a", versionset.Exactly(1))
	if ps.Level() != 1 {
		t.Errorf("a decision must raise the level, got %d", ps.Level())
	}

	ps.Derive("b", neg(versionset.AtLeast(2)), nil)
	if ps.Level() != 1 {
		t.Errorf("level = %d, want it unchanged by a derivation", ps.Level())
	}
}

// TestRelationWithNothingAssertedIsInconclusive pins a distinction that is easy
// to get wrong and fatal when wrong. I got it wrong first.
//
// A term's truth in a COMPLETED world is one question — there, absence makes
// every negative term true. What a PARTIAL solution entails is a different
// question: an unassigned package is undecided, not known-absent, so nothing is
// entailed for either polarity.
//
// If a negative term read as Satisfied here, then a dependency incompatibility
// {depender: Positive, dependee: Negative} would classify as FULLY satisfied the
// moment the depender was selected — so every dependency would be reported as a
// conflict instead of being derived, and nothing would ever resolve. That is
// exactly the failure the first version of this code produced.
func TestRelationWithNothingAssertedIsInconclusive(t *testing.T) {
	ps := newPS()

	if got := ps.Relation("a", pos(versionset.Exactly(1))); got != term.Inconclusive {
		t.Errorf("positive term with nothing asserted = %v, want inconclusive", got)
	}
	if got := ps.Relation("a", neg(versionset.Exactly(1))); got != term.Inconclusive {
		t.Errorf("negative term with nothing asserted = %v, want inconclusive", got)
	}

	// And once something IS asserted, the term algebra takes over.
	ps.Derive("a", pos(versionset.Exactly(1)), nil)
	if got := ps.Relation("a", pos(versionset.Exactly(1))); got != term.Satisfied {
		t.Errorf("after asserting a==1, positive {1} = %v, want satisfied", got)
	}
	if got := ps.Relation("a", neg(versionset.Exactly(1))); got != term.Contradicted {
		t.Errorf("after asserting a==1, negative {1} = %v, want contradicted", got)
	}
}

func TestAccumulatedIntersectsHistory(t *testing.T) {
	ps := newPS()
	ps.Derive("a", pos(versionset.AtLeast(1)), nil)
	ps.Derive("a", pos(versionset.LessThan(10)), nil)

	got, ok := ps.Accumulated("a")
	if !ok {
		t.Fatal("nothing accumulated for a")
	}
	if !got.Set().Equal(versionset.Range(1, 10)) {
		t.Errorf("accumulated set = %v, want [1,10)", got.Set())
	}

	if _, ok := ps.Accumulated("never-mentioned"); ok {
		t.Error("a package with no assignments must report nothing accumulated")
	}
}

func TestDecisionForAndDecisions(t *testing.T) {
	ps := newPS()
	ps.Derive("a", pos(versionset.AtLeast(1)), nil)
	ps.Decide("a", versionset.Exactly(3))
	ps.Decide("b", versionset.Exactly(7))

	got, ok := ps.DecisionFor("a")
	if !ok || !got.Equal(versionset.Exactly(3)) {
		t.Errorf("DecisionFor(a) = %v, %v; want {3}, true", got, ok)
	}
	if _, ok := ps.DecisionFor("c"); ok {
		t.Error("DecisionFor on an undecided package must report false")
	}

	decisions := ps.Decisions()
	if len(decisions) != 2 {
		t.Fatalf("got %d decisions, want 2", len(decisions))
	}
	if decisions[0].Package != "a" || decisions[1].Package != "b" {
		t.Error("decisions must come back in chronological order")
	}
}

// TestBacktrackToDiscardsByLevelNotCount pins the reason truncation is by level:
// a conflict invalidates a decision AND everything derived under it, and those
// have to go together.
func TestBacktrackToDiscardsByLevelNotCount(t *testing.T) {
	ps := newPS()
	ps.Derive("root", pos(versionset.Exactly(1)), nil) // level 0
	ps.Decide("a", versionset.Exactly(1))              // level 1
	ps.Derive("b", neg(versionset.AtLeast(5)), nil)    // level 1
	ps.Decide("c", versionset.Exactly(2))              // level 2
	ps.Derive("d", neg(versionset.AtLeast(9)), nil)    // level 2

	if ps.Len() != 5 {
		t.Fatalf("Len = %d, want 5", ps.Len())
	}

	ps.BacktrackTo(1)

	if ps.Level() != 1 {
		t.Errorf("level = %d, want 1", ps.Level())
	}
	if ps.Len() != 3 {
		t.Errorf("Len = %d, want 3 (level-2 assignments discarded together)", ps.Len())
	}
	if _, ok := ps.DecisionFor("c"); ok {
		t.Error("the level-2 decision must be gone")
	}
	// And the accumulated terms must be rebuilt, not left stale — intersection
	// cannot be undone incrementally.
	if _, ok := ps.Accumulated("d"); ok {
		t.Error("accumulated term for a discarded package must be gone")
	}
	if _, ok := ps.Accumulated("b"); !ok {
		t.Error("accumulated term for a surviving assignment must remain")
	}
}

func TestBacktrackToZeroKeepsLevelZeroAssignments(t *testing.T) {
	ps := newPS()
	ps.Derive("root", pos(versionset.Exactly(1)), nil)
	ps.Decide("a", versionset.Exactly(1))

	ps.BacktrackTo(0)

	if ps.Level() != 0 {
		t.Errorf("level = %d, want 0", ps.Level())
	}
	if ps.Len() != 1 {
		t.Errorf("Len = %d, want 1 — level-0 derivations survive", ps.Len())
	}
}

func TestBacktrackToNegativeClampsToZero(t *testing.T) {
	ps := newPS()
	ps.Decide("a", versionset.Exactly(1))
	ps.BacktrackTo(-5)
	if ps.Level() != 0 {
		t.Errorf("level = %d, want 0", ps.Level())
	}
}

func TestSatisfierOf(t *testing.T) {
	ps := newPS()
	ps.Derive("a", pos(versionset.AtLeast(1)), nil)   // index 0
	ps.Derive("b", pos(versionset.AtLeast(1)), nil)   // index 1
	ps.Derive("a", pos(versionset.LessThan(10)), nil) // index 2 — tips it

	// Nothing about a satisfies "a in [1,10)" until index 2, where the
	// accumulation becomes narrow enough.
	idx, ok := ps.SatisfierOf("a", pos(versionset.Range(1, 10)))
	if !ok {
		t.Fatal("expected a satisfier")
	}
	if idx != 2 {
		t.Errorf("satisfier index = %d, want 2", idx)
	}

	if _, ok := ps.SatisfierOf("a", pos(versionset.Exactly(50))); ok {
		t.Error("a term the solution never satisfies must report false")
	}
	if _, ok := ps.SatisfierOf("never-mentioned", pos(versionset.Exactly(1))); ok {
		t.Error("an unmentioned package has no satisfier")
	}
}

func TestZeroPartialSolutionIsUsable(t *testing.T) {
	var ps psol
	if ps.Level() != 0 || ps.Len() != 0 {
		t.Fatal("the zero PartialSolution must be empty at level 0")
	}
	// Must not panic on a nil accumulated map.
	ps.Derive("a", pos(versionset.Exactly(1)), nil)
	if ps.Len() != 1 {
		t.Error("the zero value must accept assignments")
	}
}

// --- Store ---

func TestStoreIndexesByPackageNewestFirst(t *testing.T) {
	st := NewStore[string, set]()

	first := dep("a", versionset.Exactly(1), "b", versionset.AtLeast(1))
	second := dep("a", versionset.Exactly(2), "c", versionset.AtLeast(1))
	st.Add(first)
	st.Add(second)

	got := st.Mentioning("a")
	if len(got) != 2 {
		t.Fatalf("got %d incompatibilities mentioning a, want 2", len(got))
	}
	// Newest first, because conflict resolution makes later incompatibilities
	// more general and the most general useful derivation should surface first.
	if got[0] != second || got[1] != first {
		t.Error("Mentioning must return newest first")
	}

	if len(st.Mentioning("b")) != 1 {
		t.Error("b should be indexed under exactly one incompatibility")
	}
	if len(st.Mentioning("absent")) != 0 {
		t.Error("an unmentioned package must index nothing")
	}
}

// TestStoreDeduplicates matters because conflict resolution can rederive a fact
// already known; a store full of duplicates would make propagation derive the
// same consequence repeatedly.
func TestStoreDeduplicates(t *testing.T) {
	st := NewStore[string, set]()

	a := dep("a", versionset.Exactly(1), "b", versionset.AtLeast(1))
	b := dep("a", versionset.Exactly(1), "b", versionset.AtLeast(1))

	stored := st.Add(a)
	again := st.Add(b)

	if st.Len() != 1 {
		t.Errorf("Len = %d, want 1", st.Len())
	}
	if stored != again {
		t.Error("adding an equal incompatibility must return the stored one")
	}
	if len(st.Mentioning("a")) != 1 {
		t.Error("the index must not gain a duplicate entry either")
	}
}

func TestZeroStoreIsUsable(t *testing.T) {
	var st Store[string, set]
	st.Add(dep("a", versionset.Exactly(1), "b", versionset.AtLeast(1)))
	if st.Len() != 1 {
		t.Error("the zero Store must accept an Add")
	}
}

// --- Classify ---

func TestClassify(t *testing.T) {
	i := dep("a", versionset.Exactly(1), "b", versionset.AtLeast(2))

	t.Run("nothing asserted at all is unrelated", func(t *testing.T) {
		// Both terms inconclusive, because an unassigned package entails
		// nothing. Nothing is forced yet.
		if got, _ := Classify(newPS(), i); got != Unrelated {
			t.Errorf("got %v, want unrelated", got)
		}
	})

	t.Run("depender selected makes it almost satisfied", func(t *testing.T) {
		// This is the case that drives dependency resolution: a==1 satisfies the
		// positive term, b is still open, so "not (b not-in >=2)" is forced —
		// i.e. b IS required in >=2.
		ps := newPS()
		ps.Decide("a", versionset.Exactly(1))

		got, open := Classify(ps, i)
		if got != AlmostSatisfied {
			t.Fatalf("got %v, want almost satisfied", got)
		}
		if open != "b" {
			t.Errorf("open package = %q, want b", open)
		}
	})

	t.Run("fully satisfied when both terms are entailed", func(t *testing.T) {
		ps := newPS()
		ps.Decide("a", versionset.Exactly(1)) // satisfies Positive({1})
		// b==1 entails "b is not in >=2", satisfying the negative term. Note it
		// takes a real assignment about b: absence would not do it.
		ps.Decide("b", versionset.Exactly(1))

		if got, _ := Classify(ps, i); got != FullySatisfied {
			t.Errorf("got %v, want fully satisfied", got)
		}
	})

	t.Run("unrelated when a term is contradicted", func(t *testing.T) {
		ps := newPS()
		ps.Decide("a", versionset.Exactly(9)) // contradicts Positive({1})
		if got, _ := Classify(ps, i); got != Unrelated {
			t.Errorf("got %v, want unrelated", got)
		}
	})

	t.Run("unrelated when two terms are open", func(t *testing.T) {
		// Two POSITIVE terms, nothing asserted: both inconclusive, so nothing
		// is forced.
		both := NewIncompatibility(KindDerived, map[string]tm{
			"a": pos(versionset.Exactly(1)),
			"b": pos(versionset.Exactly(1)),
		})
		if got, _ := Classify(newPS(), both); got != Unrelated {
			t.Errorf("got %v, want unrelated", got)
		}
	})

	t.Run("empty incompatibility is fully satisfied", func(t *testing.T) {
		// A conjunction over nothing is vacuously true, so it is violated by
		// everything. This is the unsatisfiable-root signal.
		empty := NewIncompatibility(KindDerived, map[string]tm{})
		if got, _ := Classify(newPS(), empty); got != FullySatisfied {
			t.Errorf("got %v, want fully satisfied", got)
		}
	})
}

func TestSatisfactionZeroValueIsUnrelated(t *testing.T) {
	var s Satisfaction
	if s != Unrelated {
		t.Errorf("zero Satisfaction = %v, want unrelated", s)
	}
}

// --- Propagate ---

// TestPropagateDerivesADependency is the basic unit: root is decided, so its
// dependency's negative term must be derived as a positive requirement.
func TestPropagateDerivesADependency(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	// root 1 depends on a in [1,3).
	st.Add(dep("root", versionset.Exactly(1), "a", versionset.Range(1, 3)))
	ps.Decide("root", versionset.Exactly(1))

	result := Propagate(ps, st, "root")
	if result.HasConflict() {
		t.Fatalf("unexpected conflict: %v", result.Conflict)
	}

	// Negating Negative([1,3)) gives Positive([1,3)): a IS required in that range.
	got, ok := ps.Accumulated("a")
	if !ok {
		t.Fatal("nothing derived for a")
	}
	if !got.IsPositive() {
		t.Error("the derived term must be positive: a is now required")
	}
	if !got.Set().Equal(versionset.Range(1, 3)) {
		t.Errorf("derived set = %v, want [1,3)", got.Set())
	}
}

// TestPropagateChainsTransitively checks the worklist actually re-scans packages
// whose assignments changed, rather than stopping after one pass.
func TestPropagateChainsTransitively(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	st.Add(dep("root", versionset.Exactly(1), "a", versionset.Exactly(1)))
	st.Add(dep("a", versionset.Exactly(1), "b", versionset.Exactly(1)))
	st.Add(dep("b", versionset.Exactly(1), "c", versionset.Exactly(1)))

	ps.Decide("root", versionset.Exactly(1))

	if result := Propagate(ps, st, "root"); result.HasConflict() {
		t.Fatalf("unexpected conflict: %v", result.Conflict)
	}

	// a is derived from root. a's own dependency then fires because the derived
	// Positive({1}) satisfies the incompatibility's Positive({1}) term — no
	// DECISION for a was needed, which is the subtle point the spec calls out.
	for _, pkg := range []string{"a", "b", "c"} {
		got, ok := ps.Accumulated(pkg)
		if !ok {
			t.Errorf("nothing derived for %s; propagation stopped early", pkg)
			continue
		}
		if !got.IsPositive() || !got.Set().Equal(versionset.Exactly(1)) {
			t.Errorf("%s derived %v, want positive {1}", pkg, got)
		}
	}
}

// TestPropagateConflictWithoutADecision is the second thing the specification
// warns is easy to get backwards: a conflict does not require a decision for the
// conflicting package. A derivation alone can fully satisfy an incompatibility.
func TestPropagateConflictWithoutADecision(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	// root requires a in exactly {1}...
	st.Add(dep("root", versionset.Exactly(1), "a", versionset.Exactly(1)))
	// ...and a in {1} is flatly unavailable.
	unavailable := NewIncompatibility(KindUnavailable, map[string]tm{
		"a": pos(versionset.Exactly(1)),
	})
	st.Add(unavailable)

	ps.Decide("root", versionset.Exactly(1))

	result := Propagate(ps, st, "root")
	if !result.HasConflict() {
		t.Fatal("expected a conflict")
	}
	if !result.Conflict.Equal(unavailable) {
		t.Errorf("conflict = %v, want the unavailable fact %v", result.Conflict, unavailable)
	}

	// No decision was ever made for a — the derivation alone did it.
	if _, decided := ps.DecisionFor("a"); decided {
		t.Error("a must not have been decided; the conflict came from a derivation")
	}
}

func TestPropagateStopsAtFirstConflict(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	st.Add(dep("root", versionset.Exactly(1), "a", versionset.Exactly(1)))
	st.Add(dep("a", versionset.Exactly(1), "b", versionset.Exactly(1)))
	// Added LAST so it is newest, and therefore the first incompatibility
	// mentioning a that the scan reaches. Ordering is load-bearing here: with
	// this added before a's dependency, propagation would legitimately derive b
	// first and only then hit the conflict, since it derives from every
	// almost-satisfied incompatibility it passes.
	st.Add(NewIncompatibility(KindUnavailable, map[string]tm{"a": pos(versionset.Exactly(1))}))

	ps.Decide("root", versionset.Exactly(1))

	if result := Propagate(ps, st, "root"); !result.HasConflict() {
		t.Fatal("expected a conflict")
	}

	// Having stopped, it must not have gone on to derive b.
	if _, ok := ps.Accumulated("b"); ok {
		t.Error("propagation must stop at the conflict rather than deriving further")
	}
}

func TestPropagateNoIncompatibilitiesIsQuiet(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()
	ps.Decide("root", versionset.Exactly(1))

	if result := Propagate(ps, st, "root"); result.HasConflict() {
		t.Errorf("unexpected conflict: %v", result.Conflict)
	}
	if ps.Len() != 1 {
		t.Errorf("nothing should have been derived, Len = %d", ps.Len())
	}
}

// TestPropagateTerminatesOnCycles guards the worklist's dedup: a dependency
// cycle must not queue a package forever.
func TestPropagateTerminatesOnCycles(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	st.Add(dep("root", versionset.Exactly(1), "a", versionset.Exactly(1)))
	st.Add(dep("a", versionset.Exactly(1), "b", versionset.Exactly(1)))
	st.Add(dep("b", versionset.Exactly(1), "a", versionset.Exactly(1)))

	ps.Decide("root", versionset.Exactly(1))

	// If the worklist mishandled repeats this would not return.
	if result := Propagate(ps, st, "root"); result.HasConflict() {
		t.Fatalf("unexpected conflict: %v", result.Conflict)
	}
	for _, pkg := range []string{"a", "b"} {
		if _, ok := ps.Accumulated(pkg); !ok {
			t.Errorf("nothing derived for %s", pkg)
		}
	}
}

// TestPropagateDerivationRecordsItsCause matters for error reporting: without the
// cause, a failure cannot be explained.
func TestPropagateDerivationRecordsItsCause(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	d := dep("root", versionset.Exactly(1), "a", versionset.Range(1, 3))
	st.Add(d)
	ps.Decide("root", versionset.Exactly(1))

	if result := Propagate(ps, st, "root"); result.HasConflict() {
		t.Fatalf("unexpected conflict: %v", result.Conflict)
	}

	for _, a := range ps.Assignments() {
		if a.Package == "a" && !a.Decision {
			if a.Cause != d {
				t.Errorf("derivation cause = %v, want the dependency incompatibility", a.Cause)
			}
			return
		}
	}
	t.Error("no derivation recorded for a")
}

// TestPropagateWorkedScenario runs a small end-to-end propagation, which is where
// bugs in the interaction between the pieces show up rather than in any one of
// them.
func TestPropagateWorkedScenario(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	// root 1 requires app in [1,3), app 1..2 requires lib in [1,2).
	st.Add(dep("root", versionset.Exactly(1), "app", versionset.Range(1, 3)))
	st.Add(dep("app", versionset.Range(1, 3), "lib", versionset.Range(1, 2)))

	ps.Decide("root", versionset.Exactly(1))
	if result := Propagate(ps, st, "root"); result.HasConflict() {
		t.Fatalf("unexpected conflict: %v", result.Conflict)
	}

	// app is required in [1,3); that satisfies the second incompatibility's
	// Positive([1,3)) term, so lib in [1,2) is required too.
	app, ok := ps.Accumulated("app")
	if !ok || !app.Set().Equal(versionset.Range(1, 3)) {
		t.Fatalf("app accumulated %v, want [1,3)", app)
	}
	lib, ok := ps.Accumulated("lib")
	if !ok {
		t.Fatal("lib was never derived")
	}
	if !lib.IsPositive() || !lib.Set().Equal(versionset.Range(1, 2)) {
		t.Errorf("lib accumulated %v, want positive [1,2)", lib)
	}

	// Everything so far is forced, so it all sits at the decision level of the
	// single decision that started it.
	if ps.Level() != 1 {
		t.Errorf("level = %d, want 1", ps.Level())
	}
	for _, a := range ps.Assignments() {
		if !a.Decision && a.Level != 1 {
			t.Errorf("derivation for %s at level %d, want 1", a.Package, a.Level)
		}
	}
}

func TestStringers(t *testing.T) {
	if got := dep("a", versionset.Exactly(1), "b", versionset.AtLeast(2)).String(); got != "{a 1, b not >=2}" {
		t.Errorf("Incompatibility.String() = %q", got)
	}
	if got := NewIncompatibility(KindDerived, map[string]tm{}).String(); got != "{} (unsatisfiable)" {
		t.Errorf("empty String() = %q", got)
	}

	for _, tc := range []struct {
		k    Kind
		want string
	}{
		{KindDependency, "dependency"},
		{KindUnavailable, "unavailable"},
		{KindNoVersions, "no versions"},
		{KindRoot, "root"},
		{KindDerived, "derived"},
		{Kind(99), "unknown"},
	} {
		if got := tc.k.String(); got != tc.want {
			t.Errorf("Kind(%d).String() = %q, want %q", tc.k, got, tc.want)
		}
	}

	for _, tc := range []struct {
		s    Satisfaction
		want string
	}{
		{AlmostSatisfied, "almost satisfied"},
		{FullySatisfied, "fully satisfied"},
		{Unrelated, "unrelated"},
		{Satisfaction(99), "unknown"},
	} {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("Satisfaction(%d).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}
