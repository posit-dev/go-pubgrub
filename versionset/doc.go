// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package versionset defines the version-set abstraction the solver operates on.
//
// PubGrub does not care what a version is. It needs only that versions form a
// total order and that sets of them support intersection, union, complement, and
// emptiness testing. Keeping that as an interface here is what lets the same
// solver serve Python, R, or anything else — the ecosystem supplies the
// ordering and the set representation, and the algorithm stays unchanged.
//
// Nothing in this package may assume PEP 440, semantic versioning, or any other
// specific scheme. A version comparison that "knows" about pre-release
// semantics belongs in the adapter, not here.
package versionset
