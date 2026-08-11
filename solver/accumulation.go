// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"github.com/posit-dev/go-pubgrub/term"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// accumulation is a per-package intersection of asserted terms.
//
// The partial solution keeps one of these for its whole history, and §7.2's
// satisfier queries build throwaway ones while replaying a prefix. They must
// answer relation questions identically — a satisfier index computed under
// different rules than Classify uses would point at the wrong assignment — so
// both go through this one type rather than reimplementing the rules.
type accumulation[P comparable, S versionset.Set[S]] map[P]term.Term[S]

// add folds t into what is asserted about pkg.
func (acc accumulation[P, S]) add(pkg P, t term.Term[S]) {
	if existing, ok := acc[pkg]; ok {
		acc[pkg] = existing.Intersect(t)
		return
	}
	acc[pkg] = t
}

// relation reports what the accumulation ENTAILS about t, for t's package.
//
// # Unknown is inconclusive for both polarities
//
// With nothing asserted about the package, the answer is Inconclusive whatever
// the term's polarity. This is worth being precise about, because it looks like
// it contradicts the term algebra and does not.
//
// A term's truth in a COMPLETED world is one question: there, absence makes
// every negative term true, which is the asymmetry term.Term documents. What an
// accumulation entails is a different question. An unassigned package is not
// known-absent, it is merely undecided — a version could still be decided later,
// which would make a negative term false. So nothing is entailed either way.
//
// Treating unknown as Satisfied for negative terms breaks the algorithm at its
// core: a dependency incompatibility is {depender: Positive, dependee: Negative},
// so once the depender is selected the dependee's negative term would read as
// already satisfied, the incompatibility would classify as fully satisfied, and
// every dependency would be reported as a conflict instead of being DERIVED.
// Dependency resolution would never resolve anything.
//
// # An always-false term is the one exception
//
// Positive(∅) asks for a version from an empty set, so it is false in every
// world — including one where the package is still undecided. §2.5's definition
// ("S contradicts t if t is forced false whenever every term in S is true")
// therefore yields Contradicted with nothing asserted, and §2.4 says outright
// that an incompatibility holding such a term "will never fire and never needs
// to be checked again".
//
// Without this case the inconclusive rule above swallows it: the always-false
// term looks like the single open term of an almost-satisfied incompatibility,
// and propagation derives its negation — recording an assignment of the
// vacuously-true "not ∅" for a package nothing has said anything about. §2.5's
// own table is written for nonempty sets, whose justification — "a version could
// still be decided later that lands in r" — is exactly what fails when r is
// empty.
func (acc accumulation[P, S]) relation(pkg P, t term.Term[S]) term.Relation {
	if t.IsAlwaysFalse() {
		return term.Contradicted
	}

	asserted, ok := acc[pkg]
	if !ok {
		return term.Inconclusive
	}
	return asserted.Relation(t)
}

// satisfies reports whether the accumulation entails every term of inc, i.e.
// whether inc is fully satisfied — violated — by it.
func (acc accumulation[P, S]) satisfies(inc *Incompatibility[P, S]) bool {
	for pkg, t := range inc.terms {
		if acc.relation(pkg, t) != term.Satisfied {
			return false
		}
	}
	return true
}
