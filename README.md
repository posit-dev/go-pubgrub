# go-pubgrub

A Go implementation of [PubGrub](https://medium.com/@nex3/pubgrub-2fb6470504f),
the version-solving algorithm behind Dart's `pub`, with ports in Swift, Rust,
Ruby and elsewhere.

Deliberately **language-agnostic**: it knows about versions, ranges, and
constraints, and nothing about any particular package ecosystem. Supply an
ordering and a set representation and it will solve for you.

Part of [RFD 0001 — Native Go PyPI Dependency Resolution](https://github.com/rstudio/package-manager/blob/main/docs/rfds/0001-pypi-native-resolver/README.md),
where it serves Python resolution via
[`go-pyresolver`](https://github.com/posit-dev/go-pyresolver) — but it carries no
Python-specific code, so it can serve R, Julia, or anything else.

> **Status: skeleton.** Packages are populated per RFD 0001 Phase 4. Nothing here
> is usable yet, and the module has no released version.

## Why PubGrub

Two reasons, and the second is underrated.

Naively walking a dependency graph produces wrong answers on real inputs.
Requirements routinely contain transitive multi-version conflicts — `foo>=1.0`
alongside a `bar` that transitively needs `foo<1.0` — and a breadth-first walk
either silently resolves them wrongly or hard-fails.

And when resolution genuinely cannot succeed, PubGrub can **explain why**. It
keeps a derivation graph recording which constraints forced the contradiction, so
the failure can be reported as a chain of reasoning rather than "no solution
found". For anyone who has debugged a dependency conflict, that is most of the
value.

## Packages

| Package | Scope |
|---|---|
| `term/` | The term algebra: allowed and disallowed version sets, and the relations between them |
| `versionset/` | The version-set abstraction an ecosystem supplies |
| `solver/` | Unit propagation, decision making, conflict-driven backjumping |
| `report/` | Turning a failed resolution into a human explanation |

## Independence

This is written from published prose, not translated from another
implementation. No source from `pubgrub-rs`, `astral-sh/pubgrub`, or any other
PubGrub implementation has been read while writing it.

That is a legal property rather than a boast: `pubgrub-rs` is MPL-2.0, whose
copyleft attaches at file level. The policy and the CI controls that enforce it
are in [`CLEAN-ROOM.md`](CLEAN-ROOM.md) — read it before contributing.

Credit for the algorithm belongs to Natalie Weizenbaum and the Dart team, who
published it as prose precisely so it could be implemented elsewhere.

## Build & test

```bash
go build ./...
go test ./...
go vet ./...
```

Module floor is **Go 1.25**.

## License

Dual-licensed under **Apache-2.0** OR **MIT** at your option. Every source file
carries `SPDX-License-Identifier: Apache-2.0 OR MIT`. See [`NOTICE`](NOTICE).
