# Project Charter: GitHub Praxis Management Utility

**Status:** Active
**Last Updated:** 2026-11-16

## Vision

A GitHub CLI extension that streamlines Kanban-style GitHub Projects workflows by unifying issue tracking, project field values, sub-issue hierarchy, and workflow automation into a single cohesive tool.

## Problem Statement

Managing GitHub Projects through the `gh` CLI is fragmented. Project field updates, sub-issue relationships, and workflow automation require multiple tools or hand-written GraphQL queries. Developers need one interface that updates an issue and its project metadata in a single command.

## Target Users

- Developers using GitHub Projects for issue tracking
- Teams managing issue hierarchies (epics, stories, tasks)
- Solo developers and small teams wanting CLI-first project management
- AI-assisted development workflows driving the board from the terminal

## Tech Stack

| Layer | Technology |
|-------|------------|
| Language | Go |
| Framework | Cobra CLI |
| API | GitHub GraphQL |

## Key Entities

| Entity | Location |
|--------|----------|
| Internal packages | internal/ |
| E2E test files | test/e2e/ |
| CI workflows | .github/workflows/ |
| Design decision records | Construction/Design-Decisions/ |

## In Scope

- Project field management (status, priority, custom fields)
- Sub-issue hierarchy with progress tracking
- Batch operations (intake, triage, split, batch mutations)
- Workflow automation (branch tracking)
- Terminal Kanban board visualization
- Cross-repository issue operations
- Auto-create labels and custom fields
- Label management (sync, add, update, delete)
- Config integrity verification
- Status transition validation
- E2E test infrastructure
- Resilience to upstream GraphQL schema rollouts (cached-metadata fallback for ProjectV2 field resolver via `.gh-pmu.json`)
- Complete pagination for user-facing fetches (labels, comments, projects, project field values)
- Fail-loud error propagation across the cmd and API layers, with partial-failure exit codes
- Offline validation of GraphQL operations against a vendored GitHub schema
- Status field value reconcile (`status --update`; `init` adds missing required values)
- Custom field creation and listing (`field create`, `field list`)
- Issue comment, git history and piped-JSON filter utilities (`comment`, `history`, `filter`)
- Terms acceptance gate
- Organization-level issue fields alongside project fields
- Targeting another project board from create commands (`--project`)
- Checkbox gate markers for intentionally open acceptance criteria
- Dry-run previews for create and branch-start commands

## Out of Scope

- Web UI — gh-pmu is terminal-first
- Replacing the base `gh` issue and PR commands
- Multiple organizations' projects in one configuration
- Notifications

## Companion Repositories

Repositories this project works alongside. `Searchable` governs whether prior-art
sweeps read that repo's issues; `File Issues` governs whether `--target` may file
into it. Both default to false — nothing becomes filable by omission.

| Repository | Responsibility | Relationship | Repo | Board | Searchable | File Issues | Direction |
|---|---|---|---|---|---|---|---|
| idpf-praxis-dev | IDPF framework development — commands, rules, scripts and references deployed into this project | Upstream framework; gh-pmu consumes its releases through the hub | rubrical-worker/idpf-praxis-dev | rubrical-worker/7 | yes | yes | bidirectional |
| idpf-skills-dev | Claude Code skill development for the idpf-praxis-skills distribution repository; the skills this project installs per project | Upstream skills source; gh-pmu installs its published skills into this project | rubrical-works/idpf-skills-dev | rubrical-works/6 | yes | yes | upstream |
| idpf-phm-project-templates | Catalog of project templates (pre-filled charter, skill assignments, lock files, configuration) for new IDPF projects | Template source for IDPF project creation | rubrical-works/idpf-phm-project-templates | rubrical-works/7 | yes | yes | upstream |

---
*See Project/ for architecture, constraints and test strategy*
