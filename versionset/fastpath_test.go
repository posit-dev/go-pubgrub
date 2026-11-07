// SPDX-License-Identifier: Apache-2.0 OR MIT

package versionset

import "testing"

// fastMask is a bitmask set over 0..5 that also offers the optional fast paths.
// The counters let a test see which path IsSubsetOf and Difference took.
type fastMask struct{ m uint8 }

const fastAll = 1<<universe - 1

var subsetCalls, differenceCalls int

func (a fastMask) Intersect(b fastMask) fastMask { return fastMask{a.m & b.m} }
func (a fastMask) Union(b fastMask) fastMask     { return fastMask{a.m | b.m} }
func (a fastMask) Complement() fastMask          { return fastMask{^a.m & fastAll} }
func (a fastMask) IsEmpty() bool                 { return a.m == 0 }
func (a fastMask) Equal(b fastMask) bool         { return a.m == b.m }

func (a fastMask) IsSubsetOf(b fastMask) bool {
	subsetCalls++
	return a.m&^b.m == 0
}

func (a fastMask) Difference(b fastMask) fastMask {
	differenceCalls++
	return fastMask{a.m &^ b.m}
}

var (
	_ Set[fastMask]         = fastMask{}
	_ SubsetOfer[fastMask]  = fastMask{}
	_ Differencer[fastMask] = fastMask{}
)

// TestOptionalFastPathsAreUsedAndAgree checks the helpers call the optional
// methods when a set has them, and that the answers match the complement path.
func TestOptionalFastPathsAreUsedAndAgree(t *testing.T) {
	subsetCalls, differenceCalls = 0, 0
	pairs := 0
	for x := 0; x <= fastAll; x++ {
		for y := 0; y <= fastAll; y++ {
			a, b := fastMask{uint8(x)}, fastMask{uint8(y)}
			pairs++

			wantSubset := a.Intersect(b.Complement()).IsEmpty()
			if got := IsSubsetOf(a, b); got != wantSubset {
				t.Fatalf("IsSubsetOf(%b, %b) = %v, complement path says %v", x, y, got, wantSubset)
			}
			wantDiff := a.Intersect(b.Complement())
			if got := Difference(a, b); !got.Equal(wantDiff) {
				t.Fatalf("Difference(%b, %b) = %b, complement path says %b", x, y, got.m, wantDiff.m)
			}
		}
	}
	if subsetCalls != pairs {
		t.Errorf("IsSubsetOf used the fast path %d times, want %d", subsetCalls, pairs)
	}
	if differenceCalls != pairs {
		t.Errorf("Difference used the fast path %d times, want %d", differenceCalls, pairs)
	}
}

// TestIntsHasNoFastPath pins that a set without the optional methods still works.
func TestIntsHasNoFastPath(t *testing.T) {
	a, b := Range(1, 4), Range(2, 6)
	if IsSubsetOf(a, b) {
		t.Error("[1,4) is not a subset of [2,6)")
	}
	if !Difference(a, b).Equal(Range(1, 2)) {
		t.Errorf("Difference = %v, want [1,2)", Difference(a, b))
	}
}
