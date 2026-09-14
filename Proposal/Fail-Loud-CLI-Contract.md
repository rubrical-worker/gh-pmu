# Proposal: Fail-Loud CLI Contract

**Status:** Draft
**Created:** 2026-08-27
**Author:** AI Assistant
**Tracking Issue:** #914
**Diagrams:** None

---

## Problem Statement

`gh pmu` presents two contracts to the outside world: the text a human reads, and the
exit code plus JSON a script consumes. The first is in good shape. The second is
incomplete in five specific ways, each independently observable today, with no MCP
server involved.

**1. Read-only commands write to disk.** `PersistentPreRunE` (`cmd/root.go:57-63`) runs
`refreshConfigVersion`, which resolves `.gh-pmu.json` and stamps the running version into
it. `gh pmu list` — a pure read — therefore dirties the working tree. This is observable
right now: the tree carries an uncommitted `"version": "1.5.2"` to `"1.5.3"` change
produced by ordinary command runs. It breaks clean-tree assertions in CI and pre-commit
hooks, and invites conflicts on a `.gh-pmu.json` shared across a team.

**2. Scope is ambient and only partly steerable.** `refreshConfigVersion`
(`cmd/root.go:138`), `runDailyIntegrityCheck` (`cmd/root.go:239`), and
`api.defaultFieldsCacheLoader` each independently call `os.Getwd()` and walk up for
`.gh-pmu.json`. A `--dir` flag exists on exactly two of twenty top-level commands —
`accept` (`cmd/accept.go:40`) and `config` (`cmd/config.go:57`) — so there is no general
way to target another checkout, and a monorepo subdirectory resolves ambiguously. The
same ambient resolution forces the `protectRepoRoot` guard, which exists only because
tests inherit the real working directory and would otherwise rewrite the repository's own
config (#436).

**3. Production state is package-global.** `api.SetFallbackOptions` writes
`fallbackOptions` with no mutex (`internal/api/fields_fallback.go:56`), driven from
`PersistentPreRunE`. `fieldsCacheWarning`, `protectRepoRoot`, and
`projectFieldByNameFetcher` share the shape.

This is a latent defect, not an active one: there is no `t.Parallel()` anywhere in the
suite and one command per process makes the current arrangement safe. The cost is
threefold and real nonetheless. `SetFallbackOptionsForTesting` and `protectRepoRoot` are
test-only escape hatches that exist *because* the production state is global; the suite
cannot adopt `t.Parallel()`; and intra-command fan-out is blocked. That last is the
substantive one — `intake`, `triage`, and `BatchUpdateProjectItemFields` are inherently
parallelizable and cannot safely fan out while the fallback options and cache loader are
shared mutable state.

**4. The error taxonomy dies at the process boundary.** `internal/api/errors.go`
deliberately separates `ErrOwnerNotFound` from `ErrProjectNotFound` (#861), and adds
`ErrFieldResolverUnavailable`, `ErrRateLimited`, `ErrNotAuthenticated`, and
retryable-5xx detection. All of it collapses into `os.Exit(1)` at `main.go:11`.

For a CLI the exit code *is* the machine contract, and this one carries a single bit. The
codebase already models richer outcomes it cannot express: `cmd/split.go:261` computes a
`"partial"` status into its JSON while the process still exits 1, indistinguishable from
total failure, and `internal/api/assignee.go:142` notes outright that "Both cases exit 1".
The charter lists fail-loud error propagation with partial-failure exit codes as in-scope
work, so this is unfinished business rather than new scope.

**5. The JSON surface is partial and self-contradictory.** Nine of twenty top-level
commands emit JSON, in three incompatible flag shapes: `--json` as a bool (`board`,
`filter`, `history`, `intake`, `split`, `triage`, `sub list`), `--json=fields` as a string
(`list`, `view`, `branch current`), and `list`'s self-describing
`NoOptDefVal = "_list_"`. The shapes actively collide — `view` has a dedicated
`--json-fields` bool for field discovery (`cmd/view.go:130`), while `list` overloads bare
`--json` for that same purpose, so the identical flag means "all fields" on one command
and "describe your fields" on another.

The eleven top-level commands with no JSON output at all are precisely the mutating ones:
`create`, `edit`, `move`, `close`, `comment`, `field`, `label`, `assignee`, `config`,
`init`, `accept`. No script can machine-confirm a write.

## Proposed Solution

Four independently shippable tranches, ordered by value-to-risk. Each lands on its own;
none depends on a later one.

**Tranche A — Exit-code taxonomy and JSON error envelope.**
Change `cmd.Execute()` to return a typed error and have `main.go` map the existing
sentinels to a documented exit table: `2` usage, `3` not found, `4` auth, `5` rate
limited, `6` partial failure, `7` upstream unavailable (`ErrFieldResolverUnavailable`),
`1` retained as the generic fallback. Under `--json`, emit a stable error envelope on
stderr carrying the same discriminator. Strictly additive — every failure that exits
non-zero today continues to, and no success becomes a failure.

**Tranche B — Stop unsolicited config writes.**
Restrict the version stamp to commands that already persist config (`init`, `config`,
`accept`). Elsewhere, when the stamp is stale, emit a one-line stderr notice pointing at
an explicit `gh pmu config refresh`. Honour a `GH_PMU_NO_CONFIG_WRITE` environment
variable for CI and sandboxed runs.

**Tranche C — Unify the JSON surface.**
Converge every command on `view`'s shape: bare `--json` means all fields, an optional
comma-separated list selects them, and `--json-fields` performs discovery. Add result
objects to the eleven mutating commands so a write can be machine-confirmed. This is the
one tranche with a genuine backward-compatibility break — `list --json` changes meaning
from "describe fields" to "emit all fields" — so it ships behind a deprecation cycle that
warns for one minor release before switching.

**Tranche D — Explicit scope, instance state.**
Resolve scope once in `PersistentPreRunE` into a `Scope` value and pass it explicitly into
`internal/api` and `internal/config` rather than having three layers re-derive it. Promote
`--dir` to a persistent flag with a `-C` alias, following git convention and generalising
the precedent already set on `accept` and `config`. Move `fallbackOptions` and the
fields-cache loader onto `Client`. Retires both `protectRepoRoot` and
`SetFallbackOptionsForTesting`, and unblocks `t.Parallel()` and intra-command fan-out.

## Implementation Criteria

- [ ] `cmd.Execute()` returns a typed error and `main.go` maps sentinels to documented exit codes
- [ ] Exit codes 2 through 7 are documented in README and covered by tests, with 1 retained as the generic fallback
- [ ] `split`'s existing `"partial"` outcome exits 6 rather than 1
- [ ] Under `--json`, failures emit a stable machine-readable error envelope on stderr carrying the same discriminator
- [ ] No command that only reads writes to `.gh-pmu.json`; the version stamp is limited to `init`, `config`, and `accept`
- [ ] `gh pmu config refresh` exists as the explicit way to restamp, and `GH_PMU_NO_CONFIG_WRITE` suppresses the write
- [ ] Every JSON-capable command accepts bare `--json` for all fields and `--json=a,b` for selection
- [ ] `--json-fields` performs discovery on every JSON-capable command, and `list`'s `_list_` overload is removed after one minor release of deprecation warning
- [ ] The eleven mutating commands emit a JSON result object confirming what changed
- [ ] Scope is resolved once in `PersistentPreRunE` and passed explicitly; no `internal/api` or `internal/config` path calls `os.Getwd()`
- [ ] `--dir` is a persistent flag with a `-C` alias, honoured by every command
- [ ] `protectRepoRoot` and `SetFallbackOptionsForTesting` are removed, with `fallbackOptions` and the fields-cache loader as fields on `Client`
- [ ] `go test -race ./...` passes with `t.Parallel()` enabled in the `cmd` and `internal/api` suites
- [ ] Human-readable output is byte-identical for every command not explicitly listed above

## Alternatives Considered

- **Do nothing:** Defensible in isolation, but items 4 and 5 are the CLI's machine
  contract, and the charter already claims partial-failure exit codes as in-scope. Doing
  nothing leaves stated scope unimplemented while the repository's own `.claude` scripts
  continue to consume `gh pmu` by parsing prose.

- **Ship the MCP front end first (#913) and let it force these fixes:** Rejected. It
  couples correctness work to a feature that may never be scheduled, and inverts the
  dependency — these are CLI defects that an MCP would merely have exposed. Fixing them
  here makes #913's steps 1 and 2 redundant and narrows that proposal to the `cmd/mcp/`
  front end alone.

- **Fix only tranches A and C, leaving the process-model items:** A legitimate reduced
  scope. It delivers the entire machine contract — exit codes and JSON — without touching
  `PersistentPreRunE` or the globals, so it carries markedly less risk. The cost is that
  the test escape hatches stay, `t.Parallel()` stays unavailable, and read commands keep
  dirtying the tree. Worth taking if v1.6 capacity is tight.

- **Introduce a `--porcelain` mode instead of unifying `--json`:** Sidesteps the
  `list --json` break by adding a parallel surface, but leaves two machine formats to
  maintain and does nothing for the eleven commands that emit no structured output at
  all. Rejected as strictly more surface for less benefit.

## Impact Assessment

- **Scope:** `main.go`, `cmd/root.go`, every `output*JSON` path across the `cmd` package,
  `internal/api/errors.go`, `internal/api/fields_fallback.go`, `internal/api/client.go`,
  and `internal/config`. Tranches A and B are narrow; C touches every JSON-emitting
  command; D touches call paths throughout `internal/api`.
- **Risk:** Low for A — additive, no success-path change. Low for B, though the behaviour
  change is user-visible and strictly less surprising. Medium for C, carrying the only
  real compatibility break in the proposal. Medium-high for D, being non-trivial surgery
  on code that currently works.
- **Effort:** A and B are small. C is moderate but broad. D is the bulk of the work.
- **Scheduling:** Tranches A and B fit the v1.5.x line on charter grounds — they complete
  the fail-loud error-propagation work v1.5.0 began. C and D are v1.6.
- **Relationship to #913:** This proposal delivers #913's steps 1 and 2 on CLI merit
  alone. If both are accepted, #913 should narrow to the `cmd/mcp/` front end and its
  hard-exclude list.
- **Open question:** Whether the exit-code table should follow `gh`'s own conventions
  where they overlap, to avoid surprising users who script both tools together.
