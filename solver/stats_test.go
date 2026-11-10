// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

func solveStats(t *testing.T, u *universe) Stats {
	t.Helper()
	s := New("root", versionset.Exactly(1), u)
	if _, err := s.Solve(); err != nil {
		t.Fatalf("Solve: %v", err)
	}
	return s.Stats
}

func totalCalls(u *universe) int {
	n := 0
	for _, c := range u.calls {
		n += c
	}
	return n
}

// root needs a and b, each with one version and no requirements.
// Decisions: root, a, b. KeyBuilds: root, then a and b, then b alone.
func TestStatsTwoLeaves(t *testing.T) {
	u := newUniverse().
		with("root", 1, requirement("a", versionset.AtLeast(1)), requirement("b", versionset.AtLeast(1))).
		with("a", 1).with("b", 1)
	u.constantRank = true
	got := solveStats(t, u)
	if got.PropagateVisits == 0 {
		t.Error("PropagateVisits = 0, want > 0")
	}
	got.PropagateVisits = 0
	want := Stats{
		Decisions: 3, Backtracks: 0, PreCommitClassifications: 2,
		KeyBuilds: 4, DependenciesCalls: 3, LiveDependencyRecords: 2,
	}
	if got != want {
		t.Errorf("stats = %+v, want %+v", got, want)
	}
	if n := totalCalls(u); n != got.DependenciesCalls {
		t.Errorf("provider saw %d Dependencies calls, Stats says %d", n, got.DependenciesCalls)
	}
}

// c needs a<2 but a@2 was chosen first, so the solver backtracks once.
func TestStatsBacktrack(t *testing.T) {
	u := newUniverse().
		with("root", 1, requirement("a", versionset.AtLeast(1)), requirement("b", versionset.AtLeast(1))).
		with("a", 1).with("a", 2).
		with("b", 1, requirement("c", versionset.AtLeast(1))).
		with("c", 1, requirement("a", versionset.LessThan(2)))
	u.constantRank = true
	got := solveStats(t, u)
	if got.PropagateVisits == 0 {
		t.Error("PropagateVisits = 0, want > 0")
	}
	got.PropagateVisits = 0
	// Exact counts depend on candidate order, so check what holds for any
	// order: one backtrack, root/a/b/c each decided and asked for their
	// dependencies at least once, and counters that bound each other.
	if got.Backtracks != 1 {
		t.Errorf("Backtracks = %d, want 1", got.Backtracks)
	}
	if got.Decisions < 4 {
		t.Errorf("Decisions = %d, want >= 4", got.Decisions)
	}
	if got.DependenciesCalls < 4 {
		t.Errorf("DependenciesCalls = %d, want >= 4", got.DependenciesCalls)
	}
	if got.LiveDependencyRecords > got.DependenciesCalls {
		t.Errorf("LiveDependencyRecords %d > DependenciesCalls %d", got.LiveDependencyRecords, got.DependenciesCalls)
	}
	if got.KeyBuilds < got.Decisions {
		t.Errorf("KeyBuilds %d < Decisions %d", got.KeyBuilds, got.Decisions)
	}
	if n := totalCalls(u); n != got.DependenciesCalls {
		t.Errorf("provider saw %d Dependencies calls, Stats says %d", n, got.DependenciesCalls)
	}
}

func TestStatsFilledOnError(t *testing.T) {
	// root needs a, a needs root>=2: unsolvable after one decision and one backtrack.
	u := newUniverse().with("root", 1, requirement("a", versionset.AtLeast(1))).
		with("a", 1, requirement("root", versionset.AtLeast(2)))
	s := New("root", versionset.Exactly(1), u)
	if _, err := s.Solve(); err == nil {
		t.Fatal("want Unsolvable")
	}
	if s.Stats.Decisions != 1 || s.Stats.Backtracks != 1 || s.Stats.DependenciesCalls != 2 {
		t.Errorf("stats not filled on Unsolvable: %+v", s.Stats)
	}

	bad := newUniverse().with("root", 1, requirement("a", versionset.AtLeast(1))).with("a", 1)
	bad.failFor = "a"
	s = New("root", versionset.Exactly(1), bad)
	if _, err := s.Solve(); err == nil {
		t.Fatal("want provider error")
	}
	if s.Stats.Decisions != 1 || s.Stats.DependenciesCalls != 1 {
		t.Errorf("stats not filled on error: %+v", s.Stats)
	}
}
