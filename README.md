# gh-pmu

[![CI](https://github.com/rubrical-works/gh-pmu/actions/workflows/ci.yml/badge.svg)](https://github.com/rubrical-works/gh-pmu/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/rubrical-works/gh-pmu)](https://goreportcard.com/report/github.com/rubrical-works/gh-pmu)
[![Release](https://img.shields.io/github/v/release/rubrical-works/gh-pmu)](https://github.com/rubrical-works/gh-pmu/releases/latest)
[![Gosec](https://github.com/rubrical-works/gh-pmu/actions/workflows/gosec.yml/badge.svg)](https://github.com/rubrical-works/gh-pmu/actions/workflows/gosec.yml)
[![License](https://img.shields.io/github/license/rubrical-works/gh-pmu)](LICENSE)

**P**raxis **M**anagement **U**tility — a GitHub CLI extension for project workflows, sub-issue hierarchies, and batch operations.

> **Note:** gh-pmu is designed for **Kanban-style GitHub Projects** with status-based columns (Backlog, In Progress, In Review, Done). It integrates seamlessly with the [IDPF-Praxis](https://github.com/rubrical-works/idpf-praxis) framework for structured development workflows, but works standalone without any framework.

## Features

📋 **Project Management** - List, view, create, and update issues with project field values in one command

🔗 **Sub-Issue Hierarchy** - Create and manage parent-child issue relationships with progress tracking

⚡ **Batch Operations** - Intake untracked issues, triage with rules, split checklists into sub-issues

📊 **Board View** - Terminal Kanban board visualization

🚀 **Workflow Commands** - Branch management with artifact generation

🔄 **Cross-Repository** - Work with sub-issues across multiple repositories

## Installation

```bash
gh extension install rubrical-works/gh-pmu
```

## Upgrade

```bash
gh extension upgrade gh-pmu
```

## Quick Start

```bash
# Initialize configuration
gh pmu init

# List issues with project metadata
gh pmu list

# View issue with project fields and sub-issue progress
gh pmu view 42

# Update status
gh pmu move 42 --status in_progress

# Create sub-issue
gh pmu sub create --parent 42 --title "Subtask"

# Start a branch (release, patch, or feature workflow)
gh pmu branch start --name release/v1.2.0
gh pmu move 42 --branch current
gh pmu branch close
```

## Standalone Usage

`gh pmu` works as a standalone tool without any framework integration. The optional `framework` field in `.gh-pmu.json` enables workflow restrictions when used with process frameworks like [IDPF](https://github.com/rubrical-works/idpf-praxis).

**Standalone (default):**
- All commands work normally
- No workflow routing or approval gates
- Simple project management

**With framework integration:**
- Adds workflow restrictions and checkpoint discipline
- Structured patterns (epic→story hierarchies)
- Domain specialist role system

To use standalone, simply omit the `framework` field from your config:

```json
{
  "project": {
    "owner": "your-org",
    "number": 1
  },
  "repositories": ["your-org/your-repo"],
  "fields": {
    "status": {
      "values": {"backlog": "Backlog", "in_progress": "In progress", "in_review": "In review", "done": "Done"}
    },
    "priority": {
      "values": {"p0": "P0", "p1": "P1", "p2": "P2"}
    }
  }
}
```

## Documentation

| Guide | Description |
|-------|-------------|
| [Configuration](docs/configuration.md) | Setup `.gh-pmu.json`, field aliases, triage rules |
| [Commands](docs/commands.md) | Complete command reference with examples |
| [Sub-Issues](docs/sub-issues.md) | Parent-child hierarchies, epics, progress tracking |
| [Batch Operations](docs/batch-operations.md) | Intake, triage, and split workflows |
| [Workflows](docs/workflows.md) | Branch management |
| [gh vs gh pmu](docs/gh-comparison.md) | When to use each CLI |
| [Development](docs/development.md) | Building, testing, contributing |

## Commands

```
Project:    init, list, view, create, edit, comment, move, close, board, field
Sub-Issues: sub add, sub create, sub list, sub remove
Batch:      intake, triage, split
Labels:     label sync, label list, label add, label update, label delete
Workflows:  branch, validation, accept
Config:     config verify, status --update
Utilities:  filter, history
```

Run `gh pmu --help` for full command list.

## Unique Capabilities

Flags and features not available in base `gh` CLI:

| Command | Unique Flags | Purpose |
|---------|--------------|---------|
| `list` | `--status`, `--priority`, `--has-sub-issues` | Filter by project fields |
| `view` | `--body-file`, `--body-stdout`, `--comments` | Export body, show comments |
| `create` | `--status`, `--priority`, `--branch`, `--from-file` | Set project fields on create |
| `edit` | `--body-file`, `--body-stdin`, `--remove-label` | Round-trip body editing |
| `close` | `--update-status` | Move to 'done' before closing |
| `move` | `--recursive`, `--dry-run`, `--depth`, `--branch` | Cascade updates to sub-issues |
| `sub create` | `--inherit-labels`, `--inherit-milestone` | Inherit from parent issue |
| `split` | `--from`, `--dry-run` | Create sub-issues from checklist |
| `branch` | `start`, `add`, `close`, `reopen`, `--tag` | Branch-based deployment workflow |

See [gh vs gh pmu](docs/gh-comparison.md) for detailed comparison.

## Resilience

`gh pmu` is hardened against GitHub's `ProjectV2.fields` GraphQL resolver
returning HTTP 500 — a failure mode triggered when GitHub auto-provisions
new field types whose backing rows don't satisfy the existing
`ProjectV2FieldCommon` interface contract (observed 2026-05-14 with the
Created / Closed / Updated timestamp field rollout).

When the resolver crashes, `gh pmu` falls back to the `metadata.fields[]`
cache in `.gh-pmu.json` (populated by `gh pmu init`) and emits a single
stderr warning:

```
warning: ProjectV2 field resolver unavailable (GraphQL 5xx); using cached
field metadata from .gh-pmu.json (<N> fields).
```

Commands that translate field names → option IDs (`create --status`,
`move --priority`, `move --branch`, etc.) continue to work transparently.

Two flags tune the behaviour:

| Flag | Effect |
|------|--------|
| `--no-cache-fallback` | Disable the fallback. The resolver error propagates unchanged (preserves the prior fail-loud behaviour for CI / strict automation). |
| `--refresh-cached-fields` | When fallback engages, refresh each cached field via per-name `ProjectV2.field(name:)` queries (the resolver path that survives the rollout bug). Partial failures retain the cached value and log a count. |

`gh pmu init` has its own recovery path: when the bulk fetch crashes, init
falls back to a per-name lookup against a documented standard ProjectV2
field set (Title, Assignees, Status, Labels, Linked pull requests,
Milestone, Repository, Reviewers, Parent issue, Sub-issues progress,
Priority, Size, Estimate, Start date, Target date, Branch) so it can still
populate the cache with what's reachable. Re-run after GitHub recovers to
capture the full set.

## Framework Integration

`gh-pmu` is the backbone of project management for framework-driven development. When paired with a process framework like IDPF-Praxis, it provides:

- **Branch-tracked development** — every feature, release, and patch gets a tracked branch with automatic sub-issue management
- **Epic-to-story hierarchies** — epics decompose into stories, each tracked through the full lifecycle (Backlog → In Progress → In Review → Done)
- **Workflow enforcement** — status transition validation prevents accidental state changes and ensures work follows the defined process
- **Acceptance criteria gates** — issues cannot be closed until all acceptance criteria are checked, maintaining quality standards
- **Artifact generation** — release notes and changelogs are generated automatically from tracked issues

For teams using IDPF-Praxis for structured development, `gh-pmu` replaces the need for external project management tools by keeping everything in GitHub — issues, project boards, branch tracking, and release management — all accessible from the terminal.

## Attribution

This project builds upon work from [@yahsan2](https://github.com/yahsan2):
- [gh-pm](https://github.com/yahsan2/gh-pm)
- [gh-sub-issue](https://github.com/yahsan2/gh-sub-issue)

## License

MIT
