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

## Fixtures

The suite runs against one or more **fixtures**, each a live GitHub owner with a test repository and a template project (#940). They are defined in `fixture_test.go`, the only file in this package that names an owner, a repository or a project number.

| Fixture | Owner | Repository | Template project | Covers |
|---------|-------|------------|------------------|--------|
| `user` | `rubrical-worker` (user) | `rubrical-worker/gh-pmu-e2e-test` | #41 | Every generic workflow test |
| `org` | `rubrical-works` (organization) | `rubrical-works/gh-pmu-e2e-test` | dedicated template, number recorded in `fixture_test.go` | Organization issue fields (#932): classification, the `issue_field` override, `move --priority` writes, org-scoped `init` |

Select fixtures with `E2E_FIXTURE`:

| Value | Runs |
|-------|------|
| unset or `user` | The user fixture only. This is the default, so ordinary runs and API volume are unchanged. Org tests report SKIP. |
| `org` | The org fixture only. User tests report SKIP. |
| `all` | Both |

Any other value stops the run in `TestMain`.

```bash
E2E_FIXTURE=org go test -tags e2e ./test/e2e/ -v -count=1 -timeout 30m
```

**The org fixture is set up by hand, once.** Until its template project's number is recorded in `fixture_test.go` (`orgFixture.TemplateProject`), every org test skips and says so. To set it up:

1. Create the repository `rubrical-works/gh-pmu-e2e-test`.
2. The template is the org's existing **Kanban Template** project (#2). The tests only copy it with `init --source-project`, and orphan cleanup never matches it: it deletes by the repository's title and skips the template's number. The tests rely on it keeping:
   - the native **Status** field;
   - **Priority**, backed by the organization's Priority issue field;
   - one ordinary (native) project field (Size).

   Keep it small. Org discovery adds a paginated `issueFields` query and samples up to 100 items. Renaming or removing those fields breaks the org tests.
3. Its number is recorded in `orgFixture.TemplateProject`. Change it there if the template ever moves.

Orphan cleanup (`E2E_CLEANUP=true`) runs for every fixture owner, selected or not. It deletes projects titled after the fixture's repository and never deletes the template. An owner whose template isn't recorded yet is skipped.

### Token scopes

| Fixture | Needs |
|---------|-------|
| `user` | `repo`, `project` |
| `org` | `repo`, `project`, `read:org`, and write access to the organization's issue fields |

Check with `gh auth status`. Add a missing scope with `gh auth refresh -s read:org,project`.

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
