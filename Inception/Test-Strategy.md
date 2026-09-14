# Test Strategy: gh-pmu

**Last Updated:** 2026-09-14

---

## Testing Philosophy

Multi-layer testing with emphasis on table-driven unit tests against mocked API clients. Live-API coverage lives in a local-only e2e suite; CI never calls the GitHub API (burst traffic from CI tripped abuse detection and locked the account — #876).

---

## Framework

<!-- GENERATED from framework-config.json testing.suites[] by /charter. framework-config.json is the authority; edits here are reported at /charter refresh, not kept. -->

| Role | Harness | Command (`full`) | Execution |
|------|---------|------------------|-----------|
| unit | go test (`go-test`) | `go test ./...` | gate |
| e2e | go test (subprocess invocation) (`go-subprocess-e2e`) | `go test -tags e2e ./test/e2e/ -v` | manual-only — live GitHub API, requires `gh` auth; run locally before release |

---

## Test Pyramid

| Level | Approach | Runs in |
|-------|----------|---------|
| Unit | Table-driven tests with mock clients, standard `testing` package | CI (`go test -short -race`), `/work` sweep |
| Integration (manual) | `cmd/*_integration_test.go` behind `-tags integration` | Manual only; compile-checked in CI |
| E2E | `test/e2e/` behind `-tags e2e`, against a real project | Local, pre-release; compile-checked in CI |

---

## Test Types

### Unit Tests

- **Framework:** Go standard `testing` package
- **Location:** `*_test.go` alongside source files
- **Naming:** `TestFunctionName_Scenario`
- **API isolation:** mock clients per command (`intakeClient`, `statusUpdateClient`, …) and capture transports in `internal/api`
- **Schema validation:** every GraphQL document is validated offline against the vendored schema in `testdata/graphql/` (`internal/api/schema_operations_test.go`)

### Integration Tests (manual layer)

- **Framework:** Go `testing` with `//go:build integration`
- **Location:** `cmd/*_integration_test.go`
- **Scope:** Commands not covered by e2e (create, edit, intake, split, triage, sub-commands)
- **CI:** compile check only (`go vet -tags "integration e2e" ./...`) — no API traffic

### End-to-End Tests

- **Framework:** Go `testing` with `//go:build e2e`
- **Location:** `test/e2e/`
- **Scope:** Live workflows against a real GitHub project; requires `gh` auth
- **When:** Locally before release (`go test -tags e2e ./test/e2e/ -v`)

---

## Quality Gates

| Gate | Criteria | Enforcement |
|------|----------|-------------|
| Pre-commit | `go test ./...`, `go vet`, `golangci-lint run` | Developer discipline, `/work` sweep |
| PR / push | Tests (`-short -race`), vet incl. tag-gated code, build, coverage report, gosec, CodeQL | GitHub Actions |
| Release | E2E suite passes locally | Manual verification |

---

## Test Data Strategy

| Type | Approach |
|------|----------|
| Unit test data | Inline fixtures, table-driven, canned GraphQL JSON |
| Schema data | Vendored GitHub schema in `testdata/graphql/` with recorded provenance |
| E2E test data | Real issues in a test project, with cleanup gating |

---

## Special Testing Considerations

### Performance Testing

Benchmarks for hot paths (e.g. `BenchmarkIntake_ClassifyCandidates`). `TestRunIntegrityCheck_Performance` enforces a 200ms budget on the daily config check.

### Security Testing

gosec and CodeQL in CI. Authentication is delegated to `gh`; no credential storage to test.

### Accessibility Testing

Not applicable (terminal CLI).

---

## Definition of "Tested"

A feature is considered tested when:
- [x] Unit tests cover core logic and edge cases
- [x] Error paths are tested
- [x] New GraphQL operations pass vendored-schema validation
- [x] All tests pass in CI

---

*See also: Charter-Details.md, Constraints.md*
