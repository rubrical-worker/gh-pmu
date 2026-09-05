---
version: "v0.100.2"
description: Create a branch with tracker issue (project)
argument-hint: "<branch-name> (e.g., release/v0.16.0, my-feature, bugfix-123)"
copyright: "Rubrical Works (c) 2026"
---
<!-- EXTENSIBLE -->
# /create-branch
Create a new branch and associated tracker issue for any branch type.
**Extension Points:** `.claude/metadata/extension-points.json` or `/extensions list --command create-branch`
---
## Prerequisites
- `gh pmu` extension installed
- `.gh-pmu.json` configured
---
## Arguments
| Argument | Description |
|----------|-------------|
| `$1` | Branch name (valid git name) |
---
## Execution Guidance
Use 2-3 coarse tasks: "Validate and create", "Configure", "Report and commit". Validate inline. Chain independent commands with `&&` or parallel where noted.
---
## Workflow
### Step 1: Validate and Check Working Directory
Validate branch name inline — no spaces, no git-invalid chars. Invalid/empty → error+stop. No tool call.

Then:
```bash
git status --porcelain
```
**If changes exist:**
1. Report: "Uncommitted changes detected. These will be carried to the new branch."
2. Save output for Step 4
3. Continue (do NOT block)

<!-- USER-EXTENSION-START: pre-create -->
### Verify Config
```bash
git status --porcelain .gh-pmu.json
```
**If `success` is false, STOP.**
<!-- USER-EXTENSION-END: pre-create -->

### Step 2: Create Branch and Populate Tracker
```bash
gh pmu branch start --name "$BRANCH"
```
Creates git branch and tracker issue with `branch` label. Extract tracker number.

Write tracker body and update:
```markdown
## Branch: $BRANCH

Tracker issue for branch `$BRANCH`.

### Workflow

- **Assign issues:** `/assign-branch #N #N ...`
- **Work all issues:** `/work #[tracker-number]` (processes sub-issues sequentially)
- **Work single issue:** `/work #N`
- **When ready:** `/merge-branch` or `/prepare-release`

### Sub-Issues

Issues assigned to this branch appear as sub-issues below.
```
```bash
gh pmu edit [TRACKER_NUMBER] -F .tmp-body.md && rm .tmp-body.md
```
### Step 3: Configure Branch (parallelizable)
Switch, push, set labels, auto-assign — independent after creation.
```bash
git checkout "$BRANCH" && git push -u origin "$BRANCH"
```
**In parallel** after checkout/push:
```bash
node .claude/scripts/shared/lib/active-label.js ensure [TRACKER_NUMBER]
```
```bash
gh pmu move [TRACKER_NUMBER] --branch "$BRANCH" && gh issue edit [TRACKER_NUMBER] --add-label assigned
```

<!-- USER-EXTENSION-START: post-create -->
<!-- USER-EXTENSION-END: post-create -->

### Step 4: Report Completion
```
Branch created.

Branch: $BRANCH
Tracker: #[tracker-issue-number]
```
**If uncommitted changes in Step 1:** Report carried-over files from saved output.

**Conditional Commit Prompt:** If any changes exist (staged/unstaged/untracked):
**ASK USER:** "Stage and commit all changes to new branch? (y/n)"
- **Yes:** Auto-generate message: `chore: committed {file summaries} during create-branch` (basenames). Run `git add -A && git commit -m "<auto>"`, report. Do NOT prompt for commit message.
- **No:** Continue without modifying working tree.

**Always end with:**
```
Next steps:
1. Assign issues: /assign-branch #N #N ...
2. Work issues: work #N
3. When ready: /prepare-release
```
---
**End of Create Branch**
