---
version: "v0.99.0"
description: Create a bug issue with standard template (project)
argument-hint: "<title>"
copyright: "Rubrical Works (c) 2026"
---
<!-- EXTENSIBLE -->
# /bug
Create a labeled bug issue with standard template and add to project board.
**Extension Points:** `.claude/metadata/extension-points.json` or `/extensions list --command bug`
## Prerequisites
- `gh pmu` installed, `.gh-pmu.json` configured
## Arguments
| Argument | Description |
|----------|-------------|
| `<title>` | Bug title (e.g., `assign-branch fails on Windows paths`) |
| `--assignee <value>` | GitHub login for the new issue. Omitted → `@me`. |

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
### Step 2b: Detect Version
Priority: `package.json` → `version` | git tag (`git describe --tags --abbrev=0`) | prompt user.
**If detected**, confirm via `AskUserQuestion` with "Yes, use {version}" (default) / "No, let me specify".
**Override provided:** use it.

<!-- USER-EXTENSION-START: pre-create -->
<!-- USER-EXTENSION-END: pre-create -->

### Step 3: Create Issue
Body template:
```markdown
## Bug Report

**Description:**
{user description or "To be documented"}

**Version:**
{detected or user-provided version}

**Steps to Reproduce:**
1. ...

**Expected Behavior:**
...

**Actual Behavior:**
...

**Scope:**
- **In scope:** {infer from description, or "To be documented"}
- **Out of scope:** {infer from description, or "To be documented"}
**Acceptance Criteria:**
- [ ] {infer from description, or "To be documented"}

**Proposed Fix:**
{infer from description if enough context, or "To be documented"}
```
Populate from user input where possible. Use "To be documented" only where insufficient.

Create:
```bash
gh pmu create --title "[Bug]: {title}" --label bug --status backlog --priority p1 --assignee {assignee} -F .tmp-body.md
rm .tmp-body.md
```
**Note:** Always `-F .tmp-body.md` (never inline `--body`).
**Assignee:** substitute `{assignee}` from `node .claude/scripts/shared/lib/gh-pmu-config.js --assignee <value>` — pass the user's `--assignee` value, omit when none given. Helper returns that login, else `@me`; reads no config file. NEVER hardcode a login or drop the flag (omitted `--assignee` silently creates an unassigned issue). Unresolvable login → `gh pmu` exits 1 and creates nothing; report the error, do NOT retry without the flag.
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
