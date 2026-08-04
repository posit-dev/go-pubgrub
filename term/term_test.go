// SPDX-License-Identifier: Apache-2.0 OR MIT

package term

import (
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// The oracle for terms is a truth vector over every possible world for one
// package. With a universe of versions 0..universe-1 there are universe+1
// worlds: the package is absent, or exactly one version is selected.
//
// Evaluating a term in every world reduces the algebra to boolean vector
// operations, which is a complete proof over this universe. It also forces the
// absence world to be modelled explicitly — and absence is precisely where a
// negative term differs from a positive term over the complement set, the one
// asymmetry the specification warns is easy to get wrong and damaging when wrong.
const universe = 5

// worldAbsent is the world in which no version of the package is selected. The
// remaining worlds are indexed 1..universe, where world v+1 means "version v is
// selected".
const worldAbsent = 0

const numWorlds = universe + 1

// truth is a term's truth value in each world.
type truth [numWorlds]bool

// evaluate computes the oracle truth vector for a term, from the definition in
// the specification rather than from the implementation.
func evaluate(positive bool, mask int) truth {
	var out truth

	// Absence: every negative term is true, every positive term is false.
	out[worldAbsent] = !positive

	for v := range universe {
		inSet := mask&(1<<v) != 0
		if positive {
			out[v+1] = inSet
		} else {
			out[v+1] = !inSet
		}
	}
	return out
}

func fromMask(mask int) versionset.Ints {
	var s versionset.Ints
	for v := range universe {
		if mask&(1<<v) != 0 {
			s = s.Union(versionset.Exactly(int64(v)))
		}
	}
	return s
}

// actual computes a term's truth vector by asking the implementation, using
// Relation against a probe term that pins a single world.
//
// This is what connects the oracle to the code: for world "version v selected",
// the probe is Positive({v}); a term is true in that world exactly when the
// probe satisfies it. For the absence world there is no version to probe with,
// so it is read from the term's polarity, which is the definition.
func actual(tm Term[versionset.Ints]) truth {
	var out truth
	out[worldAbsent] = !tm.IsPositive()

	for v := range universe {
		probe := Positive(versionset.Exactly(int64(v)))
		out[v+1] = probe.Satisfies(tm)
	}
	return out
}

const fullMask = (1 << universe) - 1

func build(positive bool, mask int) Term[versionset.Ints] {
	if positive {
		return Positive(fromMask(mask))
	}
	return Negative(fromMask(mask))
}

// TestEvaluationMatchesOracle establishes that the implementation and the oracle
// agree on what every term means, before any algebra is tested against it.
func TestEvaluationMatchesOracle(t *testing.T) {
	for _, positive := range []bool{true, false} {
		for mask := 0; mask <= fullMask; mask++ {
			want := evaluate(positive, mask)
			got := actual(build(positive, mask))
			if got != want {
				t.Errorf("term(positive=%v, mask=%05b): truth %v, want %v", positive, mask, got, want)
			}
		}
	}
}

// TestNegativeIsNotPositiveComplement is the asymmetry, stated as a test.
//
// Negative(s) and Positive(sᶜ) agree in every world where a version is selected,
// and differ in the absence world. Conflating them would make "must not be
// version 2" mean "must be some version other than 2" — the solver would demand
// packages nobody asked for.
func TestNegativeIsNotPositiveComplement(t *testing.T) {
	for mask := 0; mask <= fullMask; mask++ {
		s := fromMask(mask)
		neg := Negative(s)
		posComplement := Positive(s.Complement())

		negTruth := actual(neg)
		posTruth := actual(posComplement)

		for v := range universe {
			if negTruth[v+1] != posTruth[v+1] {
				t.Errorf("mask %05b: differ on selected version %d, which they must not", mask, v)
			}
		}
		if negTruth[worldAbsent] == posTruth[worldAbsent] {
			t.Errorf("mask %05b: must differ on absence, but both are %v",
				mask, negTruth[worldAbsent])
		}
		if !negTruth[worldAbsent] {
			t.Errorf("mask %05b: a negative term must be TRUE when the package is absent", mask)
		}
		if posTruth[worldAbsent] {
			t.Errorf("mask %05b: a positive term must be FALSE when the package is absent", mask)
		}
	}
}

func TestNegateMatchesOracle(t *testing.T) {
	for _, positive := range []bool{true, false} {
		for mask := 0; mask <= fullMask; mask++ {
			got := actual(build(positive, mask).Negate())

			var want truth
			base := evaluate(positive, mask)
			for i := range base {
				want[i] = !base[i]
			}

			if got != want {
				t.Errorf("negate(positive=%v, mask=%05b): %v, want %v", positive, mask, got, want)
			}
		}
	}
}

func TestIntersectMatchesOracle(t *testing.T) {
	forEachPair(t, func(t *testing.T, aPos bool, aMask int, bPos bool, bMask int) {
		got := actual(build(aPos, aMask).Intersect(build(bPos, bMask)))

		av, bv := evaluate(aPos, aMask), evaluate(bPos, bMask)
		var want truth
		for i := range want {
			want[i] = av[i] && bv[i]
		}

		if got != want {
			t.Errorf("(%v,%05b) ∧ (%v,%05b): %v, want %v", aPos, aMask, bPos, bMask, got, want)
		}
	})
}

func TestUnionMatchesOracle(t *testing.T) {
	forEachPair(t, func(t *testing.T, aPos bool, aMask int, bPos bool, bMask int) {
		got := actual(build(aPos, aMask).Union(build(bPos, bMask)))

		av, bv := evaluate(aPos, aMask), evaluate(bPos, bMask)
		var want truth
		for i := range want {
			want[i] = av[i] || bv[i]
		}

		if got != want {
			t.Errorf("(%v,%05b) ∨ (%v,%05b): %v, want %v", aPos, aMask, bPos, bMask, got, want)
		}
	})
}

// TestRelationMatchesOracle is the most important test here: Relation is what
// unit propagation and conflict resolution are built on.
//
// Oracle definition, directly from the meaning of the words:
//
//	satisfies   iff in every world where a is true, b is true
//	contradicts iff in every world where a is true, b is false
//
// When a is true in NO world (the always-false term), both conditions hold
// vacuously. The implementation checks satisfaction first, so it reports
// Satisfied; that is logically sound — a contradiction entails everything — and
// the oracle mirrors that precedence deliberately rather than by accident.
func TestRelationMatchesOracle(t *testing.T) {
	forEachPair(t, func(t *testing.T, aPos bool, aMask int, bPos bool, bMask int) {
		av, bv := evaluate(aPos, aMask), evaluate(bPos, bMask)

		allImplyTrue, allImplyFalse := true, true
		for i := range av {
			if !av[i] {
				continue
			}
			if !bv[i] {
				allImplyTrue = false
			}
			if bv[i] {
				allImplyFalse = false
			}
		}

		want := Inconclusive
		switch {
		case allImplyTrue:
			want = Satisfied
		case allImplyFalse:
			want = Contradicted
		}

		got := build(aPos, aMask).Relation(build(bPos, bMask))
		if got != want {
			t.Errorf("(%v,%05b).Relation(%v,%05b) = %v, want %v",
				aPos, aMask, bPos, bMask, got, want)
		}

		// The convenience wrappers must not drift from Relation.
		a, b := build(aPos, aMask), build(bPos, bMask)
		if a.Satisfies(b) != (got == Satisfied) {
			t.Error("Satisfies disagrees with Relation")
		}
		if a.Contradicts(b) != (got == Contradicted) {
			t.Error("Contradicts disagrees with Relation")
		}
	})
}

// TestAlwaysFalseEntailsEverything pins the vacuous case explicitly, since it is
// a deliberate precedence choice rather than something the oracle discovered.
func TestAlwaysFalseEntailsEverything(t *testing.T) {
	alwaysFalse := Positive(versionset.Empty())
	if !alwaysFalse.IsAlwaysFalse() {
		t.Fatal("Positive(∅) must report IsAlwaysFalse")
	}

	for _, positive := range []bool{true, false} {
		for mask := 0; mask <= fullMask; mask++ {
			if got := alwaysFalse.Relation(build(positive, mask)); got != Satisfied {
				t.Errorf("an always-false term should vacuously satisfy everything, got %v", got)
			}
		}
	}
}

func TestDegenerateTerms(t *testing.T) {
	alwaysFalse := Positive(versionset.Empty())
	alwaysTrue := Negative(versionset.Empty())

	if !alwaysFalse.IsAlwaysFalse() || alwaysFalse.IsAlwaysTrue() {
		t.Error("Positive(∅) must be always-false and not always-true")
	}
	if !alwaysTrue.IsAlwaysTrue() || alwaysTrue.IsAlwaysFalse() {
		t.Error("Negative(∅) must be always-true and not always-false")
	}

	// Verified against the oracle rather than asserted.
	if got := actual(alwaysTrue); got != (truth{true, true, true, true, true, true}) {
		t.Errorf("Negative(∅) truth = %v, want all true", got)
	}
	if got := actual(alwaysFalse); got != (truth{false, false, false, false, false, false}) {
		t.Errorf("Positive(∅) truth = %v, want all false", got)
	}

	// A non-empty set must not be mistaken for degenerate.
	if Positive(versionset.Exactly(1)).IsAlwaysFalse() {
		t.Error("Positive({1}) is not always false")
	}
	if Negative(versionset.Exactly(1)).IsAlwaysTrue() {
		t.Error("Negative({1}) is not always true")
	}
}

// TestNegateKeepsTheSet guards the implementation detail the specification is
// emphatic about: negation flips polarity and must NOT complement the set.
func TestNegateKeepsTheSet(t *testing.T) {
	s := versionset.Range(1, 4)
	tm := Positive(s)

	neg := tm.Negate()
	if neg.IsPositive() {
		t.Error("negation must flip polarity")
	}
	if !neg.Set().Equal(s) {
		t.Errorf("negation changed the set to %v; it must stay %v", neg.Set(), s)
	}
	if !neg.Negate().Equal(tm) {
		t.Error("negation must be an involution")
	}
}

func TestEqual(t *testing.T) {
	a := Positive(versionset.Range(1, 3))
	if !a.Equal(Positive(versionset.Exactly(1).Union(versionset.Exactly(2)))) {
		t.Error("Equal must follow the set's logical equality, not its representation")
	}
	if a.Equal(Negative(versionset.Range(1, 3))) {
		t.Error("terms of differing polarity must not be equal")
	}
	if a.Equal(Positive(versionset.Range(1, 4))) {
		t.Error("terms over differing sets must not be equal")
	}
}

func TestZeroTermIsAlwaysFalse(t *testing.T) {
	// Documented as a safe zero value: it asserts nothing satisfies it, rather
	// than silently asserting everything does.
	var zero Term[versionset.Ints]
	if !zero.IsAlwaysFalse() {
		t.Error("the zero Term must be always-false")
	}
	if zero.IsAlwaysTrue() {
		t.Error("the zero Term must not be always-true")
	}
}

func TestRelationZeroValueIsInconclusive(t *testing.T) {
	// A Relation that was never computed must read as the answer that asserts
	// least.
	var r Relation
	if r != Inconclusive {
		t.Errorf("zero Relation = %v, want Inconclusive", r)
	}
}

func TestStringRendersPolarity(t *testing.T) {
	if got := Positive(versionset.Exactly(2)).String(); got != "2" {
		t.Errorf("positive String() = %q, want %q", got, "2")
	}
	if got := Negative(versionset.Exactly(2)).String(); got != "not 2" {
		t.Errorf("negative String() = %q, want %q", got, "not 2")
	}
}

func TestRelationString(t *testing.T) {
	for _, tc := range []struct {
		r    Relation
		want string
	}{
		{Satisfied, "satisfied"},
		{Contradicted, "contradicted"},
		{Inconclusive, "inconclusive"},
		{Relation(99), "unknown"},
	} {
		if got := tc.r.String(); got != tc.want {
			t.Errorf("Relation(%d).String() = %q, want %q", tc.r, got, tc.want)
		}
	}
}

// forEachPair runs fn over every ordered pair of terms in the universe: two
// polarities and 2^universe sets on each side.
func forEachPair(t *testing.T, fn func(t *testing.T, aPos bool, aMask int, bPos bool, bMask int)) {
	t.Helper()

	for _, aPos := range []bool{true, false} {
		for aMask := 0; aMask <= fullMask; aMask++ {
			for _, bPos := range []bool{true, false} {
				for bMask := 0; bMask <= fullMask; bMask++ {
					fn(t, aPos, aMask, bPos, bMask)
				}
			}
		}
	}
}
