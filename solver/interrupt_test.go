// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// walkUniverse makes the solver turn p down one version at a time with no
// conflict: q is fixed at 1, every p above 2 needs q = 2, and p 2 needs nothing.
func walkUniverse(versions int) *universe {
	u := newUniverse().
		with("root", 1, requirement("q", versionset.Exactly(1)), requirement("p", versionset.AtLeast(1))).
		with("q", 1).with("q", 2).
		with("p", 2)
	for v := int64(2); v <= int64(versions); v++ {
		u.with("p", 2*v, requirement("q", versionset.Exactly(2)))
	}
	return u
}

// An interrupt that fires on call N is the last call: the solve stops there,
// in the round that made it, and hands back the error it returned.
func TestInterruptStopsAtTheRound(t *testing.T) {
	stop := errors.New("stop")
	const fireAt = 50
	calls := 0
	s := New("root", versionset.Exactly(1), walkUniverse(400))
	s.Interrupt = func() error {
		calls++
		if calls == fireAt {
			return stop
		}
		return nil
	}
	_, err := s.Solve()
	var ie *InterruptedError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v, want an *InterruptedError", err)
	}
	if calls != fireAt {
		t.Errorf("Interrupt was called %d times, want %d: no call after it fired", calls, fireAt)
	}
	// The walk has no conflicts, so there is one call per round, counting from round 0.
	if ie.Rounds != fireAt-1 {
		t.Errorf("Rounds = %d, want %d", ie.Rounds, fireAt-1)
	}
	if !errors.Is(err, stop) || !errors.Is(err, ErrInterrupted) {
		t.Errorf("err = %v: want errors.Is to match the returned error and ErrInterrupted", err)
	}
}

func TestInterruptErrorsIsContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	s := New("root", versionset.Exactly(1), walkUniverse(40))
	s.Interrupt = ctx.Err
	_, err := s.Solve()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want errors.Is(err, context.DeadlineExceeded)", err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	s = New("root", versionset.Exactly(1), walkUniverse(40))
	s.Interrupt = ctx2.Err
	if _, err := s.Solve(); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want errors.Is(err, context.Canceled)", err)
	}
}

// chainConflict builds n derivations at level 0, x(i) = 1 forcing x(i+1) = 1,
// and a conflict on the last one. Resolving it folds one cause per step.
func chainConflict(n int) (*psol, *Store[string, set], *Incompatibility[string, set]) {
	ps := newPS()
	st := NewStore[string, set]()
	name := func(i int) string { return fmt.Sprintf("x%d", i) }
	root := NewIncompatibility(KindRoot, map[string]tm{name(0): neg(versionset.Exactly(1))})
	st.Add(root)
	ps.Derive(name(0), pos(versionset.Exactly(1)), root)
	for i := 0; i < n; i++ {
		dep := NewIncompatibility(KindDependency, map[string]tm{
			name(i):     pos(versionset.Exactly(1)),
			name(i + 1): neg(versionset.Exactly(1)),
		})
		st.Add(dep)
		ps.Derive(name(i+1), pos(versionset.Exactly(1)), dep)
	}
	bad := NewIncompatibility(KindUnavailable, map[string]tm{name(n): pos(versionset.Exactly(1))})
	st.Add(bad)
	return ps, st, bad
}

// One conflict can take many steps inside a single round, so the interrupt is
// also checked per step, and the resolution stops at the step that fired.
func TestInterruptStopsConflictResolutionWithinTheStep(t *testing.T) {
	const n, fireAt = 30, 7
	stop := errors.New("stop")

	ps, st, bad := chainConflict(n)
	steps := 0
	_, err := resolve(ps, st, "root", bad, nil, func() error {
		steps++
		if steps == fireAt {
			return stop
		}
		return nil
	})
	if err != stop { //nolint:errorlint // resolve hands the interrupt's error back as it is
		t.Fatalf("err = %v, want the interrupt's error", err)
	}
	if steps != fireAt {
		t.Errorf("interrupt called %d times, want %d: it must stop at the step that fired", steps, fireAt)
	}

	// Without an interrupt the same conflict resolves in n steps.
	ps, st, bad = chainConflict(n)
	steps = 0
	res, err := resolve(ps, st, "root", bad, nil, func() error { steps++; return nil })
	if err != nil || !res.Unsolvable {
		t.Fatalf("uninterrupted: res %v, err %v, want unsolvable", res, err)
	}
	if steps < n {
		t.Errorf("a conflict of %d folds took %d steps, want at least %d", n, steps, n)
	}
}

// Through Solve, an interrupt that fires on the last call of an unsolvable
// solve ends it with the interrupt's error, not the Unsolvable result.
func TestInterruptReachesConflictStepsThroughSolve(t *testing.T) {
	u := newUniverse().
		with("root", 1, requirement("a", versionset.AtLeast(1))).
		with("a", 1, requirement("b", versionset.Exactly(1))).
		with("b", 1, requirement("c", versionset.Exactly(2))).
		with("c", 1)
	baseline := New("root", versionset.Exactly(1), u)
	baseCalls := 0
	baseline.Interrupt = func() error { baseCalls++; return nil }
	_, err := baseline.Solve()
	var unsolvable *Unsolvable[string, set]
	if !errors.As(err, &unsolvable) {
		t.Fatalf("baseline err = %v, want unsolvable", err)
	}
	if baseline.Stats.Backtracks == 0 {
		t.Fatalf("fixture resolved no conflict: %+v", baseline.Stats)
	}
	stop := errors.New("stop")
	s := New("root", versionset.Exactly(1), u)
	calls := 0
	s.Interrupt = func() error {
		calls++
		if calls == baseCalls {
			return stop
		}
		return nil
	}
	if _, err := s.Solve(); !errors.Is(err, stop) {
		t.Fatalf("err = %v, want the interrupt's error from the last call", err)
	}
}

// A nil Interrupt, and one that never fires, leave the answer and the counters alone.
func TestNilInterruptChangesNothing(t *testing.T) {
	for _, n := range []int{40, 400} {
		plain := New("root", versionset.Exactly(1), walkUniverse(n))
		want, err := plain.Solve()
		if err != nil {
			t.Fatal(err)
		}
		hooked := New("root", versionset.Exactly(1), walkUniverse(n))
		hooked.Interrupt = func() error { return nil }
		got, err := hooked.Solve()
		if err != nil {
			t.Fatalf("an Interrupt that returns nil changed the solve: %v", err)
		}
		assertSelected(t, got, map[string]int64{"root": 1, "q": 1, "p": 2})
		assertSelected(t, want, map[string]int64{"root": 1, "q": 1, "p": 2})
		if plain.Stats != hooked.Stats {
			t.Errorf("n=%d: stats differ: %+v vs %+v", n, plain.Stats, hooked.Stats)
		}
	}
}
