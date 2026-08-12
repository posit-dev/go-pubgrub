// SPDX-License-Identifier: Apache-2.0 OR MIT

package report

import (
	"sort"
	"strings"

	"github.com/posit-dev/go-pubgrub/solver"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// phraser turns one incompatibility into one clause of English.
//
// It holds no state beyond the Formatter: every sentence is a function of the
// incompatibility alone, which is what lets §9's walk decide freely where a
// sentence goes without also having to decide what it says.
type phraser[P comparable, S versionset.Set[S]] struct {
	fmtr Formatter[P, S]
}

// describe renders "name range", collapsing an all-encompassing range to "every
// version of name" as §9's framing conventions prefer.
//
// Whether a range is all-encompassing is decided through the versionset algebra
// (a set whose complement is empty), not by asking the ecosystem — go-pubgrub has
// no way to recognize "*" or ">=0" and is not allowed to learn one.
func (p *phraser[P, S]) describe(pkg P, s S) string {
	name := p.fmtr.Package(pkg)
	if s.Complement().IsEmpty() {
		return "every version of " + name
	}
	return name + " " + p.fmtr.Set(s)
}

// sortedTerms returns the incompatibility's terms ordered by formatted package
// name, so a sentence built from a map is stable across runs.
func (p *phraser[P, S]) sortedTerms(inc *solver.Incompatibility[P, S]) []P {
	pkgs := inc.Packages()
	sort.Slice(pkgs, func(i, j int) bool {
		return p.fmtr.Package(pkgs[i]) < p.fmtr.Package(pkgs[j])
	})
	return pkgs
}

// split separates an incompatibility's terms into the packages it asserts a range
// FOR (positive) and the packages it asserts a range AGAINST (negative).
//
// The distinction carries the whole meaning: a dependency is a positive term on
// the depender plus a negative term on what it needs, so positives read as "this
// is selected" and negatives read as "and this is what it then requires".
func (p *phraser[P, S]) split(inc *solver.Incompatibility[P, S]) (positive, negative []P) {
	for _, pkg := range p.sortedTerms(inc) {
		t, _ := inc.Term(pkg)
		if t.IsPositive() {
			positive = append(positive, pkg)
		} else {
			negative = append(negative, pkg)
		}
	}
	return positive, negative
}

// list joins clauses as English rather than as a comma-separated machine list.
func list(clauses []string) string {
	switch len(clauses) {
	case 0:
		return ""
	case 1:
		return clauses[0]
	case 2:
		return clauses[0] + " and " + clauses[1]
	default:
		return strings.Join(clauses[:len(clauses)-1], ", ") + " and " + clauses[len(clauses)-1]
	}
}

// fact states an external incompatibility — a leaf of the derivation graph, and so
// something known about real packages rather than anything the solver concluded.
//
// Phrasing is chosen by Kind, which is what Kind is for. Kind cannot be trusted to
// say whether a node is derived (KindDerived is the zero value, so an unlabeled
// external fact reports it), but the walk has already established derivedness from
// causes by the time this is called; here Kind is only being asked how to word a
// fact that is known to be one. Every case also checks the SHAPE it expects and
// falls back to the generic wording if the incompatibility does not have it, since
// a caller may seed facts of its own with any terms it likes.
func (p *phraser[P, S]) fact(inc *solver.Incompatibility[P, S]) string {
	positive, negative := p.split(inc)

	switch inc.Kind() {
	case solver.KindDependency:
		if len(positive) == 1 && len(negative) == 1 {
			dependerTerm, _ := inc.Term(positive[0])
			neededTerm, _ := inc.Term(negative[0])
			return p.describe(positive[0], dependerTerm.Set()) +
				" depends on " + p.describe(negative[0], neededTerm.Set())
		}

	case solver.KindNoVersions:
		if len(positive) == 1 && len(negative) == 0 {
			t, _ := inc.Term(positive[0])
			if t.Set().Complement().IsEmpty() {
				return p.fmtr.Package(positive[0]) + " has no versions"
			}
			return "no version of " + p.fmtr.Package(positive[0]) + " matches " + p.fmtr.Set(t.Set())
		}

	case solver.KindUnavailable:
		if pkg, ok := p.onlyPackage(inc); ok {
			t, _ := inc.Term(pkg)
			return p.describe(pkg, t.Set()) + " cannot be used"
		}

	case solver.KindRoot:
		// §9's convention: name the root package without a version, since it only
		// ever has one and stating it adds nothing a reader needs.
		if pkg, ok := p.onlyPackage(inc); ok {
			return p.fmtr.Package(pkg) + " is the package being resolved"
		}

	case solver.KindDerived:
		// An external fact a caller built without naming a kind. Nothing to add
		// beyond what its terms already say.
	}

	return p.conclusion(inc)
}

// conclusion states what an incompatibility rules out, in the voice of something
// worked out rather than something looked up.
//
// The reading is uniform across every shape, which is why it can also serve as the
// fallback wording for an external fact whose Kind does not match its terms:
// positive terms are what would have to be selected, negative terms are what that
// selection would then require, and the incompatibility says those cannot hold
// together.
func (p *phraser[P, S]) conclusion(inc *solver.Incompatibility[P, S]) string {
	if inc.IsEmpty() {
		return "version solving has failed"
	}

	positive, negative := p.split(inc)

	positiveClauses := make([]string, 0, len(positive))
	for _, pkg := range positive {
		t, _ := inc.Term(pkg)
		positiveClauses = append(positiveClauses, p.describe(pkg, t.Set()))
	}
	negativeClauses := make([]string, 0, len(negative))
	for _, pkg := range negative {
		t, _ := inc.Term(pkg)
		negativeClauses = append(negativeClauses, p.describe(pkg, t.Set()))
	}

	switch {
	case len(negative) == 0 && len(positive) == 1:
		return p.cannotBeUsed(inc, positive[0])
	case len(negative) == 0 && len(positive) == 2:
		return list(positiveClauses) + " cannot both be selected"
	case len(negative) == 0:
		return list(positiveClauses) + " cannot all be selected together"
	case len(positive) == 0 && len(negative) == 1:
		return negativeClauses[0] + " is required"
	case len(positive) == 0:
		return "one of " + list(negativeClauses) + " is required"
	case len(positive) == 1:
		return positiveClauses[0] + " requires " + list(negativeClauses)
	default:
		return list(positiveClauses) + " require " + list(negativeClauses)
	}
}

// cannotBeUsed words the single-positive-term case, which is the commonest
// intermediate conclusion.
func (p *phraser[P, S]) cannotBeUsed(inc *solver.Incompatibility[P, S], pkg P) string {
	t, _ := inc.Term(pkg)
	if t.Set().Complement().IsEmpty() {
		return "no version of " + p.fmtr.Package(pkg) + " can be used"
	}
	return p.describe(pkg, t.Set()) + " cannot be used"
}

// rootFailure words the very last line of a report.
//
// §7.4's terminal incompatibility is either empty or a lone positive term about
// the root package, and both mean the same thing to a user: what they asked for
// cannot be built. Saying that outright is more useful than the literal reading of
// the terms, which would be "no version of the root package can be used" — true,
// but it invites the reader to think their own project is at fault rather than the
// requirements they gave it.
func (p *phraser[P, S]) rootFailure(inc *solver.Incompatibility[P, S]) string {
	if inc.IsEmpty() {
		return "version solving has failed"
	}
	positive, negative := p.split(inc)
	if len(positive) == 1 && len(negative) == 0 {
		return "the requirements of " + p.fmtr.Package(positive[0]) + " cannot be satisfied"
	}
	return p.conclusion(inc)
}

// onlyPackage returns the single package an incompatibility mentions, and false if
// it mentions any other number of them.
func (p *phraser[P, S]) onlyPackage(inc *solver.Incompatibility[P, S]) (P, bool) {
	pkgs := p.sortedTerms(inc)
	if len(pkgs) != 1 {
		var zero P
		return zero, false
	}
	return pkgs[0], true
}
