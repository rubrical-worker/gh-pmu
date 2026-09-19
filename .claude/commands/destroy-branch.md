---
version: "v0.104.0"
description: Safely delete a branch after confirmation, using the IDPF framework.
argument-hint: "[branch-name] [--force]"
copyright: "Rubrical Works (c) 2026"
---
<!-- EXTENSIBLE -->
# /destroy-branch
Safely abandon and delete a branch. Destructive — requires explicit confirmation.
**Extension Points:** `.claude/metadata/extension-points.json` or `/extensions list --command destroy-branch`
---
## Arguments
| Argument | Description |
|----------|-------------|
| `[branch-name]` | Branch to destroy (defaults to current) |
| `--force` | Skip confirmation (dangerous) |
---
## Pre-Checks
### Identify Target
```bash
BRANCH=${1:-$(git branch --show-current)}
```
### Cannot Destroy Main
```bash
if [ "$BRANCH" = "main" ] || [ "$BRANCH" = "master" ]; then
  echo "ERROR: Cannot destroy main/master branch"
  exit 1
fi
```
### Check Exists
```bash
git rev-parse --verify "$BRANCH" 2>/dev/null
```
**FAIL if branch does not exist.**
### Workstream Detection (Pre-Destroy)
Before confirming, check workstream plan:
1. **Read from disk:** `loadWorkstreamsMetadata('.workstreams.json')`. Not found → skip.
2. **Check:** `preDestroyWorkstreamCheck(metadata, branchName)`. `isWorkstream: false` → skip.
3. **Show expanded confirmation:**
   - `orphanWarning`: epics losing workstream assignment
   - `assignedEpics`: list with titles
   - `activeSiblings`: other active streams
4. **Proceed to Phase 1 confirmation** — informational, non-blocking
---

<!-- USER-EXTENSION-START: pre-destroy -->
<!-- USER-EXTENSION-END: pre-destroy -->

## Phase 1: Confirmation
**⚠️ DESTRUCTIVE OPERATION**

Will permanently delete:
- Local: `$BRANCH`
- Remote: `origin/$BRANCH`
- Artifacts: `Releases/[prefix]/[identifier]/`
- Tracker issue (closed "not planned")
### Step 1.1: Show What Will Be Destroyed
```bash
git log main..$BRANCH --oneline 2>/dev/null || echo "No unmerged commits"
ls -la Releases/*/$BRANCH/ 2>/dev/null || echo "No release artifacts found"
```
### Step 1.2: Require Explicit Confirmation
**If `--force` NOT passed:**
**ASK USER:** Type the branch name to confirm. Must be exactly `$BRANCH`.
**If mismatch, ABORT.**

<!-- USER-EXTENSION-START: post-confirm -->
<!-- USER-EXTENSION-END: post-confirm -->

---
## Phase 2: Close Tracker
### Step 2.1: Find Tracker
```bash
gh pmu branch current --json tracker 2>/dev/null
```
### Step 2.1.5: Remove Active Label
If found:
```bash
node .claude/scripts/shared/lib/active-label.js remove [TRACKER_NUMBER]
```
### Step 2.2: Close as Not Planned
```bash
gh issue close [TRACKER_NUMBER] \
  --reason "not planned" \
  --comment "Branch destroyed via /destroy-branch. Work abandoned."
```
### Step 2.3: Close Branch in Project
```bash
gh pmu branch close 2>/dev/null || echo "No branch to close"
```
---
## Phase 3: Delete Artifacts
### Step 3.1: Identify Dir
- `release|patch/vX.Y.Z` → `Releases/release|patch/vX.Y.Z/`
- `feature/name` → `Releases/feature/name/` (if exists)
### Step 3.2: Delete
```bash
ARTIFACT_DIR="Releases/${BRANCH_PREFIX}/${BRANCH_ID}"
if [ -d "$ARTIFACT_DIR" ]; then
  rm -rf "$ARTIFACT_DIR"
  git add -A
  git commit -m "chore: remove artifacts for destroyed branch $BRANCH"
fi
```
---
## Phase 4: Delete Branch
### Step 4.1: Switch to Main (if on target)
```bash
if [ "$(git branch --show-current)" = "$BRANCH" ]; then
  git checkout main
  git pull origin main
fi
```
### Step 4.2: Delete Remote
```bash
git push origin --delete "$BRANCH" 2>/dev/null || echo "Remote branch not found"
```
### Step 4.3: Delete Local
```bash
git branch -D "$BRANCH"
```
`-D` (force) — user confirmed abandoning unmerged work.

<!-- USER-EXTENSION-START: post-destroy -->
<!-- USER-EXTENSION-END: post-destroy -->

### Workstream Metadata Update (Post-Destroy)
After deletion, if applicable:
1. **Read from disk:** `loadWorkstreamsMetadata('.workstreams.json')`. Not found → skip.
2. **Report data:** `postDestroyWorkstreamUpdate(metadata, branchName)` returns `orphanedEpics` and `reassignmentOptions`. Despite the name it writes nothing; `plan-workstreams.js` never writes `.workstreams.json`.
3. **Persist:** call `isWorkstreamBranch(branchName, metadata)` from `.claude/scripts/shared/lib/workstream-utils.js` first. `false` → skip to step 5, persisting and committing nothing (`updateStatus()` throws `Branch "X" not found`; destroying a non-workstream branch stays a no-op). `true` → `updateStatus(branchName, 'destroyed', '.')`. Do **NOT** write `.workstreams.json` with the Write tool — `updateStatus()` writes atomically (temp + rename) and enforces valid-transition rules, including the block on reviving a destroyed stream.
4. **Commit:** only if step 3 persisted. `git add .workstreams.json && git commit -m "Update workstream metadata: $BRANCH destroyed"`
5. **Epic reassignment:** If `orphanedEpics` non-empty, present `reassignmentOptions`:
   - Each option shows a sibling branch and its epics; user selects a target or "leave unassigned"
   - Target selected: `gh pmu move [epic#] --branch [target]` per epic
6. **All destroyed:** No active streams remain → "No active workstreams remain. Consider removing `.workstreams.json`."

---
## Completion
Branch destroyed:
- ✅ User confirmed
- ✅ Tracker closed (not planned)
- ✅ Artifacts deleted
- ✅ Remote deleted
- ✅ Local deleted

**Cannot be undone.** Unpushed commits are lost.
---
## Recovery
1. **Pushed:** check teammate clones
2. **Local only:** `git reflog` within ~30 days
3. **Artifacts:** backups or git history
---
**End of Destroy Branch**
