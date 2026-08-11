# Contributing to go-pubgrub

## Implement from the published descriptions

This library is written from the published prose descriptions of the PubGrub algorithm, listed in
[`docs/ALGORITHM.md`](docs/ALGORITHM.md). That document is the specification this code
implements, and it is the right thing to work from — it is more precise than the code about
*why* each rule is what it is.

**Please do not contribute code derived from another PubGrub implementation.** Some are under
licenses whose terms we cannot accept, and independent derivation is a property worth keeping
intact for all of them. If you have read one and want to contribute anyway, say so in your pull
request so we can review the change on that basis. Working from `docs/ALGORITHM.md` avoids the
question entirely.

The dependency check in CI enforces the part that can be checked mechanically: the module graph
must stay free of other solver implementations.

## How independence is checked

CI cannot check this. It once tried, by requiring a ticked checkbox in the pull request
description — but the description is written by the author, so the check asked the author to
gate the author. An agent that wanted a green build simply ticked it. That gate is gone; two
local steps replaced it.

**Before merging any change to Go files**, run the provenance audit:

```bash
.claude/skills/independence-audit/audit.sh --paths '%gpb-worktrees/<branch>%' --paths '%/scratchpad/gpb/%'
```

It answers a narrow, checkable question — did the sessions that wrote this change ever retrieve
source from a known PubGrub implementation? — from this project's own agent activity log. It
never opens a reference implementation, so it is safe to run anywhere. Report the verdict in the
pull request.

**Before cutting a tag**, or after the audit reports `REVIEW`, run the deep review
(`.claude/skills/independence-deep-review`). That one compares this code against other
implementations, so it must read them, so it runs in a dedicated throwaway session that never
writes this library. Its output never goes in a repository, a pull request, or an issue.

Both skills document their own rules. The short version: the contaminating step happens rarely,
deliberately, and never in a session that writes code.

### What a passing audit does and does not say

It is a statement about **process** — what was opened, fetched and searched. It is not a
statement about what a model's weights encode, and no checkbox, human or otherwise, can be.
Claims in this repository are worded to match what is actually checkable, and should stay
that way.

## Where the specification and the code disagree

Prefer the specification, and then fix the specification if it turns out to be wrong.

`docs/ALGORITHM.md` §11 records the points where its sources were ambiguous, contradicted each
other, or contradicted themselves — including one case where the specification's own truth table
disagrees with its own worked example, and the worked example is right. If you hit a
disagreement, add it to §11 rather than resolving it silently in code. The next person needs the
reasoning, not just the outcome.

## Tests

**Assert the law the type claims to uphold, not the behaviour the implementation happens to
have.** An implementation and a test written from the same misunderstanding will agree with each
other, and that has already produced several defects here that shipped with a passing test
beside them.

Concretely, for a bug fix:

1. Write the test.
2. Revert *only* the fix and confirm the test fails, for the reason it claims.
3. Restore the fix.

If a test passes with and without the change, it is not testing the change. Consider also
mutating the code in the *opposite* direction — a fix for "this rejects too much" should not be
satisfiable by something that accepts everything.

The worked example in `docs/ALGORITHM.md` §10 is transcribed as a test. When conflict resolution
lands, extend that test rather than starting a new one; it is the one place the expected answers
are stated independently of the code.

## Build and test

```bash
go build ./...
go test -race ./...
gofmt -l .
golangci-lint run ./...
```
