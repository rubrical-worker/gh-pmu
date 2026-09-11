---
version: "v0.101.0"
description: Create a proposal document and tracking issue (project)
argument-hint: "<title> [--prior-art] [--update [changes]]"
copyright: "Rubrical Works (c) 2026"
---

<!-- EXTENSIBLE -->
# /proposal

Creates a proposal document (`Proposal/[Name].md`) and a tracking issue with the `proposal` label. Also triggered by the `idea:` alias.

**Extension Points:** See `.claude/metadata/extension-points.json` or run `/extensions list --command proposal`

## Prerequisites

- `gh pmu` extension installed
- `.gh-pmu.json` configured in repository root

## Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `<title>` | No | Proposal title (e.g., `Dark Mode Support`) |
| `--prior-art` | No | Run prior-art sweep before composing proposal. Absent = current behavior, no sweep. |
| `--assignee <value>` | No | GitHub login for the new issue. Omitted → `@me`. |
| `--target <owner/name>` | No | File the issue against a registered companion repository instead of this one. |
| `--update [changes]` | No | Revise an existing `Proposal/[Name].md` instead of creating one. Following text is applied directly; a bare flag starts an interactive `AskUserQuestion` workflow. Requires the document to exist. |

If no title provided, prompt the user. **Alias:** `idea:` is identical to `proposal:`.

## Execution Instructions

**REQUIRED:** Before executing:

1. Use `TaskCreate` for one task per step below. No routing → bulk create upfront (see rule `07-task-creation-timing.md`).
2. Include one task per active (non-empty) `USER-EXTENSION` block.
3. Mark tasks `in_progress` → `completed` via `TaskUpdate`.
4. **Post-Compaction:** Re-read spec, call `TaskList`, resume from first incomplete task.

## Workflow

### Step 1: Parse Arguments

Extract `<title>` from arguments. **If empty:** ask for title. **If special characters** (backticks, quotes): escape for shell; on Windows use temp file approach.

**`--prior-art` token:** recognize anywhere in argument text, **remove it from the title**, set sweep flag. Absent → no sweep. Strip **before** name conversion, or the token lands in the filename (`Dark-Mode-Support---Prior-Art.md`). Required for direct slash-command invocation (no hook runs there); on the trigger-word path `workflow-trigger.js` already strips flag-shaped tokens by **shape**, not allowlist membership (#2515). A `--` that is not flag-shaped (bare separator, `---` rule, `--` in prose) is preserved verbatim.

**`--update` token:** recognize anywhere in argument text, **remove it from the title**, set update mode. Strip **before** name conversion or the token lands in the filename (`Dark-Mode-Support---Update.md`). Text following the flag is the **change instruction**; an empty remainder selects the interactive form (Step 3b).
**Text form is direct-invocation only (#2767).** On the trigger-word path a recognised flag claims **exactly one token** as its value (`02-github-workflow.md`), so `proposal: Dark Mode --update rename the risk section` cannot carry the remainder. Not a gap to patch: a rest-of-line variant would change value attachment for **every** command sharing the convention to suit one flag. There `--update` takes its **bare interactive form**, which needs no value.

**Name conversion:** Replace spaces with hyphens, Title-Case each word. Example: `dark mode support` → `Dark-Mode-Support`.

### Step 2: Check for Existing Proposal

**Under `--update` an existing document is the precondition, not a conflict — do NOT raise the overwrite prompt (#2767).** Overwrite is the destructive path this mode avoids: it discards `**Tracking Issue:**`, `**Diagrams:**` and any `**Prior Art:**` section, and its only alternative is STOP.
**`--update` with no `Proposal/[Name].md` is an error.** Report `Proposal/[Name].md does not exist — nothing to update.` and **STOP**. Never fall through to create: a mistyped title would silently create a second proposal under the wrong name while the user believes they edited the first.

If `Proposal/[Name].md` exists, ask `Proposal/[Name].md already exists. Overwrite? (yes/no)`. No → STOP.

### Step 3: Gather Description (Mode Selection)

| Input | Title | Mode |
|-------|-------|------|
| Bare `/proposal` (no title, no description) | Ask in Step 1 | **Default to Guided** (no mode prompt) |
| Title only `/proposal Dark Mode` | Provided | **Ask Quick/Guided** via `AskUserQuestion` |
| Title + description `/proposal Dark Mode - adds theme switching` | Provided | **Auto-select Quick** (no mode prompt) |

**Detection:** Descriptive phrase beyond title (dash-separated, sentence, multi-word detail) → "title + description". Short title (1-4 words, no separator) → "title only".

#### Quick Mode

Single prompt: `Briefly describe the proposal (problem and proposed solution):`

If user provides description: populate template. If declines/skip: placeholder sections.

#### Guided Mode

Walk through sections:

1. **Problem Statement:** "What problem does this solve?"
2. **Proposed Solution:** "How would you solve it?" (follow-up: "Any specific files/components affected?")
3. **Implementation Criteria:** "What defines 'done'? List the acceptance criteria."
4. **Alternatives Considered:** "What alternatives did you consider and why reject them?" (skippable)
5. **Impact Assessment:** "Scope, risk level (low/med/high), effort estimate?" (skippable)
6. **Screen Discovery:** "Any screens affected?" (skippable)
   - Yes → offer `/catalog-screens` or link existing `Screen-Specs/`
   - Existing specs found → list and ask which to reference
   - No or skip → continue without screen references

**For each prompt:** capture answer, or on "skip" leave "To be documented" placeholder. Populated sections replace placeholders.

#### Title-Only Mode Prompt

```javascript
AskUserQuestion({
  questions: [{
    question: "How would you like to create this proposal?",
    header: "Mode",
    options: [
      { label: "Quick", description: "Single prompt — describe the proposal in one go" },
      { label: "Guided", description: "Step-by-step — prompted for each section individually" }
    ],
    multiSelect: false
  }]
});
```

### Step 3b: Apply the Update (`--update` only)
**Trigger:** update mode from Step 1. **Replaces** Step 3's mode selection — Quick/Guided author a *new* proposal and have nothing to offer an existing one.
**Text form (`--update <changes>`):** apply the described changes. **Preserve `**Tracking Issue:**`, `**Diagrams:**` and any `**Prior Art:**` verbatim** unless the instruction names them — they record history, not content.
**Interactive form (bare `--update`):** offer the editable sections via `AskUserQuestion`, then prompt for each selected revision:
```javascript
AskUserQuestion({
  questions: [{
    question: "Which sections of this proposal do you want to revise?",
    header: "Sections",
    options: [
      { label: "Problem Statement", description: "What problem this solves" },
      { label: "Proposed Solution", description: "How it would be solved" },
      { label: "Implementation Criteria", description: "What defines done" },
      { label: "Alternatives Considered", description: "Options weighed and rejected" }
    ],
    multiSelect: true
  }]
});
```
**Impact Assessment** offered the same way when present. Per Guided-mode convention, a skipped section is left as-is, never reset to a placeholder.
**Tracking-issue propagation:** the update reaches the tracking issue's `### Summary` and **nothing else** in that body. Resolve the number from `**Tracking Issue:**`, rewrite `### Summary` to match the revised Problem Statement and Proposed Solution via `gh pmu view $N --body-stdout` → edit → `gh pmu edit $N -F` → `rm`.
**Preserve `**File:** Proposal/[Name].md` and `### Lifecycle` verbatim.** The `**File:**` marker is what `/create-prd` reads to locate the document; a rewrite that drops or reflows it breaks PRD conversion for a proposal whose only change was a reworded paragraph. Leaving the issue stale reintroduces the divergence this mode closes.
**No resolvable tracking issue → report and continue** (`Document updated; tracking issue not updated — <reason>.`). The document is the artifact the user asked to change.
### Step 3a: Prior-Art Sweep (`--prior-art` only)

**Trigger:** sweep flag from Step 1. Absent the flag, skip this step entirely.

Runs **before** the proposal document is composed, so findings change what gets written rather than annotating a document already wrong.

**Distinct from and additional to Step 2.** Step 2 only tests whether `Proposal/[Name].md` exists — a filename check. It does not look at the codebase, other proposals' contents, or issue history, so it cannot detect a capability that already ships under a different name.

**Re-read `.claude/metadata/prior-art-sweep.json` from disk at use** (rule `01-anti-hallucination.md`). Surfaces, excludes, stopwords, disposition signals and body formats live only there — not restated here.

1. **Resolve surfaces:** `node -e "console.log(JSON.stringify(require('./.claude/scripts/shared/lib/prior-art-surfaces.js').resolveSurfaces(require('./.claude/metadata/prior-art-sweep.json'), process.cwd())))"`. Missing surface → skipped, not fatal. **Zero resolved is a failed sweep** — go to Error Handling, emit `PARTIAL`, do **not** report a clean result.
2. **Derive terms** per `termDerivation`: feature nouns, **and** terms from the files this change would touch. The second axis is not optional — prior work often shares no vocabulary with the request.
3. **Search** resolved surfaces honoring `excludes`; search issue history per `searchSurfaces.issueHistory`, including closed.
4. **Apply `questions`** to what the search returned.
5. **Classify** against `dispositions`; take that disposition's `action`.

| Verdict | Action |
|---|---|
| `already-shipped` | **STOP.** Create neither the document nor the tracking issue. Report conflicting issue numbers and file paths. |
| `found-but-warranted` | Continue. Record `**Prior Art:**` — what exists, how this differs. |
| `none-found` | Continue. Record `noneFoundFormat` line including terms searched. |
**Two marker forms are recognised on read (#2700).** Emission is unchanged — always the bold inline form from `prior-art-sweep.json` `bodyFormat`. Detection also accepts a markdown heading:

| Form | Example | Recognised |
|---|---|---|
| Bold inline (emitted) | `**Prior Art:** found — …` | yes |
| Markdown heading | `## Prior Art` / `### Prior Art:` — any level | yes |
| Bare, neither | `Prior Art: found — …` | **no** |

Before #2700 only the bold counted, so a researched `## Prior Art` read as one nobody wrote. Authoritative: `bodyFormat.recognisedForms`, pinned to `classifyMarker` by test.


**Both artifacts carry the section.** Write `**Prior Art:**` into `Proposal/[Name].md` (Step 4) **and** the tracking issue body (Step 5) — a reader of either must see what was searched without opening the other.

**Emit the section on every `--prior-art` invocation, including a nil result.** Presence records the sweep ran; absence means none ran.

Use exact `bodyFormat` strings (`heading`, `noneFoundFormat`, `foundEntryFormat`, `partialFormat`) — never paraphrase, so consumers test the marker without parsing prose. `PARTIAL` is treated as equivalent to an absent marker and triggers re-sweep.

**`reviewSweep` gates this path too, in one mode (#2564).** Four values (`framework-config.json`, absent = `recommend`): `full`, `recommend`, `flag-only`, `off`. First three honor `--prior-art`; only `off` refuses. Delegate, do not re-derive:
```bash
node -e "console.log(JSON.stringify(require('./.claude/scripts/shared/lib/prior-art-marker.js').decideFlagSweep({reviewSweep:REVIEW_SWEEP})))"
```
`REVIEW_SWEEP` ← `framework-config.json` `reviewSweep` (absent = `recommend`) → `{sweep, refused, mode, message}`. `sweep:true` → run the sweep step as written. `refused:true` → **do not sweep**; report `message` verbatim, emit **no** `**Prior Art:**` section (absence reads as "no sweep ran"), continue creating the proposal. Never a silent no-op — the user typed a flag and must learn why nothing happened and what to change.
Mode `off` deliberately overrides a typed flag. Earlier revisions of this spec, its sibling, and the schema all promised `--prior-art` swept regardless; #2564 inverts that on purpose, and the refusal message is what keeps the inversion honest.

<!-- USER-EXTENSION-START: pre-create -->
<!-- USER-EXTENSION-END: pre-create -->

### Step 4: Create Proposal Document

Ensure `Proposal/` directory exists. Create `Proposal/[Name].md`:

```markdown
# Proposal: [Title]

**Status:** Draft
**Created:** [YYYY-MM-DD]
**Author:** AI Assistant
**Tracking Issue:** (will be updated after issue creation)
**Diagrams:** None

---

## Problem Statement

[Problem description or "To be documented"]

[Sweep ran (Step 3a): **Prior Art:** section here, using bodyFormat strings]

## Proposed Solution

[Solution description or "To be documented"]

## Implementation Criteria

- [ ] [Criterion 1]
- [ ] [Criterion 2]

## Alternatives Considered

- [Alternative 1]: [Why not chosen]

## Impact Assessment

- **Scope:** [Files/components affected]
- **Risk:** [Low/Medium/High]
- **Effort:** [Estimate]
```

**Diagrams:** When a diagram path is specified, update `**Diagrams:**` from "None" to the path(s). Create `Proposal/Diagrams/` lazily. Naming: `Proposal/Diagrams/[Name]-*.drawio.svg`.

### Step 5: Create Tracking Issue

Build issue body:

```markdown
## Proposal: [Title]

**File:** Proposal/[Name].md

### Summary

[Brief description from Step 3]

### Lifecycle

- [ ] Proposal reviewed
- [ ] Ready for PRD conversion
```

**Critical:** Body MUST include `**File:** Proposal/[Name].md` — required for `/create-prd` integration.

**Sweep ran (Step 3a):** include the `**Prior Art:**` section here too, after `### Summary`. Both artifacts carry it.

```bash
gh pmu create --title "Proposal: {title}" --label proposal --status backlog --priority p2 --assignee {assignee} -F .tmp-body.md
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
**Assignee:** substitute `{assignee}` from `node .claude/scripts/shared/lib/gh-pmu-config.js --assignee <value>` — pass the user's `--assignee` value, omit when none given. Helper returns that login, else `@me`; reads no config file. NEVER hardcode a login or drop the flag (omitted `--assignee` silently creates an unassigned issue). Unresolvable login → `gh pmu` exits 1 and creates nothing; report the error, do NOT retry without the flag.

**Note:** Always use `-F .tmp-body.md` (never inline `--body`).

### Step 6: Update Proposal with Issue Reference

Update tracking issue field: `**Tracking Issue:** #[issue-number]`

### Step 6a: Commit Proposal

**Guard:** Only commit if changes exist:
```bash
git diff --name-only -- "Proposal/"
git diff --cached --name-only -- "Proposal/"
```
If no changes, skip silently.

**If changes exist:**
```bash
git add "Proposal/[Name].md"
```

Commit message:
```
docs: add proposal — [Title] (Refs #$ISSUE_NUM)
```

For modifications: `docs: update proposal — [Title] (Refs #$ISSUE_NUM)`

**Note:** Use `Refs #` (not `Fixes #`) per workflow rules — proposal issue stays open.

### Step 7: Cleanup, Report, and STOP
Three parts, in order. The prune is **part of** this step, and this step is **numbered** — `One task per numbered step` now covers it, so an unpruned list surfaces as an unfinished task like any other. The halt is part (3) and lives nowhere earlier: while it sat in this step's TITLE a reader stopped at the title and never reached the prune (#2641).
**(1) Prune the task list** (unconditional — every path, including early-exit paths where Phase 1 created tasks and later phases never ran):
1. `TaskList` — enumerate all tasks.
2. For every task owned by this `/proposal` invocation, `TaskUpdate status=deleted`.
3. Do **not** delete tasks created outside this invocation (user TODOs).
**(2) Emit the closing output:**
Report the document path `Proposal/[Name].md`, the created issue number and title, Status `Backlog`, Label `proposal`, then offer `/review-proposal` or `/create-prd` with the issue number.

<!-- USER-EXTENSION-START: post-create -->
<!-- USER-EXTENSION-END: post-create -->
**(3) STOP.** Do not begin work unless user explicitly says "work", "implement the proposal", or "work issue".
## Error Handling

| Situation | Response |
|-----------|----------|
| No title provided | Prompt user for title |
| Empty title after prompt | "A proposal title is required." → STOP |
| Existing file, user declines overwrite | STOP without creating anything |
| `Proposal/` directory missing | Create it silently |
| `gh pmu create` fails | "Failed to create issue: {error}" → STOP |
| Special characters in title | Escape for shell safety |
| Sweep: issue history unavailable (`gh` error) | Warn, emit `partialFormat` naming what ran and what failed, continue. Never claim a clean sweep. |
| Sweep: **zero `searchSurfaces` resolved** | Warn, emit `partialFormat`, continue. A sweep that searched nothing is failed, not empty — never emit `noneFoundFormat` here. |
| Sweep: `prior-art-sweep.json` missing/unreadable | Warn, skip sweep, emit no `**Prior Art:**` section. Absence correctly reads as "no sweep ran". |
**End of /proposal Command**
