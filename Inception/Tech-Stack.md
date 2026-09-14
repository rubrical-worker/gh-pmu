# Tech Stack: gh-pmu

**Last Updated:** 2026-09-14

---

## Core Stack

| Layer | Technology | Version | Rationale |
|-------|------------|---------|-----------|
| Language | Go | 1.23 | Static binary, CLI performance |
| Runtime | Native binary | - | No runtime dependencies |
| Framework | Cobra | 1.10.1 | Standard Go CLI framework |
| API Client | go-gh | 2.12.1 | Official GitHub CLI SDK |
| GraphQL | shurcooL-graphql | 0.0.4 | GitHub GraphQL API access |

---

## Development Tools

| Tool | Version | Purpose |
|------|---------|---------|
| Package Manager | go mod | 1.23 | Dependency management |
| Build Tool | go build / GoReleaser | - | Binary compilation, releases |
| Linter | golangci-lint | Latest | Code quality |
| Formatter | gofmt | Built-in | Code formatting |
| Test Framework | go test | Built-in | Unit, integration (`-tags integration`) and e2e (`-tags e2e`) tests |
| Security Scanning | gosec, CodeQL | GitHub Actions | Static security analysis (`gosec.yml`, `codeql.yml`) |

---

## Infrastructure

| Component | Technology | Environment |
|-----------|------------|-------------|
| Hosting | GitHub Releases | Production |
| CI/CD | GitHub Actions | All environments |
| Container | N/A | CLI binary distribution |
| Distribution | GoReleaser | Cross-platform builds |

---

## Key Dependencies

### Production Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| github.com/cli/go-gh/v2 | 2.12.1 | GitHub CLI SDK |
| github.com/cli/shurcooL-graphql | 0.0.4 | GraphQL client |
| github.com/spf13/cobra | 1.10.1 | CLI framework |
| gopkg.in/yaml.v3 | 3.0.1 | YAML parsing (embedded defaults) |
| github.com/charmbracelet/lipgloss | 1.1.1 (pre-release) | Terminal styling |
| github.com/vektah/gqlparser/v2 | 2.5.36 | Offline GraphQL validation against the vendored schema |
| golang.org/x/term | 0.30.0 | Terminal detection |

### Notable Indirect Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| charmbracelet/x/ansi | 0.8.0 | ANSI escape codes (indirect) |
| mattn/go-isatty | 0.20.0 | TTY detection (indirect) |

---

## Version Constraints

| Dependency | Constraint | Reason |
|------------|------------|--------|
| Go | >= 1.23 | Module features, generics |
| go-gh | v2.x | GitHub CLI v2 compatibility |

---

## Upgrade Plan

| Dependency | Current | Target | Timeline |
|------------|---------|--------|----------|
| Cobra | 1.10.1 | Latest | As needed |

---

*See also: Architecture.md, Constraints.md*
