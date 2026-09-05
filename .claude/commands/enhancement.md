---
version: "v0.100.2"
description: Create an enhancement issue with standard template (project)
argument-hint: "<title> [--prior-art]"
copyright: "Rubrical Works (c) 2026"
---
<!-- EXTENSIBLE -->
# /enhancement
Create a labeled enhancement issue with standard template and add to project board.
**Extension Points:** `.claude/metadata/extension-points.json` or `/extensions list --command enhancement`
---
## Prerequisites
- `gh pmu` extension installed
- `.gh-pmu.json` configured
---
## Arguments
| Argument | Description |
|----------|-------------|
| `<title>` | Enhancement title (e.g., `add dark mode`) |
| `--prior-art` | Run prior-art sweep before composing body. Absent = current behavior, no sweep. |
| `--assignee <value>` | GitHub login for the new issue. Omitted → `@me`. |
| `--target <owner/name>` | File the issue against a registered companion repository instead of this one. |

If not provided, prompt user.
---
## Execution
**REQUIRED before executing:**
1. Use `TaskCreate` for one task per step below. No routing → bulk create upfront (see rule `07-task-creation-timing.md`).
2. Include one task per active (non-empty) `USER-EXTENSION` block.
3. Mark tasks `in_progress` → `completed` via `TaskUpdate`.
4. **Post-Compaction:** re-read spec, call `TaskList`, resume from first incomplete task.
---
## Workflow
### Step 1: Parse Arguments
Extract `<title>`.
**Empty:** Ask user before proceeding.
**Special chars** (backticks, quotes): Escape for shell. On Windows, use temp file per shell safety.
**`--prior-art` token:** recognize anywhere in argument text, **remove it from the title**, set sweep flag. Absent → no sweep. Required for direct slash-command invocation (no hook runs there); on the trigger-word path `workflow-trigger.js` already strips flag-shaped tokens by **shape**, not allowlist membership (#2515). A `--` that is not flag-shaped (bare separator, `---` rule, `--` in prose) is preserved verbatim.
### Step 2: Gather Description
Extract `<body>` from args.
**IF insufficient detail**, THEN:
```
Describe the enhancement (what it does, why it's useful):
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
### Step 2b: Prior-Art Sweep (`--prior-art` only)
**Trigger:** sweep flag from Step 1. Absent the flag, skip this step entirely.
Runs **before** the body is composed, so findings change what gets written rather than annotating a body already wrong.
**Re-read `.claude/metadata/prior-art-sweep.json` from disk at use** (rule `01-anti-hallucination.md`). Surfaces, excludes, stopwords, disposition signals and body formats live only there — not restated here.
1. **Resolve surfaces:** `node -e "console.log(JSON.stringify(require('./.claude/scripts/shared/lib/prior-art-surfaces.js').resolveSurfaces(require('./.claude/metadata/prior-art-sweep.json'), process.cwd())))"`. Missing surface → skipped, not fatal. **Zero resolved is a failed sweep** — go to Error Handling, emit `PARTIAL`, do **not** report a clean result.
2. **Derive terms** per `termDerivation`: feature nouns, **and** terms from the files this change would touch. The second axis is not optional — prior work often shares no vocabulary with the request.
3. **Search** resolved surfaces honoring `excludes`; search issue history per `searchSurfaces.issueHistory`, including closed.
4. **Apply `questions`** to what the search returned.
5. **Classify** against `dispositions`; take that disposition's `action`.

| Verdict | Action |
|---|---|
| `already-shipped` | **STOP.** Create no issue. Report conflicting issue numbers and file paths. |
| `found-but-warranted` | Continue. Record `**Prior Art:**` — what exists, how this differs. |
| `none-found` | Continue. Record `noneFoundFormat` line including terms searched. |
**Two marker forms are recognised on read (#2700).** Emission is unchanged — always the bold inline form from `prior-art-sweep.json` `bodyFormat`. Detection also accepts a markdown heading:

| Form | Example | Recognised |
|---|---|---|
| Bold inline (emitted) | `**Prior Art:** found — …` | yes |
| Markdown heading | `## Prior Art` / `### Prior Art:` — any level | yes |
| Bare, neither | `Prior Art: found — …` | **no** |

Before #2700 only the bold counted, so a researched `## Prior Art` read as one nobody wrote. Authoritative: `bodyFormat.recognisedForms`, pinned to `classifyMarker` by test.


**Emit the section on every `--prior-art` invocation, including a nil result.** Presence records the sweep ran; absence means none ran. Omitting on nil makes "nothing found" indistinguishable from "nobody looked".
Use exact `bodyFormat` strings (`heading`, `noneFoundFormat`, `foundEntryFormat`, `partialFormat`) — never paraphrase, so consumers test the marker without parsing prose. `PARTIAL` is treated as equivalent to an absent marker and triggers re-sweep.
**`reviewSweep` gates this path too, in one mode (#2564).** Four values (`framework-config.json`, absent = `recommend`): `full`, `recommend`, `flag-only`, `off`. First three honor `--prior-art`; only `off` refuses. Delegate, do not re-derive:
```bash
node -e "console.log(JSON.stringify(require('./.claude/scripts/shared/lib/prior-art-marker.js').decideFlagSweep({reviewSweep:REVIEW_SWEEP})))"
```
`REVIEW_SWEEP` ← `framework-config.json` `reviewSweep` (absent = `recommend`) → `{sweep, refused, mode, message}`. `sweep:true` → run the sweep step as written. `refused:true` → **do not sweep**; report `message` verbatim, emit **no** `**Prior Art:**` section (absence reads as "no sweep ran"), continue creating the issue. Never a silent no-op — the user typed a flag and must learn why nothing happened and what to change.
Mode `off` deliberately overrides a typed flag. Earlier revisions of this spec, its sibling, and the schema all promised `--prior-art` swept regardless; #2564 inverts that on purpose, and the refusal message is what keeps the inversion honest.

<!-- USER-EXTENSION-START: pre-create -->
<!-- USER-EXTENSION-END: pre-create -->

### Step 3: Create Issue
Body template:
```markdown
## Enhancement

**Description:**
{user description or "To be documented"}

**Motivation:**
{infer from description, or "To be documented"}

**Proposed Solution:**
{infer from description, or "To be documented"}

**Scope:**
- **In scope:** {infer from description, or "To be documented"}
- **Out of scope:** {infer from description, or "To be documented"}
**Acceptance Criteria:**
- [ ] {infer from description, or "To be documented"}
```
Populate from user input where possible. Use "To be documented" only where insufficient.
**Sweep ran (Step 2b):** insert `**Prior Art:**` after `**Description:**`, using `bodyFormat` strings.
Create:
```bash
gh pmu create --title "[Enhancement]: {title}" --label enhancement --status backlog --priority p2 --assignee {assignee} -F .tmp-body.md
```

**Cross-repo filing (`--target <owner/name>`, #2665):** resolve BEFORE composing the issue — a refusal after the body is written wastes the work and tempts a retry against the wrong repo.
```javascript
const { resolveFilingTarget, resolveBoardFields, formatUnresolvedBoardFields } = require('.claude/scripts/shared/lib/companion-projects.js');
const target = resolveFilingTarget(charterContent, requestedRepo);
```
`ok:false` → report `reason` verbatim, **STOP**. Two refusals, never merged: not registered → register with `/charter update --register-proj`; registered with `fileIssues:false` → marked read-only on purpose, enable with `--register-proj`. Searchable-but-not-filable is the common case, so the second is usually correct rather than an oversight, and one combined message sends the user to the wrong remedy half the time.
`ok:true` → add `-R <owner/name>` to `gh pmu create`; all other flags unchanged.
**Board fields:** `resolveBoardFields(target.entry)`. `resolved:true` → set them. `resolved:false` → **create the issue anyway**, then print `formatUnresolvedBoardFields(repo, resolution)`, naming the unset fields and why. NEVER guess a field ID — a guess files onto the wrong board column silently, which is worse than an unset field plus a line saying so.
```bash
rm .tmp-body.md
```
**Note:** Always `-F .tmp-body.md` (never inline `--body`).
**Assignee:** substitute `{assignee}` from `node .claude/scripts/shared/lib/gh-pmu-config.js --assignee <value>` — pass the user's `--assignee` value, omit when none given. Helper returns that login, else `@me`; reads no config file. NEVER hardcode a login or drop the flag (omitted `--assignee` silently creates an unassigned issue). Unresolvable login → `gh pmu` exits 1 and creates nothing; report the error, do NOT retry without the flag.
### Step 4: Cleanup, Report, and STOP
Three parts, in order. The prune is **part of** this step, and this step is **numbered** — `One task per numbered step` now covers it, so an unpruned list surfaces as an unfinished task like any other. The halt is part (3) and lives nowhere earlier: while it sat in this step's TITLE a reader stopped at the title and never reached the prune (#2641).
**(1) Prune the task list** (unconditional — every path, including early-exit paths where Phase 1 created tasks and later phases never ran):
1. `TaskList` — enumerate all tasks.
2. For every task owned by this `/enhancement` invocation, `TaskUpdate status=deleted`.
3. Do **not** delete tasks created outside this invocation (user TODOs).
**(2) Emit the closing output:**
Report the created issue number and title, Status `Backlog`, Label `enhancement`, then the follow-on sequence: `/review-issue`, `/assign-branch`, `work` with the issue number.

<!-- USER-EXTENSION-START: post-create -->
<!-- USER-EXTENSION-END: post-create -->
**(3) STOP.** Do NOT begin work unless user says "work", "fix that", or "implement that".
---
## Error Handling
| Situation | Response |
|-----------|----------|
| No title | Prompt user |
| Empty after prompt | "An enhancement title is required." → STOP |
| `gh pmu create` fails | "Failed to create issue: {error}" → STOP |
| Special chars | Escape for shell safety |
| Sweep: issue history unavailable (`gh` error) | Warn, emit `partialFormat` naming what ran and what failed, continue. Never claim a clean sweep. |
| Sweep: **zero `searchSurfaces` resolved** | Warn, emit `partialFormat`, continue. A sweep that searched nothing is failed, not empty — never emit `noneFoundFormat` here. |
| Sweep: `prior-art-sweep.json` missing/unreadable | Warn, skip sweep, emit no `**Prior Art:**` section. Absence correctly reads as "no sweep ran". |
---
**End of /enhancement Command**
