// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"strings"
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// fixtureClashFirst: d has many versions and its newest clashes with b and c,
// which have only two each. By version count alone b and c are always decided
// before d.
func fixtureClashFirst() *universe {
	return newUniverse().
		with("root", 1, requirement("b", ge(1)), requirement("c", ge(1)), requirement("d", ge(1))).
		with("b", 1).with("b", 2).
		with("c", 1).with("c", 2).
		with("d", 1).with("d", 2).with("d", 3).with("d", 4).
		with("d", 5, requirement("b", lt(2)), requirement("c", lt(2)))
}

func solveClashFirst(t *testing.T, u *universe, clashFirst bool) string {
	t.Helper()
	s := New("root", versionset.Exactly(1), u)
	s.ClashThreshold, s.ClashFirst = 1, clashFirst
	var picks []string
	s.pickHook = func(pkg string, tier int) { picks = append(picks, pkg) }
	if _, err := s.Solve(); err != nil {
		t.Fatalf("Solve: %v", err)
	}
	return strings.Join(picks, " ")
}

// After the first clash d is decided again before b, although b has fewer versions.
func TestClashFirstDecidesClashingPackageFirst(t *testing.T) {
	if got, want := solveClashFirst(t, fixtureClashFirst(), true), "root c b d d d c b"; got != want {
		t.Errorf("picks = %q, want %q", got, want)
	}
}

// Off, the order is the version-count order, as before.
func TestClashFirstOffKeepsVersionCountOrder(t *testing.T) {
	if got, want := solveClashFirst(t, fixtureClashFirst(), false), "root c b d b d"; got != want {
		t.Errorf("picks = %q, want %q", got, want)
	}
}

// ClashFirst does nothing without a positive ClashThreshold.
func TestClashFirstNeedsThreshold(t *testing.T) {
	plain, err := New("root", versionset.Exactly(1), fixtureClashFirst()).Solve()
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	for _, threshold := range []int{0, -1} {
		s := New("root", versionset.Exactly(1), fixtureClashFirst())
		s.ClashThreshold, s.ClashFirst = threshold, true
		sol, err := s.Solve()
		if err != nil {
			t.Fatalf("Solve: %v", err)
		}
		if strings.Join(sol.Order, " ") != strings.Join(plain.Order, " ") {
			t.Errorf("T=%d: order %v differs from default %v", threshold, sol.Order, plain.Order)
		}
	}
}

// Comparison only: the tier beats a lower rank; equal tiers fall back to rank;
// an equal rank and tier keeps the incumbent (no place tie-break).
func TestPreferCandidateClashFirst(t *testing.T) {
	o := &packageOrder[string]{tierFirst: true, places: map[string]int{}, marked: map[string]bool{}}
	hi := func(rank, tier int) candidate[string, set] { return withTier(candOf("hi", true, rank), tier) }
	lo := func(rank, tier int) candidate[string, set] { return withTier(candOf("lo", true, rank), tier) }

	if !preferCandidate(hi(167, tierFirst), lo(2, tierDefault), o) {
		t.Error("a higher tier must beat a lower rank")
	}
	if preferCandidate(lo(2, tierDefault), hi(167, tierFirst), o) {
		t.Error("a lower tier must lose to a higher one despite the lower rank")
	}
	if !preferCandidate(lo(2, tierFirst), hi(167, tierFirst), o) {
		t.Error("equal tier: the lower rank must win")
	}
	if preferCandidate(lo(2, tierFirst), hi(2, tierFirst), o) || preferCandidate(hi(2, tierFirst), lo(2, tierFirst), o) {
		t.Error("equal rank and tier: neither may displace the incumbent")
	}
	if !preferCandidate(hi(0, tierFirst), lo(0, tierDefault), o) {
		t.Error("both unavailable: the higher tier must win")
	}
}

// With OwnOrder, an Exact-marked package beats a clash-promoted unmarked one.
func TestPreferCandidateClashFirstOwnOrderMarkedFirst(t *testing.T) {
	o := newPackageOrder[string](false, true)
	o.tierFirst = true
	o.place("plain")
	o.place("exact")
	o.marked["exact"] = true
	plain := withTier(candOf("plain", true, 9), tierFirst)
	exact := withTier(candOf("exact", true, 1), tierDefault)

	if !preferCandidate(exact, plain, o) {
		t.Error("a marked package must beat a higher-tier unmarked one")
	}
	if preferCandidate(plain, exact, o) {
		t.Error("a higher-tier unmarked package must not beat a marked one")
	}
	o.marked["plain"] = true
	if !preferCandidate(plain, exact, o) {
		t.Error("both marked: the higher tier must win")
	}
}

// StoredKeys and StableDiscovery do not change the ClashFirst pick sequence.
func TestClashFirstSamePicksWithStoredKeysAndStableDiscovery(t *testing.T) {
	picks := func(stable, stored bool) string {
		s := New("root", versionset.Exactly(1), fixtureClashFirst())
		s.ClashThreshold, s.ClashFirst = 1, true
		s.StableDiscovery, s.StoredKeys = stable, stored
		var got []string
		s.pickHook = func(pkg string, _ int) { got = append(got, pkg) }
		if _, err := s.Solve(); err != nil {
			t.Fatalf("Solve: %v", err)
		}
		return strings.Join(got, " ")
	}
	for _, stable := range []bool{false, true} {
		plain, stored := picks(stable, false), picks(stable, true)
		if plain != stored {
			t.Errorf("stable=%v: picks %q with StoredKeys, %q without", stable, stored, plain)
		}
		if !stable && plain != "root c b d d d c b" {
			t.Errorf("picks = %q, want %q", plain, "root c b d d d c b")
		}
	}
}
