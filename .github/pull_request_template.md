## What

<!-- What does this change do, and why? -->

## Independence

This library implements the PubGrub algorithm from its published prose descriptions. See
[`CONTRIBUTING.md`](../blob/main/CONTRIBUTING.md).

Paste the result of the provenance audit — it is a fact about this change, not a promise:

```
.claude/skills/independence-audit/audit.sh --paths '%gpb-worktrees/<branch>%' --paths '%/scratchpad/gpb/%'
```

> Verdict: <PASS | REVIEW | NO COVERAGE> · lineage: <n> sessions · log covers: <date> onward

<!--
NO COVERAGE is not a pass; it means the audit could not answer. Usually the --paths pattern
matched nothing. Widen it rather than reporting it as clean.

A PASS is a statement about process: what was opened, fetched and searched. It is not a
statement about what a model's weights encode, and nothing you can tick here would be.

Do NOT paste a REVIEW finding into this description. An adverse similarity finding in a public
record asserts resemblance to copyleft code. Send it to a maintainer directly.
-->

If you have read another implementation, say so here. That is not automatically unwelcome; it
just needs to be reviewed on that basis.

<!-- Notes, if any. -->

## Testing

<!-- What did you run, and what did it show? -->

<!--
For a bug fix: revert only the fix and confirm the test fails for the reason it claims. A test
that passes with and without the change is not testing the change.
-->
