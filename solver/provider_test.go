// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"errors"
	"fmt"
	"math"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// universe is a table-driven Provider: the published versions of each package,
// and what each version requires.
//
// It exists so a scenario reads close to the way the specification writes one —
// a list of packages, their versions, and their dependencies — rather than as a
// sequence of calls. Everything the solver learns arrives through this interface,
// so a scenario expressed here is a complete input.
type universe struct {
	// versions lists each package's published versions, in any order.
	versions map[string][]int64

	// requires maps a package and version to what that version depends on.
	requires map[string]map[int64][]Dependency[string, set]

	// calls counts Dependencies calls per package, so a test can assert the
	// laziness the specification calls load-bearing.
	calls map[string]int

	// failFor makes Candidates fail for one package, to exercise the error paths.
	failFor string

	// offer, when set, overrides what Candidates returns as its best version —
	// used to check that the solver does not trust it.
	offer map[string]int64

	// offerSet does the same with an arbitrary SET rather than a single version, so
	// a test can offer a RANGE. That is the case ps.Eligible cannot reject on its
	// own, since it tests non-disjointness rather than containment.
	offerSet map[string]set

	// constantRank makes Candidates report rank 1 for every available package: the
	// legal-but-useless extreme of the hint, which disables the heuristic
	// completely.
	//
	// It is legal because the solver only ever COMPARES ranks. Note it is not an
	// upper bound on the usable versions — "wide" has three and this reports one —
	// and the contract deliberately does not require one.
	constantRank bool
}

func newUniverse() *universe {
	return &universe{
		versions: map[string][]int64{},
		requires: map[string]map[int64][]Dependency[string, set]{},
		calls:    map[string]int{},
	}
}

// with publishes a version of a package, with the given requirements.
func (u *universe) with(pkg string, version int64, deps ...Dependency[string, set]) *universe {
	u.versions[pkg] = append(u.versions[pkg], version)
	if len(deps) > 0 {
		if u.requires[pkg] == nil {
			u.requires[pkg] = map[int64][]Dependency[string, set]{}
		}
		u.requires[pkg][version] = deps
	}
	return u
}

// Candidates implements Provider: whether any version lies within allowed, the
// LATEST of them (which is §8's stated preference), and how many there are.
//
// The rank this returns is exact, because with an in-memory version list that is
// free. A real provider is explicitly allowed to approximate it — see
// constantRank, which is how the tests check that approximating changes the search
// order without changing the answer.
func (u *universe) Candidates(pkg string, allowed set) (set, bool, int, error) {
	if pkg == u.failFor {
		return versionset.Empty(), false, 0, errors.New("index unavailable")
	}

	best := int64(0)
	rank := 0
	for _, v := range u.versions[pkg] {
		if !allowed.Contains(v) {
			continue
		}
		rank++
		if rank == 1 || v > best {
			best = v
		}
	}
	if rank == 0 {
		// ⚠️ A deliberately HOSTILE rank on the unavailable path, not 0.
		//
		// The contract says rank is ignored when found is false, so this is legal —
		// and it is the only way the suite can prove the solver honours that. With 0
		// here, unavailability would keep winning preferCandidate's ordering purely
		// because 0 is the numeric minimum, so a solver that dropped the found
		// branch and compared rank alone would still pass. Returning the maximum
		// makes that regression fail instead of hiding.
		return versionset.Empty(), false, math.MaxInt, nil
	}
	if u.constantRank {
		rank = 1
	}
	if override, ok := u.offerSet[pkg]; ok {
		return override, true, rank, nil
	}
	if override, ok := u.offer[pkg]; ok {
		return versionset.Exactly(override), true, rank, nil
	}
	return versionset.Exactly(best), true, rank, nil
}

// Dependencies implements Provider.
func (u *universe) Dependencies(pkg string, version set) ([]Dependency[string, set], error) {
	u.calls[pkg]++

	for _, v := range u.versions[pkg] {
		if versionset.Exactly(v).Equal(version) {
			return u.requires[pkg][v], nil
		}
	}
	return nil, fmt.Errorf("no such version %v of %s", version, pkg)
}

// requirement is a dependency on a range of another package.
func requirement(pkg string, allowed set) Dependency[string, set] {
	return Dependency[string, set]{Package: pkg, Allowed: allowed}
}

// spanning is a dependency that every version of the depender in depender shares,
// which is §8's collapsing of adjacent versions with identical requirements.
func spanning(pkg string, allowed, depender set) Dependency[string, set] {
	return Dependency[string, set]{Package: pkg, Allowed: allowed, Depender: depender}
}

var _ Provider[string, set] = (*universe)(nil)
