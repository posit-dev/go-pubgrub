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
// The rendering is deliberately separate from the solving so that the message
// format can change without touching the algorithm, and so a caller can walk the
// graph itself to produce its own presentation.
package report
