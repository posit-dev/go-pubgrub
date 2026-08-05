// SPDX-License-Identifier: Apache-2.0 OR MIT

package versionset

import (
	"testing"
)

// The oracle: over a small universe of versions 0..universe-1, every subset is a
// bitmask, and set algebra is bitmask algebra. Exhaustively comparing Ints
// against that is a complete proof of correctness on this universe, which is far
// stronger than any hand-picked table — and set algebra bugs are exactly the kind
// that hide in an unconsidered case rather than an obvious one.
const universe = 6

// fromMask builds an Ints containing exactly the versions whose bit is set.
//
// Deliberately built one version at a time and unioned, so the constructor path
// under test is the same normalization the solver will exercise, and so touching
// intervals get merged rather than assembled pre-merged.
func fromMask(mask int) Ints {
	var s Ints
	for v := range universe {
		if mask&(1<<v) != 0 {
			s = s.Union(Exactly(int64(v)))
		}
	}
	return s
}

// toMask reads back which of the universe's versions a set contains.
func toMask(s Ints) int {
	mask := 0
	for v := range universe {
		if s.Contains(int64(v)) {
			mask |= 1 << v
		}
	}
	return mask
}

const fullMask = (1 << universe) - 1

func TestFromMaskRoundTrips(t *testing.T) {
	// If this fails, every other test in this file is meaningless.
	for mask := 0; mask <= fullMask; mask++ {
		if got := toMask(fromMask(mask)); got != mask {
			t.Fatalf("round trip of mask %06b gave %06b", mask, got)
		}
	}
}

func TestIntersectMatchesOracle(t *testing.T) {
	for a := 0; a <= fullMask; a++ {
		for b := 0; b <= fullMask; b++ {
			got := toMask(fromMask(a).Intersect(fromMask(b)))
			if want := a & b; got != want {
				t.Errorf("%06b ∩ %06b = %06b, want %06b", a, b, got, want)
			}
		}
	}
}

func TestUnionMatchesOracle(t *testing.T) {
	for a := 0; a <= fullMask; a++ {
		for b := 0; b <= fullMask; b++ {
			got := toMask(fromMask(a).Union(fromMask(b)))
			if want := a | b; got != want {
				t.Errorf("%06b ∪ %06b = %06b, want %06b", a, b, got, want)
			}
		}
	}
}

func TestComplementMatchesOracle(t *testing.T) {
	for a := 0; a <= fullMask; a++ {
		// Compared only within the universe: the true complement also contains
		// every representable version outside 0..universe-1.
		got := toMask(fromMask(a).Complement())
		if want := fullMask & ^a; got != want {
			t.Errorf("¬%06b = %06b within the universe, want %06b", a, got, want)
		}
	}
}

func TestDifferenceMatchesOracle(t *testing.T) {
	for a := 0; a <= fullMask; a++ {
		for b := 0; b <= fullMask; b++ {
			got := toMask(Difference(fromMask(a), fromMask(b)))
			if want := a & ^b; got != want {
				t.Errorf("%06b \\ %06b = %06b, want %06b", a, b, got, want)
			}
		}
	}
}

func TestIsEmptyMatchesOracle(t *testing.T) {
	for a := 0; a <= fullMask; a++ {
		if got, want := fromMask(a).IsEmpty(), a == 0; got != want {
			t.Errorf("IsEmpty(%06b) = %v, want %v", a, got, want)
		}
	}
}

// TestEqualIsLogicalNotRepresentational is the property the Set contract calls
// out as the most damaging to get wrong: two encodings of the same versions must
// compare equal, or the solver fails to recognize incompatibilities it has
// already derived and can loop or invent a conflict.
func TestEqualIsLogicalNotRepresentational(t *testing.T) {
	for a := 0; a <= fullMask; a++ {
		for b := 0; b <= fullMask; b++ {
			if got, want := fromMask(a).Equal(fromMask(b)), a == b; got != want {
				t.Errorf("Equal(%06b, %06b) = %v, want %v", a, b, got, want)
			}
		}
	}

	// The specific case that motivates merging touching intervals.
	if !Range(1, 2).Union(Range(2, 3)).Equal(Range(1, 3)) {
		t.Error("[1,2) ∪ [2,3) must equal [1,3); touching intervals have to merge")
	}
	// And built the other way round, to prove normalization is order-independent.
	if !Range(2, 3).Union(Range(1, 2)).Equal(Range(1, 3)) {
		t.Error("union must normalize regardless of operand order")
	}
	// Three ways to build the same singleton pair.
	viaExactly := Exactly(4).Union(Exactly(5))
	viaRange := Range(4, 6)
	if !viaExactly.Equal(viaRange) {
		t.Errorf("Exactly(4) ∪ Exactly(5) = %v, want it equal to %v", viaExactly, viaRange)
	}
}

func TestSubsetAndDisjointMatchOracle(t *testing.T) {
	for a := 0; a <= fullMask; a++ {
		for b := 0; b <= fullMask; b++ {
			sa, sb := fromMask(a), fromMask(b)

			if got, want := IsSubsetOf(sa, sb), a & ^b == 0; got != want {
				t.Errorf("IsSubsetOf(%06b, %06b) = %v, want %v", a, b, got, want)
			}
			if got, want := IsDisjointFrom(sa, sb), a&b == 0; got != want {
				t.Errorf("IsDisjointFrom(%06b, %06b) = %v, want %v", a, b, got, want)
			}
		}
	}
}

// TestRequiredLaws checks the identities the Set documentation declares
// mandatory. They are what the term algebra is derived from, so a violation here
// invalidates the layer above rather than merely being untidy.
func TestRequiredLaws(t *testing.T) {
	for a := 0; a <= fullMask; a++ {
		sa := fromMask(a)

		// Involution.
		if !sa.Complement().Complement().Equal(sa) {
			t.Errorf("double complement changed %06b", a)
		}

		for b := 0; b <= fullMask; b++ {
			sb := fromMask(b)

			// Commutativity.
			if !sa.Intersect(sb).Equal(sb.Intersect(sa)) {
				t.Errorf("intersect not commutative for %06b, %06b", a, b)
			}
			if !sa.Union(sb).Equal(sb.Union(sa)) {
				t.Errorf("union not commutative for %06b, %06b", a, b)
			}

			// De Morgan, both directions.
			if !sa.Union(sb).Complement().Equal(sa.Complement().Intersect(sb.Complement())) {
				t.Errorf("(a ∪ b)ᶜ ≠ aᶜ ∩ bᶜ for %06b, %06b", a, b)
			}
			if !sa.Intersect(sb).Complement().Equal(sa.Complement().Union(sb.Complement())) {
				t.Errorf("(a ∩ b)ᶜ ≠ aᶜ ∪ bᶜ for %06b, %06b", a, b)
			}
		}
	}

	// Associativity, over a sample of triples rather than the full cube, which
	// would be 262,144 iterations for no additional confidence.
	for a := 0; a <= fullMask; a += 3 {
		for b := 0; b <= fullMask; b += 5 {
			for c := 0; c <= fullMask; c += 7 {
				sa, sb, sc := fromMask(a), fromMask(b), fromMask(c)
				if !sa.Intersect(sb).Intersect(sc).Equal(sa.Intersect(sb.Intersect(sc))) {
					t.Errorf("intersect not associative for %06b, %06b, %06b", a, b, c)
				}
				if !sa.Union(sb).Union(sc).Equal(sa.Union(sb.Union(sc))) {
					t.Errorf("union not associative for %06b, %06b, %06b", a, b, c)
				}
			}
		}
	}
}

func TestConstructors(t *testing.T) {
	if !Empty().IsEmpty() {
		t.Error("Empty() is not empty")
	}
	if All().IsEmpty() {
		t.Error("All() must not be empty")
	}
	if !Empty().Complement().Equal(All()) {
		t.Error("¬∅ must be the universe")
	}
	if !All().Complement().IsEmpty() {
		t.Error("¬universe must be empty")
	}

	// Inverted and degenerate ranges are the empty set, not a panic and not an
	// inverted interval that would corrupt later algebra.
	for _, s := range []Ints{Range(5, 5), Range(9, 2), LessThan(minInt64)} {
		if !s.IsEmpty() {
			t.Errorf("%v should be empty", s)
		}
	}

	if !Exactly(7).Contains(7) || Exactly(7).Contains(6) || Exactly(7).Contains(8) {
		t.Error("Exactly(7) must contain exactly 7")
	}
	if !AtLeast(3).Contains(3) || AtLeast(3).Contains(2) {
		t.Error("AtLeast(3) is wrong at its boundary")
	}
	if !LessThan(3).Contains(2) || LessThan(3).Contains(3) {
		t.Error("LessThan(3) is wrong at its boundary")
	}
}

// TestUpperBoundIsNotAVersion pins that maxInt64 is the universe's exclusive
// upper bound rather than a member of it.
//
// This replaces a test that asserted the OPPOSITE and was wrong. It checked that
// Exactly(maxInt64) was non-empty and did not contain maxInt64-1 — both true of
// the buggy implementation — but never asked whether it contained maxInt64
// itself. It did not. So the set reported IsEmpty() == false while holding
// nothing, which violated the involution law this package declares mandatory.
//
// The lesson is the one that keeps recurring here: a test written from the same
// understanding as the implementation will agree with it. Assert the LAW, not the
// behavior you expect the code to have.
func TestUpperBoundIsNotAVersion(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  Ints
	}{
		{"Exactly(maxInt64)", Exactly(maxInt64)},
		{"AtLeast(maxInt64)", AtLeast(maxInt64)},
	} {
		if !tc.set.IsEmpty() {
			t.Errorf("%s: IsEmpty() = false, want true", tc.name)
		}
		if !tc.set.Equal(Empty()) {
			t.Errorf("%s: must equal the empty set", tc.name)
		}
		if tc.set.Contains(maxInt64) {
			t.Errorf("%s: must not contain maxInt64", tc.name)
		}
		// The law that the old test's subject broke.
		if !tc.set.Complement().Complement().Equal(tc.set) {
			t.Errorf("%s: involution violated", tc.name)
		}
	}

	// A set must never claim to be non-empty while containing nothing. That is
	// the precise shape of the defect, so state it directly.
	for _, v := range []int64{maxInt64, maxInt64 - 1, 0, minInt64} {
		s := Exactly(v)
		if !s.IsEmpty() && !s.Contains(v) {
			t.Errorf("Exactly(%d) reports non-empty but does not contain %d", v, v)
		}
	}

	// The version just below the bound is still representable.
	if !Exactly(maxInt64 - 1).Contains(maxInt64 - 1) {
		t.Error("maxInt64-1 must remain a representable version")
	}
}

func TestZeroValueIsEmptySet(t *testing.T) {
	var s Ints
	if !s.IsEmpty() {
		t.Error("the zero Ints must be the empty set")
	}
	if !s.Complement().Equal(All()) {
		t.Error("the zero value must complement to the universe")
	}
	// Operations on the zero value must not panic.
	if !s.Union(Exactly(1)).Equal(Exactly(1)) {
		t.Error("zero ∪ {1} should be {1}")
	}
	if !s.Intersect(All()).IsEmpty() {
		t.Error("zero ∩ universe should be empty")
	}
}

func TestString(t *testing.T) {
	for _, tc := range []struct {
		set  Ints
		want string
	}{
		{Empty(), "∅"},
		{All(), "*"},
		{Exactly(3), "3"},
		{Range(1, 5), "[1,5)"},
		{AtLeast(2), ">=2"},
		{LessThan(2), "<2"},
		{Exactly(1).Union(Exactly(5)), "1 ∪ 5"},
	} {
		if got := tc.set.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}

// TestNoAliasingBetweenOperands guards the immutability the Set contract
// requires: the solver retains sets inside incompatibilities for a whole solve,
// so an operation that mutated an operand's backing array would corrupt
// incompatibilities derived earlier.
func TestNoAliasingBetweenOperands(t *testing.T) {
	a := Exactly(1).Union(Exactly(3))
	b := Exactly(2)
	before := a.String()

	_ = a.Union(b)
	_ = a.Intersect(b)
	_ = a.Complement()
	_ = Difference(a, b)

	if after := a.String(); after != before {
		t.Errorf("operand mutated: was %s, now %s", before, after)
	}
}
