# go-pubgrub — Claude Code guide

A language-agnostic Go implementation of the PubGrub version-solving algorithm.

> **Status: in progress.** `term/`, `versionset/` and most of `solver/` are
> implemented. Conflict-driven backjumping, decision making, and `report/` are
> not yet written.

## 🔴 READ THIS FIRST: implement from the prose, not from another implementation

See [`CONTRIBUTING.md`](CONTRIBUTING.md). Before writing any solver code:

**Do NOT read** `pubgrub-rs/pubgrub` or `astral-sh/pubgrub` (both MPL-2.0,
file-level copyleft), `contriboss/pubgrub-go` or any other Go PubGrub port, any
PubGrub implementation in any language, or `uv`'s resolver.

**Do read** Weizenbaum's article, the Dart `pub` solver prose, the PubGrub
guide's prose pages (not the code they link), and published test fixtures.

This is the inverse of the sibling repositories. In `go-pyresolver` and
`go-python-packaging`, reading `uv`'s crates is *explicitly encouraged* and
adapting with attribution is the intended pattern. Here it is forbidden. If you
work across both in one session, do not carry context from a permitted source in
one repo into implementation work in this one.

### Implement from `docs/ALGORITHM.md`

[`docs/ALGORITHM.md`](docs/ALGORITHM.md) is a specification derived from the
permitted prose, and it lists the sources it was written from. **Work from it
rather than from the web.** If it is wrong or incomplete, fix it from the
permitted sources first and then implement — do not go around it.

Its §11 lists what prose could not settle. Two entries deserve tests before the
solver is trusted:

- **The backtrack floor when there is no previous satisfier.** The Dart document's
  prose says decision level 1; all six of its own worked examples use level 0, and
  one narrates backtracking "all the way to level 0" in exactly that situation.
  The spec resolves this as **0** and says so. Needs a dedicated test for
  under- and over-backtracking at the very first decision.
- **The joint-satisfaction correction in conflict resolution.** Demonstrated by
  one worked identity, not proven for every shape of range overlap. Wants
  property-based tests: fuzz pairs of ranges and confirm the derived prior cause
  is logically implied by its two parents.

Also noted there: neither source proves the outer conflict-resolution loop
terminates in bounded rounds for arbitrary graphs. Correctness is not in doubt,
but a max-iteration safety valve is worth considering for production, kept
separate from correctness.

### Specific to LLM-assisted work

- **Never give an implementation agent web access.** An agent asked to
  "implement PubGrub" will search, and the first result is `pubgrub-rs`. Work
  from a written specification derived from the prose instead, so the
  implementation step needs no external source at all.
- When delegating, state the prohibition explicitly in the prompt. It is not
  enough to omit the forbidden sources; name them and say not to open them.
- Review generated code for translation artifacts: Rust idioms rendered awkwardly
  in Go, variable names that match a published implementation, comment structure
  that mirrors another codebase.

### Every pull request needs a provenance audit

Run it before merging any change to Go files:

```bash
.claude/skills/independence-audit/audit.sh \
  --paths '%gpb-worktrees/<branch>%' --paths '%/scratchpad/gpb/%'
```

Exit 0 is `PASS`, 2 is `REVIEW`, **3 is `NO COVERAGE` and is not a pass**. Put the
verdict in the pull request. Full instructions live in the skill.

Always include the scratchpad pattern. Code gets drafted under
`/private/tmp/.../scratchpad/`, and a sweep that misses it reports a clean lineage
that never existed.

There is no longer a CI gate for this. Requiring a ticked box in the pull request
description asked the author to gate the author, and an agent wanting a green
build ticked it. `.github/workflows/independence.yml` still fails if a PubGrub
implementation appears in `go.mod` or `go.sum` — the one part of this that a
machine can actually know.

**Do not run `independence-deep-review` in a session that touches this code.** It
reads reference source. It is for a dedicated throwaway session.

## Module layout

| Package | Scope |
|---|---|
| `term/` | Term algebra: allowed/disallowed version sets and their relations |
| `versionset/` | The version-set abstraction an ecosystem supplies |
| `solver/` | Unit propagation, decision making, conflict-driven backjumping |
| `report/` | Failed resolution to human explanation |

## Design constraints

**Nothing here may know about any packaging ecosystem.** No PEP 440, no semantic
versioning, no pre-release semantics. Those live in the adapter
([`go-pyresolver`](https://github.com/posit-dev/go-pyresolver)). A version
comparison that "knows" a pre-release sorts below its release is an ecosystem
rule and does not belong here. This is what lets the same solver serve future R
or Julia resolution, which is the stated reason the module is separate at all.

**The solver performs no I/O.** It asks its caller for facts through an
interface. That keeps it testable against fixtures and is why it can be driven by
an air-gapped index as easily as a networked one.

**`term/` and `versionset/` stay separate.** A bug in "does term A satisfy term
B" and a bug in "what is the intersection of these two ranges" are different
bugs; merging the layers makes both harder to isolate.

**Error reporting is a feature, not a nicety.** The derivation graph is much of
why PubGrub is worth implementing. Keep `report/` separate from `solver/` so the
message format can change without touching the algorithm.

## Build & test

```bash
go build ./...
go test ./...
go vet ./...
```

Module floor is **Go 1.25**, matching the sibling modules. No dependencies, and
that is worth preserving — a generic algorithm library should not drag anything
in.

## Code Style

- Follow standard Go conventions.
- **Formatting:** always run `gofmt` before committing. `gofmt -w .` in place, or
  `gofmt -l .` to list unformatted files (must print nothing).
- **License header:** every `.go` file begins with, as its first line:
  ```go
  // SPDX-License-Identifier: Apache-2.0 OR MIT
  ```

### Verify lint matches CI before claiming "lint clean"

CI runs `golangci-lint` via `golangci/golangci-lint-action@v9`, pinned to
golangci-lint **v2.11.2**. Reproduce it from the module root:

```bash
golangci-lint config verify
golangci-lint run ./...
```

Run it **from the module root**. A path outside the module prints a reassuring
`0 issues.` beside a typechecking error, having scanned nothing.

`.golangci.yml` uses the v2 schema, where `gofmt` is a **formatter** under the
top-level `formatters:` block, never under `linters:`.

## Testing notes

The published test corpora are the point of leverage here. `astral-sh/packse`
(Apache-2.0/MIT) defines resolution scenarios as TOML, usable as oracle
fixtures — and using published *fixtures* is explicitly permitted by the policy,
unlike reading implementation source.

A SAT-oracle cross-check of the whole solver is planned: generate random
dependency universes, solve each with the solver and with a SAT solver, and assert
they agree on satisfiability. That catches wrong answers rather than crashes,
which is the failure mode a resolver actually has.
