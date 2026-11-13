// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"errors"
	"fmt"
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// redecideUniverse needs a, then fillers, then b. b version i needs c = i, and
// c version j needs a = versions+1-j, so every b tried clashes with the chosen
// a. Each clash backjumps over a and the fillers, which are then decided again.
func redecideUniverse(versions, fillers int) *universe {
	reqs := []Dependency[string, set]{requirement("b", versionset.AtLeast(1))}
	for i := 0; i < fillers; i++ {
		reqs = append(reqs, requirement(fmt.Sprintf("f%03d", i), versionset.AtLeast(1)))
	}
	u := newUniverse().with("root", 1, requirement("a", versionset.AtLeast(1)))
	for i := 0; i < fillers; i++ {
		u.with(fmt.Sprintf("f%03d", i), 1)
	}
	n := int64(versions)
	for v := int64(1); v <= n; v++ {
		u.with("a", v, reqs...)
		u.with("b", v, requirement("c", versionset.Exactly(v)))
		u.with("c", v, requirement("a", versionset.Exactly(n+1-v)))
	}
	u.constantRank = true
	return u
}

// The fixture is only useful if it re-decides many packages per conflict.
func TestRedecideFixtureRedecidesManyPerConflict(t *testing.T) {
	s := New("root", versionset.Exactly(1), redecideUniverse(10, 50))
	if _, err := s.Solve(); err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if s.Stats.Backtracks == 0 || s.Stats.Decisions/s.Stats.Backtracks < 20 {
		t.Fatalf("stats %+v: want at least 20 decisions per backtrack", s.Stats)
	}
}

func TestMaxConflictsDoesNotTripWhereMaxRoundsDoes(t *testing.T) {
	build := func() *universe { return redecideUniverse(10, 50) }

	rounds := New("root", versionset.Exactly(1), build())
	rounds.MaxRounds = 300
	if _, err := rounds.Solve(); err == nil {
		t.Fatal("MaxRounds=300 should give up: the fixture needs 500+ decisions")
	}

	conflicts := New("root", versionset.Exactly(1), build())
	conflicts.MaxConflicts = 300
	sol, err := conflicts.Solve()
	if err != nil {
		t.Fatalf("MaxConflicts=300 gave up after %d conflicts: %v", conflicts.Stats.Backtracks, err)
	}
	if got := sol.Selected["a"]; !got.Equal(versionset.Exactly(int64(10))) {
		t.Errorf("a = %v, want 10", got)
	}
}

func TestMaxConflictsGivesUpWithTypedError(t *testing.T) {
	s := New("root", versionset.Exactly(1), redecideUniverse(10, 50))
	s.MaxConflicts = 3

	_, err := s.Solve()
	if err == nil {
		t.Fatal("expected the conflict limit to stop the solve")
	}
	if !errors.Is(err, ErrMaxConflicts) {
		t.Fatalf("err = %v, want errors.Is(err, ErrMaxConflicts)", err)
	}
	var limit *MaxConflictsError
	if !errors.As(err, &limit) || limit.Limit != 3 {
		t.Errorf("err = %#v, want *MaxConflictsError with Limit 3", err)
	}
	var unsolvable *Unsolvable[string, set]
	if errors.As(err, &unsolvable) {
		t.Error("hitting the conflict limit is not a proof of unsolvability")
	}
	if s.Stats.Backtracks != 3 {
		t.Errorf("Backtracks = %d, want 3: the solve should stop at the limit", s.Stats.Backtracks)
	}
}

func TestMaxConflictsDefaultIsOff(t *testing.T) {
	s := New("root", versionset.Exactly(1), redecideUniverse(10, 50))
	if _, err := s.Solve(); err != nil {
		t.Fatalf("Solve with no limits: %v", err)
	}
}

// With both caps set, whichever is reached first ends the solve.
func TestMaxRoundsAndMaxConflictsFirstHitWins(t *testing.T) {
	t.Run("rounds first", func(t *testing.T) {
		s := New("root", versionset.Exactly(1), redecideUniverse(10, 50))
		s.MaxRounds, s.MaxConflicts = 300, 8
		_, err := s.Solve()
		if err == nil || errors.Is(err, ErrMaxConflicts) {
			t.Fatalf("err = %v, want the round-limit error", err)
		}
	})
	t.Run("conflicts first", func(t *testing.T) {
		s := New("root", versionset.Exactly(1), redecideUniverse(10, 50))
		s.MaxRounds, s.MaxConflicts = 100000, 3
		_, err := s.Solve()
		if !errors.Is(err, ErrMaxConflicts) {
			t.Fatalf("err = %v, want ErrMaxConflicts", err)
		}
	})
}
