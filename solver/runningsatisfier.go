// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"sort"

	"github.com/posit-dev/go-pubgrub/term"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// runningState is the RunningSatisfier state: for each assignment, the
// intersection of its term with every earlier term about the same package, and
// for each package the ascending indices of its assignments.
type runningState[P comparable, S versionset.Set[S]] struct {
	terms []term.Term[S] // parallel to PartialSolution.assignments
	byPkg map[P][]int

	// queries counts satisfier queries answered from the running terms.
	queries int
}

// enableRunningSatisfier turns the switch on, reading whatever is assigned
// already. The accumulated terms are rebuilt from the running terms.
func (ps *PartialSolution[P, S]) enableRunningSatisfier() {
	ps.ensure()
	ps.rs = &runningState[P, S]{byPkg: make(map[P][]int)}
	ps.accumulated = make(accumulation[P, S], len(ps.accumulated))
	for i, a := range ps.assignments {
		ps.noteRunning(a, i)
	}
}

// noteRunning computes the running term of the assignment at index and makes it
// the accumulated term of its package.
func (ps *PartialSolution[P, S]) noteRunning(a Assignment[P, S], index int) {
	run := a.Term
	if prev, ok := ps.accumulated[a.Package]; ok {
		run = prev.Intersect(a.Term)
	}
	ps.accumulated[a.Package] = run
	ps.rs.terms = append(ps.rs.terms, run)
	ps.rs.byPkg[a.Package] = append(ps.rs.byPkg[a.Package], index)
}

// truncateRunning drops the running state of the assignments from keep on and
// resets each affected package's accumulated term to its last survivor.
func (ps *PartialSolution[P, S]) truncateRunning(removed []Assignment[P, S], keep int) {
	rs := ps.rs
	for i := len(removed) - 1; i >= 0; i-- {
		pkg := removed[i].Package
		list := rs.byPkg[pkg]
		list = list[:len(list)-1]
		if len(list) == 0 {
			delete(rs.byPkg, pkg)
			delete(ps.accumulated, pkg)
			continue
		}
		rs.byPkg[pkg] = list
		ps.accumulated[pkg] = rs.terms[list[len(list)-1]]
	}
	rs.terms = rs.terms[:keep]
}

// firstIndexSatisfyingRunning is FirstIndexSatisfying by binary search: a running
// term only narrows, so once it satisfies t it keeps doing so.
func (ps *PartialSolution[P, S]) firstIndexSatisfyingRunning(pkg P, t term.Term[S]) (int, bool) {
	ps.rs.queries++
	list := ps.rs.byPkg[pkg]
	k := sort.Search(len(list), func(m int) bool { return ps.rs.terms[list[m]].Satisfies(t) })
	if k == len(list) {
		return 0, false
	}
	return list[k], true
}

// previousSatisfierRunning is PreviousSatisfierOf from per-term earliest indices:
// the answer is the largest of them.
func (ps *PartialSolution[P, S]) previousSatisfierRunning(inc *Incompatibility[P, S], satisfier int) (int, bool) {
	ps.rs.queries++
	sat := ps.assignments[satisfier]
	best := -1
	for pkg, t := range inc.terms {
		if t.IsAlwaysFalse() {
			return 0, false
		}
		own := pkg == sat.Package
		if own && sat.Term.Satisfies(t) {
			continue // the satisfier alone covers this term
		}

		list := ps.rs.byPkg[pkg]
		list = list[:sort.SearchInts(list, satisfier)] // only indices below satisfier
		m := sort.Search(len(list), func(m int) bool {
			v := ps.rs.terms[list[m]]
			if own {
				v = sat.Term.Intersect(v)
			}
			return v.Satisfies(t)
		})
		if m == len(list) {
			return 0, false
		}
		if list[m] > best {
			best = list[m]
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}
