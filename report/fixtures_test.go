// SPDX-License-Identifier: Apache-2.0 OR MIT

package report_test

import "github.com/posit-dev/go-pubgrub/versionset"

// The fixtures below exist to cover each of §9's rendering shapes with a derivation
// graph the solver really produced. Which shape a universe yields is not obvious from
// reading it — it falls out of the search path — so each one names the shape it was
// chosen for, and TestShapeOfEachFixture asserts that the shape is still there.
// Editing a universe without re-checking that test is how coverage silently
// disappears.
//
// # Why the package names are words and not letters
//
// Deliberately multi-character and mutually non-overlapping, and never substrings of
// the words the phraser itself uses ("version", "matches", "depends", "requires",
// "selected"). TestEveryExternalFactIsStated works by looking for a package's name in
// a rendered line, and with names like "a" and "b" that check passes on the letters
// inside "matches" and "cannot" — it was near-vacuous until these were renamed.

// chainUniverse is a linear proof: bravo has no version satisfying what alpha needs.
//
// Shape covered: §9's base case, two external causes in one sentence.
func chainUniverse() *universe {
	return newUniverse().
		with("root", 1, requires("alpha", versionset.AtLeast(1))).
		with("alpha", 1, requires("bravo", versionset.AtLeast(2))).
		with("bravo", 1)
}

// twoWayUniverse has two dependants demanding disjoint ranges of one package.
//
// Shape covered: one derived cause plus one external, repeatedly — the commonest
// shape, and the one §9's compression applies to.
func twoWayUniverse() *universe {
	return newUniverse().
		with("root", 1,
			requires("alpha", versionset.AtLeast(1)),
			requires("charlie", versionset.AtLeast(1))).
		with("alpha", 1, requires("bravo", versionset.Range(1, 2))).
		with("charlie", 1, requires("bravo", versionset.Range(2, 3))).
		with("bravo", 1).
		with("bravo", 2)
}

// diamondUniverse has two independent branches under one root that fail for different
// reasons.
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

// reusedUniverse produces a proof in which ONE derived incompatibility is a cause of
// two others — §9's whole reason for having line numbers at all.
//
// # Why it looks arbitrary
//
// It is. The shape was found by randomized search, because it is rare: of ~500,000
// generated universes only 19 produced it, and none of the hand-written universes
// above do. Two things learned from that search are worth knowing before editing this:
//
//   - Removing the packages that never appear in the proof DESTROYS the shape. They
//     are not dead weight; they change which package decision making picks first, and
//     so the whole search path. Hand-minimizing this universe was tried and lost the
//     shape.
//   - No version constrains the same target twice. The first universes the search
//     turned up owed the reuse to a version whose own metadata contradicted itself,
//     which is not a shape worth documenting a library's behaviour around.
//
// The solve is deterministic despite the map-shaped inputs, which is what makes a
// golden over this safe.
func reusedUniverse() *universe {
	return newUniverse().
		with("root", 1,
			requires("alpha", versionset.LessThan(5)),
			requires("charlie", versionset.AtLeast(2))).
		with("alpha", 1).
		with("alpha", 2,
			requires("delta", versionset.Exactly(4)),
			requires("charlie", versionset.Range(3, 5))).
		with("alpha", 3, requires("echo", versionset.Range(1, 4))).
		with("bravo", 1, requires("alpha", versionset.LessThan(1))).
		with("bravo", 2).
		with("bravo", 3, requires("delta", versionset.AtLeast(2))).
		with("charlie", 1, requires("bravo", versionset.AtLeast(4))).
		with("charlie", 2, requires("echo", versionset.Exactly(1))).
		with("charlie", 3, requires("delta", versionset.Exactly(1))).
		with("delta", 1, requires("echo", versionset.LessThan(2))).
		with("delta", 2, requires("alpha", versionset.Range(2, 5))).
		with("delta", 3, requires("alpha", versionset.Exactly(4))).
		with("echo", 1,
			requires("charlie", versionset.Exactly(5)),
			requires("alpha", versionset.Exactly(1))).
		with("echo", 2).
		with("echo", 3)
}

// unconstrainedUniverse has a dependency with NO version constraint, which is the
// commonest dependency form in real ecosystems (`Requires-Dist: foo`, `Imports: foo`).
//
// It is encoded as a negative term over every version, and the phrasing for that is
// the opposite of the phrasing for a positive term over every version: "depends on
// bravo" means any version will do, while "depends on every version of bravo" would
// mean all of them at once. This fixture exists because that distinction was got wrong
// and nothing caught it — no other fixture contains an unbounded range at all.
func unconstrainedUniverse() *universe {
	return newUniverse().
		with("root", 1, requires("alpha", versionset.AtLeast(1))).
		with("alpha", 1, requires("bravo", versionset.All())).
		with("bravo", 1, requires("charlie", versionset.AtLeast(9))).
		with("charlie", 1)
}

// externalRootUniverse asks for a root version that was never published.
//
// The root cause is then an EXTERNAL incompatibility rather than a derived one:
// "no versions of root" is itself the conflict, with nothing concluded from it. That
// path skipped the terminal wording entirely and reported "no version of root
// matches 1", blaming the user's own project for what is really an unsatisfiable
// request.
func externalRootUniverse() *universe {
	return newUniverse().with("root", 2)
}

// allFixtures is every fixture, for the invariants that must hold of any report.
func allFixtures() map[string]func() *universe {
	return map[string]func() *universe{
		"chain":         chainUniverse,
		"two-way":       twoWayUniverse,
		"diamond":       diamondUniverse,
		"reused":        reusedUniverse,
		"unconstrained": unconstrainedUniverse,
		"external-root": externalRootUniverse,
	}
}
