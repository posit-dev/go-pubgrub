// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"github.com/posit-dev/go-pubgrub/term"
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
// ever removed on backtracking — only the partial solution is truncated. (With
// MergeDependencies, a merged-away record leaves the index but stays in All.)
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

	// mergeDeps turns on addDependency's merging (Solver.MergeDependencies).
	mergeDeps bool
	live      map[depKey[P]][]*Incompatibility[P, S]
	retired   map[*Incompatibility[P, S]]struct{}
}

// depKey names the two packages of a dependency record: depender and dependency.
type depKey[P comparable] struct{ from, to P }

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

// liveDependencyRecords counts the KindDependency incompatibilities Mentioning can
// still return.
func (st *Store[P, S]) liveDependencyRecords() int {
	n := 0
	for _, inc := range st.all {
		if _, gone := st.retired[inc]; !gone && inc.Kind() == KindDependency {
			n++
		}
	}
	return n
}

// dependencyPair returns the depender and dependency packages of a two-term
// dependency record: a positive term and a negative one.
func dependencyPair[P comparable, S versionset.Set[S]](inc *Incompatibility[P, S]) (from, to P, ok bool) {
	if inc.Kind() != KindDependency || inc.Len() != 2 {
		return from, to, false
	}
	var haveFrom, haveTo bool
	for pkg, t := range inc.terms {
		if t.IsPositive() {
			from, haveFrom = pkg, true
		} else {
			to, haveTo = pkg, true
		}
	}
	return from, to, haveFrom && haveTo
}

// addDependency is Add for a dependency record. With mergeDeps on, a record that
// asks the same range of the same package as a live one is folded into it: the
// result names the union of both dependers, and the older record leaves the
// package index (it stays in All, since earlier derivations point at it).
func (st *Store[P, S]) addDependency(inc *Incompatibility[P, S]) *Incompatibility[P, S] {
	from, to, ok := dependencyPair(inc)
	if !st.mergeDeps || !ok {
		return st.Add(inc)
	}
	need, _ := inc.Term(to)
	if need.Set().IsEmpty() {
		return st.Add(inc)
	}
	dependers, _ := inc.Term(from)
	key := depKey[P]{from, to}

	for i, x := range st.live[key] {
		xNeed, _ := x.Term(to)
		if !xNeed.Equal(need) {
			continue
		}
		xFrom, _ := x.Term(from)
		if versionset.IsSubsetOf(dependers.Set(), xFrom.Set()) {
			return x
		}
		merged := NewIncompatibility(KindDependency, map[P]term.Term[S]{
			from: term.Positive(xFrom.Set().Union(dependers.Set())),
			to:   need,
		})
		got := st.Add(merged)
		if got != merged {
			return got
		}
		st.retire(x, from, to)
		st.live[key][i] = merged
		return merged
	}

	got := st.Add(inc)
	if got == inc {
		if st.live == nil {
			st.live = make(map[depKey[P]][]*Incompatibility[P, S])
		}
		st.live[key] = append(st.live[key], got)
	}
	return got
}

// retire removes x from the index of both its packages. It stays in All.
func (st *Store[P, S]) retire(x *Incompatibility[P, S], pkgs ...P) {
	if st.retired == nil {
		st.retired = make(map[*Incompatibility[P, S]]struct{})
	}
	st.retired[x] = struct{}{}
	for _, pkg := range pkgs {
		idx := st.byPackage[pkg]
		kept := make([]int, 0, len(idx))
		for _, i := range idx {
			if st.all[i] != x {
				kept = append(kept, i)
			}
		}
		st.byPackage[pkg] = kept
	}
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
