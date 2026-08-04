# Clean-room policy

This repository implements the PubGrub algorithm from published prose only. That
independence is a legal property of the code, not a stylistic preference, and it
cannot be restored once lost — so the policy is enforced rather than merely
documented.

Derived from [RFD 0001 §7.1](https://github.com/rstudio/package-manager/blob/main/docs/rfds/0001-pypi-native-resolver/README.md).

## Why

`pubgrub-rs/pubgrub` and its forks, including `astral-sh/pubgrub`, are licensed
under the **Mozilla Public License 2.0**, whose copyleft operates at file level.
Reading that source while writing ours creates a contamination risk we would not
be able to disprove later.

Other-language ports — `contriboss/pubgrub-go` and similar — are permissively
licensed, so licensing is not the concern there. The concern is that if our
output resembles theirs structurally, we cannot defend it as independently
derived. Both risks are avoided the same way: don't read them.

The algorithm itself is published prose, written expressly so it could be
implemented in other languages. Nothing here requires reading anyone's code.

## Sources

**Permitted while implementing:**

- Natalie Weizenbaum's PubGrub article
- The Dart `pub` solver documentation prose
- The PubGrub guide's prose pages — the explanatory text, **not** the source code
  it links to
- Published test fixtures, such as packse scenarios and published lockfiles

**Not permitted:**

- `pubgrub-rs/pubgrub` source (MPL-2.0)
- `astral-sh/pubgrub` source (fork of the above; same license, same risk)
- `contriboss/pubgrub-go`, or any other Go PubGrub implementation
- Any PubGrub implementation source in any language
- `uv`'s resolver source

**Gray area:** Swift Package Manager's implementation is Apache-2.0 and legally
safe to read. Treat it as one behavioral spec among several rather than as a
translation source, and disclose it in the pull request if you consulted it.

## The derivation of record

[`docs/ALGORITHM.md`](docs/ALGORITHM.md) is a specification of the algorithm
written from the permitted prose sources, carrying its own attestation of exactly
what was read.

**Implement from that document, not from the web.** This is the practical form of
the policy: if the specification is complete enough, the implementation step needs
no external source at all, and there is no opportunity to wander into a forbidden
one. It also gives a reviewer something auditable — the derivation is a file in
the repository rather than an unverifiable claim about someone's browser history.

If the specification is wrong or incomplete, fix the specification from the
permitted sources first, then implement. Do not go around it.

Its §11 records the places prose did not settle the question — including one
where the Dart document contradicts itself about the backtrack floor. Those are
the spots that need the most test coverage.

## Controls

A note asking people to be careful is not a control. These are:

1. **Attestation in every pull request.** `.github/pull_request_template.md`
   carries the statement; contributors tick it only if it is true of that diff.
2. **CI enforcement.** `.github/workflows/clean-room.yml` fails any pull request
   that changes Go files without a ticked attestation. It skips automation
   authors and documentation-only changes, so it blocks exactly where it matters.
3. **Dependency assertion.** The same workflow fails if a known PubGrub
   implementation ever appears in `go.mod` or `go.sum`, which no attestation
   would catch.
4. **Contamination-diff review.** A named reviewer who *has* read `pubgrub-rs`
   periodically compares our structure against it as a sanity check. That person
   is therefore **barred from writing solver code** in this repository — their
   value is precisely that they are contaminated and we are not.

   > **This role is unassigned.** Until someone is named, control 4 is not in
   > effect and the first three are carrying the policy alone. Assigning it is a
   > people decision, tracked in rstudio/package-manager#18652.

## If you have already read an implementation

Say so, and do not write solver code here. You can still contribute
documentation, tests derived from published fixtures, review that does not
propose implementation structure, and the contamination-diff role above. This is
not a judgement about anyone; it is what keeps the claim defensible.

## For LLM-assisted work

RFD §7.1 addresses this directly, and it is the most likely way the policy gets
broken by accident:

- Restrict the model's context to permitted sources. Do not paste forbidden
  source into a prompt, and do not let an agent browse to it — an agent with web
  access will find `pubgrub-rs` on the first search for "pubgrub implementation".
- Review generated code for translation artifacts: non-idiomatic Go that reflects
  Rust idioms, variable names matching a published implementation, comment
  structure mirroring another codebase.
- Prefer working from a written specification derived from the prose, so the
  implementation step needs no external sources at all.
