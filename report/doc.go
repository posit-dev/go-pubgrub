// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package report turns a failed resolution into an explanation a human can act
// on.
//
// This is not a nicety bolted on afterwards; it is a large part of why PubGrub
// is worth implementing. When resolution fails, the solver holds a derivation
// graph recording exactly which incompatibilities forced the contradiction, and
// that graph can be rendered as a chain of reasoning rather than as "no solution
// found".
//
// The rendering is deliberately separate from the solving so that the message format
// can change without touching the algorithm.
//
// Start at [FromError], or at [Explain] if you already hold the root cause. The walk
// they perform implements §9 of docs/ALGORITHM.md, which every "§" in this package
// refers to.
//
// A consumer wanting its own presentation should read the [Line] values rather than
// re-walking the derivation graph, because §9's ordering and line-numbering rules are
// the hard part and there is no reason to reimplement them. Each Line carries the
// incompatibility it states, so the packages and version ranges behind a sentence are
// reachable without parsing the sentence.
package report
