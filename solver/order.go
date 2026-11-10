// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import "github.com/posit-dev/go-pubgrub/versionset"

// packageOrder holds the state for the opt-in StableDiscovery and OwnOrder
// settings. A nil *packageOrder means both are off.
//
// Places and marks only grow during a solve: backtracking never touches them.
type packageOrder[P comparable] struct {
	own    bool
	places map[P]int
	marked map[P]bool
}

// newPackageOrder returns nil when both settings are off.
func newPackageOrder[P comparable](stable, own bool) *packageOrder[P] {
	if !stable && !own {
		return nil
	}
	return &packageOrder[P]{own: own, places: map[P]int{}, marked: map[P]bool{}}
}

// place gives pkg the next unused number if it has none.
func (o *packageOrder[P]) place(pkg P) {
	if _, ok := o.places[pkg]; !ok {
		o.places[pkg] = len(o.places)
	}
}

// observe records a dependency list the provider returned for pkg: new
// packages get places in list order, and with OwnOrder an Exact one is marked.
// The first package observed, the root, gets place 0. A nil o does nothing.
func observe[P comparable, S versionset.Set[S]](o *packageOrder[P], pkg P, deps []Dependency[P, S]) {
	if o == nil {
		return
	}
	o.place(pkg)
	for _, d := range deps {
		o.place(d.Package)
		if o.own && d.Exact {
			o.marked[d.Package] = true
		}
	}
}

// before reports whether a has a smaller place than b. A package with no place
// goes after every package with one.
func (o *packageOrder[P]) before(a, b P) bool {
	pa, okA := o.places[a]
	pb, okB := o.places[b]
	switch {
	case okA && okB:
		return pa < pb
	case okA:
		return true
	default:
		return false
	}
}

// prefer reports whether a should be worked on before b, when both are available
// or both are not (aFound says which). With OwnOrder: marked first, then smaller
// place, rank never read. With StableDiscovery only: lower rank, then smaller place.
func (o *packageOrder[P]) prefer(a P, rankA int, b P, rankB int, aFound bool) bool {
	if aFound && !o.own && rankA != rankB {
		return rankA < rankB
	}
	if o.own && o.marked[a] != o.marked[b] {
		return o.marked[a]
	}
	return o.before(a, b)
}
