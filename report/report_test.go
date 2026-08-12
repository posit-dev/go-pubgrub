// SPDX-License-Identifier: Apache-2.0 OR MIT

package report_test

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/posit-dev/go-pubgrub/report"
	"github.com/posit-dev/go-pubgrub/solver"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// TestExplainGoldens freezes the rendered prose for each fixture.
//
// # What a golden written this way does and does not prove
//
// These strings were produced by running the code and reading the output, so they
// cannot show that the prose is CORRECT — a wrong sentence gets enshrined just as
// faithfully as a right one. What they do is make any change to wording, ordering,
// numbering, collapsing or break placement visible in review. Treat them as change
// detection, and rely on the invariant tests below plus TestPhrasing for meaning.
//
// This is not a theoretical caveat: an earlier version of this file passed with
// unconstrained dependencies rendered as "depends on every version of bravo", which
// says the opposite of what the fact means. No golden covered it, so nothing failed.
func TestExplainGoldens(t *testing.T) {
	goldens := map[string]string{
		"chain": `Because no version of alpha matches >=2, alpha 1 depends on bravo >=2 and no version of bravo matches >=2, alpha >=1 cannot be used.
So, because root 1 depends on alpha >=1, the requirements of root cannot be satisfied.`,

		"two-way": `Because no version of charlie matches >=2, charlie 1 depends on bravo 2 and alpha 1 depends on bravo 1, alpha 1 and charlie >=1 cannot both be selected.
And because no version of alpha matches >=2, alpha >=1 and charlie >=1 cannot both be selected.
And because root 1 depends on alpha >=1, charlie >=1 and root 1 cannot both be selected.
So, because root 1 depends on charlie >=1, the requirements of root cannot be satisfied.`,

		"diamond": `Because no version of right matches >=3 and right 1 depends on shared [5,7), right 1 ∪ >=3 requires shared [5,7). (1)
And because no version of left matches >=3, left 1 depends on shared [1,3) and right 1 ∪ >=3 requires shared [5,7) (1), left 1 ∪ >=3 and right 1 ∪ >=3 cannot both be selected.
And because left 2 depends on shared >=10, left >=1 and right 1 ∪ >=3 require shared >=10.
And because right 2 depends on shared >=10, left >=1 and right >=1 require shared >=10.
And because no version of shared matches >=10, left >=1 and right >=1 cannot both be selected.
And because root 1 depends on left >=1, right >=1 and root 1 cannot both be selected.
So, because root 1 depends on right >=1, the requirements of root cannot be satisfied.`,

		"reused": `Because no version of echo matches <1 and echo 1 depends on charlie 5, echo <2 requires charlie 5. (1)
And because charlie 2 depends on echo 1, charlie 2 cannot be used.
And because no version of charlie matches >=4, charlie 2 ∪ >=4 cannot be used. (2)

Because delta 1 depends on echo <2 and echo <2 requires charlie 5 (1), delta 1 requires charlie 5.
And because charlie 3 depends on delta 1, charlie 3 cannot be used.
And because charlie 2 ∪ >=4 cannot be used (2), charlie >=2 cannot be used.
So, because root 1 depends on charlie >=2, the requirements of root cannot be satisfied.`,

		// "depends on bravo", NOT "depends on every version of bravo".
		"unconstrained": `Because no version of alpha matches >=2, alpha 1 depends on bravo and no version of bravo matches <1 ∪ >=2, alpha >=1 requires bravo 1.
And because bravo 1 depends on charlie >=9, alpha >=1 requires charlie >=9.
And because no version of charlie matches >=9, alpha >=1 cannot be used.
So, because root 1 depends on alpha >=1, the requirements of root cannot be satisfied.`,

		// The terminal wording, not the literal reading of the external fact.
		"external-root": `So, the requirements of root cannot be satisfied.`,
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
// Without this, a well-meaning edit to a universe can quietly move it to a different
// shape, the goldens get updated to match, and coverage of the shape disappears with
// every test still green. That applies most to "reused": the shape is rare, and
// nothing about the universe advertises it.
//
// It asserts presence, not absence, so a fixture gaining a shape is not a failure.
func TestShapeOfEachFixture(t *testing.T) {
	for name, want := range map[string]shapes{
		"chain":         {bothExternal: true, oneDerived: true},
		"two-way":       {bothExternal: true, oneDerived: true},
		"diamond":       {bothExternal: true, oneDerived: true, bothDerived: true},
		"reused":        {bothExternal: true, oneDerived: true, bothDerived: true, reusedDerived: true},
		"unconstrained": {bothExternal: true, oneDerived: true},
		"external-root": {},
	} {
		t.Run(name, func(t *testing.T) {
			got := shapesIn(mustFail(t, allFixtures()[name](), "root", 1))

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
// A leaf is a fact about real packages, and the proof is only a proof if all of them
// appear. §9's compressions skip stating intermediate CONCLUSIONS, which is
// deliberate, but skipping a premise would make the explanation unsound rather than
// merely terse.
//
// # Two things this checks that the obvious version does not
//
// It requires all of a leaf's terms on ONE line, not merely somewhere in the report,
// so two halves of a fact cannot be satisfied by two unrelated sentences. And for a
// dependency it asserts the exact "<depender> depends on <needed>" clause, because a
// check that only looks for co-occurrence passes just as happily when the direction is
// inverted — which is the one error in this area that would mislead rather than
// confuse.
func TestEveryExternalFactIsStated(t *testing.T) {
	for name, build := range allFixtures() {
		t.Run(name, func(t *testing.T) {
			root := mustFail(t, build(), "root", 1)
			rendered := report.Explain(root, nil)

			for _, leaf := range leavesOf(root) {
				if leaf == root {
					// The terminal line states the failure, not the fact: an external root
					// cause is deliberately worded as "the requirements of X cannot be
					// satisfied" rather than as its own terms. TestExplainGoldens covers it.
					continue
				}

				if leaf.Kind() == solver.KindDependency {
					// Look for the exact clause across every line rather than for a line that
					// merely mentions the right tokens. Token co-occurrence finds the wrong
					// line: in the two-way fixture, "root 1 depends on alpha >=1, charlie >=1
					// and root 1 ..." contains every token of the UNRELATED fact "root depends
					// on charlie >=1", so a co-occurrence check would test the wrong sentence
					// and then report a direction error that is not there.
					if want := expectedDependencyClause(leaf); !anyLineContains(rendered, want) {
						t.Errorf("dependency %v is stated nowhere as %q — the direction or the "+
							"range is wrong in:\n%s", leaf, want, rendered)
					}
					continue
				}

				if _, ok := lineStatingAll(rendered, leaf); !ok {
					t.Errorf("external fact %v is never stated on any single line", leaf)
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
// reader meets "(2)" before anything has been labelled 2. The other direction catches
// noise: a number nothing cites is clutter, and it also means the walk labelled a line
// for a reason that turned out not to apply.
//
// It also checks Cites agrees with the prose, since a consumer trusting the integers
// while a human reads the sentence must not be told two different stories.
//
// Note this deliberately does NOT assert that a number implies the node is cited twice
// in the graph. §9 also assigns one when it interrupts the flow to describe a simple
// cause inline, which the diamond fixture does.
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
				inProse := []int{}
				for _, match := range citation.FindAllStringSubmatch(line.Text, -1) {
					number, err := strconv.Atoi(match[1])
					if err != nil {
						continue
					}
					inProse = append(inProse, number)
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

				if !equalInts(inProse, line.Cites) {
					t.Errorf("line %d cites %v in its text but reports Cites=%v", i, inProse, line.Cites)
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

// TestExplainIsDeterministic renders the SAME failure repeatedly.
//
// Incompatibilities hold their terms in a map, and Go randomizes map iteration per
// range, so any sentence built by walking one without sorting first would differ
// between runs. That would show up as a flaky golden long after the cause was
// forgotten, so it is worth pinning directly.
//
// The solve happens once, on purpose: re-solving would make a solver regression fail a
// test in report, and report's own map iteration is re-randomized per Explain call
// anyway, so nothing is lost.
func TestExplainIsDeterministic(t *testing.T) {
	for name, build := range allFixtures() {
		t.Run(name, func(t *testing.T) {
			root := mustFail(t, build(), "root", 1)

			first := report.Explain(root, nil).String()
			for i := 0; i < 50; i++ {
				if again := report.Explain(root, nil).String(); again != first {
					t.Fatalf("run %d differs from the first:\n--- run %d ---\n%s\n--- first ---\n%s",
						i, i, again, first)
				}
			}
		})
	}
}

// TestNonInjectiveFormatterIsStable covers a Formatter that maps two distinct packages
// to one name.
//
// Ordering within a sentence is by FORMATTED name, so colliding names leave the order
// of those two clauses to whatever the sort does with equal keys — and map iteration
// order feeds that sort. TestExplainIsDeterministic cannot see it, because it uses
// string keys under the default formatter, where names never collide.
func TestNonInjectiveFormatterIsStable(t *testing.T) {
	root := mustFail(t, diamondUniverse(), "root", 1)

	first := report.Explain[string, set](root, collidingFormatter{}).String()
	for i := 0; i < 100; i++ {
		if again := report.Explain[string, set](root, collidingFormatter{}).String(); again != first {
			t.Fatalf("run %d differs:\n--- run %d ---\n%s\n--- first ---\n%s", i, i, again, first)
		}
	}
}

// collidingFormatter names every package "pkg", which is the degenerate case of a
// formatter that strips a namespace or folds case.
type collidingFormatter struct{}

func (collidingFormatter) Package(string) string { return "pkg" }
func (collidingFormatter) Set(s set) string      { return s.String() }

// TestLinesCarryTheirNode checks a consumer can get from a line back to the packages
// involved without parsing English.
//
// That is the whole reason Line is generic. If this ever stops holding, the only route
// from a report to a package identity is a regex over prose the tests themselves
// describe as a matter of taste.
func TestLinesCarryTheirNode(t *testing.T) {
	root := mustFail(t, chainUniverse(), "root", 1)
	rendered := report.Explain(root, nil)

	seen := map[string]bool{}
	for i, line := range rendered.Lines {
		if line.Node == nil {
			t.Errorf("line %d has no Node: %q", i, line.Text)
			continue
		}
		for _, pkg := range line.Node.Packages() {
			seen[pkg] = true
		}
	}

	for _, want := range []string{"root", "alpha"} {
		if !seen[want] {
			t.Errorf("no line's Node mentions %q; a consumer cannot find it without a regex", want)
		}
	}
	if last := rendered.Lines[len(rendered.Lines)-1]; last.Node != root {
		t.Errorf("the terminal line's Node is not the root cause")
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
				if strings.Contains(line.Text, "(0)") {
					t.Errorf("line %d cites (0), which can never be a line number: %q", i, line.Text)
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
// An explanation is what a user sees when something has already gone wrong, so it is
// the last place that should panic.
func TestExplainNilRootCause(t *testing.T) {
	rendered := report.Explain[string, set](nil, nil)

	if len(rendered.Lines) != 1 {
		t.Fatalf("got %d lines, want exactly 1", len(rendered.Lines))
	}
	if !strings.Contains(rendered.String(), "no root cause") {
		t.Errorf("the line should say what went wrong: %q", rendered.String())
	}
}

// TestFromError covers the path every consumer would otherwise hand-roll, including
// the case that must NOT be shown to a user as a conflict.
func TestFromError(t *testing.T) {
	s := solver.New[string, set]("root", versionset.Exactly(1), chainUniverse())
	_, err := s.Solve()
	if err == nil {
		t.Fatal("the fixture is supposed to fail")
	}

	rendered, ok := report.FromError(err, report.Formatter[string, set](nil))
	if !ok {
		t.Fatalf("a resolution failure was not recognised as one: %v", err)
	}
	if !strings.Contains(rendered.String(), "the requirements of root cannot be satisfied") {
		t.Errorf("unexpected explanation: %q", rendered.String())
	}

	if _, ok := report.FromError(errors.New("index unavailable"), report.Formatter[string, set](nil)); ok {
		t.Error("a provider failure was reported as a resolution conflict; those are different " +
			"things and only one of them is the user's to fix")
	}
}

// TestFormatterOverridesNaming checks that an adapter can impose its own vocabulary,
// which is the whole reason Formatter exists: go-pubgrub is not allowed to know how any
// ecosystem spells a version range.
func TestFormatterOverridesNaming(t *testing.T) {
	rendered := report.Explain[string, set](mustFail(t, chainUniverse(), "root", 1), shoutingFormatter{})
	text := rendered.String()

	if !strings.Contains(text, "PKG:alpha") {
		t.Errorf("the formatter's package naming was not used: %q", text)
	}
	if !strings.Contains(text, "SET:>=2") {
		t.Errorf("the formatter's set naming was not used: %q", text)
	}
	if strings.Contains(text, "no version of alpha matches >=2") {
		t.Errorf("the default naming leaked through: %q", text)
	}
}

// shoutingFormatter is deliberately unlike anything the default would produce, so a
// test cannot pass by accident when the Formatter is ignored.
type shoutingFormatter struct{}

func (shoutingFormatter) Package(pkg string) string { return "PKG:" + pkg }
func (shoutingFormatter) Set(s set) string          { return "SET:" + s.String() }

// TestEmptyRootCauseFailsOutright covers §4's other failure termination: an
// incompatibility with no terms at all, which is the formal statement that nothing can
// be selected.
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

// lineStatingAll finds a single line that mentions every package and every version set
// of the given fact, and returns it.
func lineStatingAll(
	r *report.Report[string, set], fact *solver.Incompatibility[string, set],
) (string, bool) {
	for _, line := range r.Lines {
		if mentionsAll(line.Text, fact) {
			return line.Text, true
		}
	}
	return "", false
}

// anyLineContains reports whether some line contains the given clause verbatim.
func anyLineContains(r *report.Report[string, set], clause string) bool {
	for _, line := range r.Lines {
		if strings.Contains(line.Text, clause) {
			return true
		}
	}
	return false
}

func mentionsAll(text string, fact *solver.Incompatibility[string, set]) bool {
	for _, pkg := range fact.Packages() {
		if !strings.Contains(text, pkg) {
			return false
		}
		t, _ := fact.Term(pkg)
		if t.Set().Complement().IsEmpty() {
			// An unbounded range is rendered as words rather than as a set, so there is no
			// set string to look for.
			continue
		}
		if !strings.Contains(text, t.Set().String()) {
			return false
		}
	}
	return true
}

// expectedDependencyClause restates, independently of the phraser, how a dependency has
// to read.
//
// Duplicating the phrasing is the point: an assertion derived from the same code it
// checks cannot catch that code being wrong. This is the oracle for direction, which
// is why it spells out both sides rather than looking for co-occurrence.
func expectedDependencyClause(fact *solver.Incompatibility[string, set]) string {
	var depender, needed string
	for _, pkg := range fact.Packages() {
		t, _ := fact.Term(pkg)
		if t.IsPositive() {
			depender = describeLikePhraser(pkg, t.Set(), true)
		} else {
			needed = describeLikePhraser(pkg, t.Set(), false)
		}
	}
	return depender + " depends on " + needed
}

func describeLikePhraser(pkg string, s set, positive bool) string {
	if s.Complement().IsEmpty() {
		if positive {
			return "every version of " + pkg
		}
		return pkg
	}
	return pkg + " " + s.String()
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
