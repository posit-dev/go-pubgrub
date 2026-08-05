// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package solver implements the PubGrub algorithm: unit propagation, decision
// making, and conflict-driven backjumping over a set of incompatibilities.
//
// It is written from the published algorithm descriptions — see docs/ALGORITHM.md
// and CONTRIBUTING.md before contributing.
//
// The solver asks its caller for facts (what versions exist, what does this
// version depend on) through an interface and never performs I/O itself. That
// separation is what makes it testable against fixtures and reusable across
// ecosystems.
package solver
