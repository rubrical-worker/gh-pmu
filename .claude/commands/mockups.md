---
version: "v0.100.0"
description: Create text-based or diagrammatic screen mockups (project)
argument-hint: "[#NN] [--from-image <path>] [--serve [{Name}]] [--port <N>] [--open] [--showcase] [--apply-decisions] [--consolidate]"
copyright: "Rubrical Works (c) 2026"
---

<!-- EXTENSIBLE -->
# /mockups

Creates text-based or diagrammatic screen mockups. Fully interactive via `AskUserQuestion`. Accepts optional issue reference (`#NN`) to pre-populate context from a bug, enhancement, proposal, or PRD.

**Extension Points:** See `.claude/metadata/extension-points.json` or `/extensions list --command mockups`

## Prerequisites

- Shared screen spec schema: `.claude/metadata/screen-spec-schema.json`

## Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `#NN` | No | Issue number (bug/enhancement/proposal/PRD). Reads body to pre-populate flow. |
| `--from-image <path>` | No | AC21 — use reference image as visual baseline. Path validated by `validateScreenshotFile` (NFR-3 mime allowlist). Bypasses Q4. |
| `--serve [{Name}]` | No | Start a backgrounded zero-dep static server (`.claude/scripts/shared/mockups-serve.js`, http+fs+path only) on `Mockups/` (bare) or `Mockups/{Name}/` (scoped). Spawn via `Bash` `run_in_background: true`; read listening URL from helper's single-line banner via `BashOutput`; report URL + shell ID. Can run standalone or after normal generation. |
| `--port <N>` / `-p <N>` | No | Pin port; default `3000`. Helper falls back to next free port on conflict; command reports the **actual** port. |
| `--open` | No | Spawn platform default browser to the listening URL. `win32` → `start "" "<url>"`; `darwin` → `open "<url>"`; `linux` → `xdg-open "<url>"`. Launch failure warns, does not fail command. |
| `--showcase` | No | Launch Living Style Guide showcase (#2429, Story 1.3). Mockups command bundles into showcase the same way `/design-system` does — categories sourced from screen catalog (UI Components, Iconography). Mutex with `--apply-decisions`. |
| `--consolidate` | No | Merge selected mockups from several sets into one new set, record supersession, retire sources into `Mockups/Deprecated/` (#2589). No issue number; runs standalone like `--serve`. **Composes with `--serve`** (consolidate, then serve) — NOT mutually exclusive. Every decision is its own `AskUserQuestion`; **never batched**. |
| `--apply-decisions` | No | Apply pending decisions from `Design-System/showcase/decisions.json` without launching server (#2429, Story 1.4). Implicit resume on subsequent invocations: `applyMockups` in `.claude/scripts/shared/showcase-resume.js` appends rows to `Mockups/.showcase-applied.jsonl`. |

```
/mockups                           # Fully interactive, no context
/mockups #42                       # Interactive with issue #42 context
/mockups --from-image ./design/home.png
/mockups --serve                   # Serve Mockups/ on http://localhost:3000/
/mockups --serve sandbox           # Serve Mockups/sandbox/ only
/mockups --serve --open            # Serve + auto-launch browser
/mockups --serve --port 8080 --open
/mockups --showcase                # Launch Living Style Guide showcase
/mockups --apply-decisions         # Apply pending showcase decisions, no server
/mockups --consolidate             # Merge sets into one, deprecate sources
/mockups --consolidate --serve     # Consolidate, then serve the result
```

## Execution Instructions

**REQUIRED:** Parse workflow steps and create tasks via `TaskCreate`. Add a task per non-empty `USER-EXTENSION` block. Mark `in_progress` → `completed`. Re-read spec and regenerate tasks after compaction.

## Workflow

<!-- USER-EXTENSION-START: pre-mockup -->
<!-- USER-EXTENSION-END: pre-mockup -->

### Step 1: Discovery and Interactive Setup

### Step 1a: Load Context

**If `#NN` provided:** `gh issue view #NN --json body,title,labels` and extract issue type (from labels), screen/feature names, existing mockup/spec references.

**Always:** Read `.claude/metadata/screen-spec-schema.json`.

### Step 1b: Discover Existing Content

Scan `Mockups/` and subdirectories before asking questions: list all `Mockups/{Name}/` directories; inventory `Specs/`, `Screens/`, `AsciiScreens/` contents; note file names, types, counts.

### Step 1b-ii: ASCII-Only Detection and Conversion Offer

**Detection:** `AsciiScreens/` has files AND `Screens/` is empty or missing.

**If ASCII-only detected,** use `AskUserQuestion`:
- "This mockup set contains only ASCII mockups. Convert them to interactive mockups and create specs?"
- **Yes, convert** — Generate `.drawio.svg` mockups from ASCII sources and create specs in `Specs/`
- **No, continue** — Skip conversion

**If Yes:**
1. For each ASCII mockup, generate `.drawio.svg` in `Screens/` using layout/element data from ASCII source
2. Create spec `Mockups/{Name}/Specs/{Screen-Name}.md` if missing
3. Report: `"Converted {N} ASCII mockups. Created {M} screen specs."`
4. Continue normal processing

**If not ASCII-only:** Skip silently.

### Step 1c: Pipeline Context Detection

**If `#NN` provided**, check for artifacts from other UI design pipeline commands.

**Screen spec detection:** check issue body or linked proposal for `## Screen Specs` section; scan `Mockups/*/Specs/` for specs matching screens in issue title/body. If found, note for Q4 pre-selection and report `"Screen specs found: {names}"`.

**Path analysis detection:** for proposals, read linked file for `## Path Analysis`; for enhancement/bug, check issue body. If found, parse category names/path counts; report `"Path analysis found: {N} paths across {M} categories."` Then `AskUserQuestion`:
- "Use path analysis to scope mockups" — organize by scenario paths (nominal, error, edge)
- "Ignore path analysis — scope by screen"

If used: generate mockup per path implying a distinct screen state. Group by category in README.

**If no `#NN` or artifacts:** Skip to Step 1d.

### Step 1d: Interactive Question Flow

**Spec-literal option lists (#2383):** Present each question with its options **verbatim** as written. Context adaptation is permitted only as **pre-selection** of a listed option — never substitution, omission, or invention. Pre-select when context strongly implies an answer; keep full list visible so the user can override. See `Construction/Design-Decisions/2026-04-19-mockups-catalog-screens-spec-literal-questions.md`.

**Q1: What would you like to do?**
- "Create new mockups"
- "Modify existing mockups"
- "View/browse existing mockup sets"

**Conditions:** If `#NN` provided, pre-select based on issue type (enhancement/proposal → Create; bug referencing screen → Modify). If no existing mockups, skip Q1 → "Create new mockups".

**Q2: Which mockup set?** List each `Mockups/{Name}/` directory **verbatim** (directory names only — do NOT annotate with type/element-count/metadata; conflates Q2 with Q3) + "Create a new mockup set". If `#NN`, derive name from issue title (first noun phrase) and pre-suggest.

**Q2a** (new set): Ask name via free text; suggest from issue title if `#NN`. Creates `Mockups/{Name}/`.

**Q3: What type of mockups?**

**Always ask in Create-new flow.** Never skip based on inferred type. If context implies a type, pre-select but keep full list visible — every output path below must remain reachable.

- "Interactive HTML mockups" → `Screens/` as `.html`
- "ASCII/text mockups" → `AsciiScreens/`
- "Interactive UI mockups (drawio.svg)" → `Screens/`
- "Both ASCII + drawio.svg" → both

`/mockups` produces planning artifacts only (HTML / ASCII / drawio). Framework-native component generation removed per PRD #2333 (AC15/AC16/AC17).

**Q4: How should screen content be sourced?**

**Present the full option set below verbatim.** Do NOT substitute (e.g., "Regenerate Login/Dashboard" is not listed — do not offer in place of "From existing screen specs"). Do NOT drop options. Permitted adaptations: spec-documented conditionals (`#NN`-only options) + pre-selection based on context.

- "From existing screen specs"
- "From source code discovery"
- "Describe screens manually"
- "From issue #NN description" (only when `#NN`)
- "From screen catalog" (AC19 — read `Mockups/screen-catalog.json` via `loadCatalog` from `.claude/scripts/shared/lib/screen-catalog.js`, list entries for selection)
- "From reference image" (AC20 — accept screenshot path, validate via `validateScreenshotFile` from `.claude/scripts/shared/lib/screenshot-input.js`, use multimodal Read)

**Condition:** If `Specs/` has specs, show as available sources.

**Q4a** (existing specs): `AskUserQuestion` with `multiSelect: true` listing specs in `Mockups/{Name}/Specs/`.

**Q4b** (source discovery): Free text for directory to scan. Defaults to full project scan.

**Q5** (per screen): **Review mockup for {Screen}?**
- "Looks good, save it"
- "Make adjustments" → follow-up conversation
- "Skip this screen"

**Q6** (Modify flow): `AskUserQuestion` with `multiSelect: true` listing existing mockup files. Then ask what changes via conversation.

**Without `#NN`:** All questions start fresh. Flow begins at Q1 (or skips Q1 if no existing mockups).

**Per-screen progress tracking:** After Q4 resolves the screen list, create one task per screen for compaction recovery ("resume from screen N") and visible progress.

Post-compaction: re-read spec, check `Mockups/{Name}/` for partially created files, resume from first unwritten screen.

### Step 1e: Load Design Tokens

Call `getMockupPalette(projectRoot)` from `.claude/scripts/shared/lib/dtcg-token-reader`; pass result through `paletteToCSS()` and `paletteToDrawioColors()` from `.claude/scripts/shared/mockup-token-styles`.

- `palette.hasTokens === true`: tokens from `Design-System/idpf-design.tokens.json` — use token-derived values
- `palette.hasTokens === false`: built-in defaults — styling unchanged

Record for Step 7: `tokenStatus = palette.hasTokens ? 'Design-System/idpf-design.tokens.json (applied)' : 'not found (defaults used)'`

### Step 2: Generate Mockup

Based on screen elements (from spec, source discovery, manual description, or issue context), create a visual representation.

**ASCII/text mockup** → `Mockups/{Name}/AsciiScreens/{Screen-Name}-mockup.md`:

```markdown
# Mockup: {Screen Name}

**Screen Spec:** Mockups/{Name}/Specs/{Screen-Name}.md
**Created:** {YYYY-MM-DD}

## Layout
{ASCII/Unicode box drawing}

## Element Placement Notes
| Element | Position | Size/Span | Notes |
|---------|----------|-----------|-------|

*Mockup created {YYYY-MM-DD} by /mockups*
```

**Diagram-based mockup** → `Mockups/{Name}/Screens/{Screen-Name}-mockup.drawio.svg`:
Use `.drawio.svg` with editable `mxGraphModel` per the `drawio-generation` skill. Apply token-derived colors from `paletteToDrawioColors()` to `mxCell` style attributes: `fillColor`, `fontColor`, `strokeColor`.

**Interactive HTML mockup** → `Mockups/{Name}/Screens/{Screen-Name}-mockup.html`:
Self-contained HTML using Tailwind via CDN. Structure:

1. **Header badge:** Fixed mockup label with issue reference (`MOCKUP — Issue #NN`)
2. **Visual states:** Show all relevant states — Before/After, Collapsed/Expanded, current/outdated, enabled/disabled, loading/loaded. Label each with a colored chip (State 1, State 2, ...).
3. **Interactive elements:** Use `onclick` for demo behavior where helpful
4. **Implementation notes section:** `<div>` at bottom listing component names + file paths to modify, line number references, CSS/class cleanup guidance, test selector impact (`data-testid` changes), and post-success behavior
5. **Fonts:** `palette.fonts.sans` for UI, `palette.fonts.mono` for code. Fallback: `Plus Jakarta Sans` and `JetBrains Mono`.

Skeleton: `<head>` includes Tailwind CDN script and `<style>` containing `cssVars` from `paletteToCSS()`. `<body>` uses CSS variables for background/text/font and contains the fixed badge `<div>` (red-tinted), per-state blocks, and the implementation notes `<div>`.

### Step 3: Collision Protection and Write

**Before writing each file,** check if target exists.

- **Exists:** `AskUserQuestion` — Overwrite / Save with alternative name (suggest `{Screen-Name}-v2-mockup.md`) / Skip
- **Does not exist:** Write directly

Ensure directories exist: `Mockups/{Name}/{AsciiScreens,Screens,Specs,AC}/`.

### Step 4: Cross-Reference Updates

**Update screen spec** (if exists in `Specs/`): read `Mockups/{Name}/Specs/{Screen-Name}.md`; append or update a `## Related Artifacts` section listing each created mockup file (ASCII and/or `.drawio.svg`); write back.

**Mockup references its spec** via the `**Screen Spec:**` field in its header.

**Registry upsert (AC18, AC22):** Thread the upsert return value — `upsertScreen` is **pure**; discarding it persists the pre-upsert catalog and drops the new screen (#2380):

```js
catalog = upsertScreen(catalog, screenName, { status: 'active', kind, canonicalSpec, designTokens: tokensApplied ? 'applied' : 'pending', tokenDependencies });
saveCatalog(catalog);
```

From `.claude/scripts/shared/lib/screen-catalog.js`. `tokensApplied` is true when tokens consumed; `tokenDependencies` lists token keys (read by Story 1.14 propagation).

**Navigation graph regeneration (AC40):** After registry upsert, regenerate `Mockups/NAVIGATION.md` via `renderNavigationMarkdown(catalog)` from `.claude/scripts/shared/lib/navigation-graph.js`. Sections: Pages, Wizards (with steps), Unreachable (AC41).

<!-- USER-EXTENSION-START: post-mockup -->
<!-- USER-EXTENSION-END: post-mockup -->

### Step 4b: AC JSON Generation (if `#NN` provided)

1. Extract ACs via:
   ```javascript
   const { extractAC, generateACFile, mergeACFile } = require('.claude/scripts/shared/mockup-ac-generator');
   ```
2. Extract checkbox items under `**Acceptance Criteria:**` from issue body
3. If none, infer from description/proposed solution
4. Generate AC JSON via `generateACFile()`, mapping each criterion to mockup file(s)
5. Write to `Mockups/{Name}/AC/ac-{NN}.json`

**On re-run:** If `ac-{NN}.json` exists, merge via `mergeACFile()` — preserves `verified` state and mappings.

**AC JSON structure:**
```json
{
  "issue": 42,
  "title": "Issue title",
  "generated": "YYYY-MM-DD",
  "criteria": [
    { "id": "AC-1", "description": "Criterion text", "mockups": ["Screens/Login-mockup.drawio.svg"], "verified": false }
  ]
}
```

**If no `#NN`:** Skip entirely.

### Step 5: README.md Auto-Generation

Auto-generate/update `Mockups/{Name}/README.md` as an index. Include sections for `Specs`, `Screens (Interactive)`, `ASCII Screens`, and `Acceptance Criteria` — listing all files in each subdirectory; omit empty sections. Include `**Last Updated:** {YYYY-MM-DD}` and a footer marker `*Auto-generated by /mockups*`.

### Step 6: Issue Writeback (if applicable)

**Trigger:** `#NN` supplied AND at least one artifact written this invocation. Only skip condition: "no `#NN`".

**Unconditional on discovery path:** Runs **regardless of which discovery path** produced the artifacts. The Step 1d question battery, Step 1c pipeline-context detection, and any short-circuit bypass all converge here — do not gate writeback on Q1–Q6 execution, on any specific answer, or on which sourcing option was chosen. Bypass paths that still trigger writeback when `#NN` is present:

- `--from-image <path>` (bypasses Q4)
- "From source code discovery" answer to Q4 (no per-screen Q4a)
- "From issue #NN description" / "From screen catalog" / "From reference image" answers to Q4
- Step 1c pipeline-context paths (screen-spec auto-match, path-analysis-driven generation)
- Step 1b-ii ASCII-only conversion
- Any future Step 1 bypass — contract: "artifact written + `#NN` supplied → writeback runs"

Structural test `tests/commands/mockups-writeback-unconditional.test.js` (#2417 AC5) guards this property; do not relocate Step 6 or demote below `### Step 6` heading depth.

**Proposal:** Read document; append or update a `## Mockups` section listing each created mockup file. If proposal path is invalid/deleted → warn, skip writeback, mockup still created.

**Enhancement/Bug:** Update issue body via `gh pmu view #NN --body-stdout` / `gh pmu edit #NN -F`. Append or update `## Mockups` section; replace contents if section already exists.

**No `#NN`:** Skip writeback.

### Step 7: Report

```
Mockup complete.
  Mockup set: Mockups/{Name}/
  Screens: {names}
  Output: {list of created/modified files}
  AC file: {Mockups/{Name}/AC/ac-{NN}.json (created/merged) | skipped (no issue reference)}
  Tokens: {Design-System/idpf-design.tokens.json (applied) | not found (defaults used)}
  README: Mockups/{Name}/README.md (updated)
  Cross-references: {updated | no spec exists}

  Related: /catalog-screens to create or update screen specs.
```

### Step 8: Satisfaction Check and Commit Offer

If files were created/modified:

**Step 8a: Satisfaction Check** — `AskUserQuestion`: "Are the mockups satisfactory?"
- **Yes, looks good** — Proceed to commit offer
- **No, make changes** — Ask what adjustments; after revisions, return to Step 8a
- **No, discard** — Report "Mockups left uncommitted." → **STOP**

**Step 8b: Commit Offer** (only after user confirms satisfaction) — "Stage and commit mockup changes?"
- **Yes:**
  ```bash
  git add Mockups/{Name}/
  git commit -m "Refs #NN -- Add/update mockups for {Name}"
  ```
  Use `Refs #NN` when issue context available — mockup creation does not close issues. No issue context: `"Add/update mockups for {Name}"`.
- **No:** Skip — do not stage/commit.

### Step 9: Consolidate Mockup Sets (when `--consolidate` passed)
Trigger: `--consolidate` present. Runs standalone (skip Steps 1–8 when no generation requested), as Step 10 does for `--serve`. No issue number required.
**Composes with `--serve`:** both present → consolidation runs first, Step 10 serves the result — the reason this step precedes Step 10. NOT mutually exclusive, unlike `--showcase`/`--apply-decisions`.
**Re-read `.claude/metadata/mockups-consolidation.json` from disk** for the ordered decision sequence; never paraphrase from memory. Every interactive decision is its own `AskUserQuestion` — **never batched** — and selection is **per source set** (one listing per `Mockups/{Name}/`), never flattened across sets.
Contract the sequence implements — the load-bearing outcomes:
- **Supersession is recorded, not silent:** each carried-forward mockup gains a `**Supersedes:**` header line; the set's `README.md` gains a `## Consolidation` section naming source set, source file, date.
- **Nothing is moved before the user confirms** consolidation is complete; everything prior writes only into the new set. Target name suggested, overridable, re-prompted on collision — never overwritten.
- **Deprecation is a move, never a delete.** Affected sources go to `Mockups/Deprecated/` (suggested, overridable), preferring `git mv` so history follows.
- **Specs travel with their mockups** — `canonicalSpec` repointed, stale `## Related Artifacts` cross-references rewritten via Step 4's rewrite.
- **Derived state reconciled:** `Mockups/screen-catalog.json` via `loadCatalog`/`upsertScreen`/`saveCatalog`, moved entries to `status: "deprecated"`, threading `upsertScreen`'s pure return into `saveCatalog` (#2380); then `Mockups/NAVIGATION.md` via `renderNavigationMarkdown`.

### Step 10: Serve Mockups (when `--serve` passed)
**`Mockups/Deprecated/` is excluded from the served tree** and `status: deprecated` entries are filtered from the showcase bundle (#2589). With `--consolidate`, Step 9 runs first and this step serves the result.

Runs standalone or after Step 8. Steps:

1. **Resolve target:** bare `--serve` → `Mockups/`; `--serve {Name}` → `Mockups/{Name}/`. Missing target → report "Target not found: {path}. Create mockups first." → STOP.
2. **Port:** use `--port`/`-p` if given, else `3000`. Helper handles in-use fallback internally.

   **Entry point (#2590):** reported URL is browsable without an `index.html` — helper serves a directory's `index.html` when present, else a generated listing. Do **not** pre-create an index, warn about a missing one, or hand the user a deep file path instead of the root URL: a mockup set has no index (Step 5 writes `README.md`), the exact case the listing covers. Hrefs are absolute, so `/{Name}` and `/{Name}/` behave identically.
3. **Spawn server** via `Bash` with `run_in_background: true`:
   ```bash
   node .claude/scripts/shared/mockups-serve.js --root <target> --port <port>
   ```
   Capture shell ID.
4. **Read banner via `BashOutput`** until line `Serving <root> at http://localhost:<N>/` appears — extract `<N>` (actual port, may differ on fallback).
5. **Optional browser launch** (when `--open`): platform-specific one-shot backgrounded `Bash`:
   - `win32` → `start "" "http://localhost:<N>/"`
   - `darwin` → `open "http://localhost:<N>/"`
   - `linux` → `xdg-open "http://localhost:<N>/"`

   Launch failure warns, does not fail command. Server continues regardless.
6. **Report:**
   ```
   Serving Mockups/{Name}/ at http://localhost:{port}/
   Shell ID: {id}  (use KillShell to stop)
   Browser: opened | not opened (--open not passed) | launch failed: {reason}
   ```

Server runs until user kills its shell; `/mockups --serve` itself does NOT block.

### Step 11: Cleanup and STOP
Two parts, in order. The prune is **part of** this step, and this step is **numbered** and final — `One task per numbered step` now covers it, so an unpruned list surfaces as an unfinished task like any other. The halt is part (2): while it sat in Step 8's TITLE a reader stopped there, before both the prune AND the serve step, now Step 10 (#2641).
**(1) Prune the task list** (unconditional — every path, including early-exit paths where Phase 1 created tasks and later phases never ran):
1. `TaskList` — enumerate all tasks.
2. For every task owned by this `/mockups` invocation, `TaskUpdate status=deleted`.
3. Do **not** delete tasks created outside this invocation (user TODOs).
**(2) STOP.** Do not proceed without user instruction.
## Error Handling

| Situation | Response |
|-----------|----------|
| No argument and no existing mockups | Skip Q1, default to "Create new mockups" |
| `#NN` issue not found | "Issue #NN not found" → continue without issue context |
| Source discovery fails | Suggest "Describe screens manually" or "Run /catalog-screens first" |
| `Mockups/` missing | Create directory structure automatically |
| File collision on write | Ask: overwrite, alternative name, or skip |
| Spec cross-reference update fails | Warn, continue (mockup still created) |
| Proposal writeback path invalid | Warn, skip writeback, mockup still created |
| Schema file missing | "Shared schema not found at .claude/metadata/screen-spec-schema.json" → STOP |
| `--serve`: requested port in use | Helper falls back to next free port; command reports actual port. Not an error. |
| `--serve`: target `Mockups/` or `Mockups/{Name}/` missing | "Target not found: {path}. Create mockups first." → STOP |
| `--serve`: served root has no `index.html` | Expected, not an error. Helper renders a listing; report the root URL unchanged. |
| `--serve`: path is neither file nor directory | Helper returns 404. Not a command failure; server keeps running. |
| `--consolidate`: fewer than two mockup sets found | "Consolidation needs at least two mockup sets; found {N}." → STOP |
| `--consolidate`: target name collides with an existing set | Re-prompt for a different name; **never** overwrite an existing set |
| `--consolidate`: selected source file missing at move time | Report path, skip that file, continue remaining moves; list every skipped path in the final report |
| `--consolidate`: `git mv` unavailable or fails | Fall back to plain move, warn which was used, continue. History does not follow the file — say so rather than failing consolidation |
| `--serve --open`: browser launch fails | Warn with launch exit code; leave server running; do NOT fail command |
**End of /mockups Command**
