# Changelog

All notable changes to this module are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
While the major version is `0`, breaking changes ship in a **minor** bump rather
than a major one, and are always listed under a `Breaking` heading so they are not
mistaken for a safe patch upgrade.

## [Unreleased]

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

  Two details worth knowing when reading a trace: the backtrack floor when there is
  no previous satisfier is decision level **0**, which is what the primary source's
  own worked examples do rather than what its prose says; and a solution is verified
  against the whole incompatibility set before being returned, because the published
  success criterion cannot see an all-negative incompatibility whose packages are
  both unassigned.

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
