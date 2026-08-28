---
version: "v0.98.0"
description: Review a proposal with tracked history (project)
argument-hint: "#issue [--with ...] [--mode ...] [--force]"
copyright: "Rubrical Works (c) 2026"
---
<!-- EXTENSIBLE -->
# /review-proposal
Reviews a proposal document linked from a GitHub issue. Delegates setup to `review-preamble.js`. Document file updates (Reviews metadata, Review Log) are handled inline; issue body updates, comment posting, label assignment are handled by the calling orchestrator.
**Extension Points:** `/extensions list --command review-proposal`

## Prerequisites
- `gh pmu` extension installed
- `.gh-pmu.json` configured
- Issue body must contain `**File:** Proposal/[Name].md`

## Arguments
| Argument | Required | Description |
|---|---|---|
| `#issue` | Yes | Issue linked to the proposal |
| `--with` | No | Comma-separated domain extensions, or `--with all` |
| `--mode` | No | Transient override: `solo`, `team`, `enterprise` |
| `--force` | No | Force re-review even if `reviewed` label present |

## Execution Instructions
**REQUIRED:** Routed command — two-phase task creation:
1. **Phase 1 — Preamble task only:** Create one task for preamble/setup via `TaskCreate`.
2. **Phase 2 — Bulk create after routing:** After preamble confirms no redirect/early exit, bulk-create remaining tasks.
3. **On redirect or early exit:** Mark preamble completed, prune the task list per Closing Notification and Cleanup part (2), then stop. Do NOT create remaining tasks.
4. **Include Extensions:** Each non-empty `USER-EXTENSION` block → task in Phase 2.
5. **Track Progress:** `in_progress` → `completed`.
6. **Post-Compaction:** Re-read spec, resume from first incomplete task.

## Workflow
### Step 1: Setup (Preamble Script)
```bash
node ./.claude/scripts/shared/review-preamble.js $ISSUE --no-redirect [--with extensions] [--mode mode] [--force]
```
Parse JSON. `ok: false` → report `errors[0].message`, **STOP**. `earlyExit: true` → report review count, **STOP**. Extract `context` (issue, reviewNumber, `**File:**` path), `criteria`, `extensions`, `warnings`. Read proposal; not found → **STOP**.
**Extension Loading:** preamble loads from `.claude/metadata/review-extensions.json`. Unknown IDs → warnings. Missing/malformed → fall back to standard review only.

<!-- USER-EXTENSION-START: pre-review -->
<!-- USER-EXTENSION-END: pre-review -->

### Step 1b: Construction Context Discovery
Search `Construction/Design-Decisions/` and `Construction/Tech-Debt/` for keywords from proposal title and `Issue #$ISSUE` references. Report matches as `### Construction Context` with file path, title, date. If none, report `No Construction context found` and continue.

### Step 2: Evaluate Criteria

<!-- USER-EXTENSION-START: criteria-customize -->
<!-- USER-EXTENSION-END: criteria-customize -->

**Step 2a: Auto-Evaluate Objective Criteria**
Re-read `.claude/metadata/proposal-review-criteria.json` from disk (not memory). For each, use `autoCheckMethod`. Emit ✅/⚠️/❌ with evidence. Evaluates completeness, consistency, feasibility, quality, cross-references, Path Analysis, acceptance criteria format.
**Graceful degradation:** If missing/malformed, warn and use inline defaults: Required sections, Status field, Cross-references, Acceptance criteria, Prerequisites, No contradictions, Solution detail, Alternatives, Impact assessment, Criteria match solution, Edge cases, Self-contained, Writing clarity, Technical feasibility, Test coverage, Diagrams, Path Analysis, Screen coverage. If criteria array empty, warn and fall back. Per-criterion validation: skip criteria missing `autoCheckMethod`. All failures non-blocking.

**Step 2a-gate: Path Analysis Gate**
After evaluating `path-analysis-present`, if ⚠️ or ❌ (section missing):
1. **STOP** evaluation
2. `AskUserQuestion` with options:
   - "Run /paths now (Recommended)" — invoke `/paths #N`, wait, re-read proposal, re-evaluate. Now present: ✅. Still missing: ⚠️.
   - "Continue without" — record ⚠️ and resume
3. If already ✅: no prompt, continue normally.

**Step 2a-iv: Prior-Art Sweep When Marker Absent (#2517)**
Trigger: `prior-art-checked` ❌ (absent, or `PARTIAL` — incomplete sweep, treated as absent, re-swept). Covers `proposal`-labelled issues redirected here. Delegate, do not re-derive: `node -e "console.log(JSON.stringify(require('./.claude/scripts/shared/lib/prior-art-marker.js').decideSweep({body:BODY,createdAt:CREATED_AT,reviewSweep:REVIEW_SWEEP})))"` → `{sweep, status, reason}`; report the criterion with that `status`.
`pass` = complete marker, no sweep/write. `fail` = absent/`PARTIAL` under mode `full`, sweep. `recommend` ⚠️ = absent/`PARTIAL` under mode `recommend` (default) — **no sweep/write**; report ⚠️ with the `formatSweepAdvisory()` text naming the runnable command, **not** ❌, which would downgrade nearly every review since `--prior-art` is opt-in and rarely passed. `skip` ⊘ = mode `flag-only`/`off`, no sweep/write — report ⊘, **not** ❌, which downgrades every review in an opted-out project. `not-applicable` = predates the feature (pinned cutoff).
**Arguments — substitute from the named source; never a literal placeholder.** `BODY` ← `context.issue.body`; `CREATED_AT` ← **`context.issue.createdAt`** (ISO 8601, added #2539); `REVIEW_SWEEP` ← `framework-config.json` `reviewSweep` (absent = `recommend`, #2564; call `require('./.claude/scripts/shared/lib/framework-config.js').ensureReviewSweep(process.cwd())` first to materialize it — second writer alongside Praxis Hub Manager; fills a missing key only, leaves a legacy boolean for read-time migration). Reached through the preamble's redirect branch, which carries the same `context.issue`, so no second call is needed.
**`createdAt` decides whether the criterion means anything.** `isExemptFromSweep` treats absent/unparseable as exempt — safe, but it makes `decideSweep` return `not-applicable` for *every* unmarked proposal; before #2539 the preamble emitted no such field, so that is what happened. Never substitute a hand-entered date. A `CREATED_AT_UNAVAILABLE` preamble warning → say so in the criterion rather than reporting a bare `not-applicable`.
**Sweeping:** run the #2514 procedure reading `.claude/metadata/prior-art-sweep.json` (surfaces, excludes, terms, dispositions, formats) — not restated here. **Output:** findings in the review; write `**Prior Art:**` via `insertPriorArtSection` into both `Proposal/[Name].md` and the tracking issue body.
**Ordering is load-bearing.** Write **here, before Step 3** updates `**Reviews:** N`; a write during or after Step 3 races it — both read-modify-write the same content, later wins, loser vanishes silently. **Recommendation:** prior art duplicating the proposal's scope is blocking — `Needs revision`+. **Missing config:** `prior-art-sweep.json` unreadable → report criterion, warn, skip sweep; do not fail the review.

**Step 2b: Ask Subjective Criteria**
Load subjective criteria from `proposal-review-criteria.json`. **Scope Context Display:** extract scope section and present inline before asking. Handle missing scope gracefully (not an error). Use `AskUserQuestion` with each criterion's `question`/`header`/`options`. Partial reviews valid — record skipped as "⊘ Skipped". **Solo mode:** skip entirely.

**Step 2c: Extension Criteria** (if `--with` specified)
Evaluate extension criteria loaded by preamble. Auto-evaluate objective; ask subjective.

**Step 2d: Determine Recommendation** — one of: **Ready for implementation** (no blocking concerns) / **Ready with minor revisions** (small issues) / **Needs revision** (address first) / **Needs major rework** (fundamental issues).

Extension findings can **escalate** but cannot downgrade.
**Applicability Filtering:** Omit extension domain sections with no applicable findings. Only domains with findings appear in `**Extensions Applied:**`. If no findings with `--with`, fall back to standard with warning. At least one domain section must appear when `--with` is used.

### Step 3: Update Proposal File
**Update `**Reviews:** N`:** increment if exists, add `**Reviews:** 1` after metadata if not.
**Update Review Log:** append row to `## Review Log` table. If missing, insert before `**End of Proposal**` marker (or append at end).
```markdown
| # | Date | Reviewer | Findings Summary |
|---|------|----------|------------------|
| N | YYYY-MM-DD | Claude | [Brief one-line summary] |
```
Append only. **Never edit or delete existing rows.**

### Step 4: Write Findings
Write structured findings to `.tmp-$ISSUE-findings.json` for the calling orchestrator.

For non-`--with` runs, append discoverability tip:
```
Tip: Use --with security,performance to add domain-specific review criteria.
Available: security, accessibility, performance, chaos, contract, qa, seo, privacy (or --with all)
```

<!-- USER-EXTENSION-START: post-review -->
<!-- USER-EXTENSION-END: post-review -->

### Step 5: Closing Notification and Cleanup
Two parts in order; the prune is **part of** this step, not a trailing step a reader can stop before. **(1)** Output `closingNotification` from finalize script output. **(2) Prune the task list** (unconditional — every path, including redirect and early-exit paths where Phase 1 created a preamble task and Phase 2 never ran): `TaskList` to enumerate, then `TaskUpdate status=deleted` for every task owned by this `/review-proposal` invocation (Phase 1 preamble, Phase 2 step tasks, `USER-EXTENSION` tasks). Do **not** delete tasks created outside this invocation (user TODOs).

## Error Handling
| Situation | Response |
|---|---|
| Preamble `ok: false` | Report `errors[0].message` → STOP |
| Proposal file not found | Report path error → STOP |
| Issue closed | Ask user (from preamble context) |
| File write fails | Report error → STOP |

**End of /review-proposal Command**
