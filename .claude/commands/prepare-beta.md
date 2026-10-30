---
version: "v0.115.0"
description: Tag a beta from a feature branch without merging to main, using the IDPF framework.
argument-hint: "[--skip-coverage] [--skip-review] [--dry-run] [--help]"
copyright: "Rubrical Works (c) 2026"
---

<!-- EXTENSIBLE -->
# /prepare-beta
Tag a beta from a feature branch without merging to main.
**Extension Points:** `.claude/metadata/extension-points.json` or `/extensions --list --command prepare-beta`
---
## Arguments
| Argument | Description |
|----------|-------------|
| `--skip-coverage` | Skip coverage gate |
| `--skip-review` | Skip the branch diff review gate (Step 2.1); skip recorded in the Summary Checklist |
| `--dry-run` | Preview without changes; the Step 2.1 review gate reports what it would review and runs no review |
| `--help` | Open this command's docs page. Takes precedence over every other argument and performs no action. |

**`--help` takes precedence over every other argument and performs no action:** run `node .claude/scripts/shared/command-help.js prepare-beta`, report its output, and STOP.

---
## Execution Instructions
**REQUIRED:** `TaskCreate` one task per numbered phase/step and one per non-empty `USER-EXTENSION` block; skip commented-out extensions. Mark `in_progress` -> `completed`. **Post-Compaction:** re-read spec, `TaskList`, resume from first incomplete task.
---
**Prompt shape (#3233).** Every **ASK USER** below follows `{frameworkPath}/Reference/AskUserQuestion-Shape.md`: context first as markdown (results as a table), an options table keyed `{n}.{label}`, a one-line question, unprefixed labels, and for single-select a `preview` per option saying what it does; `ask-question-shape-hook.js` blocks only the tool's hard limits (4 options, 12-char header); the rest is convention.

## Pre-Checks
### Verify NOT on Main
```bash
BRANCH=$(git branch --show-current)
if [ "$BRANCH" = "main" ]; then
  echo "Error: Cannot create beta from main."
  exit 1
fi
```
---

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
JSON: `lastTag`, `commits`, `summary`. `summary` counts **conventional-commit prefixes only** — an unprefixed commit lands in `other` whatever it did.
### Recommend Version
```bash
node .claude/scripts/shared/recommend-version.js --prerelease
```
`--prerelease` is REQUIRED — without it the recommendation is a **stable** version, wrong to tag from an unmerged branch. `--prerelease=rc` picks another identifier (default `beta`). Read `warnings` in the output.
| Last tag | Recommends |
|----------|------------|
| `v0.20.0-beta.4` | `v0.20.0-beta.5` — counter advances, core held |
| `v0.20.0-alpha.3` | `v0.20.0-beta.1` — new identifier, core held |
| `v0.20.0` (stable) | `v0.21.0-beta.1` — core bumped by commit analysis, line opens |
**The two scripts classify differently by design.** `analyze-commits.js` counts prefixes only; `recommend-version.js` also resolves issue labels (`enhancement`/`story`/`epic` → feature) then keywords, so `feat: 0` beside `reason: "new feature(s)"` is expected, not a contradiction. Trust the recommendation.

<!-- USER-EXTENSION-START: post-analysis -->
### Analyze Commits

```bash
node .claude/scripts/shared/analyze-commits.js
```

### Recommend Version

```bash
node .claude/scripts/shared/recommend-version.js
```

Recommend beta version (e.g., `v1.0.0-beta.1`).

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

**ASK USER:** Confirm beta version before proceeding. Phase 1 analysis table first; options `1.Proceed`, `2.Stop`; question `Proceed with beta <version>?`; previews: *Proceed* → Phase 2; *Stop* → ends, nothing changed.
---
## Phase 2: Validation

<!-- USER-EXTENSION-START: pre-validation -->
### Lint Gate

```bash
node .claude/scripts/prepare-release/lint.js
```

The script outputs JSON: `{"success": true/false, "message": "..."}`

**If `success` is false, STOP and report the error.**

Runs `golangci-lint run --timeout=5m` to catch lint errors before tagging.
<!-- USER-EXTENSION-END: pre-validation -->

<!-- USER-EXTENSION-START: post-validation -->
### Coverage Gate (Optional for Beta)

**If `--skip-coverage` was passed, skip this section.**

```bash
node .claude/scripts/prepare-release/coverage.js
```

**If `success` is false, STOP and report the error.**

### E2E Gate (Optional for Beta)

**If `--skip-e2e` was passed, skip this section.**

```bash
node .claude/scripts/e2e/run-e2e-gate.js
```

The script outputs JSON: `{"success": true/false, "testsRun": N, "testsPassed": N, "duration": N}`

**If `success` is false, STOP and report the error.**

E2E tests validate complete workflows against the test project.
<!-- USER-EXTENSION-END: post-validation -->

### Step 2.1: Branch Diff Review Gate
Refuses a beta (a published tag users install) with an unreviewed change or an introduced high-severity finding. Runs before Phase 3 edits CHANGELOG and Phase 4 tags; the not-on-main Pre-Check already passed, so never on `main`.
- `--skip-review` → report `Review gate skipped (--skip-review).` and continue, even with `--dry-run`; the reference below is not read.
- Otherwise read `{frameworkPath}/Reference/code-review-execution.md` and execute its § Release Review Gate with `<command>` = `prepare-beta` — the whole gate (base, empty diff, `lastDiffReview` freshness, inline `/code-review --diff` on a missing/stale record, `--dry-run`, severity policy), shared with `/prepare-release`.
- The file cannot be read → **STOP** with `Review gate could not read {frameworkPath}/Reference/code-review-execution.md; /prepare-beta stopped.` The gate never passes unread.

**ASK USER:** Confirm validation passed before proceeding. Validation and review-gate table first; options `1.Continue`, `2.Stop`; question `Validation for beta <version> passed: continue to Phase 3?`; previews: *Continue* → CHANGELOG beta section; *Stop* → ends, no file edited.
---
## Phase 3: Prepare
Update CHANGELOG.md with beta section.

<!-- USER-EXTENSION-START: post-prepare -->
### Wait for CI

```bash
node .claude/scripts/shared/wait-for-ci.js
```

The script polls CI status every 60 seconds (5-minute timeout).

**If CI fails, STOP and report the error.**

<!-- USER-EXTENSION-END: post-prepare -->

<!-- USER-EXTENSION-START: pre-commit -->
<!-- USER-EXTENSION-END: pre-commit -->

---
## Phase 4: Tag (No Merge)
### Step 4.1: Commit Changes
```bash
git add -A
git commit -m "chore: prepare beta $VERSION"
git push origin $(git branch --show-current)
```

<!-- USER-EXTENSION-START: pre-tag -->

### Important Rules

1. **NEVER skip CI verification** - Always wait for green CI
2. **NEVER auto-create tags** - Always get user confirmation
3. **NEVER guess version numbers** - Base on actual commit analysis
4. **ALWAYS show changes before committing** - User must approve
5. **NEVER declare release complete after pushing tag** - Monitor until assets uploaded

### Beta Tag Authorization

The pre-push hook blocks version tags. For beta tags, authorize before pushing:

```bash
echo 'beta-authorized' > .release-authorized
git push origin $VERSION
rm .release-authorized
```
<!-- USER-EXTENSION-END: pre-tag -->

### Step 4.2: Create Beta Tag
**ASK USER:** Confirm ready to tag beta. Tag, commit and branch table first; options `1.Tag beta`, `2.Stop`; question `Tag and push beta <version>?`; previews: *Tag beta* → announce, create and push the tag (irreversible); *Stop* → no tag.
**Once confirmed, announce the tag to every peer before creating it (#2960)** — a pushed `v*` tag cannot be undone:
```bash
node .claude/scripts/shared/announce.js --event beta-starting --branch "$(git branch --show-current)" --tag "$VERSION"
```
Script composes the text (never hand-compose); **forced broadcast** — every addressable peer, live `/overwatch` included, whatever `broadcast` says — honoring only the master switch (`enabled: false`, `IDPF_X_SESSION=off`, `discovery: false`). `announcement.shouldSend` true → `SendMessage` to every `announcement.recipients` entry with `announcement.text`, then close out with the envelope's `dispatchReport` (`--dispatch-result sent|failed --ledger-id <id>`). A failed `SendMessage` is reported and recorded `failed`; tagging proceeds. `shouldSend` false → report `announcement.notice` once, continue. **Advisory, never a gate:** nothing awaits delivery, no follow-up is sent.
```bash
git tag -a $VERSION -m "Beta $VERSION"
echo "$VERSION | gates passed | $(git rev-parse HEAD)" > .release-authorized
git push origin $VERSION; rc=$?
rm -f .release-authorized
exit $rc
```
**Note:** tags the feature branch. No merge to main.
**Note:** beta tags are `v*` prereleases, so `.claude/hooks/pre-push` gates them exactly as release tags — the marker is required here too. The hook only tests existence and echoes contents verbatim, so this line becomes the beta audit record. Cleanup is unconditional — the exit code is captured **before** `rm -f` and propagated after, so a failed push cannot leave a marker that silently authorizes the next tag push.
### Step 4.3: Wait for CI on the Pushed Tag
**Conditional:** wait only if some workflow triggers on a tag:
```bash
grep -lE '^[[:space:]]*tags:' .github/workflows/*.yml .github/workflows/*.yaml 2>/dev/null
```
**If nothing matches:** skip, reporting `No tag-triggered workflows detected — skipping beta CI wait.` **If a tag-triggered workflow exists:** wait on the run the beta tag push created:
```bash
node .claude/scripts/shared/wait-for-ci.js --branch $VERSION --timeout 900
```
**If the run fails or times out, STOP and report** its URL and conclusion.
**Scope to the TAG, not the checked-out branch (#2889, #2653):** a tag-triggered run carries the tag name as `headBranch`; the checked-out branch would match the Step 4.1 branch-push run instead. **`--branch` is not optional (#2464):** bare, the newest run repo-wide supplies the verdict — and this is the only CI check on the beta path. `--timeout 900` states the 15-minute budget explicitly (#2257) — `wait-for-ci.js`'s `MAX_CAP`, so the whole request is enforced; a larger one is clamped and reported (#3022).
### Step 4.4: Update Release Notes
```bash
node .claude/scripts/shared/update-release-notes.js
```
**Relay the envelope's `warnings` array verbatim (#2950).** Non-empty → GitHub's Latest is a prerelease-shaped tag published as a full release (a beta from before prerelease flagging, #2584); the warning names the one-time manual remedy. Advisory: the release succeeded, nothing was edited, re-flagging is this project's own call. Empty → no report.
**Betas never become Latest on GitHub** — a prerelease cannot hold the Latest pointer, so a new beta missing from the Latest slot is expected on its own, not a failure.

<!-- USER-EXTENSION-START: post-tag -->
### Monitor Beta Build

```bash
node .claude/scripts/close-release/monitor-release.js
```

Monitor beta build and asset upload.

### Post-Release Reminder

**Releasing a beta does NOT close related issues.**

Issues included in this beta still require explicit user approval ("Done") to close.
Do NOT auto-close issues just because a beta shipped.
<!-- USER-EXTENSION-END: post-tag -->

---
## Next Step
For the full release: merge the feature branch to main, run `/prepare-release`.
---
## Summary Checklist
**Before tagging:**
- [ ] Not on main branch
- [ ] Commits analyzed
- [ ] Beta version confirmed
- [ ] Tests passing
- [ ] Branch diff review passed (or `--skip-review`)
- [ ] CHANGELOG updated with beta section

<!-- USER-EXTENSION-START: checklist-before-tag -->
- [ ] Coverage gate passed (or `--skip-coverage`)
<!-- USER-EXTENSION-END: checklist-before-tag -->

**After tagging:**
- [ ] Beta tag pushed
- [ ] CI workflow completed
- [ ] Release notes updated

<!-- USER-EXTENSION-START: checklist-after-tag -->
- [ ] Beta build monitored
<!-- USER-EXTENSION-END: checklist-after-tag -->
### Step 5: Closing Cleanup
The prune is **part of** this step, and this step is **numbered** — what makes the claim hold. `One task per numbered step` now covers it, so an unpruned list surfaces as an unfinished task like any other step. The same claim as prose alone was overridden by the rules beside it (#2641).

**Prune the task list** (unconditional — every path, including early-exit paths where Phase 1 created tasks and later phases never ran):
1. `TaskList` — enumerate all tasks.
2. For every task owned by this `/prepare-beta` invocation, `TaskUpdate status=deleted`.
3. Do **not** delete tasks created outside this invocation (user TODOs).

---
**End of Prepare Beta**
