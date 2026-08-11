---
name: independence-audit
description: Use before merging any pull request that changes Go files in go-pubgrub, and whenever asked to check, audit, or attest this library's clean-room independence. Determines whether the agent sessions that authored a change ever retrieved source from another PubGrub implementation. Safe to run in any session; it never opens a reference implementation.
---

# Independence provenance audit

This library is written from published prose, not from another PubGrub
implementation's source. See [`CONTRIBUTING.md`](../../../CONTRIBUTING.md) for
why that is a legal property and not a preference.

This skill answers one question: **did the sessions that authored this change
ever retrieve source from a known PubGrub implementation?**

It reads only this project's own agent activity log. It never opens a reference
implementation, so running it cannot contaminate any session. Run it anywhere.

## What this is not

It does not compare code against another implementation. That is the
`independence-deep-review` skill, which must read reference source and therefore
runs only in a dedicated session that never authors this library. **Do not do
that work here.**

It also cannot speak to what model weights encode. A `PASS` is a statement about
process — what was opened, fetched and searched — not about the derivation of the
code. Say it that way when you report it.

## Run it

```bash
.claude/skills/independence-audit/audit.sh --paths '<sql-like-pattern>'
```

The pattern matches the absolute path of files that were written. Use the
worktree the branch was developed in:

```bash
.claude/skills/independence-audit/audit.sh --paths '%gpb-worktrees/18655-backjumping%'
```

Add `--session <id>` when you know the authoring session id. The path sweep is a
heuristic and the declared id is author-supplied; each covers the other's gap.

### The scratchpad trap, in both directions

Code does get drafted under `/private/tmp/.../<session-id>/scratchpad/`, and a
sweep that misses it can report a clean lineage that never existed. But the
scratchpad path is **not branch-scoped**, so a global pattern like
`--paths '%/scratchpad/gpb/%'` sweeps in every session that ever drafted this
library's code anywhere — including sessions with no connection to the change
under audit. Each one drags its entire tree into the lineage.

That is not hypothetical. On the `#18655` branch, adding that global pattern took
the lineage from 15 sessions to 37 and flipped the verdict from `PASS` to
`REVIEW` purely by false attribution.

Scope it to the session instead, once the worktree sweep has told you which
session that is:

```bash
--paths '%/376f3ee6-d928-4bec-b385-54a1b8690dc9/scratchpad/%'
```

The audit prints every session it treated as authoring, with the file count and
time range each matched. **Read that list.** A session you do not recognise means
the pattern is too broad.

`AGENTSVIEW_DB` overrides the log location (default `~/.agentsview/sessions.db`).

## Read the exit code, not the prose

| exit | verdict | means |
|---|---|---|
| 0 | `PASS` | No known reference retrieved anywhere in the authoring lineage. |
| 2 | `REVIEW` | At least one retrieval found. Escalate to a human. |
| 3 | `NO COVERAGE` | The log cannot answer. **Not a pass.** |
| 4 | usage error | Bad invocation. |

`NO COVERAGE` is the one people get wrong. "No evidence of contamination" and
"evidence of no contamination" are different findings, and the usual cause is a
`--paths` pattern that matches nothing rather than a clean history. Widen the
pattern and check the printed coverage window against when the code was written.

## Lineage is the whole session tree

The audit climbs from the authoring session to its root, then descends over every
subagent. Descendants alone are not enough: the channel that matters most here is
a reviewer subagent reading a reference, reporting to the parent, and the parent
relaying it into a **sibling** subagent's authoring prompt. Walking down only
would miss that entirely.

## Reading a REVIEW

A finding is not automatically a violation, and the ordering is what tells them
apart.

- A retrieval **after** the code it could affect was created is a post-hoc
  review. That is the correct order for a contamination review.
- A retrieval **before** is a possible source.

Compare finding timestamps against when the files were **created**, not merely
edited. Both facts are in the log.

**Do not read the results of a flagged call to decide how serious it is.** That
is the contaminating act, and doing it in a session that writes go-pubgrub code
is precisely the failure this whole process exists to prevent. Establishing what
reached the code is the deep review's job.

The report also has two non-finding sections. Read them; they are context, not
noise:

- **Context — subagent spawns naming a reference.** Telling a subagent "do not
  open pubgrub-rs" is the policy working. But a deliberate reference-reading
  review shows up here first, so it is the fastest way to understand *why* a
  finding exists.
- **Cross-repo risk.** Reading `uv` is encouraged in `go-pyresolver` and
  `go-python-packaging` and forbidden here. One session tree routinely does
  both. These entries mark a session where that prohibition was live.

## Maintaining the pattern list

[`references.txt`](references.txt) holds three classes: forbidden, permitted
(subtracted from forbidden), and cross-repo risk. The permitted list is
load-bearing — `docs/ALGORITHM.md` is written *from* the Dart prose and the
guide's prose pages, so an audit that flags them cries wolf on exactly the
sources the policy tells you to use, and a detector that cries wolf gets ignored.

The forbidden list is inherently incomplete. Adding a pattern is cheap; do it
whenever a new implementation becomes findable.

## Verify the detector still works before trusting it

Two known cases are pinned in this repository's own history. If either stops
behaving, the audit is broken and its verdicts are worthless:

```bash
# Must exit 0 (PASS) -- the #18655 backjumping work, 15 sessions, no retrieval.
.claude/skills/independence-audit/audit.sh --paths '%gpb-worktrees/18655-backjumping%'

# Must exit 2 (REVIEW) -- the 2026-08-04/05 contamination reviews,
# 49 retrievals across six implementations.
.claude/skills/independence-audit/audit.sh \
  --paths '%gpb-worktrees/incompat-propagation%' \
  --paths '%gpb-worktrees/term-versionset%'
```

A detector with no negative control is worthless: one that always says `REVIEW`
would "pass" the second check while telling you nothing. Run both.

## Reporting the result

Put the verdict, the lineage size, and the coverage window in the pull request.
Concretely, in the Independence section:

> Provenance audit: `PASS`, 15 sessions in lineage, log covers 2025-03-20 onward.
> No known reference implementation retrieved. This is a process finding and does
> not speak to model weights.

**Never publish a `REVIEW` finding, or any deep-review output, to a public pull
request, issue, or commit message.** An adverse similarity finding in a public
record asserts resemblance to copyleft code — the opposite of what the `NOTICE`
is defending. Adverse findings go to a maintainer directly.
