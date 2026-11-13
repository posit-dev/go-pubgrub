// SPDX-License-Identifier: Apache-2.0 OR MIT

package term

import (
	"math/rand"
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// oldRelation is Relation as it was before Excludes: build both intersections.
func oldRelation[S versionset.Set[S]](t, other Term[S]) Relation {
	switch {
	case t.Intersect(other.Negate()).IsAlwaysFalse():
		return Satisfied
	case t.Intersect(other).IsAlwaysFalse():
		return Contradicted
	default:
		return Inconclusive
	}
}

// TestExcludesMatchesIntersect is exhaustive over every pair of terms on a
// five-version universe, both polarities: 64 x 64 pairs.
func TestExcludesMatchesIntersect(t *testing.T) {
	pairs := 0
	seen := map[Relation]int{}
	forEachPair(t, func(t *testing.T, aPos bool, aMask int, bPos bool, bMask int) {
		a, b := build(aPos, aMask), build(bPos, bMask)
		pairs++
		if got, want := a.Excludes(b), a.Intersect(b).IsAlwaysFalse(); got != want {
			t.Errorf("(%v,%05b).Excludes(%v,%05b) = %v, Intersect says %v", aPos, aMask, bPos, bMask, got, want)
		}
		got, want := a.Relation(b), oldRelation(a, b)
		if got != want {
			t.Errorf("(%v,%05b).Relation(%v,%05b) = %v, old code gave %v", aPos, aMask, bPos, bMask, got, want)
		}
		if a.Satisfies(b) != (want == Satisfied) || a.Contradicts(b) != (want == Contradicted) {
			t.Errorf("(%v,%05b) vs (%v,%05b): Satisfies/Contradicts disagree with the old Relation", aPos, aMask, bPos, bMask)
		}
		seen[got]++
	})
	t.Logf("pairs=%d satisfied=%d contradicted=%d inconclusive=%d", pairs, seen[Satisfied], seen[Contradicted], seen[Inconclusive])
	if pairs == 0 || seen[Satisfied] == 0 || seen[Contradicted] == 0 || seen[Inconclusive] == 0 {
		t.Fatal("the pairs did not reach every outcome")
	}
}

// q2aMask is a bitmask set over 0..11 with all three optional fast paths, and
// counters to show which ones Relation reached.
type q2aMask struct {
	m uint16
	c *q2aCalls
}

type q2aCalls struct{ subset, disjoint, difference, intersect, union int }

const q2aAll = 1<<12 - 1

func (a q2aMask) Intersect(b q2aMask) q2aMask {
	a.c.intersect++
	return q2aMask{a.m & b.m, a.c}
}

func (a q2aMask) Union(b q2aMask) q2aMask {
	a.c.union++
	return q2aMask{a.m | b.m, a.c}
}
func (a q2aMask) Complement() q2aMask  { return q2aMask{^a.m & q2aAll, a.c} }
func (a q2aMask) IsEmpty() bool        { return a.m == 0 }
func (a q2aMask) Equal(b q2aMask) bool { return a.m == b.m }

func (a q2aMask) IsSubsetOf(b q2aMask) bool {
	a.c.subset++
	return a.m&^b.m == 0
}

func (a q2aMask) IsDisjointFrom(b q2aMask) bool {
	a.c.disjoint++
	return a.m&b.m == 0
}

func (a q2aMask) Difference(b q2aMask) q2aMask {
	a.c.difference++
	return q2aMask{a.m &^ b.m, a.c}
}

var (
	_ versionset.Set[q2aMask]            = q2aMask{}
	_ versionset.SubsetOfer[q2aMask]     = q2aMask{}
	_ versionset.DisjointFromer[q2aMask] = q2aMask{}
)

// TestRelationRandomMatchesOldAndBuildsNothing: random terms over a set WITH
// the fast paths; the new Relation must agree with the old and must not call
// Intersect, Union or Difference at all.
func TestRelationRandomMatchesOldAndBuildsNothing(t *testing.T) {
	const rounds = 500000
	r := rand.New(rand.NewSource(20261113))
	newCalls, oldCalls := &q2aCalls{}, &q2aCalls{}
	pick := func() (uint16, bool) {
		m := uint16(r.Intn(q2aAll + 1))
		switch r.Intn(8) {
		case 0:
			m = 0
		case 1:
			m = q2aAll
		case 2:
			m &= uint16(r.Intn(q2aAll + 1)) // sparser, so subsets and disjoint pairs are common
		}
		return m, r.Intn(2) == 0
	}
	seen := map[Relation]int{}
	for i := 0; i < rounds; i++ {
		am, an := pick()
		bm, bn := pick()
		mk := func(c *q2aCalls) (Term[q2aMask], Term[q2aMask]) {
			a, b := Positive(q2aMask{am, c}), Positive(q2aMask{bm, c})
			if an {
				a = a.Negate()
			}
			if bn {
				b = b.Negate()
			}
			return a, b
		}
		a, b := mk(newCalls)
		oa, ob := mk(oldCalls)
		got, want := a.Relation(b), oldRelation(oa, ob)
		if got != want {
			t.Fatalf("Relation(%v %012b, %v %012b) = %v, old code gave %v", an, am, bn, bm, got, want)
		}
		seen[got]++
	}
	t.Logf("rounds=%d satisfied=%d contradicted=%d inconclusive=%d", rounds, seen[Satisfied], seen[Contradicted], seen[Inconclusive])
	t.Logf("new calls: %+v", *newCalls)
	t.Logf("old calls: %+v", *oldCalls)
	if newCalls.intersect+newCalls.union+newCalls.difference != 0 {
		t.Errorf("new Relation built a set: %+v", *newCalls)
	}
	if newCalls.subset == 0 || newCalls.disjoint == 0 {
		t.Errorf("new Relation did not reach both fast paths: %+v", *newCalls)
	}
	for _, rel := range []Relation{Satisfied, Contradicted, Inconclusive} {
		if seen[rel] < rounds/100 {
			t.Errorf("only %d of %d rounds were %v", seen[rel], rounds, rel)
		}
	}
}

// TestRelationFallbackUsesTodaysCalls: Ints has no fast path, so Relation must
// still answer through Intersect/Complement and agree with the old code.
func TestRelationFallbackUsesTodaysCalls(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	set := func() versionset.Ints {
		s := versionset.Empty()
		for n := r.Intn(4); n > 0; n-- {
			lo := int64(r.Intn(12))
			s = s.Union(versionset.Range(lo, lo+1+int64(r.Intn(5))))
		}
		if r.Intn(4) == 0 {
			s = s.Complement()
		}
		return s
	}
	seen := map[Relation]int{}
	const rounds = 200000
	for i := 0; i < rounds; i++ {
		a, b := Positive(set()), Positive(set())
		if r.Intn(2) == 0 {
			a = a.Negate()
		}
		if r.Intn(2) == 0 {
			b = b.Negate()
		}
		got, want := a.Relation(b), oldRelation(a, b)
		if got != want {
			t.Fatalf("%v.Relation(%v) = %v, old code gave %v", a, b, got, want)
		}
		if a.Excludes(b) != a.Intersect(b).IsAlwaysFalse() {
			t.Fatalf("%v.Excludes(%v) disagrees with Intersect", a, b)
		}
		seen[got]++
	}
	t.Logf("rounds=%d satisfied=%d contradicted=%d inconclusive=%d", rounds, seen[Satisfied], seen[Contradicted], seen[Inconclusive])
	for _, rel := range []Relation{Satisfied, Contradicted, Inconclusive} {
		if seen[rel] < rounds/100 {
			t.Errorf("only %d of %d rounds were %v", seen[rel], rounds, rel)
		}
	}
}

// plainMask is a bitmask set with no optional fast paths, counting the calls
// Relation makes on it.
type plainMask struct {
	m uint16
	c *q2aCalls
}

func (a plainMask) Intersect(b plainMask) plainMask {
	a.c.intersect++
	return plainMask{a.m & b.m, a.c}
}

func (a plainMask) Union(b plainMask) plainMask {
	a.c.union++
	return plainMask{a.m | b.m, a.c}
}
func (a plainMask) Complement() plainMask  { return plainMask{^a.m & q2aAll, a.c} }
func (a plainMask) IsEmpty() bool          { return a.m == 0 }
func (a plainMask) Equal(b plainMask) bool { return a.m == b.m }

var _ versionset.Set[plainMask] = plainMask{}

// TestRelationFallbackIsUsedWithoutFastPaths: with no optional interface,
// Relation goes through Intersect and agrees with the old code.
func TestRelationFallbackIsUsedWithoutFastPaths(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	newCalls, oldCalls := &q2aCalls{}, &q2aCalls{}
	for i := 0; i < 50000; i++ {
		am, bm := uint16(r.Intn(q2aAll+1)), uint16(r.Intn(q2aAll+1))
		an, bn := r.Intn(2) == 0, r.Intn(2) == 0
		mk := func(c *q2aCalls) (Term[plainMask], Term[plainMask]) {
			a, b := Positive(plainMask{am, c}), Positive(plainMask{bm, c})
			if an {
				a = a.Negate()
			}
			if bn {
				b = b.Negate()
			}
			return a, b
		}
		a, b := mk(newCalls)
		oa, ob := mk(oldCalls)
		if got, want := a.Relation(b), oldRelation(oa, ob); got != want {
			t.Fatalf("Relation = %v, old code gave %v", got, want)
		}
	}
	if newCalls.intersect == 0 {
		t.Errorf("fallback never called Intersect: %+v", *newCalls)
	}
	if newCalls.intersect > oldCalls.intersect {
		t.Errorf("fallback made more Intersect calls (%d) than the old code (%d)", newCalls.intersect, oldCalls.intersect)
	}
}
