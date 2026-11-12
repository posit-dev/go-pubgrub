// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"fmt"
	"testing"

	"github.com/posit-dev/go-pubgrub/term"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// depRecord is "from in versions needs to in need".
func depRecord(from string, versions set, to string, need set) *Incompatibility[string, set] {
	return NewIncompatibility(KindDependency, map[string]term.Term[set]{
		from: term.Positive(versions),
		to:   term.Negative(need),
	})
}

func mentioning(st *Store[string, set], pkg string) []string {
	var out []string
	for _, inc := range st.Mentioning(pkg) {
		out = append(out, inc.String())
	}
	return out
}

func addAll(merge bool, recs ...*Incompatibility[string, set]) *Store[string, set] {
	st := NewStore[string, set]()
	st.mergeDeps = merge
	for _, r := range recs {
		st.addDependency(r)
	}
	return st
}

func TestMergeFoldsSameDependerSameRange(t *testing.T) {
	r1 := depRecord("a", versionset.Exactly(1), "b", versionset.AtLeast(1))
	r2 := depRecord("a", versionset.Exactly(2), "b", versionset.AtLeast(1))
	merged := depRecord("a", versionset.Range(1, 3), "b", versionset.AtLeast(1))

	on := mentioning(addAll(true, r1, r2), "b")
	if want := []string{merged.String()}; fmt.Sprint(on) != fmt.Sprint(want) {
		t.Errorf("on: Mentioning(b) = %v, want %v", on, want)
	}
	// Newest first, so the second record is listed before the first.
	off := mentioning(addAll(false, r1, r2), "b")
	if want := []string{r2.String(), r1.String()}; fmt.Sprint(off) != fmt.Sprint(want) {
		t.Errorf("off: Mentioning(b) = %v, want %v", off, want)
	}
}

func TestMergeKeepsRetiredRecordInAll(t *testing.T) {
	r1 := depRecord("a", versionset.Exactly(1), "b", versionset.AtLeast(1))
	r2 := depRecord("a", versionset.Exactly(2), "b", versionset.AtLeast(1))
	st := addAll(true, r1, r2)
	if got := st.liveDependencyRecords(); got != 1 {
		t.Errorf("live records = %d, want 1", got)
	}
	var all []string
	for _, inc := range st.All() {
		all = append(all, inc.String())
	}
	merged := depRecord("a", versionset.Range(1, 3), "b", versionset.AtLeast(1))
	if want := []string{r1.String(), merged.String()}; fmt.Sprint(all) != fmt.Sprint(want) {
		t.Errorf("All = %v, want %v", all, want)
	}
}

func TestMergeNeverFoldsDifferentRange(t *testing.T) {
	r1 := depRecord("a", versionset.Exactly(1), "b", versionset.AtLeast(1))
	r2 := depRecord("a", versionset.Exactly(2), "b", versionset.AtLeast(2))
	got := mentioning(addAll(true, r1, r2), "b")
	if want := []string{r2.String(), r1.String()}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Mentioning(b) = %v, want %v", got, want)
	}
}

func TestMergeNeverFoldsDifferentDepender(t *testing.T) {
	r1 := depRecord("a", versionset.Exactly(1), "c", versionset.AtLeast(1))
	r2 := depRecord("b", versionset.Exactly(1), "c", versionset.AtLeast(1))
	got := mentioning(addAll(true, r1, r2), "c")
	if want := []string{r2.String(), r1.String()}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Mentioning(c) = %v, want %v", got, want)
	}
}

func TestMergeKeepsExistingRecordWhenNewOneIsCovered(t *testing.T) {
	wide := depRecord("a", versionset.Range(1, 3), "b", versionset.AtLeast(1))
	narrow := depRecord("a", versionset.Exactly(2), "b", versionset.AtLeast(1))
	st := addAll(true, wide, narrow)
	if got := mentioning(st, "b"); fmt.Sprint(got) != fmt.Sprint([]string{wide.String()}) {
		t.Errorf("Mentioning(b) = %v, want only %v", got, wide)
	}
	if st.Len() != 1 {
		t.Errorf("Len = %d, want 1", st.Len())
	}
}

// Same solution with merging on and off, on a problem that yields same-range
// records (a@1 and a@2 both need b>=1, and a@2 is declined first).
func TestMergeSolutionUnchanged(t *testing.T) {
	build := func() *universe {
		u := newUniverse().
			with("root", 1, requirement("a", versionset.AtLeast(1)), requirement("c", versionset.AtLeast(1))).
			with("a", 1, requirement("b", versionset.AtLeast(1))).
			with("a", 2, requirement("b", versionset.AtLeast(1)), requirement("c", versionset.LessThan(2))).
			with("b", 1).with("c", 1).with("c", 2)
		u.constantRank = true
		return u
	}
	run := func(merge bool) (*Solution[string, set], *Solver[string, set]) {
		s := New("root", versionset.Exactly(1), build())
		s.MergeDependencies = merge
		sol, err := s.Solve()
		if err != nil {
			t.Fatalf("Solve(merge=%v): %v", merge, err)
		}
		return sol, s
	}
	off, offS := run(false)
	on, onS := run(true)
	assertSelected(t, off, map[string]int64{"root": 1, "a": 1, "b": 1, "c": 2})
	assertSelected(t, on, map[string]int64{"root": 1, "a": 1, "b": 1, "c": 2})
	if onS.Stats.LiveDependencyRecords >= offS.Stats.LiveDependencyRecords {
		t.Errorf("live records on=%d off=%d, want fewer with merging",
			onS.Stats.LiveDependencyRecords, offS.Stats.LiveDependencyRecords)
	}
}
