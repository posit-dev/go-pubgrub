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
