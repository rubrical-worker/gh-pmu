---
version: "v0.101.0"
description: Manage GitHub Actions CI workflows interactively (project)
argument-hint: "[list|validate|add|recommend] (no args shows status)"
copyright: "Rubrical Works (c) 2026"
---
<!-- EXTENSIBLE -->
# /ci
Interactive CI workflow management for GitHub Actions.
**Extension Points:** See `.claude/metadata/extension-points.json` or run `/extensions list --command ci`
## Prerequisites
- `.github/workflows/` directory (created if adding features)
- GitHub Actions enabled
## Arguments
| Argument | Description |
|----------|-------------|
| *(none)* | Show workflow status (default) |
| `list` | List available CI features |
| `validate` | Validate workflow YAML files |
| `add <feature>` | Add a CI feature to workflows |
| `recommend` | Analyze project and suggest improvements |
| `watch [--sha <commit>]` | Monitor CI run status for a commit |
## Subcommands
### `/ci` (no arguments) — View Workflow Status
Display summary of existing workflows: name, trigger events, OS targets, language versions in table format. Reports if no workflows directory.
```bash
node .claude/scripts/shared/ci-status.js
```
### `/ci list` — List Available CI Features
Lists all 11 CI features grouped by tier (v1 High Value, v2 Medium Value) with enabled/disabled status and one-line descriptions. Shows summary count.
```bash
node .claude/scripts/shared/ci-list.js
```
### `/ci validate` — Validate Workflow YAML
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
### `/ci add <feature>` — Add CI Feature

<!-- USER-EXTENSION-START: pre-add -->
<!-- USER-EXTENSION-END: pre-add -->

**Workflow:**
1. Validate feature name against `ci-features.json`
2. Detect project language via `ci-detect-lang.js`
3. Auto-detect target workflow file via `ci-detect-workflow.js`
4. Confirm target file with user before modifying
5. Add feature configuration via `ci-modify.js` (YAML-safe)
6. Create backup before modification
7. Report changes and target file

<!-- USER-EXTENSION-START: post-add -->
<!-- USER-EXTENSION-END: post-add -->

```bash
node .claude/scripts/shared/ci-add.js <feature>
```
### `/ci recommend` — Analyze and Recommend

<!-- USER-EXTENSION-START: pre-recommend -->
<!-- USER-EXTENSION-END: pre-recommend -->

**Workflow:**
1. Analyze project stack via `ci-analyze.js` (language, test tooling, build system, deployment targets)
2. Inventory existing workflows via `ci-recommend.js`
3. Compare to best practices, categorize findings as [Add], [Remove], [Alter], [Improve]
4. Present numbered selectable menu via `ci-recommend-ui.js`
5. Apply selected recommendations via `ci-apply.js`
6. Report summary

<!-- USER-EXTENSION-START: post-recommend -->
<!-- USER-EXTENSION-END: post-recommend -->

```bash
node .claude/scripts/shared/ci-analyze.js
node .claude/scripts/shared/ci-recommend.js
```
### `/ci watch` — Monitor CI Run
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
| `list` | Execute `ci-list.js` |
| `validate` | Execute `ci-validate.js` |
| `add <feature>` | Execute `ci-add.js <feature>` |
| `recommend` | Execute `ci-analyze.js` + `ci-recommend.js` flow |
| `watch [--sha X]` | Execute `ci-watch.js --sha X` (default: HEAD) |
| Other | Error: `Unknown subcommand: $1` |
### Step 4: Execute Handler
```bash
node .claude/scripts/shared/ci-status.js
```

<!-- USER-EXTENSION-START: custom-subcommands -->
<!-- Add your custom CI subcommands here -->
<!-- USER-EXTENSION-END: custom-subcommands -->

## Error Handling
| Situation | Response |
|-----------|----------|
| No `.github/workflows/` | "No .github/workflows/ directory found" |
| Empty workflows directory | "No workflow files found" |
| YAML parse error | Report file and error, continue with other files |
| Unknown subcommand | "Unknown subcommand: {name}. Use: ci, ci list, ci validate, ci watch" |

**End of /ci Command**
