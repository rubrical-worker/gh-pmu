# Development Guide

Build, test, and contribute to gh-pmu.

## Prerequisites

- Go 1.22 or later
- GitHub CLI (`gh`) installed and authenticated
- `project` scope enabled (`gh auth refresh -s project`)

## Getting Started

```bash
# Clone the repository
git clone https://github.com/rubrical-works/gh-pmu.git
cd gh-pmu

# Build
go build -o gh-pmu .

# Install as gh extension
gh extension install .

# Verify installation
gh pmu --version
```

## Building

### Using Make

```bash
# Build binary
make build

# Build and install
make install

# Clean build artifacts
make clean
```

### Manual Build

```bash
# Standard build
go build -o gh-pmu .

# With version info
go build -ldflags "-X main.version=dev" -o gh-pmu .

# Cross-compile
GOOS=linux GOARCH=amd64 go build -o gh-pmu-linux .
GOOS=darwin GOARCH=arm64 go build -o gh-pmu-darwin .
```

## Testing

### Unit Tests

```bash
# Run all tests
go test ./...

# With verbose output
go test -v ./...

# Specific package
go test -v ./internal/api/...

# With coverage
go test -cover ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### Running Tests with Make

```bash
make test           # Run tests
make test-coverage  # Run with coverage
make test-verbose   # Verbose output
```

### Integration Tests

Integration tests run against real GitHub API with test fixtures.

**Setup:**
```bash
export TEST_PROJECT_OWNER="rubrical-works"
export TEST_PROJECT_NUMBER="99"
export TEST_REPO_OWNER="rubrical-works"
export TEST_REPO_NAME="gh-pmu-test"
```

**Run:**
```bash
go test -v -tags=integration ./...
```

**Via GitHub Actions:**
```bash
gh workflow run integration-tests.yml -f test_type=all
```

## Test Coverage

[![codecov](https://codecov.io/gh/rubrical-works/gh-pmu/graph/badge.svg)](https://codecov.io/gh/rubrical-works/gh-pmu)

Coverage reports are available on [Codecov](https://codecov.io/gh/rubrical-works/gh-pmu).

Coverage is uploaded automatically on every CI run. Click the badge above for detailed per-file breakdown.

### Why Coverage is ~68%

The practical coverage ceiling is 68-70% due to functions that are difficult to unit test:

- **Interactive CLI** - Functions using `os.Stdin` for prompts (`runInit`, `runFilter`)
- **External processes** - Functions calling `exec.Command` for git operations
- **Terminal UI** - Kanban board and history rendering requiring visual verification

These are tested manually. Refactoring for testability is tracked in issues #415 and #416.

For comprehensive testing strategy, coverage targets, and functions excluded from unit testing, see [TESTING.md](../TESTING.md).

## Project Structure

```
gh-pmu/
├── cmd/                    # Command implementations
│   ├── root.go            # Root command and flags
│   ├── init.go            # init command
│   ├── list.go            # list command
│   ├── view.go            # view command
│   ├── create.go          # create command
│   ├── move.go            # move command
│   ├── close.go           # close command
│   ├── board.go           # board command
│   ├── sub.go             # sub-issue commands
│   ├── intake.go          # intake command
│   ├── triage.go          # triage command
│   ├── split.go           # split command
│   └── ...
├── internal/
│   ├── api/               # GitHub API client
│   ├── config/            # Configuration loading
│   └── ui/                # Terminal UI components
├── docs/                  # Documentation
├── .gh-pmu.json           # Project configuration
└── main.go               # Entry point
```

## Code Style

- Follow standard Go conventions
- Run `gofmt` before committing
- Use meaningful variable names
- Add comments for exported functions

```bash
# Format code
gofmt -w .

# Lint
golangci-lint run
```

## Debugging

### Verbose Output

```bash
# Enable debug logging
GH_PMU_DEBUG=1 gh pmu list
```

### API Inspection

```bash
# See raw API calls
GH_DEBUG=api gh pmu view 42
```

### Local Testing

```bash
# Run built binary directly (not as extension)
./gh-pmu list

# With local config
./gh-pmu --config ./test-config.yml list
```

## Release Process

Releases are managed via the `/prepare-release` Claude command, which:

1. Analyzes commits since last tag
2. Recommends semantic version
3. Updates CHANGELOG.md
4. Reviews documentation freshness
5. Creates and pushes tag
6. Monitors CI release pipeline

Manual release:

```bash
# Tag and push
git tag v0.x.y
git push origin v0.x.y
```

GoReleaser handles binary builds and GitHub release creation.

## CI/CD

GitHub Actions workflows:

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `ci.yml` | Push/PR/Tag | Test (`-short`) on Linux, macOS and Windows (`-race` on Linux/macOS; coverage and its floor on Linux only), lint (incl. compile-check of `integration`/`e2e`-tagged tests), build and run `--version` on Linux, macOS and Windows, release |
| `gosec.yml` | Push/PR to main, weekly | gosec security scan once per `GOOS` (linux, darwin, windows); SARIF uploaded per GOOS |
| `codeql.yml` | Push/PR to main, weekly | CodeQL `security-extended` analysis once per `GOOS` (linux, darwin, windows); results categorized per GOOS |

> **Live-API tests never run in CI.** The integration (`-tags integration`)
> and e2e (`-tags e2e`) suites make real GitHub API calls and are run
> **locally, manually** (running them in CI caused an account lockout — see
> `Proposal/Integration-Test-Alternatives` and #876). CI only *compiles* them
> via `go vet -tags "integration e2e" ./...` so they can't rot unnoticed.
> See [TESTING.md](../TESTING.md) for how to run them.

## See Also

- [CONTRIBUTING.md](../CONTRIBUTING.md) - Contribution guidelines
- [Commands Reference](commands.md) - All commands
- [Configuration](configuration.md) - Config file format
