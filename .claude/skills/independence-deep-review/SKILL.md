---
name: independence-deep-review
description: Use only in a dedicated throwaway session, when explicitly asked for a deep, contamination, or similarity review of go-pubgrub against other PubGrub implementations - typically before cutting a tag, or after the independence-audit skill returns REVIEW. This skill reads MPL-2.0 reference source and permanently contaminates the session that runs it.
---

# Deep independence review

> **This skill contaminates the session that runs it. That is its design, not a
> side effect.**

Checking whether this library resembles another implementation requires reading
that implementation. Reading it is the act the independence claim says did not
happen. The only way to have both is to put the reading in a session that will
never write this library's code, and never let its output reach one that does.

## Stop and check before doing anything

Answer all four. If any is "no", **stop and say so** rather than proceeding.

1. Is this a **fresh session**, started for this review alone?
2. Has this session written **no** go-pubgrub code, and will it write none?
3. Is the working copy a **scratch clone**, not a worktree you develop in?
4. Did the operator ask for a *deep* review, knowing it is contaminating?

A session that has been designing, planning, or discussing this library all day
is **not** fresh. Context carries. Say so and ask for a new one.

## Hard rules

These are not style preferences. Each one closes a channel that has actually
leaked before.

- **Never edit go-pubgrub source.** Not a fix, not a typo, not a comment.
- **Never write any file inside a repository working tree.** A review report
  committed to `docs/` becomes a public artifact asserting resemblance to
  copyleft code. One was very nearly committed here.
- **Never write to a memory directory or a handoff file.** This is the channel
  that turns a one-session problem into a permanent one: every future session
  reads those. It is the single most important rule on this list.
- **Never post findings to GitHub** — no pull request comment, no issue, no
  commit message. Adverse findings go to a maintainer directly.
- **Never suggest how to change the code.** See below.

Write the report to, and only to:

```
~/independence-reviews/<YYYY-MM-DD>-<short-sha>/report.md
```

Outside every repository, so no `.gitignore` has to be trusted.

## The report can carry the contamination

This is the failure mode that makes naive reviews worse than none. A reviewer who
writes *"they do it this way, consider matching it"* — or *"consider avoiding
it"* — has laundered the reference's expression into the implementer's context.
The report is then the leak.

So constrain every finding to:

1. **An anchor in _our_ file:line.** Never their file, never their identifiers.
2. **A classification:**
   - `forced` — any correct implementation would look like this. The algorithm
     or the language leaves no choice. Not a concern.
   - `convergent` — plausibly independent, but worth a second look.
   - `suspicious` — resemblance beyond what the problem forces.
3. **Divergence evidence** — what differs, stated in terms of our code.

Nothing else. No reference-side snippets, no their-name-for-it, no "they call
this X".

**"Cannot elaborate without contaminating, needs human review" is a complete and
valid finding.** Prefer it whenever unsure. A vague finding a human resolves
beats a precise one that leaks.

If reference-side detail is genuinely necessary to act, put it in a clearly
marked appendix at the end of the report, so the reader can decide not to read
it. Never inline it.

## What to compare

Structural and textual resemblance beyond what the algorithm forces:

- Identifier names that match a published implementation where the spec's own
  vocabulary differs.
- Comment structure or wording that mirrors another codebase.
- Decomposition into the same helpers with the same boundaries, where the spec
  suggests no particular split.
- Test names, test data, and fixture ordering — these copy easily and are
  strong evidence either way.
- Rust idioms rendered awkwardly in Go, which is what a translation looks like.

Anchor everything against [`docs/ALGORITHM.md`](../../../docs/ALGORITHM.md).
Resemblance traceable to a spec section is `forced` by definition — the spec is
the shared ancestor, and that is the whole point of having written it.

Reference implementations are listed in
[`../independence-audit/references.txt`](../independence-audit/references.txt).

## Remediation is a separate session, and it is not told why

If something must change, **do not fix it here**, and do not write a prompt that
explains the resemblance. A remediation prompt that says "this looks like
pubgrub-rs's arena, rewrite it" has re-contaminated the implementer.

Brief a fresh authoring session with the *task only*:

> Rewrite `solver/incompatibility.go` from `docs/ALGORITHM.md` §7.3. Work only
> from that section. Do not consult any other source.

The reason stays with the maintainer.

## Finish

1. Write the report to the path above.
2. Tell the operator the path and the counts per classification. Nothing more.
3. Remind them the session is contaminated and should be **closed, not reused**.

Do not summarize the findings into chat beyond the counts. The chat log is
context too, and if the operator continues in this session, everything in it is
still live.
