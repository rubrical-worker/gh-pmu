---
version: "v0.116.0"
description: Manage GitHub Actions CI workflows interactively, using the IDPF framework.
argument-hint: "[--list|--validate|--add <feature> [--file <workflow>] [--paths <p1,p2>]|--recommend|--watch [--sha <commit>]] (no args shows status) [--help]"
copyright: "Rubrical Works (c) 2026"
---
<!-- EXTENSIBLE -->
# /ci
Interactive CI workflow management for GitHub Actions.
**Extension Points:** See `.claude/metadata/extension-points.json` or run `/extensions --list --command ci`
## Prerequisites
- `.github/workflows/` directory (created if adding features)
- GitHub Actions enabled
## Arguments
| Argument | Description |
|----------|-------------|
| *(none)* | Show workflow status (default) |
| `--list` | List available CI features |
| `--validate` | Validate workflow YAML files |
| `--add <feature> [--file <workflow>] [--paths <p1,p2>]` | Add a CI feature; `--file` names the workflow in `.github/workflows/` and skips auto-detection; `--paths` names `paths-ignore` patterns (comma-separated) instead of deriving them |
| `--recommend` | Analyze project and suggest improvements |
| `--watch [--sha <commit>]` | Monitor CI run status for a commit |
| `--help` | Open this command's docs page. Takes precedence over every other argument and performs no action. |

**`--help` takes precedence over every other argument and performs no action:** run `node .claude/scripts/shared/command-help.js ci`, report its output, and STOP.

**Retired forms:** `list`, `validate`, `add`, `recommend` and `watch` typed bare are no longer accepted — reject them with a message naming `--list`, `--validate`, `--add`, `--recommend` or `--watch`, and do nothing else.

## Subcommands
### `/ci` (no arguments) — View Workflow Status
Display summary of existing workflows: name, trigger events, OS targets, language versions in table format. Reports if no workflows directory.
```bash
node .claude/scripts/shared/ci-status.js
```
### `/ci --list` — List Available CI Features
Lists all CI features (21 in `ci-features.json`) grouped by tier (v1 High Value, v2 Medium Value) with enabled/disabled status and one-line descriptions. Shows summary count.
```bash
node .claude/scripts/shared/ci-list.js
```
### `/ci --validate` — Validate Workflow YAML
Validates all `.github/workflows/*.yml` files:
- YAML syntax errors
- Deprecated action versions (e.g., `checkout@v2`)
- Missing concurrency groups on PR workflows
- Hardcoded secrets/tokens
- Overly permissive permissions
Findings grouped by severity (error/warning/info).
```bash
node .claude/scripts/shared/ci-validate.js
```
### `/ci --add <feature>` — Add CI Feature

<!-- USER-EXTENSION-START: pre-add -->
<!-- USER-EXTENSION-END: pre-add -->

**Workflow:**
1. Validate feature name against `ci-features.json`
2. Detect project language via `ci-detect-lang.js`
3. Target workflow: the one `--file` names (must exist in `.github/workflows/`; no auto-detection), else `ci-detect-workflow.js` — for `trigger-scope`, the single workflow its classifier qualifies (several → refused, asks for `--file`)
4. Confirm target file with user before modifying
5. Add feature configuration via `ci-modify.js` (YAML-safe)
6. Create backup before modification
7. Report changes and target file

**Trigger features:**
- `paths-ignore`: patterns from the repo layout — doc dirs and IDPF artifact dirs present (`Proposal/`, `Construction/`, `Inception/`, `Mockups/`) plus root `'*.md'`; warns on any pattern (generated or existing) matching `.claude/commands/*.md` or other runtime markdown. Added only to existing `push`/`pull_request` events with no path filter — never adds a trigger. Nothing to ignore or no such event → refused, nothing written.
- `paths-ignore --paths '<p1>,<p2>'`: the named patterns instead of the layout, merged into each `push`/`pull_request` event's existing `paths-ignore` list without duplicates (existing entries kept), or a new list where the event has no path filter — never adds a trigger. An event with `paths:` refuses the whole edit, nothing written; a tag-only push is skipped and named. Same runtime-markdown warning; the result states what the patterns give up, including `/done` no longer waiting for CI on a push touching only them. `--paths` with any other feature → refused.
- `trigger-scope`: narrows an audit/scanner workflow to `paths:` (manifests/lockfiles, source dirs, the workflow file), weekly `schedule` and `workflow_dispatch`. Scheduled runs check the default branch only.

<!-- USER-EXTENSION-START: post-add -->
<!-- USER-EXTENSION-END: post-add -->

```bash
node .claude/scripts/shared/ci-add.js <feature> [--file <workflow>] [--paths <p1,p2>]
```
### `/ci --recommend` — Analyze and Recommend

<!-- USER-EXTENSION-START: pre-recommend -->
<!-- USER-EXTENSION-END: pre-recommend -->

**Workflow:**
1. Analyze project stack via `ci-analyze.js` (language, test tooling, build system, deployment targets)
2. Inventory workflows via `ci-recommend.js`, which first prints the per-push report:
   - **Jobs per branch push** — per workflow and total, matrix legs expanded (`include`/`exclude` applied); jobs whose `if:` excludes a branch push listed, not counted; named-branch workflows listed, not summed
   - **Duplicate suites** — a test command run as a full suite more than once per push (shard legs of one job = one run)
   - **Local verification set** — each job labeled duplicating the declared suites (`resolveSuites()`, `/work` Step 4f sweep) or adding signal, naming each: runner OS the local sweep lacks (Windows: CRLF checkout), command or action no local suite runs
   - **Advisories** — duplicate suites and duplicating jobs: hand edits with a suggestion, not menu items
3. Compare to best practices, categorize findings as [Add], [Remove], [Alter], [Improve]. `paths-ignore` offered per unfiltered `push`/`pull_request` event from the parsed `on:` block; `trigger-scope` for a workflow running on every branch push with no test step and an audit/scanner step (`npm audit`, `pip-audit`, `cargo audit`, `osv-scanner`, `semgrep`, `gitleaks`, CodeQL, Electronegativity)
   - **Every recommendation and advisory from this analysis states the CI signal it removes or defers** — its `tradeoff`, printed as `Gives up:` in report, menu and apply confirmation
4. Present numbered selectable menu via `ci-recommend-ui.js`
5. Apply selected recommendations via `ci-apply.js`
6. Report summary

<!-- USER-EXTENSION-START: post-recommend -->
<!-- USER-EXTENSION-END: post-recommend -->

```bash
node .claude/scripts/shared/ci-analyze.js
node .claude/scripts/shared/ci-recommend.js
```
### `/ci --watch` — Monitor CI Run
Monitor a workflow run by commit SHA; report structured results.
| Argument | Required | Default | Description |
|----------|----------|---------|-------------|
| `--sha <commit>` | No | `HEAD` | Commit SHA to monitor |
| `--timeout <seconds>` | No | `300` | Max wait time |
| `--poll <seconds>` | No | `15` | Polling interval |
**Workflow:**
1. If no `--sha`, use `git rev-parse HEAD`
2. Run `ci-watch.js` and display results
3. Report per-workflow conclusion with exit code
```bash
node .claude/scripts/shared/ci-watch.js --sha $SHA [--timeout $TIMEOUT] [--poll $POLL]
```
**Exit codes:** 0=pass, 1=fail, 2=timeout, 3=no-run-found, 4=cancelled
## Execution Instructions
### Step 1: Parse Subcommand
```bash
SUBCOMMAND="${1:-status}"
```
### Step 2: Verify CI Scripts Installed
```bash
ls .claude/scripts/shared/ci-status.js 2>/dev/null
```
**If script does not exist:**
```
CI scripts not installed. The /ci command requires the ci-cd-pipeline-design skill.

To install: /install-skill ci-cd-pipeline-design
To set up CI manually: create .github/workflows/ and add workflow YAML files.
```
→ **STOP** (do not attempt missing scripts)
### Step 3: Route to Handler
| Subcommand | Action |
|------------|--------|
| *(none)* or `status` | Execute `ci-status.js` |
| `--list` | Execute `ci-list.js` |
| `--validate` | Execute `ci-validate.js` |
| `--add <feature> [--file X] [--paths P]` | Execute `ci-add.js <feature> [--file X] [--paths P]` |
| `--recommend` | Execute `ci-analyze.js` + `ci-recommend.js` flow |
| `--watch [--sha X]` | Execute `ci-watch.js --sha X` (default: HEAD) |
| Other | Error: `Unknown subcommand: $1` |
### Step 4: Execute Handler
```bash
node .claude/scripts/shared/ci-status.js
```

<!-- USER-EXTENSION-START: custom-subcommands -->
<!-- USER-EXTENSION-END: custom-subcommands -->

## Error Handling
| Situation | Response |
|-----------|----------|
| No `.github/workflows/` | "No .github/workflows/ directory found" |
| Empty workflows directory | "No workflow files found" |
| YAML parse error | Report file and error, continue with other files |
| Unknown subcommand | "Unknown subcommand: {name}. Use: /ci, /ci --list, /ci --validate, /ci --add, /ci --recommend, /ci --watch" |

**End of /ci Command**
