// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package solver implements the PubGrub algorithm: unit propagation, decision
// making, and conflict-driven backjumping over a set of incompatibilities.
//
// This is the core, and the part the clean-room policy exists to protect. It is
// written from published algorithmic prose only — see the repository's CLAUDE.md
// and RFD 0001 section 7.1 before contributing.
//
// The solver asks its caller for facts (what versions exist, what does this
// version depend on) through an interface and never performs I/O itself. That
// separation is what makes it testable against fixtures and reusable across
// ecosystems.
package solver
