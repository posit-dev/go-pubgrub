// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"fmt"
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// selected solves u and returns the chosen versions, one line per package.
func selected(t *testing.T, u *universe, configure func(*Solver[string, set])) string {
	t.Helper()
	s := New("root", versionset.Exactly(1), newLogProvider(u))
	if configure != nil {
		configure(s)
	}
	sol, err := s.Solve()
	if err != nil {
		t.Fatalf("solve failed: %v", err)
	}
	return fmt.Sprint(sol.Selected)
}

// All three settings on together pick the same versions as all off. (Fixture A
// is left out: its answer legitimately changes with the decision order.)
func TestAllSettingsTogetherKeepTheSelection(t *testing.T) {
	fixtures := map[string]func() *universe{"C": fixtureC, "D": fixtureD, "E": fixtureE}
	for name, f := range fixtures {
		want := selected(t, f(), nil)
		for _, threshold := range []int{1, 2} {
			for _, recheck := range []bool{false, true} {
				got := selected(t, f(), func(s *Solver[string, set]) {
					s.MergeDependencies = true
					s.RunningSatisfier = true
					s.ClashThreshold = threshold
					s.recheck = recheck
				})
				if got != want {
					t.Errorf("%s T=%d recheck=%v: got %s, want %s", name, threshold, recheck, got, want)
				}
			}
		}
	}
}
