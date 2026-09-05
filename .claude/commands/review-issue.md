---
version: "v0.100.2"
description: Review issues with type-specific criteria (project)
argument-hint: "#issue [#issue...] [--with ...] [--mode ...] [--force] [--prior-art]"
copyright: "Rubrical Works (c) 2026"
---

<!-- EXTENSIBLE -->
# /review-issue
Reviews GitHub issues with type-specific criteria. Delegates setup to `review-preamble.js`, cleanup to `review-finalize.js`.
**Extension Points:** `.claude/metadata/extension-points.json` or `/extensions list --command review-issue`

## Prerequisites
`gh pmu` installed; `.gh-pmu.json` configured.

## Arguments
| Argument | Required | Description |
|----------|----------|-------------|
| `#issue` | Yes | Issue numbers (`#42` or `42 43 44`) |
| `--with` | No | Domain extensions (`security,performance` or `all`) |
| `--mode` | No | Review mode override: `solo`/`team`/`enterprise` |
| `--force` | No | Force re-review even if `reviewed` label present |
| `--prior-art` | No | Force the 2a-iv sweep whatever the `reviewSweep` mode (except `off`) and whatever the issue type. Boolean, takes no value |

Multi: `/review-issue #42 #43 #44` reviews each sequentially.

## Execution Instructions
**REQUIRED:** Routed command — two-phase task creation:
1. **Phase 1:** Single `TaskCreate` for preamble step only.
2. **Phase 2:** After preamble confirms path (no redirect, no early exit), bulk-create tasks for all remaining steps + one per non-empty `USER-EXTENSION` block.
3. **On redirect/early exit:** Mark preamble completed, prune the task list per Step 4 part (2), then stop; do NOT create remaining tasks.
4. **Track Progress:** mark each task `in_progress` → `completed` as you work it.
5. **Post-Compaction:** Re-read spec; resume from first incomplete task — no re-routing.

## Workflow
Multi-issue: process each sequentially through Steps 1–3.

### Step 1: Setup (Preamble Script)
```bash
node ./.claude/scripts/shared/review-preamble.js $ISSUE [--with extensions] [--mode mode] [--force]
```
Parse JSON. Branches:
- `ok: false` → report `errors[0].message` → **STOP** (skip to next in batch)
- `context.redirect` set → invoke skill **dynamically** using `context.redirect` (strip leading `/`) — all three rows below are equally normative (#2428):

  Labels `proposal`/`prd`/`test-plan` → `Skill("review-proposal"|"review-prd"|"review-test-plan", args: "#$ISSUE [--with ...] [--mode ...] [--force] [--prior-art]")`, name derived from `context.redirect`. → **STOP**. **`--prior-art` must survive the redirect (#2725):** `/review-proposal` carries its own 2a-iv, so a dropped flag there is a typed sweep request answered with silence, the discard rule `02-github-workflow.md` forbids. `/review-prd` and `/review-test-plan` carry no prior-art criterion and report it inapplicable rather than swallowing it.
- `context.issue.state === "closed"` → ask user to confirm before proceeding
- `earlyExit: true` (has `reviewed` label, no `--force`) → report review count → **STOP**

Extract: `context` (type, reviewNumber, title, labels, body), `criteria` (common from `.claude/metadata/review-mode-criteria.json`, typeSpecific from `.claude/metadata/review-criteria.json`), `extensions`, `warnings`.

Extension loading handled by preamble via `.claude/metadata/review-extensions.json`. Unknown IDs warn; missing/malformed → standard review only.

**Step 1c: Peer Announcement — review started (advisory, #2695).** After the preamble confirms the path (no `context.redirect`, no `earlyExit`) and **before** the first criterion. At Step 1 this would announce a review that a redirect or an early exit immediately invalidates — a peer told about a review that never happened is actionable and wrong (same rule as `/work` event 1). Run `node .claude/scripts/shared/peers-check.js`, then `buildAnnouncement({event: EVENTS.REVIEW_STARTED, issues: [$ISSUE], peers})` from `.claude/scripts/shared/peer-announce.js`.
**Take `data.peers`**, never the whole envelope nor the module top-level `peers` — that mistake yields `undefined`, and an `|| []` beside it becomes a genuine empty array indistinguishable from an empty working directory (#2678; #2686 open on a further divergence). **Once per issue at its own turn.** `/review-issue 42 43 44` runs the preamble per issue and any one may redirect or early-exit, so never compose one message naming the whole argument list. **Suppressed entirely under `--force`.** `/resolve-review` Step 4 calls `Skill("review-issue", "#$ISSUE --force")`, so without this one run emits `review-resolved` then `review-started` seconds apart, the second describing a nested re-review. The cycle is already announced.
**Gated by project config (#2702).** Resolve before composing: `node .claude/scripts/shared/lib/cross-session-config.js`. `groups.review` false → this event **emits nothing**, no `SendMessage` and no skip notice; the review itself unchanged. `notices` false → dispatch unchanged, the caveat and skip-reason lines not printed. Absent object, or any omitted key → enabled. **Read the resolver; never re-derive the default inline** — a second copy here is how this command and `/resolve-review` drift, and they are the two halves of one group. In the multi-issue form resolve the state **once**, not per issue: it is a project setting, and re-reading per issue invites two answers in one run. `shouldSend` true → `SendMessage` per `recipients` entry with `text`; false → report `notice` once, continue. **Advisory, fire-and-forget — nothing awaits delivery and a throwing helper must never abort the enclosing sequence.** Dispatch is not delivery (#2674).

<!-- USER-EXTENSION-START: pre-review -->
<!-- USER-EXTENSION-END: pre-review -->

### Step 2: Evaluate Criteria

<!-- USER-EXTENSION-START: criteria-customize -->
<!-- USER-EXTENSION-END: criteria-customize -->

**2a: Auto-Evaluate Objective Criteria** — for each objective criterion in `criteria.common` and `criteria.typeSpecific`, evaluate by reading issue content. Re-read `.claude/metadata/review-criteria.json` from disk (not memory) if stale. Emit ✅/⚠️/❌ with evidence using `autoCheck` field for guidance.

**2a-ii: Proposed Solution Repair + Files-to-Modify Derivation** (Bug, Enhancement, Story; NOT epic)
**(a) Repair — conditional.** Trigger: `proposed-solution` or `proposed-fix-described` is ❌/⚠️. Placeholder = under 20 chars or matches "TBD"/"To be documented"/"..."/empty. When triggered: analyze codebase, generate **Approach**, **Files to modify**, **Implementation steps**, **Testing considerations**. Present as `#### Proposed Solution (Auto-Generated)` (enhancement/story) or `#### Proposed Fix (Auto-Generated)` (bug). Otherwise content already substantive (>20 chars, no placeholder).
**(b) Derive and persist `Files to modify:` — always (#2520).** Runs for every bug/enhancement/story **regardless of whether (a) fired**. The old trigger tied the list to *repairing* a bad Proposed Solution, so a well-authored issue never got one — careful authoring guaranteed a later Step 4c halt.
- **Deficient — (a) fired:** reuse the **Files to modify** list (a) produced. Do not derive it twice.
- **Substantive — (a) skipped:** **Extract** the list from the existing Proposed Solution + codebase analysis. Do **not** regenerate a Proposed Solution; (a)'s generation path is deficient-case only.
Write into the **issue body** — review output alone leaves `scope-drift-check.js` unable to see it. **Format contract** (`extractFilesToModify`, no parser change): header exactly `**Files to modify:**` or `**Files:**` on its own line, then one backticked path per `- ` bullet; terminates at the first blank line, any `**bold:**` line, or any `##` heading. No blank line inside the list, and **not a table** — markdown puts a blank line before a table, terminating the section and yielding empty declared scope.
**Refresh, don't append.** On re-review replace the existing section in place. Reviewing an unchanged issue twice must produce no body diff.
**Ordering is load-bearing** — same rule as 2a-iv, same reason. Write **here, before Step 3 finalize runs**. `review-finalize.js` does its own read-modify-write to increment `**Reviews:** N`; a write concurrent with or after it races that update, later write wins, loser vanishes with no error. Never write during or after Step 3, and do not "simplify" by folding this write into `review-finalize.js`. **Payoff:** a declared path is exempt from the always-protected halt (`checkDrift`'s `isProtected && !inDeclared` guard), so an issue declaring what it touches clears Step 4c without a `Scope-Override`; since #2520 a declaration no longer forces blocking mode, files added during implementation warn as growth.

**2a-iii: Epic-Specific Evaluation** — for epic type: `sub-issue-review` requires recursive review of sub-issues through 2a–2b including 2a-ii with per-sub-issue body updates. `construction-context` scans `Construction/Design-Decisions/` and `Construction/Tech-Debt/` for files referencing sub-issue numbers. None found → report gracefully.

**2a-iv: Prior-Art Sweep When Marker Absent (#2517)**
Trigger: `prior-art-checked` criterion evaluates ❌ (marker absent, or `PARTIAL` — an incomplete sweep, treated as absent and re-swept), **OR `--prior-art` was passed** (#2725) — the automatic path below, the explicit one after it.
Delegate the decision; do not re-derive it:
```bash
node -e "console.log(JSON.stringify(require('./.claude/scripts/shared/lib/prior-art-marker.js').decideSweep({body:BODY,createdAt:CREATED_AT,reviewSweep:REVIEW_SWEEP})))"
```
**Arguments — substitute from the named source; never leave a literal placeholder.** `BODY` ← `context.issue.body`; `CREATED_AT` ← **`context.issue.createdAt`** (ISO 8601, added #2539); `REVIEW_SWEEP` ← returned `mode` from **materializing an absent `reviewSweep` first (#2564)**: `node -e "console.log(JSON.stringify(require('./.claude/scripts/shared/lib/framework-config.js').ensureReviewSweep(process.cwd())))"` → `{written, mode}` — second writer alongside Praxis Hub Manager (install/upgrade) so pre-existing projects converge without reinstall; writes through the validating writer, never raw `fs`, and leaves a legacy boolean alone since migration is read-time.
**`createdAt` decides whether the criterion means anything.** `isExemptFromSweep` treats an absent/unparseable value as exempt — the safe direction, but it makes `decideSweep` return `not-applicable` for *every* unmarked issue. Before #2539 the preamble emitted no such field, so that is exactly what happened. Never substitute a hand-entered date. On a `CREATED_AT_UNAVAILABLE` preamble warning, say so in the criterion rather than reporting a bare `not-applicable`: the timestamp could not be established, so the sweep was not skipped on the merits. Returns `{sweep, status, reason}`. Report the criterion with that `status`; act on `sweep`.
| status | Action |
|---|---|
| `pass` | Complete marker present — no sweep, no write |
| `fail` | Marker absent or `PARTIAL` — run the sweep |
| `recommend` ⚠️ | mode `recommend` (default), no complete marker — **no sweep, no write**. Report ⚠️ with the `formatSweepAdvisory()` text naming the runnable command. **Not** ❌: `--prior-art` is opt-in and rarely passed, so most issues carry no marker and ❌ would downgrade nearly every review for a sweep never meant to run automatically. Does not affect the recommendation |
| `skip` ⊘ | mode `flag-only` or `off` — no sweep, no write. Report ⊘, **not** ❌, which would downgrade every review in a project that opted out |
| `not-applicable` | Predates the feature (pinned cutoff) — no sweep; absence is meaningless for what could not be swept |
**Explicit `--prior-art` — this decides, not the table above (#2725).** `node -e "console.log(JSON.stringify(require('./.claude/scripts/shared/lib/prior-art-marker.js').decideFlagSweep({reviewSweep:REVIEW_SWEEP})))"` → `{sweep, refused, mode, message}`. `full`/`recommend`/`flag-only`/absent → **sweep**. `off` → no sweep, no write; report ⊘ with `message` **verbatim**, then **continue the review** — never halt. A refusal the user cannot see is the silent no-op the flag exists to remove.
**Three interactions, decided here rather than left to the implementer**, all following from `decideFlagSweep` taking **only** `reviewSweep`: it never receives the body or the timestamp, so no automatic short-circuit fires on the explicit path. That is intended, not an oversight to patch.
| Interaction | Decision |
|---|---|
| **Marker** | **Forces a re-sweep** even when `classifyMarker(body)` is already `complete`. A stale marker is the main reason a human asks for a sweep; refusing because one exists makes the flag useless exactly when it is wanted |
| **Cutoff** | **Overrides** `isExemptFromSweep(createdAt)`. That exemption stops *automatic* sweeping stamping markers into pre-feature issues; an explicit request is not automatic, and the date is not what the user is asking about |
| **Criterion trigger** | **Sweeps regardless of issue type.** `prior-art-checked` is defined in `review-criteria.json` for `enhancement` only, so `bug`, `story`, `epic` and `generic` have no ❌ to fire on. A flag effective on one type of five is the silent-drop failure on a second axis |
On a type with no `prior-art-checked` criterion do **not** invent one: sweep, write the section, and report findings as a standalone `**Prior Art:**` observation. `review-criteria.json` fixes the criterion list; this step does not extend it.
**Sweeping:** run the #2514 procedure, reading `.claude/metadata/prior-art-sweep.json` for surfaces, excludes, term derivation, dispositions and body formats — none restated here.
**Output:** findings in the review; write `**Prior Art:**` into the body via `insertPriorArtSection` from the same helper.
**Ordering is load-bearing.** Write **here, before Step 3 finalize**, so `review-finalize.js`'s read-modify-write for `**Reviews:** N` reads a body already containing the section. A write concurrent with or after finalize races it — both read-modify-write the same body, later wins, loser vanishes with no error. Never write during or after Step 3.
**Recommendation:** prior art duplicating the issue's scope is blocking — `Needs revision` or stronger, not a passing note.
**Missing config:** `prior-art-sweep.json` missing/unreadable → report criterion, warn, skip sweep. Do not fail the review.

**2a-v: Branch Auto-Assignment for Test-Plan and PRD Issues (#2657)** — trigger: issue carries a `test-plan` or `prd` label **and** has no branch assignment; `/create-prd` creates both unassigned, so until close the tracker does not know they exist and `/done --all` cannot discover an in-review test plan. **Reported, not prompted** (unlike `/create-prd` Step 3a): reviewing an issue is already working it on this branch, so no decision remains. Delegate, do not re-derive: `node .claude/scripts/shared/assign-branch.js "$ISSUE"`. **Ordering is load-bearing** — same rule as 2a-ii and 2a-iv, same reason: assign **here, before Step 3 finalize runs**, because `review-finalize.js` does its own read-modify-write to increment `**Reviews:** N`, so a write concurrent with or after it races that update, later write wins, loser vanishes with no error. **No open tracker for the current branch → report and continue** that the issue remains unassigned; **never create a branch from a review** — `/assign-branch` owns `gh pmu branch start`. **Already assigned → leave it**: an issue assigned to a different branch is **not moved**; report the existing assignment.
**2b: Ask Subjective Criteria** — for subjective criteria applicable to current reviewMode, use `AskUserQuestion`. Re-read `.claude/metadata/review-mode-criteria.json` from disk for question/options. Solo mode: skip entirely.

**2c: Extension Criteria** (if `--with`) — evaluate domain criteria loaded by preamble.
**2c-ii: Security Finding Label** — if `--with security`/`--with all` and any security finding ⚠️/❌:
```bash
gh issue edit $ISSUE --add-label=security-finding
```
All ✅ → no label.

**2d: Recommendation** — one of: `Ready for work` (no blocking concerns) / `Needs minor revision` (small) / `Needs revision` (must address before starting) / `Needs major rework` (fundamental).

### Step 3: Finalize (Script)
Write findings JSON to `.tmp-$ISSUE-findings.json`. **Read** `.claude/scripts/shared/lib/findings-schema.json` for contract structure, required fields, status values, recommendation values. Solo mode: `userEvaluated` always `[]`.
**`type`:** issue type from preamble `context` — `bug`, `enhancement`, `story`, `epic`, or `generic`. Issue-shaped types produce a `## Issue Review #N` header and keep positional AC check-off (on an issue the checkbox list **is** the reviewed AC list). NEVER write `prd`, `proposal`, or `test-plan` here (#2594).

```bash
node ./.claude/scripts/shared/review-finalize.js $ISSUE -F .tmp-$ISSUE-findings.json
```
Finalize handles: body metadata (`**Reviews:** N` increment), structured comment posting, label assignment (`reviewed`/`pending`), epic sub-issue label propagation. Clean up temp file. Report summary from output.

For non-`--with` runs, append: `Tip: Use --with security,performance to add domain-specific review criteria. Available: security, accessibility, performance, chaos, contract, qa, seo, privacy (or --with all)`
**Extensions Applied** in review comment lists only domains producing findings (omit empty). At least one domain section must appear when `--with` used; if none produce findings, fall back to standard review with warning.
**Step 3b: Peer Announcement — review passed (terminal, #2722).** Sub-step of Step 3, firing per issue right after that issue's finalize returns — not `Step 3a`, which runs once after every issue. Compose as Step 1c, with `event: EVENTS.REVIEW_PASSED` and `peers` taken from `data.peers`.
**Trigger: the finalize envelope reports `labelAssigned === 'reviewed'`.** Nothing else. `determineLabel()` returns exactly one of `pending` | `reviewed` and the swap replaces rather than adds, so that value *is* the specified success condition — `reviewed` applied, `pending` removed or never applied — with no second check needed. **The label, never the recommendation string.** `determineLabel()` tests `startsWith('Ready')`, which also admits `Ready with minor revisions`; this command's enum (2d) has exactly one `Ready*` value, so the label is an exact proxy here **and only here**. A recommendation-keyed trigger reads as equivalent and is wrong the moment it is copied — which is why `/review-prd`, `/review-proposal` and `/review-test-plan` deliberately do **not** emit this: same gap, wider enum, needing a stricter predicate rather than a copied trigger.
**`pending` emits nothing, and nor does a failed swap.** `pending` is genuinely open-ended — `/resolve-review`, abandonment, or nothing may follow — the case the two non-terminal review events were written for, so it has no terminal counterpart by design. `labelAssigned: null` means the swap failed (#2694); emitting there would announce an outcome that did not happen. **Terminal, and the text says so** — nothing follows, so a peer can stop waiting; a `terminal` flag no reader of the message ever sees is not a closer.
**NOT suppressed under `--force` — the opposite of Step 1c, deliberately.** There, suppression stops `/resolve-review` Step 4's nested `Skill("review-issue", "#$ISSUE --force")` announcing a re-review as fresh. Here it fires on **both entry paths**, because that nested re-review is exactly how a resolve cycle reaches `Ready for work`; suppressing it would silence the event on the path that most needs it. **No double-emission:** `/resolve-review` never emits this itself, and there is exactly one finalize call per issue per review — so once per issue at its own turn, never twice and never batched across the argument list.
**Gated by `groups.review`, resolved once per invocation** — reuse Step 1c's resolution rather than re-reading `cross-session-config.js`, so one run cannot gate its opener and its closer differently; that read happens even when `--force` suppresses Step 1c's event, because `--force` suppresses an event, not a read. Group false → emits nothing, no `SendMessage` and no skip notice. `shouldSend` true → `SendMessage` per `recipients` entry with `text`; false → report `notice` once, continue. **Advisory, fire-and-forget — a throwing helper must never abort Step 3.** Dispatch is not delivery (#2674).

<!-- USER-EXTENSION-START: post-review -->
<!-- USER-EXTENSION-END: post-review -->

### Step 3a: Interdependence Analysis (Multi-Issue Only)
Trigger: 2+ issues reviewed AND all eligible per `typeFilter` in `.claude/metadata/review-interdependence.json` — read the eligible/excluded sets from that file; excluded wins. Do not restate them here; a prose copy is data nothing keeps in sync (#2683).
After all individual reviews complete:
```javascript
const { analyzeInterdependence, isEligibleForInterdependence } = require('.claude/scripts/shared/review-interdependence.js');
// `labels` accepts both shapes: ['story'] or [{name:'story'}] (#2682)
const allEligible = reviewedIssues.every(i => isEligibleForInterdependence(i.labels));
if (allEligible) {
  const result = analyzeInterdependence(reviewedIssues);
}
```
`reviewedIssues` = array of `{ number, title, type, labels, body }` collected during reviews. `labels` is normally `context.issue.labels` from that issue's preamble envelope, passed through unaltered — `{name}` objects, **not** strings; do not flatten. Input it cannot read warns rather than returning a bare `false` (#2682).
Report: **Overlap** (shared scope), **Ordering** (suggested order + rationale), **Conflicts** (contradictory requirements), **Shared Criteria** (ACs in multiple issues).
If findings exist, report them with the suggested order and ask whether to update the issues with cross-references (y/n); on acceptance add `Refs #N` notes to related issue bodies. No findings → `"No interdependence detected between reviewed issues."` and continue.

Configuration: dimensions and `typeFilter` in `.claude/metadata/review-interdependence.json` (config-driven; add to `eligible`/`excluded` to customize). Single-issue: skipped.

### Step 4: Closing Notification and Cleanup
Two parts in order; the prune is **part of** this step, not a trailing step a reader can stop before. **(1)** Output `closingNotification` from finalize. Multi-issue: `"Reviews complete: #42, #43, #44"`. **(2) Prune the task list** (unconditional — every path, including redirect and early-exit paths where Phase 1 created a preamble task and Phase 2 never ran): `TaskList` to enumerate, then `TaskUpdate status=deleted` for every task owned by this `/review-issue` invocation (Phase 1 preamble, Phase 2 step tasks, `USER-EXTENSION` tasks). Do **not** delete tasks created outside this invocation (user TODOs). Nested via `Skill("review-issue")` from `/resolve-review`: the prune still runs; the parent's sweep becomes redundancy.

## Error Handling
| Situation | Response |
|-----------|----------|
| Preamble `ok: false` | Report `errors[0].message` → STOP |
| Issue not found | Preamble error → STOP |
| Issue closed | Ask user (from preamble context) |
| Unknown label | Preamble uses generic criteria |
| Finalize fails | Report error; body may already be updated |

**End of /review-issue Command**
