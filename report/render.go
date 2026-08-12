// SPDX-License-Identifier: Apache-2.0 OR MIT

package report

import (
	"strconv"

	"github.com/posit-dev/go-pubgrub/solver"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// builder accumulates §9's walk: the sentences produced so far, which conclusions
// have been given visible line numbers, and how often each node is cited.
type builder[P comparable, S versionset.Set[S]] struct {
	ph *phraser[P, S]

	// cites is the pre-pass count: how many other incompatibilities name this one as
	// a cause. Only a count of two or more can ever need a line number.
	cites map[*solver.Incompatibility[P, S]]int

	// numbers holds the visible label assigned to a node's concluding line.
	//
	// It doubles as the record of what has already been rendered, and that is the
	// only thing preventing a node from being explained twice. The invariant: a node
	// with two or more citers is refused by collapsible and numbered by finish the
	// moment its line exists, so every later parent finds it here and cites it
	// instead of deriving it again. Every recursion site therefore has to consult
	// this map IMMEDIATELY before recursing — a lookup made before some other
	// recursion runs is stale, because that recursion may have rendered and numbered
	// the node in the meantime.
	numbers map[*solver.Incompatibility[P, S]]int

	// lineIndex records which line stated a node's conclusion, so a number can be
	// attached to it after the fact — §9 assigns the number only once the line has
	// been written.
	lineIndex map[*solver.Incompatibility[P, S]]int

	// visiting is a re-entrancy guard. Nothing a caller can build should trip it: the
	// only way to create a derived incompatibility is solver's unexported newDerived,
	// and causes always predate their consequence, so the graph is acyclic by
	// construction. It exists because hanging is the worst possible failure mode for
	// a diagnostic, and a guard that costs one map write is worth having even for a
	// case that should be impossible.
	visiting map[*solver.Incompatibility[P, S]]bool

	lines   []Line[P, S]
	nextNum int

	// pendingBreak asks the next line to be preceded by a blank one.
	pendingBreak bool

	// pendingCites collects the line numbers the sentence under construction refers
	// back to, so a consumer gets them as integers rather than having to parse them
	// out of the prose.
	pendingCites []int
}

// countCitations counts, for every node reachable from root, how many DISTINCT other
// incompatibilities name it as a cause.
//
// This is §9's pre-pass, and it has to happen before any text is generated: whether a
// conclusion needs a visible number depends on whether something later will point
// back at it, which is not knowable while walking forward.
func countCitations[P comparable, S versionset.Set[S]](
	root *solver.Incompatibility[P, S],
) map[*solver.Incompatibility[P, S]]int {
	cites := map[*solver.Incompatibility[P, S]]int{}
	seen := map[*solver.Incompatibility[P, S]]bool{}

	var walk func(inc *solver.Incompatibility[P, S])
	walk = func(inc *solver.Incompatibility[P, S]) {
		if seen[inc] {
			return
		}
		seen[inc] = true

		a, b, derived := inc.Causes()
		if !derived {
			return
		}
		cites[a]++
		if b != a {
			// One node standing as both causes is a single citer, not two: the prose
			// would refer back to it once. explain relies on this agreeing with how it
			// handles that shape.
			cites[b]++
		}
		walk(a)
		walk(b)
	}
	walk(root)

	return cites
}

// explain writes the lines that derive inc, ending with a line stating inc's own
// conclusion.
//
// isRoot marks the terminal failure, whose sentence is worded as the payoff rather
// than as another intermediate step. continuing says the line about to be written
// follows directly from the one before it, which is the difference between opening a
// fresh thought and adding to one already underway.
func (b *builder[P, S]) explain(inc *solver.Incompatibility[P, S], isRoot, continuing bool) {
	if b.visiting[inc] {
		b.emit(inc, continuing, isRoot, nil,
			"the reasoning here is circular, which cannot happen for a graph the solver built")
		return
	}
	b.visiting[inc] = true
	defer delete(b.visiting, inc)

	causeA, causeB, derived := inc.Causes()
	if !derived {
		// A failure that is itself an external fact — reachable, for instance, when the
		// root version asked for was never published, so "no versions of root" IS the
		// conflict rather than anything derived from one. Nothing was concluded, so the
		// fact is the whole explanation, but the terminal line still gets the terminal
		// wording.
		if isRoot {
			b.emit(inc, continuing, isRoot, nil, b.ph.rootFailure(inc))
		} else {
			b.emit(inc, continuing, isRoot, nil, b.ph.fact(inc))
		}
		b.finish(inc)
		return
	}

	switch {
	case causeA == causeB:
		// One node standing as both causes. countCitations counts it once, so this has
		// to state it once too, or the same derivation would be rendered twice and the
		// second copy would carry no label.
		b.explainOneCause(inc, causeA, isRoot, continuing)

	case !causeA.IsDerived() && !causeB.IsDerived():
		// §9's base case: both causes are facts, so one sentence says everything.
		b.emit(inc, continuing, isRoot,
			[]string{b.ph.fact(causeA), b.ph.fact(causeB)}, b.stated(inc, isRoot))

	case causeA.IsDerived() != causeB.IsDerived():
		derivedCause, externalCause := causeA, causeB
		if causeB.IsDerived() {
			derivedCause, externalCause = causeB, causeA
		}
		b.explainOneDerived(inc, derivedCause, externalCause, isRoot, continuing)

	default:
		b.explainBothDerived(inc, causeA, causeB, isRoot, continuing)
	}

	b.finish(inc)
}

// explainOneCause handles the degenerate shape where both causes are the same node.
func (b *builder[P, S]) explainOneCause(
	inc, cause *solver.Incompatibility[P, S], isRoot, continuing bool,
) {
	if !cause.IsDerived() {
		b.emit(inc, continuing, isRoot, []string{b.ph.fact(cause)}, b.stated(inc, isRoot))
		return
	}
	if number, ok := b.numbers[cause]; ok {
		b.emit(inc, continuing, isRoot, []string{b.cited(cause, number)}, b.stated(inc, isRoot))
		return
	}
	b.explain(cause, false, continuing)
	b.emit(inc, true, isRoot, nil, b.stated(inc, isRoot))
}

// explainOneDerived handles §9's "exactly one cause is derived, the other external"
// case.
func (b *builder[P, S]) explainOneDerived(
	inc, derivedCause, externalCause *solver.Incompatibility[P, S], isRoot, continuing bool,
) {
	if number, ok := b.numbers[derivedCause]; ok {
		// Already explained and labelled: cite it instead of repeating it.
		b.emit(inc, continuing, isRoot,
			[]string{b.ph.fact(externalCause), b.cited(derivedCause, number)},
			b.stated(inc, isRoot))
		return
	}

	if b.collapsible(derivedCause) {
		// §9's compression: state the derived cause's own two facts and jump straight to
		// this conclusion, skipping an intermediate restatement a reader would have found
		// obvious.
		innerA, innerB, _ := derivedCause.Causes()
		b.emit(inc, continuing, isRoot,
			[]string{b.ph.fact(innerA), b.ph.fact(innerB), b.ph.fact(externalCause)},
			b.stated(inc, isRoot))
		return
	}

	b.explain(derivedCause, false, continuing)
	b.emit(inc, true, isRoot, []string{b.ph.fact(externalCause)}, b.stated(inc, isRoot))
}

// explainBothDerived handles §9's "both causes are themselves derived" case.
func (b *builder[P, S]) explainBothDerived(
	inc, causeA, causeB *solver.Incompatibility[P, S], isRoot, continuing bool,
) {
	numberA, hasA := b.numbers[causeA]
	numberB, hasB := b.numbers[causeB]

	switch {
	case hasA && hasB:
		// Both conclusions are already on the page: combine them by citation.
		b.emit(inc, continuing, isRoot,
			[]string{b.cited(causeA, numberA), b.cited(causeB, numberB)}, b.stated(inc, isRoot))
		return

	case hasA || hasB:
		// One is on the page. Render the other, then bring the two together.
		numbered, unnumbered, number := causeA, causeB, numberA
		if hasB {
			numbered, unnumbered, number = causeB, causeA, numberB
		}
		b.explain(unnumbered, false, continuing)
		b.emit(inc, true, isRoot, []string{b.cited(numbered, number)}, b.stated(inc, isRoot))
		return
	}

	// Neither has a number yet. §9 prefers to describe whichever cause is built from
	// two external facts inline as the "simple" one, and to render the other first —
	// so the reader gets the long derivation while it is still the subject, rather than
	// two multi-step derivations back to back with no anchor between them.
	var simpleCause, deepCause *solver.Incompatibility[P, S]
	switch {
	case b.collapsible(causeA) && !b.collapsible(causeB):
		simpleCause, deepCause = causeA, causeB
	case b.collapsible(causeB):
		// Covers "only B is simple" and "both are". The choice is not symmetric — the
		// deep cause gets a line of its own and a number, the simple one is absorbed
		// into the following sentence — so taking B keeps the first-written cause in the
		// earlier position, which is the order a reader expects.
		simpleCause, deepCause = causeB, causeA
	default:
		// Neither is simple: render both in full, separated by a visual break, and close
		// by citing the first.
		b.explain(causeA, false, continuing)
		numberA = b.assignNumber(causeA)

		// Rendering causeA may have put causeB on the page and numbered it along the
		// way, if causeB is also a cause of something inside causeA's subtree. The
		// lookup above is stale by now, so ask again rather than deriving it twice.
		if numberB, ok := b.numbers[causeB]; ok {
			b.emit(inc, true, isRoot,
				[]string{b.cited(causeA, numberA), b.cited(causeB, numberB)},
				b.stated(inc, isRoot))
			return
		}

		b.pendingBreak = true
		b.explain(causeB, false, false)
		b.emit(inc, true, isRoot, []string{b.cited(causeA, numberA)}, b.stated(inc, isRoot))
		return
	}

	b.explain(deepCause, false, continuing)
	// The flow is about to be interrupted to handle the simple cause, so the conclusion
	// just written needs a label to be referred back to.
	number := b.assignNumber(deepCause)

	innerA, innerB, _ := simpleCause.Causes()
	b.emit(inc, true, isRoot,
		[]string{b.ph.fact(innerA), b.ph.fact(innerB), b.cited(deepCause, number)},
		b.stated(inc, isRoot))
}

// collapsible reports whether a derived node can be described inline, in one clause,
// rather than getting a sentence of its own: true when both of its causes are plain
// facts AND nothing else cites it.
//
// # This is narrower than §9's condition for the one-derived case
//
// §9 (docs/ALGORITHM.md, "Exactly one cause is derived") allows collapsing a derived
// cause of "simple shape (one external + one derived-with-no-number-needed cause)",
// which is a weaker test than the one here. Applying the stricter both-causes-external
// rule to both call sites is a deliberate narrowing: folding three levels of
// derivation into a single sentence reads worse than the extra line costs. §9 says
// "usually" and puts phrasing under taste, so this is a verbosity choice, not a
// departure from the algorithm. The consequence is that in a chain where every node
// has one derived and one external cause, only the innermost node is collapsed.
//
// The citation check is what keeps the compression honest. A node cited twice must end
// up with a numbered line for the second citation to point at, so collapsing it into a
// neighbouring sentence would leave that citation dangling.
func (b *builder[P, S]) collapsible(inc *solver.Incompatibility[P, S]) bool {
	if b.cites[inc] >= 2 {
		return false
	}
	a, c, derived := inc.Causes()
	return derived && !a.IsDerived() && !c.IsDerived()
}

// stated words what an incompatibility rules out, in the voice its position calls
// for: the terminal failure is the payoff and says outright that the request cannot be
// satisfied, while every other node is an intermediate conclusion.
func (b *builder[P, S]) stated(inc *solver.Incompatibility[P, S], isRoot bool) string {
	if isRoot {
		return b.ph.rootFailure(inc)
	}
	return b.ph.conclusion(inc)
}

// cited renders a reference to an already-written conclusion, and records the
// reference so a consumer gets it as an integer too.
//
// A zero number means no line carries that label, so the reference is omitted rather
// than rendered as a citation of "(0)" that nothing defines. Silently wrong prose is
// worse than terse prose in a diagnostic.
func (b *builder[P, S]) cited(inc *solver.Incompatibility[P, S], number int) string {
	if number == 0 {
		return b.ph.conclusion(inc)
	}
	b.pendingCites = append(b.pendingCites, number)
	return b.ph.conclusion(inc) + " (" + strconv.Itoa(number) + ")"
}

// emit appends one sentence.
//
// The connective is the whole difference between a list of assertions and an argument:
// "Because" opens a line of reasoning, "And because" adds to the one already underway,
// and "So" marks the payoff.
func (b *builder[P, S]) emit(
	inc *solver.Incompatibility[P, S], continuing, isRoot bool, premises []string, conclusion string,
) {
	opener := "Because"
	switch {
	case isRoot:
		opener = "So, because"
	case continuing:
		opener = "And because"
	}

	text := conclusion + "."
	if len(premises) > 0 {
		text = opener + " " + list(premises) + ", " + conclusion + "."
	} else if isRoot {
		text = "So, " + conclusion + "."
	}

	b.lines = append(b.lines, Line[P, S]{
		Text:  capitalize(text),
		Break: b.pendingBreak,
		Cites: b.pendingCites,
		Node:  inc,
	})
	b.pendingBreak = false
	b.pendingCites = nil
}

// finish records where a node's conclusion was stated and, per §9, assigns a visible
// number AFTER the line exists — but only when something else will cite it.
func (b *builder[P, S]) finish(inc *solver.Incompatibility[P, S]) {
	b.lineIndex[inc] = len(b.lines) - 1

	if b.cites[inc] >= 2 {
		b.assignNumber(inc)
	}
}

// assignNumber labels the line that stated inc's conclusion, and returns the label.
func (b *builder[P, S]) assignNumber(inc *solver.Incompatibility[P, S]) int {
	if number, ok := b.numbers[inc]; ok {
		return number
	}

	index, ok := b.lineIndex[inc]
	if !ok {
		// Nothing to label: the conclusion was folded into another sentence rather than
		// getting a line, so there is no line to point at. cited omits the reference.
		return 0
	}

	b.nextNum++
	b.numbers[inc] = b.nextNum
	b.lines[index].Number = b.nextNum
	return b.nextNum
}

// capitalize upper-cases the first letter of a sentence built from clauses that are
// individually lower-case.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-'a'+'A') + s[1:]
	}
	return s
}
