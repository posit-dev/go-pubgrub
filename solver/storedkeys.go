// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"fmt"
	"sort"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// storedKey is one package's decide key, kept between rounds.
type storedKey[P comparable, S versionset.Set[S]] struct {
	cand candidate[P, S] // found/best/rank/allowed and the tier it was built with

	pos   int      // index of the package's first positive derivation
	state pkgState // place, exact mark and tier when built
}

// storedKeys is the StoredKeys state, owned by the partial solution so that
// every added or removed assignment can be seen. A nil *storedKeys is off.
type storedKeys[P comparable, S versionset.Set[S]] struct {
	keys map[P]*storedKey[P, S]

	// Packages with an assignment added or removed since the last round.
	touched []P

	// firstPos is the index of each package's first positive derivation; decided
	// holds the packages that have a decision. Both follow the assignment list.
	firstPos map[P]int
	decided  map[P]bool
}

func newStoredKeys[P comparable, S versionset.Set[S]]() *storedKeys[P, S] {
	return &storedKeys[P, S]{
		keys:     map[P]*storedKey[P, S]{},
		firstPos: map[P]int{},
		decided:  map[P]bool{},
	}
}

// added notes an assignment appended at index i.
func (k *storedKeys[P, S]) added(a Assignment[P, S], i int) {
	k.touched = append(k.touched, a.Package)
	if a.Decision {
		k.decided[a.Package] = true
		return
	}
	if a.Term.IsPositive() {
		if _, ok := k.firstPos[a.Package]; !ok {
			k.firstPos[a.Package] = i
		}
	}
}

// removed notes assignments cut off by backtracking; kept is how many remain.
func (k *storedKeys[P, S]) removed(cut []Assignment[P, S], kept int) {
	for _, a := range cut {
		k.touched = append(k.touched, a.Package)
		if a.Decision {
			delete(k.decided, a.Package)
		}
		if p, ok := k.firstPos[a.Package]; ok && p >= kept {
			delete(k.firstPos, a.Package)
		}
	}
}

// chooseStored is chooseCandidate with the setting on: it rebuilds only stale
// keys, in ascending first-positive position, then picks the best key.
func chooseStored[P comparable, S versionset.Set[S]](
	ps *PartialSolution[P, S], provider Provider[P, S], tierOf func(P) int, ord *packageOrder[P],
) (candidate[P, S], bool, error) {
	k := ps.sk
	var chosen candidate[P, S]

	stale := map[P]bool{}
	for _, p := range k.touched {
		if _, ok := k.firstPos[p]; !ok || k.decided[p] {
			delete(k.keys, p) // lost its last positive derivation, or decided
			continue
		}
		stale[p] = true // an assignment about it came or went
	}
	k.touched = k.touched[:0]

	for p, key := range k.keys {
		if stale[p] {
			continue
		}
		if ord.stateOfOrZero(p, tierNow(tierOf, p)) != key.state {
			stale[p] = true
		}
	}

	order := make([]P, 0, len(stale))
	for p := range stale {
		order = append(order, p)
	}
	sort.Slice(order, func(i, j int) bool { return k.firstPos[order[i]] < k.firstPos[order[j]] })

	for _, p := range order {
		ps.stats.KeyBuilds++
		accumulated, ok := ps.Accumulated(p)
		if !ok || !accumulated.IsPositive() {
			delete(k.keys, p) // unreachable, as in chooseCandidate
			continue
		}
		next, err := askProvider(ps, provider, p, accumulated.Set())
		if err != nil {
			return candidate[P, S]{}, false, err
		}
		next.tier = tierNow(tierOf, p)
		k.keys[p] = &storedKey[P, S]{
			cand: next, pos: k.firstPos[p], state: ord.stateOfOrZero(p, next.tier),
		}
	}

	var best *storedKey[P, S]
	for _, key := range k.keys {
		if best == nil || preferKey(key, best, ord) || (!preferKey(best, key, ord) && key.pos < best.pos) {
			best = key
		}
	}
	if best == nil {
		return chosen, false, nil
	}
	return best.cand, true, nil
}

// preferKey is preferCandidate on stored keys: it reads the place, mark and
// tier saved in each key, not the live ones.
func preferKey[P comparable, S versionset.Set[S]](a, b *storedKey[P, S], ord *packageOrder[P]) bool {
	if a.cand.found != b.cand.found {
		return !a.cand.found
	}
	if ord != nil {
		return ord.preferStates(a.state, a.cand.rank, b.state, b.cand.rank, a.cand.found)
	}
	return preferCandidate(a.cand, b.cand, nil)
}

func tierNow[P comparable](tierOf func(P) int, p P) int {
	if tierOf == nil {
		return 0
	}
	return tierOf(p)
}

// askProvider returns the provider's answer for pkg over allowed, reusing the
// last one when the allowed set is unchanged.
func askProvider[P comparable, S versionset.Set[S]](
	ps *PartialSolution[P, S], provider Provider[P, S], pkg P, allowed S,
) (candidate[P, S], error) {
	if m, ok := ps.candMemo[pkg]; ok && m.allowed.Equal(allowed) {
		return m, nil
	}
	best, available, rank, err := provider.Candidates(pkg, allowed)
	if err != nil {
		return candidate[P, S]{}, fmt.Errorf("solver: candidates for %s: %w", formatPackage(pkg), err)
	}
	next := candidate[P, S]{pkg: pkg, allowed: allowed, best: best, found: available, rank: rank}
	if ps.candMemo == nil {
		ps.candMemo = make(map[P]candidate[P, S])
	}
	ps.candMemo[pkg] = next
	return next, nil
}
