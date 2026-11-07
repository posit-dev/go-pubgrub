// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// askLog wraps a Provider and records each Candidates call whose allowed set
// equals the one from that package's previous call.
type askLog struct {
	Provider[string, set]
	last    map[string]set
	asks    map[string]int
	repeats []string
}

func newAskLog(p Provider[string, set]) *askLog {
	return &askLog{Provider: p, last: map[string]set{}, asks: map[string]int{}}
}

func (l *askLog) Candidates(pkg string, allowed set) (set, bool, int, error) {
	if prev, ok := l.last[pkg]; ok && prev.Equal(allowed) {
		l.repeats = append(l.repeats, pkg)
	}
	l.last[pkg] = allowed
	l.asks[pkg]++
	return l.Provider.Candidates(pkg, allowed)
}

// TestCandidatesIsNotAskedAgainForAnUnchangedAllowedSet checks the solver asks
// about a package only when its allowed set has changed since the last ask.
func TestCandidatesIsNotAskedAgainForAnUnchangedAllowedSet(t *testing.T) {
	u := newUniverse().
		with("root", 1,
			requirement("a", versionset.AtLeast(1)),
			requirement("b", versionset.AtLeast(1)),
			requirement("c", versionset.AtLeast(1))).
		with("a", 1).with("a", 2).
		with("b", 1).with("b", 2).with("b", 3).
		with("c", 1).with("c", 2).with("c", 3).with("c", 4)

	log := newAskLog(u)
	sol, err := New("root", versionset.Exactly(1), log).Solve()
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	assertSelected(t, sol, map[string]int64{"root": 1, "a": 2, "b": 3, "c": 4})

	if len(log.repeats) != 0 {
		t.Errorf("Candidates re-asked with an unchanged allowed set for %v", log.repeats)
	}
	// Each package is eligible for several rounds; one ask each is enough.
	for _, pkg := range []string{"a", "b", "c"} {
		if log.asks[pkg] != 1 {
			t.Errorf("Candidates(%s) called %d times, want 1", pkg, log.asks[pkg])
		}
	}
}

// TestCandidatesAnswerIsNotServedAfterAllowedChanges checks a changed allowed
// set is asked afresh, including when it changes back to an earlier set.
func TestCandidatesAnswerIsNotServedAfterAllowedChanges(t *testing.T) {
	u := newUniverse().
		with("root", 1,
			requirement("left", versionset.AtLeast(1)),
			requirement("mid", versionset.AtLeast(1)),
			requirement("shared", versionset.LessThan(10))).
		with("left", 1).
		with("mid", 1).
		with("mid", 2, requirement("shared", versionset.AtLeast(10))).
		with("shared", 1).with("shared", 20)

	log := newAskLog(u)
	sol, err := New("root", versionset.Exactly(1), log).Solve()
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	// shared's allowed set narrows and is then restored by the backjump; the answer
	// must still be the one for the set in force.
	assertSelected(t, sol, map[string]int64{"root": 1, "left": 1, "mid": 1, "shared": 1})
	if len(log.repeats) != 0 {
		t.Errorf("Candidates re-asked with an unchanged allowed set for %v", log.repeats)
	}
}
