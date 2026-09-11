---
version: "v0.101.0"
description: Review a test plan against its PRD (project)
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
**REQUIRED — routed command, two-phase task creation:**
1. **Phase 1 — Preamble task only:** `TaskCreate` one preamble task.
2. **Phase 2 — Bulk after routing:** After preamble confirms path, bulk-create remaining.
3. **Redirect or early exit:** Mark preamble done, prune the task list per Closing Notification and Cleanup part (2), stop.
4. **Extensions:** Active `USER-EXTENSION` block → Phase 2 task
5. Mark `in_progress` → `completed`
6. **Post-Compaction:** Re-read, resume first incomplete.
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
**Step 3a: Branch Auto-Assignment (#2657)** — trigger: issue carries a `test-plan` or `prd` label **and** has no branch assignment; `/create-prd` creates both unassigned, so until close the tracker does not know they exist and `/done --all` cannot discover an in-review test plan. **Reported, not prompted** (unlike `/create-prd` Step 3a): reviewing an issue is already working it on this branch, so no decision remains. Delegate, do not re-derive: `node .claude/scripts/shared/assign-branch.js "$ISSUE"`. **Ordering is load-bearing** — assign **here, before Step 4 finalize runs**, because `review-finalize.js` does its own read-modify-write to increment `**Reviews:** N`, so a write concurrent with or after it races that update, later write wins, loser vanishes with no error. **No open tracker for the current branch → report and continue** that the issue remains unassigned; **never create a branch from a review** — `/assign-branch` owns `gh pmu branch start`. **Already assigned → leave it**: an issue assigned to a different branch is **not moved**; report the existing assignment.
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
**Only if "Ready for approval":**
```bash
node .claude/scripts/shared/review-ac-checkoff.js --issue $ISSUE --findings .tmp-$ISSUE-findings.json --move-status in_review
```
Script reads `type` from findings JSON. With `type: "test-plan"` it returns `skipped: true` and checks off **nothing** — the template's Approval Checklist is a fixed 6-item gate, not the review criteria (#2594). **The `--move-status in_review` transition still happens**; only check-off is suppressed.
Report the skip, not a count: `"Approval gate: check-off skipped (#$ISSUE is a test plan; its Y approval gates are checked by the approver). Issue #$ISSUE moved to in_review. Run /done #$ISSUE to close the approval gate."` NEVER report `X/Y checked off` here — X is always 0.
**Otherwise:** skip — no AC check-off, no status transition.

### Step 5a: Approval Gate Confirmation (Conditional)
**Only when recommendation is "Ready for approval"**, after Step 4 finalize — never before it: `review-finalize.js` rewrites the issue body immediately beforehand, so a pre-finalize snapshot silently reverts it.
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
