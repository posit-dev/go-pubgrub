// SPDX-License-Identifier: Apache-2.0 OR MIT

package report

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/posit-dev/go-pubgrub/solver"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// Line is one sentence of an explanation.
//
// Number is the visible label a later line may cite, or zero when the line is
// never referred to again. §9 assigns one only where the prose actually needs to
// point back at a conclusion, so most lines carry none.
type Line struct {
	// Number labels this line for citation, or is zero when nothing cites it.
	Number int

	// Text is the sentence, already ending in a period. Citations of earlier
	// lines appear inside it as "(N)".
	Text string

	// Break asks for a blank line before this one. §9 calls for a visual break
	// when two multi-step derivations would otherwise run together with no
	// anchor between them.
	Break bool
}

// Report is a rendered explanation of why resolution failed.
//
// Lines are in reading order. A consumer that wants its own presentation — JSON,
// HTML, an indented tree — should use these rather than re-walking the derivation
// graph, since the walk is where §9's numbering and ordering rules live.
type Report struct {
	Lines []Line
}

// String renders the report as plain text, one sentence per line, with numbered
// lines labelled at the end so a citation of "(3)" can be found by eye.
func (r *Report) String() string {
	if r == nil || len(r.Lines) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, line := range r.Lines {
		if i > 0 {
			sb.WriteString("\n")
		}
		if line.Break {
			sb.WriteString("\n")
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
// The solver knows nothing about any packaging ecosystem, so it cannot know that
// a set is spelled ">=1.26,<2" in one ecosystem and "^1.26" in another. An
// adapter implements this to make explanations read the way its users expect.
//
// Passing nil to Explain is fine: fmt.Stringer is used where available, which is
// enough for diagnostics and for tests.
type Formatter[P comparable, S any] interface {
	// Package names a package.
	Package(pkg P) string

	// Set describes a set of versions.
	Set(s S) string
}

// Explain renders the proof carried by a failed solve.
//
// root is solver.Unsolvable's RootCause. Following its causes reaches the
// external facts that forced the failure, and §9's walk turns that graph into
// sentences: leaves become statements of fact about packages, derived nodes become
// conclusions drawn from them, and a conclusion needed in two places is stated
// once and cited by number afterwards.
//
// A nil root yields a one-line report rather than a panic. An explanation is what
// a user sees when something has already gone wrong, so it is the last place that
// should introduce a second failure.
func Explain[P comparable, S versionset.Set[S]](
	root *solver.Incompatibility[P, S], f Formatter[P, S],
) *Report {
	if f == nil {
		f = stringerFormatter[P, S]{}
	}
	ph := &phraser[P, S]{fmtr: f}

	if root == nil {
		return &Report{Lines: []Line{{Text: "Version solving failed, but no root cause was recorded."}}}
	}

	b := &builder[P, S]{
		ph:        ph,
		cites:     countCitations(root),
		numbers:   map[*solver.Incompatibility[P, S]]int{},
		lineIndex: map[*solver.Incompatibility[P, S]]int{},
		rendered:  map[*solver.Incompatibility[P, S]]bool{},
		visiting:  map[*solver.Incompatibility[P, S]]bool{},
	}
	b.explain(root, true, false)
	return &Report{Lines: b.lines}
}

// stringerFormatter is the default Formatter: fmt.Stringer where implemented,
// then fmt.Sprint, which is right for the integer-keyed and string-keyed types
// tests and diagnostics use.
type stringerFormatter[P comparable, S any] struct{}

func (stringerFormatter[P, S]) Package(pkg P) string { return stringify(pkg) }
func (stringerFormatter[P, S]) Set(s S) string       { return stringify(s) }

func stringify(v any) string {
	if s, ok := v.(interface{ String() string }); ok {
		return s.String()
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
