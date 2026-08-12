// SPDX-License-Identifier: Apache-2.0 OR MIT

package report_test

import "github.com/posit-dev/go-pubgrub/versionset"

// The fixtures below exist to cover each of §9's four rendering shapes with a
// derivation graph the solver really produced. Which shape a universe yields is
// not obvious from reading it — it falls out of the search path — so each one
// names the shape it was chosen for, and shapeOfEachFixture asserts that the
// shape is still there. Editing a universe without re-checking that test is how
// coverage silently disappears.

// chainUniverse is a linear proof: b has no version satisfying what a 1 needs.
//
// Shape covered: §9's base case, two external causes in one sentence.
func chainUniverse() *universe {
	return newUniverse().
		with("root", 1, requires("a", versionset.AtLeast(1))).
		with("a", 1, requires("b", versionset.AtLeast(2))).
		with("b", 1)
}

// twoWayUniverse has two dependants demanding disjoint ranges of one package.
//
// Shape covered: one derived cause plus one external, repeatedly — the commonest
// shape, and the one §9's compression applies to.
func twoWayUniverse() *universe {
	return newUniverse().
		with("root", 1, requires("a", versionset.AtLeast(1)), requires("c", versionset.AtLeast(1))).
		with("a", 1, requires("b", versionset.Range(1, 2))).
		with("c", 1, requires("b", versionset.Range(2, 3))).
		with("b", 1).
		with("b", 2)
}

// diamondUniverse has two independent branches under one root that fail for
// different reasons.
//
// Shape covered: both causes derived, neither yet numbered — so one is described
// inline and the other is rendered first and labelled.
func diamondUniverse() *universe {
	return newUniverse().
		with("root", 1,
			requires("left", versionset.AtLeast(1)),
			requires("right", versionset.AtLeast(1))).
		with("left", 2, requires("shared", versionset.AtLeast(10))).
		with("left", 1, requires("shared", versionset.Range(1, 3))).
		with("right", 2, requires("shared", versionset.AtLeast(10))).
		with("right", 1, requires("shared", versionset.Range(5, 7))).
		with("shared", 1).
		with("shared", 5)
}

// reusedUniverse produces a proof in which ONE derived incompatibility is a cause
// of two others — §9's whole reason for having line numbers at all.
//
// # Why it looks arbitrary
//
// It is. The shape was found by randomized search over ~500,000 small universes,
// because it is rare: only 19 of them produced it, and none of the hand-written
// universes above do. Two things learned from that search are worth knowing before
// editing this:
//
//   - Removing the packages that never appear in the proof DESTROYS the shape.
//     They are not dead weight; they change which package decision making picks
//     first, and so the whole search path. p1 is such a package here.
//   - No version constrains the same target twice. The first universes the search
//     turned up owed the reuse to a version whose own metadata contradicted itself,
//     which is not a shape worth documenting a library's behaviour around.
//
// The solve is deterministic despite the map-shaped inputs, which is what makes a
// golden over this safe: 300 runs produce one proof.
func reusedUniverse() *universe {
	return newUniverse().
		with("root", 1, requires("p0", versionset.LessThan(5)), requires("p2", versionset.AtLeast(2))).
		with("p0", 1).
		with("p0", 2, requires("p3", versionset.Exactly(4)), requires("p2", versionset.Range(3, 5))).
		with("p0", 3, requires("p4", versionset.Range(1, 4))).
		with("p1", 1, requires("p0", versionset.LessThan(1))).
		with("p1", 2).
		with("p1", 3, requires("p3", versionset.AtLeast(2))).
		with("p2", 1, requires("p1", versionset.AtLeast(4))).
		with("p2", 2, requires("p4", versionset.Exactly(1))).
		with("p2", 3, requires("p3", versionset.Exactly(1))).
		with("p3", 1, requires("p4", versionset.LessThan(2))).
		with("p3", 2, requires("p0", versionset.Range(2, 5))).
		with("p3", 3, requires("p0", versionset.Exactly(4))).
		with("p4", 1, requires("p2", versionset.Exactly(5)), requires("p0", versionset.Exactly(1))).
		with("p4", 2).
		with("p4", 3)
}

// allFixtures is every fixture, for the invariants that must hold of any report.
func allFixtures() map[string]func() *universe {
	return map[string]func() *universe{
		"chain":   chainUniverse,
		"two-way": twoWayUniverse,
		"diamond": diamondUniverse,
		"reused":  reusedUniverse,
	}
}
