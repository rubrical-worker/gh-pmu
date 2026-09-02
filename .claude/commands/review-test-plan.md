---
version: "v0.99.0"
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
