---
version: "v0.114.0"
description: Prepare a release by opening a PR, merging to main, and tagging, using the IDPF framework.
argument-hint: "[version] [--skip-coverage] [--skip-review] [--dry-run] [--help]"
copyright: "Rubrical Works (c) 2026"
---

<!-- EXTENSIBLE -->
# /prepare-release

Validate, create PR to main, merge, and tag for deployment.

**Extension Points:** See `.claude/metadata/extension-points.json` or run `/extensions --list --command prepare-release`
## Arguments
| Argument | Description |
|----------|-------------|
| `[version]` | Version to release (e.g., v1.2.0) |
| `--skip-coverage` | Skip coverage gate |
| `--skip-review` | Skip the branch diff review gate (Step 2.1); skip recorded in the Summary Checklist |
| `--dry-run` | Preview without changes; the Step 2.1 review gate reports what it would review and runs no review |
| `--help` | Open this command's docs page. Takes precedence over every other argument and performs no action. |

**`--help` takes precedence over every other argument and performs no action:** run `node .claude/scripts/shared/command-help.js prepare-release`, report its output, and STOP.

## Execution Instructions

**REQUIRED:** Before executing:
1. **Create Task List:** Parse phases and extension points, use `TaskCreate`
2. **Include Extensions:** Add task for each non-empty `USER-EXTENSION` block
3. **Track Progress:** Mark tasks `in_progress` → `completed`
4. **Post-Compaction:** Re-read spec and regenerate tasks

**Task Rules:** One task per numbered phase/step; one per active extension; skip commented-out extensions.

**Prompt shape (#3233).** Every **ASK USER** below follows `{frameworkPath}/Reference/AskUserQuestion-Shape.md`: context printed first as markdown (a results table where there are results), then an options table whose first column is `{n}.{label}`, a one-line question, unprefixed option labels, and for a single-select prompt a `preview` per option stating what that choice does. Each prompt names its context, question, options and previews; `.claude/hooks/ask-question-shape-hook.js` blocks only the tool's hard limits (4 options, a 12-character header); the rest of the shape is convention.

## Pre-Checks
### Check for Uncommitted Changes

```bash
git status --porcelain
```

**If empty (clean):** Proceed silently. **If non-empty (dirty):** print the changes as a table (`Status`, `Path`), then options table `1.Stage and commit all`, `2.Let me review first`, `3.Continue anyway`, then present via `AskUserQuestion`:

```javascript
AskUserQuestion({
  questions: [{
    question: "Uncommitted changes found: how should the release proceed?",
    header: "Dirty tree",
    options: [
      { label: "Stage and commit all", description: "Run git add -A and commit with a message you provide", preview: "Runs git add -A, asks for a commit message, commits, then continues the release on a clean tree." },
      { label: "Let me review first", description: "Stop here so you can review and handle changes manually", preview: "Stops here. Nothing is staged or committed; handle the changes, then re-run /prepare-release." },
      { label: "Continue anyway", description: "Proceed with release preparation despite uncommitted changes", preview: "Proceeds with the changes uncommitted. They can be lost when the branch is closed." }
    ],
    multiSelect: false
  }]
});
```

- **"Stage and commit all":** Ask for commit message, then `git add -A && git commit -m "<message>"`. Report commit. Continue.
- **"Let me review first":** Report `"Stopping. Review uncommitted changes, then re-run /prepare-release."` → **STOP**
- **"Continue anyway":** Report `"⚠️ Warning: Proceeding with uncommitted changes."` Continue.

### Verify Current Branch

```bash
git branch --show-current
```

Record as `$BRANCH`.

### Auto-Create Release Branch (if on main)

**If `$BRANCH` is `main`:**
1. Analyze commits: `git log $(git describe --tags --abbrev=0)..HEAD --oneline`
2. Recommend version based on commit analysis
3. **ASK USER:** Confirm version (e.g., `v0.26.0`) — print the commit analysis (commits by type, recommended bump) as a table, then options `1.Use <version>`, `2.Another version`, `3.Stop`; question `Create release/<version> from main?`; previews: *Use <version>* creates and checks out `release/<version>` locally and continues; *Another version* asks for the version, then creates that branch; *Stop* creates nothing.
4. **If `--dry-run`:** Report "Would create branch: release/v0.26.0" and stop.
5. Create release branch:
   ```bash
   gh pmu branch start --name "release/$VERSION"
   git checkout "release/$VERSION"
   ```
   **Not pushed here (#3364)** — Step 3.2 publishes it with the prep commit; a push now runs CI on a head superseded moments later.
6. Update `$BRANCH` to `release/$VERSION`
7. Report: "Created release branch: release/$VERSION. Continuing..."

**If NOT `main`:** Continue with existing working branch.

### Check for Incomplete Issues

Preview what Step 4.3's close will move, by the close's own rule (#2878):

```bash
gh pmu branch close "$BRANCH" --dry-run
```

Name `$BRANCH` explicitly (required when more than one branch is active). **Show the output verbatim; do not parse it or add `--json`.** It prints `Would move N incomplete issue(s) to backlog:` then one `#N - title` line per issue, and changes nothing. The close moves every open tracker sub-issue except Parking Lot, keyed on open/closed and tracker membership — not board Status — so a `gh pmu list --status` filter answers a different question; `--status` also takes one value, and the comma-separated query this step used to run matched nothing while Step 4.3 moved everything.

- **No issues listed:** say so explicitly — "No incomplete issues: Step 4.3's close will move nothing." — and proceed. Never infer an empty result from silence.
- **Issues listed:** **ASK USER** via `AskUserQuestion`, naming them: print them as a table (`#`, `Title`, `Status`), then options `1.Transfer`, `2.Continue anyway`, `3.Stop`; question `<N> incomplete issues move to Backlog at Step 4.3: how to proceed?`; each option's preview states its effect:
  - **Transfer** — `/transfer-issue` to another branch, re-run the dry-run, ask again.
  - **Continue anyway** — first state: "Step 4.3 will move #N, #N… to Backlog and clear their Branch field." Then proceed.
  - **Stop** — halt now, before Phase 1; nothing has changed.

<!-- USER-EXTENSION-START: pre-phase-1 -->
<!-- USER-EXTENSION-END: pre-phase-1 -->

## Phase 1: Analysis

### Step 1.1: Analyze Changes

```bash
git log $(git describe --tags --abbrev=0)..HEAD --oneline
```

### Analyze Commits

```bash
node .claude/scripts/shared/analyze-commits.js
```

Outputs JSON: `lastTag`, `commits`, `summary` (counts by type). This is the **commit inventory** the changelog consumes — it **does not decide the version**.

### Recommend Version

```bash
node .claude/scripts/shared/recommend-version.js
```

**`recommend-version.js` is authoritative for the version bump (#2602).** When the two disagree its verdict governs, and `analyze-commits.js`'s `summary` counts are not evidence against it.

They classify differently by design: `analyze-commits.js` reads conventional-commit prefixes only; `recommend-version.js` reads prefixes, then falls back to `Refs #N` → issue-label lookup, then keyword heuristics.

**Under a `Refs #N` convention the disagreement is the steady state, not an edge case.** Every commit types as `other`, so `summary` reads `feat: 0, fix: 0` on **every** release — which looks like "no features this release" when it means "this classifier cannot see them". Measured here at `v0.100.2..HEAD`: 38 commits, all `other`, against a `minor` recommendation.

<!-- USER-EXTENSION-START: post-analysis -->

### Documentation Review

Check if docs need updates based on changes:

- [ ] `docs/commands.md` - if commands/flags changed
- [ ] `docs/configuration.md` - if config options changed
- [ ] `README.md` - if user-facing features changed

**Only update if changes affect documentation.**

### E2E Impact Analysis

```bash
node .claude/scripts/e2e/analyze-e2e-impact.js
```

The script analyzes which E2E tests may be impacted by changes:
- `impactedTests`: Test files that cover changed commands
- `newCommandsWithoutTests`: Commands modified without E2E coverage
- `recommendation`: Suggested test review actions

**If `newCommandsWithoutTests` is non-empty, warn user about missing coverage.**

<!-- USER-EXTENSION-END: post-analysis -->

**ASK USER:** Confirm version before proceeding. Print the Phase 1 analysis (commits by type, recommended version) as a table, then options `1.Proceed`, `2.Stop`; question `Proceed with <version>?`; previews: *Proceed* continues to Phase 2 validation; *Stop* ends here, nothing changed.

## Phase 2: Validation

<!-- USER-EXTENSION-START: pre-validation -->
### Handle Incomplete Issues

If incomplete issues exist, prompt user:
- Transfer to next release
- Return to backlog
- Block release (cannot proceed with open issues)

### Lint Gate

```bash
node .claude/scripts/prepare-release/lint.js
```

The script outputs JSON: `{"success": true/false, "message": "..."}`

**If `success` is false, STOP and report the error.**

Runs `golangci-lint run --timeout=5m` to catch lint errors before tagging.

<!-- USER-EXTENSION-END: pre-validation -->

<!-- USER-EXTENSION-START: post-validation -->

### Step 2.1: Run Tests

```bash
go test ./...
```

### Coverage Gate

**If `--skip-coverage` was passed, skip this section.**

```bash
node .claude/scripts/prepare-release/coverage.js
```

The script outputs JSON: `{"success": true/false, "message": "...", "data": {"coverage": 87.5}}`

**If `success` is false, STOP and report the error.**

Coverage metrics include total percentage and threshold comparison.

**Configuration** (`.gh-pmu.json`):
```yaml
release:
  coverage:
    enabled: true
    threshold: 80
    skip_patterns:
      - "*_test.go"
      - "mock_*.go"
```

### E2E Gate

**If `--skip-e2e` was passed, skip this section.**

```bash
node .claude/scripts/e2e/run-e2e-gate.js
```

The script outputs JSON: `{"success": true/false, "testsRun": N, "testsPassed": N, "duration": N}`

**If `success` is false, STOP and report the error.**

E2E tests validate complete workflows against the test project.
<!-- USER-EXTENSION-END: post-validation -->

### Step 2.1: Branch Diff Review Gate
Refuses a release with an unreviewed change or an introduced high-severity finding; runs before Phase 3 touches version files.
- `--skip-review` → report `Review gate skipped (--skip-review).` and continue, even with `--dry-run`; the reference below is not read.
- Otherwise read `{frameworkPath}/Reference/code-review-execution.md` and execute its § Release Review Gate with `<command>` = `prepare-release` — the whole gate (base, empty diff, `lastDiffReview` freshness, inline `/code-review --diff` on a missing/stale record, `--dry-run`, severity policy), shared with `/prepare-beta`.
- The file cannot be read → **STOP** with `Review gate could not read {frameworkPath}/Reference/code-review-execution.md; /prepare-release stopped.` The gate never passes unread.

**ASK USER:** Confirm validation passed. Print the Phase 2 results as a table (check, result: Step 2.0 helpers, the validation phases, coverage, the review gate), then options `1.Continue`, `2.Stop`; question `Validation for <version> passed: continue to Phase 3?`; previews: *Continue* lets Phase 3 edit the version files and CHANGELOG; *Stop* ends before any file is edited.

## Phase 3: Prepare

### Step 3.1: Update Version Files

| File | Action |
|------|--------|
| `CHANGELOG.md` | Add new section following Keep a Changelog format. **Lead contract — see below.** |
| `README.md` | Update version badge or header |
| `README-DIST.md` | **If present** — verify skill/specialist counts match actuals, license populated. Absent in most consuming projects |
| `framework-config.json` | (Self-hosted only) Update `frameworkVersion` and `installedDate` |

**CHANGELOG lead contract (#2777).** The *lead* is the prose between the version heading and `**Upgrade notes:**`. `update-release-notes.js` copies it verbatim into the GitHub release body, so it is the first thing a user installing this release reads. Write it as a user would read it on the releases page:
- **One short paragraph, about three to five sentences**, saying what is new and what changed for the user.
- **No bug narrative, no root-cause explanation, no post-mortem of the release run itself.** Those have homes — `Construction/Design-Decisions/` and the release test plan — and repeating them at the top of the release page adds length and reads as an incident report.
- **Validation findings from Phase 2 go in the release test plan**, with at most a one-line pointer from the changelog.
- **Do not repeat the counts.** `update-release-notes.js` prepends its own `Feature release with N new features, M changes, and K fixes.` summary, so a lead opening with the same counts publishes them twice.
Structured parts unchanged: `**Upgrade notes:**` bullets and the `### Added` / `### Changed` / `### Fixed` lists already carry the detail and are what a reader scans.
<!-- USER-EXTENSION-START: pre-commit -->
### Update Version Constant

Update `internal/version/version.go` — set `Version` constant to new version (e.g., `const Version = "1.2.0"`).
<!-- USER-EXTENSION-END: pre-commit -->

### Step 3.2: Commit Preparation

```bash
for path in CHANGELOG.md README.md README-DIST.md Docs/; do
  if [ -e "$path" ]; then git add "$path"; fi
done
git commit -m "chore: prepare release $VERSION"
if git rev-parse --abbrev-ref --symbolic-full-name @{u} >/dev/null 2>&1; then
  git push
else
  git push -u origin "$BRANCH"
fi
```
**One push publishes the branch (#3364).** A branch tracking an upstream gets the bare `git push`; a release branch just auto-created has none, so this push publishes it with the prep commit and sets the upstream.

**Stage only what exists (#2602).** `git add` on a missing pathspec is **fatal** (exit 128) — nothing staged, no commit. `README-DIST.md` exists here and in almost no consuming project. Adjust the path list to this project's release files; the existence guard makes an extra entry harmless, not a reason to leave a wrong one.

**Casing is load-bearing.** The pathspec must match how git tracks the directory (`Docs/` here). A wrong-case pathspec is a second fatal `git add` on a case-sensitive filesystem, and passes silently on Windows and macOS — invisible where releases are usually prepared.

<!-- USER-EXTENSION-START: post-prepare -->
### Wait for CI

```bash
node .claude/scripts/shared/wait-for-ci.js
```

The script polls CI status every 60 seconds (5-minute timeout).

**If CI fails, STOP and report the error.**
<!-- USER-EXTENSION-END: post-prepare -->

**CRITICAL:** Do not proceed until CI passes.

## Phase 4: Git Operations

### Step 4.1: Create PR to Main

```bash
gh pr create --base main --head $(git branch --show-current) \
  --title "Release $VERSION"
```

<!-- USER-EXTENSION-START: post-pr-create -->

### Wait for CI
```bash
node .claude/scripts/shared/wait-for-ci.js
```
**If CI fails, STOP and report.**

<!-- USER-EXTENSION-END: post-pr-create -->

### Step 4.2: Merge PR

**ASK USER:** Approve and merge. Print the PR (number, URL, checks) as a table, then options `1.Approve and merge`, `2.Stop`; question `Merge PR #<N> into main?`; previews: *Approve and merge* announces the release, merges, then closes the tracker and tags; *Stop* leaves the PR open, nothing merged.

**Once approved, announce the release to every peer before merging (#2960).** The merge to `main`, tracker close, tag push that starts the distribution deploy and branch deletion all follow, and none can be undone:
```bash
node .claude/scripts/shared/announce.js --event release-starting --branch "$BRANCH" --tag "$VERSION"
```
Script composes the text (never hand-compose); **forced broadcast** — every addressable peer, live `/overwatch` included, whatever `broadcast` says — honoring only the master switch (`enabled: false`, `IDPF_X_SESSION=off`, `discovery: false`); announcement groups do not apply. `announcement.shouldSend` true → `SendMessage` to every `announcement.recipients` entry with `announcement.text`, then close out with the envelope's `dispatchReport` (`--dispatch-result sent|failed --ledger-id <id>`). A failed `SendMessage` is reported and recorded `failed`; the release proceeds. `shouldSend` false → report `announcement.notice` once, continue. **Advisory, never a gate:** nothing awaits delivery, no follow-up is sent.

```bash
gh pr merge --merge
```

### Step 4.3: Close Branch Tracker

Preview again — work may have landed or been transferred since Check for Incomplete Issues — and show the output verbatim:

```bash
gh pmu branch close "$BRANCH" --dry-run
```

Then close. `--yes` stays (stdin is not interactive) but runs only after the list above was shown; the close never moves an issue the user has not seen.

```bash
gh pmu branch close "$BRANCH" --yes
```

### Step 4.4: Switch to Main

```bash
before=$(git stash list | wc -l)
git stash push -m "prepare-release $VERSION"
after=$(git stash list | wc -l)

git checkout main
git pull origin main

if [ "$after" -gt "$before" ]; then git stash pop; fi
```

**Note:** the stash handles uncommitted `settings.local.json` changes (session-specific permission entries added by Claude Code).

**Pop only when something was stashed (#2602).** On a clean tree `git stash` saves nothing and exits 0, then `git stash pop` exits **1** with `No stash entries found.` — the last command before the tag step. The pair straddles checkout/pull, so the sequence appears to work and ends on a failure immediately before an irreversible tag.

The guard compares `git stash list` before and after rather than testing the tree: `git status --porcelain` can report changes `git stash push` declines to save, so a dirtiness test and the stash's own behavior can disagree. The stash count cannot.

### Step 4.4a: Merge-Commit CI Gate
After Step 4.4's pull, `main` is at the commit Step 4.6 tags. First decide whether the PR runs already tested that commit's tree:
```bash
MERGE_SHA=$(git rev-parse HEAD)
PR_MERGE_SHA=$(gh pr view "$BRANCH" --json mergeCommit --jq '.mergeCommit.oid')
node .claude/scripts/shared/wait-for-ci.js --pr-tested --pr "$BRANCH" --sha "$MERGE_SHA"
```
It prints `{ path, reason, runId, runs, workflows }` and waits nothing:
- **`path` is `skip`** → report "Merge commit $MERGE_SHA carries the PR-tested tree — verdict from PR run(s):" with each `runs[]` workflow, `runId` and `url`; continue to the pre-tag extension point without waiting.
- **`path` is `wait`** → report its `reason`, then wait on the push-triggered run for **that** commit before tagging:
```bash
node .claude/scripts/shared/wait-for-ci.js --sha "$MERGE_SHA" --timeout 900
```
**If `MERGE_SHA` differs from `PR_MERGE_SHA`, report both SHAs before waiting** — `main` moved after the merge, and the decision returns `wait`. Wait on `MERGE_SHA` regardless: it is what Step 4.6 tags.

**Why the wait can be skipped (#3444).** When `main` did not move between the PR run and the merge, the merge commit's tree is the tree the `pull_request` runs passed, so the push run re-tests it. The decision skips only on evidence: `MERGE_SHA` is the PR's merge commit, its tree equals the PR head's, and every workflow the merged tree runs on push to the default branch also runs on `pull_request` and passed on the PR head with no skipped job. Workflows pair with runs by workflow id, never by name. A push-only workflow, a skipped PR job, a missing or unsuccessful PR run, or a failed `gh`/`git` lookup returns `wait` with that reason — it never skips on missing evidence. Step 4.7's tag-scoped wait is unaffected.

**On exit 1, 2 or 5 (failed, timed out, or still running at the cap), STOP before tagging.** Report the payload's `runId` and `conclusion` (or `status`) and the run's URL (`gh run view <runId> --json url --jq .url`). Do not continue to Step 4.6: the tag push is irreversible and starts any distribution deploy.

**On exit 3 (`no_runs` / `no_executing_runs`): report "No CI run on merge commit $MERGE_SHA — continuing without a post-merge gate." and continue.**

**Why exit 3 continues here when Step 4.7 STOPs (#3255, #2952).** Step 4.7 first establishes that a tag-triggered workflow exists; this step makes no such check. Many projects run CI on pull requests only, and for them no run on the merge commit is the honest, expected outcome. `wait-for-ci.js` waits out a 90-second registration window before returning `no_runs`, so a late-registering push run is still gated; the notice keeps the absence visible.

**Scoped to the SHA, never the branch.** `--branch main` would let any other `main` run supply the verdict; `--sha` gates exactly the tagged commit. Any wait on the release branch's CI ran before the merge and says nothing about the merge commit.

<!-- USER-EXTENSION-START: pre-tag -->
### Important Rules

1. **NEVER skip CI verification** - Always wait for green CI
2. **NEVER auto-create tags** - Always get user confirmation
3. **NEVER guess version numbers** - Base on actual commit analysis
4. **ALWAYS show changes before committing** - User must approve
5. **NEVER declare release complete after pushing tag** - Monitor until assets uploaded
6. **ALWAYS verify release assets exist** - Run `gh release view` to confirm
<!-- USER-EXTENSION-END: pre-tag -->

### Step 4.5: Remove Active Label

```bash
node .claude/scripts/shared/lib/active-label.js remove [TRACKER_NUMBER]
```

### Step 4.6: Tag and Push

**ASK USER:** Confirm ready to tag. Print the tag, commit and merge-commit CI result as a table, then options `1.Tag and push`, `2.Stop`; question `Tag and push <version>?`; previews: *Tag and push* creates the annotated tag and pushes it, starting the distribution deploy; *Stop* creates no tag.

```bash
git tag -a $VERSION -m "Release $VERSION"
echo "$VERSION | gates passed | $(git rev-parse HEAD)" > .release-authorized
git push origin $VERSION; rc=$?
rm -f .release-authorized
exit $rc
```

**Note:** `.release-authorized` is the marker `.claude/hooks/pre-push` requires before allowing a `v*` tag push. The hook only tests existence and echoes contents verbatim, so this line becomes the release audit record. Cleanup is unconditional — the exit code is captured **before** `rm -f` and propagated after. A plain echo/push/rm sequence is not sufficient: a failed push aborts before the `rm`, and the surviving marker authorizes the next tag push with no gate having run.

### Step 4.7: Wait for CI on the Pushed Tag

**Conditional:** the wait is only meaningful if some workflow triggers on a tag.

```bash
grep -lE '^[[:space:]]*tags:' .github/workflows/*.yml .github/workflows/*.yaml 2>/dev/null
```

**If nothing matches:** skip with a message naming the reason: `No tag-triggered workflows detected — skipping post-tag CI wait.`

**If a tag-triggered workflow exists:**
```bash
node .claude/scripts/shared/wait-for-ci.js --branch $VERSION --timeout 900
```
**If the run fails or times out, STOP and report** its URL and conclusion. Do not continue to Step 4.8 — the tag is already pushed, so this is the last point at which a broken publish can be caught before the release is announced.

**On exit 3 (`no_runs` / `no_executing_runs`): report "expected run not found" and STOP.** Do not continue. Past the trigger check a tag-triggered workflow is known to exist, so exit 3 means the gate examined nothing, not that there was nothing to gate on. Report the payload's `status`, `waitedMs` and the tag; check `gh run list --branch $VERSION` by hand before re-running this step (#2952).

**Why STOP here when other callers continue (#2952):** `/work` Step 1a and `/prepare-beta` never establish that a run must exist, so exit 3 there is an honest "nothing to wait for". `wait-for-ci.js` waits out a registration window before `no_runs` (re-polls every 15s for up to 90s), so a run registered seconds after the push is gated normally and a surviving `no_runs` is a real absence, not a race. On v0.104.0, before the window, the wait declared `no_runs` seconds before `Deploy to Distribution Repo` registered.

**Scope the wait to the TAG, not the checked-out branch (#2653).** This step used to resolve the branch name at runtime and pass that. After Step 4.4 the checked-out branch is `main`, and a tag-triggered run carries the **tag name** as its `headBranch` — so `matchesFilter` rejected the run the tag had just created, and the newest `main` run supplied the verdict: the already-completed post-merge `Tests` run. The gate reported a pass without waiting for anything, **vacuous in exactly the projects that have tag-triggered workflows**. `$VERSION` is the tag, so the existing filter matches with no change to `selectRun()`. (The defective form is described, not quoted: a guard test rejects that literal anywhere in this step.)

**`--branch` or `--sha` is still not optional (#2464).** Accepted scopes: `--branch <name>` and `--sha <commit>`. Every `wait-for-ci.js` call here passes one. `--sha` is a deliberate scope, not a regression: the post-merge gate waits on a merge commit on `main`, which has no release branch name to filter by, and a commit filter is narrower than any branch filter — it closes the same repo-wide hole (#3255, #3312). Bare, the gate passes a null filter and `selectRun()` returns the newest run **repo-wide** — an unrelated run can supply the verdict. **`--timeout 900`** states the 15-minute budget explicitly rather than relying on adaptive extension of the 300s default (#2257) — it is `wait-for-ci.js`'s `MAX_CAP`, so the whole request is enforced; a larger one is clamped and reported (#3022).

**Why the trigger check, not a file-existence check (#2653).** The old conditional asked whether *any* workflow file exists. Almost every project has one, so it passed nearly always and sent the step on to wait against a filter that could not match — a vacuous pass dressed as a gate. Whether anything triggers on tags is the question that decides whether waiting is meaningful. The `grep` is a deliberate heuristic that errs toward waiting: over-waiting costs a timeout that reports honestly; under-waiting is the defect this step just had.
### Step 4.8: Update Release Notes

```bash
node .claude/scripts/shared/update-release-notes.js
```

<!-- USER-EXTENSION-START: post-tag -->
### Monitor Release Workflow

```bash
node .claude/scripts/close-release/monitor-release.js
```

The script monitors the tag-triggered workflow and verifies all platform binaries are uploaded:
- darwin-amd64, darwin-arm64
- linux-amd64, linux-arm64
- windows-amd64.exe, windows-arm64.exe
- checksums.txt

**If assets are missing after timeout, report warning but continue.**

### Clean Up Old Release Assets

```bash
node .claude/scripts/shared/cleanup-release-assets.js --keep 3 --dry-run
```

Options:
- `--keep <n>` - Releases to keep assets for (default: 3)
- `--dry-run` - Preview without deleting

**Preview first with `--dry-run`.**

### Post-Release Reminder

**Releasing code does NOT close related issues.**

Issues included in this release still require explicit user approval ("Done") to close.
Do NOT auto-close issues just because they shipped.
<!-- USER-EXTENSION-END: post-tag -->

## Summary Checklist

**Core (Before tagging):**
- [ ] Commits analyzed
- [ ] Version confirmed
- [ ] CHANGELOG updated
- [ ] Branch diff review passed (or `--skip-review`)
- [ ] PR merged

<!-- USER-EXTENSION-START: checklist-before-tag -->
- [ ] Coverage gate passed (or `--skip-coverage`)
- [ ] CI passing
<!-- USER-EXTENSION-END: checklist-before-tag -->

**Core (After tagging):**
- [ ] Tag pushed
- [ ] CI workflow completed — **check this only on Step 4.7's verdict**: a passing tag-scoped `wait-for-ci.js` run, or its explicit "no tag-triggered workflows" skip. Ticking it by convention is what this wording exists to prevent
- [ ] Release notes updated

<!-- USER-EXTENSION-START: checklist-after-tag -->
- [ ] All CI jobs completed
- [ ] Release assets uploaded
- [ ] Release notes updated
<!-- USER-EXTENSION-END: checklist-after-tag -->

<!-- USER-EXTENSION-START: pre-close -->
<!-- USER-EXTENSION-END: pre-close -->

## Phase 5: Close & Cleanup

**ASK USER:** Confirm deployment verified and ready to close release. Print the deploy and CI results as a table, then options `1.Close release`, `2.Stop`; question `<version> deployment verified: close the release?`; previews: *Close release* comments on the tracker, deletes the working branch and verifies the GitHub release; *Stop* leaves the branch and tracker as they are.

### Step 5.1: Add Deployment Comment

```bash
gh issue comment [TRACKER_NUMBER] --body "Release $VERSION deployed successfully"
```

### Step 5.2: Delete Working Branch

```bash
git push origin --delete $BRANCH
git branch -d $BRANCH
```

### Step 5.3: Verify GitHub Release

Check if the GitHub release already exists (Step 4.8 may have created it):

```bash
gh release view $VERSION
```

- **If release exists:** Report `"GitHub release $VERSION already exists (created by Step 4.8). Skipping creation."`
- **If release does not exist:** Create it:

```bash
gh release create $VERSION \
  --title "Release $VERSION" \
  --notes-file CHANGELOG.md
```

<!-- USER-EXTENSION-START: post-close -->

<!-- USER-EXTENSION-END: post-close -->

## Summary Checklist (Close)

<!-- USER-EXTENSION-START: checklist-close -->
- [ ] Tracker issue closed
- [ ] Release closed in project
- [ ] Working branch deleted
- [ ] GitHub Release created
<!-- USER-EXTENSION-END: checklist-close -->

## Completion

Release $VERSION is complete:
- Code merged to main
- Tag created and pushed
- Deployment verified
- Tracker issue closed
- Working branch deleted
- GitHub Release created
### Step 6: Closing Cleanup
The prune is **part of** this step, and this step is **numbered** — what makes the claim hold. `One task per numbered step` now covers it, so an unpruned list surfaces as an unfinished task like any other step. The same claim as prose alone was overridden by the rules beside it (#2641).

**Prune the task list** (unconditional — every path, including early-exit paths where Phase 1 created tasks and later phases never ran):
1. `TaskList` — enumerate all tasks.
2. For every task owned by this `/prepare-release` invocation, `TaskUpdate status=deleted`.
3. Do **not** delete tasks created outside this invocation (user TODOs).

**End of Prepare Release**
