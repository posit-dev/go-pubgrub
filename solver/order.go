// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import "github.com/posit-dev/go-pubgrub/versionset"

// packageOrder holds the state for the opt-in StableDiscovery and OwnOrder
// settings. A nil *packageOrder means both are off.
//
// Places and marks only grow during a solve: backtracking never touches them.
type packageOrder[P comparable] struct {
	own bool
	// stable is set for StableDiscovery and OwnOrder: place breaks ties. Without it
	// (ClashFirst alone) a tie keeps the incumbent, as the default order does.
	stable bool
	// tierFirst (ClashFirst) compares the tier before the rank.
	tierFirst bool
	places    map[P]int
	marked    map[P]bool
}

// newPackageOrder returns nil when both settings are off.
func newPackageOrder[P comparable](stable, own bool) *packageOrder[P] {
	if !stable && !own {
		return nil
	}
	return &packageOrder[P]{own: own, stable: true, places: map[P]int{}, marked: map[P]bool{}}
}

// place gives pkg the next unused number if it has none.
func (o *packageOrder[P]) place(pkg P) {
	if _, ok := o.places[pkg]; !ok {
		o.places[pkg] = len(o.places)
	}
}

// observeDeps records a dependency list the provider returned for pkg: new
// packages get places in list order, and with OwnOrder an Exact one is marked.
// The first package observed, the root, gets place 0. A nil o does nothing.
func observeDeps[P comparable, S versionset.Set[S]](o *packageOrder[P], pkg P, deps []Dependency[P, S]) {
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

// pkgState is what prefer reads about one package besides its rank.
type pkgState struct {
	marked bool
	placed bool
	place  int
	tier   int
}

// stateOf reads pkg's current state, with the given tier.
func (o *packageOrder[P]) stateOf(pkg P, tier int) pkgState {
	place, placed := o.places[pkg]
	return pkgState{marked: o.marked[pkg], placed: placed, place: place, tier: tier}
}

// before reports whether a has a smaller place than b. A package with no place
// goes after every package with one.
func (a pkgState) before(b pkgState) bool {
	switch {
	case a.placed && b.placed:
		return a.place < b.place
	case a.placed:
		return true
	default:
		return false
	}
}

// prefer reports whether a should be worked on before b, when both are available
// or both are not (aFound says which). With OwnOrder: marked first, then higher
// tier, then smaller place, rank never read. With StableDiscovery only: lower
// rank, then higher tier, then smaller place. Tiers of 0 play no part.
func (o *packageOrder[P]) prefer(a P, rankA, tierA int, b P, rankB, tierB int, aFound bool) bool {
	return o.preferStates(o.stateOf(a, tierA), rankA, o.stateOf(b, tierB), rankB, aFound)
}

// preferStates is prefer on states read earlier, as a stored key holds them.
func (o *packageOrder[P]) preferStates(a pkgState, rankA int, b pkgState, rankB int, aFound bool) bool {
	if o.tierFirst && a.tier != b.tier {
		return a.tier > b.tier
	}
	if aFound && !o.own && rankA != rankB {
		return rankA < rankB
	}
	if o.own && a.marked != b.marked {
		return a.marked
	}
	if a.tier != b.tier {
		return a.tier > b.tier
	}
	if !o.stable {
		return false
	}
	return a.before(b)
}

// stateOfOrZero is stateOf, and the zero state for a nil o.
func (o *packageOrder[P]) stateOfOrZero(pkg P, tier int) pkgState {
	if o == nil {
		return pkgState{tier: tier}
	}
	return o.stateOf(pkg, tier)
}
