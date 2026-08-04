# PubGrub Version-Solving Algorithm — Implementation Specification

## Attestation

This specification was written from scratch, in the author's own words, for the purpose of
an independent Go implementation. It is based exclusively on the following prose sources,
read directly (not summarized by a third-party model):

- Natalie Weizenbaum, "PubGrub: Next-Generation Version Solving," Medium.
  <https://nex3.medium.com/pubgrub-2fb6470504f> (fetched and read in full as rendered
  article text, including the worked "menu/dropdown/icons/intl" example and the footnotes).
- The Dart `pub` team's solver design document, `doc/solver.md`, in the `dart-lang/pub`
  GitHub repository. <https://github.com/dart-lang/pub/blob/master/doc/solver.md>
  (fetched as raw Markdown from
  `https://raw.githubusercontent.com/dart-lang/pub/master/doc/solver.md` and read in full,
  including all six worked examples, the error-reporting algorithm, and the "Differences
  From CDCL and Answer Set Solving" section). This is prose/design documentation, not the
  Dart solver's source code — no `.dart` implementation files were opened.
- The `pubgrub-rs` guide's internals prose pages, at `https://pubgrub-rs-guide.pages.dev`:
  `internals/intro`, `internals/overview`, `internals/terms`, `internals/incompatibilities`,
  `internals/partial_solution`, `internals/conflict_resolution`, `internals/report_tree`,
  `version_solving`, and the guide's index page. These were fetched and read as rendered
  HTML/Markdown. One small illustrative Rust `enum` sketch appears in `internals/terms`
  (showing that a `Term` is either a positive or negative range); it is a pedagogical
  sketch embedded in the guide's own prose, not a reproduction of the `pubgrub` crate's
  actual source file, and no wording or structure from it is reused verbatim below — the
  term algebra in this document is derived from the prose description and re-derived
  independently using the identities the two sources both state.

**Sources deliberately NOT read**, per the clean-room constraint: the `pubgrub-rs/pubgrub`
crate source, the `astral-sh/pubgrub` fork, `contriboss/pubgrub-go`, any other Go/Rust/
Ruby/Python/JS PubGrub implementation, `uv`'s resolver source, and any `pubgrub_crate/*`
pages of the guide (which show the actual crate's public API/code samples rather than
algorithm prose) — those pages were not fetched. The Swift Package Manager PubGrub
implementation (the one permitted gray-area source) was **not read** for this document;
everything below comes only from the three prose sources listed above, cross-checked
against each other, plus original analysis (the worked example in §10 is an example the
author constructed and hand-traced, not copied from any source).

Where the two design documents disagreed or a point could not be pinned down from prose
alone, this is flagged explicitly in §11 rather than papered over.

---

## 1. Core Vocabulary

**Package.** An entity with a name, of which zero or one *version* may be selected in a
solution. Different packages are assumed independent except through explicit dependency
information (incompatibilities).

**Version / Range.** A *range* is a set of versions of one package — it can be empty, a
single version, an unbounded or bounded interval, or the set of every version. Ranges are
the substrate on which everything else is built; they must support intersection, union,
complement, subset/disjointness tests, and must have a canonical (unique) representation,
because term/incompatibility equality and the entailment tests below are defined in terms
of range containment, and two different in-memory representations of the same logical
range must compare as identical or every later algebraic identity silently breaks.

**Term.** The atomic unit of reasoning. A term is a range plus a polarity: either
*positive* (asserting membership) or *negative* (asserting non-membership, or absence of
the package). A term is a claim that can be evaluated to true or false once you know
whether/which version of that one package is selected:

- A **positive** term over range `r` is true exactly when a version of the package *is*
  selected and that version lies in `r`. It is false if no version is selected, or the
  selected version lies outside `r`.
- A **negative** term over range `r` is true when either no version of the package is
  selected at all, *or* a version is selected but it lies outside `r`. It is false exactly
  when a version is selected and it lies inside `r`.

This asymmetry — "no selection" makes every negative term true and every positive term
false — is the one fact about terms that is easy to get wrong and important to get right:
a negative term is not simply "the positive term over the complement range." `not foo
^1.0.0` and `foo <1.0.0 or >=2.0.0` (the positive term over the literal complement range)
agree on every case *except* "no version of foo selected at all," where the negative term
is true and the complement-range positive term is false (there is no version of foo
selected, so no version can be inside or outside any positive range — the positive-term
reading is simply undefined/false there, while the negative reading captures "not
required to be in that range," which includes "not present"). Negation as an operation
(see §2) just flips positive/negative on the same range — it does not require rewriting
the range as a complement.

**Positive vs. negative in practice.** Dependencies are represented as negative terms on
the depended-upon package ("does not have a version in the range the dependency requires"
being paired, in an incompatibility, with a positive term on the depending package). A
plain "this range is required" fact is a positive term. A plain "this range must never be
selected" fact (e.g., an unpublished, yanked, or SDK-incompatible range) is a negative term
standing alone.

**Incompatibility.** A set of terms — at most one term per package — that can never *all*
be true simultaneously in any valid solution. It is a statement of the form "not (T1 and
T2 and ... and Tn)"; equivalently, at least one of its terms must be false in any solution.
See §3.

**Assignment.** A single entry appended to the partial solution: either a *decision* or a
*derivation* (below). Every assignment carries a *decision level* — see below.

**Decision.** An assignment that pins one specific package to one specific version, chosen
speculatively by the decision-making step (§8) because it appeared consistent with
everything derived so far. Decisions are the "guesses" the algorithm might have to retract.

**Derivation.** An assignment that asserts a term (usually a range, not a single version)
must hold, because unit propagation (§6) proved it follows logically from an
incompatibility and the rest of the partial solution. Every derivation records its
*cause*: the incompatibility whose near-satisfaction forced it.

**Decision level.** A non-negative integer stamped on every assignment, equal to the
number of decisions at or before that point in the partial solution (not counting the
initial fact-of-existence for the root package as a "real" decision level increment — see
§11 for exactly where the numbering starts, which is one of the few points the primary
source is internally inconsistent about). Decision level is the unit of backtracking:
undoing a conflict always means discarding every assignment above some decision level, in
one atomic step, never a single assignment in isolation.

**Partial solution.** The chronological list of all assignments made so far — the
algorithm's running best guess at part of a total solution. It grows via unit propagation
and decision making, and shrinks (only from the end, down to some decision level) during
conflict resolution. It never grows and shrinks in the same step; backtracking is a
distinct, atomic act.

**Total solution.** A partial solution in which every package that has a positive
derivation also has a matching decision. At that point every dependency that was ever
forced by unit propagation has actually been fulfilled by a concrete version, and the
solve has succeeded: the set of decisions *is* the answer.

**Derivation graph.** The directed acyclic graph whose nodes are incompatibilities: each
*derived* incompatibility has an edge from each of the (exactly two) prior incompatibilities
combined to produce it (its "causes"); each *external* incompatibility is a leaf with no
incoming edges from this process (it came from a fact about packages, not from combining
two other incompatibilities). The graph rooted at the final failure incompatibility is a
formal proof that no solution exists, and is exactly what error reporting (§9) walks.

**Satisfier / previous satisfier.** Defined precisely in §7; intuitively, the satisfier is
"the single assignment that tips an incompatibility over from almost-true to fully-true,"
and the previous satisfier is "the assignment before that which would have been enough on
its own, had the tipping assignment's own package needed help from an earlier assignment
about the same package to fully cover the incompatibility's term."

---

## 2. Term Algebra

Let `Positive(r)` and `Negative(r)` denote terms over range `r`. Two derived special
ranges matter: `∅` (empty — no version) and `Any`/`Universe` (every version).

### 2.1 Negation

Negation just flips polarity, leaving the range untouched:

```
¬Positive(r) = Negative(r)
¬Negative(r) = Positive(r)
```

### 2.2 Intersection (logical AND, "both terms hold")

Because a positive term is a subset claim and a negative term is a superset-complement
claim, intersecting two terms about the *same* package reduces to range algebra:

```
Positive(r1) ∧ Positive(r2) = Positive(r1 ∩ r2)
Positive(r1) ∧ Negative(r2) = Positive(r1 \ r2)        -- (r1 minus r2)
Negative(r1) ∧ Negative(r2) = Negative(r1 ∪ r2)
```

The middle rule is the one worth internalizing: "must be in r1, and must not be in r2" is
just "must be in r1-but-not-r2." A positive term ANDed with anything stays positive (or
becomes the impossible term below) — you cannot AND your way from a positive constraint
back to a purely negative one.

### 2.3 Union (logical OR, "either term would hold")

Union is derived from intersection and negation via De Morgan, `T1 ∨ T2 = ¬(¬T1 ∧ ¬T2)`,
which expands to:

```
Positive(r1) ∨ Positive(r2) = Positive(r1 ∪ r2)
Positive(r1) ∨ Negative(r2) = Negative(r2 \ r1)
Negative(r1) ∨ Negative(r2) = Negative(r1 ∩ r2)
```

Union of terms is needed exactly once in the whole algorithm: building the generalized
resolution rule's combined term in conflict resolution (§7.3).

### 2.4 The two degenerate terms

- `Positive(∅)` is a term that is **always false** — it demands a version be selected
  inside a range that contains no versions. An incompatibility containing this term can
  never be satisfied (one term is permanently false, so the conjunction is permanently
  false), which makes that incompatibility permanently inert/useless — it will never fire
  and never needs to be checked again. This can arise legitimately, e.g. by intersecting
  two disjoint positive constraints on the same package during merging; recognizing it lets
  an implementation prune dead incompatibilities, though it is not required for
  correctness.
- `Negative(∅)` is a term that is **always true** — "no version selected, or selected
  outside the empty range" is unconditionally true. Because an incompatibility is a
  conjunction, any term that is always true contributes nothing and can be dropped from
  it without changing its meaning. This is how normalization (§3) discards trivial terms,
  and it is the dual of the previous case.

An incompatibility that has been reduced to **zero terms** (every term was
`Negative(∅)` and got dropped) is the ultimate degenerate case: a conjunction over zero
conjuncts is vacuously true, so this incompatibility is *always* satisfied. That is
precisely the unsolvable-root signal (§7, §9): version solving has proven that no
selection of any package can avoid violating it, because there was nothing left to
violate.

### 2.5 Satisfies / Contradicts / Inconclusive

These three relations are how the algorithm decides what a term or an incompatibility
"means" given everything decided/derived so far.

Given a set of terms `S` (e.g., all assignments in the partial solution that mention one
package, or — trivially — a single term `{v}`) and a term `t`:

- `S` **satisfies** `t` if `t` is forced true whenever every term in `S` is true.
- `S` **contradicts** `t` if `t` is forced false whenever every term in `S` is true.
- Otherwise `S` is **inconclusive** for `t` — both truth values of `t` remain possible.

Because each term denotes a set of versions (a positive term denotes its range directly; a
negative term denotes "outside the range, or absent" which for range-algebra purposes we
treat as the complement range, remembering the "absent" special case only matters when
literally no assignment about the package exists yet), these relations reduce to
containment/disjointness tests on the intersection of all of `S`'s denoted ranges:

```
S satisfies t     iff  ⋂(denotations in S) ⊆ denotation(t)
S contradicts t   iff  ⋂(denotations in S) is disjoint from denotation(t)
otherwise: inconclusive
```

Worked truth table for a single package, single incoming term `v` against a candidate `t`
(the "shorthand": treat `v` satisfying/contradicting `t` as `{v}` satisfying/contradicting
`t`):

| `v` (assignment) | `t` (term being tested) | Relation |
|---|---|---|
| `Positive([1,2))` | `Positive([1,3))` | satisfies (`[1,2) ⊆ [1,3)`) |
| `Positive([1,2))` | `Positive([1,1.5))` | inconclusive (overlaps, neither subset nor disjoint) |
| `Positive([2,3))` | `Positive([1,1.5))` | contradicts (disjoint) |
| `Positive([1,2))` | `Negative([2,3))` | satisfies (`[1,2)` disjoint from `[2,3)`, so "outside `[2,3)`" is guaranteed) |
| `Positive([1,1.2))` | `Negative([1,1.5))` | contradicts (`[1,1.2)` is entirely inside `[1,1.5)`, so the selected version is guaranteed **not** to be outside `[1,1.5)` — the negative term is forced false) |
| `Positive([1,2))` | `Negative([1,1.5))` | inconclusive (`[1,2)` straddles the boundary of `[1,1.5)` — partly inside, partly outside — so the negative term is neither forced true nor forced false) |
| `Negative([1,2))` | `Positive([1,2))` | contradicts (if nothing is selected in `[1,2)`, then "a version in `[1,2)` is selected" is forced false) |
| (nothing asserted about the package at all) | `Negative(r)` for any `r` | satisfies (absence alone makes every negative term true) |
| (nothing asserted about the package at all) | `Positive(r)` for any `r` | **inconclusive**, not a contradiction (a version could still be decided later that lands in `r`) |

The corrected row above matters: "no assignment yet" is inconclusive for a positive term,
not a contradiction — the package simply hasn't been decided. It is only a genuine
contradiction once some assignment pins the package outside `r` (a `Negative(r')` with
`r ⊆ r'`, or a `Positive(r')` disjoint from `r`).

**Applying this to a whole incompatibility (multiple packages).** An incompatibility is a
set of per-package terms. The partial solution "satisfies" the incompatibility if, for
*every* package the incompatibility mentions, the partial solution's assignments about
that package (intersected together) satisfy that package's term in the incompatibility.
The partial solution "contradicts" the incompatibility if there exists *at least one*
package in the incompatibility whose term is contradicted (one false conjunct already
makes the whole conjunction permanently false — the incompatibility can never fire from
here on, no matter what else is decided). If neither — most commonly, every package's term
is satisfied except exactly one, which is inconclusive — the incompatibility is
**almost satisfied**, and that one package's term is the **unsatisfied term**. Unit
propagation (§6) exists entirely to notice "almost satisfied" and act on it, and conflict
resolution (§7) exists to handle "fully satisfied" (all terms true — a conflict).

---

## 3. Incompatibilities

An incompatibility is *context-independent*: once built, its terms are mutually
incompatible regardless of what has been decided so far or will be decided later — it is a
timeless fact, not a snapshot. This is why the set of known incompatibilities only grows
and is never rolled back on backtracking — only the partial solution (built from
decisions/derivations, which *are* time-sensitive guesses) gets rolled back.

**Normalization.** At most one term may exist per package in an incompatibility; if two
terms about the same package would otherwise appear, they must be combined via
intersection (§2.2) into one term first. Any term equal to `Negative(∅)` (always-true) is
dropped, since it contributes nothing to the conjunction (§2.4).

**Where incompatibilities come from.**

1. **External incompatibilities** encode a fact about packages that is not derived by
   combining other incompatibilities:
   - A dependency "range `rA` of package A requires range `rB` of package B" becomes
     `{A: Positive(rA), B: Negative(rB)}` — read as "you may not select A in `rA` while
     simultaneously not selecting B anywhere in `rB`," i.e., selecting A in `rA` forces
     some version of B in `rB`.
   - "This range of a package is simply never installable" (an SDK/engine mismatch,
     a yanked/unpublished range, or the decision-making failure case in §8) becomes a
     single-term incompatibility, `{A: Positive(rA)}` — asserting that `rA` alone is
     forbidden.
   - The initial fact that seeds the whole search is "the (single, known) version of the
     root package must be selected," phrased as a two-part external incompatibility
     tying the root package's own version to whatever the root's manifest requires (or,
     more simply, as the negative fact `{root: Negative(rootVersion)}` standing for "it
     is forbidden for the root package to be anything other than its one real version" —
     sources differ slightly in exact phrasing here; both encode the same constraint).
   - Because adjacent versions of the same depending package frequently have *identical*
     dependency requirements, an implementation should collapse them into one
     incompatibility spanning the contiguous range of versions sharing that requirement,
     rather than emitting one incompatibility per version (see §8 for the exact
     lower/upper-bound convention this produces).
2. **Derived incompatibilities** are produced by conflict resolution (§7) combining two
   existing incompatibilities via the resolution rule. Every derived incompatibility
   records its two causes (forming the derivation graph, §1), and, once a package's
   dependencies are known not to matter for the rest of the search, lets later unit
   propagation skip straight to the general conclusion instead of rediscovering it version
   by version.

**Reading an incompatibility.** `{A: Positive(rA), B: Negative(rB)}` reads most naturally
as "A in `rA` depends on B in `rB`" precisely because the *only* way this incompatibility
is *not* violated is if either A is not actually in `rA`, or B *is* in `rB` — i.e., if you
do pick A from `rA`, you are forced to also satisfy B's requirement.

---

## 4. The Partial Solution and Termination

The state of the solver at any moment is exactly two things: the (append/truncate-only)
partial solution, and the (append-only) incompatibility set.

**Success.** The core loop terminates successfully the moment decision-making (§8) finds
no package with an outstanding positive derivation and no matching decision — i.e., the
partial solution is already a total solution. The decisions recorded so far, read off in
order, are the answer.

**Failure.** The core loop terminates with failure the moment conflict resolution (§7),
while trying to find a root cause, derives an incompatibility that is either empty (zero
terms) or a single positive term about the root package's own version. Both are the
formal statement "the root package (i.e., the entire problem) is unsatisfiable" — see
§2.4 and §7.4. Error reporting (§9) turns the derivation graph rooted at that
incompatibility into prose.

---

## 5. Main Loop

```
add external incompatibility: "the root package must be exactly its one real version"
next := root package's name
loop:
    result := unitPropagation(next)          -- see §6
    if result is Failure(rootCauseIncompat):
        return report(rootCauseIncompat)      -- see §9
    -- no more forced derivations reachable from `next` right now
    next, done := decisionMaking()            -- see §8
    if done:
        return the decisions in the partial solution as the total solution
```

Unit propagation and decision making strictly alternate: propagate everything derivable
from the most recent change, *then and only then* make one new speculative decision (which
in turn re-triggers propagation on that decision's own package next iteration). This
strict alternation is what keeps the search from wasting a decision on a package whose
range was about to collapse anyway from information already on hand.

---

## 6. Unit Propagation

**Purpose.** Given the current partial solution, mechanically squeeze out every
consequence forced by the known incompatibilities, one derivation at a time, until no
incompatibility is "almost satisfied" anymore (everything derivable has been derived) —
or until a conflict is hit.

**Indexing requirement.** Naively re-scanning every known incompatibility after every
single new assignment does not scale: most incompatibilities are irrelevant to whatever
package just changed. Incompatibilities must be indexed by the package names they
mention, and each propagation pass should only revisit incompatibilities that mention a
package whose assignments changed during *this* propagation session (starting from the one
package handed in). Because conflict resolution tends to synthesize more broadly-useful
(more general) incompatibilities over time, scanning newest-to-oldest surfaces the most
generally-applicable derivation first when several incompatibilities could fire at once.

**Algorithm.**

```
function unitPropagation(startPackage):
    changed := { startPackage }
    while changed is not empty:
        package := remove one element from changed
        for incompatibility in incompatibilitiesMentioning(package), newest first:
            relation := classify(incompatibility, partialSolution)   -- see §2.5
            if relation == FullySatisfied:
                rootCause, term := conflictResolution(incompatibility)   -- see §7
                if conflictResolution failed:
                    return Failure(rootCause)
                append derivation ¬term to partialSolution, cause = rootCause
                changed := { term.packageName }        -- restart scan from just this package
                break out of the for-loop over incompatibilities
            else if relation == AlmostSatisfied:
                term := incompatibility's unsatisfied term
                append derivation ¬term to partialSolution, cause = incompatibility
                add term.packageName to changed
            else:
                -- Contradicted or Inconclusive: nothing to do for this incompatibility
    return Success
```

Two things are easy to get backwards here and worth calling out explicitly:

1. **Conflict resolution is invoked from *inside* unit propagation**, not as a separate
   phase the main loop calls on its own. Unit propagation's normal job is finding
   almost-satisfied incompatibilities; hitting a *fully* satisfied one just means "the
   thing I'd derive next is a contradiction of what's already true," and the correct
   response is to fix the partial solution (via conflict resolution) *before* continuing to
   derive anything else — resuming propagation from whatever package the newly-returned
   root-cause incompatibility is still almost-satisfied against.
2. **A conflict here does not require a decision to have been made for the conflicting
   package.** A term like `Positive(AnyVersion)` (or, more subtly, any term broad enough
   to already be implied by an existing derivation) can become "satisfied" purely by
   virtue of a *derivation* proving some version of that package must eventually exist,
   with no concrete version chosen yet (recall from §2.5: `⋂S ⊆ t` can hold even when `S`
   is just a narrower positive derivation and `t` is a broader positive claim about the
   same package). This is exactly how a brand-new dependency incompatibility, added the
   instant decision-making considers a candidate version, can turn out to already be fully
   satisfied by *prior* state — without that candidate ever being committed as a decision
   (§8 covers the decision-making side of this same coin).

---

## 7. Conflict Resolution (Backjumping)

### 7.1 What it has to guarantee

Conflict resolution receives an incompatibility that the current partial solution fully
satisfies (a conflict — see §2.5/§6). It must produce a *replacement* incompatibility and
mutate the partial solution (by truncating it — backtracking) so that, immediately
afterward:

1. The replacement incompatibility is only **almost** satisfied (not fully) by the
   truncated partial solution, so unit propagation has something concrete and correct to
   derive right away.
2. Whatever gets derived next is *different* from what led to this conflict — otherwise
   the algorithm would immediately regenerate the exact same conflict and loop forever.

Guarantee (1) is where nearly all of the subtlety lives, because "almost satisfied"
depends on being able to point to a single decision-level boundary such that everything at
or below it is settled and only one relevant thing above it is still open. That is only
guaranteed at a **decision-level boundary**, because decisions (and everything derived from
them) are exactly the things that get discarded together as a unit; you cannot promise
"only one term left open" if the cut point falls in the *middle* of a decision level, since
you have no way to discard part of a level.

### 7.2 Satisfier and previous satisfier

Given the conflicting (fully satisfied) incompatibility `I`:

- The **satisfier** is the *earliest* assignment in the partial solution such that the
  prefix of the partial solution ending at (and including) that assignment already fully
  satisfies `I`. Call the term of `I` that mentions the same package as the satisfier
  `term`.
- Because a term can require a range that no *single* assignment establishes on its own
  (e.g., `term` needs `>=1.0.0 <2.0.0`, but the partial solution only reaches that via two
  separate derivations — one narrowing to `>=1.0.0`, a later one narrowing further to
  `<2.0.0`), the satisfier found above might only jointly complete `term` in combination
  with an earlier assignment about the *same* package. To find out whether that happened,
  compute the **previous satisfier**: the *earliest* assignment strictly before the
  satisfier such that (partial solution up to that assignment) **plus** the satisfier
  together fully satisfy `I`. If the satisfier alone was already enough, the previous
  satisfier is simply the assignment (of any package) immediately preceding the satisfier
  chronologically that made every *other* term of `I` true (i.e., made all terms except
  the satisfier's own term satisfied) — it need not be about the same package as the
  satisfier at all. If no such earlier assignment is needed (the satisfier is the very
  first assignment, or its own term needed no help), there is no previous satisfier.

### 7.3 The resolution step (computing a "prior cause")

The logical engine underneath all of this is propositional resolution: from `{a, b}` (as
clauses, meaning "not both can be true" in incompatibility-land — but think of it in
clause form "a or b" for the classical statement of the rule) and `{¬a, c}`, you may derive
`{b, c}`. Generalized to arbitrary terms rather than atomic booleans: given
incompatibilities `{t1, q...}` and `{t2, r...}`, you may always derive
`{t1 ∪ t2, q..., r...}` (union the two terms that disagree about the same package,
conjoin — i.e. keep — everything else), because in any world where `t1 ∪ t2` holds, at
least one of `t1` or `t2` must hold, which pulls in one of the two original
incompatibilities' remaining terms. When `¬t2 ⊆ t1` (t2's negation already implies t1 —
the case where `t2 = ¬t1` exactly is the classical resolution rule above), the union
collapses and you can write the simpler `{q..., r...}` with no combined term at all.

Concretely, to compute one **prior cause** from a conflicting incompatibility `I` and its
satisfier:

1. Let `cause` be the incompatibility that produced the satisfier as a derivation (if the
   satisfier is a decision, there is no cause to resolve against — see §7.4, this is a
   terminal case, not a step-3 case).
2. Take the union of `I`'s terms and `cause`'s terms, **dropping** the term about the
   satisfier's own package from both sides first.
3. If the satisfier's assignment did *not*, by itself, fully satisfy `term` (only did so
   jointly with the previous satisfier — see §7.2's caveat), you must add back a combined
   term for the satisfier's package: `¬(satisfier \ term)`, i.e., negate the range that is
   "what the satisfier established, minus what `term` needed" — this is precisely the
   generalized-union term `t1 ∪ t2` from the paragraph above, rewritten using the identity
   `(Sᶜ \ T)ᶜ = S ∪ T` so that it can be phrased directly from `satisfier` and `term`
   without materializing `cause`'s own version of the term.
4. Normalize the result (§3): merge any duplicate per-package terms, drop always-true
   terms. This is the new **prior cause**.

### 7.4 The loop, and why each branch is what it is

```
function conflictResolution(incompatibility):
    loop:
        if incompatibility has zero terms, or
           incompatibility has exactly one term and it is a positive term about
           the root package's own version:
            return Failure(incompatibility)     -- see §2.4, §4, §9

        satisfier, term := findSatisfier(incompatibility, partialSolution)   -- §7.2
        previousSatisfier := findPreviousSatisfier(incompatibility, satisfier, partialSolution)
        previousSatisfierLevel := decisionLevel(previousSatisfier) if it exists,
                                   else the base level (see §11 for the exact floor value —
                                   the primary source is inconsistent about this number)

        if satisfier is a decision, OR previousSatisfierLevel != decisionLevel(satisfier):
            -- Safe to stop: everything above previousSatisfierLevel can be discarded as
            -- one unit, and incompatibility is guaranteed to be only almost-satisfied
            -- once that's done (see §7.1's guarantee 1).
            if incompatibility is not the original input incompatibility:
                add incompatibility to the known incompatibility set
            truncate partialSolution to remove every assignment whose decision level is
              greater than previousSatisfierLevel
            return Success(incompatibility, term)   -- unit propagation resumes on `term`'s package

        -- Otherwise: satisfier and previousSatisfier share a decision level, so we cannot
        -- yet promise "almost satisfied" after any backtrack at a level boundary. Fold the
        -- satisfier's own cause into the conflict and try again, one level's worth of
        -- reasoning further back.
        incompatibility := priorCause(incompatibility, satisfier)   -- §7.3
```

**Why the terminal check comes first.** An incompatibility with no terms, or a lone
positive term about the root package, cannot be traced back any further — there is no
package left whose "satisfier" could ever be found (the root's version is a fixed fact,
not a derivation with a cause chain to unwind), so this is the actual, final proof that no
solution exists, not merely a fact to fold in and continue.

**Why "satisfier is a decision" is enough on its own.** If the satisfier is a decision,
every assignment strictly before it (at lower decision levels, by construction — a
decision's own level is one more than everything preceding it) is settled and unrelated to
this specific guess. Truncating just above `previousSatisfierLevel` throws away this
decision (since its level is by definition `> previousSatisfierLevel` whenever the two
differ, and even when a previous-satisfier doesn't exist the base level is below the
decision's own level) together with everything that depended on it, and is guaranteed to
land exactly on "only the satisfier's package's term is still open," because nothing
below the cut point can complete `I` on its own (that's what "earliest assignment" in the
satisfier's own definition already established) and nothing between `previousSatisfier`
and the cut still contributes another package's term (`previousSatisfier`, by definition,
is where every *other* term became true).

**Why differing decision levels between satisfier and previous satisfier are also
enough.** If they differ, then the previous satisfier's level is strictly below the
satisfier's level (previous satisfier is chronologically earlier, hence same-or-lower
level; "differ" then forces strictly lower). Truncating to `previousSatisfierLevel`
removes the satisfier itself (higher level) and everything after it, while
`previousSatisfier`'s own contribution survives — again landing on exactly one open term.

**Why matching levels force another resolution round instead.** If satisfier and
previousSatisfier are at the *same* decision level, there is no level boundary you can cut
at that separates "satisfier's contribution" from "everything else needed" — both live in
the same batch that would be discarded or kept together. Cutting there either keeps both
(no progress — `I` is still fully satisfied, guarantee 1 fails) or discards both
(over-shooting — you lose information you didn't need to lose, and cannot even point to
`I` being almost-satisfied afterward because the satisfier's own contribution is now gone
too, undermining the very shape of `I`). So instead, the satisfier's *cause* is folded into
`I` via resolution (§7.3), producing a logically-equivalent-or-stronger incompatibility one
step closer to a decision, and the whole satisfier/previousSatisfier search is redone
against this new incompatibility. This is guaranteed to make progress (terminate) because
each round either reaches a terminal failure (§2.4) or strictly reduces how many
assignments in the partial solution are "still relevant" to the conflict (the loop is
walking the derivation graph backward one derivation at a time, and the graph is acyclic
and finite).

**Why guarantee (2) — "different derivation next time" — falls out for free.** The
returned incompatibility is only ever added to the known set if it differs from the
original input (deduplication), and by construction it is a strictly more general
statement than the raw conflict was (it dropped the culprit package's specific term in
favor of either nothing or a combined/widened term) — so the very next unit-propagation
pass, now missing the decision(s) that produced the old more-specific chain, is
structurally incapable of re-deriving the exact same sequence of assignments that led back
here.

### 7.5 Backjumping, restated

"Backjumping" is simply what falls out of doing the truncation exactly once, at
`previousSatisfierLevel`, instead of the naive CDCL-adjacent approach of undoing one
decision at a time and retrying: because the loop above keeps folding in causes until it
finds a decision-level gap wide enough to guarantee the "almost satisfied" property, it
can — and empirically often does — jump back multiple decision levels in one step,
skipping over intermediate decisions that had nothing to do with the actual root cause.

---

## 8. Decision Making

**Purpose.** Once unit propagation has squeezed out everything derivable, pick one
concrete package+version to commit to, moving the search forward. This is the only place
new information (a guess) enters the system; everything else is deduction.

**Eligibility.** A package/version choice is legal only if:

- The package currently has at least one positive derivation in the partial solution (it
  is actually *wanted* by something) but no decision yet (nobody has committed to it).
- The chosen version lies within the intersection of every term the partial solution has
  accumulated about that package so far.

**Which package, which version.** Any package meeting the above is a legal candidate; the
prose sources describe a heuristic, not a mandatory rule: prefer the package with the
*fewest* remaining candidate versions (to surface an eventual conflict, if one exists, as
early as possible — a narrow candidate set exhausts itself in fewer wasted iterations than
a wide one), and within that package prefer its *latest* matching version (most likely to
satisfy downstream consumers, and to already reflect the newest available fixes). Both
sources are explicit that this is a tunable heuristic, not part of the algorithm's
correctness — any legal choice preserves correctness, only performance/error-quality
differs. See §11 for what is left genuinely open here.

**Turning dependencies into incompatibilities, lazily.** Before (tentatively) committing to
a version, first materialize that version's dependencies as new external incompatibilities
(§3) and add them to the known set — but only for the version actually being considered,
never speculatively for the whole package universe up front. This laziness is
load-bearing, not an optimization detail: eagerly instantiating every version's every
dependency would mean reasoning about ranges you will never actually need, and — worse —
committing to specific concrete versions before you even know if the corresponding
constraints will remain relevant. Adjacent versions sharing byte-identical dependency
requirements should be represented as a single incompatibility whose depending-package
range spans all of them, using this convention for the bounds: the lower bound is the
first version (in ascending order) that has the requirement (omitted/unbounded if it's the
very first published version), and the upper bound is the first version *after* that run
that *doesn't* have the requirement (omitted/unbounded if the run reaches the last
published version). This both keeps the incompatibility count roughly proportional to the
number of *distinct* dependency statements rather than the number of versions, and happens
to mirror how humans write version constraints in manifests in the first place (a
contiguous range, not an enumerated version list).

**The pre-commit conflict check.** After adding the new dependency incompatibilities but
*before* actually recording the decision, check whether committing to this exact version
would make any of those brand-new incompatibilities fully satisfied given the rest of the
current partial solution. If so, do **not** record the decision — leave the package
without a decision, and simply hand control back to unit propagation (which will now find
that same incompatibility either already fully satisfied by prior state alone (§6's second
callout — this happens whenever the depending package's own already-derived range already
covers the incompatibility's term for it, which is common, since that range is exactly why
this version was even a candidate) and immediately trigger conflict resolution, or at worst
almost satisfied, yielding a clean derivation that rules out this and adjacent versions
without ever having falsely claimed them as decided. This check is what prevents the
search from ever recording a decision that is already known, at commit time, to be wrong.

**Version unavailability.** If eligibility holds for a package (something wants it) but no
published version satisfies the accumulated term at all — including the case where a
specific candidate version's metadata simply cannot be fetched/resolved, which is modeled
identically to "this version does not exist" — do not add its (nonexistent) dependencies.
Instead add a single external incompatibility asserting that the *entire currently-required
range* is forbidden (a lone positive term over that range), and hand control back to unit
propagation without recording any decision. Unit propagation will find this new
incompatibility already almost-satisfied (nothing else could contradict a term that is, by
construction, exactly what the accumulated derivations already required) and immediately
derive its negation, which is exactly the information needed to make the *next* attempt at
this package (if any wider possibility remains) or to eventually surface this as the root
cause of failure if no possibility remains at all.

**Termination signal.** If no package meets the eligibility criteria at all — nothing has
an outstanding, undecided, positive derivation — decision making reports "no more
decisions needed"; per §4 this is success.

---

## 9. Error Reporting

**Trigger.** Conflict resolution's terminal failure case (§7.4/§4) always produces an
incompatibility that is either empty or a single positive term about the root package's
exact version. That incompatibility is the root of a derivation graph (§1) — a proof, in
the formal sense, that the root package (and hence the whole request) is unsatisfiable.
Error reporting's entire job is turning that proof into readable prose.

**Traversal.** Walk the derivation graph depth-first starting from the root (failure)
incompatibility, toward its leaves (external incompatibilities, which are always facts
about actual package dependencies/availability, never derived reasoning). Because each
derived incompatibility has exactly two causes, this is a walk over a DAG whose internal
nodes have in-degree possibly greater than one (the same incompatibility can be a cause of
more than one later derivation — see §1's derivation graph note) — so a purely linear,
print-as-you-go traversal is only sufficient when the graph is in fact a simple chain.

**Line numbering for non-linear graphs.** Before generating any text, do a pass over the
graph counting, for every derived incompatibility, how many *other* incompatibilities cite
it as a cause (its out-degree in the "is a cause of" direction). Only incompatibilities
whose count is two or more will ever need to be referenced from more than one place in the
eventual prose, so only those are candidates for being assigned a visible line number —
everything else can simply be re-explained inline wherever it's needed, since it's only
needed once.

**Per-incompatibility rendering logic.** For a derived incompatibility, look at the shape
of its two causes:

- **Both causes are themselves derived.** If both already have an assigned line number,
  the two prior conclusions can simply be cited by number and combined into one new
  sentence. If only one has a number, recursively render the other cause first (giving it
  a fresh line number only if something later will need to reference it), then cite it
  and state the new conclusion. If *neither* has a number yet, prefer treating whichever
  cause is itself built from two *external* incompatibilities as the "simple" one — it can
  be described compactly in a single clause inline — and recursively render the more
  complex cause first (assigning it a number, since we're about to interrupt the flow to
  handle the simple one afterward), keeping the explanation readable rather than
  presenting two multi-step derivations back-to-back with no anchor between them. If
  neither cause is "simple" in that sense, render the first cause fully (numbering its
  concluding line), insert a visual break, render the second cause fully, and then state
  the combined conclusion citing the first cause's number.
- **Exactly one cause is derived, the other external.** If the derived cause already has a
  line number, cite it directly. Otherwise, if that derived cause itself has a simple
  shape (one external + one derived-with-no-number-needed cause), it can usually be
  collapsed into the same sentence as the current conclusion, skipping an intermediate
  "obvious" restatement — this is the general form of the "skip every other derived
  incompatibility without losing clarity" compression that keeps chains from becoming
  needlessly verbose. Otherwise, recursively render the derived cause, then state the new
  conclusion referencing it.
- **Both causes are external.** This is the base case: state both facts and their
  immediate joint conclusion in one sentence, no recursion needed.
- Finally, **after** writing this incompatibility's line, if two or more *other*
  incompatibilities cite this one as a cause (per the pre-pass count), assign the
  just-written line a number now, so later references can point back to it.

**Framing conventions (presentation, not logic).** Both design sources are explicit that
the exact English phrasing is a matter of taste, not correctness — reasonable choices
include: describing "the root package" without a version number (it only ever has one);
saying "every version of X" rather than naming an unbounded/all-encompassing range
explicitly; preferring a concluding "So, ..." over "And because ..." for the very last
line, to signal it's the payoff rather than another intermediate step; and compressing
"root depends on A and root depends on B" into "root depends on both A and B" where it
reads more naturally. None of this affects what the algorithm *computes* — only how the
already-fully-determined derivation graph gets turned into sentences.

---

## 10. Worked Example

This is an original example, constructed and hand-traced from the rules above (not copied
from either source) specifically to exercise: ordinary unit propagation, a
conflict discovered without ever committing a decision to the conflicting package (the
subtlety flagged in §6/§8), exactly one round of "same decision level, fold in the cause
and try again," and a backjump all the way past an intermediate decision once a real
decision level boundary is found.

**Universe.**

- `app` (the root) — one version, `1.0.0` — depends on `http >=1.0.0`.
- `http` — versions `1.0.0` (no dependencies) and `2.0.0` (depends on `json ^1.0.0`, i.e.
  `[1.0.0, 2.0.0)`).
- `json` — versions `1.0.0` (depends on `http >=2.5.0`, a range no published `http` version
  will ever satisfy) and `2.0.0` (no dependencies).

| # | Assignment / Incompatibility | Kind | Cause | Level |
|---|---|---|---|---|
| 1 | `app 1.0.0` | decision | — | 0 |
| 2 | `I1 = {app: [1.0.0,1.0.0], http: ¬[1.0.0,∞)}` | incompatibility (external: app's dependency) | — | — |
| 3 | `http: [1.0.0,∞)` (call it `D1`) | derivation | `I1` | 0 |
| 4 | `I2 = {http: [2.0.0,∞), json: ¬[1.0.0,2.0.0)}` | incompatibility (external: http 2.0.0's dependency) | — | — |
| 5 | `http 2.0.0` | decision | — | 1 |
| 6 | `json: [1.0.0,2.0.0)` (call it `D2`) | derivation | `I2` | 1 |
| 7 | `I3 = {json: (-∞,2.0.0), http: ¬[2.5.0,∞)}` | incompatibility (external: json 1.0.0's dependency) | — | — |

Step 3: `I1` has its `app`-term satisfied exactly by decision 1; its `http`-term is
inconclusive (nothing about `http` yet) — almost satisfied — so its negation, `http:
[1.0.0,∞)`, is derived.

Step 4–5: decision making picks `http`, the only outstanding package; its accumulated term
is `[1.0.0,∞)`; the highest matching version is `2.0.0`. Its dependency becomes `I2` (lower
bound `2.0.0` since that's the first version with the dependency and it's also the last
published version, so no upper bound). Committing `http 2.0.0` would not yet make `I2` fully
satisfied (its `json` term is still inconclusive — no `json` assignment exists), so the
decision is safe and is recorded.

Step 6: `I2`'s `http`-term is now satisfied by decision 5; its `json`-term is inconclusive
— almost satisfied — derive `json: [1.0.0,2.0.0)`.

Step 7: decision making now considers `json`. Its accumulated term, `[1.0.0,2.0.0)`,
matches only version `1.0.0` (version `2.0.0` is outside that range). `json 1.0.0`'s own
dependency becomes `I3`: since `json 1.0.0` is the first published version with this
dependency (so the lower bound is omitted, per §8's convention) and the very next version
(`2.0.0`) does *not* have it (so the upper bound is included), the depending-package range
for `json`'s side of `I3` is the **positive** term `(-∞,2.0.0)`, i.e. "json < 2.0.0" — this
must be positive, not negative, because it is the *depender* side of the dependency
(§3: a dependency is `{depender: Positive(range), dependency: Negative(range)}`).

**The pre-commit check (§8) fires here, without ever recording a decision for `json`.**
Would committing `json 1.0.0` make `I3` fully satisfied?
- `I3`'s `json`-term, positive "`<2.0.0`," is *already* satisfied by `D2` alone
  (`[1.0.0,2.0.0) ⊆ (-∞, 2.0.0)`) — no decision needed.
- `I3`'s `http`-term, "http < 2.5.0 (i.e. not >= 2.5.0)," is *already* satisfied by the
  existing decision `http 2.0.0` (2.0.0 is outside `[2.5.0,∞)`).

`I3` is therefore **already fully satisfied by the existing partial solution**, before
`json 1.0.0` is ever committed. Decision making declines to record it, returns control, and
the very next unit-propagation pass rediscovers `I3` as fully satisfied — a conflict.

**Conflict resolution on `I3 = {json: (-∞,2.0.0), http: ¬[2.5.0,∞)}`:**

- Satisfier: earliest assignment completing `I3`. `D2` (step 6, level 1) completes the
  `json`-term on its own; the `http`-term was already complete earlier, at decision 5
  (level 1). The later of the two, chronologically, is `D2` — so **satisfier = `D2`**,
  `term = (-∞,2.0.0)` (the `json` side, positive).
- Previous satisfier: the earliest assignment before `D2` such that (prefix + `D2`)
  satisfies `I3` — that's exactly decision 5 (`http 2.0.0`), which supplied the other term.
  **previousSatisfier = `http 2.0.0` decision, level 1.**
- `satisfier` (`D2`) is a *derivation*, not a decision, and `previousSatisfierLevel (1) ==
  satisfier's level (1)` — same level, so neither escape condition holds. Must fold in a
  prior cause.
- Prior cause: union `I3`'s terms with `I2`'s terms (`I2` is `D2`'s cause), dropping the
  `json`-term from both sides (that's the satisfier's own package):
  `I3` minus its `json`-term leaves `http: ¬[2.5.0,∞)`. `I2` minus its `json`-term leaves
  `http: [2.0.0,∞)`. Since `D2` (the satisfier) fully satisfied `term` by itself — no joint
  satisfaction was needed — no extra combined term is required (§7.3 step 3 is skipped).
  Normalizing the two `http` terms via intersection (§2.2): `Positive([2.0.0,∞)) ∧
  Negative([2.5.0,∞)) = Positive([2.0.0,2.5.0))`. New incompatibility:
  **`I4 = {http: [2.0.0,2.5.0)}`.**

- Loop again with `I4`. Not a terminal failure (it's about `http`, not the root). Its
  satisfier: the single `http`-term is completed exactly by decision 5 (`http 2.0.0`,
  since `2.0.0 ∈ [2.0.0,2.5.0)`) — **satisfier = `http 2.0.0` decision.** No other
  assignment about `http` contributes, so there is no previous satisfier.
- `satisfier` **is a decision** → this alone is enough to stop. (Per §11, the exact "floor"
  value used for `previousSatisfierLevel` when there is no previous satisfier is one of the
  two things the primary source is internally inconsistent about; this trace uses `0`,
  matching every one of that source's own worked-example tables.) Truncate the partial
  solution to remove everything above level `0` — this discards decision 5 (`http 2.0.0`)
  and derivation `D2` (`json`) entirely. Add `I4` to the known incompatibility set. Return
  `(I4, term = [2.0.0,2.5.0) about http)`.

| # | Assignment / Incompatibility | Kind | Cause | Level |
|---|---|---|---|---|
| 8 | `I4 = {http: [2.0.0,2.5.0)}` | incompatibility (derived; conflict resolution) | `I3`, `I2` | — |
| 9 | `http: ¬[2.0.0,2.5.0)` (call it `D3`) | derivation | `I4` | 0 |
| 10 | `http 1.0.0` | decision | — | 1 |

Step 9: back in unit propagation, `I4` (now with the conflicting decision gone) has its
lone `http`-term inconclusive — trivially "almost satisfied" (zero of one terms satisfied,
one inconclusive) — so its negation, `http: ¬[2.0.0,2.5.0)`, is derived.

Step 10: decision making reconsiders `http`. Its accumulated term is now
`[1.0.0,∞) ∧ ¬[2.0.0,2.5.0) = [1.0.0,2.0.0) ∪ [2.5.0,∞)` (§2.2). Only `http 1.0.0` matches
(`http 2.0.0` is now excluded); it has no dependencies, so no new incompatibility is added
and no conflict check is needed. Decision recorded: `http 1.0.0`, level 1.

No package still has an outstanding positive derivation without a decision (`json` was
never actually required by anything still standing — its only derivation, `D2`, was
discarded in the backtrack, and `I2`'s `json`-term is now permanently contradicted by
`http 1.0.0` rather than pursued further). Decision making reports done.

**Final solution:** `app 1.0.0`, `http 1.0.0` — `json` never needed to be considered again,
even though it took one full round of conflict resolution to prove that, and the proof
generalized cleanly to "no version of `http` in `[2.0.0, 2.5.0)` will ever work," which is
strictly more useful than "specifically `http 2.0.0` doesn't work" would have been.

---

## 11. Ambiguities and Open Questions

These are the places prose alone did not settle the question, or where the two primary
sources actually disagreed (or, in one case, one source disagreed with **itself**). Flagged
here deliberately rather than silently resolved, since these are exactly the spots a Go
implementation needs the most care and the most test coverage.

1. **The decision-level "floor" used when no previous satisfier exists is internally
   inconsistent in the Dart document itself.** Its conflict-resolution prose says: *"Let
   previousSatisfierLevel be previousSatisfier's decision level, or decision level 1 if
   there is no previousSatisfier,"* and adds a note that *"decision level 1 is the level
   where the root package was selected."* But every one of that same document's six
   worked-example tables places the root package's own decision at **level 0**, and its
   "Performing Conflict Resolution" example explicitly narrates backtracking "all the way
   to level 0" in a situation with no previous satisfier — which is only consistent with a
   floor of 0, not 1. The `pubgrub-rs` guide's conflict-resolution page does not give a
   concrete numeric floor at all (it stays at the conceptual level and, notably, ends by
   posing this exact question rhetorically and unanswered — *"where do we cut? ... Would
   that not be guaranteed if we picked another decision level?"* — rather than asserting a
   settled answer). This specification resolves it as **0**, because that is the only
   value consistent with every concrete example rather than the one inconsistent prose
   sentence, and used 0 throughout §10's trace — but this is this document's judgment call,
   not a fact both sources agree on, and it deserves a dedicated property/unit test in the
   Go implementation (verify no under- or over-backtracking at the very first decision).
2. **The exact decision-making tie-breaking heuristic is explicitly left open by the
   source itself.** "Fewest matching versions, then highest version" is stated as *"a
   heuristic... there's likely room for improvement,"* not a specification. Neither source
   defines what to do on an exact tie (two packages with the same count of matching
   versions), nor whether "fewest matching versions" should account for versions already
   known-bad from prior incompatibilities versus merely "fewest ever published." An
   implementation is free to choose here, but different reasonable choices will produce
   different (still correct) solver traces and different sets of packages surfaced first
   in an error — this affects UX quality, not correctness, but is worth pinning down
   deliberately rather than by accident of map/slice iteration order.
3. **The precise general form of the "joint satisfaction" correction in conflict
   resolution (§7.3 step 3) is given by the Dart source only via one worked identity and a
   single illustrative note**, not a fully worked proof covering every shape of range
   overlap (e.g., what if the satisfier's contribution and `term` are themselves disjoint
   in an unexpected way, or one is degenerate per §2.4). The algebra checks out on the
   cases both sources actually walk through, and on the case worked in §10, but an
   implementation should back this specific step with property-based tests (e.g., fuzz
   pairs of ranges and confirm the derived prior cause is logically implied by its two
   parents) rather than trusting a single hand-traced example to have covered every edge.
4. **Whether the initial root-package fact is best modeled as one incompatibility or two**
   is phrased slightly differently between sources (a bare negative fact "root must be
   exactly its one version" versus an incompatibility tying a root decision to the root's
   own dependencies) — both are logically equivalent ways of seeding the same constraint,
   but neither source gives a single canonical incompatibility shape for it, so this is an
   implementation detail left to the adapter layer, not something prose pins down exactly.
5. **"Unavailable version metadata" and "SDK/engine-incompatible range" are described as
   producing the same shape of incompatibility (a lone forbidden positive term), but
   neither source is fully explicit about whether they should be tagged/distinguishable in
   the incompatibility's own external-fact metadata** for the purposes of later, better
   error messages (e.g., "this version's metadata could not be fetched" reads very
   differently to a user than "this version doesn't support your platform," even though
   both behave identically to the solver's core loop). This is a place where the
   *algorithm* is unambiguous but *what to carry alongside it for reporting* is not
   specified by prose, and is left to the adapter.
6. **The exact English wording used in error reporting is explicitly a "suggestion rather
   than a prescription"** per the Dart source itself — this is not really an ambiguity in
   the algorithm (the *logical structure* of what gets said and in what order is fully
   pinned down by §9's traversal/line-numbering rules) but is worth flagging so a Go
   implementation doesn't over-fit its user-facing message templates to either source's
   exact phrasing as if it were part of the spec.
7. **Whether the "avoid the same conflict" guarantee (§7.1's guarantee 2) is ever formally
   proven versus asserted by both sources via example** — both sources demonstrate it
   working across several worked examples and give an intuitive argument (the returned
   incompatibility is strictly more general and the offending decision is gone), but
   neither source offers a proof that this always terminates in a bounded number of
   rounds for arbitrary dependency graphs (only that the *inner* per-conflict loop
   terminates, per §7.4). In practice this is exactly what clause-learning search
   procedures rely on in general, but it is asserted rather than derived from first
   principles in either source, and is worth keeping in mind if the Go implementation ever
   needs to reason about worst-case behavior (e.g., guarding against pathological inputs
   with a max-iteration safety valve in production, separate from correctness).
8. **THIS DOCUMENT CONTRADICTS ITSELF about what an unassigned package entails, and §2.5
   is the half that is wrong.** Added 2026-08-04 after an independent correctness review
   of the solver found it; it is a defect in this specification, not in the code.

   §2.5's truth table states that with nothing asserted about a package, a *negative* term
   is **satisfied**, on the reasoning that absence makes every negative term true. But §10's
   worked trace contradicts that at steps 3, 4-5 and 6, all of which treat an unassigned
   package as **inconclusive** for a negative term — and §6's unit propagation collapses
   entirely without the inconclusive reading, because a dependency incompatibility is
   `{depender: Positive, dependee: Negative}`, so a satisfied-by-absence negative term makes
   every dependency classify as *fully* satisfied the moment the depender is selected. Every
   dependency would be reported as a conflict and nothing would ever resolve.

   **The resolution is that §2.5's row describes a term's truth in a COMPLETED world, while
   the partial solution's relation asks what the assignments so far ENTAIL.** Those are
   different questions. An unassigned package is undecided, not known-absent: a version could
   still be decided later, which would make a negative term false. So the partial solution
   entails nothing about it, for either polarity.

   This was found the hard way — the implementation followed §2.5 literally first, broke every
   dependency derivation, and failed six propagation tests before being corrected. §2.5's
   table should be read as being about worlds, and a sentence saying so belongs in §2.5
   itself. Until that is written, prefer §10's behavior wherever the two disagree.
