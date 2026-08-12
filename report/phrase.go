// SPDX-License-Identifier: Apache-2.0 OR MIT

package report

import (
	"sort"
	"strings"

	"github.com/posit-dev/go-pubgrub/solver"
	"github.com/posit-dev/go-pubgrub/term"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// phraser turns one incompatibility into one clause of English.
//
// It holds no state beyond the Formatter: every sentence is a function of the
// incompatibility alone, which is what lets §9's walk decide freely where a sentence
// goes without also having to decide what it says.
type phraser[P comparable, S versionset.Set[S]] struct {
	fmtr Formatter[P, S]
}

// describe renders "name range" for one term.
//
// # Polarity changes the words, not just the emphasis
//
// An all-encompassing range means opposite things in the two polarities, and §9's
// convention of collapsing it to "every version" is only right for one of them:
//
//   - A POSITIVE term over every version says the package is selected, whichever
//     version that is — "every version of foo" reads correctly.
//   - A NEGATIVE term over every version is how an unconstrained requirement is
//     encoded: a dependency is a negative term on what it needs, so "depends on
//     <this>" wants to read "depends on foo", meaning any version at all. Rendering
//     it as "depends on every version of foo" says foo is needed in all its versions
//     simultaneously, which is a different and impossible claim.
//
// That distinction is not academic. A requirement with no version constraint —
// `Requires-Dist: foo`, `Imports: foo` — is the commonest dependency form in the
// ecosystems this library serves, so the negative case is the one most reports will
// contain.
//
// Whether a range is all-encompassing is decided through the versionset algebra (a
// set whose complement is empty), not by asking the ecosystem: go-pubgrub has no way
// to recognize "*" or ">=0" and is not allowed to learn one.
func (p *phraser[P, S]) describe(pkg P, t term.Term[S]) string {
	name := p.fmtr.Package(pkg)

	if t.Set().Complement().IsEmpty() {
		if t.IsPositive() {
			return "every version of " + name
		}
		return name
	}
	return name + " " + p.fmtr.Set(t.Set())
}

// sortedTerms returns the incompatibility's terms ordered by formatted package name,
// so a sentence built from a map is stable across runs.
//
// The sort is stable and falls back to the formatted SET, because sorting on the
// formatted name alone leaves the order of two distinct packages that format to the
// same name unspecified — see Formatter's note on injectivity. With both keys equal
// the clauses are identical strings, so their order is unobservable.
func (p *phraser[P, S]) sortedTerms(inc *solver.Incompatibility[P, S]) []P {
	pkgs := inc.Packages()

	sort.SliceStable(pkgs, func(i, j int) bool {
		nameI, nameJ := p.fmtr.Package(pkgs[i]), p.fmtr.Package(pkgs[j])
		if nameI != nameJ {
			return nameI < nameJ
		}
		termI, _ := inc.Term(pkgs[i])
		termJ, _ := inc.Term(pkgs[j])
		return p.fmtr.Set(termI.Set()) < p.fmtr.Set(termJ.Set())
	})

	return pkgs
}

// split separates an incompatibility's terms into the packages it asserts a range
// FOR (positive) and the packages it asserts a range AGAINST (negative).
//
// The distinction carries the whole meaning: a dependency is a positive term on the
// depender plus a negative term on what it needs, so positives read as "this is
// selected" and negatives read as "and this is what it then requires".
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

// clauses renders one clause per package, in the given order.
func (p *phraser[P, S]) clauses(inc *solver.Incompatibility[P, S], pkgs []P) []string {
	out := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		t, _ := inc.Term(pkg)
		out = append(out, p.describe(pkg, t))
	}
	return out
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
// falls back to the generic wording if the incompatibility does not have it, since a
// caller may seed facts of its own with any terms it likes.
func (p *phraser[P, S]) fact(inc *solver.Incompatibility[P, S]) string {
	positive, negative := p.split(inc)

	switch inc.Kind() {
	case solver.KindDependency:
		if len(positive) == 1 && len(negative) == 1 {
			dependerTerm, _ := inc.Term(positive[0])
			neededTerm, _ := inc.Term(negative[0])
			return p.describe(positive[0], dependerTerm) +
				" depends on " + p.describe(negative[0], neededTerm)
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
			return p.describe(pkg, t) + " cannot be used"
		}

	case solver.KindRoot:
		// §9's convention: name the root package without a version, since it only ever
		// has one and stating it adds nothing a reader needs.
		if pkg, ok := p.onlyPackage(inc); ok {
			return p.fmtr.Package(pkg) + " is the package being resolved"
		}

	case solver.KindDerived:
		// An external fact a caller built without naming a kind. Nothing to add beyond
		// what its terms already say.
	}

	return p.conclusion(inc)
}

// conclusion states what an incompatibility rules out, in the voice of something
// worked out rather than something looked up.
//
// The reading is uniform across every shape, which is why it can also serve as the
// fallback wording for an external fact whose Kind does not match its terms: positive
// terms are what would have to be selected, negative terms are what that selection
// would then require, and the incompatibility says those cannot hold together.
func (p *phraser[P, S]) conclusion(inc *solver.Incompatibility[P, S]) string {
	if inc.IsEmpty() {
		return "version solving has failed"
	}

	positive, negative := p.split(inc)
	positiveClauses := p.clauses(inc, positive)
	negativeClauses := p.clauses(inc, negative)

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
	return p.describe(pkg, t) + " cannot be used"
}

// rootFailure words the very last line of a report.
//
// §7.4's terminal incompatibility is either empty or a lone positive term about the
// root package, and both mean the same thing to a user: what they asked for cannot
// be built. Saying that outright is more useful than the literal reading of the
// terms, which would be "no version of the root package can be used" — true, but it
// invites the reader to think their own project is at fault rather than the
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

// onlyPackage returns the single package an incompatibility mentions, and false if it
// mentions any other number of them.
func (p *phraser[P, S]) onlyPackage(inc *solver.Incompatibility[P, S]) (P, bool) {
	pkgs := p.sortedTerms(inc)
	if len(pkgs) != 1 {
		var zero P
		return zero, false
	}
	return pkgs[0], true
}
