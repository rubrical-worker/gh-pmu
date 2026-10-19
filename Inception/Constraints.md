# Constraints: gh-pmu

**Last Updated:** 2026-10-19

---

## Technical Constraints

### Required Technologies

| Technology | Reason | Flexibility |
|------------|--------|-------------|
| Go 1.23+ | Module features, gh ecosystem | None |
| go-gh SDK | GitHub CLI extension system | None |
| GraphQL | Projects v2 field mutations | None |
| Cobra | CLI framework standard | Low |

### Prohibited Technologies

| Technology | Reason |
|------------|--------|
| CGO | Cross-compilation complexity |
| External databases | CLI should be stateless |
| Web frameworks | Terminal-only tool |

### Compatibility Requirements

| Requirement | Specification |
|-------------|---------------|
| OS support | Windows, macOS, Linux (amd64, arm64 release binaries); CI builds and tests natively on Linux and macOS only — Windows is cross-compiled by GoReleaser, not in the CI build matrix |
| gh CLI version | v2.0+ |
| GitHub | Projects v2 (not classic) |

---

## Resource Constraints

### Team

| Aspect | Value |
|--------|-------|
| Team size | Solo developer |
| Availability | Part-time |
| Development style | AI-assisted |

### Budget

| Category | Constraint |
|----------|------------|
| Infrastructure | GitHub Actions free tier |
| Third-party services | None required |
| Tools/licenses | Open source only |

### Timeline

| Milestone | Deadline | Flexibility |
|-----------|----------|-------------|
| Releases | As needed | Soft |
| Bug fixes | P0 < 1 week | Hard |

---

## Business Constraints

### Stakeholder Requirements

| Stakeholder | Requirement | Priority |
|-------------|-------------|----------|
| Users | Stable CLI interface | P0 |
| Contributors | Clear documentation | P1 |
| Ecosystem | gh extension compatibility | P0 |

### Regulatory/Compliance

| Regulation | Requirement | Impact |
|------------|-------------|--------|
| MIT License | Attribution required | Low |
| No PII | No data collection | None |

---

## Operational Constraints

### Infrastructure

| Constraint | Specification |
|------------|---------------|
| Hosting | GitHub Releases |
| Distribution | GoReleaser cross-compile |
| Updates | `gh extension upgrade` |

### Security

| Constraint | Requirement |
|------------|-------------|
| Authentication | Delegated to gh CLI |
| Data handling | No local storage of secrets |
| Audit requirements | None (via GitHub audit log) |

---

## Constraint Trade-offs

| Constraint A | vs | Constraint B | Decision |
|--------------|----|--------------|---------|
| Feature richness | vs | Binary size | Keep < 15MB |
| API completeness | vs | Rate limits | Batch where possible |
| Cross-platform | vs | Native features | Go standard library |

---

*See also: Architecture.md, Tech-Stack.md*
