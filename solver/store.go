// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"github.com/posit-dev/go-pubgrub/versionset"
)

// Store holds every known incompatibility, indexed by the packages they mention.
//
// # Why the index exists
//
// Re-scanning every incompatibility after every assignment does not scale: most
// are irrelevant to whatever package just changed. Propagation only revisits
// incompatibilities mentioning a package whose assignments changed during the
// current pass, which is what the per-package index provides.
//
// # Why iteration is newest-first
//
// Conflict resolution synthesizes progressively more general incompatibilities
// over time. When several could fire at once, taking the newest surfaces the most
// broadly-applicable derivation first, which prunes more of the search than
// rediscovering a narrow fact version by version would.
//
// The store is append-only. Incompatibilities are timeless facts, so nothing is
// ever removed on backtracking — only the partial solution is truncated.
//
// The zero value is an empty store and is ready to use.
type Store[P comparable, S versionset.Set[S]] struct {
	all []*Incompatibility[P, S]

	// byPackage maps a package to indices into all, in insertion order.
	byPackage map[P][]int

	// empty holds the empty incompatibility, if one has been added. It cannot be
	// indexed by package, since it mentions none, and there is at most one of it
	// because all empty incompatibilities are Equal. See Add.
	empty *Incompatibility[P, S]
}

// NewStore returns an empty store.
func NewStore[P comparable, S versionset.Set[S]]() *Store[P, S] {
	return &Store[P, S]{byPackage: make(map[P][]int)}
}

// Add records an incompatibility and indexes it under every package it mentions.
//
// Returns the stored incompatibility. Adding an incompatibility equal to one
// already stored returns the existing one and stores nothing, so the store holds
// no duplicates — which matters because conflict resolution can rederive a fact
// already known, and a store full of duplicates would make propagation
// repeatedly derive the same consequence.
//
// # The empty incompatibility is handled separately
//
// Both loops below are driven by the term map, so an incompatibility with no
// terms would skip dedup entirely AND be indexed under no package — landing in
// the store as an un-findable duplicate that Mentioning can never return. It
// gets its own slot instead. The empty incompatibility is precisely the object
// whose appearance means "no solution exists", so it is the last thing that
// should be silently unreachable, even though §7.4 returns it before reaching
// this method today.
func (st *Store[P, S]) Add(inc *Incompatibility[P, S]) *Incompatibility[P, S] {
	if st.byPackage == nil {
		st.byPackage = make(map[P][]int)
	}

	if inc.IsEmpty() {
		if st.empty != nil {
			return st.empty
		}
		st.empty = inc
		st.all = append(st.all, inc)
		return inc
	}

	// Only incompatibilities sharing a package can be equal, so the search is
	// bounded by one package's index rather than the whole store.
	for pkg := range inc.terms {
		for _, idx := range st.byPackage[pkg] {
			if st.all[idx].Equal(inc) {
				return st.all[idx]
			}
		}
		break
	}

	index := len(st.all)
	st.all = append(st.all, inc)
	for pkg := range inc.terms {
		st.byPackage[pkg] = append(st.byPackage[pkg], index)
	}
	return inc
}

// Len reports how many incompatibilities are stored.
func (st *Store[P, S]) Len() int { return len(st.all) }

// All returns every incompatibility in insertion order. The slice must not be
// modified.
func (st *Store[P, S]) All() []*Incompatibility[P, S] { return st.all }

// Mentioning returns the incompatibilities mentioning pkg, NEWEST FIRST.
//
// See the type documentation for why the order is not an implementation detail.
func (st *Store[P, S]) Mentioning(pkg P) []*Incompatibility[P, S] {
	indices := st.byPackage[pkg]

	out := make([]*Incompatibility[P, S], 0, len(indices))
	for i := len(indices) - 1; i >= 0; i-- {
		out = append(out, st.all[indices[i]])
	}
	return out
}
