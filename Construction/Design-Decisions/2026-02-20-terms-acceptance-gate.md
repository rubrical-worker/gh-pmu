# Design Decision: Terms and Conditions Acceptance Gate

**Date:** 2026-02-20
**Status:** Accepted
**Context:** Issue #694 — [Enhancement]: Accept terms and conditions

## Decision

Implemented a terms acceptance gate that blocks all `gh pmu` commands until the user explicitly accepts terms and conditions. Acceptance is per-repo (shared), embedded in the binary, and version-aware.

## Key Decisions

### 1. Dev builds skip the gate
Production builds (version set via ldflags) enforce acceptance. Dev builds (`version == "dev"`) skip it entirely. This prevents the gate from interfering with development and testing while ensuring production users always accept.

### 2. PersistentPreRunE on root command
Used Cobra's `PersistentPreRunE` on the root command to intercept all subcommands. No existing commands used this hook, making it safe to add. Exempt commands (`init`, `accept`, `help`) are checked by name in a map.

### 3. Per-repo shared acceptance
Acceptance is stored in `.gh-pmu.yml` (committed to repo), so one person's acceptance covers all collaborators. The acceptor sees a notice about this shared scope. This was chosen over per-user (global config) because the requirement specified per-repo first-use detection.

### 4. Terms embedded via go:embed
Follows the existing pattern from `internal/defaults/defaults.yml`. Terms text lives in `internal/defaults/terms.txt` and is compiled into the binary. No external dependencies at acceptance time.

### 5. Version-based re-acceptance
Compares the major version only (ignores minor and patch) — amended by #919, 2026-09-14; the original decision compared major.minor. Re-acceptance on every minor release blocked collaborators out of proportion to the change, so it is now tied to major releases; a terms change that must be re-accepted ships in a major release. Uses simple string parsing rather than a semver library. Dev versions are handled specially: dev current skips re-acceptance, dev accepted always triggers re-acceptance on real versions.

## Alternatives Considered

- **Global config (~/.config/gh-pmu/)**: Rejected because requirement specified per-repo detection
- **Middleware pattern**: Rejected in favor of Cobra's built-in PersistentPreRunE
- **Interactive-only acceptance**: Added `--yes` flag for Claude Code / CI support
- **Semver library**: Rejected — simple string split is sufficient for the version comparison

## Consequences

- All production builds will prompt on first use per repo
- Existing repos need to run `gh pmu accept` after upgrading
- Claude Code integration requires startup rule to detect and handle acceptance
- Test suite unaffected (dev builds skip gate)

## Issues Encountered

- Existing tests failed when gate was first added because they `os.Chdir` to temp dirs with configs lacking acceptance. Solved by having dev builds skip the gate, with gate tests using `setTestVersion()` to enable it explicitly.
