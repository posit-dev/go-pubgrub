// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"github.com/posit-dev/go-pubgrub/versionset"
)

// Decision tiers. A higher value is decided sooner; the zero value is the
// default tier every package starts in.
const (
	tierDefault = iota
	tierSecond
	tierFirst
)

// clashKind says which situation produced a clash.
type clashKind int

const (
	// clashTurnedDown: a version was declined at decision time.
	clashTurnedDown clashKind = iota
	// clashLearned: conflict analysis ended with a learned fact.
	clashLearned
)

// clash is one conflict the solver noticed: a blamed package and the other
// packages involved. Only tests read it, through Solver.clashHook.
type clash[P comparable] struct {
	blamed   P
	involved []P
	kind     clashKind
}

// tierState counts clashes per package and holds the tiers. It exists only when
// ClashThreshold is positive.
type tierState[P comparable] struct {
	threshold int
	root      P

	blamed, involved map[P]int
	tier             map[P]int

	// Packages whose tally reached the threshold since the last promotion, in the
	// order they got there.
	reachedBlamed, reachedInvolved []P
}

func newTierState[P comparable](threshold int, root P) *tierState[P] {
	return &tierState[P]{
		threshold: threshold,
		root:      root,
		blamed:    map[P]int{},
		involved:  map[P]int{},
		tier:      map[P]int{},
	}
}

func (t *tierState[P]) tierOf(p P) int { return t.tier[p] }

// record counts one clash. A clash blamed on the root, or left with no involved
// package once the root is dropped, counts for nothing.
func (t *tierState[P]) record(blamed P, involved []P) {
	if blamed == t.root {
		return
	}
	others := make([]P, 0, len(involved))
	for _, p := range involved {
		if p != t.root {
			others = append(others, p)
		}
	}
	if len(others) == 0 {
		return
	}

	t.blamed[blamed]++
	if t.blamed[blamed] == t.threshold {
		t.reachedBlamed = append(t.reachedBlamed, blamed)
	}
	for _, p := range others {
		t.involved[p]++
		if t.involved[p] == t.threshold {
			t.reachedInvolved = append(t.reachedInvolved, p)
		}
	}
}

// promote applies the pending promotions and returns the packages that newly
// entered the second tier.
func (t *tierState[P]) promote() []P {
	for _, p := range t.reachedBlamed {
		t.tier[p] = tierFirst
	}
	var newSecond []P
	for _, p := range t.reachedInvolved {
		if t.tier[p] == tierDefault {
			t.tier[p] = tierSecond
			newSecond = append(newSecond, p)
		}
	}
	t.reachedBlamed, t.reachedInvolved = nil, nil
	return newSecond
}

// undoForPromotion drops the earliest decision of any newly second-tier package,
// and everything after it. It reports whether anything was undone.
func undoForPromotion[P comparable, S versionset.Set[S]](ps *PartialSolution[P, S], newSecond []P) bool {
	if len(newSecond) == 0 {
		return false
	}
	want := make(map[P]bool, len(newSecond))
	for _, p := range newSecond {
		want[p] = true
	}
	for _, a := range ps.Assignments() {
		if a.Decision && want[a.Package] {
			ps.BacktrackTo(a.Level - 1)
			return true
		}
	}
	return false
}

// incompatibilityPackages lists the packages inc names, other than skip, in
// map order (random). Fine: tallies ignore order and the undo picks the
// earliest decision.
func incompatibilityPackages[P comparable, S versionset.Set[S]](inc *Incompatibility[P, S], skip P) []P {
	out := make([]P, 0, inc.Len())
	for p := range inc.terms {
		if p != skip {
			out = append(out, p)
		}
	}
	return out
}
