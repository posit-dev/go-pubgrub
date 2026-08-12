// SPDX-License-Identifier: Apache-2.0 OR MIT

package report

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/posit-dev/go-pubgrub/solver"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// Line is one sentence of an explanation, plus the machine-readable facts behind
// it.
//
// Text is the prose. Node, Cites and Number are what let a consumer build its own
// presentation — a JSON payload, an HTML page with each package linked, an indented
// tree — without re-deriving anything and without parsing English. That matters
// because §9's ordering and numbering rules are the hard part of this package, and a
// consumer should be able to reuse them rather than reimplement them.
//
// # Text is incomplete on its own
//
// A line's own Number is NOT part of Text, while its citations of other lines ARE,
// as a literal "(3)". A consumer rendering Text itself must append the Number, or
// every citation in the report will dangle with nothing carrying the matching label.
// String does this; anything hand-rolled has to. Cites carries the same references
// as integers, so a consumer never needs to parse them back out of the sentence.
type Line[P comparable, S versionset.Set[S]] struct {
	// Number labels this line so a later line can cite it, or is zero when nothing
	// cites it. §9 assigns one only where the prose needs to point back at a
	// conclusion, so most lines carry none.
	Number int

	// Text is the sentence, already ending in a period.
	Text string

	// Break asks for a blank line before this one. §9 calls for a visual break when
	// two multi-step derivations would otherwise run together with no anchor between
	// them.
	Break bool

	// Cites lists the Numbers of the lines this sentence refers back to, in the order
	// they appear in Text.
	Cites []int

	// Node is the incompatibility whose conclusion this line states. It is the way
	// back to the packages and version ranges involved:
	//
	//	for _, pkg := range line.Node.Packages() { ... }
	//
	// Nil only on a line reporting that there was nothing to explain.
	Node *solver.Incompatibility[P, S]
}

// Report is a rendered explanation of why resolution failed. Lines are in reading
// order.
type Report[P comparable, S versionset.Set[S]] struct {
	Lines []Line[P, S]
}

// String renders the report as plain text, one sentence per line, with numbered
// lines labelled at the end so a citation of "(3)" can be found by eye.
func (r *Report[P, S]) String() string {
	if r == nil || len(r.Lines) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, line := range r.Lines {
		if i > 0 {
			sb.WriteString("\n")
			if line.Break {
				sb.WriteString("\n")
			}
		}
		sb.WriteString(line.Text)
		if line.Number != 0 {
			sb.WriteString(" (" + strconv.Itoa(line.Number) + ")")
		}
	}
	return sb.String()
}

// Formatter supplies the vocabulary go-pubgrub is deliberately blind to: how to
// name a package, and how to describe a set of versions.
//
// The solver knows nothing about any packaging ecosystem, so it cannot know that a
// set is spelled ">=1.26,<2" in one ecosystem and "^1.26" in another. An adapter
// implements this to make explanations read the way its users expect.
//
// Passing an untyped nil to Explain is fine: fmt.Stringer is used where available,
// which is enough for diagnostics and for tests. A typed nil (a nil pointer stored
// in the interface) is a caller bug and will panic like any other nil receiver.
//
// # Package should be injective
//
// Two distinct packages that format to the same name are ordered arbitrarily within
// a sentence, because the ordering that makes output deterministic is over the
// FORMATTED name. A formatter that strips a namespace or folds case can therefore
// make one report's clause order differ from the next. Keep the mapping one-to-one,
// or accept that the ordering of same-named packages is unspecified.
type Formatter[P comparable, S any] interface {
	// Package names a package.
	Package(pkg P) string

	// Set describes a set of versions.
	Set(s S) string
}

// Explain renders the proof carried by a failed solve.
//
// root is solver.Unsolvable's RootCause. Following its causes reaches the external
// facts that forced the failure, and §9 of docs/ALGORITHM.md turns that graph into
// sentences: leaves become statements of fact about packages, derived nodes become
// conclusions drawn from them, and a conclusion needed in two places is stated once
// and cited by number afterwards.
//
// A nil root yields a one-line report rather than a panic. An explanation is what a
// user sees when something has already gone wrong, so it is the last place that
// should introduce a second failure.
func Explain[P comparable, S versionset.Set[S]](
	root *solver.Incompatibility[P, S], f Formatter[P, S],
) *Report[P, S] {
	if f == nil {
		f = stringerFormatter[P, S]{}
	}
	ph := &phraser[P, S]{fmtr: f}

	if root == nil {
		return &Report[P, S]{Lines: []Line[P, S]{
			{Text: "Version solving failed, but no root cause was recorded."},
		}}
	}

	b := &builder[P, S]{
		ph:        ph,
		cites:     countCitations(root),
		numbers:   map[*solver.Incompatibility[P, S]]int{},
		lineIndex: map[*solver.Incompatibility[P, S]]int{},
		visiting:  map[*solver.Incompatibility[P, S]]bool{},
	}
	b.explain(root, true, false)

	return &Report[P, S]{Lines: b.lines}
}

// FromError renders the explanation for an error returned by Solve, and reports
// whether the error was a resolution failure at all.
//
// False means the solve could not be carried out — a provider that failed or broke
// its contract — which is a different thing from "these requirements conflict" and
// should not be shown to a user as one.
func FromError[P comparable, S versionset.Set[S]](
	err error, f Formatter[P, S],
) (*Report[P, S], bool) {
	var unsolvable *solver.Unsolvable[P, S]
	if !errors.As(err, &unsolvable) {
		return nil, false
	}
	return Explain(unsolvable.RootCause, f), true
}

// stringerFormatter is the default Formatter: fmt.Stringer where implemented, then
// fmt.Sprint.
type stringerFormatter[P comparable, S any] struct{}

func (stringerFormatter[P, S]) Package(pkg P) string { return stringify(pkg) }
func (stringerFormatter[P, S]) Set(s S) string       { return stringify(s) }

func stringify(v any) string {
	if s, ok := v.(interface{ String() string }); ok {
		return s.String()
	}
	return fmt.Sprint(v)
}
