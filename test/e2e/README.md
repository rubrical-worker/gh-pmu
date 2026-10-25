# E2E Tests

End-to-end tests for gh-pmu that run against a real GitHub project. These tests build the binary with coverage instrumentation and execute commands against live GitHub APIs.

## Prerequisites

- GitHub CLI authenticated (`gh auth status`)
- `.gh-pmu.json` configured in the repository root
- Network access to GitHub API

## Running

```bash
go test -tags e2e ./test/e2e/ -v -count=1
```

The `e2e` build tag is required — `go test ./...` does **not** include these tests.

## What's Tested

| Suite | Coverage |
|-------|----------|
| Board | Rendering, filtering by priority |
| Branch | Start, current, list, close lifecycle |
| Filter | By status, priority, branch, no-branch, combined |
| Init | Non-interactive modes, flag validation, overwrite |
| View | JSON output with standard and project fields |
| Workflow | Create-to-close, sub-issues, multi-move, labels, force-yes |

## Runtime

A full run currently takes over 10 minutes against the live test project (#942), so pass an explicit `-timeout` (for example `-timeout 30m`) rather than relying on Go's 10-minute default. Tests create and delete real GitHub issues; cleanup runs automatically via `t.Cleanup`.

## Rate Limiting

Every live API call the suite makes, `runPMU` and every direct `gh` call (`runGH`), goes through one pacing point: the limiter from `internal/testutil/ratelimit`, created in `TestMain` (#920). The suite is a sustained burst of writes against one account. That traffic is what got the old CI integration workflow's account locked out (#876).

| Variable | Default | Meaning |
|----------|---------|---------|
| `E2E_MIN_INTERVAL_MS` | `250` | Minimum gap before any live call |
| `E2E_MUTATING_INTERVAL_MS` | `1000` | Minimum gap before a call that writes (create, move, close, delete, ...) |
| `E2E_MAX_RATE_LIMIT_RETRIES` | `3` | Retries for a rate-limited direct `gh` call |
| `E2E_RATE_LIMIT_BASE_DELAY_MS` | `5000` | First backoff delay; doubles per retry |
| `E2E_RATE_LIMIT_MAX_DELAY_MS` | `60000` | Cap on any single wait, including a server `Retry-After` |

An invalid value (not a non-negative integer) stops the run in `TestMain` with an error naming the variable. It never falls back silently to the default.

How rate limits are handled:

- **`gh pmu` calls are not retried again.** The binary already retries 429 and rate-limited 403 itself (#710, #849). A rate limit that reaches `runPMU` has survived those retries, so the test fails with a message naming rate limiting, not a generic assertion.
- **Direct `gh` calls are retried** with bounded backoff: `Retry-After` when the output carries one, otherwise exponential from the base delay, each wait capped at the maximum.
- **A secondary rate limit (abuse detection) that is not cleared trips a circuit breaker.** Every later call fails immediately with `circuit breaker open` and sends no traffic. Continuing to send requests after abuse detection fires is how an account gets locked out.

**Runtime impact:** pacing adds up to the interval per call, but only when calls come faster than that. With several hundred live calls per run, the defaults can add a few minutes on top of the current runtime. Lower the intervals to trade safety for speed. Raise them if a run trips the breaker.

## Adding Tests

- Use the `//go:build e2e` build tag
- Use `createTestIssue` / `createTestBranch` helpers from `helpers_test.go`
- Register cleanup with the helper's built-in `t.Cleanup` to delete test issues
- Prefix test issue titles with `[E2E]` for easy identification
