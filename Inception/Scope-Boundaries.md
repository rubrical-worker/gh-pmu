# Scope Boundaries: gh-pmu

**Last Updated:** 2026-10-19

---

## In Scope (Current Release)

### Features

- [x] **Project Management**: Initialize, list, view, create, edit, move, close issues with project fields
- [x] **Sub-Issue Hierarchy**: Parent-child relationships with progress tracking
- [x] **Batch Operations**: Intake untracked issues, triage with rules, split checklists
- [x] **Board View**: Terminal Kanban visualization
- [x] **Workflow Commands**: Branch-based development workflows
- [x] **Label Management**: Sync, add, update, delete repository labels
- [x] **Config Integrity**: Checksum-based config drift detection
- [x] **Status Validation**: Transition rules for workflow enforcement
- [x] **Terms Gate**: Acceptance gate for CLI usage
- [x] **Complete Pagination**: Labels, comments, projects, and project field values fetch without silent truncation
- [x] **Fail-Loud Errors**: cmd and API layers propagate failures with partial-failure exit codes
- [x] **Schema Resilience**: Cached-metadata fallback when the ProjectV2 fields resolver is unavailable
- [x] **Status Field Reconcile**: `status --update` adds missing required Status values; `init` adds them on setup
- [x] **Custom Fields**: Create and list project fields
- [x] **Utilities**: Issue comments, git history with issue references, filtering piped issue JSON by project fields

### Capabilities

| Capability | Priority | Status |
|------------|----------|--------|
| Issue CRUD with project fields | P0 | Done |
| Sub-issue hierarchy | P0 | Done |
| Terminal Kanban board | P1 | Done |
| Batch intake/triage | P1 | Done |
| Branch workflow | P1 | Done |
| Label management | P1 | Done |
| Config integrity verification | P2 | Done |
| Status transition validation | P2 | Done |
| Field value aliases | P2 | Done |
| Cross-repository sub-issues | P2 | Done |
| Complete pagination for user-facing fetches | P0 | Done |
| Fail-loud error propagation | P0 | Done |
| Cached-metadata fallback for field resolver | P1 | Done |
| Offline GraphQL validation against vendored schema | P2 | Done |
| Status field value reconcile | P1 | Done |
| Custom field create/list | P2 | Done |
| Comment, history and filter utilities | P2 | Done |

---

## Out of Scope

### Explicitly Excluded

| Item | Reason | Future Consideration? |
|------|--------|----------------------|
| Web UI | Terminal-first philosophy | No |
| Multi-org projects | Complexity, limited demand | Maybe |
| Notifications | Not project management | No |

### Deferred to Future Releases

| Item | Target Release | Dependencies |
|------|----------------|--------------|
| Custom field types | TBD | GitHub API support |
| Project templates | TBD | User feedback |
| Metrics/velocity | TBD | Data collection |

---

## User Workflows

### Primary Workflow

**Name:** Issue Lifecycle Management

1. User runs `gh pmu list --status backlog`
2. System displays issues with project metadata
3. User runs `gh pmu move 42 --status in_progress`
4. System updates project field, confirms change
5. User completes work, runs `gh pmu move 42 --status done`
6. System moves to Done, auto-closes issue

### Secondary Workflows

| Workflow | Description | Priority |
|----------|-------------|----------|
| Sub-issue management | Create/track child issues | P0 |
| Branch management | Branch-based deployments | P1 |
| Batch triage | Bulk status updates | P2 |

---

## Boundaries with External Systems

| System | We Handle | They Handle |
|--------|-----------|-------------|
| GitHub Issues | Project field updates, sub-issue linking | Issue storage, notifications |
| GitHub Projects | Field mutations via GraphQL | UI, views, automations |
| gh CLI | Extension commands | Base issue/PR commands |

---

## Scope Change Process

Changes to scope require:
1. Update this document with proposed changes
2. Update CHARTER.md if vision changes
3. Create GitHub issue for tracking
4. Update documentation after implementation

---

*See also: Charter-Details.md, Milestones.md*
