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

// pt pairs a package with a term, for the inputs a map cannot express.
func pt(pkg string, t tm) PackageTerm[string, set] {
	return PackageTerm[string, set]{Package: pkg, Term: t}
}

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

// TestIncompatibilityNormalizesDuplicatePackages pins §3's per-package
// normalization on the constructor that can actually express the input.
//
// A map cannot hold two terms for one package, so the intersection branch that
// used to live in NewIncompatibility was unreachable — and its doc comment
// promised a normalization that could not happen. §7.3's resolution step is where
// two terms about one package genuinely arise, so it needs a constructor whose
// signature can carry them, and this is the law that constructor upholds.
func TestIncompatibilityNormalizesDuplicatePackages(t *testing.T) {
	i := NewIncompatibilityFrom(KindDependency,
		pt("a", pos(versionset.AtLeast(1))),
		pt("a", pos(versionset.LessThan(5))),
		pt("b", neg(versionset.Exactly(9))),
	)

	if i.Len() != 2 {
		t.Fatalf("Len = %d, want 2 (the two terms about a must merge into one)", i.Len())
	}

	got, ok := i.Term("a")
	if !ok {
		t.Fatal("term for a missing")
	}
	if !got.IsPositive() || !got.Set().Equal(versionset.Range(1, 5)) {
		t.Errorf("merged term for a = %v, want positive [1,5)", got)
	}
}

// TestIncompatibilityFromMergesToAlwaysFalse pins the outcome §2.4 calls out:
// intersecting two disjoint positive constraints on one package yields
// Positive(∅), which makes the incompatibility inert rather than unsatisfiable.
// It is a legitimate result of merging, not an error, and the distinction matters
// because an inert incompatibility must never fire while an EMPTY one means the
// problem has no solution.
func TestIncompatibilityFromMergesToAlwaysFalse(t *testing.T) {
	i := NewIncompatibilityFrom(KindDerived,
		pt("a", pos(versionset.Range(1, 2))),
		pt("a", pos(versionset.Range(5, 6))),
	)

	if i.IsEmpty() {
		t.Error("merging to an always-false term must not empty the incompatibility: " +
			"an empty one means no solution exists, which this does not say")
	}
	if !i.IsInert() {
		t.Error("an incompatibility whose merged term is Positive(∅) is inert")
	}
}

// TestIncompatibilityFromTreatsAlwaysTrueAsTheIdentity pins what §3's two
// normalizations actually mean when they meet: an always-true term contributes
// nothing to a conjunction, so merging with one must leave the other term
// untouched, and one standing alone must disappear.
//
// This test started out asserting that the ORDER of the two normalizations was
// load-bearing — drop-then-merge would supposedly delete a real constraint. It is
// not: intersecting with Negative(∅) is the identity in every polarity
// combination, and merging can only produce an always-true term out of always-true
// inputs, so the two orders compute the same thing. Mutating the code to drop
// first left the test passing, which is how the false premise surfaced. What is
// pinned here is the identity, which is true and is what the constructor relies on.
func TestIncompatibilityFromTreatsAlwaysTrueAsTheIdentity(t *testing.T) {
	alwaysTrue := neg(versionset.Empty())

	for _, other := range []tm{
		pos(versionset.Exactly(3)),
		neg(versionset.AtLeast(3)),
		pos(versionset.Empty()), // always false: still the identity's argument
	} {
		merged := NewIncompatibilityFrom(KindDerived, pt("a", alwaysTrue), pt("a", other))
		reversed := NewIncompatibilityFrom(KindDerived, pt("a", other), pt("a", alwaysTrue))
		alone := NewIncompatibilityFrom(KindDerived, pt("a", other))

		if !merged.Equal(alone) {
			t.Errorf("merging %v with an always-true term gave %v, want %v: an always-true "+
				"term contributes nothing to a conjunction", other, merged, alone)
		}
		if !reversed.Equal(alone) {
			t.Errorf("the identity must hold in both orders, got %v for %v", reversed, other)
		}
	}

	// And an always-true term standing alone is dropped entirely.
	if !NewIncompatibilityFrom(KindDerived, pt("a", alwaysTrue)).IsEmpty() {
		t.Error("an incompatibility of nothing but always-true terms reduces to the empty one")
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
	derived := newDerived([]PackageTerm[string, set]{
		pt("a", pos(versionset.Exactly(1))),
		pt("b", neg(versionset.AtLeast(2))),
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

	d := newDerived([]PackageTerm[string, set]{pt("a", pos(versionset.Exactly(1)))}, a, b)
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

func TestFirstIndexSatisfying(t *testing.T) {
	ps := newPS()
	ps.Derive("a", pos(versionset.AtLeast(1)), nil)   // index 0
	ps.Derive("b", pos(versionset.AtLeast(1)), nil)   // index 1
	ps.Derive("a", pos(versionset.LessThan(10)), nil) // index 2 — tips it

	// Nothing about a satisfies "a in [1,10)" until index 2, where the
	// accumulation becomes narrow enough.
	idx, ok := ps.FirstIndexSatisfying("a", pos(versionset.Range(1, 10)))
	if !ok {
		t.Fatal("expected a satisfier")
	}
	if idx != 2 {
		t.Errorf("satisfier index = %d, want 2", idx)
	}

	if _, ok := ps.FirstIndexSatisfying("a", pos(versionset.Exactly(50))); ok {
		t.Error("a term the solution never satisfies must report false")
	}
	if _, ok := ps.FirstIndexSatisfying("never-mentioned", pos(versionset.Exactly(1))); ok {
		t.Error("an unmentioned package has no satisfier")
	}
}

// TestFirstIndexSatisfyingIgnoresOtherPackages pins the per-package filter, which
// nothing tested: deleting it — a plausible misreading of §7.2's "earliest
// assignment in the partial solution" — passed the whole suite.
//
// Terms about different packages constrain different packages. Intersecting them
// makes an assignment about b able to "satisfy" a term about a, which then points
// the satisfier at an assignment that has nothing to do with the conflict.
func TestFirstIndexSatisfyingIgnoresOtherPackages(t *testing.T) {
	ps := newPS()
	ps.Derive("b", pos(versionset.Exactly(5)), nil)

	if idx, ok := ps.FirstIndexSatisfying("a", pos(versionset.AtLeast(1))); ok {
		t.Errorf("index %d satisfies a term about a, but only b has been assigned; "+
			"assignments about other packages must not be folded in", idx)
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

// --- Regressions from the independent correctness review of 2026-08-04 ---
//
// Each of these asserts a LAW the type claims to uphold, not the behavior the
// implementation happened to have. Three separate defects in this repository have
// now been shipped with a passing test beside them, because a test written from
// the same understanding as the implementation agrees with it.

// TestInertIncompatibilityNeverFires pins §2.4's "will never fire and never
// needs to be checked again" for a term asking for a version from an empty
// range.
//
// Positive(∅) is false in every world, so §2.5's definition of contradiction
// yields Contradicted even with nothing asserted. Reading §2.5's *table* instead
// (which says a positive term is inconclusive when nothing is asserted, on the
// grounds that "a version could still be decided later that lands in r" —
// untrue when r is empty) makes the always-false term look like the single open
// term of an almost-satisfied incompatibility, and propagation then derives the
// vacuous "not ∅" for a package nothing has mentioned.
func TestInertIncompatibilityNeverFires(t *testing.T) {
	inert := NewIncompatibility(KindDerived, map[string]tm{
		"root": pos(versionset.Exactly(1)),
		"x":    pos(versionset.Empty()),
	})
	if !inert.IsInert() {
		t.Fatal("precondition: incompatibility with Positive(∅) should be inert")
	}

	ps := newPS()
	ps.Decide("root", versionset.Exactly(1))

	if got := ps.Relation("x", pos(versionset.Empty())); got != term.Contradicted {
		t.Errorf("Relation(unassigned, Positive(∅)) = %v, want Contradicted: an "+
			"always-false term is contradicted by every state, including the empty one", got)
	}

	st := NewStore[string, set]()
	st.Add(inert)
	before := ps.Len()
	Propagate(ps, st, "root")

	if ps.Len() != before {
		t.Errorf("Propagate added %d assignment(s) from an inert incompatibility; §2.4 says it never fires",
			ps.Len()-before)
	}
	if _, ok := ps.Accumulated("x"); ok {
		t.Error("Accumulated(\"x\") reports ok, but nothing has been asserted about x")
	}
}

// TestLevelAlwaysEqualsDecisionCount pins §1's definition of a decision level as
// "the number of decisions at or before that point", which §7.4's correctness
// argument rests on. BacktrackTo clamped the low side only, so a target above the
// current level raised the level with no decision behind it — permanently, and
// leaving the surviving assignments unreachable by any later BacktrackTo, since
// their levels all sit below the inflated one.
func TestLevelAlwaysEqualsDecisionCount(t *testing.T) {
	ps := newPS()
	ps.Decide("a", versionset.Exactly(1))

	ps.BacktrackTo(7) // Above the current level: nonsense, and must not be honored.
	if ps.Level() != len(ps.Decisions()) {
		t.Errorf("after BacktrackTo(7): Level() = %d, want %d (= number of decisions)",
			ps.Level(), len(ps.Decisions()))
	}

	ps.Decide("b", versionset.Exactly(1))
	if ps.Level() != len(ps.Decisions()) {
		t.Errorf("after a later Decide: Level() = %d, want %d — the inflation is permanent",
			ps.Level(), len(ps.Decisions()))
	}

	// Backtracking within range still truncates, so the clamp did not disable it.
	ps.BacktrackTo(1)
	if ps.Level() != 1 || len(ps.Decisions()) != 1 {
		t.Errorf("BacktrackTo(1): level=%d decisions=%d, want 1 and 1", ps.Level(), len(ps.Decisions()))
	}
}

// TestStoreDedupsEmptyIncompatibility pins Add's documented "the store holds no
// duplicates". Both of Add's loops are driven by the term map, so a zero-term
// incompatibility skipped dedup AND was indexed under no package — landing as an
// un-findable duplicate. It is the object whose appearance means "no solution
// exists", so being silently unreachable is the wrong property for it to have.
func TestStoreDedupsEmptyIncompatibility(t *testing.T) {
	st := NewStore[string, set]()
	first := NewIncompatibility(KindDerived, map[string]tm{})
	second := NewIncompatibility(KindDerived, map[string]tm{})

	if !first.IsEmpty() || !first.Equal(second) {
		t.Fatal("precondition: both should be empty and Equal")
	}

	if got := st.Add(first); got != first {
		t.Error("Add of the first empty incompatibility should store and return it")
	}
	if got := st.Add(second); got != first {
		t.Error("Add of an Equal empty incompatibility should return the stored one")
	}
	if st.Len() != 1 {
		t.Errorf("Len() = %d after adding two Equal empty incompatibilities, want 1", st.Len())
	}
}

// TestIsDerivedFollowsCausesNotKind pins the authority for §9's graph walk,
// which follows causes and reports nil-cause nodes as the external facts that
// forced the failure. KindDerived is the zero value — chosen so an unlabeled
// incompatibility would not masquerade as an authoritative external fact, which
// is right for the label and exactly inverted for the graph.
func TestIsDerivedFollowsCausesNotKind(t *testing.T) {
	labeled := NewIncompatibility(KindDerived, map[string]tm{"a": pos(versionset.Exactly(1))})
	if labeled.IsDerived() {
		t.Error("IsDerived() is true for an incompatibility with no causes; §9 would walk it as a leaf")
	}

	real := newDerived([]PackageTerm[string, set]{pt("a", pos(versionset.Exactly(1)))},
		dep("a", versionset.Exactly(1), "b", versionset.Exactly(1)),
		dep("b", versionset.Exactly(1), "c", versionset.Exactly(1)))
	if !real.IsDerived() {
		t.Error("IsDerived() is false for an incompatibility built from two causes")
	}
	if _, _, derived := real.Causes(); !derived {
		t.Error("Causes() and IsDerived() disagree")
	}
}

// --- §10's worked example, transcribed ---

// Versions from §10's universe, mapped onto the reference integer sets. Only the
// ordering matters, so 1.0.0 -> 100 and 2.5.0 -> 250 keeps the arithmetic legible
// against the spec's own tables.
const (
	v100 = 100 // 1.0.0
	v200 = 200 // 2.0.0
	v250 = 250 // 2.5.0
)

// TestSection10Trace replays §10's hand-traced example against the code.
//
// This exists because the correctness review of 2026-08-04 found that §10's trace
// would have caught three of its findings unaided, and no test transcribed it. Every
// other test in this file was written from the same reading of the spec as the
// implementation, which is exactly the condition under which a test agrees with a
// bug. §10 is the one artifact in the project that states, independently and
// concretely, what the answers are supposed to be.
//
// # What it covers
//
// Steps 1-7 are propagation and classification. Steps 8-10 are conflict
// resolution: the satisfier arithmetic, the round of folding in a prior cause,
// the exact derived incompatibility I4, and the backjump — asserted through
// Resolve rather than reconstructed by hand. TestSolveSection10 then runs the same
// universe end to end through the main loop and checks it reaches §10's final
// answer.
//
// # ⚠️ Absolute decision levels are NOT asserted here, on purpose
//
// §10 numbers the root decision level 0 (per §1's parenthetical exemption) and this
// implementation numbers it 1, upholding §1's main sentence instead. Asserting
// absolute levels here would pin the scheme rather than the logic, so this asserts
// the scheme-INDEPENDENT relations: which assignments share a level, that a
// decision is exactly one level above what precedes it, and — for the backjump —
// which assignments survive it. All hold under either numbering. See Level and
// baseLevel for the scheme and the floor that has to match it.
func TestSection10Trace(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	// The universe. app 1.0.0 (root) needs http >=1.0.0; http 2.0.0 needs
	// json [1.0.0,2.0.0); json 1.0.0 needs http >=2.5.0, which nothing satisfies.
	i1 := dep("app", versionset.Exactly(v100), "http", versionset.AtLeast(v100))
	i2 := dep("http", versionset.AtLeast(v200), "json", versionset.Range(v100, v200))

	// I3's json side is POSITIVE: json is the DEPENDER here, and §3 encodes a
	// dependency as {depender: Positive, dependee: Negative}. §10 calls this out
	// explicitly because getting the polarity backwards is the easy mistake.
	i3 := NewIncompatibility(KindDependency, map[string]tm{
		"json": pos(versionset.LessThan(v200)),
		"http": neg(versionset.AtLeast(v250)),
	})

	// --- Steps 1-3: decide the root, derive http >=1.0.0 from I1 ---

	st.Add(i1)
	ps.Decide("app", versionset.Exactly(v100))

	if got, open := Classify(ps, i1); got != AlmostSatisfied || open != "http" {
		t.Fatalf("step 3: Classify(I1) = %v open=%q, want almost satisfied open=\"http\" "+
			"(app-term satisfied by the decision, http-term inconclusive)", got, open)
	}

	if result := Propagate(ps, st, "app"); result.HasConflict() {
		t.Fatalf("step 3: unexpected conflict %v", result.Conflict)
	}

	// D1: "http: [1.0.0,∞)". §10 records the derivation as POSITIVE — it is the
	// negation of I1's negative http-term, not a copy of it.
	d1 := indexOfAssignment(t, ps, "http")
	if got := ps.Assignments()[d1]; got.Decision {
		t.Error("step 3: D1 must be a derivation, not a decision")
	} else if want := pos(versionset.AtLeast(v100)); !got.Term.Equal(want) {
		t.Errorf("step 3: D1 = %v, want %v", got.Term, want)
	}
	if ps.Assignments()[d1].Level != ps.Assignments()[0].Level {
		t.Error("step 3: D1 is a consequence of the root decision, so it belongs to that " +
			"decision's level — a derivation never raises the level")
	}

	// --- Steps 4-5: http 2.0.0 is a SAFE decision, and that is the subtle part ---

	st.Add(i2)

	// §10: "Committing http 2.0.0 would not yet make I2 fully satisfied (its json
	// term is still inconclusive), so the decision is safe and is recorded." This is
	// the §8 pre-commit check passing. It is asserted before the decision, because
	// after the decision the answer changes and the check becomes untestable.
	probe := newPS()
	for _, a := range ps.Assignments() {
		if a.Decision {
			probe.Decide(a.Package, a.Term.Set())
		} else {
			probe.Derive(a.Package, a.Term, a.Cause)
		}
	}
	probe.Decide("http", versionset.Exactly(v200))
	if got, _ := Classify(probe, i2); got == FullySatisfied {
		t.Error("step 5: committing http 2.0.0 must NOT make I2 fully satisfied — its " +
			"json-term is inconclusive, which is what makes the decision safe")
	}

	levelBefore := ps.Level()
	ps.Decide("http", versionset.Exactly(v200))
	if ps.Level() != levelBefore+1 {
		t.Errorf("step 5: a decision must raise the level by exactly 1, got %d -> %d",
			levelBefore, ps.Level())
	}

	// --- Step 6: derive json [1.0.0,2.0.0) from I2 ---

	if got, open := Classify(ps, i2); got != AlmostSatisfied || open != "json" {
		t.Fatalf("step 6: Classify(I2) = %v open=%q, want almost satisfied open=\"json\"", got, open)
	}
	if result := Propagate(ps, st, "http"); result.HasConflict() {
		t.Fatalf("step 6: unexpected conflict %v", result.Conflict)
	}

	d2 := indexOfAssignment(t, ps, "json")
	if want := pos(versionset.Range(v100, v200)); !ps.Assignments()[d2].Term.Equal(want) {
		t.Errorf("step 6: D2 = %v, want %v", ps.Assignments()[d2].Term, want)
	}

	httpDecision := indexOfDecision(t, ps, "http")
	if ps.Assignments()[d2].Level != ps.Assignments()[httpDecision].Level {
		t.Error("step 6: D2 is forced by the http decision, so it shares that decision's level")
	}

	// --- Step 7: I3 is ALREADY fully satisfied, before json is ever decided ---
	//
	// This is the subtlety §6 and §8 flag, and the reason the example was
	// constructed: the conflict is discovered without any decision ever being
	// recorded for the conflicting package. D2 alone satisfies I3's json-term
	// ([1.0.0,2.0.0) ⊆ (-∞,2.0.0)) and the existing http 2.0.0 decision satisfies its
	// http-term (2.0.0 ∉ [2.5.0,∞)).

	if got, _ := Classify(ps, i3); got != FullySatisfied {
		t.Fatalf("step 7: Classify(I3) = %v, want fully satisfied — D2 and the http "+
			"decision satisfy both terms with no json decision ever recorded", got)
	}
	if _, decided := ps.DecisionFor("json"); decided {
		t.Error("step 7: json must never have been decided; the conflict precedes any json decision")
	}

	// Propagation must REPORT it rather than resolve it: §7 is the caller's job.
	st.Add(i3)
	result := Propagate(ps, st, "json")
	if !result.HasConflict() {
		t.Fatal("step 7: propagation must stop and report I3 as a conflict")
	}
	if !result.Conflict.Equal(i3) {
		t.Errorf("step 7: reported conflict = %v, want I3 = %v", result.Conflict, i3)
	}

	// --- Conflict resolution: the satisfier is the MAXIMUM over I3's terms ---
	//
	// §10: "D2 (step 6) completes the json-term on its own; the http-term was
	// already complete earlier, at decision 5. The later of the two,
	// chronologically, is D2 — so satisfier = D2."
	//
	// The per-term satisfiers are checked individually first, because that is where
	// the arithmetic can go wrong quietly: taking whichever term Go's map iteration
	// yields first would pick the http DECISION here, §7.4's escape would fire, and
	// the round of resolution that produces the generalization would be skipped.

	for pkg, want := range map[string]int{"json": d2, "http": httpDecision} {
		term, _ := i3.Term(pkg)
		idx, ok := ps.FirstIndexSatisfying(pkg, term)
		if !ok {
			t.Fatalf("FirstIndexSatisfying(%q, %v) found none, but Classify says I3 is fully satisfied", pkg, term)
		}
		if idx != want {
			t.Errorf("per-term satisfier of I3's %s-term = %d, want %d", pkg, idx, want)
		}
	}

	satisfier, satisfierPkg, ok := ps.SatisfierOf(i3)
	if !ok {
		t.Fatal("SatisfierOf(I3) found none, but Classify says I3 is fully satisfied")
	}
	if satisfier != d2 || satisfierPkg != "json" {
		t.Errorf("§7.2's satisfier of I3 = index %d about %q, want %d about \"json\" (D2) — "+
			"§10 requires the LATER of the two per-term satisfiers", satisfier, satisfierPkg, d2)
	}
	if ps.Assignments()[satisfier].Decision {
		t.Error("§7.2's satisfier of I3 must be a DERIVATION (D2), not a decision — if it " +
			"reads as a decision, §7.4's escape fires and the required round of resolution " +
			"never happens, silently producing a weaker solver")
	}

	// §10: "Previous satisfier: the earliest assignment before D2 such that
	// (prefix + D2) satisfies I3 — that's exactly decision 5 (http 2.0.0), which
	// supplied the other term." Note it is not about the satisfier's own package.
	previous, hasPrevious := ps.PreviousSatisfierOf(i3, satisfier)
	if !hasPrevious {
		t.Fatal("I3's satisfier needs the http decision to supply the other term, so a " +
			"previous satisfier exists")
	}
	if previous != httpDecision {
		t.Errorf("§7.2's previous satisfier of I3 = index %d, want %d (the http 2.0.0 decision)",
			previous, httpDecision)
	}

	// §10: previousSatisfierLevel == satisfier's level, so neither §7.4 escape holds
	// and a prior cause must be folded in.
	if ps.Assignments()[previous].Level != ps.Assignments()[satisfier].Level {
		t.Error("conflict resolution: D2 and the previous satisfier (the http decision) must " +
			"be at the SAME level, which is what forces a round of folding in a prior cause")
	}

	// --- Step 8: the round of resolution derives I4 = {http: [2.0.0,2.5.0)} ---
	//
	// §10 works this out term by term: dropping the json-term from both I3 and I2
	// leaves http ¬[2.5.0,∞) and http [2.0.0,∞); D2 satisfied its term by itself so
	// §7.3 step 3 is skipped; and intersecting the two http terms gives
	// Positive([2.0.0,2.5.0)). Both parents speak about http, which is exactly the
	// per-package merge a map-shaped constructor cannot express.

	incTerm, _ := i3.Term(satisfierPkg)
	i4 := priorCause(i3, i2, satisfierPkg, ps.Assignments()[satisfier].Term, incTerm)

	want := NewIncompatibility(KindDerived, map[string]tm{"http": pos(versionset.Range(v200, v250))})
	if !i4.Equal(want) {
		t.Errorf("step 8: prior cause = %v, want %v", i4, want)
	}
	if _, ok := i4.Term("json"); ok {
		t.Error("step 8: the satisfier's own package must be dropped from both sides")
	}
	if a, b, derived := i4.Causes(); !derived || a != i3 || b != i2 {
		t.Error("step 8: the derived incompatibility must record I3 and I2 as its causes, " +
			"or §9 cannot walk back to the facts that forced the failure")
	}

	// --- Steps 8-9: Resolve does the whole thing, and backjumps past a decision ---

	appDecision := indexOfDecision(t, ps, "app")
	survivors := ps.Len() - 2 // everything except the http decision and D2

	resolution, err := Resolve(ps, st, "app", i3)
	if err != nil {
		t.Fatalf("Resolve returned an error: %v", err)
	}
	if resolution.Unsolvable {
		t.Fatal("§10's conflict is resolvable: it ends in a solution, not a proof of failure")
	}
	if !resolution.Incompatibility.Equal(want) {
		t.Errorf("Resolve returned %v, want I4 = %v", resolution.Incompatibility, want)
	}
	if resolution.Package != "http" {
		t.Errorf("Resolve says to resume on %q, want \"http\" — I4's only term", resolution.Package)
	}

	// §10: "Truncate the partial solution to remove everything above level 0 — this
	// discards decision 5 (http 2.0.0) and derivation D2 (json) entirely." Asserted
	// as which assignments survive, which is true under either numbering scheme.
	if ps.Len() != survivors {
		t.Errorf("after the backjump: %d assignments, want %d (the http decision and D2 discarded)",
			ps.Len(), survivors)
	}
	if _, decided := ps.DecisionFor("http"); decided {
		t.Error("the backjump must discard the http 2.0.0 decision: it is the guess that failed")
	}
	if _, ok := ps.Accumulated("json"); ok {
		t.Error("the backjump must discard D2, which was derived under the discarded decision")
	}
	if ps.Assignments()[appDecision].Package != "app" || !ps.Assignments()[appDecision].Decision {
		t.Error("the backjump must NOT discard the root decision: backtracking below the root's " +
			"level throws away work that has to be immediately re-derived")
	}
	if _, ok := ps.Accumulated("http"); !ok {
		t.Error("D1 (http >=1.0.0) sits at the root decision's level and must survive")
	}

	// §7.1's guarantee 1, which is the whole point of cutting at a level boundary:
	// what comes back is ALMOST satisfied, so propagation has exactly one thing to
	// derive. Still fully satisfied would mean the same conflict immediately again.
	if satisfaction, open := Classify(ps, resolution.Incompatibility); satisfaction != AlmostSatisfied || open != "http" {
		t.Errorf("after the backjump, I4 classifies as %v (open %q), want almost satisfied about "+
			"http — §7.1's first guarantee", satisfaction, open)
	}

	// §7.4 adds the replacement to the known set, since it is not the original
	// input. Without that, the generalization is derived and thrown away.
	if len(st.Mentioning("http")) < 4 {
		t.Error("I4 must be added to the store: it is the generalization the round of " +
			"resolution existed to produce")
	}
	found := false
	for _, inc := range st.All() {
		if inc.Equal(want) {
			found = true
		}
	}
	if !found {
		t.Error("the store does not contain I4")
	}
}

// indexOfAssignment returns the index of the single non-decision assignment about
// pkg, failing if there is not exactly one.
func indexOfAssignment(t *testing.T, ps *psol, pkg string) int {
	t.Helper()
	found := -1
	for i, a := range ps.Assignments() {
		if a.Package == pkg && !a.Decision {
			if found >= 0 {
				t.Fatalf("expected exactly one derivation about %q, found several", pkg)
			}
			found = i
		}
	}
	if found < 0 {
		t.Fatalf("no derivation about %q", pkg)
	}
	return found
}

// indexOfDecision returns the index of pkg's decision.
func indexOfDecision(t *testing.T, ps *psol, pkg string) int {
	t.Helper()
	for i, a := range ps.Assignments() {
		if a.Package == pkg && a.Decision {
			return i
		}
	}
	t.Fatalf("no decision about %q", pkg)
	return -1
}
