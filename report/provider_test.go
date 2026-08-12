// SPDX-License-Identifier: Apache-2.0 OR MIT

package report_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/posit-dev/go-pubgrub/solver"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// set is the version-set type every fixture here uses.
type set = versionset.Ints

// dep is one requirement of one version.
type dep = solver.Dependency[string, set]

// universe is a table-driven solver.Provider over integer versions: what is
// published, and what each published version requires.
//
// # Why this exists rather than reusing solver's own test provider
//
// solver's universe is in-package (solver/provider_test.go) and so cannot be
// imported. That is not an accident worth working around: report's fixtures have
// to come from real solves, because a DERIVED incompatibility cannot be built
// from outside solver — newDerived is unexported, so no test here can fabricate a
// derivation graph. Every graph report is asked to explain is therefore one the
// solver actually produced.
type universe struct {
	versions map[string][]int64
	requires map[string]map[int64][]dep
}

func newUniverse() *universe {
	return &universe{
		versions: map[string][]int64{},
		requires: map[string]map[int64][]dep{},
	}
}

// with publishes a version of a package, with the given requirements.
func (u *universe) with(pkg string, version int64, deps ...dep) *universe {
	u.versions[pkg] = append(u.versions[pkg], version)
	if len(deps) > 0 {
		if u.requires[pkg] == nil {
			u.requires[pkg] = map[int64][]dep{}
		}
		u.requires[pkg][version] = deps
	}
	return u
}

// Candidates implements solver.Provider: how many published versions lie within
// allowed, and the latest of them, which is §8's stated preference.
func (u *universe) Candidates(pkg string, allowed set) (set, int, error) {
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
	return versionset.Exactly(best), count, nil
}

// Dependencies implements solver.Provider.
func (u *universe) Dependencies(pkg string, version set) ([]dep, error) {
	for _, v := range u.versions[pkg] {
		if versionset.Exactly(v).Equal(version) {
			return u.requires[pkg][v], nil
		}
	}
	return nil, fmt.Errorf("no such version %v of %s", version, pkg)
}

// requires is a dependency on a range of another package.
func requires(pkg string, allowed set) dep {
	return dep{Package: pkg, Allowed: allowed}
}

var _ solver.Provider[string, set] = (*universe)(nil)

// mustFail solves u for the given root and returns the root-cause
// incompatibility, failing the test if the solve did not fail the way a fixture
// needs it to.
func mustFail(t *testing.T, u *universe, root string, version int64) *solver.Incompatibility[string, set] {
	t.Helper()

	s := solver.New[string, set](root, versionset.Exactly(version), u)
	s.MaxRounds = 1000

	sol, err := s.Solve()
	if err == nil {
		t.Fatalf("solve succeeded with %v, but the fixture needs it to fail", sol.Selected)
	}

	var unsolvable *solver.Unsolvable[string, set]
	if !errors.As(err, &unsolvable) {
		t.Fatalf("solve failed with %v, which is not an *Unsolvable — the fixture is broken, "+
			"not the request unsatisfiable", err)
	}
	return unsolvable.RootCause
}
