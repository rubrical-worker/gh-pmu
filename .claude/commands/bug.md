---
version: "v0.110.0"
description: Create a bug issue from the IDPF framework's standard template.
argument-hint: "<title> [--help]"
copyright: "Rubrical Works (c) 2026"
---
<!-- EXTENSIBLE -->
# /bug
Create a labeled bug issue with standard template and add to project board.
**Extension Points:** `.claude/metadata/extension-points.json` or `/extensions --list --command bug`
## Prerequisites
- `gh pmu` installed, `.gh-pmu.json` configured
## Arguments
| Argument | Description |
|----------|-------------|
| `<title>` | Bug title (e.g., `assign-branch fails on Windows paths`) |
| `--assignee <value>` | GitHub login for the new issue. Omitted → `@me`. |
| `--target <owner/name>` | File the issue against a registered companion repository instead of this one. |
| `--help` | Open this command's docs page. Takes precedence over every other argument and performs no action. |

**`--help` takes precedence over every other argument and performs no action:** run `node .claude/scripts/shared/command-help.js bug`, report its output, and STOP.

If not provided, prompt user.
## Execution
**REQUIRED before executing:**
1. Use `TaskCreate` for one task per step below. No routing → bulk create upfront (see rule `07-task-creation-timing.md`).
2. Include one task per active (non-empty) `USER-EXTENSION` block.
3. Mark tasks `in_progress` → `completed` via `TaskUpdate`.
4. **Post-Compaction:** re-read spec, call `TaskList`, resume from first incomplete task.
## Workflow
### Step 1: Parse Arguments
Extract `<title>`.
**Empty:** Ask user before proceeding.
**Special chars** (backticks, quotes): Escape for shell. On Windows, use temp file per shell safety.
### Step 2: Gather Description
Extract `<body>` from args.
**IF insufficient detail**, THEN:
```
Describe the bug (steps to reproduce, expected vs actual behavior):
```
**Description provided:** use as body. **Declined/"skip":** minimal body.
### Step 2a: AC Feasibility Quick-Check (#2424)
If AC text in description mentions a verification mechanism (see `.claude/metadata/ac-feasibility-prompts.json` `verificationGate.triggerPhrases`), apply the `verificationGate` prompt. Append warning to issue body's Scope section; do **not** block. Trigger list is heuristic.
**Also apply `phaseFeasibility` (#2726)** — the question `verificationGate` does not ask: can this AC close inside the phase that owns it? A named mechanism can exist and the AC still be unsatisfiable ("user-reviewed and approved before merge" names a real review whose output does not exist when the box must be checked). Ask whether the condition is knowably true when `/work` reaches Step 5. Apply `phaseFeasibility.actionIfOutOfPhase`:
| Disposition | When | Result |
|---|---|---|
| **Drop** | Work appears in `phaseFeasibility.ownedElsewhere` — CHANGELOG, tagging, release publication belong to `/prepare-release` | Do not author the AC; tell the user which command owns it |
| **Annotate** | Gate is genuinely load-bearing (required human review or sign-off) | `- [ ] {acText} → GATE: {phase}` per `annotationFormat`, and **name the event that resolves it** — a token that cannot name one is not a gate |
Prefer annotation over deletion when the requirement is real: the goal is to stop the gate deadlocking `in_review`, not to remove the requirement. **Warning-only, as `verificationGate` is** — do **not** block creation. `/bug` and `/enhancement` take free text and often carry no ACs, so blocking would stop capture over an AC nobody wrote.
### Step 2b: Detect Version
Priority: `package.json` → `version` | git tag (`git describe --tags --abbrev=0`) | prompt user.
**Why `package.json` first (#2954):** in a user project it is authoritative — the app's own version; a git tag may lag it or belong to something else. Keep this order (the framework repo keeps `package.json` in step with each release instead).
**If detected**, confirm via `AskUserQuestion` with "Yes, use {version}" (default) / "No, let me specify".
**Override provided:** use it.
### Step 2c: Derive Priority (#2992)
Re-read `.claude/metadata/bug-priority-rubric.json` from disk. Compare the bug's stated impact (Description, Actual Behavior, Scope) against each `classes` entry's `description` and `signals`; apply `precedence` — highest priority among matching classes wins. The matched class `id` is the **reason**.
**No class matches → fall back to P2** (`fallback.priority`) and state `fallback.reason` to the user — never choose silently, never fall back to P1 (the fixed default this step replaces, #1520 → #2992).
**Confirm in the same `AskUserQuestion` call** as Step 2b's version (a second question, so filing still costs one prompt): "Priority: {P?} — {reason}. Is this correct?" → "Yes, use {P?}" (default) / "No, let me override". On override ask `p0`/`p1`/`p2`. The confirmed value is `{priority}` in Step 3.
**Why derived:** #1520's "bugs are more urgent than enhancements" holds for broken behavior on a supported path — the rubric keeps it at P1/P0 — but a fixed P1 overstated advisory and dev-only bugs, which `severity-proportionate` flagged repeatedly.

<!-- USER-EXTENSION-START: pre-create -->
<!-- USER-EXTENSION-END: pre-create -->

### Step 3: Create Issue
**Creation goes through the shared script (#3049).** `.claude/scripts/shared/file-issue.js` carries the template, label, status, assignee resolution, a unique `.tmp-` body file and `gh pmu create` — one implementation for `/bug`, `/enhancement` and `/code-review`, so they cannot drift. This step supplies only what it judges: field text, the Step 2c priority, and which Step 2a gates it applied.
**Generate the entry path first — once per invocation, before the entry is composed.** A fixed path collides across sessions filing in this directory — one write lands between another's write and its filing, filing the wrong content silently; no issue number exists yet to name it after (#1034 covers editing only). Suffix from a shelled-out command, never invented:
```bash
BUG_ENTRY_FILE=".tmp-bug-entry-$(node -e "console.log(require('crypto').randomBytes(4).toString('hex'))").json"
```
**Keep the `.tmp-` prefix** so the startup stale-scratch sweep collects a file an interrupted run left.
Write one entry to `$BUG_ENTRY_FILE`:
```json
{
  "type": "bug",
  "title": "{title}",
  "priority": "{priority}",
  "assignee": "{login — only when --assignee was given}",
  "gatesApplied": ["verificationGate", "phaseFeasibility"],
  "fields": {
    "description": "...", "version": "...", "stepsToReproduce": "...",
    "expectedBehavior": "...", "actualBehavior": "...",
    "inScope": "...", "outOfScope": "...",
    "acceptanceCriteria": ["..."], "proposedFix": "..."
  }
}
```
The script renders the standard bug template — Description, Version, Steps to Reproduce, Expected Behavior, Actual Behavior, Scope, Acceptance Criteria, Proposed Fix — filling omitted fields with "To be documented". Populate from the user's description; omit a field only without enough input. Good default AC: "Bug no longer reproduces following the Steps to Reproduce".
`{priority}` is the Step 2c value — never a literal. The script refuses an entry without one rather than defaulting: priority is judged here, never in the script.
`gatesApplied` names only gates Step 2a actually applied — leave out any it did not reach, so `gatesNotApplied` reports it instead of hiding it.
Create:
```bash
node .claude/scripts/shared/file-issue.js --input $BUG_ENTRY_FILE
rm $BUG_ENTRY_FILE
```
Report from `results[0]`: `number`, `url`, `priority`. `ok: false` → "Failed to create issue: {error}" → STOP. A `warning` means created but number unreadable — it **exists**; do NOT file again.
**Assignee:** the script resolves it via `gh-pmu-config.js` — the entry's `assignee` when `--assignee <login>` was given, else `@me` — and never drops the flag (an omitted `--assignee` silently creates an unassigned issue). NEVER hardcode a login. An unresolvable login makes `gh pmu` exit 1 and create nothing; the script reports it as the entry's `error`. Report it; do NOT retry without the login.
**Cross-repo filing (`--target <owner/name>`, #2665):** resolve BEFORE composing the issue — a refusal after the body is written wastes the work and tempts a retry against the wrong repo.
```javascript
const { resolveFilingTarget, resolveBoardFields, formatUnresolvedBoardFields } = require('.claude/scripts/shared/lib/companion-projects.js');
const target = resolveFilingTarget(charterContent, requestedRepo);   // charterContent = CHARTER.md
```
`ok:false` → report `reason` verbatim, **STOP**. Two refusals, never merged: not registered → register with `/charter --update --register-proj`; registered with `fileIssues:false` → marked read-only on purpose, enable with `--register-proj`. Searchable-but-not-filable is the common case — usually deliberate; one combined message sends the user to the wrong remedy.
`ok: true` → **do NOT run `file-issue.js --input`, and never `gh pmu create -R`.** Render the same template into a body file, then delegate to the companion helper:
```bash
BUG_BODY_FILE=".tmp-bug-body-$(node -e "console.log(require('crypto').randomBytes(4).toString('hex'))").md"
node .claude/scripts/shared/file-issue.js --input $BUG_ENTRY_FILE --render-to $BUG_BODY_FILE
```
```javascript
const { fileCompanionIssue } = require('.claude/scripts/shared/file-companion-issue.js');
const resolution = resolveBoardFields(target.entry);   // { resolved, fields, board, reason }
const result = fileCompanionIssue({
  repo: target.entry.repo, title, bodyFile: $BUG_BODY_FILE,
  labels: [LABEL], assignee, status: STATUS, priority: PRIORITY,
  board: target.entry.board || null, fields: resolution.fields,
});
```
```bash
rm $BUG_BODY_FILE $BUG_ENTRY_FILE
```
**Why not `gh pmu create -R` (#2775).** gh-pmu takes the *repository* from `-R` but the *project* from the **local** `.gh-pmu.json`, with no override — so the issue lands on **this** repo's board (px-manager#1155, 2026-09-04).
The helper uses the **bare `gh issue create`** form, then adds the issue to the *companion's* board — the one case the bare form is correct, since the **local** board must not be reached. Membership is redirected, not abandoned.
`PRIORITY` is the Step 2c value, resolved against the **companion board's own** options, not this repo's `.gh-pmu.json`: no matching option → reported under `fields.unset`, filed without a priority rather than on a guess. `assignee` from `node .claude/scripts/shared/lib/gh-pmu-config.js --assignee <login>` when given, else `node .claude/scripts/shared/lib/gh-pmu-config.js --assignee` (bare flag → `@me`); NEVER run the helper with no arguments.
**Report the envelope; re-derive none of it.** `{ok, issue:{number,url}, board:{added, owner, number, fields:{set,unset}}, errors}`.
| Envelope | Report |
|---|---|
| `ok: false` | The `errors` verbatim; nothing was created, or the failure is named |
| `board.added: true`, `fields.unset` empty | Issue number/URL, the board joined, the fields set |
| `board.added: true`, `fields.unset` non-empty | The same **plus every `errors` entry** — an unset field must never pass for a set one |
| `board.added: false` with a board registered | Issue created, board-add failed; print `errors`. The issue **exists** — do not file it again |
| No board registered | Issue created, no board touched. Print `formatUnresolvedBoardFields(repo, resolution)` unchanged |
**Never guess a field or option id** — the helper resolves them from the companion's `gh project field-list`; unresolvable → unset.
**Note:** The body always travels by file (`-F` / `--body-file`), never inline `--body`.
### Step 4: Cleanup, Report, and STOP
Three parts, in order. The prune is **part of** this step, and this step is **numbered** — `One task per numbered step` now covers it, so an unpruned list surfaces as an unfinished task like any other. The halt is part (3) and lives nowhere earlier: while it sat in this step's TITLE a reader stopped at the title and never reached the prune (#2641).
**(1) Prune the task list** (unconditional — every path, including early-exit paths where Phase 1 created tasks and later phases never ran):
1. `TaskList` — enumerate all tasks.
2. For every task owned by this `/bug` invocation, `TaskUpdate status=deleted`.
3. Do **not** delete tasks created outside this invocation (user TODOs).
**(2) Emit the closing output:**
```
Created: Issue #$ISSUE_NUM — [Bug]: {title}
Status: Backlog
Label: bug

Say "/review-issue #$ISSUE_NUM" then "/assign-branch #$ISSUE_NUM" then "work #$ISSUE_NUM" to start working on this bug.
```

<!-- USER-EXTENSION-START: post-create -->
<!-- USER-EXTENSION-END: post-create -->
**(3) STOP.** Do NOT begin work unless user says "work", "fix that", or "implement that".
## Error Handling
| Situation | Response |
|-----------|----------|
| No title | Prompt user |
| Empty after prompt | "A bug title is required." → STOP |
| `gh pmu create` fails | "Failed to create issue: {error}" → STOP |
| Special chars | Escape for shell safety |
**End of /bug Command**
