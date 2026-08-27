# Proposal: MCP Server Front End

**Status:** Draft
**Created:** 2026-08-27
**Author:** AI Assistant
**Tracking Issue:** #913
**Diagrams:** None

---

## Problem Statement

`gh pmu` is a CLI. Every capability it has assumes the process model of a CLI: one
command per process, a meaningful working directory, and a single caller. An MCP
front end violates all three assumptions at once — it is a long-lived process,
launched from an arbitrary directory by a client, asked about potentially many
repositories, and capable of servicing concurrent tool calls.

Four properties of the current code make a naive MCP wrapper unsafe rather than
merely slow:

1. **Scope is ambient, not passed.** `refreshConfigVersion` (`cmd/root.go:138`),
   `runDailyIntegrityCheck` (`cmd/root.go:239`), and `api.defaultFieldsCacheLoader`
   each independently resolve configuration by calling `os.Getwd()` and walking up
   for `.gh-pmu.json`. A server has no meaningful working directory, so all three
   resolve against whatever directory the MCP client happened to launch from.

2. **Every command writes to disk before doing anything.** `PersistentPreRunE`
   stamps the running version into `.gh-pmu.json` on every non-exempt command. This
   is observable right now: the working tree carries an uncommitted
   `"version": "1.5.2"` to `"1.5.3"` change produced by ordinary command runs. Per
   tool call, in a server, this becomes write amplification and spurious VCS churn.

3. **Package-level mutable state is unguarded.** `api.SetFallbackOptions` writes
   `fallbackOptions` with no mutex (`internal/api/fields_fallback.go:56`), driven from
   `PersistentPreRunE` on each invocation. One command per process makes that safe;
   concurrent tool calls make it a data race. `fieldsCacheWarning sync.Once` has a
   quieter failure: in a server it fires once for the entire process lifetime, so
   every repository after the first silently loses its cache-fallback warning.
   `protectRepoRoot` and `projectFieldByNameFetcher` share the shape.

4. **The error taxonomy dies at the process boundary.** `internal/api/errors.go`
   deliberately separates `ErrOwnerNotFound` from `ErrProjectNotFound` (#861), and
   adds `ErrFieldResolverUnavailable`, `ErrRateLimited`, `ErrNotAuthenticated`, and
   retryable-5xx detection. All of it collapses into `os.Exit(1)` at `main.go:11`.

Compounding this, JSON output is partial and inconsistent. Nine of twenty top-level
commands emit JSON, across three incompatible flag shapes (`--json` as bool; `--json=fields`
as string; `list`'s self-describing `NoOptDefVal = "_list_"`). The eleven top-level
commands with no JSON output at all are precisely the mutating ones — `create`, `edit`,
`move`, `close`, `comment`, `field`, `label`, `assignee`, `config`, `init`, `accept` —
so a shell-out wrapper would return prose to a model for exactly the operations where a
confirmable structured result matters most.

There is a prior parked design, `Proposal/ParkingLot/PROPOSAL-MCP-Server.md`
(2025-12-25). It is broader in ambition and is treated here as superseded; see
Alternatives Considered.

## Proposed Solution

Build the MCP server as a **second front end over `internal/api` and `internal/config`,
sibling to `cmd/`** — not as a layer downstream of the CLI. Sequence the work so each
prerequisite stands on its own merit and lands independently.

**Step 1 — Decouple scope from the working directory.**
Thread an explicit config/repo scope through `internal/api` and `internal/config`.
`cmd/` continues to resolve that scope from `os.Getwd()` exactly as it does today, so
there is no user-visible change. This is a pure refactor, independently correct, and it
retires the `protectRepoRoot` test hack introduced for #436.

**Step 2 — Make the globals instance state.**
Move `fallbackOptions` and the fields-cache loader onto `Client`. Same argument as step 1:
it removes a test-only escape hatch and closes a latent data race, whether or not the MCP
work ever ships.

**Step 3 — Add `cmd/mcp/` as a sibling front end.**
Expose roughly twelve curated tools rather than transliterating 42 command nodes. Read-heavy:
`list`, `view`, `board`, `filter`, `sub list`, `history`. A deliberately small write set:
`create`, `edit`, `move`, `comment`, `sub add`. Errors map from the existing sentinels into
structured MCP error payloads, preserving the distinction #861 exists to protect.

**Step 4 — Hard-exclude the dangerous surface.**
The following must be unreachable from MCP: the entire git/filesystem group on `Client`
(`WriteFile`, `MkdirAll`, `GitAdd`, `GitCommit`, `GitTag`, `GitCheckoutNewBranch`),
`DeleteProject`, `DeleteProjectField`, `init`, `accept`, and `history`'s app-launching path
(`cmd/history.go:1028-1032`, which shells to `cmd /c start` / `open` / `xdg-open`).

Exposing ~35 leaf commands one-to-one would bloat client context and degrade tool selection;
the curation is a feature, not a shortcut.

## Implementation Criteria

- [ ] `internal/api` and `internal/config` accept an explicit config/repo scope; no call path reaches `os.Getwd()`
- [ ] `cmd/` resolves scope from the working directory, preserving current CLI behavior with no flag or output changes
- [ ] The `protectRepoRoot` guard added for #436 is removed, with tests still passing
- [ ] `fallbackOptions` and the fields-cache loader are fields on `Client`, not package-level state
- [ ] The cache-fallback warning fires per client instance rather than once per process
- [ ] `go test -race ./...` passes with a test exercising concurrent `Client` use across two scopes
- [ ] `cmd/mcp/` exposes the read set: `list`, `view`, `board`, `filter`, `sub list`, `history`
- [ ] `cmd/mcp/` exposes the write set, and only that write set: `create`, `edit`, `move`, `comment`, `sub add`
- [ ] Existing error sentinels map to distinct structured MCP errors, with `ErrOwnerNotFound` and `ErrProjectNotFound` remaining distinguishable
- [ ] The hard-exclude list is unreachable from MCP, enforced by a test that fails if a new tool registers one of them
- [ ] No MCP tool call writes to `.gh-pmu.json` as a side effect
- [ ] Terms acceptance has an explicit human-in-the-loop path; the server never self-accepts
- [ ] All CLI commands behave identically before and after, verified by the existing suites

## Alternatives Considered

- **Shell out to `gh pmu` from the MCP server:** Fastest to stand up and the worst
  long-term. Inherits every process-level assumption in the Problem Statement, pays
  fork plus auth plus config-walk per tool call, and returns prose for all eleven
  mutating commands that lack JSON. Rejected.

- **Revive `PROPOSAL-MCP-Server.md` as written:** The parked design is genuinely
  useful — TTL caching, SQLite error-pattern accumulation with preflight checks, SSE
  transport — and its SDK research (official `modelcontextprotocol/go-sdk`) still
  holds. Two problems. It is partly stale: it configures via `.gh-pmu.yml`, which is
  now `.gh-pmu.json`, and its premise that `IsRateLimited` has "no retry logic follows"
  was overtaken by `internal/api/retry_client.go` and its idempotency guard. More
  importantly its Phase 1 instructs "wrap existing command logic as MCP tools", which
  is precisely the trap this proposal exists to avoid. Superseded, not discarded — its
  caching and error-intelligence phases remain good candidates once the foundation is
  correct.

- **Expose all ~35 leaf commands as MCP tools:** Mechanically simple, but bloats client
  context and measurably degrades model tool selection. Rejected in favor of curation.

- **Do nothing:** Defensible for v1.5.x. Steps 1 and 2 still carry independent value as
  correctness fixes, so they are worth scheduling even if step 3 never is.

## Impact Assessment

- **Scope:** `internal/api/fields_fallback.go`, `internal/api/client.go`,
  `internal/config/config.go`, `cmd/root.go`, plus a new `cmd/mcp/` package. Steps 1-2
  touch call paths throughout `internal/api`; step 3 is additive.
- **Risk:** Medium-high for steps 1-2 — non-trivial surgery on code that currently works,
  in a release line whose stated focus is stabilization. Low for steps 3-4, which are
  purely additive and gated behind the hard-exclude test.
- **Effort:** Step 1 is the bulk of the work. Steps 1-2 are a meaningful refactor;
  steps 3-4 are moderate given a clean foundation.
- **Scheduling:** This is v1.6 work. The charter's current focus is v1.5.x stabilization
  of the correctness and error-surfacing work shipped in v1.5.0, and this does not belong
  in that line. Steps 1-2 could be argued into v1.5.x on correctness grounds alone,
  but only deliberately.
- **Open question:** Whether an adopted Go MCP SDK constrains the module's current Go 1.23
  floor, and whether `intake`/`triage`'s config-driven behavior can be expressed as tool
  parameters without re-implementing `.gh-pmu.json` in the tool schema.
