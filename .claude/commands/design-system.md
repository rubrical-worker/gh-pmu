---
version: "v0.99.0"
description: Produce DTCG-compliant design tokens with pluggable adapter architecture (project)
argument-hint: "[--init | --discover | --export <adapter> | --theme <name>]"
copyright: "Rubrical Works (c) 2026"
---
<!-- EXTENSIBLE -->
# /design-system
Produce DTCG-compliant design token file (`Design-System/idpf-design.tokens.json`) with pluggable adapter architecture. Integrates with `/mockups` and `/catalog-screens`.
**Extension Points:** `.claude/metadata/extension-points.json` or `/extensions list --command design-system`
---
## Prerequisites
- Node.js built-ins only (no npm deps)
- DTCG schema: `.claude/scripts/shared/lib/dtcg-schema.js`
- DTCG tokens: `.claude/scripts/shared/lib/dtcg-tokens.js`
---
## Arguments
| Argument | Description |
|----------|-------------|
| *(none)* | Init when no tokens; status summary when tokens exist. Bare behavior unchanged. |
| `--init` | Force init regardless of state. If valid tokens exist, prompt to confirm → `backupAndOffer(path)` (copies to `*.bak.{ts}`) → Init. Decline → STOP, no writes. Unreadable file → shared `'unreadable'` path (no double-prompt). |
| `--discover` | Extract tokens from existing code via adapters |
| `--export <adapter>` | Export tokens to framework-specific format |
| `--export all` | Export to all detected formats |
| `--theme <name>` | Generate/apply theme override |
| `--from-screenshot <path>` | AC31 — extract token candidates (color, typography, spacing, gradient) from a visual reference. Path validated via `validateScreenshotFile` from `.claude/scripts/shared/lib/screenshot-input.js`. Multimodal Read produces candidates → `applyCandidateSelection` from `.claude/scripts/shared/lib/screenshot-token-candidates.js` applies user accept/reject/partial decision before any write (AC32). |
| `--diff` | AC33 — 4-category drift report (additions/removals/mismatches/broken aliases) via each adapter's `diff(projectRoot, currentTokens)` (AC38), aggregated by `buildDriftReport` + rendered by `formatDriftReport` from `.claude/scripts/shared/lib/token-drift-report.js`. No writes. |
| `--discover --diff` | AC34 — combine discovery + diff, then selectively apply via `applySelectedDiff` (AC39). User decline → no writes. |
| `--showcase` | Launch Living Style Guide showcase (#2429, Story 1.3): build bundle via `.claude/scripts/shared/showcase-bundle.js`, start loopback server via `.claude/scripts/shared/showcase-server.js`, open browser. Mutex with `--apply-decisions`. |
| `--apply-decisions` | Apply pending decisions from `Design-System/showcase/decisions.json` without launching the server (#2429, Story 1.4). Implicit resume on subsequent invocations: pending decisions detected via `.applied.json` cursor sidecar are dispatched to `applyDesignSystem` in `.claude/scripts/shared/showcase-resume.js`, stamping `$lifecycle` on the addressed token. |

```
/design-system              # Init or status; auto-resumes pending decisions
/design-system --init       # Force init; backup-and-replace if tokens exist
/design-system --discover   # Discover from code
/design-system --export css-vars   # CSS custom properties
/design-system --export all        # All detected formats
/design-system --theme dark        # Dark theme override
/design-system --from-screenshot ./design/home.png   # Bootstrap tokens from image
/design-system --showcase           # Browser-based design review (#2429)
/design-system --apply-decisions    # Apply pending decisions headlessly
```
---
## Execution
**REQUIRED:**
1. Parse workflow steps → `TaskCreate`
2. Mark tasks `in_progress` → `completed`
---
## Workflow
### Step 1: Check Existing Token File
Invoke `detectMode('Design-System/idpf-design.tokens.json')` from `.claude/scripts/shared/lib/token-update-mode.js` (AC35):
- **`'init'`** (missing): proceed to init (Step 2)
- **`'update'`** (exists + parses): update walkthrough — list groups, selective edit, `mergeUpdate(original, edits)` preserves untouched groups byte-for-byte
- **`'unreadable'`** (parse fails): report parse error, offer `backupAndOffer(path)` to back up to `*.bak.{ts}` and reinit (Exception #7)
### Step 2: Mode Selection
- **`--init` + `'init'`:** Init (Step 3), identical to bare invocation on empty project.
- **`--init` + `'update'`:** `AskUserQuestion` — "Tokens exist. Back up + re-init?"
  - **Yes** → `backupAndOffer(path)` → Init (Step 3)
  - **No** → STOP, no writes. Report: "Existing tokens preserved."
- **`--init` + `'unreadable'`:** delegates to Step 1's `backupAndOffer` path (no double-prompt).
- **No args + no tokens:** Init (Step 3). *(Bare-invocation behavior unchanged.)*
- **No args + tokens exist:** Show summary + actions. *(Bare-invocation behavior unchanged.)*
- **`--discover`:** Step 4
- **`--export`:** Step 5
- **`--theme`:** Step 6

**`detectMode` interaction:** retains three-value return (`'init'` / `'update'` / `'unreadable'`). `--init` bypasses `'update'` (forces Init after backup confirmation); still honors `'unreadable'` for shared parse-error recovery.
### Step 3: Init Mode (Interactive)
Guided walkthrough (modeled on `/charter`). Category set is **fixed and auditable** — sourced from `.claude/metadata/design-system-init-categories.json` (schema: `design-system-init-categories.schema.json`). Two sessions MUST present identical option sets.

**Top-level (all four `alwaysSelected:true`):**
1. **Color palette** — primary, secondary, accent, neutral scale, semantic
2. **Typography** — families, size scale, weight scale
3. **Spacing** — base unit + scale
4. **Components** — aggregate of DTCG-common pattern tokens; subcategories selectable per `defaultOn`

**Component subcategories:**

| Subcategory | Default |
|---|---|
| Border radii | ON |
| Shadows / elevation | ON |
| Transitions (duration + easing) | ON |
| Z-index layers | ON |
| Breakpoints | ON |
| Icon sizes | ON |
| Opacity scale | OFF |
| Blur / filter | OFF |
| Motion / keyframes | OFF |
| Stroke widths | OFF |
| Grid tokens | OFF |

**Default rule:** ON when the pattern appears in the majority of mature web/mobile design systems (Material, Carbon, Polaris, Atlassian); OFF when specialized/advanced/domain-specific.

**`AskUserQuestion`:** read `.claude/metadata/design-system-init-categories.json` from disk, pass `componentPatterns` as options with `defaultOn` driving selection state. Do NOT improvise or reorder — the data file is authoritative. Parity test `tests/metadata/design-system-init-categories.test.js` enforces spec ↔ data sync.

Output: valid DTCG `idpf-design.tokens.json` + `idpf-design.schema.json`.

<!-- USER-EXTENSION-START: discovery-adapters -->
<!-- USER-EXTENSION-END: discovery-adapters -->

### Step 4: Discovery Mode
Load adapters from `Design-System/adapters/discovery/`:
1. Run each `detect(projectRoot)`
2. For `true`, run `extract(projectRoot)`
3. Merge discovered tokens
4. Present for confirmation/refinement
5. Write to `idpf-design.tokens.json`

No adapters detect → offer fallback to init.

<!-- USER-EXTENSION-START: export-adapters -->
<!-- USER-EXTENSION-END: export-adapters -->

### Step 5: Export Mode
Load `Design-System/adapters/export/`:
1. Read `idpf-design.tokens.json`
2. Run adapter `translate(dtcgTokens, options)`
3. Write outputs
4. Report generated files
### Step 6: Theme Mode
1. Generate `Design-System/themes/<name>.tokens.json`
2. Theme overrides base tokens with same `$type`
3. Validate type consistency
4. Check circular alias references
### Step 7: Validate and Write
1. Scaffold `Design-System/` structure
2. `dtcg-schema.generateSchema()`
3. `dtcg-schema.validateTokens()`
4. `dtcg-tokens.writeTokenFiles()`
5. Report: files created, token count, validation status
### Step 8: Token Change Propagation (AC36, AC37)
After write in update mode:
1. Compute `changedKeys` = paths whose `$value` differs between original and merged token tree.
2. `loadCatalog('Mockups/screen-catalog.json')` (from `.claude/scripts/shared/lib/screen-catalog.js`).
3. `findAffectedMockups(catalog, changedKeys)` from `.claude/scripts/shared/lib/token-propagation.js` — returns screens whose `tokenDependencies` (AC37) overlap.
4. Empty → "No affected mockups."
5. Non-empty → `AskUserQuestion` yes/no/select → `applyPropagationDecision` → regenerate via `/mockups` for each chosen screen (mockups remain stale on `no`).
---
## Directory Structure
```
Design-System/
  ├── idpf-design.tokens.json      (main DTCG token file)
  ├── idpf-design.schema.json      (generated JSON Schema)
  ├── themes/                       (theme override files)
  ├── adapters/
  │   ├── discovery/                (discovery adapters)
  │   └── export/                   (export adapters)
```
---
## Error Handling
| Error | Response |
|-------|----------|
| Invalid token file (parse) | Specific JSON error, offer regenerate |
| Schema validation failure | Errors with paths, offer fix |
| Adapter crash | Warn, continue with others |
| Unknown DTCG type | Error with valid type list |
| Circular alias | Report cycle path, block write |
### Step 9: Closing Cleanup
The prune is **part of** this step, and this step is **numbered** — what makes the claim hold. `One task per numbered step` now covers it, so an unpruned list surfaces as an unfinished task like any other step. The same claim as prose alone was overridden by the rules beside it (#2641).

**Prune the task list** (unconditional — every path, including early-exit paths where Phase 1 created tasks and later phases never ran):
1. `TaskList` — enumerate all tasks.
2. For every task owned by this `/design-system` invocation, `TaskUpdate status=deleted`.
3. Do **not** delete tasks created outside this invocation (user TODOs).

---
**End of /design-system Command**
