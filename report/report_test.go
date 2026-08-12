// SPDX-License-Identifier: Apache-2.0 OR MIT

package report_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/posit-dev/go-pubgrub/report"
	"github.com/posit-dev/go-pubgrub/solver"
)

// TestExplainGoldens freezes the rendered prose for each fixture.
//
// The goldens are here to catch drift, not to define correctness: §9 is explicit
// that the English is a matter of taste. What they are actually load-bearing for is
// the STRUCTURE the wording reveals — which conclusions got numbers, where the
// visual break landed, which derivations were collapsed into a neighbouring
// sentence. Rewording is fine; changing those without meaning to is the regression.
func TestExplainGoldens(t *testing.T) {
	goldens := map[string]string{
		"chain": `Because no version of a matches >=2, a 1 depends on b >=2 and no version of b matches >=2, a >=1 cannot be used.
So, because root 1 depends on a >=1, the requirements of root cannot be satisfied.`,

		"two-way": `Because no version of c matches >=2, c 1 depends on b 2 and a 1 depends on b 1, a 1 and c >=1 cannot both be selected.
And because no version of a matches >=2, a >=1 and c >=1 cannot both be selected.
And because root 1 depends on a >=1, c >=1 and root 1 cannot both be selected.
So, because root 1 depends on c >=1, the requirements of root cannot be satisfied.`,

		"diamond": `Because no version of right matches >=3 and right 1 depends on shared [5,7), right 1 ∪ >=3 requires shared [5,7). (1)
And because no version of left matches >=3, left 1 depends on shared [1,3) and right 1 ∪ >=3 requires shared [5,7) (1), left 1 ∪ >=3 and right 1 ∪ >=3 cannot both be selected.
And because left 2 depends on shared >=10, left >=1 and right 1 ∪ >=3 require shared >=10.
And because right 2 depends on shared >=10, left >=1 and right >=1 require shared >=10.
And because no version of shared matches >=10, left >=1 and right >=1 cannot both be selected.
And because root 1 depends on left >=1, right >=1 and root 1 cannot both be selected.
So, because root 1 depends on right >=1, the requirements of root cannot be satisfied.`,

		"reused": `Because no version of p4 matches <1 and p4 1 depends on p2 5, p4 <2 requires p2 5. (1)
And because p2 2 depends on p4 1, p2 2 cannot be used.
And because no version of p2 matches >=4, p2 2 ∪ >=4 cannot be used. (2)

Because p3 1 depends on p4 <2 and p4 <2 requires p2 5 (1), p3 1 requires p2 5.
And because p2 3 depends on p3 1, p2 3 cannot be used.
And because p2 2 ∪ >=4 cannot be used (2), p2 >=2 cannot be used.
So, because root 1 depends on p2 >=2, the requirements of root cannot be satisfied.`,
	}

	for name, build := range allFixtures() {
		t.Run(name, func(t *testing.T) {
			got := report.Explain(mustFail(t, build(), "root", 1), nil).String()
			want, ok := goldens[name]
			if !ok {
				t.Fatalf("no golden for fixture %q; every fixture needs one", name)
			}
			if got != want {
				t.Errorf("explanation drifted.\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// TestShapeOfEachFixture asserts each fixture still exercises the §9 shape it was
// chosen for.
//
// Without this, a well-meaning edit to a universe can quietly move it to a
// different shape, the goldens get updated to match, and coverage of the shape
// disappears with every test still green. That applies most to "reused": the shape
// is rare, and nothing about the universe advertises it.
func TestShapeOfEachFixture(t *testing.T) {
	for name, want := range map[string]struct {
		bothExternal  bool
		oneDerived    bool
		bothDerived   bool
		reusedDerived bool
	}{
		"chain":   {bothExternal: true, oneDerived: true},
		"two-way": {bothExternal: true, oneDerived: true},
		"diamond": {bothExternal: true, oneDerived: true, bothDerived: true},
		"reused":  {bothExternal: true, oneDerived: true, reusedDerived: true},
	} {
		t.Run(name, func(t *testing.T) {
			root := mustFail(t, allFixtures()[name](), "root", 1)
			got := shapesIn(root)

			if want.bothExternal && !got.bothExternal {
				t.Errorf("no node with two external causes; §9's base case is uncovered")
			}
			if want.oneDerived && !got.oneDerived {
				t.Errorf("no node with one derived and one external cause")
			}
			if want.bothDerived && !got.bothDerived {
				t.Errorf("no node with two derived causes; §9's hardest branch is uncovered")
			}
			if want.reusedDerived && !got.reusedDerived {
				t.Errorf("no derived node is cited twice, so line numbering is uncovered — " +
					"this fixture was found by search precisely because that shape is rare")
			}
		})
	}
}

// TestEveryExternalFactIsStated checks that no leaf of the derivation graph is
// silently dropped.
//
// A leaf is a fact about real packages, and the proof is only a proof if all of
// them appear. §9's compressions skip stating intermediate CONCLUSIONS, which is
// deliberate, but skipping a premise would make the explanation unsound rather than
// merely terse.
func TestEveryExternalFactIsStated(t *testing.T) {
	for name, build := range allFixtures() {
		t.Run(name, func(t *testing.T) {
			root := mustFail(t, build(), "root", 1)
			rendered := report.Explain(root, nil)

			for _, leaf := range leavesOf(root) {
				for _, pkg := range leaf.Packages() {
					term, _ := leaf.Term(pkg)
					set := term.Set().String()

					if !someLineMentions(rendered, pkg, set) {
						t.Errorf("external fact %v is never stated: no line mentions both %q and %q",
							leaf, pkg, set)
					}
				}
			}
		})
	}
}

// TestCitationsResolveBackwards checks the two things a numbered line has to get
// right: every citation points at a line that exists and has already been read, and
// every number assigned is actually cited.
//
// The backwards direction matters because a forward reference is unreadable — the
// reader meets "(2)" before anything has been labelled 2. The other direction
// catches noise: a number nothing cites is clutter, and it also means the walk
// labelled a line for a reason that turned out not to apply.
//
// Note this deliberately does NOT assert that a number implies the node is cited
// twice in the graph. §9 also assigns one when it interrupts the flow to describe a
// simple cause inline, which the diamond fixture does.
func TestCitationsResolveBackwards(t *testing.T) {
	citation := regexp.MustCompile(`\((\d+)\)`)

	for name, build := range allFixtures() {
		t.Run(name, func(t *testing.T) {
			rendered := report.Explain(mustFail(t, build(), "root", 1), nil)

			definedAt := map[int]int{}
			for i, line := range rendered.Lines {
				if line.Number == 0 {
					continue
				}
				if previous, clash := definedAt[line.Number]; clash {
					t.Errorf("line %d reuses number %d, already used by line %d",
						i, line.Number, previous)
				}
				definedAt[line.Number] = i
			}

			citedSomewhere := map[int]bool{}
			for i, line := range rendered.Lines {
				for _, match := range citation.FindAllStringSubmatch(line.Text, -1) {
					number, err := strconv.Atoi(match[1])
					if err != nil {
						continue
					}
					citedSomewhere[number] = true

					source, defined := definedAt[number]
					switch {
					case !defined:
						t.Errorf("line %d cites (%d), which no line defines: %q", i, number, line.Text)
					case source >= i:
						t.Errorf("line %d cites (%d), defined later at line %d — a forward reference",
							i, number, source)
					}
				}
			}

			for number, i := range definedAt {
				if !citedSomewhere[number] {
					t.Errorf("line %d is labelled (%d) but nothing cites it", i, number)
				}
			}
		})
	}
}

// TestExplainIsDeterministic renders the same failure repeatedly.
//
// Incompatibilities hold their terms in a map, and Go randomizes map iteration per
// range, so any sentence built by walking one without sorting first would differ
// between runs. That would show up as a flaky golden long after the cause was
// forgotten, so it is worth pinning directly.
func TestExplainIsDeterministic(t *testing.T) {
	for name, build := range allFixtures() {
		t.Run(name, func(t *testing.T) {
			first := report.Explain(mustFail(t, build(), "root", 1), nil).String()
			for i := 0; i < 50; i++ {
				again := report.Explain(mustFail(t, build(), "root", 1), nil).String()
				if again != first {
					t.Fatalf("run %d differs from the first:\n--- run %d ---\n%s\n--- first ---\n%s",
						i, i, again, first)
				}
			}
		})
	}
}

// TestLinesAreWellFormed checks the mechanical properties of every line.
func TestLinesAreWellFormed(t *testing.T) {
	for name, build := range allFixtures() {
		t.Run(name, func(t *testing.T) {
			rendered := report.Explain(mustFail(t, build(), "root", 1), nil)

			if len(rendered.Lines) == 0 {
				t.Fatal("no lines: a failed solve always has a proof to explain")
			}
			if rendered.Lines[0].Break {
				t.Error("the first line asks for a blank line before it")
			}

			for i, line := range rendered.Lines {
				if strings.TrimSpace(line.Text) == "" {
					t.Errorf("line %d is empty", i)
				}
				if !strings.HasSuffix(line.Text, ".") {
					t.Errorf("line %d does not end in a period: %q", i, line.Text)
				}
				if first := line.Text[:1]; first != strings.ToUpper(first) {
					t.Errorf("line %d does not start with a capital: %q", i, line.Text)
				}
			}

			last := rendered.Lines[len(rendered.Lines)-1].Text
			if !strings.HasPrefix(last, "So,") {
				t.Errorf("the last line should read as the payoff, not as another step: %q", last)
			}
		})
	}
}

// TestExplainNilRootCause covers the caller who passes a nil root cause.
//
// An explanation is what a user sees when something has already gone wrong, so it
// is the last place that should panic.
func TestExplainNilRootCause(t *testing.T) {
	rendered := report.Explain[string, set](nil, nil)

	if len(rendered.Lines) != 1 {
		t.Fatalf("got %d lines, want exactly 1", len(rendered.Lines))
	}
	if !strings.Contains(rendered.String(), "no root cause") {
		t.Errorf("the line should say what went wrong: %q", rendered.String())
	}
}

// TestFormatterOverridesNaming checks that an adapter can impose its own
// vocabulary, which is the whole reason Formatter exists: go-pubgrub is not allowed
// to know how any ecosystem spells a version range.
func TestFormatterOverridesNaming(t *testing.T) {
	rendered := report.Explain[string, set](mustFail(t, chainUniverse(), "root", 1), shoutingFormatter{})
	text := rendered.String()

	if !strings.Contains(text, "PKG:a") {
		t.Errorf("the formatter's package naming was not used: %q", text)
	}
	if !strings.Contains(text, "SET:>=2") {
		t.Errorf("the formatter's set naming was not used: %q", text)
	}
	if strings.Contains(text, "no version of a matches") {
		t.Errorf("the default naming leaked through: %q", text)
	}
}

// shoutingFormatter is deliberately unlike anything the default would produce, so a
// test cannot pass by accident when the Formatter is ignored.
type shoutingFormatter struct{}

func (shoutingFormatter) Package(pkg string) string { return "PKG:" + pkg }
func (shoutingFormatter) Set(s set) string          { return "SET:" + s.String() }

// TestEmptyRootCauseFailsOutright covers §4's other failure termination: an
// incompatibility with no terms at all, which is the formal statement that nothing
// can be selected.
func TestEmptyRootCauseFailsOutright(t *testing.T) {
	empty := solver.NewIncompatibility[string, set](solver.KindDerived, nil)

	got := report.Explain(empty, nil).String()
	if !strings.Contains(got, "version solving has failed") {
		t.Errorf("an empty root cause should say solving failed outright, got %q", got)
	}
}

// --- helpers over the derivation graph -------------------------------------

// shapes records which of §9's cause shapes appear in a proof.
type shapes struct {
	bothExternal  bool
	oneDerived    bool
	bothDerived   bool
	reusedDerived bool
}

// shapesIn walks the graph and reports which shapes it contains.
func shapesIn(root *solver.Incompatibility[string, set]) shapes {
	var found shapes
	cites := map[*solver.Incompatibility[string, set]]int{}
	seen := map[*solver.Incompatibility[string, set]]bool{}

	var walk func(inc *solver.Incompatibility[string, set])
	walk = func(inc *solver.Incompatibility[string, set]) {
		if seen[inc] {
			return
		}
		seen[inc] = true

		a, b, derived := inc.Causes()
		if !derived {
			return
		}
		cites[a]++
		if b != a {
			cites[b]++
		}

		switch {
		case a.IsDerived() && b.IsDerived():
			found.bothDerived = true
		case a.IsDerived() != b.IsDerived():
			found.oneDerived = true
		default:
			found.bothExternal = true
		}

		walk(a)
		walk(b)
	}
	walk(root)

	for inc, n := range cites {
		if n >= 2 && inc.IsDerived() {
			found.reusedDerived = true
		}
	}
	return found
}

// leavesOf returns the external incompatibilities the proof rests on.
func leavesOf(root *solver.Incompatibility[string, set]) []*solver.Incompatibility[string, set] {
	var leaves []*solver.Incompatibility[string, set]
	seen := map[*solver.Incompatibility[string, set]]bool{}

	var walk func(inc *solver.Incompatibility[string, set])
	walk = func(inc *solver.Incompatibility[string, set]) {
		if seen[inc] {
			return
		}
		seen[inc] = true

		a, b, derived := inc.Causes()
		if !derived {
			leaves = append(leaves, inc)
			return
		}
		walk(a)
		walk(b)
	}
	walk(root)

	return leaves
}

// someLineMentions reports whether one line names both a package and a version set,
// which is the observable trace of a fact having been stated.
func someLineMentions(r *report.Report, pkg string, set string) bool {
	for _, line := range r.Lines {
		if strings.Contains(line.Text, pkg) && strings.Contains(line.Text, set) {
			return true
		}
	}
	return false
}
