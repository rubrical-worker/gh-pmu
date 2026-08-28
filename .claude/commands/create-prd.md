---
version: "v0.98.0"
description: Transform proposal into Agile PRD
argument-hint: "<issue-number> | extract [<directory>]"
copyright: "Rubrical Works (c) 2026"
---
<!-- EXTENSIBLE -->
# /create-prd
Transform a proposal document into an Agile PRD with user stories, acceptance criteria, and epic groupings.
**Extension Points:** `.claude/metadata/extension-points.json` or `/extensions list --command create-prd`

## Prerequisites
Load shared from `.claude/metadata/command-boilerplate.json` -> `prerequisites.common`. **Graceful degradation when boilerplate not found:** use defaults — `gh pmu` installed, `.gh-pmu.json` configured.
**Command-specific:** proposal issue with `proposal` label; body links `Proposal/[Name].md`; document exists in `Proposal/`; (Recommended) `CHARTER.md` + `Inception/`.

## Arguments
| Argument | Description |
|----------|-------------|
| `<issue-number>` | Proposal issue (`123` or `#123`) |
| `extract` | Extract PRD from codebase (requires `/charter`) |
| `extract <directory>` | Extract from specific directory |
| `--assignee <value>` | GitHub login for the new issue. Omitted → `@me`. |

## Modes
| Mode | Invocation | Description |
|------|------------|-------------|
| **Issue-Driven** | `/create-prd 123` | Transform proposal to PRD |
| **Extract** | `/create-prd extract [src/]` | Extract PRD from codebase |
| **Interactive** | `/create-prd` | Prompt for mode selection |

## Execution Instructions
**REQUIRED:** Load from `.claude/metadata/command-boilerplate.json` -> `executionInstructions.steps` and `executionInstructions.todoRules`. Defaults if missing: generate TaskCreate tasks from phases/steps, include extension point todos, track progress, re-read spec after compaction.

## Workflow (Issue-Driven Mode)

### Phase 1: Fetch Proposal from Issue
**Step 1: Parse issue number**
```bash
issue_num="${1#\#}"
```
**Step 2: Fetch and validate**
```bash
gh issue view $issue_num --json labels,body --jq '.labels[].name' | grep -q "proposal"
```
If not a proposal issue, error: `Issue #$issue_num does not have the 'proposal' label.` Tell user to create with `proposal: <description>`.

**Step 3: Extract proposal document path** — pattern `/Proposal\/[A-Za-z0-9_-]+\.md/`. If not found, error: `Could not find proposal document link in issue #$issue_num. Expected: File: Proposal/[Name].md`.

**Step 4: Load context files**
| File | Required | Purpose |
|------|----------|---------|
| `<extracted-proposal-path>` | Yes | Source proposal |
| `CHARTER.md` | Recommended | Project scope validation |
| `Inception/Scope-Boundaries.md` | Recommended | In/out of scope |
| `Inception/Constraints.md` | Optional | Technical constraints |
| `Inception/Architecture.md` | Optional | System architecture |

**Load Anti-Hallucination Rules:**
| Context | Rules Path |
|---------|------------|
| All projects | `{frameworkPath}/Assistant/Anti-Hallucination-Rules-for-PRD-Work.md` |

<!-- USER-EXTENSION-START: pre-analysis -->
<!-- USER-EXTENSION-END: pre-analysis -->

### Phase 2: Validate Against Charter
| Finding | Action |
|---------|--------|
| Aligned | Proceed |
| Possibly misaligned | Ask for confirmation |
| Conflicts with out-of-scope | Flag, offer resolution |

**Resolution Options:** 1) Expand charter scope; 2) Defer to future release; 3) Proceed anyway (creates drift); 4) Revise proposal.

### Phase 3: Analyze Proposal Gaps
| Element | Detection Patterns | Gap Action |
|---------|-------------------|------------|
| Problem statement | "Problem:", "Issue:", first paragraph | Ask if missing |
| Proposed solution | "Solution:", "Approach:" | Ask if missing |
| User stories | "As a...", "User can..." | Generate questions |
| Acceptance criteria | "- [ ]", "Done when" | Generate questions |
| Priority | "P0-P3", "High/Medium/Low" | Ask if missing |

<!-- USER-EXTENSION-START: post-analysis -->
<!-- USER-EXTENSION-END: post-analysis -->

### Phase 3.5: Extract Path Analysis (if present)
Check proposal for `## Path Analysis`. **If exists**, extract paths per category to inform PRD:
| Path Category | Informs |
|---------------|---------|
| Exception Paths | Error handling ACs |
| Edge / Corner Cases | Boundary-condition ACs |
| Negative Test Scenarios | Test plan negative cases |
| Nominal Path | Primary user story flow |
| Alternative Paths | Alternative flow ACs |
**Process:** parse each `###` under `## Path Analysis`; extract numbered items as scenarios; store by category for Phase 4.5 / 6.5. Missing: proceed — non-blocking.

### Phase 3.6: Extract Screen Spec References (if present)
**If `## Screen Specs`:** parse refs (e.g. `Screen-Specs/{Screen-Name}.md`); read each; use element data (field names, types, validation, defaults) to inform Phase 4.5 ACs.
**If `## Mockups`:** parse refs (e.g. `Mockups/{Screen-Name}-mockup.md`); note availability for cross-reference.
**Consumption only** — `/create-prd` reads refs, does not discover/create. Missing files: warn, continue. Neither section present: proceed — non-blocking.

### Phase 4: Dynamic Question Generation
Context-aware questions for missing elements. Reference specific proposal details; only ask truly missing items; allow "skip"/"not sure"; 3-5 questions at a time.

<!-- USER-EXTENSION-START: pre-transform -->
<!-- USER-EXTENSION-END: pre-transform -->

### Phase 4.5: Story Transformation
Transform proposal requirements into Agile stories: identify USER, CAPABILITY, BENEFIT, then transform.
**Anti-Pattern:** flag implementation details (file operations, internal changes, code-level) and move to Technical Notes.

<!-- USER-EXTENSION-START: post-transform -->
<!-- USER-EXTENSION-END: post-transform -->

#### Solo-Mode Epic Preference
Check `reviewMode` from `framework-config.json`:
```javascript
const { getReviewMode } = require('./.claude/scripts/shared/lib/review-mode.js');
const mode = getReviewMode(process.cwd(), null);
```
| Mode | Behavior |
|------|----------|
| `solo` | Prompt user: consolidate into single epic? |
| `team` / `enterprise` | No prompt — standard multi-epic grouping |
**Solo prompt** (via `AskUserQuestion`):
```javascript
AskUserQuestion({
  questions: [{
    question: "Solo mode detected. Group all stories under a single epic for simplicity? (Or keep multiple epics for planned workstream use)",
    header: "Epic structure",
    options: [
      { label: "Single epic (Recommended)", description: "Consolidate all stories under one epic — simpler for solo dev" },
      { label: "Keep multiple epics", description: "Standard multi-epic grouping (e.g., for concurrent workstreams)" }
    ],
    multiSelect: false
  }]
});
```
Confirmed: consolidate to 1 epic, title from proposal name (e.g. "Epic 1: {Feature Name}"), stories become Story 1.1, 1.2, ... Declined: standard multi-epic grouping. `team`/`enterprise`: skip entirely.
### Phase 4.6: AC Feasibility Gate (#2425)
Re-read `.claude/metadata/ac-feasibility-prompts.json`. For each AC produced in Phase 4.5, apply `classify`, `verificationGate`, `deliverableSplit` prompts. On `verificationGate.triggerPhrases` match + no existing test using that mechanism (Grep), **ASK USER**: (a) rewrite, (b) feasibility spike + issue, or (c) weaker mechanism. **Do not proceed to Phase 5 until each flagged AC resolved.** Trigger list heuristic; prompts load-bearing. No `prdLevel` block needed — existing three prompts handle coarse ACs.

### Phase 5: Priority Validation
| Priority | Distribution |
|----------|--------------|
| P0 (Must) | <=40% |
| P1 (Should) | 30-40% |
| P2 (Could) | >=20% |
**Small PRD Exemption:** skip for <6 stories.

<!-- USER-EXTENSION-START: pre-diagram -->
<!-- USER-EXTENSION-END: pre-diagram -->

### Phase 5.5a: Diagram Style Selection
```javascript
AskUserQuestion({
  questions: [{
    question: "Which diagram style should this PRD use?",
    header: "Diagram style",
    options: [
      { label: "drawio (Rich SVG)", description: "Generate .drawio.svg files — editable in draw.io, rich visuals, stored in Diagrams/" },
      { label: "ASCII (Text-based UML)", description: "Text-based UML inline in PRD markdown — renders everywhere, clean diffs, no external tooling" }
    ],
    multiSelect: false
  }]
});
```
Store selection for Phase 5.5b.

### Phase 5.5b: Diagram Generation
| Type | Default | When |
|------|---------|------|
| Use Case | ON | User-facing features |
| Activity | ON | Multi-step workflows |
| Sequence | OFF | API/service interactions |
| Class | OFF | Data models, entities |
| Component | OFF | System architecture |
| State | OFF | State machines |
**drawio style:** load `.claude/skills/drawio-generation/SKILL.md`. Generate UML as `.drawio.svg` at `PRD/{PRD-Name}/Diagrams/{Epic-Name}/{type}-{description}.drawio.svg`.
**ASCII style:** generate UML **inline** in PRD markdown with box-drawing characters. Rules: wrap in ` ```text ... ``` ` for monospace; no plain-ASCII substitutes (`+`, `-`, `|`); proper monospace alignment (one col per char); place under `### Diagrams` per epic. No `Diagrams/` directory — all inline.
**ASCII templates:**
| Type | Key Elements |
|------|-------------|
| Use Case | Actors (stick figure), ellipses, system boundary |
| Activity | Start/end nodes, action boxes, decision diamonds, arrows |
| Sequence | Participant boxes, lifelines, arrows, activation bars |
| Class | Class boxes with compartments (name, attributes, methods) |
| Component | Component boxes with stereotype, interfaces |
| State | State boxes, transitions with labels, start/end markers |

<!-- USER-EXTENSION-START: diagram-generator -->
<!-- USER-EXTENSION-END: diagram-generator -->

<!-- USER-EXTENSION-START: post-diagram -->
<!-- USER-EXTENSION-END: post-diagram -->

<!-- USER-EXTENSION-START: pre-generation -->
<!-- USER-EXTENSION-END: pre-generation -->

### Phase 6: Generate PRD
Structure: `PRD/{PRD-Name}/PRD-{PRD-Name}.md` with `Diagrams/{Epic-Name}/{type}-{description}.drawio.svg` under it (drawio only). ASCII style: diagrams inline, no `Diagrams/`. Flat legacy PRDs (`PRD/PRD-{name}.md`) grandfathered.
Create PRD at `PRD/{name}/PRD-{name}.md`. Load template `{frameworkPath}/Templates/artifacts/prd-template.md` and populate. **Graceful degradation:** template missing — warn `"PRD template file missing, using inline fallback."`, use sections: Overview, Epics, User Stories, Diagrams, Technical Notes, Out of Scope, Dependencies, Open Questions.

<!-- USER-EXTENSION-START: post-generation -->
<!-- USER-EXTENSION-END: post-generation -->

<!-- USER-EXTENSION-START: quality-checklist -->
<!-- USER-EXTENSION-END: quality-checklist -->

### Phase 6.5: Generate TDD Test Plan
**Step 1: Load test configuration**
| Source | Data |
|--------|------|
| `Inception/Test-Strategy.md` | Test framework, coverage targets, TDD philosophy |
| `Inception/Tech-Stack.md` | Language (test syntax) |
**Fallback if Test-Strategy.md missing:** check `{frameworkPath}/IDPF-Agile/Agile-Core.md` TDD Cycle section; warn `"No Test-Strategy.md found. Using framework defaults. Run /charter to customize."`; defaults 80% unit coverage, framework "TBD".
**Step 2: Generate** `PRD/{name}/Test-Plan-{name}.md`. Load template `{frameworkPath}/Templates/artifacts/test-plan-template.md`; populate. **Graceful degradation:** template missing — warn `"Test plan template file missing, using inline fallback."`, sections: Source, Test Strategy Overview, Epic Test Coverage, Integration Test Points, E2E Scenarios, Coverage Targets, Approval Checklist.
**Derivation:** parse each story's ACs; generate 2-3 test cases per AC (valid, invalid, edge); identify cross-story/cross-epic integration points; extract E2E scenarios from user journeys.

### Phase 6.6: Create Test Plan Approval Issue
**Assignee:** substitute `{assignee}` from `node .claude/scripts/shared/lib/gh-pmu-config.js --assignee <value>` — pass the user's `--assignee` value, omit when none given. Helper returns that login, else `@me`; reads no config file. NEVER hardcode a login or drop the flag (omitted `--assignee` silently creates an unassigned issue). Unresolvable login → `gh pmu` exits 1 and creates nothing; report the error, do NOT retry without the flag. Applies to both `gh pmu create` calls below.
```bash
gh pmu create --label test-plan --label approval-required --assignee {assignee} \
  --title "Approve Test Plan: {Name}" \
  --body "## Test Plan Review

A TDD test plan has been generated for **{Name}**.

**Test Plan:** PRD/{name}/Test-Plan-{name}.md
**PRD:** PRD/{name}/PRD-{name}.md

## Review Checklist

- [ ] Test cases cover all acceptance criteria
- [ ] Edge cases and error scenarios included
- [ ] Integration test points are complete
- [ ] E2E scenarios cover critical paths
- [ ] Coverage targets are appropriate

## Instructions

1. Review the test plan document
2. Check all boxes above when satisfied
3. Comment with any required changes
4. Close this issue to approve

**⚠️ Create-Backlog is blocked until this issue is closed.**" \
  --status backlog
```
Update test plan frontmatter with the approval issue number after creation.

### Phase 7: Proposal Lifecycle Completion
**Only Issue-Driven Mode.**

**Step 1: Move proposal document** — check git tracking first.
```bash
git ls-files --error-unmatch Proposal/{Name}.md 2>/dev/null
git add Proposal/{Name}.md       # if untracked
git mv Proposal/{Name}.md Proposal/Implemented/{Name}.md
```
`git ls-files` succeeds = tracked, skip `git add`; fails = untracked, `git add` before `git mv`.

**Step 2: Close proposal issue**
```bash
gh issue close $issue_num --comment "Transformed to PRD: PRD/{name}/PRD-{name}.md"
gh pmu move $issue_num --status done
```

**Step 3: Create PRD tracking issue**
```bash
gh pmu create --label prd --assignee {assignee} \
  --title "PRD: {Name}" \
  --body "## PRD Document

**File:** PRD/{name}/PRD-{name}.md
**Test Plan:** PRD/{name}/Test-Plan-{name}.md
**Source Proposal:** #$issue_num (closed)

## Status

- [ ] PRD reviewed
- [ ] Test plan approved (see #{test_plan_issue})
- [ ] Ready for backlog creation

## Next Step

1. Review and close test plan approval issue: #{test_plan_issue}
2. Run: \`/create-backlog {this-issue-number}\`" \
  --status backlog
```

**Step 4: Commit generated artifacts** — atomic commit of PRD + proposal move after Steps 1-3 so a single commit captures durable state.
```bash
git add PRD/{name}/
# Step 1 `git mv` staged the deletion; stage dest too in case source was untracked.
[ -f Proposal/Implemented/{Name}.md ] && git add Proposal/Implemented/{Name}.md

if git diff --cached --quiet; then
  echo "Nothing to commit — PRD artifacts already tracked identically."
  commit_sha=""
else
  git commit -m "Refs #$issue_num — generate PRD + Test Plan, move proposal to Implemented"
  commit_sha="$(git rev-parse HEAD)"
fi
```
**Edge cases:**
| Situation | Response |
|-----------|----------|
| No changes to commit | `git diff --cached --quiet` short-circuits; skip, continue — non-blocking |
| Unrelated staged changes | Use explicit paths — never `git add .` |
| Proposal already in `Implemented/` | Step 1 skipped `git mv`; commit covers only `PRD/{name}/` |
| Diagrams (drawio) | Already under `PRD/{name}/Diagrams/`, picked up by `git add PRD/{name}/` |
| `git commit` fails | Surface error; on disk complete but uncommitted — do NOT roll back |
**Message discipline:** `Refs #$issue_num` (not `Fixes`/`Closes`/`Resolves`) per `.claude/rules/02-github-workflow.md` — Step 2 already closed the proposal via `gh issue close`.

**Step 5: Report completion**
```
PRD: PRD/{name}/PRD-{name}.md | Test Plan: PRD/{name}/Test-Plan-{name}.md
Proposal archived: Proposal/Implemented/{Name}.md | Proposal issue #{issue_num} closed
PRD tracker issue: #{prd_issue_num} | Test plan approval issue: #{test_plan_issue_num}
Diagrams: PRD/{name}/Diagrams/ (if generated)
Committed: {commit_sha} (empty if skipped)
⚠️ Approve test plan (#{test_plan_issue_num}) before running /create-backlog
Next: /create-backlog {prd_issue_num}
```

## Interactive Mode
For `/create-prd` (no arguments): prompt `1. From a proposal issue (enter issue number)` / `2. From existing code (extraction)`. Option 1: prompt for issue number, run Issue-Driven workflow.

## Workflow (Extract Mode)
For `/create-prd extract` or `/create-prd extract <directory>`:

### Step 1: Check Prerequisites
Verify `.claude/skills/codebase-analysis/SKILL.md` exists. Check `Inception/`.
**If skill missing:** `codebase-analysis skill not installed. Install via px-manager or ask user to install.` -> **STOP**
**If `Inception/` missing:** warn; offer `/charter` (non-blocking).

### Step 2: Load Skill
Read `.claude/skills/codebase-analysis/SKILL.md` for analysis capabilities and workflow.

### Step 3: Run Codebase Analysis
Delegate to codebase-analysis skill (entire project or specified directory). Skill handles tech stack, architecture inference, test parsing, NFR detection.

### Step 4: Bridge to Phase 6
Use skill output to generate PRD via Phase 6. Same diagram selection (5.5a) and generation (5.5b) as Issue-Driven. Present extracted features with confidence levels for user selection before generation.

### Step 5: Add Extraction Metadata
Augment Phase 6 output: confidence levels per story, extraction metadata section, evidence citations per feature.

### Step 6: Commit generated PRD
Extract Mode has no proposal to move and no PRD tracker at commit time — commit scope is PRD directory only.
```bash
git add PRD/{name}/

if git diff --cached --quiet; then
  echo "Nothing to commit — PRD artifacts already tracked identically."
  commit_sha=""
else
  # No tracker issue yet — reference extraction source path instead of `Refs #`.
  git commit -m "Add extracted PRD: {name} (source: {extract_path})"
  commit_sha="$(git rev-parse HEAD)"
fi
```
**Why no `Refs #`:** Extract Mode runs without upstream proposal issue. PRD tracker (if manually created later) can be linked via follow-up commit. Commit unconditional (no flag to suppress) — `git reset HEAD~1` if user wants to regroup.

## Error Handling
| Situation | Response |
|-----------|----------|
| Issue not found | "Issue #N not found. Check the issue number?" |
| Missing proposal label | "Issue #N does not have 'proposal' label." |
| No proposal path in body | "Could not find proposal document link in issue body." |
| Proposal file missing | "Proposal not found at <path>. Check the file exists?" |
| No Inception/ artifacts | "No charter context. Proceeding with limited validation." |
| User skips all questions | "Insufficient detail. Add more to proposal first?" |
| Empty proposal | "Proposal needs more detail. Minimum: problem + solution." |

## Quality Checklist
- [ ] All user stories have acceptance criteria
- [ ] Requirements prioritized (P0-P2)
- [ ] Priority distribution valid (or <6 stories)
- [ ] Technical Notes separated from stories
- [ ] Out of scope explicitly stated
- [ ] Open questions flagged
- [ ] PRD is Create-Backlog compatible
### Step 8: Closing Cleanup
The prune is **part of** this step, and this step is **numbered** — what makes the claim hold. `One task per numbered step` now covers it, so an unpruned list surfaces as an unfinished task like any other step. The same claim as prose alone was overridden by the rules beside it (#2641).

**Prune the task list** (unconditional — every path, including early-exit paths where Phase 1 created tasks and later phases never ran):
1. `TaskList` — enumerate all tasks.
2. For every task owned by this `/create-prd` invocation, `TaskUpdate status=deleted`.
3. Do **not** delete tasks created outside this invocation (user TODOs).

**End of /create-prd Command**
