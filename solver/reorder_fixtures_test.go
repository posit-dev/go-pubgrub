// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// logProvider wraps a universe: every package has the same rank, the root's
// dependencies are listed in reverse, and every call is logged.
type logProvider struct {
	u   *universe
	log []string
}

func newLogProvider(u *universe) *logProvider {
	u.constantRank = true
	return &logProvider{u: u}
}

func (p *logProvider) Candidates(pkg string, allowed set) (set, bool, int, error) {
	best, found, rank, err := p.u.Candidates(pkg, allowed)
	p.log = append(p.log, fmt.Sprintf("C %s %v", pkg, allowed))
	return best, found, rank, err
}

func (p *logProvider) Dependencies(pkg string, version set) ([]Dependency[string, set], error) {
	deps, err := p.u.Dependencies(pkg, version)
	p.log = append(p.log, fmt.Sprintf("D %s %v", pkg, version))
	if pkg == "root" {
		rev := make([]Dependency[string, set], len(deps))
		for i, d := range deps {
			rev[len(deps)-1-i] = d
		}
		return rev, err
	}
	return deps, err
}

func ge(v int64) set { return versionset.AtLeast(v) }
func lt(v int64) set { return versionset.LessThan(v) }

// fixtureA .. fixtureE are the behavior spec's fixtures. Dependencies are
// written in the order the spec gives them.
func fixtureA() *universe {
	return newUniverse().
		with("root", 1, requirement("b", ge(1)), requirement("c", ge(1)), requirement("d", ge(1))).
		with("b", 1).with("b", 2).
		with("c", 1).with("c", 2).
		with("d", 1).
		with("d", 2, requirement("b", lt(2)), requirement("c", lt(2)))
}

func fixtureC() *universe {
	return newUniverse().
		with("root", 1, requirement("s", ge(1)), requirement("t", ge(1))).
		with("s", 1, requirement("u", ge(1))).
		with("t", 1, requirement("u", ge(1))).
		with("u", 1)
}

func fixtureD() *universe {
	return newUniverse().
		with("root", 1, requirement("d", ge(1)), requirement("b", ge(1))).
		with("b", 1).
		with("b", 2, requirement("c", ge(2))).
		with("c", 1).
		with("c", 2, requirement("d", lt(2))).
		with("d", 1).with("d", 2)
}

func fixtureDMutation() *universe {
	return newUniverse().
		with("root", 1, requirement("d", ge(1)), requirement("b", ge(1))).
		with("b", 1).
		with("b", 2, requirement("c", ge(2))).
		with("c", 1).
		with("c", 2, requirement("d", lt(3))).
		with("d", 1).with("d", 2)
}

func fixtureE() *universe {
	return newUniverse().
		with("root", 1, requirement("a", ge(1)), requirement("b", ge(1))).
		with("a", 1).
		with("a", 2, requirement("c", ge(1))).
		with("b", 1).
		with("b", 2, requirement("d", ge(1)), requirement("c", lt(2))).
		with("c", 1, requirement("e", ge(2))).
		with("d", 1, requirement("e", lt(2))).
		with("e", 1).with("e", 2)
}

// runLog formats one solve: the provider's call log and the solution.
func runLog(u *universe, configure func(*Solver[string, set])) (string, error) {
	p := newLogProvider(u)
	s := New("root", versionset.Exactly(1), p)
	if configure != nil {
		configure(s)
	}
	sol, err := s.Solve()
	if err != nil {
		return strings.Join(p.log, "\n"), err
	}
	keys := make([]string, 0, len(sol.Selected))
	for k := range sol.Selected {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(strings.Join(p.log, "\n"))
	b.WriteString("\nsolution:")
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%v", k, sol.Selected[k])
	}
	fmt.Fprintf(&b, "\norder: %v", sol.Order)
	return b.String(), nil
}
