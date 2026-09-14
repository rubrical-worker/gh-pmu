# Project Charter: GitHub Praxis Management Utility

**Status:** Active
**Last Updated:** 2026-09-14

## Vision

A GitHub CLI extension that streamlines project workflows by unifying issue tracking, sub-issue hierarchy, and workflow automation into a single cohesive tool.

## Current Focus

v1.5.x - Stabilize the correctness and error-surfacing work shipped in v1.5.0 (2026-07-23)

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

## Key Entities

| Entity | Count | Location |
|--------|-------|----------|
| Internal packages | 8 | internal/ |
| E2E test files | 11 | test/e2e/ |
| CI workflows | 3 | .github/workflows/ |
| Design decision records | 14 | Construction/Design-Decisions/ |

---
*See Inception/ for full specifications*
