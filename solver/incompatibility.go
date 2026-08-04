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

// NewIncompatibility builds a normalized incompatibility from terms.
//
// Terms about the same package are intersected. Always-true terms are dropped.
// The resulting map is owned by the incompatibility and never mutated
// afterwards, since incompatibilities are retained for the whole solve.
func NewIncompatibility[P comparable, S versionset.Set[S]](
	kind Kind, terms map[P]term.Term[S],
) *Incompatibility[P, S] {
	normalized := make(map[P]term.Term[S], len(terms))
	for pkg, t := range terms {
		if existing, ok := normalized[pkg]; ok {
			t = existing.Intersect(t)
		}
		if t.IsAlwaysTrue() {
			// Contributes nothing to the conjunction.
			continue
		}
		normalized[pkg] = t
	}

	return &Incompatibility[P, S]{kind: kind, terms: normalized}
}

// newDerived builds an incompatibility recording the two causes it came from.
func newDerived[P comparable, S versionset.Set[S]](
	terms map[P]term.Term[S], a, b *Incompatibility[P, S],
) *Incompatibility[P, S] {
	inc := NewIncompatibility(KindDerived, terms)
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
func (inc *Incompatibility[P, S]) Causes() (a, b *Incompatibility[P, S], derived bool) {
	if inc.causes[0] == nil || inc.causes[1] == nil {
		return nil, nil, false
	}
	return inc.causes[0], inc.causes[1], true
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
