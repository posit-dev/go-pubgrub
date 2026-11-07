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
	want := Stats{
		Decisions: 5, Backtracks: 1, PreCommitClassifications: 5,
		KeyBuilds: 10, DependenciesCalls: 6, LiveDependencyRecords: 4,
	}
	if got != want {
		t.Errorf("stats = %+v, want %+v", got, want)
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
