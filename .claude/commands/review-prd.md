---
version: "v0.98.0"
description: Review a PRD with tracked history (project)
argument-hint: "#issue [--with ...] [--mode ...] [--force]"
copyright: "Rubrical Works (c) 2026"
---
<!-- EXTENSIBLE -->
# /review-prd
Review a PRD document linked from a GitHub issue. Delegates setup to `review-preamble.js`, cleanup to `review-finalize.js`. Self-contained: handles document updates, issue finalization, AC check-off directly.
**Extension Points:** `.claude/metadata/extension-points.json` or `/extensions list --command review-prd`
---
## Prerequisites
- `gh pmu` installed
- `.gh-pmu.json` configured
- Issue body references PRD file path
---
## Arguments
| Argument | Description |
|----------|-------------|
| `#issue` | Issue linked to PRD (e.g., `#42`) |
| `--with` | Comma-sep domain extensions or `all` |
| `--mode` | Transient mode: `solo`, `team`, `enterprise` |
| `--force` | Force re-review even if `reviewed` label present |
---
## Execution Instructions
**REQUIRED — routed command, two-phase task creation:**
1. **Phase 1 — Preamble task only:** `TaskCreate` single preamble task.
2. **Phase 2 — Bulk after routing:** After preamble confirms path, bulk-create remaining.
3. **Redirect or early exit:** Mark preamble done, prune the task list per Closing Notification and Cleanup part (2), stop.
4. **Extensions:** Active `USER-EXTENSION` block → Phase 2 task
5. Mark `in_progress` → `completed`
6. **Post-Compaction:** Re-read, resume first incomplete.
---
## Workflow
### Step 1: Setup (Preamble)
```bash
node ./.claude/scripts/shared/review-preamble.js $ISSUE --no-redirect [--with extensions] [--mode mode] [--force]
```
Parse JSON. `ok: false` → `errors[0].message` → **STOP**. `earlyExit: true` → report review count → **STOP**.
Extract: `context` (issue data, reviewNumber, PRD file path), `criteria`, `extensions`, `warnings`.
Read PRD at extracted path. Not found → **STOP**.
**Extension Loading:** Preamble handles from `.claude/metadata/review-extensions.json`. Unknown IDs warn; if the registry is not found or malformed, fall back to standard review only.
### Step 1b: Locate Test Plan
Check `Test-Plan-*.md` in same directory.
Exists → read for cross-reference. Missing → warn, continue PRD-only (non-blocking).

<!-- USER-EXTENSION-START: pre-review -->
<!-- USER-EXTENSION-END: pre-review -->

### Step 2: Evaluate Criteria

<!-- USER-EXTENSION-START: criteria-customize -->
<!-- USER-EXTENSION-END: criteria-customize -->

**2a: Auto-Evaluate Objective**
Re-read `.claude/metadata/prd-review-criteria.json` from disk. Use `autoCheckMethod` per criterion. Emit ✅/⚠️/❌ with evidence. Reviews acceptance criteria; evaluates requirements completeness, user story format, AC, NFRs (perf/security/scale), cross-refs, story numbering.
**`requiresTestPlan`:** skip when no test plan.
**Graceful:** missing/malformed → warn + inline defaults. Skip criteria missing `autoCheckMethod`. Non-blocking.

**2b: Ask Subjective**
Load subjective from `prd-review-criteria.json`. **Decomposition preview:** extract epic/story structure, display before asking. Use `AskUserQuestion` with each `question`, `header`, `options`. Partial valid. **Solo:** skip entirely.

**2c: Extension Criteria** (if `--with`)
Auto-evaluate objective; ask subjective.

**2d: Recommendation**
- **Ready for backlog creation** — No blocking
- **Ready with minor revisions** — Small issues
- **Needs revision** — Address first
- **Needs major rework** — Fundamental issues

Extensions can **escalate** but not downgrade.
**Applicability filter:** Omit extension sections with no findings. Only domains with findings in `**Extensions Applied:**`. No findings + `--with` → fallback to standard with warning. At least one domain section must appear when `--with` used.
### Step 3: Update PRD File
**`**Reviews:** N`:** increment if exists, else add `**Reviews:** 1` after metadata.
**Review Log:** append row to `## Review Log` table. Missing section → insert before `**End of PRD**` (or append at end — DD14 fallback).
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
Finalize handles: body metadata (`**Reviews:** N` increment), structured comment, label assignment (`reviewed`/`pending`). Clean up temp file. **Read** `.claude/scripts/shared/lib/findings-schema.json` for contract structure, required fields, status values, recommendation values.
**`type` MUST be `"prd"`** — not `"story"`, `"generic"`, omitted (#2594). Drives two behaviours:
- **Header verb.** `review-finalize.js` derives `## PRD Review #N`. Any other value emits `## Issue Review #N`, which `/resolve-review` cannot reconcile with a PRD — reports `NO_REVIEW` against a review that exists.
- **AC check-off suppression.** `prd` is tracker-shaped, so Step 5 leaves the tracker's lifecycle checklist alone instead of checking boxes positionally.
Non-`--with`: append:
```
Tip: Use --with security,performance to add domain-specific review criteria.
Available: security, accessibility, performance, chaos, contract, qa, seo, privacy (or --with all)
```
### Step 5: AC Check-Off (Conditional)
**Only if recommendation starts with "Ready for":**
```bash
node .claude/scripts/shared/review-ac-checkoff.js --issue $ISSUE --findings .tmp-$ISSUE-findings.json
```
Script reads `type` from findings JSON. With `type: "prd"` it returns `skipped: true` and checks off **nothing** — a PRD tracker's checklist is a 3-item lifecycle gate, not the ~20 review criteria, so positional check-off would check whichever box aligned with a passing criterion (#2594).
Report the skip, not a count: `"AC check-off skipped: #$ISSUE is a PRD tracker; its Y lifecycle gates are owned by the commands that close them."` NEVER report `X/Y checked off` here — X is always 0. No status transition — `/create-backlog` owns it, and owns "Ready for backlog creation" too.
**Otherwise:** skip entirely.

<!-- USER-EXTENSION-START: post-review -->
<!-- USER-EXTENSION-END: post-review -->

### Step 6: Closing Notification and Cleanup
Two parts in order; the prune is **part of** this step, not a trailing step a reader can stop before. **(1)** Output `closingNotification` from finalize output. **(2) Prune the task list** (unconditional — every path, including redirect and early-exit paths where Phase 1 created a preamble task and Phase 2 never ran): `TaskList` to enumerate, then `TaskUpdate status=deleted` for every task owned by this `/review-prd` invocation (Phase 1 preamble, Phase 2 step tasks, `USER-EXTENSION` tasks). Do **not** delete tasks created outside this invocation (user TODOs).
---
## Error Handling
| Situation | Response |
|-----------|----------|
| Preamble `ok: false` | `errors[0].message` → STOP |
| PRD not found | Path error → STOP |
| Test plan missing | Warn, continue |
| Issue closed | Ask user (from preamble context) |
| Write fails | Report error → STOP |
---
**End of /review-prd Command**
