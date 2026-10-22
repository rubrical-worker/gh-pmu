# Project Charter Details: gh-pmu

**Generated:** 2026-01-04
**Last Updated:** 2026-10-19

---

## 1. Vision (Full)

### Problem Statement

Managing GitHub Projects through the `gh` CLI is fragmented. Project field updates, sub-issue relationships, and workflow automation require multiple tools or manual GraphQL queries. Developers need a unified interface to streamline issue tracking with project metadata in a single command.

### Target Users

- Developers using GitHub Projects for issue tracking
- Teams managing complex issue hierarchies (epics, stories, tasks)
- Solo developers and small teams wanting CLI-first project management
- AI-assisted development workflows (Claude Code, Copilot Workspace)

### Value Proposition

- **Unified commands**: Combine issue operations with project field updates
- **Sub-issue hierarchy**: Native parent-child relationships with progress tracking
- **Batch operations**: Automate intake, triage, and issue splitting
- **Workflow automation**: Branch-based development workflows with artifact generation
- **Terminal-native**: Kanban board visualization without leaving the CLI

### Success Criteria

- Extension installed via `gh extension install`
- Reduces project management commands by 50%+ vs raw `gh` CLI
- Sub-issue hierarchy visible in GitHub Projects UI
- Branch workflows generate standard release artifacts

---

## 2. Scope (Full)

### In Scope (Current Release)

- [x] Project initialization and configuration
- [x] Issue CRUD with project field support
- [x] Sub-issue hierarchy (add, create, list, remove)
- [x] Batch operations (intake, triage, split)
- [x] Terminal Kanban board
- [x] Branch workflow with artifact generation
- [x] Label management (sync, add, update, delete)
- [x] Config integrity verification
- [x] Status transition validation
- [x] Terms acceptance gate
- [x] Status field value reconcile
- [x] Custom field create/list
- [x] Comment, history and filter utilities

### Out of Scope

- GitHub Issues API replacement (uses native `gh` for issue operations)
- Web UI (terminal-only)
- Multi-organization support in single config

### Key User Workflows

1. **Issue tracking**: `gh pmu list` → `gh pmu view 42` → `gh pmu move 42 --status done`
2. **Sub-issue management**: `gh pmu sub create --parent 42 --title "Subtask"` → `gh pmu sub list 42`
3. **Branch workflow**: `gh pmu branch start --name release/v2.0.0` → `gh pmu move 42 --branch current` → `gh pmu branch close`

---

## 3. Technical Approach (Full)

### Technology Stack

| Layer | Technology | Rationale |
|-------|------------|-----------|
| Language | Go 1.23 | CLI performance, static binary, `gh` ecosystem |
| Framework | Cobra | Standard for Go CLI applications |
| API | GitHub GraphQL | Project field mutations require GraphQL |
| Config | JSON | Machine-friendly, git-friendly |

### Architecture Style

CLI Extension - Single binary that extends GitHub CLI (`gh`) with project management commands.

### Key Technical Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| GraphQL vs REST | GraphQL | Projects v2 requires GraphQL for mutations |
| Config format | JSON | `.gh-pmu.json` — YAML support removed in v1.4.x |
| Alias system | Field value aliases | User-friendly status names (e.g., `in_progress` vs `47fc9ee4`) |

### Technical Risks

| Risk | Probability | Mitigation |
|------|-------------|------------|
| GitHub API changes | Medium | Version pinning, integration tests |
| Rate limiting | Low | Batched queries, caching |

---

## 4. Quality Approach

### Testing Strategy

- **Unit tests:** Table-driven tests for command parsing, validation
- **Integration tests:** Mock API client, end-to-end command execution
- **UAT tests:** Epic-based user acceptance scenarios

### Coverage Target

CI enforces a 15% total line-coverage floor (`.github/workflows/ci.yml`, `THRESHOLD=15`); 70%+ remains the aspiration (see `coverage.html`)

### Definition of Done

A feature is complete when:
- [x] Unit tests pass
- [x] Integration tests pass
- [x] Documentation updated
- [x] CHANGELOG entry added

---

## 5. Constraints

### Timeline

Ongoing development, release-driven cadence

### Resources

Solo developer with AI-assisted development

### Technical Constraints

- Must use: `gh` CLI SDK (go-gh)
- Must support: Windows, macOS, Linux (Windows cross-compiled; not in the CI build matrix)
- Must work with: GitHub Projects v2

### Compliance

None (MIT licensed open source)

---

## 6. Milestones

### Current Target

**v1.6.x**: Stabilize v1.6.0 — batched intake, Status field reconcile, persistent 1.x terms acceptance

### Milestone Roadmap

| Milestone | Target | Criteria |
|-----------|--------|----------|
| v1.4.x | Released | Security fixes, documentation audit, stability |
| v1.5.x | Released | Correctness and error-surfacing overhaul, complete pagination, fail-loud exit codes, offline schema validation |
| v1.6.x | Current | Intake performance, Status field reconcile, terms persistence |
| v1.7.0 | Next | TBD based on backlog |

See `Milestones.md` for full release history.

---

## Changelog

| Date | Change | Author |
|------|--------|--------|
| 2026-01-04 | Charter extracted from codebase | Claude |
| 2026-01-21 | Update focus to v0.14.0, add scope items, update milestones | Claude |
| 2026-03-27 | Update to v1.4.x, remove microsprint refs, add new capabilities | Claude |
| 2026-10-19 | Charter refresh: focus v1.6.x, scope adds shipped commands, coverage floor, Windows CI note | Claude |

---

*Generated by IDPF Framework*
