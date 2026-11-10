// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"strings"
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

const hugeThreshold = 1 << 30

// solveWith runs u with the order settings and threshold, and returns the
// decision order, the Dependencies call log, and the solution.
func solveWith(t *testing.T, u *universe, stable, own bool, threshold int) (string, string, *Solution[string, set]) {
	t.Helper()
	lu := &loggedUniverse{universe: u}
	s := New("root", versionset.Exactly(1), lu)
	s.StableDiscovery, s.OwnOrder, s.ClashThreshold = stable, own, threshold
	sol, err := s.Solve()
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	return strings.Join(sol.Order, " "), strings.Join(lu.log, " "), sol
}

// withTier is a candidate with a tier set.
func withTier(c candidate[string, set], tier int) candidate[string, set] {
	c.tier = tier
	return c
}

// Stable-discovery spec section 1, closing paragraph: with ClashThreshold on, the
// preference is unavailable, rank, higher tier, place. In fixture A the turned-down
// d and the involved b are promoted, so the tier beats the place (b < c < d).
func TestStableDiscoveryWithClashThreshold(t *testing.T) {
	want := map[string]int64{"root": 1, "b": 1, "c": 1, "d": 2}

	order, calls, sol := solveWith(t, fixtureA(), true, false, 1)
	if order != "root d b c" {
		t.Errorf("order = %q, want %q", order, "root d b c")
	}
	if want := "root@1 b@2 c@2 d@2 d@2 b@1 c@1"; calls != want {
		t.Errorf("calls = %q, want %q", calls, want)
	}
	assertSelected(t, sol, want)

	// Threshold zero or less plays no part: stable-discovery alone.
	for _, threshold := range []int{0, -1} {
		order, calls, sol := solveWith(t, fixtureA(), true, false, threshold)
		if order != "root b c d" || calls != "root@1 b@2 c@2 d@2 d@1" {
			t.Errorf("T=%d: order %q calls %q, want the stable-only run", threshold, order, calls)
		}
		assertSelected(t, sol, map[string]int64{"root": 1, "b": 2, "c": 2, "d": 1})
	}
}

// Same paragraph, comparison only: rank first, then tier, then place.
func TestPreferCandidateStableWithTiers(t *testing.T) {
	o := newPackageOrder[string](true, false)
	o.place("early")
	o.place("late")
	early := func(found bool, rank, tier int) candidate[string, set] {
		return withTier(candOf("early", found, rank), tier)
	}
	late := func(found bool, rank, tier int) candidate[string, set] {
		return withTier(candOf("late", found, rank), tier)
	}

	if !preferCandidate(late(true, 1, tierDefault), early(true, 2, tierFirst), o) {
		t.Error("lower rank must beat a higher tier")
	}
	if !preferCandidate(late(true, 2, tierFirst), early(true, 2, tierSecond), o) {
		t.Error("equal rank: the higher tier must beat the smaller place")
	}
	if preferCandidate(early(true, 2, tierSecond), late(true, 2, tierFirst), o) {
		t.Error("equal rank: the lower tier must lose despite the smaller place")
	}
	if !preferCandidate(early(true, 2, tierFirst), late(true, 2, tierFirst), o) {
		t.Error("equal rank and tier: the smaller place must win")
	}
	if !preferCandidate(late(false, 0, tierFirst), early(false, 0, tierDefault), o) {
		t.Error("both unavailable: the higher tier must beat the smaller place")
	}
}

// Own-order spec test I, first half: with a threshold no tally reaches, every
// spec case gives what it gives without one.
func TestOrderSettingsWithUnreachedThreshold(t *testing.T) {
	for _, c := range orderCases() {
		for _, m := range c.modes {
			t.Run(c.name+"/"+m.name, func(t *testing.T) {
				order, calls, sol := solveWith(t, c.build(), m.stable, m.own, hugeThreshold)
				if order != m.order {
					t.Errorf("order = %q, want %q", order, m.order)
				}
				if calls != m.calls {
					t.Errorf("Dependencies calls = %q, want %q", calls, m.calls)
				}
				assertSelected(t, sol, c.sel)
			})
		}
	}
}

// Own-order spec test I, second half, comparison only: with the same mark a
// higher tier beats a lower one whatever the places; a mark beats a higher tier.
func TestPreferCandidateOwnOrderWithTiers(t *testing.T) {
	o := newPackageOrder[string](false, true)
	o.place("first")
	o.place("second")

	first := func(found bool, rank, tier int) candidate[string, set] {
		return withTier(candOf("first", found, rank), tier)
	}
	second := func(found bool, rank, tier int) candidate[string, set] {
		return withTier(candOf("second", found, rank), tier)
	}

	if !preferCandidate(second(true, 9, tierFirst), first(true, 1, tierSecond), o) {
		t.Error("same mark: the higher tier must win whatever the places and ranks")
	}
	if preferCandidate(first(true, 1, tierSecond), second(true, 9, tierFirst), o) {
		t.Error("same mark: the lower tier must lose despite the smaller place")
	}

	o.marked["first"] = true
	if !preferCandidate(first(true, 1, tierDefault), second(true, 1, tierFirst), o) {
		t.Error("a mark must beat a higher tier")
	}
	if preferCandidate(second(true, 1, tierFirst), first(true, 1, tierDefault), o) {
		t.Error("a higher tier must not beat a mark")
	}

	o.marked["second"] = true
	if !preferCandidate(second(true, 1, tierFirst), first(true, 1, tierDefault), o) {
		t.Error("equal marks: the higher tier must beat the smaller place")
	}
}

// End to end, tier in play under OwnOrder: fixture A has no marks, so the order
// is places and tiers, and the tier moves d and b ahead of c.
func TestOwnOrderWithClashThreshold(t *testing.T) {
	order, calls, sol := solveWith(t, fixtureA(), false, true, 1)
	if order != "root d b c" {
		t.Errorf("order = %q, want %q", order, "root d b c")
	}
	if want := "root@1 b@2 c@2 d@2 d@2 b@1 c@1"; calls != want {
		t.Errorf("calls = %q, want %q", calls, want)
	}
	assertSelected(t, sol, map[string]int64{"root": 1, "b": 1, "c": 1, "d": 2})
}

// exactly2 requires exactly version 2 of pkg, with Exact set.
func exactly2(pkg string) Dependency[string, set] {
	return Dependency[string, set]{Package: pkg, Allowed: versionset.Exactly(2), Exact: true}
}

// unavailableMarked: m@2 names z (no version >= 2, unmarked, place 2) and then w
// (exactly 2, marked, place 3). Neither has an available version, so both belong
// to the no-version tier. The specs do not say how two packages in that tier are
// ordered under OwnOrder; the tier text gives the remaining preferences in
// order, so the marked w comes before z despite the larger place.
func unavailableMarked() *universe {
	return newUniverse().
		with("root", 1, atLeast("m", 1)).
		with("m", 1).
		with("m", 2, atLeast("z", 2), exactly2("w")).
		with("z", 1).with("w", 1)
}

// solvePicks returns the packages in the order the solver picked them.
func solvePicks(t *testing.T, stable, own bool) string {
	t.Helper()
	s := New("root", versionset.Exactly(1), unavailableMarked())
	s.StableDiscovery, s.OwnOrder = stable, own
	var picks []string
	s.pickHook = func(pkg string, _ int) { picks = append(picks, pkg) }
	sol, err := s.Solve()
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	assertSelected(t, sol, map[string]int64{"root": 1, "m": 1})
	return strings.Join(picks, " ")
}

func TestOwnOrderUnavailableMarked(t *testing.T) {
	// OwnOrder: the marked, unavailable w goes before the unmarked z.
	if got := solvePicks(t, false, true); got != "root m w m" {
		t.Errorf("OwnOrder picks = %q, want %q", got, "root m w m")
	}
	// StableDiscovery only: Exact is ignored, so the smaller place, z, goes first.
	if got := solvePicks(t, true, false); got != "root m z m" {
		t.Errorf("StableDiscovery picks = %q, want %q", got, "root m z m")
	}

	// Comparison: an unavailable package beats a marked available one, and a
	// marked unavailable one beats an unmarked unavailable one.
	o := newPackageOrder[string](false, true)
	o.place("early")
	o.place("late")
	o.marked["late"] = true
	if !preferCandidate(candOf("early", false, 0), candOf("late", true, 1), o) {
		t.Error("unavailable must beat a marked available package")
	}
	if !preferCandidate(candOf("late", false, 0), candOf("early", false, 0), o) {
		t.Error("both unavailable: the marked one must go first")
	}
	if preferCandidate(candOf("early", false, 0), candOf("late", false, 0), o) {
		t.Error("both unavailable: the unmarked one must go second")
	}
}
