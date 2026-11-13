// SPDX-License-Identifier: Apache-2.0 OR MIT

package versionset

import "testing"

// disjointMask is fastMask plus the optional IsDisjointFrom.
type disjointMask struct {
	fastMask
	n *int
}

func (a disjointMask) Intersect(b disjointMask) disjointMask {
	return disjointMask{a.fastMask.Intersect(b.fastMask), a.n}
}

func (a disjointMask) Union(b disjointMask) disjointMask {
	return disjointMask{a.fastMask.Union(b.fastMask), a.n}
}
func (a disjointMask) Complement() disjointMask  { return disjointMask{a.fastMask.Complement(), a.n} }
func (a disjointMask) Equal(b disjointMask) bool { return a.m == b.m }

func (a disjointMask) IsDisjointFrom(b disjointMask) bool {
	*a.n++
	return a.m&b.m == 0
}

var (
	_ Set[disjointMask]            = disjointMask{}
	_ DisjointFromer[disjointMask] = disjointMask{}
)

// TestDisjointFastPathIsUsedAndAgrees: every pair over the six-version universe.
func TestDisjointFastPathIsUsedAndAgrees(t *testing.T) {
	n, pairs := 0, 0
	calls := &fastCalls{}
	for x := 0; x <= fastAll; x++ {
		for y := 0; y <= fastAll; y++ {
			a := disjointMask{fastMask{uint8(x), calls}, &n}
			b := disjointMask{fastMask{uint8(y), calls}, &n}
			pairs++
			if got, want := IsDisjointFrom(a, b), a.Intersect(b).IsEmpty(); got != want {
				t.Fatalf("IsDisjointFrom(%b, %b) = %v, Intersect says %v", x, y, got, want)
			}
			// A set without the method takes the Intersect path and must agree.
			if got, want := IsDisjointFrom(a.fastMask, b.fastMask), a.m&b.m == 0; got != want {
				t.Fatalf("fallback IsDisjointFrom(%b, %b) = %v, want %v", x, y, got, want)
			}
		}
	}
	if n != pairs {
		t.Errorf("IsDisjointFrom used the fast path %d times, want %d", n, pairs)
	}
}
