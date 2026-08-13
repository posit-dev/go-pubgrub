// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"strings"
	"testing"

	"github.com/posit-dev/go-pubgrub/versionset"
)

// --- §8: eligibility ---

func TestEligible(t *testing.T) {
	ps := newPS()
	ps.Derive("a", pos(versionset.Range(1, 5)), nil)
	ps.Derive("b", neg(versionset.AtLeast(3)), nil)

	for _, tc := range []struct {
		name string
		pkg  string
		set  set
		want bool
	}{
		{"within the accumulated term", "a", versionset.Exactly(3), true},
		{"outside the accumulated term", "a", versionset.Exactly(9), false},
		{"on the exclusive upper bound", "a", versionset.Exactly(5), false},
		{"outside a negative term's set", "b", versionset.Exactly(1), true},
		{"inside a negative term's set", "b", versionset.Exactly(4), false},
		{"nothing accumulated at all", "unknown", versionset.Exactly(1), true},
		{"the empty set is never eligible", "unknown", versionset.Empty(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ps.Eligible(tc.pkg, tc.set); got != tc.want {
				t.Errorf("Eligible(%q, %v) = %v, want %v", tc.pkg, tc.set, got, tc.want)
			}
		})
	}
}

// TestDecideRejectsAnIneligibleVersion pins the precondition §8 states, on the
// state that used to corrupt silently: deciding a version outside the accumulated
// term intersects that term down to Positive(∅), and term.Relation tests Satisfied
// before Contradicted, so an always-false accumulated term answers Satisfied to
// EVERY term about the package, of either polarity.
//
// Classify then reports arbitrary unrelated incompatibilities as fully satisfied
// conflicts and §7 builds a proof tree out of one. Vacuously those answers are
// "correct" — an inconsistent assumption set entails everything — which is exactly
// why the caller cannot tell a real conflict from state it has already wrecked.
// Nothing afterwards points back at the decision that caused it, so it fails here.
func TestDecideRejectsAnIneligibleVersion(t *testing.T) {
	ps := newPS()
	ps.Derive("a", pos(versionset.Range(1, 3)), nil)

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("Decide accepted a version outside the accumulated term; every later " +
				"relation test about that package would answer Satisfied")
		}
		if msg, ok := recovered.(string); !ok || !strings.Contains(msg, "ineligible") {
			t.Errorf("panic value = %v, want a message naming the ineligible decision", recovered)
		}
	}()

	ps.Decide("a", versionset.Exactly(9))
}

// TestDecideRejectsAnIneligibleVersionMeasuredEffect records what the guard
// prevents, by demonstrating the mechanism on the accumulated term directly rather
// than through the corrupted state it would produce.
func TestDecideRejectsAnIneligibleVersionMeasuredEffect(t *testing.T) {
	accumulated := pos(versionset.Range(1, 3))
	corrupted := accumulated.Intersect(pos(versionset.Exactly(9)))

	if !corrupted.IsAlwaysFalse() {
		t.Fatal("precondition: an ineligible decision should empty the accumulated term")
	}

	unrelated := pos(versionset.Exactly(100))
	if corrupted.Relation(unrelated) != satisfiedRelation() {
		t.Error("an always-false accumulated term answers Satisfied to an unrelated positive " +
			"term, which is what turns a caller error into a plausible-looking conflict")
	}
	if corrupted.Relation(neg(versionset.Exactly(100))) != satisfiedRelation() {
		t.Error("...and to its negation as well, of either polarity")
	}
}

// --- §8: decision making ---

func TestMakeDecisionPrefersFewestVersionsThenLatest(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	// wide has three candidates, narrow has two. §8 prefers the narrower, to
	// surface an eventual conflict in fewer wasted iterations.
	u := newUniverse().
		with("wide", 1).with("wide", 2).with("wide", 3).
		with("narrow", 1).with("narrow", 2)

	ps.Derive("wide", pos(versionset.AtLeast(1)), nil)
	ps.Derive("narrow", pos(versionset.AtLeast(1)), nil)

	outcome, err := MakeDecision(ps, st, u)
	if err != nil {
		t.Fatalf("MakeDecision: %v", err)
	}
	if !outcome.Decided {
		t.Fatal("expected a decision")
	}
	if outcome.Package != "narrow" {
		t.Errorf("decided %q, want \"narrow\": §8 prefers the package with the fewest "+
			"candidate versions", outcome.Package)
	}
	if !outcome.Version.Equal(versionset.Exactly(2)) {
		t.Errorf("decided version %v, want 2 — §8 prefers the latest matching version",
			outcome.Version)
	}
}

// TestMakeDecisionTieBreakIsChronological pins the choice §11 item 2 records as
// left open by the sources: neither says what to do when two packages have the
// same number of candidates.
//
// Any legal choice preserves correctness, so what is being pinned is
// DETERMINISM — the alternative is a solver whose trace, and whose choice of which
// package to name first in an error, changes between runs on identical input
// because it followed Go's map iteration order.
func TestMakeDecisionTieBreakIsChronological(t *testing.T) {
	u := newUniverse().with("first", 1).with("second", 1)

	// Ten runs, because a map-order tie-break would need several to show itself.
	for i := 0; i < 10; i++ {
		ps := newPS()
		st := NewStore[string, set]()
		ps.Derive("first", pos(versionset.AtLeast(1)), nil)
		ps.Derive("second", pos(versionset.AtLeast(1)), nil)

		outcome, err := MakeDecision(ps, st, u)
		if err != nil {
			t.Fatalf("MakeDecision: %v", err)
		}
		if outcome.Package != "first" {
			t.Fatalf("run %d decided %q, want \"first\": on a tie the package whose first "+
				"positive derivation came earliest wins", i, outcome.Package)
		}
	}
}

// TestMakeDecisionIgnoresPackagesWithoutAPositiveDerivation pins §8's eligibility.
// A negative derivation says only "not these versions", which is not a reason to
// select anything — treating it as one would make the solver install packages
// nobody asked for.
func TestMakeDecisionIgnoresPackagesWithoutAPositiveDerivation(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()
	u := newUniverse().with("unwanted", 1).with("decided", 1)

	ps.Derive("unwanted", neg(versionset.AtLeast(5)), nil)
	ps.Derive("decided", pos(versionset.Exactly(1)), nil)
	ps.Decide("decided", versionset.Exactly(1))

	outcome, err := MakeDecision(ps, st, u)
	if err != nil {
		t.Fatalf("MakeDecision: %v", err)
	}
	if !outcome.Done {
		t.Errorf("decided %q; nothing is outstanding, so §8 reports done and §4 calls that "+
			"success", outcome.Package)
	}
}

// TestMakeDecisionMaterializesDependenciesLazily pins the laziness §8 calls
// load-bearing: requirements are instantiated only for the version actually being
// considered.
func TestMakeDecisionMaterializesDependenciesLazily(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()
	u := newUniverse().
		with("a", 1, requirement("dep", versionset.Exactly(7))).
		with("a", 2, requirement("dep", versionset.Exactly(8))).
		with("dep", 7).with("dep", 8)

	ps.Derive("a", pos(versionset.AtLeast(1)), nil)

	if _, err := MakeDecision(ps, st, u); err != nil {
		t.Fatalf("MakeDecision: %v", err)
	}

	if u.calls["a"] != 1 {
		t.Errorf("Dependencies called %d times for a, want 1 — only for the version being "+
			"considered", u.calls["a"])
	}
	// a 2 was chosen, so only its requirement exists. a 1's must not.
	if len(st.Mentioning("dep")) != 1 {
		t.Fatalf("%d incompatibilities mention dep, want 1", len(st.Mentioning("dep")))
	}
	got := st.Mentioning("dep")[0]
	if term, _ := got.Term("dep"); !term.Equal(neg(versionset.Exactly(8))) {
		t.Errorf("materialized %v, want the requirement of the version actually considered", got)
	}
}

// TestMakeDecisionUsesTheProviderDependerSpan pins §8's collapsing of adjacent
// versions that share a requirement into one incompatibility spanning them, which
// keeps the incompatibility count proportional to the number of distinct
// requirements rather than the number of versions.
func TestMakeDecisionUsesTheProviderDependerSpan(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()
	span := versionset.Range(1, 3)
	u := newUniverse().
		with("a", 1, spanning("dep", versionset.Exactly(7), span)).
		with("a", 2, spanning("dep", versionset.Exactly(7), span)).
		with("dep", 7)

	ps.Derive("a", pos(versionset.AtLeast(1)), nil)
	if _, err := MakeDecision(ps, st, u); err != nil {
		t.Fatalf("MakeDecision: %v", err)
	}

	inc := st.Mentioning("dep")[0]
	if got, _ := inc.Term("a"); !got.Equal(pos(span)) {
		t.Errorf("depender term = %v, want %v — the span every version sharing the "+
			"requirement covers", got, pos(span))
	}
}

func TestMakeDecisionRejectsADependerSpanExcludingTheVersion(t *testing.T) {
	// A span that does not contain the version being considered states the
	// requirement about versions that do not have it, while failing to state it
	// about the one that does.
	ps := newPS()
	st := NewStore[string, set]()
	u := newUniverse().
		with("a", 1, spanning("dep", versionset.Exactly(7), versionset.Range(5, 9))).
		with("dep", 7)

	ps.Derive("a", pos(versionset.AtLeast(1)), nil)
	_, err := MakeDecision(ps, st, u)
	if err == nil {
		t.Fatal("expected an error for a depender span that excludes the version considered")
	}
	if !strings.Contains(err.Error(), "depender range") {
		t.Errorf("err = %v, want the depender-span rejection", err)
	}
}

// TestMakeDecisionPreCommitCheckDeclines pins §8's pre-commit check, which is what
// stops the search from ever recording a decision already known at commit time to
// be wrong.
//
// The shape is §10's step 7: the candidate's own requirement turns out to be
// violated by state that already exists, so no decision is recorded and
// propagation is handed the incompatibility instead.
func TestMakeDecisionPreCommitCheckDeclines(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	// json 1 requires http >= 250, and http is already decided at 200.
	u := newUniverse().
		with("json", 100, spanning("http", versionset.AtLeast(v250), versionset.LessThan(v200))).
		with("json", 200).
		with("http", 100).with("http", 200)

	ps.Derive("http", pos(versionset.AtLeast(v100)), nil)
	ps.Decide("http", versionset.Exactly(v200))
	ps.Derive("json", pos(versionset.Range(v100, v200)), nil)

	outcome, err := MakeDecision(ps, st, u)
	if err != nil {
		t.Fatalf("MakeDecision: %v", err)
	}
	if outcome.Decided {
		t.Error("committing this version would violate the requirement just added, so §8 must " +
			"not record the decision")
	}
	if outcome.Package != "json" {
		t.Errorf("resume package = %q, want \"json\"", outcome.Package)
	}
	if _, decided := ps.DecisionFor("json"); decided {
		t.Error("no decision may be recorded for json")
	}
	// The requirement still goes in: propagation needs it to find the conflict.
	if len(st.Mentioning("json")) == 0 {
		t.Error("the candidate's requirements must be added even though the decision was declined")
	}
	if satisfaction, _ := Classify(ps, st.Mentioning("json")[0]); satisfaction != FullySatisfied {
		t.Error("propagation should now find that incompatibility fully satisfied by prior " +
			"state alone, which is what hands the problem to conflict resolution")
	}
}

// TestMakeDecisionNoVersionsForbidsTheRange pins §8's unavailability case, which
// covers both "no published version matches" and "this version's metadata cannot be
// fetched" — the specification models them identically.
func TestMakeDecisionNoVersionsForbidsTheRange(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()
	u := newUniverse().with("a", 1).with("a", 2)

	// Nothing published lies in [5,9).
	ps.Derive("a", pos(versionset.Range(5, 9)), nil)

	outcome, err := MakeDecision(ps, st, u)
	if err != nil {
		t.Fatalf("MakeDecision: %v", err)
	}
	if outcome.Decided {
		t.Fatal("nothing is available, so nothing may be decided")
	}
	if u.calls["a"] != 0 {
		t.Error("the dependencies of a version that does not exist must not be requested")
	}

	if st.Len() != 1 {
		t.Fatalf("store holds %d incompatibilities, want 1", st.Len())
	}
	forbidden := st.All()[0]
	if got, _ := forbidden.Term("a"); !got.Equal(pos(versionset.Range(5, 9))) {
		t.Errorf("forbidden term = %v, want positive [5,9) — the ENTIRE currently required "+
			"range, which is what makes it act on immediately", got)
	}
	if forbidden.Kind() != KindNoVersions {
		t.Errorf("kind = %v, want no versions; the kind is what lets an explanation say why", forbidden.Kind())
	}
}

// TestMakeDecisionRejectsAVersionOutsideTheAllowedSet pins that the solver does not
// trust the Provider on this point. A decision outside the accumulated term
// corrupts the partial solution in a way no later error points back to, so a
// misbehaving provider has to surface as an error from the solve.
//
// # The offered version is PUBLISHED, and that is the whole design of this test
//
// An earlier version offered version 99, which the test universe does not publish.
// That passed — but for the wrong reason: the error came from Dependencies
// rejecting an unknown version, not from the eligibility check. Removing the check
// entirely left the test passing, which is how it was found. The version offered
// here exists, so Dependencies succeeds and the eligibility check is the only thing
// that can reject it — and with the check removed, Decide's own guard panics
// instead, which is the corruption the check exists to convert into an error.
func TestMakeDecisionRejectsAVersionOutsideTheAllowedSet(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()
	u := newUniverse().with("a", 1).with("a", 2).with("a", 5)
	u.offer = map[string]int64{"a": 5}

	// 5 is published, but the accumulated term allows only [1,3).
	ps.Derive("a", pos(versionset.Range(1, 3)), nil)

	_, err := MakeDecision(ps, st, u)
	if err == nil {
		t.Fatal("expected an error: the provider offered a version outside the allowed set")
	}
	if !strings.Contains(err.Error(), "outside the allowed set") {
		t.Errorf("err = %v, want the eligibility rejection; any other error means this test "+
			"is passing for a different reason than it claims", err)
	}
	if _, decided := ps.DecisionFor("a"); decided {
		t.Error("no decision may be recorded for an ineligible version")
	}
}

func TestMakeDecisionPropagatesProviderErrors(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()
	u := newUniverse().with("a", 1)
	u.failFor = "a"

	ps.Derive("a", pos(versionset.AtLeast(1)), nil)
	_, err := MakeDecision(ps, st, u)
	if err == nil {
		t.Fatal("a provider failure must be reported, not read as an empty candidate list — " +
			"the difference is between an unavailable index and a package that does not exist")
	}
	// Pin the provider's own error as the cause, so this cannot pass on some
	// unrelated failure.
	if !strings.Contains(err.Error(), "index unavailable") {
		t.Errorf("err = %v, want the provider's own error wrapped", err)
	}
	if st.Len() != 0 {
		t.Error("a provider failure must not be recorded as \"no versions available\": that " +
			"would forbid a range on the strength of an unreachable index")
	}
}

// TestMakeDecisionPrefersUnavailabilityOverEveryAvailablePackage pins the ordering
// that the found/rank split had to make explicit.
//
// Before the split, unavailability was a count of zero, and zero is the numeric
// minimum — so an unavailable package won the heuristic comparison for free, and
// MakeDecision reached its unavailability case on the first round in which any
// eligible package had nothing available. With a separate boolean that is no longer
// automatic.
//
// # Why this is worth a test of its own
//
// Losing it does not produce a wrong answer, which is exactly why it needs
// pinning. The solve would still terminate and still reach the same conclusion,
// just after deciding the available packages first, requesting their dependencies
// and adding their incompatibilities on the way — more work, a different trace, and
// a different package named first in an error. Nothing fails loudly.
//
// # What makes this test able to fail
//
// The universe reports math.MaxInt as the rank of an unavailable package rather
// than 0 (legal: the contract says rank is ignored when found is false). That is
// deliberate. With 0 there, a solver that dropped the found comparison and ordered
// by rank alone would still put unavailability first and this test would pass
// while asserting nothing. The hostile rank makes that regression fail.
func TestMakeDecisionPrefersUnavailabilityOverEveryAvailablePackage(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	// "available" has exactly one candidate, so its rank is 1 — the smallest a
	// found package can report. "missing" publishes nothing in the required range.
	u := newUniverse().with("available", 1).with("missing", 1)

	// Derived FIRST, so neither the chronological tie-break nor iteration order can
	// be what hands the win to "missing".
	ps.Derive("available", pos(versionset.Exactly(1)), nil)
	ps.Derive("missing", pos(versionset.Range(5, 9)), nil)

	outcome, err := MakeDecision(ps, st, u)
	if err != nil {
		t.Fatalf("MakeDecision: %v", err)
	}

	if outcome.Decided {
		t.Fatalf("decided %q %v, but the unavailable package should have been taken first: "+
			"unavailability must be ordered ahead of every available package, whatever rank "+
			"the provider reported for it", outcome.Package, outcome.Version)
	}
	if outcome.Package != "missing" {
		t.Errorf("outcome package = %q, want \"missing\"", outcome.Package)
	}
	if u.calls["available"] != 0 {
		t.Error("the available package's dependencies were requested, so the solver worked " +
			"through it before noticing the unavailable one — that is the regression this " +
			"test exists for")
	}

	if st.Len() != 1 {
		t.Fatalf("store holds %d incompatibilities, want 1 (the forbidden range)", st.Len())
	}
	forbidden := st.All()[0]
	if forbidden.Kind() != KindNoVersions {
		t.Errorf("kind = %v, want no versions", forbidden.Kind())
	}
	if got, _ := forbidden.Term("missing"); !got.Equal(pos(versionset.Range(5, 9))) {
		t.Errorf("forbidden term = %v, want positive [5,9)", got)
	}
}

// TestMakeDecisionRankChangesTheOrderNotTheValidity pins what rank actually is: an
// input to the search ORDER that cannot make a resolution invalid.
//
// # What this deliberately does NOT assert
//
// An earlier version solved one scenario twice and required IDENTICAL pins. That
// was worthless twice over. The scenario had exactly one solution, so identical
// pins were guaranteed however rank was handled — it passed with the ordering
// comparison reversed, and passed with its own constantRank lever deleted. And the
// invariant it claimed is false: a constant rank CAN move pins to a different valid
// answer, which is measured behaviour against a real index and is exactly why the
// contract warns against constants.
//
// So this asserts the two things that are both true and checkable: the order
// changes, and the answer stays valid.
func TestMakeDecisionRankChangesTheOrderNotTheValidity(t *testing.T) {
	// wide has three candidates and narrow two, and wide is derived FIRST. So the
	// two strategies genuinely disagree: an exact rank prefers narrow for having
	// fewer, while a constant rank makes every package tie and hands it to the
	// earliest derivation, which is wide.
	//
	// Asserted through MakeDecision rather than a whole Solve, so the derive order
	// is stated by the test instead of falling out of propagation.
	pick := func(constant bool) string {
		t.Helper()
		ps := newPS()
		st := NewStore[string, set]()
		u := newUniverse().
			with("wide", 1).with("wide", 2).with("wide", 3).
			with("narrow", 1).with("narrow", 2)
		u.constantRank = constant

		ps.Derive("wide", pos(versionset.AtLeast(1)), nil)
		ps.Derive("narrow", pos(versionset.AtLeast(1)), nil)

		outcome, err := MakeDecision(ps, st, u)
		if err != nil {
			t.Fatalf("MakeDecision (constantRank=%v): %v", constant, err)
		}
		if !outcome.Decided {
			t.Fatalf("MakeDecision (constantRank=%v) decided nothing", constant)
		}
		return outcome.Package
	}

	exactPick, constantPick := pick(false), pick(true)

	if exactPick == constantPick {
		t.Errorf("both strategies chose %q, so rank changed nothing here and this test is not "+
			"exercising the heuristic it claims to", exactPick)
	}
	if exactPick != "narrow" {
		t.Errorf("exact rank chose %q, want \"narrow\": fewer candidates wins", exactPick)
	}
	if constantPick != "wide" {
		t.Errorf("constant rank chose %q, want \"wide\": every rank ties, so the earliest "+
			"derivation wins", constantPick)
	}

	// And the invariant rank must never break: a constant still yields a VALID
	// resolution. Weaker than pin identity on purpose — see above.
	u := newUniverse().
		with("wide", 1).with("wide", 2).with("wide", 3).
		with("narrow", 1).with("narrow", 2).
		with("root", 0,
			requirement("wide", versionset.AtLeast(1)),
			requirement("narrow", versionset.AtLeast(1)))
	u.constantRank = true

	sol, err := New("root", versionset.Exactly(0), u).Solve()
	if err != nil {
		t.Fatalf("Solve with a constant rank: %v", err)
	}
	for _, pkg := range []string{"wide", "narrow"} {
		ver, ok := sol.Selected[pkg]
		if !ok {
			t.Errorf("constant rank: %s is not pinned, but root requires it", pkg)
			continue
		}
		if !versionset.IsSubsetOf(ver, versionset.AtLeast(1)) {
			t.Errorf("constant rank: %s pinned %v, outside root's requirement >=1", pkg, ver)
		}
	}
}

// TestMakeDecisionUnavailableTieBreakIsChronological covers the other half of
// preferCandidate's ordering, which nothing else reaches.
//
// When two eligible packages are BOTH unavailable the incumbent is kept, so the
// earliest derivation still wins. Flipping that comparison left every other test in
// the repo green: which unavailable package gets its KindNoVersions incompatibility
// first decides which one a failure explanation names first, and no other fixture
// has two simultaneously-eligible unavailable packages.
func TestMakeDecisionUnavailableTieBreakIsChronological(t *testing.T) {
	// Ten runs, because a map-order tie-break needs several to show itself.
	for i := 0; i < 10; i++ {
		ps := newPS()
		st := NewStore[string, set]()
		u := newUniverse().with("earlier", 1).with("later", 1)

		// Neither publishes anything in [5,9), so both are unavailable.
		ps.Derive("earlier", pos(versionset.Range(5, 9)), nil)
		ps.Derive("later", pos(versionset.Range(5, 9)), nil)

		outcome, err := MakeDecision(ps, st, u)
		if err != nil {
			t.Fatalf("MakeDecision: %v", err)
		}
		if outcome.Decided {
			t.Fatal("neither package has a version, so nothing may be decided")
		}
		if outcome.Package != "earlier" {
			t.Fatalf("run %d took %q first, want \"earlier\": on a tie between two unavailable "+
				"packages the earliest derivation wins, which is what keeps the package named "+
				"first in an explanation stable", i, outcome.Package)
		}
		if st.Len() != 1 {
			t.Fatalf("store holds %d incompatibilities, want 1", st.Len())
		}
		if _, ok := st.All()[0].Term("earlier"); !ok {
			t.Errorf("the forbidden term is about %v, want it about \"earlier\"", st.All()[0].Terms())
		}
	}
}

// TestMakeDecisionRejectsAnEmptyBestWithItsOwnMessage pins the diagnostic added
// alongside the containment guard.
//
// ∅ passes IsSubsetOf — it is a subset of everything — so it is ps.Eligible that
// rejects it, and before this it shared the containment message. "Outside the
// allowed set" is true of ∅ only lawyerly and points a provider author at a range
// problem they do not have; the actual fault is that found said something is
// available and best names nothing. The message is the whole feature here, so the
// message is what this asserts.
func TestMakeDecisionRejectsAnEmptyBestWithItsOwnMessage(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	// A version IS in range, so the fixture reports found = true; offerSet then
	// overrides best with the empty set. That is the found/best disagreement.
	u := newUniverse().with("a", 1).with("a", 2)
	u.offerSet = map[string]set{"a": versionset.Empty()}

	ps.Derive("a", pos(versionset.AtLeast(1)), nil)

	_, err := MakeDecision(ps, st, u)
	if err == nil {
		t.Fatal("a best of ∅ must be refused: ps.Decide panics on it, and the empty set is " +
			"not a version any decision can record")
	}
	if !strings.Contains(err.Error(), "found and best disagree") {
		t.Errorf("err = %v, want it to name the found/best disagreement rather than the "+
			"containment failure, which is what sends a provider author looking for a range "+
			"problem that is not there", err)
	}
	if _, decided := ps.DecisionFor("a"); decided {
		t.Error("no decision may be recorded for a refused candidate")
	}
}

// TestMakeDecisionRejectsARangeOverlappingTheAllowedSet pins the containment half of
// the best check.
//
// ps.Eligible tests NON-DISJOINTNESS, which coincides with containment only for a
// single version — and singleton-ness is the one property of best that cannot be
// checked, since versionset.Set has no singleton predicate. So before MakeDecision
// also required IsSubsetOf, a provider returning a range that merely OVERLAPPED
// allowed passed, was recorded as the chosen version, and reached Solution.Selected:
// a decision outside the accumulated term, by the one route the disjointness test
// cannot see. ps.Decide's guard did not catch it, and neither did Solve's final
// ViolatedBy pass.
func TestMakeDecisionRejectsARangeOverlappingTheAllowedSet(t *testing.T) {
	ps := newPS()
	st := NewStore[string, set]()

	u := newUniverse().with("a", 1).with("a", 2)
	// [1,10) overlaps the required [1,3) without being contained in it.
	u.offerSet = map[string]set{"a": versionset.Range(1, 10)}

	ps.Derive("a", pos(versionset.Range(1, 3)), nil)

	_, err := MakeDecision(ps, st, u)
	if err == nil {
		t.Fatal("a best that overlaps the allowed set without being inside it must be refused: " +
			"accepting it decides a version outside the accumulated term, which nothing " +
			"downstream reports")
	}
	if !strings.Contains(err.Error(), "outside the allowed set") {
		t.Errorf("err = %v, want it to name the allowed set", err)
	}
	if _, decided := ps.DecisionFor("a"); decided {
		t.Error("no decision may be recorded for a refused candidate")
	}
}

// satisfiedRelation names term.Satisfied without importing term into this file's
// assertions, keeping the test's vocabulary the same as the solver's.
func satisfiedRelation() interface{ String() string } {
	var ps psol
	ps.Derive("x", pos(versionset.Exactly(1)), nil)
	return ps.Relation("x", pos(versionset.Exactly(1)))
}
