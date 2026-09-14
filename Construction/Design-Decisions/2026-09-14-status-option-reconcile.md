# Design Decision: Status Option Reconcile
**Date:** 2026-09-14
**Status:** Accepted
**Context:** Issue #917 -- Ensure 9 minimum Status values on init and add gh pmu status --update

## Decision
One planner, `planStatusReconcile` (`cmd/status.go`), computes the complete `singleSelectOptions` list for the Status field. `gh pmu init` runs it in init mode (add + case-only rename) and `gh pmu status --update` in update mode (also recolor to defaults and fill empty descriptions). The list is sent with a new `UpdateProjectFieldOptions` (`updateProjectV2Field`).

## Rationale
- `singleSelectOptions` replaces the whole list; an existing option sent without its `id` loses its value on every item. A single planner is the one place that guarantees every existing option is sent, in place, with its `id`, color and description.
- `color` and `description` are non-null and overwrite on update, so the update input needs them without `omitempty`.

## Alternatives Considered
- **Add `ID` to the existing `ProjectV2SingleSelectFieldOptionInput`**: rejected. That struct's `omitempty` on color/description is what `CreateProjectField` relies on; a separate `ProjectV2SingleSelectFieldOptionUpdateInput` keeps create unchanged and makes the no-`omitempty` rule explicit.
- **Detect cached fields by checking for empty colors**: rejected as the primary signal. Fields served from `.gh-pmu.json` are marked `ProjectField.FromCache` at the fallback; the planner additionally refuses any option without a color as a second guard.
- **Keep the missing-option check in `validateRequiredFields`**: removed. A missing Status value is now added, not rejected; if the reconcile is skipped (cached data) init proceeds with a warning instead of failing.
- **A new init failure step for the reconcile**: not added. An update failure reuses `validate-required-fields` in the rollback trailer, so the documented step vocabulary is unchanged.

## Consequences
- `status --update` refreshes `.gh-pmu.json`: every Status value gets its derived alias, aliases pointing at a renamed value follow the rename, aliases for values not on the board are left alone, and Status metadata is replaced.
- `status --update` is a new `Config.Save` caller; it saves the Config it loaded, so unmodeled keys survive (#910 guard updated).
- `config verify` checks aliases in the local config only, so a board fixed by hand but not re-initialized still triggers the alert until `status --update` or `init` refreshes the aliases.

## Issues Encountered
- `TestRunIntegrityCheck_Performance` (200ms budget) fails on this Windows machine at the pre-session commit as well; unrelated to this change.

---
*Documented during completion of #917*
