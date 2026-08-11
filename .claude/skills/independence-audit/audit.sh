#!/usr/bin/env bash
# Provenance audit for go-pubgrub's independence policy.
#
# Answers ONE question: did the agent sessions that authored a given set of files
# ever retrieve source from a known PubGrub implementation?
#
# It reads only this project's own agent activity log. It never opens a reference
# implementation, so running it cannot contaminate any session. See
# ../../../CONTRIBUTING.md for the policy and SKILL.md for how to read the output.
#
# Exit codes are the result. Read them, not the prose:
#   0  PASS         no known reference retrieved anywhere in the authoring lineage
#   2  REVIEW       at least one retrieval found; needs a human
#   3  NO COVERAGE  the log cannot answer (missing, or pruned past the window)
#   4  USAGE        bad invocation
#
# NO COVERAGE is deliberately not 0. "No evidence of contamination" and "evidence
# of no contamination" are different findings and a silent pass would conflate
# them.

set -euo pipefail

DB="${AGENTSVIEW_DB:-$HOME/.agentsview/sessions.db}"
SKILL_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REFS="$SKILL_DIR/references.txt"

usage() {
	cat >&2 <<'EOF'
usage: audit.sh --paths <sql-like-pattern> [--paths ...] [--session <id>] [--since <iso8601>]

  --paths    Where the code was authored, as an SQL LIKE pattern against the
             absolute file path. Repeatable. At least one required.
             Include scratchpad paths: code gets drafted in /private/tmp too.
               e.g. --paths '%gpb-worktrees/18655-backjumping%'
                    --paths '%/scratchpad/gpb/%'
  --session  Additionally treat this session id as authoring, even if it wrote
             no matching file. Use when you know the id; the path sweep is a
             heuristic and this is the cross-check.
  --since    Ignore activity before this timestamp. Omit to audit all of time.

environment:
  AGENTSVIEW_DB  path to the AgentsView SQLite database
                 (default: ~/.agentsview/sessions.db)
EOF
	exit 4
}

PATHS=()
SESSIONS=()
SINCE=""
while [ $# -gt 0 ]; do
	case "$1" in
	--paths) [ $# -ge 2 ] || usage; PATHS+=("$2"); shift 2 ;;
	--session) [ $# -ge 2 ] || usage; SESSIONS+=("$2"); shift 2 ;;
	--since) [ $# -ge 2 ] || usage; SINCE="$2"; shift 2 ;;
	-h | --help) usage ;;
	*) echo "audit.sh: unknown argument: $1" >&2; usage ;;
	esac
done
[ ${#PATHS[@]} -gt 0 ] || usage

command -v sqlite3 >/dev/null 2>&1 || {
	echo "NO COVERAGE: sqlite3 is not installed." >&2
	exit 3
}
[ -r "$DB" ] || {
	echo "NO COVERAGE: cannot read $DB" >&2
	echo "  Set AGENTSVIEW_DB, or run 'agentsview sync' first." >&2
	exit 3
}
[ -r "$REFS" ] || {
	echo "NO COVERAGE: cannot read $REFS" >&2
	exit 3
}

# SQL string literal: double any single quote.
sqlquote() { printf "%s" "${1//\'/\'\'}"; }

# Build a predicate over an arbitrary SQL expression from one class of pattern in
# references.txt. Class is 'deny', 'allow' or 'cross'. Patterns come from a file
# in this repository, not from user input.
#
# Returns 1=0 when the class is empty, so an empty allowlist subtracts nothing
# rather than suppressing everything.
class_predicate() {
	local expr="$1" want="$2" first=1 out="" line cls
	while IFS= read -r line; do
		case "$line" in '' | '#'*) continue ;; esac
		case "$line" in
		'!'*) cls=allow; line="${line#\!}" ;;
		'?'*) cls=cross; line="${line#\?}" ;;
		*) cls=deny ;;
		esac
		[ "$cls" = "$want" ] || continue
		[ $first -eq 1 ] && first=0 || out+=" or "
		out+="$expr like '$(sqlquote "$line")'"
	done <"$REFS"
	[ $first -eq 1 ] && { echo "1=0"; return; }
	echo "($out)"
}

# A finding is a forbidden match that is not also a permitted one. The
# subtraction is what keeps the permitted prose sources -- which docs/ALGORITHM.md
# is written from -- out of the findings.
ref_predicate() {
	local expr="$1"
	echo "($(class_predicate "$expr" deny) and not $(class_predicate "$expr" allow))"
}

cross_predicate() {
	local expr="$1"
	echo "($(class_predicate "$expr" cross) and not $(class_predicate "$expr" allow))"
}

path_predicate() {
	local first=1 out="" p
	for p in "${PATHS[@]}"; do
		[ $first -eq 1 ] && first=0 || out+=" or "
		out+="tc.file_path like '$(sqlquote "$p")'"
	done
	echo "($out)"
}

seed_sessions() {
	local first=1 out="" s
	for s in "${SESSIONS[@]}"; do
		[ $first -eq 1 ] && first=0 || out+=" union "
		out+="select '$(sqlquote "$s")'"
	done
	[ $first -eq 1 ] && { echo ""; return; }
	echo " union $out"
}

REF_INPUT="$(ref_predicate "lower(coalesce(tc.input_json,''))")"
REF_FILE="$(ref_predicate "lower(coalesce(tc.file_path,''))")"
CROSS_INPUT="$(cross_predicate "lower(coalesce(tc.input_json,''))")"
CROSS_FILE="$(cross_predicate "lower(coalesce(tc.file_path,''))")"
PATHPRED="$(path_predicate)"
SEEDS="$(seed_sessions)"
SINCEPRED="1=1"
[ -n "$SINCE" ] && SINCEPRED="m.timestamp >= '$(sqlquote "$SINCE")'"

# Shared lineage CTE. Authoring sessions are those that wrote a matching Go file,
# plus any explicitly declared session.
#
# Lineage is the whole session TREE containing an authoring session, not just its
# descendants. Walking down only would miss the channel that matters most here: a
# reviewer subagent reads a reference, reports to the parent, and the parent then
# relays it into a SIBLING subagent's authoring prompt. So climb to the root
# first, then descend over everything.
LINEAGE="
with recursive authoring(s) as (
  select distinct tc.session_id from tool_calls tc join messages m on m.id = tc.message_id
   where tc.tool_name in ('Write','Edit','NotebookEdit')
     and lower(coalesce(tc.file_path,'')) like '%.go'
     and $PATHPRED and $SINCEPRED
  $SEEDS
),
ancestry(s) as (
  select s from authoring
  union
  select tc.session_id from tool_calls tc join ancestry a on tc.subagent_session_id = a.s
),
lineage(s) as (
  select s from ancestry
  union
  select tc.subagent_session_id from tool_calls tc join lineage l on tc.session_id = l.s
   where tc.subagent_session_id is not null
)
"

# Retrieval verbs: tools that pull content into a context. Write/Edit are
# deliberately absent -- the policy documents themselves NAME the forbidden
# repositories, and matching prose scored 138 false positives against 9 real
# retrievals on this repository's own history.
FETCH_TOOLS="'WebFetch','WebSearch','mcp__github__get_file_contents','mcp__github__search_code'"
# Reading a local clone. Bash is handled separately: it needs a fetch verb too,
# or 'grep pubgrub-rs go.mod' (which independence.yml itself runs) would flag.
READ_TOOLS="'Read','Grep','Glob'"

# Bash needs a fetch verb as well as a pattern match, or 'grep pubgrub-rs go.mod'
# -- which independence.yml itself runs -- would be reported as a retrieval.
# gh api is the channel that actually did the most work here, so it must be in
# this list; omitting it made an earlier hand-run of this audit miss 30 calls.
BASH_FETCH="(lower(coalesce(tc.input_json,'')) like '%clone%'
          or lower(coalesce(tc.input_json,'')) like '%curl%'
          or lower(coalesce(tc.input_json,'')) like '%wget%'
          or lower(coalesce(tc.input_json,'')) like '%gh api%'
          or lower(coalesce(tc.input_json,'')) like '%githubusercontent%')"

retrieval_query() {
	local inpred="$1" filepred="$2"
	echo "$LINEAGE
select m.timestamp, tc.session_id, tc.tool_name,
       replace(substr(coalesce(nullif(tc.file_path,''), tc.input_json),1,200), char(10), ' ')
  from tool_calls tc join messages m on m.id = tc.message_id
 where tc.session_id in (select s from lineage) and $SINCEPRED
   and ( (tc.tool_name in ($FETCH_TOOLS) and ($inpred or $filepred))
      or (tc.tool_name in ($READ_TOOLS) and $filepred)
      or (tc.tool_name = 'Bash' and $inpred and $BASH_FETCH) )
 order by m.timestamp;"
}

FINDINGS="$(retrieval_query "$REF_INPUT" "$REF_FILE")"
CROSS="$(retrieval_query "$CROSS_INPUT" "$CROSS_FILE")"

COUNTS="$LINEAGE
select (select count(*) from authoring), (select count(*) from lineage);"

COVERAGE="select coalesce(min(started_at),'-'), coalesce(max(started_at),'-'), count(*) from sessions;"

# Informational only: spawn prompts that NAME a reference. Not a violation --
# telling a subagent 'do not open pubgrub-rs' is the policy working. But it is
# how a deliberate reference-reading review gets found, so surface it.
MENTIONS="$LINEAGE
select m.timestamp, tc.session_id, coalesce(tc.subagent_session_id,'-'),
       replace(substr(coalesce(tc.input_json,''),1,160), char(10), ' ')
  from tool_calls tc join messages m on m.id = tc.message_id
 where tc.session_id in (select s from lineage) and $SINCEPRED
   and tc.tool_name = 'Agent' and $REF_INPUT
 order by m.timestamp;"

read -r AUTHORING_N LINEAGE_N < <(sqlite3 -separator ' ' "$DB" "$COUNTS")
IFS='|' read -r COV_MIN COV_MAX COV_N < <(sqlite3 -separator '|' "$DB" "$COVERAGE")

echo "go-pubgrub independence provenance audit"
echo "  database          $DB"
echo "  log coverage      $COV_N sessions, $COV_MIN .. $COV_MAX"
echo "  paths audited     ${PATHS[*]}"
[ ${#SESSIONS[@]} -gt 0 ] && echo "  declared sessions ${SESSIONS[*]}"
[ -n "$SINCE" ] && echo "  since             $SINCE"
echo "  authoring         $AUTHORING_N session(s)"
echo "  lineage           $LINEAGE_N session(s) including subagents"
echo

if [ "$AUTHORING_N" -eq 0 ]; then
	cat <<EOF
NO COVERAGE: no session in the log wrote a Go file matching those paths.

That is not a pass. Either the pattern is wrong, or the authoring sessions
predate the log, or they were pruned. Check the coverage window above against
when the code was written, and widen --paths to include scratchpad locations.
EOF
	exit 3
fi

MENTION_OUT="$(sqlite3 -separator '  ' "$DB" "$MENTIONS" || true)"
if [ -n "$MENTION_OUT" ]; then
	echo "Context -- subagent spawns naming a reference implementation."
	echo "Not violations: naming a source in order to forbid it is the policy working."
	echo "Read them anyway; a deliberate reference-reading review appears here first."
	printf '%s\n' "$MENTION_OUT" | sed 's/^/  /'
	echo
fi

CROSS_OUT="$(sqlite3 -separator '  ' "$DB" "$CROSS" || true)"
if [ -n "$CROSS_OUT" ]; then
	CROSS_N="$(printf '%s\n' "$CROSS_OUT" | grep -c . || true)"
	echo "Cross-repo risk -- $CROSS_N retrieval(s) of a source permitted in the sibling"
	echo "repositories but forbidden here. Not a violation on its own: one session tree"
	echo "routinely works on go-pyresolver and this library both. It means CLAUDE.md's"
	echo "'do not carry context across' prohibition was live during authoring."
	printf '%s\n' "$CROSS_OUT" | head -20 | sed 's/^/  /'
	[ "$CROSS_N" -gt 20 ] && echo "  ... $((CROSS_N - 20)) more"
	echo
fi

FINDING_OUT="$(sqlite3 -separator '  ' "$DB" "$FINDINGS" || true)"
if [ -z "$FINDING_OUT" ]; then
	cat <<EOF
PASS: no retrieval of a known reference implementation in $LINEAGE_N session(s).

Scope of this claim, stated so it is not over-read:
  - No KNOWN reference was retrieved. references.txt is not exhaustive.
  - It says nothing about what model weights encode. This is a process
    finding, not a statement about the derivation of the code.
  - It covers only sessions still in the log ($COV_MIN onward).
EOF
	exit 0
fi

cat <<EOF
REVIEW: retrieval of a known reference implementation found in the lineage.

Do NOT read the results of these calls in a session that writes go-pubgrub code.
Escalate to a human. Establishing what, if anything, reached the code is the
deep review's job (skill: independence-deep-review), which runs in a dedicated
session that never authors this library.

EOF
printf '%s\n' "$FINDING_OUT" | sed 's/^/  /'
echo
echo "Order matters: a retrieval AFTER the code it could affect was already"
echo "written is a post-hoc review. One BEFORE is a possible source. Compare"
echo "these timestamps against when the files were created, not merely edited."
exit 2
