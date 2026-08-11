// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"errors"
	"fmt"

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

// Candidates implements Provider: how many versions lie within allowed, and the
// LATEST of them, which is §8's stated preference.
func (u *universe) Candidates(pkg string, allowed set) (set, int, error) {
	if pkg == u.failFor {
		return versionset.Empty(), 0, errors.New("index unavailable")
	}

	best := int64(0)
	count := 0
	for _, v := range u.versions[pkg] {
		if !allowed.Contains(v) {
			continue
		}
		count++
		if count == 1 || v > best {
			best = v
		}
	}
	if count == 0 {
		return versionset.Empty(), 0, nil
	}
	if override, ok := u.offer[pkg]; ok {
		return versionset.Exactly(override), count, nil
	}
	return versionset.Exactly(best), count, nil
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
