// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"sort"
	"strings"

	"github.com/posit-dev/go-pubgrub/term"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// Kind describes where an incompatibility came from, for error reporting.
type Kind int

const (
	// KindDerived means conflict resolution produced this by combining two
	// others. Zero value, so an incompatibility built without stating a kind
	// does not masquerade as an authoritative external fact.
	KindDerived Kind = iota

	// KindDependency means "this range of A requires that range of B".
	KindDependency

	// KindUnavailable means a range of a package cannot be selected at all —
	// yanked, unpublished, or platform-incompatible.
	KindUnavailable

	// KindNoVersions means no version of a package satisfies what was asked.
	KindNoVersions

	// KindRoot is the seed fact: the root package is exactly its one version.
	KindRoot
)

// String implements fmt.Stringer.
func (k Kind) String() string {
	switch k {
	case KindDependency:
		return "dependency"
	case KindUnavailable:
		return "unavailable"
	case KindNoVersions:
		return "no versions"
	case KindRoot:
		return "root"
	case KindDerived:
		return "derived"
	default:
		return "unknown"
	}
}

// Incompatibility is a set of terms, at most one per package, that can never all
// hold at once. Read it as "not (T1 and T2 and ... )", or equivalently "at least
// one of these terms must be false in any solution".
//
// # It is timeless
//
// An incompatibility is context-independent: once built, its terms are mutually
// incompatible regardless of what has been decided before or after. That is why
// the incompatibility set only ever GROWS and is never rolled back on
// backtracking — only the partial solution, which is built from time-sensitive
// guesses, gets truncated.
//
// # How to read one
//
//	{A: Positive(rA), B: Negative(rB)}
//
// means "A in rA depends on B in rB". The only way not to violate it is for A to
// not be in rA, or for B to be in rB — so selecting A from rA forces some
// version of B within rB. This encoding is why dependencies use a NEGATIVE term
// on the depended-upon package.
//
// # Normalization
//
// At most one term per package: two terms about one package are intersected into
// one. Always-true terms, meaning Negative of the empty set, are dropped since
// they contribute nothing to a conjunction. Both happen at construction, so
// every Incompatibility in circulation is normalized and two of them can be
// compared directly.
type Incompatibility[P comparable, S versionset.Set[S]] struct {
	terms map[P]term.Term[S]

	kind Kind

	// causes holds the two incompatibilities combined to derive this one, and is
	// nil for external facts. This is what makes the derivation graph walkable:
	// following causes from a failure reaches the external facts that forced it,
	// which is exactly the proof that no solution exists.
	causes [2]*Incompatibility[P, S]
}

// PackageTerm pairs a package with a term, for building an incompatibility from
// input that may mention one package more than once.
//
// A map cannot express that, which is why this type exists: §7.3's resolution
// step unions the terms of two incompatibilities, and both may speak about the
// same package — §10's does, and the intersection of those two terms is the whole
// content of the incompatibility it derives.
type PackageTerm[P comparable, S versionset.Set[S]] struct {
	Package P
	Term    term.Term[S]
}

// NewIncompatibility builds a normalized incompatibility from a map of terms.
//
// Always-true terms are dropped, since they contribute nothing to a conjunction
// (§2.4). Per-package intersection cannot arise here — a map holds one term per
// key already — so use NewIncompatibilityFrom when the input may name a package
// twice.
//
// The terms are copied; the caller's map is not retained.
func NewIncompatibility[P comparable, S versionset.Set[S]](
	kind Kind, terms map[P]term.Term[S],
) *Incompatibility[P, S] {
	normalized := make(map[P]term.Term[S], len(terms))
	for pkg, t := range terms {
		if t.IsAlwaysTrue() {
			continue
		}
		normalized[pkg] = t
	}

	return &Incompatibility[P, S]{kind: kind, terms: normalized}
}

// NewIncompatibilityFrom builds a normalized incompatibility from pairs, applying
// §3's normalization in full: terms about the same package are INTERSECTED into
// one, and always-true terms are dropped.
//
// Intersecting two terms about one package can produce Positive(∅), the
// always-false term — two disjoint positive constraints on one package do it —
// which makes the whole incompatibility inert (§2.4, IsInert) rather than
// unsatisfiable. That is a legitimate outcome, not an error.
func NewIncompatibilityFrom[P comparable, S versionset.Set[S]](
	kind Kind, pairs ...PackageTerm[P, S],
) *Incompatibility[P, S] {
	merged := make(map[P]term.Term[S], len(pairs))
	for _, p := range pairs {
		if existing, ok := merged[p.Package]; ok {
			merged[p.Package] = existing.Intersect(p.Term)
			continue
		}
		merged[p.Package] = p.Term
	}

	// Merging first, then dropping always-true terms. The order is immaterial —
	// intersecting with Negative(∅) is the identity, and merging can only produce
	// an always-true term out of always-true inputs — so this is not a hazard to
	// preserve, just the order §3 lists them in.
	return NewIncompatibility(kind, merged)
}

// newDerived builds an incompatibility from pairs, recording the two causes it
// came from. Pairs rather than a map because §7.3's union produces two terms
// about one package — see PackageTerm.
func newDerived[P comparable, S versionset.Set[S]](
	pairs []PackageTerm[P, S], a, b *Incompatibility[P, S],
) *Incompatibility[P, S] {
	inc := NewIncompatibilityFrom(KindDerived, pairs...)
	inc.causes = [2]*Incompatibility[P, S]{a, b}
	return inc
}

// Terms returns the incompatibility's terms.
//
// The returned map must not be modified: it is the incompatibility's own state,
// shared with every holder for the lifetime of the solve.
func (inc *Incompatibility[P, S]) Terms() map[P]term.Term[S] { return inc.terms }

// Term returns the term for pkg, and whether the incompatibility mentions it.
func (inc *Incompatibility[P, S]) Term(pkg P) (term.Term[S], bool) {
	t, ok := inc.terms[pkg]
	return t, ok
}

// Len reports how many packages the incompatibility mentions.
func (inc *Incompatibility[P, S]) Len() int { return len(inc.terms) }

// Kind reports where the incompatibility came from.
func (inc *Incompatibility[P, S]) Kind() Kind { return inc.kind }

// Causes returns the two incompatibilities combined to derive this one, and
// false for an external fact.
//
// # This is the authority on derivedness, and Kind is not
//
// §9's explanation walk is defined over the derivation graph — it follows causes
// toward leaves and reports those leaves as the external facts that forced the
// failure. So a node with nil causes IS a leaf to that walk, whatever its Kind
// says.
//
// Kind is a label for phrasing the explanation; it cannot be trusted to answer
// this question, because KindDerived is the zero value and so is what any
// incompatibility built without naming a kind reports. That zero value was
// chosen so an unlabeled incompatibility would not masquerade as an
// authoritative external fact — which is right for the label and exactly
// inverted for the graph. IsDerived resolves it the safe way for both: ask
// causes.
func (inc *Incompatibility[P, S]) Causes() (a, b *Incompatibility[P, S], derived bool) {
	if inc.causes[0] == nil || inc.causes[1] == nil {
		return nil, nil, false
	}
	return inc.causes[0], inc.causes[1], true
}

// IsDerived reports whether this incompatibility came from conflict resolution
// rather than being an external fact, per its causes rather than its Kind. See
// Causes for why those can disagree and why causes wins.
func (inc *Incompatibility[P, S]) IsDerived() bool {
	return inc.causes[0] != nil && inc.causes[1] != nil
}

// Packages returns the mentioned packages, in map order.
func (inc *Incompatibility[P, S]) Packages() []P {
	out := make([]P, 0, len(inc.terms))
	for pkg := range inc.terms {
		out = append(out, pkg)
	}
	return out
}

// IsEmpty reports whether the incompatibility has no terms.
//
// An empty incompatibility is a conjunction over nothing, which is vacuously
// true — so it is ALWAYS violated, by everything. Deriving one is the formal
// statement that the problem has no solution, and it is one of the two failure
// terminations of the main loop.
func (inc *Incompatibility[P, S]) IsEmpty() bool { return len(inc.terms) == 0 }

// IsInert reports whether this incompatibility can never fire.
//
// True when some term is always false, since then the conjunction is
// permanently false and no assignment can satisfy the whole. Such an
// incompatibility never needs checking again and may be pruned. It arises
// legitimately, for example by intersecting two disjoint positive constraints on
// one package during normalization.
func (inc *Incompatibility[P, S]) IsInert() bool {
	for _, t := range inc.terms {
		if t.IsAlwaysFalse() {
			return true
		}
	}
	return false
}

// ViolatedBy reports whether a COMPLETE selection violates this incompatibility:
// every term true at once, which is what an incompatibility says can never
// happen.
//
// selected maps each chosen package to the version chosen for it; a package
// absent from the map was not selected at all. A version is a singleton set, for
// which "lies in the term's set" is subset and "lies outside it" is
// disjointness — stated that way so the test is well defined for a caller that
// passes a wider set.
//
// # This is a different question from Classify, deliberately
//
// Classify asks what a PARTIAL solution entails, where an unmentioned package is
// merely undecided and entails nothing. This asks whether a FINISHED world
// violates the incompatibility, where absence is a fact: it makes every negative
// term true and every positive term false, the asymmetry term.Term documents.
//
// The distinction is exactly the gap §11 item 8 records in the specification, and
// it is why §4's success criterion needs this as a cross-check. An
// incompatibility with two or more terms, all negative, is satisfied by absence
// alone — so a solution that omits both packages violates it, while every
// relation test Classify can make about it stays Inconclusive forever and unit
// propagation never fires. Solve uses this to refuse to return such a
// "solution".
func (inc *Incompatibility[P, S]) ViolatedBy(selected map[P]S) bool {
	for pkg, t := range inc.terms {
		version, ok := selected[pkg]
		if !ok {
			// Absence makes a negative term true and a positive one false.
			if t.IsPositive() {
				return false
			}
			continue
		}

		if t.IsPositive() {
			// True when the selected version lies in the term's set.
			if !versionset.IsSubsetOf(version, t.Set()) {
				return false
			}
			continue
		}
		// A negative term is true when the selected version lies outside its set.
		if !versionset.IsDisjointFrom(version, t.Set()) {
			return false
		}
	}
	return true
}

// Equal reports whether both incompatibilities assert the same thing.
//
// Because construction normalizes, this is a term-by-term comparison rather than
// anything set-theoretic. Causes are deliberately NOT compared: two
// incompatibilities asserting the same fact are the same fact regardless of how
// each was reached.
func (inc *Incompatibility[P, S]) Equal(other *Incompatibility[P, S]) bool {
	if inc == other {
		return true
	}
	if inc == nil || other == nil {
		return false
	}
	if len(inc.terms) != len(other.terms) {
		return false
	}
	for pkg, t := range inc.terms {
		otherTerm, ok := other.terms[pkg]
		if !ok || !t.Equal(otherTerm) {
			return false
		}
	}
	return true
}

// String renders the incompatibility for diagnostics, with packages sorted so
// output is stable across runs rather than following map order.
func (inc *Incompatibility[P, S]) String() string {
	if len(inc.terms) == 0 {
		return "{} (unsatisfiable)"
	}

	parts := make([]string, 0, len(inc.terms))
	for pkg, t := range inc.terms {
		parts = append(parts, formatPackage(pkg)+" "+t.String())
	}
	sort.Strings(parts)

	return "{" + strings.Join(parts, ", ") + "}"
}

// formatPackage renders a package key without requiring it to be a string.
func formatPackage[P comparable](pkg P) string {
	if s, ok := any(pkg).(interface{ String() string }); ok {
		return s.String()
	}
	if s, ok := any(pkg).(string); ok {
		return s
	}
	return "pkg"
}
