// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/posit-dev/go-pubgrub/term"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// callLogProvider logs every Candidates and Dependencies call.
type callLogProvider struct {
	*universe
	log []string
}

func (p *callLogProvider) Candidates(pkg string, allowed set) (set, bool, int, error) {
	p.log = append(p.log, fmt.Sprintf("C %s %v", pkg, allowed))
	return p.universe.Candidates(pkg, allowed)
}

func (p *callLogProvider) Dependencies(pkg string, version set) ([]Dependency[string, set], error) {
	p.log = append(p.log, fmt.Sprintf("D %s %v", pkg, version))
	return p.universe.Dependencies(pkg, version)
}

type skRun struct {
	order, calls, sel, err string
	builds                 int
}

func (r skRun) decisions() skRun { r.builds = 0; return r }

func runSK(u *universe, stable, own bool, threshold int, stored bool) skRun {
	p := &callLogProvider{universe: u}
	s := New("root", versionset.Exactly(1), p)
	s.StableDiscovery, s.OwnOrder, s.ClashThreshold, s.StoredKeys = stable, own, threshold, stored
	sol, err := s.Solve()
	r := skRun{calls: strings.Join(p.log, "|"), builds: s.Stats.KeyBuilds}
	if err != nil {
		r.err = err.Error()
		return r
	}
	r.order = strings.Join(sol.Order, " ")
	r.sel = fmt.Sprint(sol.Selected)
	return r
}

// dependenciesOnly is the Dependencies calls of a run, as the spec's call logs.
func dependenciesOnly(calls string) string {
	var out []string
	for _, c := range strings.Split(calls, "|") {
		if strings.HasPrefix(c, "D ") {
			out = append(out, c)
		}
	}
	return strings.Join(out, " ")
}

func skUniverseA() *universe {
	return newUniverse().
		with("root", 1, atLeast("a", 1), atLeast("b", 1)).
		with("a", 1).with("a", 2).with("b", 1).with("b", 2)
}

func skUniverseB() *universe {
	return newUniverse().
		with("root", 1, atLeast("p", 1), atLeast("q", 1), atLeast("r", 1)).
		with("p", 1, atLeast("r", 1), atLeast("q", 1)).
		with("q", 1).with("r", 1)
}

func skUniverseC() *universe {
	return newUniverse().
		with("root", 1, atLeast("a", 1), atLeast("b", 1)).
		with("a", 1, requirement("b", lt(2))).
		with("b", 1).with("b", 2)
}

func skUniverseD() *universe {
	return newUniverse().
		with("root", 1, atLeast("m", 1), atLeast("j", 1), exact1("k", false)).
		with("m", 1, exact1("k", true)).
		with("j", 1).with("k", 1)
}

func checkSK(t *testing.T, name string, r skRun, order, calls string, builds int) {
	t.Helper()
	if r.err != "" {
		t.Fatalf("%s: %s", name, r.err)
	}
	if r.order != order {
		t.Errorf("%s: order = %q, want %q", name, r.order, order)
	}
	if got := dependenciesOnly(r.calls); got != calls {
		t.Errorf("%s: calls = %q, want %q", name, got, calls)
	}
	if r.builds != builds {
		t.Errorf("%s: key builds = %d, want %d", name, r.builds, builds)
	}
}

// Spec A: fewer builds, same decisions.
func TestStoredKeysA(t *testing.T) {
	off := runSK(skUniverseA(), false, false, 0, false)
	on := runSK(skUniverseA(), false, false, 0, true)
	checkSK(t, "off", off, "root b a", dependencyCallLog("root@1", "b@2", "a@2"), 4)
	checkSK(t, "on", on, "root b a", dependencyCallLog("root@1", "b@2", "a@2"), 3)
	if off.sel != on.sel || off.sel != "map[a:2 b:2 root:1]" {
		t.Errorf("selection off %s, on %s", off.sel, on.sel)
	}
}

// dependencyCallLog formats the spec's "pkg@version" call list the way callLogProvider
// logs a Dependencies call, with the version set's own text.
func dependencyCallLog(calls ...string) string {
	out := make([]string, len(calls))
	for i, c := range calls {
		pkg, v, _ := strings.Cut(c, "@")
		out[i] = fmt.Sprintf("D %s %v", pkg, versionset.Exactly(mustInt(v)))
	}
	return strings.Join(out, " ")
}

func mustInt(s string) int64 {
	var n int64
	if _, err := fmt.Sscan(s, &n); err != nil {
		panic(err)
	}
	return n
}

// Spec B: a key is kept across a decision (with StableDiscovery).
func TestStoredKeysB(t *testing.T) {
	checkSK(t, "off", runSK(skUniverseB(), true, false, 0, false), "root p q r", dependencyCallLog("root@1", "p@1", "q@1", "r@1"), 7)
	checkSK(t, "on", runSK(skUniverseB(), true, false, 0, true), "root p q r", dependencyCallLog("root@1", "p@1", "q@1", "r@1"), 4)
}

// Spec C: a narrowed range makes a key stale. A kept b key would offer b@2.
func TestStoredKeysC(t *testing.T) {
	for _, on := range []bool{false, true} {
		r := runSK(skUniverseC(), false, false, 0, on)
		checkSK(t, fmt.Sprintf("stored=%v", on), r, "root a b", dependencyCallLog("root@1", "a@1", "b@1"), 4)
		if strings.Contains(r.calls, "b@2") || strings.Contains(r.calls, "D b {2}") {
			t.Errorf("stored=%v: b@2 was offered: %s", on, r.calls)
		}
	}
}

// Spec D: a mark with no new assignment makes a key stale.
func TestStoredKeysD(t *testing.T) {
	checkSK(t, "off", runSK(skUniverseD(), false, true, 0, false), "root m k j", dependencyCallLog("root@1", "m@1", "k@1", "j@1"), 7)
	checkSK(t, "on", runSK(skUniverseD(), false, true, 0, true), "root m k j", dependencyCallLog("root@1", "m@1", "k@1", "j@1"), 5)
}

// Spec E: a tier change makes a key stale.
func TestStoredKeysE(t *testing.T) {
	for _, on := range []bool{false, true} {
		r := runSK(fixtureA(), true, false, 1, on)
		if r.order != "root d b c" || dependenciesOnly(r.calls) != dependencyCallLog("root@1", "b@2", "c@2", "d@2", "d@2", "b@1", "c@1") {
			t.Errorf("T=1 stored=%v: order %q calls %q", on, r.order, dependenciesOnly(r.calls))
		}
		if r.sel != "map[b:1 c:1 d:2 root:1]" {
			t.Errorf("T=1 stored=%v: selection %s", on, r.sel)
		}
		for _, th := range []int{0, 2} {
			r := runSK(fixtureA(), true, false, th, on)
			if r.order != "root b c d" || dependenciesOnly(r.calls) != dependencyCallLog("root@1", "b@2", "c@2", "d@2", "d@1") {
				t.Errorf("T=%d stored=%v: order %q calls %q", th, on, r.order, dependenciesOnly(r.calls))
			}
		}
	}
}

// settingCombos is every mix of the other settings.
func settingCombos() [][3]int {
	var out [][3]int
	for _, m := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		for _, th := range []int{0, 1, 2} {
			out = append(out, [3]int{m[0], m[1], th})
		}
	}
	return out
}

func sameDecisions(t *testing.T, name string, build func() *universe) {
	t.Helper()
	for _, c := range settingCombos() {
		off := runSK(build(), c[0] == 1, c[1] == 1, c[2], false)
		on := runSK(build(), c[0] == 1, c[1] == 1, c[2], true)
		if off.decisions() != on.decisions() {
			t.Errorf("%s stable=%d own=%d T=%d:\n off %+v\n  on %+v", name, c[0], c[1], c[2], off, on)
		}
		if on.builds > off.builds {
			t.Errorf("%s stable=%d own=%d T=%d: builds on %d > off %d", name, c[0], c[1], c[2], on.builds, off.builds)
		}
	}
}

// Spec F: every existing order case and fixture gives the same result with the
// setting on, alone and with each other setting.
func TestStoredKeysSameDecisions(t *testing.T) {
	for _, c := range orderCases() {
		sameDecisions(t, c.name, c.build)
	}
	for name, f := range map[string]func() *universe{
		"fixtureA": fixtureA, "fixtureC": fixtureC, "fixtureD": fixtureD,
		"fixtureDMutation": fixtureDMutation, "fixtureE": fixtureE,
		"A": skUniverseA, "B": skUniverseB, "C": skUniverseC, "D": skUniverseD,
	} {
		sameDecisions(t, name, f)
	}
}

// randomUniverse builds a small random graph: some versions require others,
// some with exact marks. Unsolvable ones are fine.
func randomUniverse(rng *rand.Rand) *universe {
	u := newUniverse()
	pkgs := []string{"p0", "p1", "p2", "p3", "p4", "p5"}
	reqs := func(self string, n int) []Dependency[string, set] {
		var deps []Dependency[string, set]
		for i := 0; i < n; i++ {
			p := pkgs[rng.Intn(len(pkgs))]
			if p == self {
				continue
			}
			v := int64(1 + rng.Intn(3))
			switch rng.Intn(3) {
			case 0:
				deps = append(deps, requirement(p, versionset.AtLeast(v)))
			case 1:
				deps = append(deps, requirement(p, versionset.LessThan(v+1)))
			default:
				deps = append(deps, Dependency[string, set]{Package: p, Allowed: versionset.Exactly(v), Exact: rng.Intn(2) == 0})
			}
		}
		return deps
	}
	u.with("root", 1, reqs("root", 2+rng.Intn(3))...)
	for _, p := range pkgs {
		for v := int64(1); v <= int64(1+rng.Intn(3)); v++ {
			u.with(p, v, reqs(p, rng.Intn(3))...)
		}
	}
	return u
}

// Random graphs, including ones that backtrack: decisions and every provider
// call must match the setting-off run. Seed 663 with ClashThreshold 1 is the
// one that notices a tier change not making a key stale (spec E's gap).
func TestStoredKeysRandomGraphs(t *testing.T) {
	for seed := int64(0); seed < 700; seed++ {
		build := func() *universe { return randomUniverse(rand.New(rand.NewSource(seed))) }
		for _, c := range settingCombos() {
			off := runSK(build(), c[0] == 1, c[1] == 1, c[2], false)
			on := runSK(build(), c[0] == 1, c[1] == 1, c[2], true)
			if off.decisions() != on.decisions() {
				t.Fatalf("seed %d stable=%d own=%d T=%d:\n off %+v\n  on %+v", seed, c[0], c[1], c[2], off, on)
			}
		}
	}
}

// Off is the zero value: no stored-key state is made.
func TestStoredKeysOffMakesNoState(t *testing.T) {
	s := New("root", versionset.Exactly(1), skUniverseA())
	if _, err := s.Solve(); err != nil {
		t.Fatal(err)
	}
	if s.ps.keyBook != nil {
		t.Error("stored-key state exists with the setting off")
	}
}

// A tier change with no assignment about the package makes its key stale (spec
// 3d). Drives chooseCached directly, moving the tier between rounds.
func TestStoredKeysTierChangeRebuildsKey(t *testing.T) {
	u := newUniverse().with("x", 1).with("y", 1)
	ps := NewPartialSolution[string, set]()
	ps.keyBook = newKeyCache[string, set]()
	ps.Derive("x", term.Positive(versionset.AtLeast(1)), nil)
	ps.Derive("y", term.Positive(versionset.AtLeast(1)), nil)
	ord := newPackageOrder[string](true, false)
	ord.place("x")
	ord.place("y")
	tier := map[string]int{}
	tierOf := func(p string) int { return tier[p] }

	round := func(want string, wantBuilds int) {
		t.Helper()
		c, ok, err := chooseCached(ps, u, tierOf, ord)
		if err != nil || !ok {
			t.Fatalf("chooseCached: ok=%v err=%v", ok, err)
		}
		if c.pkg != want || ps.stats.KeyBuilds != wantBuilds {
			t.Errorf("picked %s with %d key builds, want %s with %d", c.pkg, ps.stats.KeyBuilds, want, wantBuilds)
		}
	}
	round("x", 2) // both keys built; x has the earlier place
	round("x", 2) // nothing changed: no rebuild
	tier["y"] = 1
	round("y", 3) // only y's tier changed: y rebuilt and now wins
	tier["y"] = 0
	round("x", 4) // tier back: y rebuilt again
}
