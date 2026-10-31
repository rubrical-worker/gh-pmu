# Design Decision: Priority P3 and Add-Only Priority Reconcile
**Date:** 2026-10-31
**Status:** Accepted
**Context:** Issue #955 -- Add support for an additional P3 priority level

## Decision
`internal/defaults/defaults.yml` declares Priority options P0–P3. A new project gets them from `ensureOptionalProjectFields`; `buildFieldMappingsFromMetadata` derives `p3: P3` from the live options.

An existing project-level Priority field lacking P3 is fixed by `reconcileInitPriority` (`cmd/init.go`), called from both init paths after `reconcileInitStatus` and before `ensureOptionalProjectFields`. It reuses `planStatusReconcile` (`cmd/status.go`) in a new `statusReconcileAddOnly` mode: a case-insensitive match counts as present and is sent as found, never renamed.

## Rationale
- `planStatusReconcile` already guarantees the one property that matters: every existing option is sent in place with its `id`, color and description, so items keep their Priority values when `updateProjectV2Field` replaces the list. A second planner would duplicate that guarantee.
- Add-only rather than init mode: the issue requires P3 be added "without altering existing options". Init mode's case-only rename (`p1` → `P1`) would alter one.
- `updateProjectV2Field` requires a color on every option, and the Priority defaults are plain names, so an added option with no declared color is sent `GRAY` (`defaultAddedOptionColor`).
- Priority is optional (`create_if_missing`), so every skip and failure warns and init continues. Status is required, so its update failure still rolls back the `--source-project` path.

## Alternatives Considered
- **Declare colors on the Priority defaults**: rejected for now. Changing the shape of the Priority entries also changes what `CreateProjectField` sends for new projects. The GRAY fallback is scoped to the reconcile.
- **Fail init on a Priority update error**: rejected. It would make an optional field block the required setup, and on `--source-project` delete the just-created project.
- **Reconcile Priority on unclassified fields** (the per-name resolver fallback): rejected. `Unclassified` means it is unknown whether the field is an org issue field, whose options a project-field update cannot change. Skipped with a warning, alongside the two skips the issue names (org issue field, cached metadata).
- **Drive `runInitExistingProject` with a fake client for the parity test**: not possible without refactoring it, because it builds its own `api.Client`. Parity is asserted on the call order in both functions via `go/ast`, and the `--source-project` path is also tested behaviorally.

## Consequences
- `gh pmu init` on an existing project adds P3 and prints `Priority: added "P3"`; the refetch then writes `p3: P3` to `.gh-pmu.json`.
- Existing p0–p2 configs are untouched until `init` is re-run; `p3` stays invalid for them.
- The `buildFieldMappingsFromMetadata` fallback (used only when no Priority field is found) still lists p0–p2, so it never names an option that may not exist.
- `planStatusReconcile`'s no-color error no longer says "status option", since it now serves Priority too. Its `status*` names are kept, to stay within scope.

## Issues Encountered
- No Go toolchain on the development machine; Go 1.23.8 was installed via winget to match CI.
- `gofmt -l` reports every file on this Windows checkout because of CRLF working-copy line endings; formatting was checked on LF-normalized content instead.

---
*Documented during completion of #955*
