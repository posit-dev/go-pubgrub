// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

var runningPkgs = []string{"a", "b", "c"}

func randTerm(r *rand.Rand) tm {
	lo := int64(r.Intn(5))
	s := versionset.Range(lo, lo+1+int64(r.Intn(4)))
	if r.Intn(3) == 0 {
		return neg(s)
	}
	return pos(s)
}

func randIncompat(r *rand.Rand) *inc {
	terms := map[string]tm{}
	for n := 1 + r.Intn(3); n > 0; n-- {
		terms[runningPkgs[r.Intn(len(runningPkgs))]] = randTerm(r)
	}
	return NewIncompatibility(KindDerived, terms)
}

// compareQueries fails when the two partial solutions answer any satisfier query
// differently. what names the step, for the failure message.
func compareQueries(t *testing.T, what string, r *rand.Rand, off, on *psol) bool {
	t.Helper()
	if off.Len() != on.Len() {
		t.Errorf("%s: Len off=%d on=%d", what, off.Len(), on.Len())
		return false
	}
	for _, p := range runningPkgs {
		a, aok := off.Accumulated(p)
		b, bok := on.Accumulated(p)
		if aok != bok || (aok && !a.Equal(b)) {
			t.Errorf("%s: Accumulated(%s) off=%v,%v on=%v,%v", what, p, a, aok, b, bok)
			return false
		}
		for k := 0; k < 4; k++ {
			q := randTerm(r)
			i1, ok1 := off.FirstIndexSatisfying(p, q)
			i2, ok2 := on.FirstIndexSatisfying(p, q)
			if i1 != i2 || ok1 != ok2 {
				t.Errorf("%s: FirstIndexSatisfying(%s, %v) off=%d,%v on=%d,%v", what, p, q, i1, ok1, i2, ok2)
				return false
			}
		}
	}
	for k := 0; k < 6; k++ {
		in := randIncompat(r)
		i1, p1, ok1 := off.SatisfierOf(in)
		i2, p2, ok2 := on.SatisfierOf(in)
		if i1 != i2 || p1 != p2 || ok1 != ok2 {
			t.Errorf("%s: SatisfierOf(%v) off=%d,%s,%v on=%d,%s,%v", what, in, i1, p1, ok1, i2, p2, ok2)
			return false
		}
		for s := 0; s < off.Len(); s++ {
			j1, ok1 := off.PreviousSatisfierOf(in, s)
			j2, ok2 := on.PreviousSatisfierOf(in, s)
			if j1 != j2 || ok1 != ok2 {
				t.Errorf("%s: PreviousSatisfierOf(%v, %d) off=%d,%v on=%d,%v", what, in, s, j1, ok1, j2, ok2)
				return false
			}
		}
	}
	return true
}

// Random derive, decide and backtrack steps are applied to a partial solution
// with the option off and one with it on. Every query must agree after every
// step, before and after each backtrack.
func TestRunningSatisfierAgreesWithRescan(t *testing.T) {
	for seed := int64(0); seed < 200; seed++ {
		r := rand.New(rand.NewSource(seed))
		off, on := newPS(), newPS()
		on.enableRunningSatisfier()
		for step := 0; step < 25; step++ {
			what := fmt.Sprintf("seed %d step %d", seed, step)
			switch op := r.Intn(6); {
			case op < 3:
				p, tt := runningPkgs[r.Intn(len(runningPkgs))], randTerm(r)
				off.Derive(p, tt, nil)
				on.Derive(p, tt, nil)
				what += " derive"
			case op < 4:
				p, tt := runningPkgs[r.Intn(len(runningPkgs))], randTerm(r)
				if !tt.IsPositive() || !off.Eligible(p, tt.Set()) {
					continue
				}
				off.Decide(p, tt.Set())
				on.Decide(p, tt.Set())
				what += " decide"
			default:
				lvl := r.Intn(off.Level() + 1)
				off.BacktrackTo(lvl)
				on.BacktrackTo(lvl)
				what += fmt.Sprintf(" backtrack to %d", lvl)
			}
			if !compareQueries(t, what, r, off, on) {
				return
			}
		}
	}
}

// Off is the default: no running state exists unless the option is set.
func TestRunningSatisfierIsOffByDefault(t *testing.T) {
	s := New("root", versionset.Exactly(1), backtrackUniverse())
	if _, err := s.Solve(); err != nil {
		t.Fatal(err)
	}
	if s.ps.rs != nil {
		t.Error("running state exists with RunningSatisfier unset")
	}
}

func backtrackUniverse() *universe {
	u := newUniverse().
		with("root", 1, requirement("a", versionset.AtLeast(1)), requirement("b", versionset.AtLeast(1))).
		with("a", 1).with("a", 2).
		with("b", 1, requirement("c", versionset.AtLeast(1))).
		with("c", 1, requirement("a", versionset.LessThan(2)))
	u.constantRank = true
	return u
}

// The solver gives the same answer with the option on, it backtracks (so
// conflict resolution ran), and the running terms answered the satisfier queries.
func TestRunningSatisfierSolveUnchangedAndUsed(t *testing.T) {
	run := func(on bool) (*Solution[string, set], *Solver[string, set]) {
		s := New("root", versionset.Exactly(1), backtrackUniverse())
		s.RunningSatisfier = on
		sol, err := s.Solve()
		if err != nil {
			t.Fatalf("Solve(on=%v): %v", on, err)
		}
		return sol, s
	}
	offSol, offS := run(false)
	onSol, onS := run(true)
	want := map[string]int64{"root": 1, "a": 1, "b": 1, "c": 1}
	assertSelected(t, offSol, want)
	assertSelected(t, onSol, want)
	if fmt.Sprint(offSol.Order) != fmt.Sprint(onSol.Order) {
		t.Errorf("order off=%v on=%v", offSol.Order, onSol.Order)
	}
	if offS.Stats != onS.Stats {
		t.Errorf("stats off=%+v on=%+v", offS.Stats, onS.Stats)
	}
	if onS.Stats.Backtracks == 0 {
		t.Error("no backtrack happened; the test does not reach BacktrackTo")
	}
	if onS.ps.rs == nil || onS.ps.rs.queries == 0 {
		t.Error("running terms answered no satisfier query")
	}
}
