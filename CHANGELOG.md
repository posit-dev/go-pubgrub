# Changelog

All notable changes to this module are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
While the major version is `0`, breaking changes ship in a **minor** bump rather
than a major one, and are always listed under a `Breaking` heading so they are not
mistaken for a safe patch upgrade.

## [Unreleased]

### Changed

- `Provider.Candidates`' contract now states three things it left to inference, all
  found by a review of the published API rather than by a failure:

  - `rank` must be **deterministic** for the same `(pkg, allowed)` within a solve.
    `docs/ALGORITHM.md` promises that neither the solver's choices nor the package
    named first in an error vary between runs on identical input, and that promise is
    only as good as this. Calling `rank` a "hint" was being left to read as licence to
    vary it.
  - The prohibition on `rank` costing I/O is now a **should** with guidance. As a
    "must not" it forbade the only sensible option for a provider whose index answers
    existence cheaply but can only count by enumerating, while the next paragraph
    disparaged the sole remaining alternative.
  - `allowed` is documented as always non-empty, which propagation guarantees, so an
    implementation does not need to guess whether it needs that case.

- A `best` of ∅ now gets its own error naming the `found`/`best` disagreement, rather
  than being reported as "outside the allowed set" — true of ∅ only in a lawyerly
  sense, and it sent a provider author hunting for a range problem.

### Notes

- The 0.2.0 migration recipe (`found: count > 0`, `rank: count`) is behaviour-identical
  for the **ordering**, which is what it was about, but 0.2.0 also tightened
  `MakeDecision` to require `best` to be contained in `allowed` rather than merely
  overlapping it. A provider that had been returning a non-singleton `best` overlapping
  the accumulated term compiled and ran at 0.1.0 and errors now. That was listed under
  0.2.0's `Fixed`, but the migration note did not cross-reference it, so anyone who
  followed the recipe and then hit `"outside the allowed set"` had been told the upgrade
  preserved behaviour.

## [0.2.0] - 2026-08-13

### Breaking

- `solver.Provider.Candidates` now returns `(best S, found bool, rank int, err error)`
  instead of `(best S, count int, err error)`. Every implementation must be updated;
  the change is mechanical, and for most providers it also makes them much cheaper.

  The single `count` was carrying two unrelated obligations. Whether it was **zero**
  was correctness-bearing — the solver derives a `KindNoVersions` incompatibility from
  it — while **how large** a nonzero count was only fed the package-choice heuristic
  (which package to work on next), which the prose sources and this library's own
  documentation call tunable. Because the two shared one return value, satisfying the
  correctness half appeared to require the exactness the heuristic half wanted, and an
  exact count means establishing that every version in range is usable.

  Splitting them makes the correctness half an *existence* question. A **true** answer
  is dischargeable by finding one usable version and stopping; a **false** one still
  requires examining everything in range, since that is what proving absence means, and
  that cost is irreducible. What the split removes is paying the exhaustive walk on
  every package rather than only where the answer really is "nothing". Measured on a
  932,861-package index, a provider rewritten this way read **31.6x fewer** metadata
  records while producing identical resolutions and identical failure explanations
  across 200 packages.

  `rank` is a hint in the strict sense: the solver only ever compares one against
  another and never reads it as a quantity. It need not be a count, an upper bound, or
  non-negative. Counting the in-range versions before testing usability is the intended
  implementation.

  To migrate: return `found: count > 0` and `rank: count` for behaviour identical to
  before, then make `found` short-circuit and `rank` cheap when convenient.

  ⚠️ `rank` is ignored when `found` is false, and unavailability is ordered ahead of
  every available package by the solver. Providers do not need to encode that in
  `rank`, and must not rely on a sentinel value to achieve it.

  ⚠️ A constant `rank` is legal and is a bad idea: it disables the heuristic, and
  measured against a real index that silently moved pins to a different *valid* answer.

### Fixed

- `MakeDecision` now requires `best` to be **contained in** the allowed set, not merely
  to overlap it. `ps.Eligible` tests non-disjointness, which coincides with containment
  only for a single version — and singleton-ness is the one property of `best` the solver
  cannot check, since `versionset.Set` has no singleton predicate. A provider returning a
  range that merely overlapped the accumulated term therefore had it accepted, decided,
  and carried into `Solution.Selected`: a decision outside the accumulated term, arriving
  by the one route the disjointness check could not see, with `ps.Decide`'s guard and
  `Solve`'s final `ViolatedBy` pass both blind to it.

### Changed

- `Provider.Candidates`' documentation no longer claims the count "drives §8's
  heuristic and nothing else" — it also drove the correctness-bearing unavailability
  branch. It also no longer implies the solver checks that `best` is a single version:
  `versionset.Set` has no singleton predicate, so a range returned there is recorded
  as the chosen version. That is an uncatchable provider bug, now documented as one.

## [0.1.0] - 2026-08-12

First release. The algorithm is complete: unit propagation, decision making,
conflict-driven backjumping, and error reporting.

### Added

- `versionset/` — the version-set abstraction an ecosystem supplies: intersection,
  union, complement, subset and disjointness over a canonical representation.
  `Ints` is a reference implementation over integer versions, used by the tests.

- `term/` — the term algebra. A term is a version set plus a polarity, and the
  asymmetry that "no version selected" makes every negative term true and every
  positive term false is the fact the rest of the library rests on.

- `solver/` — the solve loop. Incompatibility store with a derivation graph, unit
  propagation, decision making, and conflict-driven backjumping. `Solve` returns
  either a `Solution` or an `*Unsolvable` carrying the root-cause incompatibility.

  Two details worth knowing when reading a trace. First, the backtrack floor when
  there is no previous satisfier is **read off the partial solution** rather than
  hard-coded: it is the level of the first decision, or the current level when no
  decision has been made yet. ⚠️ Note the level numbering here starts the first
  decision at **1**, while the primary source's worked examples number that same
  decision **0** — so the floor this code computes as `1` is the one those examples
  call `0`. They agree on behaviour ("keep the root decision, discard everything
  above it") and differ only in where counting starts.

  Second, a solution is verified against the whole incompatibility set before being
  returned, because the published success criterion cannot see an all-negative
  incompatibility whose packages are both unassigned.

  `MaxRounds` bounds the outer loop. Neither prose source proves it terminates in
  bounded rounds for arbitrary graphs, so a caller facing untrusted input has
  somewhere to put a limit instead of hanging. It defaults to unbounded.

- `report/` — a failed solve rendered as prose. `Explain` and `FromError` walk the
  derivation graph and produce a `*Report`, whose `Line` values carry the sentence
  plus the incompatibility it states and the lines it cites, so a consumer can build
  its own presentation without parsing English or re-deriving §9's ordering and
  line-numbering rules. `Formatter` supplies the package and version-range
  vocabulary; it defaults to `fmt.Stringer`.

### Notes

- **No dependencies, and that is intended to stay true.** A generic algorithm library
  should not drag anything in.
- **Nothing here knows about any packaging ecosystem.** No PEP 440, no semantic
  versioning, no pre-release rules — those belong to the adapter. That is what lets
  the same solver serve Python, R or anything else, and it is the reason this module
  is separate at all.
- **Written from published prose, not translated from another implementation.** See
  `CONTRIBUTING.md` and `docs/ALGORITHM.md`, which lists the sources it was derived
  from.
