---
version: "v0.103.0"
description: Dedicate a session to observing cross-session activity (project)
argument-hint: "[--auto-create] [--force]"
copyright: "Rubrical Works (c) 2026"
---
<!-- MANAGED -->
# /hall-monitor
Observes cross-session activity: consumes the lifecycle announcements peers broadcast, correlates them against local commits, reports what no single working session can see.
**MANAGED, no `USER-EXTENSION` blocks** — no per-project customization surface; every tunable value is data in `.claude/metadata/hall-monitor-signals.json` (#2746 reasoning).
## Prerequisites
`gh pmu` + `.gh-pmu.json` (only for `--auto-create`); messaging discoverable — see **Degradation**.
## Arguments
| Argument | Required | Description |
|----------|----------|-------------|
| `--auto-create` | No | File bugs automatically for filable findings, and offer enhancements. **Opt-in; off by default.** Absent, findings are reported and nothing is filed. |
| `--force` | No | Start even when a live marker names another monitor, displacing it. **Required on win32**, where liveness is pid-existence only so a recycled pid reads as live. No value. |
## What This Command Can and Cannot See
**This section is the contract: a monitor implying wider coverage than it has is worse than none, because a reader stops looking.**
| Source | Observable? |
|---|---|
| Lifecycle announcements addressed to this session | **Yes** — the whole vocabulary below |
| Local git state (`git log`, `git status`, working tree) | **Yes** |
| A direct `SendMessage` between two **other** sessions | **No** |
| Another working directory, machine, or user | **No** |
**Direct messages between other sessions are point-to-point and unobservable** — no bus, no log, no tap. Say so rather than presenting the analysis as complete coverage.
**No new transport is required, and none should be added.** `peer-announce.js` `resolveRecipients()` dispatches to **every addressable discovered peer**, so a discoverable idle session already receives all eleven events. The gap this fills is a **consumer**, not a channel.
**Events consumed** — full vocabulary, no subset: `work-started`, `work-completed`, `push-started`, `ci-terminal`, `ci-resolved`, `push-rejected`, `review-started`, `review-resolved`, `review-passed`, `review-findings`, `fixtures-provisioned` — the last (#2827) names scratch board issues `/qa` provisioned for a manual check; a later `work-started` on one of those numbers is expected, not a collision.
**The monitor emits no announcements of its own.** It performs no work, so emitting adds noise to every peer's inbox for no signal.
## Run Model
**A self-paced `/loop`, not a new scheduler.** Wake on inbound announcements; long fallback for git polling (`observation.gitPollFallbackSeconds`, bounded to the `ScheduleWakeup` clamp).
**Never schedule short-interval polling for harness-notified work** — announcements and background tasks re-invoke the session already, so a short tick learns nothing. The fallback covers only what the harness cannot notify: commits from another session.
## Workflow
### Step 1: Resolve Configuration
```bash
node .claude/scripts/shared/lib/cross-session-config.js
```
**Never re-derive the defaults inline** — six consumers share the absence rule, and the `discovery: false` group implication is what a local copy gets wrong.
### Step 1a: Claim the Presence Marker (#2769)
**Delegate the decision; do not re-derive it here.**
```bash
node -e "console.log(JSON.stringify(require('./.claude/scripts/shared/lib/hall-monitor-presence.js').decideStart(process.cwd(), {force: FORCE})))"
```
`FORCE` ← `true` when `--force` was passed, else `false`. Returns `{proceed, reason, message}`.
**Session identity travels in `CLAUDE_PID`, inherited — do not substitute it (#2795).** `decideStart` resolves the session pid from the environment, the same source `peers-check.js` uses to recognise itself. It must **never** be `process.pid`: inside `node -e` that is the node child's pid, fresh every invocation and never the marker's, which made the `self` row unreachable and left a re-arming monitor refusing itself. A substituted placeholder was rejected for the same reason — an unsubstituted one fails in that direction, silently.
- `proceed: false` → **refuse to start.** Report `message` verbatim and **STOP**. Write nothing.
- `proceed: true` → write `.hall-monitor.json` at the project root with `{pid, procStart, startedAt, cwd, version}`, then continue. **`pid` is the session pid (`CLAUDE_PID`)** — the identity `decideStart` compares next tick. Writing this process's pid makes the marker read `stale-pid` immediately and defeats the `self` row.
| `reason` | `proceed` | Meaning |
|---|---|---|
| `no-marker` | yes | Nothing there; write |
| `stale-pid`, `stale-boot`, `malformed`, `cwd-mismatch` | yes | **Overwrite. Never refuse** |
| `self` | yes | This **session's** own marker — the same monitor re-arming, not a second one. Reachable only because `CLAUDE_PID` carries the session pid into the subprocess |
| `live` | **no** | A different monitor is running here; `message` names its pid, `startedAt` and `--force` |
| `live` with `--force` | yes | Overwrite, naming the pid displaced |
| `self-pid-unresolved` | **no** | A marker is live but `CLAUDE_PID` is unset or unreadable, so this session cannot tell whether it is its own. Its own reason, never folded into `live` |
**Refusal is scoped to `live` and `self-pid-unresolved`, and must stay that way.** The helper never deletes, so this overwrite is the **only** cleanup path a stale marker has; if any stale reason could refuse, one crashed monitor would lock out every future monitor in this directory permanently — `malformed` being the sharpest case, where a truncated write becomes an unrecoverable lock.
**`self-pid-unresolved` refuses but is not a `live` verdict and must not be reported as one.** Treating it as `self` starts a second monitor on a guess; treating it as `live` blames a foreign monitor that may not exist and points at `--force`, which is not the remedy for meeting your own marker. `--force` still displaces the marker in this state, being the remedy for a foreign marker and a recycled win32 pid regardless of whether identity was established.
**Writing a file is not an announcement.** #2768's *emits nothing* criterion is preserved: nothing is sent to any peer.
**Under `discovery: false` the marker is still written** — the monitor receives nothing, so quiet narration of announcements that never arrive is harmless.
### Step 2: Establish the Baseline
Record current `HEAD` and the open issues in flight; everything reported later is a delta against it.
### Step 3: Observe
Hold received announcements. Each tick, read commits since the last:
```bash
git log --oneline <last-tick-sha>..HEAD
```
### Step 4: Correlate and Report
Compare announced against landed. Finding kinds, severities and rationales are **data** — re-read `.claude/metadata/hall-monitor-signals.json` from disk at use (rule `01-anti-hallucination.md`).
**Re-validate before reporting.** Observations go stale inside one tick: an issue read as `in_review` can be `Done` ninety seconds later, closed by a session that never announced it. Re-check any issue state a finding rests on immediately before reporting, and say so when it could not be re-confirmed.
Severity `report` findings are never filed — they name a risk, not a defect.
### Step 5: Auto-Create (`--auto-create` only)
**Opt-in; absent the flag this step does not run.** Only `filable` **and** `autoCreatable` findings qualify.
**Two guards bound this step, both metadata-driven — thresholds in `hall-monitor-signals.json`, tunable without a spec edit:**
| Guard | What it stops |
|---|---|
| **dedupe** (`autoCreate.dedupe`) | A **standing** condition re-derived every tick becoming one issue per tick. The fingerprint excludes `detail` — the part most likely reworded between ticks while the finding is the same one. |
| **rate limit** (`autoCreate.rateLimit`) | An analysis that goes wrong filing an unbounded number of issues. The cap is the blast radius. |
```javascript
const { evaluateAutoCreate } = require('.claude/scripts/shared/lib/hall-monitor-guards.js');
const verdict = evaluateAutoCreate({ finding, filings, now: Date.now(), signals });
```
`allowed: false` → report `reason` (`duplicate`, `rate-limited`, `signals-unreadable`), file nothing. **Refusal is reported, never silent** — an unexplained non-filing is indistinguishable from a monitor that never looked.
**Bug path — delegate to a subagent; that is what keeps the monitor responsive.** Filing is multi-step (sweep, compose, `gh pmu create`) and inline it is blocking — a monitor that stops monitoring while it files has stopped being one. Spawn a subagent to run `/bug`.
- A **prior-art sweep is mandatory**; an `already-shipped` verdict **skips** the filing. Duplicate filing is a known failure here, and an unattended filer has no human check.
- Auto-filed issues carry the `auto-filed` label (`autoCreate.label`) — recognisable and reversible.
- **The subagent runs degraded and must say so:** `framework-dev` has no `TaskCreate`/`TaskList`, so `/bug` falls back to rule `07-task-creation-timing.md`'s inline checklist and the `TaskList` compaction-recovery guarantee does not hold.
**Enhancement path — stays in this session; it cannot be delegated.** Same tool boundary: `framework-dev` has **no `AskUserQuestion`**, so a subagent cannot make an offer at all. Bug filing is non-interactive and delegable; an offer is not. **Never file an enhancement unattended.**
### Step 6: Continue or Stop
Schedule the next tick, or stop when the user says so. One line per tick; quiet ticks collapse.
**Release the marker on stop.** Remove `.hall-monitor.json` **only when its `pid` matches this process**; leave a foreign marker untouched. This closes the interleave `--force` allows: without the ownership test, monitor A exiting would delete a marker monitor B had overwritten, leaving B live and unmarked.
**Abnormal termination leaves a stale marker by design.** No crash hook, and none proposed: the next monitor's start-time overwrite is the recovery, and the startup `Peers:` row names the stale marker meanwhile.
## Degradation
| Condition | Behaviour |
|---|---|
| `crossSessionMessaging.discovery` false | Not discovered, **receives no announcements**. Degrade to **git-only observation** and report that plainly — a quiet channel and an unwatched one are otherwise indistinguishable, and only one is fine. |
| `enabled` false | Same; cause reported as `crossSessionMessaging.enabled: false` — the key the user set, not the one false downstream. |
| No peers discovered | Normal. Report git-derived findings only. |
| `hall-monitor-signals.json` missing/unreadable | Report the gap, continue report-only. `--auto-create` **refuses** (`signals-unreadable`); failing open leaves an unbounded filer. |
## Error Handling
| Situation | Response |
|-----------|----------|
| `gh` unavailable | Report; git-derived findings continue. Never halt the loop. |
| A guard helper throws | Cannot happen by construction; if it did, report and continue. |
| Subagent filing fails | Report the failure **and** the finding, so it is not lost with the attempt. |
| `--auto-create`, no filable findings | Nothing to do. Do NOT lower the bar to produce one. |
**End of /hall-monitor Command**
