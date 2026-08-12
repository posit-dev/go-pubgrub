# go-pubgrub

A Go implementation of [PubGrub](https://medium.com/@nex3/pubgrub-2fb6470504f),
the version-solving algorithm behind Dart's `pub`, with ports in Swift, Rust,
Ruby and elsewhere.

Deliberately **language-agnostic**: it knows about versions, ranges, and
constraints, and nothing about any particular package ecosystem. Supply an
ordering and a set representation and it will solve for you.

Its first consumer is
[`go-pyresolver`](https://github.com/posit-dev/go-pyresolver), which supplies
Python packaging semantics — but this module carries no Python-specific code, so
it can serve R, Julia, or anything else.

> **Status: `v0.1.0`.** The algorithm is complete — `term/`, `versionset/`, `solver/`
> and `report/` cover unit propagation, decision making, conflict-driven
> backjumping, and rendering a failed solve's derivation graph as prose. Pre-1.0:
> the API may still change, and while the major version is `0` a breaking change
> ships in a minor bump. See [`CHANGELOG.md`](CHANGELOG.md).

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

## How this was written

From the published algorithm descriptions, not translated from another
implementation. [`docs/ALGORITHM.md`](docs/ALGORITHM.md) is the specification
this code implements and lists the prose sources it was written from.

Credit for the algorithm belongs to Natalie Weizenbaum and the Dart team, who
published it as prose precisely so it could be implemented elsewhere.

If you are contributing, please see [`CONTRIBUTING.md`](CONTRIBUTING.md).

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
