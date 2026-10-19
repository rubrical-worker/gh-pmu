# Milestones: gh-pmu

**Last Updated:** 2026-10-19

---

## Current Focus

**Version:** v1.6.x
**Status:** Active — stabilize v1.6.0: batched intake, Status field reconcile, persistent 1.x terms acceptance

---

## Release History

| Version | Date | Key Deliverables |
|---------|------|------------------|
| v1.6.0 | 2026-09-14 | Batched intake via repository-scoped search (12.2s → 1.2s); `status --update` reconciles required Status values; `intake --list` explicit mode; terms acceptance valid across 1.x |
| v1.5.3 | 2026-08-25 | Config `version` refreshed when stale; `Config.Save` preserves unmodeled keys; CommonMark-correct checkbox counting; `MigrateYAML` removed |
| v1.5.2 | 2026-08-19 | `config verify --resolve-view`; unused `release` config section removed |
| v1.5.1 | 2026-07-28 | `--assignee @me` resolves on every command; an unresolvable assignee fails before creating the issue |
| v1.5.0 | 2026-07-23 | Correctness and error-surfacing overhaul; pagination for user-facing fetches; fail-loud partial-failure exit codes; test-quality overhaul with offline GraphQL schema validation |
| v1.4.10 | 2026-05-14 | Cached-metadata fallback for the ProjectV2 fields resolver during the upstream HTTP 500 rollout |
| v1.4.9 | 2026-05-05 | Atomic `init` with rollback; transparent retry on transient 5xx |
| v1.4.8 | 2026-04-29 | `init --project` creates missing optional fields, matching the `--source-project` path |
| v1.4.7 | 2026-04-23 | Branch-tracker rendering fix; `fields.branch` emitted and preserved on re-init |
| v1.4.6 | 2026-04-15 | GraphQL input boundary validation across the API client |
| v1.4.5 | 2026-03-27 | `init --project` to connect an existing ProjectV2; docs refresh |
| v1.4.4 | 2026-03 | Security fixes, gofmt alignment |
| v1.4.3 | 2026-03 | Maintenance and stability |
| v1.4.2 | 2026-03 | Bug fixes |
| v1.4.1 | 2026-03 | Patch fixes |
| v1.4.0 | 2026-03 | Feature release |
| v1.3.2 | 2026-03 | Patch fixes |
| v1.3.1 | 2026-02 | Config integrity verification |
| v1.3.0 | 2026-02 | Label management, config verify |
| v1.2.x | 2026-02 | Status transition validation |
| v1.1.0 | 2026-02 | Terms acceptance gate |
| v1.0.x | 2026-01 | First stable release series |

See `CHANGELOG.md` for detailed release notes per version.

---

## Risks to Timeline

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| GitHub API changes | Low | High | Version pinning, tests |
| Breaking gh changes | Low | Medium | Pin go-gh version |
| Scope creep | Medium | Medium | Strict PR review |

---

*See also: Scope-Boundaries.md, Charter-Details.md*
