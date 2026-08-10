// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"errors"
	"fmt"

	"github.com/posit-dev/go-pubgrub/term"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// ErrNoSatisfier reports that conflict resolution was handed, or derived, an
// incompatibility that the partial solution does not fully satisfy.
//
// §7 is defined only over a conflict — an incompatibility every one of whose
// terms the partial solution entails — and every step preserves that: the prior
// cause of a satisfied incompatibility is itself satisfied, since its terms come
// from the two satisfied parents. So this cannot happen on a correct call, and it
// is reported rather than worked around because the alternative is a loop that
// cannot make progress and does not stop.
var ErrNoSatisfier = errors.New("solver: conflict resolution reached an incompatibility with no satisfier")

// Resolution is what conflict resolution hands back to unit propagation.
type Resolution[P comparable, S versionset.Set[S]] struct {
	// Incompatibility is the replacement for the conflict: the incompatibility
	// propagation should resume from. When Unsolvable is set it is instead the
	// root cause — the proof that nothing can be solved.
	//
	// On success it is guaranteed to be IN the store, so the propagation that
	// resumes on Package can actually find it. See where Resolve adds it for why
	// that guarantee is stated here rather than left to the caller.
	Incompatibility *Incompatibility[P, S]

	// Package is the package whose term is the open one after backtracking, i.e.
	// where propagation resumes. Meaningless when Unsolvable.
	Package P

	// Unsolvable reports §7.4's terminal case: the derivation reached an
	// incompatibility that cannot be traced back any further, which is the formal
	// statement that the whole problem has no solution. The partial solution is
	// NOT truncated in that case — its state is part of the proof.
	Unsolvable bool
}

// Resolve implements §7: given a conflict — an incompatibility the partial
// solution fully satisfies — produce a replacement and backtrack so that
// propagation has something new and correct to derive.
//
// It mutates ps by truncating it (never by appending), and may add derived
// incompatibilities to st.
//
// # What it guarantees on success
//
// §7.1's two guarantees, and both are worth stating as properties a test can
// check rather than as prose:
//
//  1. The returned incompatibility is only ALMOST satisfied by the truncated
//     partial solution, so propagation derives exactly one thing from it. This is
//     why the truncation happens at a decision-level boundary and nowhere else:
//     decisions and everything derived under them are discarded as a unit, and
//     "only one term still open" cannot be promised by a cut in the middle of a
//     level.
//  2. What gets derived next differs from what led here, so the same conflict is
//     not immediately regenerated. It falls out: the offending decision is gone,
//     and the returned incompatibility is a strictly more general statement than
//     the raw conflict was.
//
// # Why the loop
//
// When the satisfier and the previous satisfier share a decision level there is
// no boundary to cut at that separates the satisfier's contribution from the rest
// of what the conflict needs. Cutting above the pair keeps both and makes no
// progress; cutting below discards both and loses the shape of the conflict
// itself. So the satisfier's own cause is folded in (§7.3), producing a conflict
// one derivation closer to a decision, and the search is redone. The derivation
// graph is finite and acyclic, so this terminates.
func Resolve[P comparable, S versionset.Set[S]](
	ps *PartialSolution[P, S], st *Store[P, S], root P, conflict *Incompatibility[P, S],
) (Resolution[P, S], error) {
	current := conflict

	for {
		// §7.4: the terminal check comes FIRST. An incompatibility with no terms,
		// or a lone positive term about the root package, cannot be traced back
		// further — the root's version is a fixed fact, not a derivation with a
		// cause chain to unwind — so it is the final proof that no solution
		// exists rather than a fact to fold in and continue. Reaching for a
		// satisfier first would find the root's own decision and truncate to a
		// level that keeps it, leaving the conflict satisfied and the main loop
		// spinning.
		if current.IsEmpty() || isRootFailure(current, root) {
			return Resolution[P, S]{Incompatibility: current, Unsolvable: true}, nil
		}

		satisfierIndex, satisfierPkg, ok := ps.SatisfierOf(current)
		if !ok {
			return Resolution[P, S]{}, fmt.Errorf("%w: %v", ErrNoSatisfier, current)
		}
		satisfier := ps.Assignments()[satisfierIndex]

		// The term of `current` about the satisfier's own package: §7.3's `term`,
		// and what propagation resumes on.
		incTerm, found := current.Term(satisfierPkg)
		if !found {
			// SatisfierOf only names a package `current` mentions.
			return Resolution[P, S]{}, fmt.Errorf("%w: %v has no term for the satisfier's package",
				ErrNoSatisfier, current)
		}

		previousLevel := ps.baseLevel()
		if previousIndex, exists := ps.PreviousSatisfierOf(current, satisfierIndex); exists {
			previousLevel = ps.Assignments()[previousIndex].Level
		}

		if satisfier.Decision || previousLevel != satisfier.Level {
			// A level boundary exists that separates the satisfier from
			// everything else the conflict needed, so the truncation can promise
			// §7.1's guarantee 1.
			//
			// A decision qualifies on its own: everything strictly before it is at
			// a lower level by construction, so cutting to previousLevel discards
			// the decision and everything that depended on it, and nothing below
			// the cut could complete the conflict — that is what "earliest
			// assignment" in the satisfier's definition already established.
			// §7.4 adds the replacement to the known set "if incompatibility is not
			// the original input incompatibility". That condition is stated to keep
			// the store free of a duplicate of a fact propagation is already
			// scanning, and Store.Add achieves the same thing more directly by
			// deduplicating — so adding unconditionally stores no more than the
			// conditional version does.
			//
			// It also buys something the conditional version does not: whatever this
			// returns is guaranteed to be IN the store, and therefore findable by
			// the propagation that has to resume on it. The condition holds exactly
			// when no round of resolution ran, and in that case a caller who passed
			// a conflict it had not stored would get back an incompatibility that
			// propagation cannot see — so propagation would derive nothing, decision
			// making would run instead, and the conflict would be silently dropped.
			// Solve cannot reach that (its conflicts come from the store by
			// construction), which is exactly what makes it the kind of latent hazard
			// worth closing rather than documenting.
			current = st.Add(current)

			ps.BacktrackTo(previousLevel)
			return Resolution[P, S]{Incompatibility: current, Package: satisfierPkg}, nil
		}

		if satisfier.Cause == nil {
			// A derivation with no recorded cause cannot be resolved against.
			// Propagate always records one; a hand-built partial solution might
			// not, and silently treating it as a decision would under-backtrack.
			return Resolution[P, S]{}, fmt.Errorf(
				"solver: derivation for %s at index %d has no cause, so §7.3 has nothing to resolve against",
				formatPackage(satisfierPkg), satisfierIndex)
		}

		current = priorCause(current, satisfier.Cause, satisfierPkg, satisfier.Term, incTerm)
	}
}

// isRootFailure reports §7.4's second terminal shape: a single positive term
// about the root package.
//
// The root has exactly one version, so a positive term about it that the partial
// solution satisfies is a statement that the root's own version is forbidden —
// hence that the request itself is unsatisfiable. The version is not re-checked
// here: Resolve only ever asks about an incompatibility the partial solution
// fully satisfies, and the only way a positive term about the root is satisfied
// is by the root's decision landing inside it.
func isRootFailure[P comparable, S versionset.Set[S]](inc *Incompatibility[P, S], root P) bool {
	if inc.Len() != 1 {
		return false
	}
	t, ok := inc.Term(root)
	return ok && t.IsPositive()
}

// priorCause implements §7.3: resolve the conflict against the incompatibility
// that produced its satisfier, yielding a more general incompatibility one
// derivation further back.
//
// The underlying rule is propositional resolution generalized to terms: from
// {t1, q...} and {t2, r...} you may derive {t1 ∪ t2, q..., r...}, because in any
// world where t1 ∪ t2 holds at least one of t1 or t2 holds, which pulls in one of
// the two originals' remaining terms.
//
// satisfierTerm is the satisfier assignment's own term and incTerm is inc's term
// about that same package.
func priorCause[P comparable, S versionset.Set[S]](
	inc, cause *Incompatibility[P, S], pkg P, satisfierTerm, incTerm term.Term[S],
) *Incompatibility[P, S] {
	pairs := make([]PackageTerm[P, S], 0, inc.Len()+cause.Len())

	// Steps 1-2: everything from both sides EXCEPT the satisfier's package. Both
	// sides can speak about the same other package, which is why this collects
	// pairs — NewIncompatibilityFrom intersects them per §3, and in §10's trace
	// that intersection is the entire content of the result.
	for p, t := range inc.terms {
		if p == pkg {
			continue
		}
		pairs = append(pairs, PackageTerm[P, S]{Package: p, Term: t})
	}
	for p, t := range cause.terms {
		if p == pkg {
			continue
		}
		pairs = append(pairs, PackageTerm[P, S]{Package: p, Term: t})
	}

	// Step 3: if the satisfier did not satisfy inc's term ON ITS OWN — it only did
	// so jointly with an earlier assignment about the same package — the
	// package's term cannot simply be dropped, and a combined one goes back in.
	//
	// ¬(satisfier \ term) is the spec's phrasing, and it is the generalized rule's
	// t1 ∪ t2 written without materializing the cause's own term: the satisfier's
	// assignment is the negation of the cause's term for this package, so
	// ¬(S ∧ ¬T) = T ∪ ¬S is exactly incTerm ∪ (the cause's term). Computing it
	// from the satisfier keeps the two in step even if the cause carries a term
	// that has since been intersected with something else.
	//
	// # The guard is an optimization, not a correctness condition
	//
	// "S satisfies T" is by definition "S ∧ ¬T is always false", and the combined
	// term is the negation of exactly that — so whenever the guard is false the
	// term it would add is Negative(∅), which §3's normalization drops. Adding it
	// unconditionally would compute the same incompatibility. The guard is kept
	// because it states §7.3's own distinction where a reader looks for it, but
	// nothing rests on the two staying in step by accident:
	// TestPriorCauseStep3GuardMatchesNormalization pins the equivalence, so if the
	// definition of satisfaction ever drifts from the algebra, that is what fails.
	if !satisfierTerm.Satisfies(incTerm) {
		pairs = append(pairs, PackageTerm[P, S]{
			Package: pkg,
			Term:    satisfierTerm.Intersect(incTerm.Negate()).Negate(),
		})
	}

	// Step 4 is NewIncompatibilityFrom's job: merge duplicate per-package terms,
	// drop always-true ones.
	return newDerived(pairs, inc, cause)
}
