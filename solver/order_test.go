// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"fmt"
	"strings"
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// loggedUniverse records each Dependencies call as "pkg@version".
type loggedUniverse struct {
	*universe
	log []string
}

func (l *loggedUniverse) Dependencies(pkg string, version set) ([]Dependency[string, set], error) {
	for v := int64(0); v < 10; v++ {
		if version.Contains(v) {
			l.log = append(l.log, fmt.Sprintf("%s@%d", pkg, v))
			break
		}
	}
	return l.universe.Dependencies(pkg, version)
}

func atLeast(pkg string, v int64) Dependency[string, set] {
	return requirement(pkg, versionset.AtLeast(v))
}

// exact1 requires exactly version 1 of pkg; mark says whether Exact is set.
func exact1(pkg string, mark bool) Dependency[string, set] {
	return Dependency[string, set]{Package: pkg, Allowed: versionset.Exactly(1), Exact: mark}
}

type orderMode struct {
	name         string
	stable, own  bool
	order, calls string
}

// orderCase lists what each setting combination must produce. A mode left out
// is not asserted.
type orderCase struct {
	name  string
	build func() *universe
	sel   map[string]int64
	modes []orderMode
}

func mode(name string, stable, own bool, order, calls string) orderMode {
	return orderMode{name: name, stable: stable, own: own, order: order, calls: calls}
}

func orderCases() []orderCase {
	return []orderCase{
		{
			name: "stable A: setting changes the pick",
			build: func() *universe {
				return newUniverse().
					with("root", 1, atLeast("a", 1), atLeast("b", 1)).
					with("a", 1).with("a", 2).with("b", 1).with("b", 2)
			},
			sel: map[string]int64{"root": 1, "a": 2, "b": 2},
			modes: []orderMode{
				mode("off", false, false, "root b a", "root@1 b@2 a@2"),
				mode("stable", true, false, "root a b", "root@1 a@2 b@2"),
			},
		},
		{
			name: "stable B: rank still first",
			build: func() *universe {
				return newUniverse().
					with("root", 1, atLeast("a", 1), atLeast("b", 1)).
					with("a", 1).with("a", 2).with("a", 3).with("b", 1)
			},
			sel: map[string]int64{"root": 1, "a": 3, "b": 1},
			modes: []orderMode{
				mode("off", false, false, "root b a", "root@1 b@1 a@3"),
				mode("stable", true, false, "root b a", "root@1 b@1 a@3"),
			},
		},
		{
			name: "stable C: a later naming does not move a place",
			build: func() *universe {
				return newUniverse().
					with("root", 1, atLeast("p", 1), atLeast("q", 1), atLeast("r", 1)).
					with("p", 1, atLeast("r", 1), atLeast("q", 1)).
					with("q", 1).with("r", 1)
			},
			sel: map[string]int64{"root": 1, "p": 1, "q": 1, "r": 1},
			modes: []orderMode{
				mode("off", false, false, "root r q p", "root@1 r@1 q@1 p@1"),
				mode("stable", true, false, "root p q r", "root@1 p@1 q@1 r@1"),
			},
		},
		{
			name: "stable D: places survive backtracking",
			build: func() *universe {
				return newUniverse().
					with("root", 1, atLeast("m", 1)).
					with("m", 1, atLeast("x", 1), atLeast("y", 1), atLeast("z", 1)).
					with("m", 2, atLeast("y", 1), atLeast("z", 1), atLeast("x", 1), atLeast("w", 2)).
					with("x", 1).with("y", 1).with("z", 1).with("w", 1)
			},
			sel: map[string]int64{"root": 1, "m": 1, "x": 1, "y": 1, "z": 1},
			modes: []orderMode{
				mode("off", false, false, "root m z y x", "root@1 m@2 m@1 z@1 y@1 x@1"),
				mode("stable", true, false, "root m y z x", "root@1 m@2 m@1 y@1 z@1 x@1"),
			},
		},
		{
			name: "stable E: nothing to break",
			build: func() *universe {
				return newUniverse().
					with("root", 1, atLeast("a", 1)).
					with("a", 1, atLeast("b", 1)).with("b", 1)
			},
			sel: map[string]int64{"root": 1, "a": 1, "b": 1},
			modes: []orderMode{
				mode("off", false, false, "root a b", "root@1 a@1 b@1"),
				mode("stable", true, false, "root a b", "root@1 a@1 b@1"),
				mode("own", false, true, "root a b", "root@1 a@1 b@1"),
			},
		},
		{
			name: "own A: rank is ignored",
			build: func() *universe {
				return newUniverse().
					with("root", 1, atLeast("a", 1), atLeast("b", 1)).
					with("a", 1).with("a", 2).with("a", 3).with("b", 1)
			},
			sel: map[string]int64{"root": 1, "a": 3, "b": 1},
			modes: []orderMode{
				mode("off", false, false, "root b a", "root@1 b@1 a@3"),
				mode("stable", true, false, "root b a", "root@1 b@1 a@3"),
				mode("own", false, true, "root a b", "root@1 a@3 b@1"),
				mode("both", true, true, "root a b", "root@1 a@3 b@1"),
			},
		},
		{
			name: "own B: the mark puts a package ahead of an earlier place",
			build: func() *universe {
				return newUniverse().
					with("root", 1, atLeast("a", 1), atLeast("b", 1)).
					with("a", 1, exact1("c", true)).with("b", 1).with("c", 1)
			},
			sel: map[string]int64{"root": 1, "a": 1, "b": 1, "c": 1},
			modes: []orderMode{
				mode("off", false, false, "root b a c", "root@1 b@1 a@1 c@1"),
				mode("stable", true, false, "root a b c", "root@1 a@1 b@1 c@1"),
				mode("own", false, true, "root a c b", "root@1 a@1 c@1 b@1"),
				mode("both", true, true, "root a c b", "root@1 a@1 c@1 b@1"),
			},
		},
		{
			name: "own C: without the flag, no mark",
			build: func() *universe {
				return newUniverse().
					with("root", 1, atLeast("a", 1), atLeast("b", 1)).
					with("a", 1, exact1("c", false)).with("b", 1).with("c", 1)
			},
			sel: map[string]int64{"root": 1, "a": 1, "b": 1, "c": 1},
			modes: []orderMode{
				mode("off", false, false, "root b a c", "root@1 b@1 a@1 c@1"),
				mode("own", false, true, "root a b c", "root@1 a@1 b@1 c@1"),
			},
		},
		{
			name: "own D: a mark from an undone version stays",
			build: func() *universe {
				return newUniverse().
					with("root", 1, atLeast("m", 1), atLeast("n", 1)).
					with("m", 1).
					with("m", 2, atLeast("j", 1), exact1("k", true), atLeast("w", 2)).
					with("n", 1, atLeast("j", 1), atLeast("k", 1)).
					with("j", 1).with("k", 1).with("w", 1)
			},
			sel: map[string]int64{"root": 1, "m": 1, "n": 1, "j": 1, "k": 1},
			modes: []orderMode{
				mode("off", false, false, "root n m k j", "root@1 n@1 k@1 j@1 m@2 n@1 m@1 k@1 j@1"),
				mode("stable", true, false, "root m n j k", "root@1 n@1 j@1 k@1 m@2 m@1 n@1 j@1 k@1"),
				mode("own", false, true, "root m n k j", "root@1 m@2 m@1 n@1 k@1 j@1"),
				mode("both", true, true, "root m n k j", "root@1 m@2 m@1 n@1 k@1 j@1"),
			},
		},
		{
			name: "own E: unavailable still comes before a marked package",
			build: func() *universe {
				return newUniverse().
					with("root", 1, atLeast("m", 1)).
					with("m", 1).
					with("m", 2, exact1("a", true), atLeast("b", 2)).
					with("a", 1).with("b", 1)
			},
			sel: map[string]int64{"root": 1, "m": 1},
			modes: []orderMode{
				mode("off", false, false, "root m", "root@1 m@2 m@1"),
				mode("own", false, true, "root m", "root@1 m@2 m@1"),
			},
		},
		{
			name: "own F: nothing changes",
			build: func() *universe {
				return newUniverse().
					with("root", 1, atLeast("a", 1)).with("a", 1).with("a", 2)
			},
			sel: map[string]int64{"root": 1, "a": 2},
			modes: []orderMode{
				mode("off", false, false, "root a", "root@1 a@2"),
				mode("stable", true, false, "root a", "root@1 a@2"),
				mode("own", false, true, "root a", "root@1 a@2"),
			},
		},
	}
}

func TestOrderSettings(t *testing.T) {
	for _, c := range orderCases() {
		for _, m := range c.modes {
			t.Run(c.name+"/"+m.name, func(t *testing.T) {
				lu := &loggedUniverse{universe: c.build()}
				s := New("root", versionset.Exactly(1), lu)
				s.StableDiscovery, s.OwnOrder = m.stable, m.own
				sol, err := s.Solve()
				if err != nil {
					t.Fatalf("Solve: %v", err)
				}
				if got := strings.Join(sol.Order, " "); got != m.order {
					t.Errorf("order = %q, want %q", got, m.order)
				}
				if got := strings.Join(lu.log, " "); got != m.calls {
					t.Errorf("Dependencies calls = %q, want %q", got, m.calls)
				}
				assertSelected(t, sol, c.sel)
			})
		}
	}
}

func candOf(pkg string, found bool, rank int) candidate[string, set] {
	return candidate[string, set]{pkg: pkg, found: found, rank: rank}
}

// TestPreferCandidateStable is spec 1 test G: lower rank wins whatever the
// places, equal rank goes to the smaller place, and two unavailable packages go
// by place.
func TestPreferCandidateStable(t *testing.T) {
	o := newPackageOrder[string](true, false)
	o.place("early")
	o.place("late")

	if !preferCandidate(candOf("late", true, 1), candOf("early", true, 2), o) {
		t.Error("lower rank must win whatever the places")
	}
	if preferCandidate(candOf("early", true, 2), candOf("late", true, 1), o) {
		t.Error("higher rank must lose even with the smaller place")
	}
	if !preferCandidate(candOf("early", true, 2), candOf("late", true, 2), o) {
		t.Error("equal rank: the smaller place must win")
	}
	if preferCandidate(candOf("late", true, 2), candOf("early", true, 2), o) {
		t.Error("equal rank: the larger place must lose")
	}
	if !preferCandidate(candOf("early", false, 9), candOf("late", false, 0), o) {
		t.Error("both unavailable: the smaller place must win")
	}
	if !preferCandidate(candOf("late", false, 0), candOf("early", true, 1), o) {
		t.Error("unavailable must come before available")
	}
	if !preferCandidate(candOf("early", true, 1), candOf("unplaced", true, 1), o) {
		t.Error("a package with no place must go after one with a place")
	}
}

// TestPreferCandidateOwnOrder is spec 2 test H.
func TestPreferCandidateOwnOrder(t *testing.T) {
	o := newPackageOrder[string](false, true)
	o.place("first")
	o.place("second")
	o.marked["second"] = true

	if !preferCandidate(candOf("second", true, 9), candOf("first", true, 1), o) {
		t.Error("the marked package must win whatever the places and ranks")
	}
	if preferCandidate(candOf("first", true, 1), candOf("second", true, 9), o) {
		t.Error("the unmarked package must lose")
	}

	o.marked["second"] = false
	if !preferCandidate(candOf("first", true, 9), candOf("second", true, 1), o) {
		t.Error("equal marks: the smaller place must win whatever the rank")
	}
	if preferCandidate(candOf("second", true, 1), candOf("first", true, 9), o) {
		t.Error("equal marks: the larger place must lose whatever the rank")
	}

	o.marked["first"] = true
	if !preferCandidate(candOf("second", false, 0), candOf("first", true, 1), o) {
		t.Error("a package with no available version must beat a marked one")
	}
}

// TestExactIgnoredWithoutOwnOrder checks Exact does not change the order unless
// OwnOrder is on: marks are only recorded for OwnOrder.
func TestExactIgnoredWithoutOwnOrder(t *testing.T) {
	o := newPackageOrder[string](true, false)
	observeDeps(o, "root", []Dependency[string, set]{exact1("c", true)})
	if o.marked["c"] {
		t.Error("c was marked although OwnOrder is off")
	}
	if got := newPackageOrder[string](false, false); got != nil {
		t.Error("both settings off must give a nil order")
	}
}
