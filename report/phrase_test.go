// SPDX-License-Identifier: Apache-2.0 OR MIT

package report_test

import (
	"strings"
	"testing"

	"github.com/posit-dev/go-pubgrub/report"
	"github.com/posit-dev/go-pubgrub/solver"
	"github.com/posit-dev/go-pubgrub/term"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// TestConclusionPhrasing covers the CONCLUSION wordings, for term shapes a real solve
// does not happen to produce.
//
// # Why this can be a table when the fixtures cannot
//
// EXTERNAL incompatibilities are hand-buildable: NewIncompatibility is exported. Only
// DERIVED ones are not, because newDerived is unexported — the constraint that forces
// report_test.go's fixtures to come from real solves. That constraint says nothing about
// facts, so a multi-term incompatibility can be handed straight to Explain here.
//
// # What this path can and cannot see
//
// A lone external incompatibility IS the terminal failure, so it is worded by
// rootFailure, which shortcuts the single-positive-term shape to "the requirements of X
// cannot be satisfied" and otherwise falls through to conclusion. So this table can
// exercise every MULTI-term conclusion, but not the single-term ones and not fact() —
// those need a real proof with the fact as a non-root leaf, which is
// TestFactPhrasingFromRealSolves below.
func TestConclusionPhrasing(t *testing.T) {
	oneOnly := versionset.Exactly(1)
	fromTwo := versionset.AtLeast(2)

	for _, tc := range []struct {
		name string
		inc  *solver.Incompatibility[string, set]
		want string
	}{
		{
			// A Kind whose shape does not match what that Kind means. The generic reading
			// has to take over rather than mis-describing it.
			name: "a dependency-labelled fact with two positive terms",
			inc: solver.NewIncompatibility(solver.KindDependency, map[string]term.Term[set]{
				"alpha": term.Positive(oneOnly),
				"bravo": term.Positive(fromTwo),
			}),
			want: "alpha 1 and bravo >=2 cannot both be selected",
		},
		{
			name: "an unlabelled fact falls back to the generic reading",
			inc: solver.NewIncompatibility(solver.KindDerived, map[string]term.Term[set]{
				"alpha": term.Positive(oneOnly),
				"bravo": term.Negative(fromTwo),
			}),
			want: "alpha 1 requires bravo >=2",
		},
		{
			name: "three positive terms",
			inc: solver.NewIncompatibility(solver.KindDerived, map[string]term.Term[set]{
				"alpha":   term.Positive(oneOnly),
				"bravo":   term.Positive(fromTwo),
				"charlie": term.Positive(oneOnly),
			}),
			want: "alpha 1, bravo >=2 and charlie 1 cannot all be selected together",
		},
		{
			name: "a lone negative term is a requirement",
			inc: solver.NewIncompatibility(solver.KindDerived, map[string]term.Term[set]{
				"alpha": term.Negative(fromTwo),
			}),
			want: "alpha >=2 is required",
		},
		{
			name: "two negative terms are alternatives",
			inc: solver.NewIncompatibility(solver.KindDerived, map[string]term.Term[set]{
				"alpha": term.Negative(fromTwo),
				"bravo": term.Negative(oneOnly),
			}),
			want: "one of alpha >=2 and bravo 1 is required",
		},
		{
			name: "two positive terms requiring two others",
			inc: solver.NewIncompatibility(solver.KindDerived, map[string]term.Term[set]{
				"alpha":   term.Positive(oneOnly),
				"bravo":   term.Positive(fromTwo),
				"charlie": term.Negative(oneOnly),
				"delta":   term.Negative(fromTwo),
			}),
			want: "alpha 1 and bravo >=2 require charlie 1 and delta >=2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A single external incompatibility as the root cause renders as one line
			// stating it, which is the shortest way to observe the phraser through the
			// public API.
			got := report.Explain(tc.inc, nil).String()

			if !strings.Contains(got, tc.want) {
				t.Errorf("got %q,\nwant it to contain %q", got, tc.want)
			}
		})
	}
}

// TestFactPhrasingFromRealSolves covers the fact() wordings that need the fact to be a
// non-root leaf of a real proof.
//
// fact() is unreachable with a hand-built incompatibility, because a lone external
// incompatibility is the terminal failure and gets the terminal wording instead. So each
// case here provokes a real solve that puts the fact in question inside the proof.
func TestFactPhrasingFromRealSolves(t *testing.T) {
	t.Run("a caller-seeded unavailable range", func(t *testing.T) {
		// What an adapter does for a yanked or platform-incompatible release: state a fact
		// the Provider interface cannot express. The solver never builds this Kind itself,
		// so without seeding one it is dead in every test.
		unavailable := solver.NewIncompatibility(solver.KindUnavailable, map[string]term.Term[set]{
			"bravo": term.Positive(versionset.AtLeast(1)),
		})

		u := newUniverse().
			with("root", 1, requires("alpha", versionset.AtLeast(1))).
			with("alpha", 1, requires("bravo", versionset.AtLeast(1))).
			with("bravo", 1)

		got := report.Explain(mustFailWithFacts(t, u, "root", 1, unavailable), nil).String()
		if want := "bravo >=1 cannot be used"; !strings.Contains(got, want) {
			t.Errorf("got:\n%s\nwant it to contain %q", got, want)
		}
	})

	t.Run("a package with no versions at all", func(t *testing.T) {
		// alpha requires bravo with no constraint, and bravo has nothing published — so the
		// no-versions fact covers every version rather than a range.
		u := newUniverse().
			with("root", 1, requires("alpha", versionset.AtLeast(1))).
			with("alpha", 1, requires("bravo", versionset.All()))

		got := report.Explain(mustFail(t, u, "root", 1), nil).String()
		if want := "bravo has no versions"; !strings.Contains(got, want) {
			t.Errorf("got:\n%s\nwant it to contain %q", got, want)
		}
	})

	t.Run("a dependency shared by every version of the depender", func(t *testing.T) {
		// A spanning depender is the only way an incompatibility holds a POSITIVE term over
		// an unbounded range, which is the polarity where "every version of X" is the right
		// reading. The opposite polarity is the unconstrained fixture.
		u := newUniverse().
			with("root", 1, requires("alpha", versionset.AtLeast(1))).
			with("alpha", 1, spanning("bravo", versionset.AtLeast(9), versionset.All())).
			with("alpha", 2, spanning("bravo", versionset.AtLeast(9), versionset.All())).
			with("bravo", 1)

		got := report.Explain(mustFail(t, u, "root", 1), nil).String()
		if want := "every version of alpha depends on bravo >=9"; !strings.Contains(got, want) {
			t.Errorf("got:\n%s\nwant it to contain %q", got, want)
		}
	})
}

// TestPhrasingIsDeterministicForEveryShape re-renders each table shape, since these are
// the multi-term incompatibilities where clause order is decided by sorting a map.
func TestPhrasingIsDeterministicForEveryShape(t *testing.T) {
	inc := solver.NewIncompatibility(solver.KindDerived, map[string]term.Term[set]{
		"alpha":   term.Positive(versionset.Exactly(1)),
		"bravo":   term.Positive(versionset.AtLeast(2)),
		"charlie": term.Negative(versionset.Exactly(1)),
		"delta":   term.Negative(versionset.AtLeast(2)),
		"echo":    term.Negative(versionset.LessThan(9)),
	})

	first := report.Explain(inc, nil).String()
	for i := 0; i < 100; i++ {
		if again := report.Explain(inc, nil).String(); again != first {
			t.Fatalf("run %d differs:\n%s\nvs\n%s", i, again, first)
		}
	}
}
