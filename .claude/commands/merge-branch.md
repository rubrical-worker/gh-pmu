---
version: "v0.103.0"
description: Merge branch to main with gated checks (project)
argument-hint: "[--skip-gates] [--dry-run]"
copyright: "Rubrical Works (c) 2026"
---
<!-- EXTENSIBLE -->
# /merge-branch
Merge current branch to main with gated validation. For merges without version tags (features, refactoring). For versioned releases, use `/prepare-release`.
**Extension Points:** `.claude/metadata/extension-points.json` or `/extensions list --command merge-branch`
---
## Arguments
| Argument | Description |
|----------|-------------|
| `--skip-gates` | Emergency bypass (caution) |
| `--dry-run` | Preview only |
---
## Execution
**REQUIRED:**
1. Parse phases+extensions → `TaskCreate`
2. Task per active `USER-EXTENSION` block
3. Mark `in_progress` → `completed`
4. **Post-Compaction:** re-read, regenerate tasks

**Rules:** One task per numbered phase/step; one per active extension; skip commented-out; phase/step name as content.
---
## Pre-Checks
### Verify Feature Branch
```bash
BRANCH=$(git branch --show-current)
```
Must NOT be `main`. Typical: `feature/*`, `fix/*`, `idpf/*`, `patch/*`, `release/*`.
### Check Tracker
```bash
gh pmu branch current --json tracker
```
If present, closed at end.
---

<!-- USER-EXTENSION-START: pre-gate -->
<!-- Setup: prepare environment before gate checks -->
<!-- USER-EXTENSION-END: pre-gate -->

## Phase 1: Gates
**If `--skip-gates`, skip to Phase 2.**
### Default Gates (Framework-Provided)
Always run (cannot disable):
#### Gate 1.1: No Uncommitted Changes
```bash
git status --porcelain
```
**FAIL if output non-empty.**
#### Gate 1.2: Tests Pass
```bash
npm test 2>/dev/null || echo "No test script configured"
```
**FAIL if tests fail.** Skip if no script.

<!-- USER-EXTENSION-START: gates -->
#### Gate 1.3: E2E Tests Pass

**If `--skip-e2e` was passed, skip this gate.**

```bash
node .claude/scripts/e2e/run-e2e-gate.js
```

The script outputs JSON: `{"success": true/false, "testsRun": N, "testsPassed": N, "duration": N}`

**FAIL if `success` is false.**

E2E tests validate complete workflows against the test project.
<!-- USER-EXTENSION-END: gates -->

### Summary
- ✅ Passed
- ❌ Failed (with details)

**Any failure → STOP.**

<!-- USER-EXTENSION-START: post-gate -->
<!-- Post-gate: actions after all gates pass -->
<!-- USER-EXTENSION-END: post-gate -->

---
## Phase 2: Create and Merge PR
### Step 2.1: Push
```bash
git push origin $(git branch --show-current)
```
### Step 2.2: Create PR
```bash
gh pr create --base main --head $(git branch --show-current) \
  --title "Merge: $(git branch --show-current)"
```

<!-- USER-EXTENSION-START: post-pr-create -->

### Wait for CI
```bash
node .claude/scripts/framework/wait-for-ci.js
```
<!-- USER-EXTENSION-END: post-pr-create -->

### Step 2.2a: Mergeability Gate
Ask GitHub whether the PR *can* merge before asking a human to review it. A conflicting branch otherwise surfaces at 2.5, after a reviewer approved a PR that could never merge.
```bash
gh pr view --json mergeable,mergeStateStatus
```
**Exempt from `--skip-gates`.** That flag waives **policy** gates (approval, tests); mergeability is a correctness precondition — an unmergeable PR cannot merge no matter who approves it. Run on every path. Solo repos reach a merge only via `--skip-gates` (Gate 2.4 cannot be self-approved), so the exemption protects the users with no second reviewer.
| `mergeable` | Action |
|---|---|
| `MERGEABLE` | Report `✅ PR is mergeable — no conflicts with main`, continue to 2.3 |
| `CONFLICTING` | Report affected files, **STOP** — no approval ask, no merge attempt |
| `UNKNOWN` | Poll (below) |
**On `CONFLICTING`:** run `gh pr diff --name-only`. GitHub exposes no per-file conflict list, so this is the PR's changed-file set, not a conflict set — report it as where to look. User resolves: rebase onto `main` or merge `main` in, push, re-run.
**On `UNKNOWN`:** re-query at most **5** times, **2** seconds apart (~10s worst case). GitHub computes mergeability asynchronously, so a just-created PR very often returns `UNKNOWN`; treating that as failure makes the gate flaky on the fast path it protects. Still `UNKNOWN` after the final attempt → emit `⚠️ Mergeability unknown after 5 attempts — proceeding` and **continue**. Never block.
Read-only: no local merge, no `git fetch`, no working-tree change. `mergeable` reflects the merge GitHub would perform. Local dry-run merge was rejected (#2141); #2524 measured it at 197/351 pairings (56% false positives).

### Step 2.3: Wait for Approval
**ASK USER:** Review and approve the PR.
```bash
gh pr view --json reviewDecision
```
#### Gate 2.4: PR Approved
**FAIL if not approved** (unless `--skip-gates`).
### Step 2.5: Merge
```bash
gh pr merge --merge
git checkout main
git pull origin main
```

<!-- USER-EXTENSION-START: post-merge -->
<!-- Post-merge: actions after PR is merged -->
<!-- USER-EXTENSION-END: post-merge -->

### Step 2.6: Workstream Detection (Post-Merge)
After merge, check workstream plan:
1. **Read from disk:** `loadWorkstreamsMetadata('.workstreams.json')`. Not found → skip.
2. **Check:** `postMergeWorkstreamCheck(metadata, mergedBranch)`. `isWorkstream: false` → skip. Returns report data only (`activeSiblings`, `allMerged`, `sharedModules`) — nothing in `plan-workstreams.js` writes `.workstreams.json`.
3. **Persist:** `updateStatus(mergedBranch, 'merged', '.')` from `.claude/scripts/shared/lib/workstream-utils.js`. Do **NOT** write `.workstreams.json` with the Write tool — `updateStatus()` writes atomically (temp + rename) and enforces valid-transition rules; a direct write does neither and can tear the file, breaking `/merge-branch` and `/destroy-branch` for every stream. It re-reads from disk, so step 1's contract holds.
4. **Commit:** `git add .workstreams.json && git commit -m "Update workstream metadata: $BRANCH merged"`
5. **Sibling warning:** `activeSiblings` non-empty → `formatSiblingWarning(activeSiblings, sharedModules)`, display
6. **All merged:** `allMerged: true` → "All workstreams merged. Consider removing `.workstreams.json`."
---
## Phase 3: Cleanup
### Step 3.1: Close Tracker (if exists)
```bash
node .claude/scripts/shared/lib/active-label.js remove [TRACKER_NUMBER]
gh issue close [TRACKER_NUMBER] --comment "Branch merged to main"
```
### Step 3.2: Close Branch in Project
```bash
gh pmu branch close 2>/dev/null || echo "No branch to close"
```
### Step 3.3: Delete Branch
```bash
git push origin --delete $BRANCH
git branch -d $BRANCH
```

<!-- USER-EXTENSION-START: post-close -->
<!-- Post-close: notifications, announcements -->
<!-- USER-EXTENSION-END: post-close -->

---
## Completion
- ✅ All gates passed
- ✅ PR created and merged
- ✅ Tracker closed (if applicable)
- ✅ Branch deleted
---
---
## Error Handling
| Condition | Cause | Resolution |
|---|---|---|
| `CONFLICTING` at 2.2a | Branch conflicts with `main`. **STOP** — no approval requested, no merge attempted. | Rebase onto `main` or merge `main` in, resolve, push, re-run `/merge-branch`. PR stays open; re-running re-queries it. |
| `UNKNOWN` after 5 attempts at 2.2a | GitHub had not computed mergeability within ~10s. **Not a failure** — warn and continue. | None. A real conflict still fails loudly at 2.5; the gate is an early warning, not the only guard. |
| `gh pr view` fails at 2.2a | Network failure, missing auth, or no PR for the current branch. | Report the `gh` error verbatim. Check `gh auth status` or confirm 2.2 created the PR, then re-run. |
| Not approved at Gate 2.4 | No approving review on the PR. | Obtain a review, or re-run with `--skip-gates` if project policy allows self-merge. `--skip-gates` does **not** bypass 2.2a. |
| `gh pr merge` fails at 2.5 | Branch protection, a required check, or a conflict introduced after 2.2a. | Report the `gh` error verbatim and STOP. Do not retry with `--admin` or force-merge. |

## Comparison: /merge-branch vs /prepare-release
| Feature | /merge-branch | /prepare-release |
|---------|---------------|------------------|
| Version bump | No | Yes |
| CHANGELOG update | No | Yes |
| Git tag | No | Yes |
| GitHub Release | No | Yes |
| Gates | Yes | Yes (via validation) |
| PR to main | Yes | Yes |
| Close tracker | Yes | Yes |
| Delete branch | Yes | Yes |

**Use `/merge-branch`:** Feature, fix, non-versioned work.
**Use `/prepare-release`:** Versioned releases with CHANGELOG + tags.
### Step 4: Closing Cleanup
The prune is **part of** this step, and this step is **numbered** — what makes the claim hold. `One task per numbered step` now covers it, so an unpruned list surfaces as an unfinished task like any other step. The same claim as prose alone was overridden by the rules beside it (#2641).

**Prune the task list** (unconditional — every path, including early-exit paths where Phase 1 created tasks and later phases never ran):
1. `TaskList` — enumerate all tasks.
2. For every task owned by this `/merge-branch` invocation, `TaskUpdate status=deleted`.
3. Do **not** delete tasks created outside this invocation (user TODOs).

---
**End of Merge Branch**
