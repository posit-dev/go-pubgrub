// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"github.com/posit-dev/go-pubgrub/term"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// Satisfaction classifies an incompatibility against the partial solution.
type Satisfaction int

const (
	// Unrelated means at least one term is contradicted, or more than one is
	// still open. Nothing can be concluded.
	//
	// Zero value on purpose: an unclassified incompatibility must not read as a
	// conflict or as something to derive from.
	Unrelated Satisfaction = iota

	// AlmostSatisfied means every term but one is satisfied. The negation of the
	// remaining term is forced, and deriving it is unit propagation's whole job.
	AlmostSatisfied

	// FullySatisfied means every term is satisfied — the incompatibility is
	// violated, which is a conflict.
	FullySatisfied
)

// String implements fmt.Stringer.
func (s Satisfaction) String() string {
	switch s {
	case AlmostSatisfied:
		return "almost satisfied"
	case FullySatisfied:
		return "fully satisfied"
	case Unrelated:
		return "unrelated"
	default:
		return "unknown"
	}
}

// Classify reports how ps relates to inc, and for AlmostSatisfied which package's
// term is the one still open.
//
// An incompatibility is a conjunction, so:
//
//   - any term CONTRADICTED means the conjunction cannot hold: Unrelated.
//   - every term satisfied means it is violated: FullySatisfied.
//   - exactly one term inconclusive, rest satisfied: AlmostSatisfied, and that
//     term's negation is forced.
//   - two or more inconclusive: nothing forced yet, so Unrelated.
func Classify[P comparable, S versionset.Set[S]](
	ps *PartialSolution[P, S], inc *Incompatibility[P, S],
) (Satisfaction, P) {
	return classify(inc, ps.Relation)
}

// classify is Classify over any source of relations, so that §8's pre-commit
// check can ask the same question of a hypothetical decision without restating
// the rules. Two copies of this decision procedure would be free to disagree, and
// a disagreement between "what propagation thinks" and "what decision making
// thinks" is the kind that produces a wrong answer rather than an error.
func classify[P comparable, S versionset.Set[S]](
	inc *Incompatibility[P, S], relation func(P, term.Term[S]) term.Relation,
) (Satisfaction, P) {
	var unsatisfied P
	inconclusive := 0

	for pkg, t := range inc.terms {
		switch relation(pkg, t) {
		case term.Contradicted:
			// One term can never hold, so the conjunction cannot either.
			var zero P
			return Unrelated, zero
		case term.Inconclusive:
			inconclusive++
			if inconclusive > 1 {
				// Two open terms means nothing is forced: either could still go
				// either way.
				var zero P
				return Unrelated, zero
			}
			unsatisfied = pkg
		case term.Satisfied:
			// Nothing to record.
		}
	}

	if inconclusive == 0 {
		var zero P
		return FullySatisfied, zero
	}
	return AlmostSatisfied, unsatisfied
}

// PropagationResult reports how a propagation pass ended.
type PropagationResult[P comparable, S versionset.Set[S]] struct {
	// Conflict is the fully satisfied incompatibility that stopped the pass, or
	// nil if propagation completed with nothing left to derive.
	//
	// Propagation deliberately stops rather than resolving: resolution requires
	// backjumping, which is a separate concern. The caller resolves and resumes.
	Conflict *Incompatibility[P, S]
}

// HasConflict reports whether propagation stopped on a conflict.
func (r PropagationResult[P, S]) HasConflict() bool { return r.Conflict != nil }

// Propagate derives every consequence forced by the store, starting from the
// package whose assignments most recently changed, and stops on the first
// conflict.
//
// # What it does
//
// Repeatedly: take a changed package, scan the incompatibilities mentioning it
// newest-first, and for each almost-satisfied one append the negation of its
// open term as a derivation. Deriving something changes that term's package, so
// it joins the worklist. The pass ends when nothing is left to derive.
//
// # Where conflict resolution belongs
//
// If an incompatibility is found FULLY satisfied, propagation returns it as a
// conflict rather than resolving it. In the full algorithm, resolution happens
// here — inside propagation, not as a separate phase — because the correct
// response to "the thing I would derive next contradicts what is already true"
// is to repair the partial solution BEFORE deriving anything else. Splitting it
// out keeps this function testable on its own; the caller is responsible for
// resolving and resuming.
//
// # A conflict does not require a decision
//
// A term can become satisfied purely by a DERIVATION proving some version must
// eventually exist, with no concrete version chosen. The relation test is set
// containment, so a narrow positive derivation satisfies a broader positive
// claim about the same package. That is how a dependency incompatibility added
// while merely *considering* a candidate can turn out to be already fully
// satisfied by prior state, without that candidate ever being decided.
func Propagate[P comparable, S versionset.Set[S]](
	ps *PartialSolution[P, S], st *Store[P, S], start P,
) PropagationResult[P, S] {
	changed := []P{start}
	// Membership is tracked separately so a package cannot be queued twice.
	queued := map[P]bool{start: true}

	for len(changed) > 0 {
		pkg := changed[0]
		changed = changed[1:]
		delete(queued, pkg)

		for _, inc := range st.Mentioning(pkg) {
			satisfaction, open := Classify(ps, inc)

			switch satisfaction {
			case FullySatisfied:
				return PropagationResult[P, S]{Conflict: inc}

			case AlmostSatisfied:
				t, ok := inc.Term(open)
				if !ok {
					// Classify only names a package the incompatibility
					// mentions, so this is unreachable; guarded rather than
					// trusted, because a silent wrong derivation here would be
					// very hard to trace.
					continue
				}

				// The conjunction cannot hold and every other term does, so the
				// open term must be false: derive its negation.
				ps.Derive(open, t.Negate(), inc)

				// That package's assignments changed, so incompatibilities
				// mentioning it may now fire.
				if !queued[open] {
					queued[open] = true
					changed = append(changed, open)
				}

			case Unrelated:
				// Nothing to conclude from this one.
			}
		}
	}

	return PropagationResult[P, S]{}
}
