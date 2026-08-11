// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"errors"
	"strings"
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// assertSelected checks the solution chose exactly these package/version pairs.
func assertSelected(t *testing.T, sol *Solution[string, set], want map[string]int64) {
	t.Helper()

	if len(sol.Selected) != len(want) {
		t.Errorf("selected %d packages, want %d: got %v", len(sol.Selected), len(want), sol.Selected)
	}
	for pkg, version := range want {
		got, ok := sol.Selected[pkg]
		if !ok {
			t.Errorf("%s was not selected, want %d", pkg, version)
			continue
		}
		if !got.Equal(versionset.Exactly(version)) {
			t.Errorf("%s = %v, want %d", pkg, got, version)
		}
	}
	for pkg := range sol.Selected {
		if _, ok := want[pkg]; !ok {
			t.Errorf("%s was selected but nothing required it", pkg)
		}
	}
}

// TestSolveSection10 runs §10's universe through the whole loop and checks it
// reaches §10's answer.
//
// TestSection10Trace checks the mechanism step by step; this checks the outcome,
// which is the part a step-by-step test cannot: that propagation, decision making
// and conflict resolution compose into a terminating search that finds the
// solution §10 hand-traced, rather than three pieces that are each individually
// right.
func TestSolveSection10(t *testing.T) {
	// app 1.0.0 (root) depends on http >=1.0.0. http 2.0.0 depends on
	// json [1.0.0,2.0.0); http 1.0.0 has no dependencies. json 1.0.0 depends on
	// http >=2.5.0, which no published http satisfies; json 2.0.0 has none.
	//
	// The depender spans are §8's convention: the lower bound is the first version
	// with the requirement and the upper bound the first version after that run
	// without it, each omitted where the run reaches the end of what is published.
	u := newUniverse().
		with("app", v100, spanning("http", versionset.AtLeast(v100), versionset.Exactly(v100))).
		with("http", v100).
		with("http", v200, spanning("json", versionset.Range(v100, v200), versionset.AtLeast(v200))).
		with("json", v100, spanning("http", versionset.AtLeast(v250), versionset.LessThan(v200))).
		with("json", v200)

	s := New("app", versionset.Exactly(v100), u)
	sol, err := s.Solve()
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}

	// §10: "Final solution: app 1.0.0, http 1.0.0 — json never needed to be
	// considered again."
	assertSelected(t, sol, map[string]int64{"app": v100, "http": v100})

	if len(sol.Order) == 0 || sol.Order[0] != "app" {
		t.Errorf("decision order = %v, want the root first", sol.Order)
	}

	// §10: the proof "generalized cleanly to no version of http in [2.0.0, 2.5.0)
	// will ever work, which is strictly more useful than specifically http 2.0.0
	// doesn't work". That generalization is the reason conflict resolution exists,
	// and a solver that reached the right answer by simply retrying versions one at
	// a time would pass every assertion above and fail this one.
	want := NewIncompatibility(KindDerived, map[string]tm{"http": pos(versionset.Range(v200, v250))})
	found := false
	for _, inc := range s.Store().All() {
		if !inc.Equal(want) {
			continue
		}
		found = true
		if !inc.IsDerived() {
			t.Error("I4 must record its causes; §9's explanation walk has nothing to follow otherwise")
		}
	}
	if !found {
		t.Errorf("the store does not hold %v, so the round of conflict resolution did not "+
			"happen: the search retried versions instead of generalizing", want)
	}

	// json was reconsidered and rejected, never selected.
	if _, ok := sol.Selected["json"]; ok {
		t.Error("json must not be in the solution")
	}
}

func TestSolveTrivialRoot(t *testing.T) {
	u := newUniverse().with("root", 1)

	sol, err := New("root", versionset.Exactly(1), u).Solve()
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	assertSelected(t, sol, map[string]int64{"root": 1})
}

func TestSolveTransitiveChain(t *testing.T) {
	u := newUniverse().
		with("root", 1, requirement("a", versionset.AtLeast(1))).
		with("a", 1).
		with("a", 2, requirement("b", versionset.Exactly(1))).
		with("b", 1)

	sol, err := New("root", versionset.Exactly(1), u).Solve()
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	// a 2 is the latest match, and it pulls in b.
	assertSelected(t, sol, map[string]int64{"root": 1, "a": 2, "b": 1})
}

// TestSolveBackjumpsPastAnIrrelevantDecision is the property §7.5 is named for:
// backtracking lands past every decision that had nothing to do with the root
// cause, in one step, rather than undoing one decision at a time.
//
// The universe is constructed for this test rather than taken from a source. `left`
// is decided first and is irrelevant to the conflict; `mid` then forces a version
// of `shared` that `root` has already excluded. A solver that undid one decision at
// a time would retract `left` on the way back and re-decide it; one that backjumps
// keeps it.
func TestSolveBackjumpsPastAnIrrelevantDecision(t *testing.T) {
	u := newUniverse().
		with("root", 1,
			requirement("left", versionset.AtLeast(1)),
			requirement("mid", versionset.AtLeast(1)),
			requirement("shared", versionset.LessThan(10))).
		// left has one candidate, so §8 decides it first and it never changes.
		with("left", 1).
		// mid 2 demands a version of shared that root has ruled out; mid 1 does not.
		with("mid", 1).
		with("mid", 2, requirement("shared", versionset.AtLeast(10))).
		with("shared", 1).with("shared", 20)

	s := New("root", versionset.Exactly(1), u)
	sol, err := s.Solve()
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}

	assertSelected(t, sol, map[string]int64{"root": 1, "left": 1, "mid": 1, "shared": 1})

	// left is decided once. A second decision for it would mean the backjump went
	// deeper than the root cause required and had to redo settled work.
	decisions := 0
	for _, a := range s.PartialSolution().Assignments() {
		if a.Decision && a.Package == "left" {
			decisions++
		}
	}
	if decisions != 1 {
		t.Errorf("left was decided %d times in the surviving partial solution, want 1", decisions)
	}
}

// TestSolveUnsolvable checks the failure path end to end, and that what comes back
// is a proof rather than a message.
func TestSolveUnsolvable(t *testing.T) {
	// root needs a >=2, and only a 1 is published.
	u := newUniverse().
		with("root", 1, requirement("a", versionset.AtLeast(2))).
		with("a", 1)

	_, err := New("root", versionset.Exactly(1), u).Solve()
	if err == nil {
		t.Fatal("expected the solve to fail")
	}

	var unsolvable *Unsolvable[string, set]
	if !errors.As(err, &unsolvable) {
		t.Fatalf("err is %T, want *Unsolvable — a caller has to be able to tell "+
			"\"these requirements conflict\" from \"the solve could not be carried out\"", err)
	}

	root := unsolvable.RootCause
	if root == nil {
		t.Fatal("Unsolvable must carry the root cause")
	}
	if !root.IsEmpty() && !isRootFailure(root, "root") {
		t.Errorf("root cause = %v, want either empty or a lone positive term about the root", root)
	}

	// The proof has to reach the facts that forced it, or §9 has nothing to say.
	leaves := externalLeaves(root, map[*Incompatibility[string, set]]bool{})
	if len(leaves) == 0 {
		t.Fatal("the root cause has no external facts under it")
	}
	kinds := map[Kind]bool{}
	for _, leaf := range leaves {
		kinds[leaf.Kind()] = true
	}
	if !kinds[KindNoVersions] {
		t.Errorf("the proof does not reach the \"no versions\" fact that actually caused the "+
			"failure; leaves were %v", leaves)
	}
}

// TestSolveRejectsASolutionViolatingAnAllNegativeIncompatibility pins the
// cross-check on §4's success criterion.
//
// §4 declares success when every package with a positive derivation has a matching
// decision. An incompatibility of two or more terms, all negative, says "a is in x
// OR b is in y" — and with neither package assigned every relation test about it is
// Inconclusive forever, so unit propagation never fires and §4's criterion reports
// a solution that violates it. The gap is in §4/§6 as specified, not in this
// implementation of them, so it is caught rather than papered over: the answer is
// checked against the whole incompatibility set in completed-world semantics before
// being returned.
func TestSolveRejectsASolutionViolatingAnAllNegativeIncompatibility(t *testing.T) {
	u := newUniverse().with("root", 1).with("a", 1).with("b", 1)

	s := New("root", versionset.Exactly(1), u)
	allNegative := NewIncompatibility(KindDerived, map[string]tm{
		"a": neg(versionset.Exactly(1)),
		"b": neg(versionset.Exactly(1)),
	})
	s.Store().Add(allNegative)

	// Nothing about it is ever entailed: it propagates nothing and never conflicts.
	if satisfaction, _ := Classify(s.PartialSolution(), allNegative); satisfaction != Unrelated {
		t.Fatal("precondition: an all-negative incompatibility classifies as unrelated with " +
			"nothing assigned")
	}

	if _, err := s.Solve(); err == nil {
		t.Error("Solve returned a solution that omits both a and b, which violates an " +
			"incompatibility in its own store; §4's criterion cannot see that, so the answer " +
			"has to be verified before it is returned")
	}
}

// TestSolveMaxRoundsIsASafetyValve covers the bound §11 item 7 suggests: neither
// prose source proves the OUTER loop terminates in a bounded number of rounds for
// arbitrary graphs, so a caller facing untrusted input has somewhere to put a limit
// instead of hanging. It is separate from correctness, and off by default.
func TestSolveMaxRoundsIsASafetyValve(t *testing.T) {
	u := newUniverse().
		with("root", 1, requirement("a", versionset.AtLeast(1))).
		with("a", 1)

	s := New("root", versionset.Exactly(1), u)
	s.MaxRounds = 1

	_, err := s.Solve()
	if err == nil {
		t.Fatal("expected the round limit to stop the solve")
	}
	if !strings.Contains(err.Error(), "gave up") {
		t.Errorf("err = %v, want the round-limit error; any other error means this universe "+
			"fails for an unrelated reason and the valve is untested", err)
	}
	var unsolvable *Unsolvable[string, set]
	if errors.As(err, &unsolvable) {
		t.Error("hitting the round limit is not a proof that no solution exists, and must not " +
			"be reported as one — the requirements here are satisfiable")
	}

	// And with no limit the same universe solves.
	if _, err := New("root", versionset.Exactly(1), u).Solve(); err != nil {
		t.Errorf("the same solve without a limit failed: %v", err)
	}
}

func TestSolveReportsProviderFailures(t *testing.T) {
	u := newUniverse().
		with("root", 1, requirement("a", versionset.AtLeast(1))).
		with("a", 1)
	u.failFor = "a"

	_, err := New("root", versionset.Exactly(1), u).Solve()
	if err == nil {
		t.Fatal("expected an error")
	}

	var unsolvable *Unsolvable[string, set]
	if errors.As(err, &unsolvable) {
		t.Error("a provider failure must not be reported as \"no solution exists\": the " +
			"requirements may be perfectly satisfiable and the index merely unreachable")
	}
}

// TestSolveSeedsTheRootAsANegativeFact pins how the root enters, which §11 item 4
// leaves to the implementation. Seeding it as "it is forbidden for the root to be
// anything other than its one version" means propagation derives the requirement
// and decision making decides it like any other package — so the root needs no
// special case in the loop, in §7's floor, or in §8's eligibility.
func TestSolveSeedsTheRootAsANegativeFact(t *testing.T) {
	u := newUniverse().with("root", 7)

	s := New("root", versionset.Exactly(7), u)
	seeded := s.Store().All()
	if len(seeded) != 1 {
		t.Fatalf("New seeded %d incompatibilities, want 1", len(seeded))
	}
	if seeded[0].Kind() != KindRoot {
		t.Errorf("seed kind = %v, want root", seeded[0].Kind())
	}
	got, ok := seeded[0].Term("root")
	if !ok {
		t.Fatal("the seed must be about the root package")
	}
	if got.IsPositive() {
		t.Error("the seed must be NEGATIVE: a positive term about the root is §7.4's terminal " +
			"failure shape, so seeding one would report every solve as unsolvable")
	}

	if _, err := s.Solve(); err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if _, decided := s.PartialSolution().DecisionFor("root"); !decided {
		t.Error("the root must end up decided through the ordinary decision path")
	}
}

// externalLeaves collects the incompatibilities with no causes reachable from inc,
// which are the external facts §9 reports as the reasons for a failure.
func externalLeaves(
	inc *Incompatibility[string, set], seen map[*Incompatibility[string, set]]bool,
) []*Incompatibility[string, set] {
	if inc == nil || seen[inc] {
		return nil
	}
	seen[inc] = true

	a, b, derived := inc.Causes()
	if !derived {
		return []*Incompatibility[string, set]{inc}
	}
	return append(externalLeaves(a, seen), externalLeaves(b, seen)...)
}
