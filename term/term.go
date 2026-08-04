// SPDX-License-Identifier: Apache-2.0 OR MIT

package term

import (
	"github.com/posit-dev/go-pubgrub/versionset"
)

// Relation is the outcome of testing one term against another.
type Relation int

const (
	// Inconclusive means both truth values of the tested term remain possible.
	//
	// First, so the zero value is the answer that asserts least. A Relation that
	// was never computed must not read as Satisfied.
	Inconclusive Relation = iota

	// Satisfied means the tested term is forced true.
	Satisfied

	// Contradicted means the tested term is forced false.
	Contradicted
)

// String implements fmt.Stringer.
func (r Relation) String() string {
	switch r {
	case Satisfied:
		return "satisfied"
	case Contradicted:
		return "contradicted"
	case Inconclusive:
		return "inconclusive"
	default:
		return "unknown"
	}
}

// Term is a claim about one package: that a version is selected within a set
// (positive), or that no version in the set is selected (negative).
//
// # The asymmetry that matters
//
// "No version of the package is selected at all" makes every NEGATIVE term true
// and every POSITIVE term false. So a negative term is NOT the positive term
// over the complement set: they agree on every selected version and differ
// precisely on absence.
//
// Concretely, Negative(set) and Positive(set.Complement()) both exclude the
// versions in set, but only the negative form is also satisfied when the package
// is absent. Getting this wrong makes the solver demand packages nobody asked
// for, because "must not be version 2" would be read as "must be some version
// other than 2".
//
// This is why Negate flips polarity and leaves the set untouched, rather than
// complementing the set.
//
// # Why the polarity field is stored inverted
//
// The field is `negative`, not `positive`, so that the ZERO Term is Positive
// over the zero set — for a well-behaved implementation, Positive(∅), the
// always-FALSE term.
//
// That is deliberate and worth preserving. A zero value that came out as
// Negative(∅) would be the always-TRUE term, meaning a Term that was never
// initialized would silently satisfy everything asked of it. Always-false is the
// safe default: an uninitialized term asserts that nothing can satisfy it, which
// surfaces as a conflict rather than as a constraint quietly disappearing.
type Term[S versionset.Set[S]] struct {
	set S

	// negative is stored rather than positive so the zero value is the
	// always-false term. See the type documentation.
	negative bool
}

// Positive returns a term asserting a selected version lies in set.
func Positive[S versionset.Set[S]](set S) Term[S] {
	return Term[S]{set: set, negative: false}
}

// Negative returns a term asserting no selected version lies in set — which is
// also satisfied when the package is absent entirely.
func Negative[S versionset.Set[S]](set S) Term[S] {
	return Term[S]{set: set, negative: true}
}

// Set returns the term's version set. The polarity is not applied; a negative
// term's set is the set it excludes.
func (t Term[S]) Set() S { return t.set }

// IsPositive reports the term's polarity.
func (t Term[S]) IsPositive() bool { return !t.negative }

// Negate returns the logical negation, flipping polarity and leaving the set
// alone. See the type documentation for why this is not a complement.
func (t Term[S]) Negate() Term[S] {
	return Term[S]{set: t.set, negative: !t.negative}
}

// IsAlwaysFalse reports whether no assignment can satisfy this term.
//
// True only for Positive(∅): it demands a selected version inside a set holding
// none. An incompatibility containing such a term can never be satisfied, so it
// can never fire and may be pruned.
func (t Term[S]) IsAlwaysFalse() bool {
	return !t.negative && t.set.IsEmpty()
}

// IsAlwaysTrue reports whether every assignment satisfies this term.
//
// True only for Negative(∅): "no selected version lies in the empty set" holds
// unconditionally. Such a term contributes nothing to a conjunction and can be
// dropped from an incompatibility without changing its meaning.
func (t Term[S]) IsAlwaysTrue() bool {
	return t.negative && t.set.IsEmpty()
}

// Intersect returns the term holding exactly when both terms hold.
//
// From the algebra:
//
//	Positive(a) ∧ Positive(b) = Positive(a ∩ b)
//	Positive(a) ∧ Negative(b) = Positive(a \ b)
//	Negative(a) ∧ Negative(b) = Negative(a ∪ b)
//
// The mixed case is the one worth internalizing: "must be in a, and must not be
// in b" is "must be in a-but-not-b". A positive term intersected with anything
// stays positive — there is no way to intersect back to a purely negative claim,
// which is what keeps "this package is required" from decaying into "this
// package is optional".
func (t Term[S]) Intersect(other Term[S]) Term[S] {
	switch {
	case !t.negative && !other.negative:
		return Positive(t.set.Intersect(other.set))
	case !t.negative && other.negative:
		return Positive(versionset.Difference(t.set, other.set))
	case t.negative && !other.negative:
		return Positive(versionset.Difference(other.set, t.set))
	default:
		return Negative(t.set.Union(other.set))
	}
}

// Union returns the term holding when either term holds.
//
// Derived from intersection and negation by De Morgan, T1 ∨ T2 = ¬(¬T1 ∧ ¬T2):
//
//	Positive(a) ∨ Positive(b) = Positive(a ∪ b)
//	Positive(a) ∨ Negative(b) = Negative(b \ a)
//	Negative(a) ∨ Negative(b) = Negative(a ∩ b)
//
// The whole algorithm needs this in exactly one place: building the combined
// term during conflict resolution.
func (t Term[S]) Union(other Term[S]) Term[S] {
	return t.Negate().Intersect(other.Negate()).Negate()
}

// Relation reports how this term constrains other, assuming both concern the
// same package.
//
// Read it as: given that this term holds, is other forced true (Satisfied),
// forced false (Contradicted), or still open (Inconclusive)?
//
// The implementation reduces to set containment via the two degenerate terms.
// "This term forces other true" is exactly "this term and NOT other cannot hold
// together", and likewise for false — which means Relation needs no case
// analysis over the four polarity combinations, and cannot disagree with
// Intersect. Deriving it this way is deliberate: hand-written case analysis over
// four combinations is where an implementation of this algebra typically goes
// wrong, and the errors are silent.
func (t Term[S]) Relation(other Term[S]) Relation {
	switch {
	case t.Intersect(other.Negate()).IsAlwaysFalse():
		return Satisfied
	case t.Intersect(other).IsAlwaysFalse():
		return Contradicted
	default:
		return Inconclusive
	}
}

// Satisfies reports whether this term forces other true.
func (t Term[S]) Satisfies(other Term[S]) bool {
	return t.Relation(other) == Satisfied
}

// Contradicts reports whether this term forces other false.
func (t Term[S]) Contradicts(other Term[S]) bool {
	return t.Relation(other) == Contradicted
}

// Equal reports whether both terms make the same claim.
func (t Term[S]) Equal(other Term[S]) bool {
	return t.negative == other.negative && t.set.Equal(other.set)
}

// String renders the term for diagnostics.
func (t Term[S]) String() string {
	s := any(t.set)
	text := "?"
	if str, ok := s.(interface{ String() string }); ok {
		text = str.String()
	}
	if !t.negative {
		return text
	}
	return "not " + text
}
