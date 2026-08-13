// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"fmt"

	"github.com/posit-dev/go-pubgrub/term"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// Provider supplies the package facts the solver cannot know: which versions
// exist, and what they require.
//
// The solver performs no I/O of its own — everything it learns arrives through
// this interface — which is what lets it be driven equally by a network index, an
// offline mirror, or a table of fixtures.
//
// # Both methods are asked lazily, and that is load-bearing
//
// Dependencies is only ever called for a version the solver is actually about to
// consider, never for the whole version universe up front. Eagerly instantiating
// every version's requirements would mean reasoning about ranges that will never
// be needed, and committing to concrete versions before knowing whether their
// constraints stay relevant. An implementation is free to cache, but should not
// pre-fetch on the solver's behalf.
type Provider[P comparable, S versionset.Set[S]] interface {
	// Candidates reports which version of pkg the solver should try first within
	// allowed, whether there is one at all, and how urgent this package is to
	// work on.
	//
	// # found is the correctness-bearing answer, and it is an EXISTENCE question
	//
	// found must be false exactly when no version of pkg can be used within
	// allowed — including the case where a version exists but its metadata cannot
	// be fetched, which §8 models identically to the version not existing. The
	// solver treats false as "unavailable": it derives a KindNoVersions
	// incompatibility and forbids the whole of allowed. Reporting false when
	// something is usable forbids a range that is fine; reporting true when
	// nothing is usable hands back a best the solver cannot act on.
	//
	// ⚠️ Because this is existence and not cardinality, a TRUE answer is
	// discharged by finding one usable version and stopping. Do not enumerate
	// allowed merely to count. That is the point of the split: a provider that
	// counts pays per candidate version, where one that answers existence pays per
	// package it actually decides.
	//
	// A FALSE answer is different and cannot be short-circuited: proving that
	// nothing in allowed is usable means examining all of it. That cost is
	// irreducible, and it is correctness-bearing. So what the split removes is
	// paying the exhaustive walk on EVERY package rather than only on the ones
	// where the answer really is "nothing" — a common-case saving, not a
	// worst-case one.
	//
	// # rank only orders the search
	//
	// rank feeds §8's PACKAGE-choice heuristic — which package to work on next, not
	// which version of it, since best is supplied directly. Both prose sources are
	// explicit that this is tunable rather than part of correctness, and Solve
	// verifies its answer against the whole incompatibility set regardless, so rank
	// cannot make a resolution WRONG.
	//
	// It is a hint in the strict sense: the solver only ever compares one rank
	// against another and never reads it as a quantity. Nothing requires it to be a
	// count, an upper bound, or non-negative. It also must not cost I/O that
	// answering found did not already require.
	//
	// Counting the versions in allowed BEFORE testing usability is the intended
	// implementation — free, since that list has to exist anyway, and on everything
	// measured it preserved the ordering an exact count produced.
	//
	// ⚠️ rank cannot make a resolution wrong, but it CAN change which of several
	// legal resolutions is found. A constant disables the heuristic entirely, and
	// measured against a real index that silently moved pins to a different valid
	// answer. Legal, cheap, and a bad idea: return something with signal in it.
	//
	// rank is ignored entirely when found is false. Unavailability is ordered ahead
	// of every available package by the solver itself, so an implementation neither
	// needs to encode that in rank nor may rely on a sentinel value to achieve it.
	//
	// # best, and what the solver does NOT verify about it
	//
	// best must be a single version lying within allowed. Containment is enforced:
	// MakeDecision requires it explicitly, because ps.Eligible alone tests only
	// that best is not DISJOINT from what has accumulated, which coincides with
	// containment for a single version and not for a range.
	//
	// Singleton-ness is NOT checked, because versionset.Set has no singleton
	// predicate and none can be derived from the five methods it has. A range that
	// happens to sit inside allowed is therefore accepted and recorded as the chosen
	// version, reaching Solution.Selected. That is a provider bug with no loud
	// failure attached. Do not do it.
	//
	// best is never acted on when found is false.
	Candidates(pkg P, allowed S) (best S, found bool, rank int, err error)

	// Dependencies reports what pkg at the given single version requires.
	//
	// Returning no dependencies is normal and means the version constrains
	// nothing else.
	Dependencies(pkg P, version S) ([]Dependency[P, S], error)
}

// Dependency is one requirement of one version: some version of Package within
// Allowed must be selected.
type Dependency[P comparable, S versionset.Set[S]] struct {
	// Package is what is required.
	Package P

	// Allowed is the set of versions of Package that would satisfy the
	// requirement.
	Allowed S

	// Depender optionally widens the incompatibility to every version of the
	// DEPENDING package that shares this requirement, which §8 asks for: adjacent
	// versions frequently have byte-identical requirements, and collapsing them
	// keeps the incompatibility count proportional to the number of distinct
	// requirements rather than the number of versions. Its bounds are the first
	// version having the requirement and the first version after that run not
	// having it, each omitted when the run reaches the end of what is published.
	//
	// Leave it empty to mean "only the version being considered", which is always
	// correct and only ever costs extra incompatibilities. A Depender that does
	// NOT contain the version being considered is a Provider bug and is rejected:
	// it would state the requirement about versions that do not have it while
	// failing to state it about the one that does.
	Depender S
}

// DecisionOutcome is the result of one turn of §8's decision making.
type DecisionOutcome[P comparable, S versionset.Set[S]] struct {
	// Package is where unit propagation should resume. Meaningless when Done.
	Package P

	// Version is the version decided, when Decided.
	Version S

	// Decided reports whether a decision was actually recorded. It is false when
	// §8's pre-commit check declined the candidate, or when no version was
	// available at all — in both cases an incompatibility was added instead and
	// propagation has something to do about it.
	Decided bool

	// Done reports §8's termination signal: no package has an outstanding
	// positive derivation without a decision, so per §4 the partial solution is
	// already total and the solve has succeeded.
	Done bool
}

// MakeDecision implements §8: choose one package and version to commit to,
// materializing that version's dependencies as incompatibilities first.
//
// This is the only place a guess enters the system; everything else is deduction.
// It may add incompatibilities to st and may append one decision to ps.
//
// # It can decline to decide, and that is not a failure
//
// Three of its four outcomes record no decision:
//
//   - Done: nothing is outstanding, so the solve is finished.
//   - The candidate's own dependencies, once added, would already be violated —
//     §8's pre-commit check. Recording the decision would assert something known
//     at commit time to be wrong, so it is left unrecorded and propagation is
//     handed the new incompatibility instead: it will either find it already
//     fully satisfied (a conflict, for §7 to resolve) or almost satisfied,
//     yielding a derivation that rules out this and adjacent versions.
//   - No version of the package is available within what has accumulated. A lone
//     forbidden positive term over the whole required range goes in, and
//     propagation turns it into the information needed to try the next
//     possibility or to surface the failure.
func MakeDecision[P comparable, S versionset.Set[S]](
	ps *PartialSolution[P, S], st *Store[P, S], provider Provider[P, S],
) (DecisionOutcome[P, S], error) {
	var zero DecisionOutcome[P, S]

	chosen, eligible, err := chooseCandidate(ps, provider)
	if err != nil {
		return zero, err
	}
	if !eligible {
		// §8's termination signal: nothing is eligible.
		return DecisionOutcome[P, S]{Done: true}, nil
	}
	pkg, allowed, best := chosen.pkg, chosen.allowed, chosen.best

	if !chosen.found {
		// §8's unavailability case. The forbidden range is exactly what the
		// accumulated derivations already required, so nothing else can contradict
		// it and propagation will act on it immediately.
		//
		// Built from pkg and allowed alone — both computed here from the partial
		// solution, never supplied by the provider — so this needs nothing from
		// Candidates beyond the fact that nothing is available. chosen.best is
		// meaningless on this path and is never acted on.
		st.Add(NewIncompatibility(KindNoVersions, map[P]term.Term[S]{
			pkg: term.Positive(allowed),
		}))
		return DecisionOutcome[P, S]{Package: pkg}, nil
	}

	// Refusing beats trusting: a decision outside the accumulated term makes every
	// later relation test about the package answer Satisfied, and nothing
	// afterwards points back here.
	//
	// ⚠️ Both halves are needed, and ps.Eligible alone is NOT enough. Eligible tests
	// that best is not DISJOINT from what has accumulated, which coincides with
	// containment only for a single version — and singleton-ness is the one property
	// of best that cannot be checked, since versionset.Set has no singleton
	// predicate. So a provider returning a RANGE that merely overlaps allowed passed
	// this check, was decided, and reached Solution.Selected: a version outside the
	// accumulated term, arriving by the one route the disjointness test cannot see,
	// with ps.Decide's guard and Solve's final ViolatedBy pass both blind to it.
	// IsSubsetOf closes that.
	//
	// Eligible is still asked, because it also rejects the empty set, which
	// IsSubsetOf accepts: ∅ is a subset of everything.
	if !ps.Eligible(pkg, best) || !versionset.IsSubsetOf(best, allowed) {
		return zero, fmt.Errorf("solver: provider offered %s %v, which is outside the allowed set %v",
			formatPackage(pkg), best, allowed)
	}

	deps, err := provider.Dependencies(pkg, best)
	if err != nil {
		return zero, fmt.Errorf("solver: dependencies of %s %v: %w", formatPackage(pkg), best, err)
	}

	// §8: materialize the dependencies BEFORE deciding, so the pre-commit check
	// below can see them.
	added := make([]*Incompatibility[P, S], 0, len(deps))
	for _, d := range deps {
		depender := best
		if !d.Depender.IsEmpty() {
			if !versionset.IsSubsetOf(best, d.Depender) {
				return zero, fmt.Errorf(
					"solver: dependency of %s %v names a depender range %v that excludes that version",
					formatPackage(pkg), best, d.Depender)
			}
			depender = d.Depender
		}

		added = append(added, st.Add(NewIncompatibility(KindDependency, map[P]term.Term[S]{
			pkg:       term.Positive(depender),
			d.Package: term.Negative(d.Allowed),
		})))
	}

	// §8's pre-commit check, asked of a HYPOTHETICAL decision: would committing
	// this exact version violate one of the requirements just added?
	for _, inc := range added {
		if satisfaction, _ := classifyAssuming(ps, inc, pkg, best); satisfaction == FullySatisfied {
			return DecisionOutcome[P, S]{Package: pkg}, nil
		}
	}

	ps.Decide(pkg, best)
	return DecisionOutcome[P, S]{Package: pkg, Version: best, Decided: true}, nil
}

// candidate is one package §8 could decide, and what the provider says about it.
type candidate[P comparable, S versionset.Set[S]] struct {
	pkg P

	// allowed is the set the accumulated term permits: what the provider was
	// asked about, and the range §8's unavailability case forbids.
	allowed S

	// best is the version to try and found whether there is one; rank is the
	// provider's ordering hint. When found is false, nothing is available within
	// allowed, and both best and rank are meaningless.
	best  S
	found bool
	rank  int
}

// chooseCandidate applies §8's eligibility and heuristic. It reports false when
// no package is eligible, which is §8's termination signal.
//
// # Eligibility, and why a positive derivation is the test
//
// §8 requires a package to have at least one positive derivation and no decision.
// A positive derivation is something actually WANTING the package: a negative one
// says only "not these versions", which is not a reason to select anything. That
// also makes the accumulated term positive — a positive term intersected with
// anything stays positive — so the allowed SET is the accumulated term's set
// directly, with no polarity case analysis.
//
// # The heuristic, and the tie-break the sources leave open
//
// §8 prefers the package with the FEWEST candidate versions, to surface an
// eventual conflict as early as possible, and both prose sources are explicit that
// this is a tunable heuristic rather than part of correctness.
//
// Neither settles what to do on an exact tie (§11 item 2). This picks the package
// whose first positive derivation came EARLIEST, which is deterministic and
// independent of map iteration order — the alternative being a solver that
// produces different traces, and different packages named first in an error, from
// run to run on identical input.
//
// ⚠️ An unavailable package is ordered ahead of every available one, which is what
// makes MakeDecision reach its unavailability case in the round it appears rather
// than after deciding the available packages first. See preferCandidate: that
// ordering used to fall out of zero being the smallest legal count, and is now
// explicit. Note it orders solver ROUNDS and not provider work — Candidates is
// asked about every eligible package on every round regardless of who wins.
func chooseCandidate[P comparable, S versionset.Set[S]](
	ps *PartialSolution[P, S], provider Provider[P, S],
) (candidate[P, S], bool, error) {
	var chosen candidate[P, S]

	// Named for what it means to the caller — some package was eligible to work on
	// — and kept distinct from candidate.found, which is whether the PROVIDER has a
	// version for that package. The two are independent: an eligible package with
	// nothing available is exactly §8's unavailability case.
	eligible := false

	decided := make(map[P]bool)
	for _, a := range ps.Assignments() {
		if a.Decision {
			decided[a.Package] = true
		}
	}

	seen := make(map[P]bool)

	// Chronological order, so the tie-break is by first positive derivation and
	// nothing here depends on map iteration order.
	for _, a := range ps.Assignments() {
		if a.Decision || !a.Term.IsPositive() || decided[a.Package] || seen[a.Package] {
			continue
		}
		seen[a.Package] = true

		accumulated, ok := ps.Accumulated(a.Package)
		if !ok || !accumulated.IsPositive() {
			// Unreachable: a positive assignment about the package exists, and a
			// positive term intersected with anything stays positive.
			continue
		}

		best, available, rank, err := provider.Candidates(a.Package, accumulated.Set())
		if err != nil {
			return candidate[P, S]{}, false, fmt.Errorf("solver: candidates for %s: %w",
				formatPackage(a.Package), err)
		}

		next := candidate[P, S]{
			pkg:     a.Package,
			allowed: accumulated.Set(),
			best:    best,
			found:   available,
			rank:    rank,
		}
		if !eligible || preferCandidate(next, chosen) {
			chosen = next
			eligible = true
		}
	}

	return chosen, eligible, nil
}

// preferCandidate reports whether a should be worked on before b.
//
// ⚠️ Unavailability comes FIRST, ahead of every available package, regardless of
// rank. Before the found/rank split this was implicit: unavailability was a count
// of zero, zero is the smallest legal count, so it won the comparison for free.
// With a separate boolean that ordering has to be stated, and losing it does not
// fail loudly — MakeDecision would still reach its unavailability case eventually,
// just after deciding the available packages first, doing their dependency lookups
// and adding their incompatibilities on the way. Wrong-but-still-correct behaviour
// of exactly the kind that hides.
//
// Among available packages it is strictly-fewer, so the earliest of a rank tie
// wins and the caller's chronological iteration is what breaks it.
func preferCandidate[P comparable, S versionset.Set[S]](a, b candidate[P, S]) bool {
	if a.found != b.found {
		return !a.found
	}
	if !a.found {
		// Both unavailable: keep the incumbent, so the earliest still wins.
		return false
	}
	return a.rank < b.rank
}

// classifyAssuming reports how inc would classify if pkg were decided at version,
// without recording anything.
//
// §8's pre-commit check is specifically about what committing WOULD do, so it
// cannot be asked of the partial solution as it stands. It goes through the same
// classification as propagation, with one package's accumulated term overridden,
// rather than a second implementation of the same rules.
func classifyAssuming[P comparable, S versionset.Set[S]](
	ps *PartialSolution[P, S], inc *Incompatibility[P, S], pkg P, version S,
) (Satisfaction, P) {
	hypothetical := term.Positive(version)
	if accumulated, ok := ps.Accumulated(pkg); ok {
		hypothetical = accumulated.Intersect(hypothetical)
	}
	assumed := accumulation[P, S]{pkg: hypothetical}

	return classify(inc, func(p P, t term.Term[S]) term.Relation {
		if p == pkg {
			return assumed.relation(p, t)
		}
		return ps.Relation(p, t)
	})
}
