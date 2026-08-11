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
	// Candidates reports the versions of pkg that lie within allowed: how many
	// there are, and which single one to try first.
	//
	// The count drives §8's heuristic and nothing else, so an approximation
	// changes which order things are tried, never whether the answer is correct.
	// It must be 0 exactly when no version of pkg lies within allowed, since that
	// is what the solver treats as "unavailable" — including the case where a
	// version exists but its metadata cannot be fetched, which §8 models
	// identically to the version not existing.
	//
	// best must be a single version, and must lie within allowed. The solver
	// rejects one that does not rather than trusting it, because a decision
	// outside the accumulated term corrupts the partial solution in a way that no
	// later error points back to.
	Candidates(pkg P, allowed S) (best S, count int, err error)

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

	if chosen.count == 0 {
		// §8's unavailability case. The forbidden range is exactly what the
		// accumulated derivations already required, so nothing else can contradict
		// it and propagation will act on it immediately.
		st.Add(NewIncompatibility(KindNoVersions, map[P]term.Term[S]{
			pkg: term.Positive(allowed),
		}))
		return DecisionOutcome[P, S]{Package: pkg}, nil
	}

	if !ps.Eligible(pkg, best) {
		// Refusing beats trusting: a decision outside the accumulated term makes
		// every later relation test about the package answer Satisfied, and
		// nothing afterwards points back here.
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

	// best is the version to try, and count how many lie within allowed. A count
	// of zero means nothing is available and best is meaningless.
	best  S
	count int
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
func chooseCandidate[P comparable, S versionset.Set[S]](
	ps *PartialSolution[P, S], provider Provider[P, S],
) (candidate[P, S], bool, error) {
	var chosen candidate[P, S]
	found := false

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

		best, count, err := provider.Candidates(a.Package, accumulated.Set())
		if err != nil {
			return candidate[P, S]{}, false, fmt.Errorf("solver: candidates for %s: %w",
				formatPackage(a.Package), err)
		}

		// Strictly fewer, so the earliest of a tie wins.
		if !found || count < chosen.count {
			chosen = candidate[P, S]{pkg: a.Package, allowed: accumulated.Set(), best: best, count: count}
			found = true
		}
	}

	return chosen, found, nil
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
