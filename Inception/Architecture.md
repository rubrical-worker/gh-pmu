# Architecture: gh-pmu

**Last Updated:** 2026-09-14

---

## Overview

gh-pmu is a GitHub CLI extension built as a single Go binary. It extends `gh` with project management commands that combine issue operations with GitHub Projects v2 field mutations.

## Architecture Style

**Style:** CLI Extension

**Rationale:** Leverages the existing `gh` CLI ecosystem, provides native terminal experience, and distributes as a static binary without runtime dependencies.

---

## System Context

```
┌─────────────────────────────────────────────────┐
│                    gh CLI                        │
│  ┌───────────────────────────────────────────┐  │
│  │              gh-pmu Extension              │  │
│  │  ┌─────────┐  ┌─────────┐  ┌──────────┐  │  │
│  │  │   cmd   │  │ internal │  │ templates│  │  │
│  │  │(Cobra)  │  │  /api    │  │  (YAML)  │  │  │
│  │  └────┬────┘  └────┬────┘  └──────────┘  │  │
│  │       │            │                      │  │
│  │       └────────────┼──────────────────────│  │
│  │                    │                      │  │
│  └────────────────────┼──────────────────────┘  │
└───────────────────────┼─────────────────────────┘
                        │
                        ▼
              ┌─────────────────┐
              │  GitHub GraphQL │
              │       API       │
              └─────────────────┘
```

---

## Component Overview

| Component | Responsibility | Technology |
|-----------|----------------|------------|
| cmd/ | Command parsing, validation, output | Cobra CLI |
| internal/api/ | GraphQL queries and mutations | go-gh, shurcooL-graphql |
| internal/config/ | Config loading, alias resolution | encoding/json |
| internal/defaults/ | Default labels, fields, and config values | Go |
| internal/framework/ | IDPF framework detection and validation | Go |
| internal/integrity/ | Config checksum and drift detection | Go |
| internal/testutil/ | Shared test helpers and fixtures | Go |
| internal/ui/ | Terminal styling, board rendering | Lipgloss |
| internal/version/ | Version constant and display | Go |

---

## Data Flow

### Primary Data Path

1. User invokes `gh pmu <command> <args>`
2. Cobra parses flags and arguments
3. Config loaded from `.gh-pmu.json`
4. API client executes GraphQL query/mutation
5. Response formatted and output to terminal

### Typical Command Flow

```
User Input → Cobra Parsing → Config Load → API Call → Format Output
    │              │              │            │            │
    ▼              ▼              ▼            ▼            ▼
 "gh pmu     Parse flags    Load         GraphQL      Table/JSON
  move 42    & args        .gh-pmu.json  mutation     to stdout
  --status
  done"
```

---

## External Integrations

| System | Type | Purpose |
|--------|------|---------|
| GitHub GraphQL API | API | Project field mutations, issue queries |
| GitHub REST API | API | Issue operations (delegated to gh) |
| gh CLI | Extension Host | Authentication, base commands |

---

## Key Architectural Decisions

| Decision | Choice | Alternatives Considered | Rationale |
|----------|--------|------------------------|-----------|
| API protocol | GraphQL | REST | Projects v2 requires GraphQL for mutations |
| Config format | JSON | YAML, TOML | `.gh-pmu.json` — YAML support removed in v1.4.x |
| CLI framework | Cobra | urfave/cli | Standard for Go CLI, better completion |
| Distribution | gh extension | Standalone binary | Leverage existing auth, ecosystem |

---

## Non-Functional Considerations

| Aspect | Approach |
|--------|----------|
| Scalability | Single-user CLI, no scaling concerns |
| Security | Delegated to `gh` auth, no credential storage |
| Performance | < 500ms startup, batched API calls |
| Observability | Error messages to stderr, `--verbose` flag |

---

*See also: Tech-Stack.md, Constraints.md*
