---
version: "v0.105.0"
description: Review a test plan against its PRD, using the IDPF framework.
argument-hint: "#issue [--mode ...] [--force]"
copyright: "Rubrical Works (c) 2026"
---
<!-- EXTENSIBLE -->
# /review-test-plan
Review a TDD test plan against its source PRD for coverage completeness. Delegates setup to `review-preamble.js`, cleanup to `review-finalize.js`. Self-contained: document updates, issue finalization, AC check-off.
---
## Prerequisites
- `gh pmu` installed
- `.gh-pmu.json` configured
- Issue body has `**Test Plan:**` and `**PRD:**`
---
## Arguments
| Argument | Description |
|----------|-------------|
| `#issue` | Issue linked to test plan (e.g., `#42`) |
| `--mode` | Transient: `solo`, `team`, `enterprise` |
| `--force` | Force re-review even if `reviewed` label |
---
## Execution
**REQUIRED — routed command, two-phase task creation:** Phase 1 creates one preamble task; Phase 2 bulk-creates the rest once the preamble confirms the path, **emitted in one message as parallel tool calls** — not one call per message (`07-task-creation-timing.md` § Emission). Redirect or early exit → mark preamble done, prune the task list per Closing Notification and Cleanup part (2), stop.
An active `USER-EXTENSION` block becomes a Phase 2 task. Track `in_progress` → `completed`. Post-compaction: re-read, resume from the first incomplete task.
---
## Workflow
### Step 1: Setup (Preamble)
```bash
node ./.claude/scripts/shared/review-preamble.js $ISSUE --no-redirect [--mode mode] [--force]
```
Parse JSON. `ok: false` → `errors[0].message` → **STOP**. `earlyExit: true` → report review count → **STOP**.
Extract: `context` (issue data, reviewNumber, `**Test Plan:**`/`**PRD:**` paths), `criteria`, `warnings`.
Read both files. Either missing → **STOP**.

<!-- USER-EXTENSION-START: pre-review -->
<!-- USER-EXTENSION-END: pre-review -->

### Step 1a: Source PRD Review Gate (#2786)
**After Step 1's early-exit/closed checks, before Step 2 evaluates any criterion.** A test plan is derived from its PRD, so a verdict taken against an unreviewed PRD can be invalidated by that PRD's own review — and nothing re-triggers the test-plan review when it is.
**1 — Resolve the tracker.** The approval issue cites the PRD *file*, not the tracker, so the link runs the wrong way and is resolved by helper, not regex: `resolvePrdTracker({approvalIssue,body})` from `.claude/scripts/shared/lib/prd-tracker-lookup.js`. Three strategies in order — the explicit `**PRD Tracker:** #N` marker, a `prd`-labelled issue referencing this approval issue, then the PRD file path; `strategy` names which answered, and a multi-candidate `path` match warns `ambiguous-path-match`.
**2 — Evaluate via the Phase 1c helper, never a second copy:** `review-state.js --issue $PRD`, then `evaluateReviewGate({body,reviewState})` from `.claude/scripts/shared/lib/create-backlog-review-gate.js`. `/create-backlog` Phase 1c gates on this same condition one step **later** (#2694); reusing its decision is what stops the two gates disagreeing about what "reviewed" means. **The filename is historical** — the helper takes a body and a state and knows nothing about backlogs, as `branch-review-gate.js` is named for its first consumer.
| Outcome | Action |
|---|---|
| `gate: false` | Proceed to Step 2 silently |
| `gate: true` | `AskUserQuestion`: run `/review-prd #$PRD` first (**recommended**), or continue against the unreviewed PRD |
| `indeterminate` | **Proceed**, saying the pass was indeterminate — per #2577, blocking would let a `gh` outage stop every test-plan review |
| No tracker | **Report and proceed.** Extract Mode PRDs create none, and refusing a review because the PRD was never given a tracker punishes the wrong thing |
| Search failed | Report the error and proceed — "could not look" is not "there is no tracker" |
**Continuing is permitted and recorded.** The plan is still useful feedback on an unsettled PRD, but the review comment states the PRD was unreviewed at review time so the verdict cannot later be mistaken for one taken against a settled PRD — what happened on `rubrical-worker/px-manager#1157`, reviewed four minutes after its PRD's review landed with findings, every coverage figure describing a version superseded within the hour.
**`--force` does not bypass this gate.** It overrides the `reviewed` label on the **test plan**; it says nothing about the state of the *PRD*, a different issue with its own history. Treating it as a bypass would widen a flag about one issue into permission to ignore another's. The nested `/resolve-review` re-review evaluates this gate too.

### Step 2: Evaluate Criteria
**2a: Auto-Evaluate Objective**
Re-read `.claude/metadata/test-plan-review-criteria.json` from disk. Use `autoCheckMethod` per criterion to evaluate test plan + PRD. Emit ✅/⚠️/❌ with evidence. Use `shouldEvaluate(criterionId, ...)` from `review-mode.js` to filter by reviewMode.
**Coverage Analysis (P0):** Execute `coverageAnalysis.procedure` from criteria file. Map PRD acceptance criteria → test cases. Report coverage as structured findings.
**Graceful:** If `test-plan-review-criteria.json` is not found or malformed, warn and fall back to standard criteria only, handling missing fields per-criterion (skip criterion if invalid). Inline defaults: AC coverage, Test framework specified, Test levels, Story-to-test mapping, Error scenarios, Boundary conditions, Failure modes, Integration points, Component interactions, Data flow, E2E critical journeys, E2E happy/error paths, E2E→PRD mapping, Framework consistency, Coverage targets, Test coverage proportionate. Empty/missing criteria → inline defaults. Skip criteria missing `autoCheckMethod`. Non-blocking.

**2b: Ask Subjective**
Load subjective from criteria file. Use `AskUserQuestion` with each `question`, `header`, `options`. Partial valid. **Solo:** skip.
**Coverage gaps reported as bullet-point concerns** (not tables) — for `/resolve-review` parser compatibility.

**2c: Recommendation**
- **Ready for approval** — All ACs covered, no blockers
- **Ready with minor gaps** — Small gaps
- **Needs revision** — Significant gaps
- **Needs major rework** — Fundamental issues
### Step 3: Update Test Plan File
**`**Reviews:** N`:** increment or add `**Reviews:** 1`.
**Review Log:** append row to `## Review Log` table. Missing section → append at end.
```markdown
| # | Date | Reviewer | Findings Summary |
|---|------|----------|------------------|
| N | YYYY-MM-DD | Claude | [Brief one-line summary] |
```
**Never edit or delete existing rows.**
### Step 3a: Branch Auto-Assignment (#2657)
Trigger: issue carries a `test-plan` or `prd` label **and** has no branch assignment; `/create-prd` creates both unassigned, so until close the tracker does not know they exist and `/done --all` cannot discover an in-review test plan. **Reported, not prompted** (unlike `/create-prd` Step 3a): reviewing an issue is already working it on this branch, so no decision remains. Delegate, do not re-derive: `node .claude/scripts/shared/assign-branch.js "$ISSUE"`. **Ordering is load-bearing** — assign **here, before Step 4 finalize runs**, because `review-finalize.js` does its own read-modify-write to increment `**Reviews:** N`, so a write concurrent with or after it races that update, later write wins, loser vanishes with no error. **No open tracker for the current branch → report and continue** that the issue remains unassigned; **never create a branch from a review** — `/assign-branch` owns `gh pmu branch start`. **Already assigned → leave it**: an issue assigned to a different branch is **not moved**; report the existing assignment.
### Step 4: Finalize (Self-Contained)
Write findings to `.tmp-$ISSUE-findings.json`, run:
```bash
node ./.claude/scripts/shared/review-finalize.js $ISSUE -F .tmp-$ISSUE-findings.json
```
Finalize: body metadata (`**Reviews:** N` increment), structured comment, labels (`reviewed`/`pending`). Clean up temp file. **Read** `.claude/scripts/shared/lib/findings-schema.json` for contract structure, required fields, status values, recommendation values.
**`type` MUST be `"test-plan"`** — not `"story"`, `"generic"`, omitted (#2594). Drives two behaviours:
- **Header verb.** `review-finalize.js` derives `## Test Plan Review #N`. Any other value emits `## Issue Review #N`, which `/resolve-review` cannot reconcile with a test plan — reports `NO_REVIEW` against a review that exists.
- **AC check-off suppression.** `test-plan` is tracker-shaped, so Step 5 leaves the template's fixed 6-item Approval Checklist alone. The `--move-status in_review` transition still happens.
### Step 5: Approval Gate AC Check-Off (Conditional)
**Only if "Ready for approval" or "Ready with minor gaps":**
```bash
node .claude/scripts/shared/review-ac-checkoff.js --issue $ISSUE --findings .tmp-$ISSUE-findings.json --move-status in_review
```
Script reads `type` from findings JSON. With `type: "test-plan"` it returns `skipped: true` and checks off **nothing** — the template's Approval Checklist is a fixed 6-item gate, not the review criteria (#2594). **The `--move-status in_review` transition still happens**; only check-off is suppressed.
Report the skip, not a count: `"Approval gate: check-off skipped (#$ISSUE is a test plan; its Y approval gates are checked by the approver). Issue #$ISSUE moved to in_review. Run /done #$ISSUE to close the approval gate."` NEVER report `X/Y checked off` here — X is always 0.
**If "Needs revision":** skip — no AC check-off, no status transition.
**Step 5 shares Step 5a's trigger, decided rather than inherited (#2785).** One decision: Step 5a presents the plan for approval, Step 5 performs the `in_review` transition handing it over. Widening one alone yields a "Ready with minor gaps" review that confirms gates on an issue it never moved, or moves an issue whose gates were never rolled up — the two conditional steps silently disagreeing. Approval is still on the table under "Ready with minor gaps" (Step 2c: small coverage gaps), so both run.

### Step 5a: Approval Gate Confirmation (Conditional)
**Only when recommendation is "Ready for approval" or "Ready with minor gaps"**, after Step 4 finalize — never before it: `review-finalize.js` rewrites the issue body immediately beforehand, so a pre-finalize snapshot silently reverts it.
**"Needs revision" is the only recommendation that skips this phase** — a plan with significant coverage gaps has nothing to confirm. **Why the trigger admits both, and must not be narrowed back (#2785).** The risk prompt requires a **blocked** gate; a gate is blocked only when a backing criterion is not `pass`; a non-passing criterion excludes "Ready for approval" by Step 2c. Under the old trigger the two could never hold together — when the phase ran no gate could be blocked, and when a gate could be blocked the phase did not run. The risk branch was dead, and "Ready with minor gaps" got no rollup at all, no confirmation for the gates that *did* pass, and a report saying the gates are "checked by the approver". Observed on `rubrical-worker/px-manager#1157`: thirteen gate-backing criteria passed, two non-gate criteria warned, all five auto-checkable gates would have been `checkable`, and all six boxes were left unchecked.
**Re-read invariant: every body write re-reads its surface immediately beforehand** — not once per phase, but immediately before each write, on each surface. Issue body and plan document are re-read separately, each directly before its own write.
**1 — Compute the rollup.** A gate is `checkable` only when **every** backing criterion evaluated `pass`; `warn`, `fail` and `skip` all block — `skip` included, since a skipped criterion produced no evidence and treating absence of information as a pass approves what nobody looked at.
**2 — Confirmation prompt.** For `checkable` gates, confirm via `AskUserQuestion` before writing anything. **Skip this prompt entirely when no gate is auto-checkable.**
**3 — Risk prompt.** For each `blocked` gate present its risk record — blocking criterion and evidence, reproduced verbatim from `risk.criteria`, never re-read from the findings — and ask whether the user accepts the risk. **Skip this prompt entirely when no gate is blocked.** An `unresolvable` gate is **not** a risk prompt: never evaluated, so there is no risk to weigh — report it, leave it unchecked.
**4 — Write both surfaces, on acceptance only:** confirmed gates in **both** the issue's `## Review Checklist` and the plan document's `## Approval Checklist`. Gates resolve **by declared text, never by index** — a gate whose text matches no line is reported unresolvable and left unchecked rather than resolved by position, because checking the wrong box is strictly worse than checking none and a fixed checklist has no positional correspondence with a review's findings (#2594).
**Declining leaves both surfaces unwritten** and the issue otherwise unchanged: no checkbox written, no `## Approval Decisions` section, no label changes, no status move beyond Step 5's.
**5 — Record the decisions.** On acceptance append `## Approval Decisions` to the **approval issue body** — every gate, its computed status, backing criteria verdicts, and the user's response **verbatim** — then `rm` the temp files. **In the body, not only a comment**: `/resolve-review` and `/done` parse the body, and a comment is not state they read. Verbatim because paraphrasing a risk acceptance is how an accepted risk becomes an approved gate.
```bash
node -e "const g=require('./.claude/metadata/test-plan-approval-gates.json');const f=require('./.tmp-$ISSUE-findings.json');const {computeApprovalGates}=require('./.claude/scripts/shared/test-plan-approval-gates.js');console.log(JSON.stringify(computeApprovalGates(g,f),null,1))" > .tmp-$ISSUE-gates.json
node -e "const {applyApprovalGateCheckOff}=require('./.claude/scripts/shared/review-ac-checkoff.js');const g=require('./.claude/metadata/test-plan-approval-gates.json');const r=require('./.tmp-$ISSUE-gates.json');console.log(JSON.stringify(applyApprovalGateCheckOff({issue:$ISSUE,planPath:'$PLAN_PATH',gateMap:g,rollup:r})))"
node -e "const {renderApprovalDecisions}=require('./.claude/scripts/shared/review-ac-checkoff.js');const g=require('./.claude/metadata/test-plan-approval-gates.json');const r=require('./.tmp-$ISSUE-gates.json');console.log(renderApprovalDecisions(g,r,RESPONSES))" >> .tmp-$ISSUE-decisions.md
```
**The approval issue is NEVER auto-closed by this phase.** Closure remains with `/done`. Step 5's `--move-status in_review` transition is preserved exactly.
<!-- USER-EXTENSION-START: post-review -->
<!-- USER-EXTENSION-END: post-review -->

### Step 6: Closing Notification and Cleanup
Two parts in order; the prune is **part of** this step, not a trailing step a reader can stop before. **(1)** Output `closingNotification` from finalize output. **(2) Prune the task list** (unconditional — every path, including redirect and early-exit paths where Phase 1 created a preamble task and Phase 2 never ran): `TaskList` to enumerate, then `TaskUpdate status=deleted` for every task owned by this `/review-test-plan` invocation (Phase 1 preamble, Phase 2 step tasks, `USER-EXTENSION` tasks). Do **not** delete tasks created outside this invocation (user TODOs).
---
## Error Handling
| Situation | Response |
|-----------|----------|
| Preamble `ok: false` | `errors[0].message` → STOP |
| Test plan missing | Path error → STOP |
| PRD missing | Path error → STOP |
| Issue closed | Ask user (from preamble) |
| Write fails | Report error → STOP |
---
**End of /review-test-plan Command**
