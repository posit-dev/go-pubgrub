// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package term implements the term algebra PubGrub reasons with.
//
// A term is a statement about a package: either that some set of its versions is
// allowed, or that some set is disallowed. The solver's entire reasoning reduces
// to operations on these — intersection, union, negation — plus the relations
// between them: whether one term satisfies another, contradicts it, or leaves it
// undecided.
//
// This package knows nothing about any packaging ecosystem. It is deliberately
// separate from versionset so that the logical layer and the version-arithmetic
// layer can be tested independently: a bug in "does A satisfy B" and a bug in
// "what is the intersection of these two ranges" are very different bugs, and
// conflating them makes both harder to find.
package term
