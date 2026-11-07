// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// TestPropagateRemembersContradictedIncompatibilities checks Propagate records an
// incompatibility with a contradicted term, and forgets it on BacktrackTo.
func TestPropagateRemembersContradictedIncompatibilities(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	st.Add(dep("root", versionset.Exactly(1), "a", versionset.Range(1, 3)))
	// Needs a in [5,6), which root 1 rules out.
	x := st.Add(NewIncompatibility(KindDerived, map[string]tm{
		"b": pos(versionset.Range(1, 2)),
		"a": pos(versionset.Range(5, 6)),
	}))

	ps.Decide("root", versionset.Exactly(1))
	if r := Propagate(ps, st, "root"); r.HasConflict() {
		t.Fatalf("unexpected conflict: %v", r.Conflict)
	}
	if _, ok := ps.contradicted[x]; !ok {
		t.Fatal("an incompatibility with a contradicted term was not remembered")
	}

	ps.BacktrackTo(0)
	if _, ok := ps.contradicted[x]; ok {
		t.Fatal("BacktrackTo kept a contradicted memo")
	}
}

// TestPropagateSkipsRememberedIncompatibility plants a memo entry for an
// incompatibility that is really satisfied: Propagate must skip it, not conflict.
func TestPropagateSkipsRememberedIncompatibility(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	st.Add(dep("root", versionset.Exactly(1), "a", versionset.Range(1, 3)))
	x := st.Add(NewIncompatibility(KindDerived, map[string]tm{"a": pos(versionset.Range(1, 3))}))

	ps.Decide("root", versionset.Exactly(1))
	ps.markContradicted(x)
	if r := Propagate(ps, st, "root"); r.HasConflict() {
		t.Fatal("Propagate examined an incompatibility it had remembered as contradicted")
	}
}

// TestPropagateFiresAgainAfterBacktrack is the other half: an incompatibility
// skipped as contradicted must act again once the contradiction is undone.
func TestPropagateFiresAgainAfterBacktrack(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	st.Add(dep("root", versionset.Exactly(1), "a", versionset.Range(1, 3)))
	st.Add(dep("root", versionset.Exactly(2), "a", versionset.Range(5, 6)))
	// b in [1,2) and a in [5,6) cannot both hold.
	st.Add(NewIncompatibility(KindDerived, map[string]tm{
		"b": pos(versionset.Range(1, 2)),
		"a": pos(versionset.Range(5, 6)),
	}))

	// With root 1, a is in [1,3): the incompatibility is contradicted.
	ps.Decide("root", versionset.Exactly(1))
	if r := Propagate(ps, st, "root"); r.HasConflict() {
		t.Fatalf("unexpected conflict: %v", r.Conflict)
	}
	if _, ok := ps.Accumulated("b"); ok {
		t.Fatal("b was derived while the incompatibility was contradicted")
	}

	// With root 2, a is in [5,6): it now forces b out of [1,2).
	ps.BacktrackTo(0)
	ps.Decide("root", versionset.Exactly(2))
	if r := Propagate(ps, st, "root"); r.HasConflict() {
		t.Fatalf("unexpected conflict: %v", r.Conflict)
	}
	got, ok := ps.Accumulated("b")
	if !ok {
		t.Fatal("nothing derived for b after backtracking")
	}
	if got.IsPositive() || !got.Set().Equal(versionset.Range(1, 2)) {
		t.Errorf("b term = %v, want not [1,2)", got)
	}
}
