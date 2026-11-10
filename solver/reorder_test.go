// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// observed is everything a test can see of one solve.
type observed struct {
	log     string // provider calls, solution and order, from runLog
	picks   []string
	clashes []string
	err     error
}

// observe solves u with the given threshold and hooks on. recheck runs the
// optional re-propagation after an undo.
func observe(u *universe, threshold int, recheck bool) observed {
	var o observed
	o.log, o.err = runLog(u, func(s *Solver[string, set]) {
		s.ClashThreshold = threshold
		s.recheck = recheck
		s.pickHook = func(pkg string, tier int) {
			o.picks = append(o.picks, fmt.Sprintf("%s/%d", pkg, tier))
		}
		s.clashHook = func(c clash[string]) {
			inv := append([]string(nil), c.involved...)
			sort.Strings(inv)
			kind := "turned-down"
			if c.kind == clashLearned {
				kind = "learned"
			}
			o.clashes = append(o.clashes, fmt.Sprintf("%s blamed=%s involved=%v", kind, c.blamed, inv))
		}
	})
	return o
}

var (
	wantClashesD = []string{
		"turned-down blamed=c involved=[d]",
		"learned blamed=c involved=[d]",
	}
	wantClashesE = []string{
		"turned-down blamed=d involved=[e]",
		"learned blamed=d involved=[]",
		"learned blamed=b involved=[c]",
	}
)

func mustEqual(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n got  %v\n want %v", what, got, want)
	}
}

func hasLines(t *testing.T, o observed, lines ...string) {
	t.Helper()
	if o.err != nil {
		t.Fatalf("solve failed: %v", o.err)
	}
	for _, l := range lines {
		if !strings.Contains(o.log, l) {
			t.Errorf("log lacks %q:\n%s", l, o.log)
		}
	}
}

// Setting off, zero or negative: the provider calls and the solution are what the
// base solver did, with or without the clash hook.
func TestOffIsInert(t *testing.T) {
	fixtures := map[string]func() *universe{"A": fixtureA, "C": fixtureC, "D": fixtureD, "E": fixtureE}
	for name, f := range fixtures {
		for _, threshold := range []int{0, -1, math.MinInt} {
			o := observe(f(), threshold, false)
			if o.err != nil {
				t.Fatalf("%s: %v", name, o.err)
			}
			if o.log != offGolden[name] {
				t.Errorf("%s T=%d: call log differs from the base:\n%s", name, threshold, o.log)
			}
		}
	}
}

// Fixture D: a turn-down, then the learned fact. The intermediate fact gives no
// clash.
func TestClashesFixtureD(t *testing.T) {
	o := observe(fixtureD(), 0, false)
	mustEqual(t, "clashes", o.clashes, wantClashesD)
	hasLines(t, o, "solution: b=1 d=2 root=1")
	mustEqual(t, "picks", o.picks, []string{"root/0", "d/0", "b/0", "c/0", "c/0", "b/0"})

	// The mutation fixture: c@2 requires d<3, so there is no conflict at all.
	m := observe(fixtureDMutation(), 0, false)
	mustEqual(t, "mutation clashes", m.clashes, []string(nil))
	hasLines(t, m, "solution: b=2 c=2 d=2 root=1", "order: [root d b c]")
}

// Fixture E: analysis right after an undo gives its own clash.
func TestClashesFixtureE(t *testing.T) {
	o := observe(fixtureE(), 0, false)
	mustEqual(t, "clashes", o.clashes, wantClashesE)
	hasLines(t, o, "solution: a=2 b=1 c=1 e=2 root=1")
	mustEqual(t, "picks", o.picks,
		[]string{"root/0", "a/0", "b/0", "c/0", "d/0", "d/0", "b/0", "c/0", "e/0"})
}

// Fixture A: with T=1 the setting changes which package comes right after root.
func TestFixtureAChangesTheNextPick(t *testing.T) {
	off := observe(fixtureA(), 0, false)
	hasLines(t, off, "solution: b=2 c=2 d=1 root=1", "order: [root b c d]")
	mustEqual(t, "off picks", off.picks, []string{"root/0", "b/0", "c/0", "d/0", "d/0"})
	mustEqual(t, "off clashes", off.clashes, []string{"turned-down blamed=d involved=[b]"})

	on := observe(fixtureA(), 1, false)
	hasLines(t, on, "solution: b=1 c=1 d=2 root=1", "order: [root d b c]")
	// d is in the first tier (2), b in the second (1).
	mustEqual(t, "on picks", on.picks,
		[]string{"root/0", "b/0", "c/0", "d/0", "d/2", "b/1", "c/0"})
}

// Fixture B: with the threshold above the one clash, nothing differs from off.
func TestBelowThresholdMatchesOff(t *testing.T) {
	off := observe(fixtureA(), 0, false)
	for _, threshold := range []int{2, 100} {
		on := observe(fixtureA(), threshold, false)
		mustEqual(t, "log", on.log, off.log)
		mustEqual(t, "picks", on.picks, off.picks)
		mustEqual(t, "clashes", on.clashes, off.clashes)
	}
}

// Fixture C: no conflicts, so T=1 changes nothing.
func TestNoConflictsMatchesOff(t *testing.T) {
	off := observe(fixtureC(), 0, false)
	on := observe(fixtureC(), 1, false)
	mustEqual(t, "log", on.log, off.log)
	mustEqual(t, "picks", on.picks, off.picks)
	mustEqual(t, "clashes", on.clashes, []string(nil))
}

// Fixture E at T=1 must return the off solution.
func TestFixtureEKeepsTheSolutionUnderTheSetting(t *testing.T) {
	for _, threshold := range []int{1, 2, 3} {
		o := observe(fixtureE(), threshold, false)
		hasLines(t, o, "solution: a=2 b=1 c=1 e=2 root=1")
	}
}

// Re-propagating after an undo is optional; both ways must agree.
func TestRecheckAfterUndoMakesNoDifference(t *testing.T) {
	fixtures := map[string]func() *universe{"A": fixtureA, "C": fixtureC, "D": fixtureD, "E": fixtureE}
	for name, f := range fixtures {
		for _, threshold := range []int{1, 2, 3} {
			plain, again := observe(f(), threshold, false), observe(f(), threshold, true)
			mustEqual(t, fmt.Sprintf("%s T=%d log", name, threshold), again.log, plain.log)
			mustEqual(t, fmt.Sprintf("%s T=%d picks", name, threshold), again.picks, plain.picks)
		}
	}
}

// Preference order: unavailable first, then rank, then tier, then earliest.
func TestPreferCandidate(t *testing.T) {
	cand := func(found bool, rank, tier int) candidate[string, set] {
		return candidate[string, set]{found: found, rank: rank, tier: tier}
	}
	cases := []struct {
		name string
		a, b candidate[string, set]
		want bool
	}{
		{"lower rank beats a higher tier", cand(true, 1, tierDefault), cand(true, 2, tierFirst), true},
		{"and not the reverse", cand(true, 2, tierFirst), cand(true, 1, tierDefault), false},
		{"first beats second", cand(true, 1, tierFirst), cand(true, 1, tierSecond), true},
		{"second beats default", cand(true, 1, tierSecond), cand(true, 1, tierDefault), true},
		{"default loses to second", cand(true, 1, tierDefault), cand(true, 1, tierSecond), false},
		{"equal tier keeps the incumbent", cand(true, 1, tierSecond), cand(true, 1, tierSecond), false},
		{"unavailable beats a first-tier package", cand(false, math.MaxInt, tierDefault), cand(true, 0, tierFirst), true},
		{"unavailable: higher tier first", cand(false, 0, tierFirst), cand(false, 0, tierDefault), true},
		{"unavailable: lower tier loses", cand(false, 0, tierDefault), cand(false, 0, tierSecond), false},
		{"unavailable: equal tier keeps the incumbent", cand(false, 0, tierSecond), cand(false, 0, tierSecond), false},
	}
	for _, c := range cases {
		if got := preferCandidate(c.a, c.b); got != c.want {
			t.Errorf("%s: preferCandidate = %v, want %v", c.name, got, c.want)
		}
	}
}

// Tier rules, fed scripted clashes.
func TestTierRules(t *testing.T) {
	tier := func(ts *tierState[string], p string) int { return ts.tierOf(p) }

	t.Run("involved reaching T gives the second tier only", func(t *testing.T) {
		ts := newTierState(2, "root")
		ts.record("x", []string{"y"})
		mustEqual(t, "before", ts.promote(), []string(nil))
		ts.record("z", []string{"y"})
		mustEqual(t, "newly second", ts.promote(), []string{"y"})
		mustEqual(t, "y", tier(ts, "y"), tierSecond)
		mustEqual(t, "z", tier(ts, "z"), tierDefault)
	})

	t.Run("blamed reaching T gives the first tier, even from the second", func(t *testing.T) {
		ts := newTierState(1, "root")
		ts.record("a", []string{"y"})
		ts.promote()
		mustEqual(t, "y", tier(ts, "y"), tierSecond)
		mustEqual(t, "a", tier(ts, "a"), tierFirst)
		ts.record("y", []string{"a"})
		mustEqual(t, "no undo for a blamed promotion", ts.promote(), []string(nil))
		mustEqual(t, "y", tier(ts, "y"), tierFirst)
	})

	t.Run("both before one pick gives the first tier", func(t *testing.T) {
		ts := newTierState(1, "root")
		ts.record("p", []string{"q"})
		ts.record("r", []string{"p"})
		mustEqual(t, "newly second", ts.promote(), []string{"q"})
		mustEqual(t, "p", tier(ts, "p"), tierFirst)
	})

	t.Run("a package already first or second is not moved by a later involved tally", func(t *testing.T) {
		ts := newTierState(1, "root")
		ts.record("a", []string{"s"})
		ts.promote()
		ts.record("b", []string{"s", "a"})
		mustEqual(t, "newly second", ts.promote(), []string(nil))
		mustEqual(t, "s", tier(ts, "s"), tierSecond)
		mustEqual(t, "a", tier(ts, "a"), tierFirst)
	})

	t.Run("each tally promotes once", func(t *testing.T) {
		ts := newTierState(1, "root")
		ts.record("a", []string{"s"})
		ts.record("a", []string{"s"})
		mustEqual(t, "newly second", ts.promote(), []string{"s"})
		ts.record("a", []string{"s"})
		mustEqual(t, "later", ts.promote(), []string(nil))
	})

	t.Run("the root is never involved and a blamed root is ignored", func(t *testing.T) {
		ts := newTierState(1, "root")
		ts.record("a", []string{"root"})
		ts.record("root", []string{"a"})
		mustEqual(t, "newly second", ts.promote(), []string(nil))
		mustEqual(t, "a", tier(ts, "a"), tierDefault)
		mustEqual(t, "root", tier(ts, "root"), tierDefault)
		ts.record("a", []string{"root", "b"})
		mustEqual(t, "b", ts.promote(), []string{"b"})
	})

	t.Run("an empty involved set changes nothing", func(t *testing.T) {
		ts := newTierState(1, "root")
		ts.record("a", nil)
		mustEqual(t, "newly second", ts.promote(), []string(nil))
		mustEqual(t, "a", tier(ts, "a"), tierDefault)
	})
}

// Fixture E at T=1: the first clash earns a promotion and two more clashes
// follow in conflicting rounds. None of it may apply until the conflict-free
// point, so the clashes match the off run and all tiers show up together.
func TestPromotionWaitsForAConflictFreePoint(t *testing.T) {
	o := observe(fixtureE(), 1, false)
	mustEqual(t, "clashes", o.clashes, wantClashesE)
	mustEqual(t, "picks", o.picks,
		[]string{"root/0", "a/0", "b/0", "c/0", "d/0", "d/2", "b/2", "c/1", "e/1"})
}
