---
version: "v0.99.0"
description: Tag beta from feature branch (no merge to main)
argument-hint: "[--skip-coverage] [--dry-run] [--help]"
copyright: "Rubrical Works (c) 2026"
---

<!-- EXTENSIBLE -->
# /prepare-beta
Tag a beta from a feature branch without merging to main.
**Extension Points:** `.claude/metadata/extension-points.json` or `/extensions list --command prepare-beta`
---
## Arguments
| Argument | Description |
|----------|-------------|
| `--skip-coverage` | Skip coverage gate |
| `--dry-run` | Preview without changes |
| `--help` | Show extension points |
---
## Execution Instructions
**REQUIRED:** `TaskCreate` one task per numbered phase/step and one per non-empty `USER-EXTENSION` block; skip commented-out extensions. Mark `in_progress` -> `completed`. **Post-Compaction:** re-read spec, `TaskList`, resume from first incomplete task.
---
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

**ASK USER:** Confirm beta version before proceeding.
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

**ASK USER:** Confirm validation passed before proceeding.
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
**ASK USER:** Confirm ready to tag beta.
```bash
git tag -a $VERSION -m "Beta $VERSION"
echo "$VERSION | gates passed | $(git rev-parse HEAD)" > .release-authorized
git push origin $VERSION; rc=$?
rm -f .release-authorized
exit $rc
```
**Note:** tags the feature branch. No merge to main.
**Note:** beta tags are `v*` prereleases, so `.claude/hooks/pre-push` gates them exactly as release tags — the marker is required here too. The hook only tests existence and echoes contents verbatim, so this line becomes the beta audit record. Cleanup is unconditional — the exit code is captured **before** `rm -f` and propagated after, so a failed push cannot leave a marker that silently authorizes the next tag push.
### Step 4.3: Wait for CI Workflow
**Conditional:** `ls .github/workflows/*.yml .github/workflows/*.yaml 2>/dev/null` first. **No workflow files found:** skip the CI wait, reporting `No CI workflows detected — skipping CI wait.` **Workflow files exist:**
```bash
node .claude/scripts/shared/wait-for-ci.js
```
**If CI fails, STOP and report.**
### Step 4.4: Update Release Notes
```bash
node .claude/scripts/shared/update-release-notes.js
```

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
