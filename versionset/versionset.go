// SPDX-License-Identifier: Apache-2.0 OR MIT

package versionset

// Set is the version-set abstraction the solver reasons over.
//
// An ecosystem supplies the implementation; the solver never learns what a
// version is. Everything the algorithm needs reduces to the operations below,
// which is what lets one solver serve Python, R, or anything else.
//
// The type parameter is the implementing type itself, so operations stay
// concrete and allocation-free rather than boxing every range into an
// interface on the solver's hot path:
//
//	type MySet struct{ ... }
//	func (s MySet) Intersect(other MySet) MySet { ... }
//	var _ versionset.Set[MySet] = MySet{}
//
// # Required laws
//
// Implementations must satisfy these. They are not stylistic — the solver's
// algebra is derived from them, and an implementation that breaks one produces
// wrong resolutions rather than errors.
//
//   - Intersect and Union are commutative and associative.
//   - Complement is an involution: s.Complement().Complement() equals s.
//   - De Morgan holds: (a ∪ b)ᶜ = aᶜ ∩ bᶜ, and (a ∩ b)ᶜ = aᶜ ∪ bᶜ.
//   - Equal is an equivalence relation, and it must be TRUE for any two
//     representations denoting the same set of versions.
//
// That last requirement is the one most easily violated and the most damaging.
// The solver compares terms and incompatibilities for equality, so two
// different in-memory encodings of the same logical range must report Equal.
// An implementation that keeps, say, [1,2) ∪ [2,3) distinct from [1,3) will
// silently fail to recognize incompatibilities it has already derived, and the
// solver can then loop or report a conflict that does not exist. Canonicalize
// on construction.
//
// Implementations should be immutable, or at least must never mutate the
// receiver or the argument: the solver retains sets inside incompatibilities
// for the lifetime of a solve.
type Set[S any] interface {
	// Intersect returns the versions in both sets.
	Intersect(other S) S

	// Union returns the versions in either set.
	Union(other S) S

	// Complement returns every version not in this set.
	Complement() S

	// IsEmpty reports whether the set contains no versions.
	//
	// This is the solver's only primitive test. Subset and disjointness are
	// derived from it — see IsSubsetOf and IsDisjointFrom — so an implementation
	// needs to get exactly this one predicate right.
	IsEmpty() bool

	// Equal reports whether both sets denote the same versions. See the
	// canonicalization requirement above.
	Equal(other S) bool
}

// IsSubsetOf reports whether a contains only versions also in b.
//
// Derived rather than required of implementations: a ⊆ b exactly when a has
// nothing outside b. Deriving it means an implementation cannot get subset and
// emptiness to disagree with each other.
func IsSubsetOf[S Set[S]](a, b S) bool {
	return a.Intersect(b.Complement()).IsEmpty()
}

// IsDisjointFrom reports whether a and b share no versions.
func IsDisjointFrom[S Set[S]](a, b S) bool {
	return a.Intersect(b).IsEmpty()
}

// Difference returns the versions in a that are not in b.
func Difference[S Set[S]](a, b S) S {
	return a.Intersect(b.Complement())
}
