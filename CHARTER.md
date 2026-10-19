# Project Charter: GitHub Praxis Management Utility

**Status:** Active
**Last Updated:** 2026-10-19

## Vision

A GitHub CLI extension that streamlines project workflows by unifying issue tracking, sub-issue hierarchy, and workflow automation into a single cohesive tool.

## Current Focus

v1.6.x - Stabilize v1.6.0 (2026-09-14): faster batched intake, Status field reconcile (`status --update`), and terms acceptance that persists across 1.x releases

## Tech Stack

| Layer | Technology |
|-------|------------|
| Language | Go 1.23 |
| Framework | Cobra CLI |
| API | GitHub GraphQL (go-gh, shurcooL-graphql) |

## In Scope (Current)

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

## Key Entities

| Entity | Count | Location |
|--------|-------|----------|
| Internal packages | 8 | internal/ |
| E2E test files | 11 | test/e2e/ |
| CI workflows | 3 | .github/workflows/ |
| Design decision records | 14 | Construction/Design-Decisions/ |

## Companion Repositories

Repositories this project works alongside. `Searchable` governs whether prior-art
sweeps read that repo's issues; `File Issues` governs whether `--target` may file
into it. Both default to false — nothing becomes filable by omission.

| Repository | Responsibility | Relationship | Repo | Board | Searchable | File Issues |
|---|---|---|---|---|---|---|
| idpf-praxis-dev | IDPF framework development — commands, rules, scripts and references deployed into this project | Upstream framework; gh-pmu consumes its releases through the hub | rubrical-worker/idpf-praxis-dev | rubrical-worker/7 | yes | yes |
| idpf-skills-dev | Claude Code skill development for the idpf-praxis-skills distribution repository; the skills this project installs per project | Upstream skills source; gh-pmu installs its published skills into this project | rubrical-works/idpf-skills-dev | rubrical-works/6 | yes | yes |
| idpf-phm-project-templates | Catalog of project templates (pre-filled charter, skill assignments, lock files, configuration) for new IDPF projects | Template source for IDPF project creation | rubrical-works/idpf-phm-project-templates | rubrical-works/7 | yes | yes |

---
*See Inception/ for full specifications*
