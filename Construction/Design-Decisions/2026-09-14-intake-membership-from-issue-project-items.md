# Design Decision: Intake Membership From Issue projectItems
**Date:** 2026-09-14
**Status:** Accepted
**Context:** Issue #918 -- Improve gh pmu intake performance and require explicit --list mode

## Decision
`gh pmu intake` decides whether an open issue is already on the project by reading that issue's own `projectItems(first: 20)` through a new slim search query (`SearchIntakeCandidates`), instead of paging the whole project board with `GetProjectItems` and building an ID set.

`--apply` adds issues through a new aliased batch mutation (`BatchAddIssuesToProject`, 50 per request) and sets fields through the existing `BatchUpdateProjectItemFields`.

Listing requires `--list`; with no mode flag intake prints help and exits 0 before loading config or creating a client.

## Rationale
- Board scans cost scale with every item ever added, closed ones included; the inverted lookup scales with open issues. Measured on this repository: 12.2s before, 1.2s after.
- ProjectV2 items cannot be filtered server-side (#624), so filtering the board could not remove the cost.
- `BatchUpdateProjectItemFields` (#543) already batches field updates with per-item results; only the add half was missing.

## Alternatives Considered
- **Slim board-scan query (as #531 did for `board`)**: rejected — still pages every board item.
- **Targeted paginated check for issues on more than 20 projects**: rejected for now. Such issues are reported on stderr and not listed, which meets the fail-loud contract from #860 without a second query shape. Revisit if the warning is ever seen in practice.
- **Missing mode as a usage error (non-zero exit)**: rejected for now; help with exit 0 was the specified behavior. Open proposal #914 tranche A would map usage errors to exit 2 and may revisit this.

## Consequences
- Intake no longer depends on `GetProjectItems`; a guard test pins its absence from `intakeClient`.
- An issue added successfully but whose field update fails is counted in `(N failed)` and excluded from the `applied` JSON list, although it is on the board.
- Scripts relying on bare `gh pmu intake` or `gh pmu intake --json` to list must add `--list`.
- Optional server-side `--assignee` (approach E in #918) was not implemented; assignee filtering remains client-side after `@me` resolution.

## Issues Encountered
- `cmd/intake_integration_test.go` still passes `--apply ""`, which `cobra.NoArgs` (#867) rejects as a stray positional. Pre-existing and out of scope for #918.

---
*Documented during completion of #918*
