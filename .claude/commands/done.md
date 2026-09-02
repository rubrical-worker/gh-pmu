---
version: "v0.99.0"
description: Complete issues with criteria verification and status transitions (project)
argument-hint: "[#issue... | --all] [--yes|-y] (optional)"
copyright: "Rubrical Works (c) 2026"
---
<!-- EXTENSIBLE -->
# /done
Move issues from `in_review` → `done` with a STOP boundary. Final transition only — `/work` owns `in_progress` → `in_review`.
**Extension Points:** `/extensions list --command done`

## Prerequisites
- `gh pmu` installed, `.gh-pmu.json` configured
- Issue in `in_review` (use `/work` first)

## Arguments
| Argument | Description |
|---|---|
| `#issue` | Single issue (`#42` or `42`) |
| `#issue #issue...` | Multiple |
| `--all` | All `in_review` on current branch (with confirmation) |
| `--yes` / `-y` | Auto-approve interactive prompts. With `--all`: unattended batch. Does NOT bypass safety gates (AC verification, force-move prohibition). |
| *(none)* | Query `in_review` issues for selection |

## Execution Instructions
**REQUIRED:** Routed command — two-phase task creation. Phase 1: one preamble task. Phase 2 (after preamble confirms no redirect/early exit): bulk-create remaining tasks. Redirect/early exit → mark preamble complete, stop. For each non-empty `USER-EXTENSION`, add a Phase 2 task. Track `in_progress` → `completed`. Post-compaction: re-read spec, resume from first incomplete task — no re-routing.
---
## Workflow
### Step 1: Context Gathering (Preamble Script)
Consolidates validation, diff verification, status transition, tracker linking, CI pre-check.

```bash
node .claude/scripts/shared/done-preamble.js --issue $ISSUE          # single
node .claude/scripts/shared/done-preamble.js --issues "$ISSUE1,$ISSUE2"  # multiple
node .claude/scripts/shared/done-preamble.js                         # discovery
```

Parse JSON, check `ok`:
- **`ok: false`:** report `errors[]` (`code`, `message`, optional `suggestion`) → **STOP**
- **Discovery** (`discovery` field): `mode: 'query'` (no-args) — present `discovery.issues` for user selection, re-run with `--issue N`. `mode: 'all'` (`--all`) — present list, ask "Complete all N in_review issues?"; yes → re-run with `--issues` for all numbers (deferred push: single push after last); empty list → "No in_review issues on current branch", STOP. **`yes: true` in envelope** (`--yes`/`-y`): SKIP the prompt, re-run with `--issues` for all (pass `--yes` through). `query` mode still requires user selection.
- **`ok: true` + `diffVerification`:** `requiresConfirmation: true` → report `warnings`, ask "Continue? (yes/no)"; yes → re-run with `--force-move`; no → **STOP**. **`yes: true` in envelope:** SKIP the prompt, re-run with `--force-move` (pass `--yes` through); still report warnings for audit. `requiresConfirmation: false` → already moved to done, proceed.

**Safety gates under `--yes`:** suppresses interactive prompts only. Does NOT bypass AC verification, force-move prohibition (`/work` Step 4b), `gh pmu` errors, or any failure halt — all halt as usual regardless of `--yes`.

- **`ok: true` + `gates.movedToDone: true`:** report `Issue #$ISSUE: $TITLE → Done`. `context.trackerLinked: true` → `Linked #$ISSUE to branch tracker #$TRACKER`. `context.nextSteps` present → report `context.nextSteps.guidance` (approval-gate steps, e.g., `/review-prd` before `/create-backlog`).

Report any `warnings[]` (non-blocking).

**Multiple issues:** process each through Step 1 sequentially; execute Steps 2–3 once after the last (batch push). Count total at start, track position.

### Step 1a: Epic Detection (epic completion flow)
After preamble succeeds for a single issue, check `context.issue.labels` for `epic`. Not an epic → skip to Step 2.

**Epic detected:** `gh pmu sub list $ISSUE`, then classify:

| Sub-Issue Status | Action |
|---|---|
| `done` | Skip — already complete |
| `in_review` | Queue for done processing |
| `in_progress` | **Warn:** "Sub-issue #N is still in_progress — complete via /work first" |
| `backlog`/`ready`/other | **Warn:** "Sub-issue #N is in {status} — was never started" |

All `done` → skip processing, proceed to epic. `in_review` exist → process each through standard `/done` (Steps 1–3); per-sub-issue `Sub-issue #N: $TITLE → Done (M/T processed)`; push deferred until after epic. Then run preamble for the epic itself. Final report:
```
Epic #$ISSUE: $TITLE — Done
  Sub-issues completed: N
  Sub-issues already done: M
  Sub-issues warned (not ready): K
  Epic: Done
```
**Push for epics:** all sub-issue + epic transitions are a single batch — push deferred until after epic completes (Step 2).

<!-- USER-EXTENSION-START: pre-done -->
<!-- USER-EXTENSION-END: pre-done -->

### Step 1b: Post Work Summary Comment
After each issue moves to done, post a summary comment IF commits referencing the issue exist. `git log --all --oneline --grep="Refs #$ISSUE\|Fixes #$ISSUE\|Closes #$ISSUE"`. No commits → skip (no-op close). Otherwise: get latest SHA + `git diff --name-only $FIRST_COMMIT~1..$LATEST_COMMIT`, construct repo URL from `.gh-pmu.json` `repositories[0]`, post comment via `-F` containing `**Work completed:**` heading, a `Files changed:` bulleted list of backticked paths, and a `Commit: https://github.com/{owner}/{repo}/commit/{sha}` URL line (multiple commits → link latest). **Non-blocking:** comment failure → log warning, continue.

### Step 2: Push (Batch-Aware)
Not last in batch → skip push → `"Push deferred (N remaining)"`. Single issue OR last in batch — four sub-steps, in order. The sync guard (#2635) sits between the no-commit check and the push: when another developer pushed this branch first, a bare push is rejected non-fast-forward and the only spec-less recovery is `--force`, which destroys their commits.
**2.1 No-commit detection:** `git log @{u}..HEAD --oneline` empty → `"Nothing to push"`, skip to Step 3.
**2.2 Fetch and pin the remote tip:**
```bash
node .claude/scripts/shared/branch-sync-check.js
```
Read `data.status` and `data.upstreamSha` — the post-fetch `@{upstream}`, the last commit the remote already has. Persist it for Step 3 so it survives compaction: `echo "$UPSTREAM_SHA" > .tmp-push-base-$ISSUE.txt`. `fetched: false` → status and SHA came from the cached ref: say so, continue — the push in 2.4 is the authoritative check. `success: false` → warn, write an empty file, continue.
**2.3 Act on `status`:**
| `status` | Action |
|---|---|
| `ahead` | Continue to 2.4. |
| `diverged` | Another developer pushed while this branch was being worked. **Rebase, not merge:** `git rebase @{u}`. Clean → 2.4. Conflict → capture the paths with `git diff --name-only --diff-filter=U`, then `git rebase --abort`, report the paths, **STOP** — the issue is already Done on the board from Step 1; say so, and that the commits remain local. Recovery by hand: `git rebase @{u}`, resolve, `git rebase --continue`, `git push`. |
| `up-to-date`, `behind` | Nothing local to push (2.1 catches this; reaching here means the ref moved between the calls). Report, skip to Step 3 as "Nothing to push". |
| `no-upstream` | Continue to 2.4; git's own push error is the report if none is configured. |
**Why rebase, not merge:** the per-AC `Refs #N` commits are what `scope-drift-check.js`, `log-changed-files.js`, and `nonstop-audit.js` key off — they match on message, not SHA. A merge commit adds an unattributed commit; a rebase keeps the set intact. Step 4f ran on the pre-rebase tree; Step 3's CI monitor is the check that the rebased tree still passes.
**2.4 Push:**
```bash
git push
```
Report `Pushed.` Rejected (non-fast-forward — a push landed between 2.2 and now) → report git's error verbatim and **STOP**; `/done` cannot re-run for a closed issue, so repeat 2.2–2.4 by hand. **NEVER `git push --force` or `--force-with-lease` at this step** — both overwrite the other developer's commits; a rejection means the guard must be repeated, not overridden.

**Only execute after push (Step 2 actually pushed).** If push was deferred (not last in batch) or skipped (nothing to push), skip this extension — same contract as Step 3. Unguarded, a batch fires it once per issue with nothing pushed.

<!-- USER-EXTENSION-START: post-push -->
<!-- USER-EXTENSION-END: post-push -->

### Step 3: Background CI Monitoring (Batch-Aware)
**Only after push (Step 2 actually pushed).** Deferred/skipped → skip CI monitoring for this issue.

`sha=$(git rev-parse HEAD)`. Check `context.ci.hasPushWorkflows`: `false` → skip, report `"CI skipped (no push-triggered workflows)"`. **Pre-check paths-ignore:** `shouldSkipMonitoring(changedFiles, pathsIgnore)` is synchronous, returns `boolean`. `pathsIgnore` from workflow YAML; `changedFiles` from the **whole pushed range** — what GitHub evaluates `paths-ignore` against: `git diff --name-only "$(cat .tmp-push-base-$ISSUE.txt)..@{u}" && rm .tmp-push-base-$ISSUE.txt`. The base is the remote tip Step 2.2 pinned after its fetch and before its push, so the range is exactly this run's push, rebased or not. Read it from the file, never a variable: Step 2's post-compaction contract carries no variables; the file does. Remove it once read. All match → skip, `"CI skipped (paths-ignore)"`.
**Fail open whenever that range cannot be resolved** — skip the pre-check, arm the monitor, report the degradation. Guard the general condition, not a list; known causes: a branch's first push (`upstreamSha` null → empty file), no configured upstream, missing file (Step 2 never reached 2.2). **No `HEAD~1` fallback** — it reinstates the defect exactly where it would fire. Unnecessary monitor is harmless; a missed failure is not.
**Why not the tip commit:** `/work` commits per AC and defers push to `/done`, so even a single-issue `/done` pushes several commits and `HEAD~1` sees only the last. A docs-only tip → skip reported, no monitor armed, real CI failure never surfaced.
**Why a pinned file, not the reflog (#2635):** the reflog's previous ref value is this run's push only while nothing else moves the ref in between — and a push by **any other process sharing the same `.git`** (second agent session, terminal, editor integration) does. (Step 2.2's own fetch does not break it: it moves the ref to exactly the base wanted, or finds nothing new and leaves no entry.) The earlier trade-off accepted that fragility because a SHA in a variable did not survive compaction. A SHA in a file survives both — compaction-proof and immune to concurrent ref movement — which is why 2.2 writes one.
Otherwise spawn background (`run_in_background: true`):
```bash
node ./.claude/scripts/shared/ci-watch.js --sha $SHA --timeout 600
```
Report `"CI monitoring started in background."`

**Exit codes:**
| Code | Report |
|---|---|
| 0 | `"CI passed for #$ISSUE (duration)"` |
| 1 | `"CI FAILED. Failed step: \"step-name\". Run: gh run view <id> --log-failed"` |
| 2 | `"CI still running after 10m. Check: gh run list --commit $SHA"` |
| 3 | `"No CI run triggered (paths-ignore likely)"` |
| 4 | `"CI cancelled (superseded by newer push)"` |

Multiple workflows → report per-workflow from `workflows[]`.

<!-- USER-EXTENSION-START: post-done -->
<!-- USER-EXTENSION-END: post-done -->

### Step 4: Cleanup
**MUST DO:** Clear task list.
---
## Peer Announcements (#2663)
Tells other sessions in this working directory that a push is happening, then how it resolved. Peers from `peers-check.js`, payloads from `peer-announce.js`, delivery by `SendMessage`.
**Take `data.peers`, from the CLI (#2678)** — same access as `/work` Steps 3 and 6:
```bash
node .claude/scripts/shared/peers-check.js
```
```javascript
const { buildAnnouncement, EVENTS } = require('.claude/scripts/shared/peer-announce.js');
const a = buildAnnouncement({ event: EVENTS.PUSH_STARTED, issues, peers });   // peers === envelope.data.peers
```
**The two shapes are not interchangeable and picking wrong fails silently.** `checkPeers()` returns `peers` at the **top level**; the CLI wraps it as **`data.peers`**. Requiring the module and reading `data.peers` yields `undefined`, and an `|| []` beside it makes that a genuine empty array before `buildAnnouncement` sees it — after which nothing downstream can tell the mistake from an empty working directory. This section previously named the helper and not the shape, and the reported incident came from this path.
**A non-array `peers` is now named as such** rather than reported as empty, naming `data.peers`. That guard cannot see an empty array a caller manufactured — hence the shape written here as well as enforced there.
| Event | When |
|---|---|
| 3 — push started | Step 2, **immediately before** `git push`, **once per push** — not once per issue in a deferred batch |
| 4 — terminal | Step 3, at **arming time**, emitted by `/done` |
**Event 3 fires only when a push occurs** — Step 2.1 finding nothing to push emits nothing and arms no watch.
**Every event 3 is followed by exactly one terminal event, on every path.** Both CI skips (`no push-triggered workflows`, `paths-ignore`) are terminal and emitted by `/done`, the only emitter — `ci-watch.js` is never launched on those paths.
**The armed-monitor event is terminal too**, carrying the run URL and stating that no further announcement will follow. Consequence of the #2660 refutation: `ci-watch.js` is neither slash command nor hook so cannot call `SendMessage`, and the raw-socket send was refuted (six shapes accepted, none delivered). Nothing follows, so the event says so rather than leaving a peer waiting forever. Step 3 failing open over an unresolved range marks the payload **degraded**, reading differently from a clean one.
**A rejected push emits a correction** to the same peers, stating the commits remain local. `/done` is never made to wait for CI; `wait-for-ci.js` is never invoked from the announcement path.
**No receiving session runs git** — no pull offer, no `branch-sync-check.js` delegation, no working-tree mutation on receipt. **No announcement asserts a peer is "behind"**: a shared `HEAD` and index make that unreachable, and `branch-sync-check.js` reports `ahead` before the push and `up-to-date` after.
**Advisory, never a gate.** A helper that throws inside Step 2 does not abort the push.
---
## Error Handling
| Situation | Response |
|---|---|
| Issue not found | "Issue #N not found." → STOP |
| Issue already closed | "Issue #N is already closed." → skip |
| Issue still in_progress | "Complete work first via /work." → STOP |
| Issue in other status | "Move to in_progress first via /work." → STOP |
| No issues in review | "No issues in review." → STOP |
| `gh pmu` fails | "Failed to update issue: {error}" → STOP |
---
**End of /done Command**
