# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.6.0] - 2026-09-14

`gh pmu intake` is now much faster: it reads each issue's project membership
directly instead of scanning the whole board, and `--apply` adds issues and sets
their fields in batches. A new `gh pmu status --update` brings a project's Status
field up to the nine standard values with their default colors, and `gh pmu init`
now adds missing Status values instead of refusing to continue. Terms acceptance
now carries across minor and patch upgrades and is asked for again only when the
major version changes.

**Upgrade notes:**
- `gh pmu intake` with no mode flag now prints help instead of listing issues. Scripts that ran bare `gh pmu intake` must add `--list` (#918)
- An existing terms acceptance recorded on 1.x stays valid through this and later 1.x releases; no `gh pmu accept` is needed after upgrading (#919)

### Added
- `gh pmu status --update` adds missing required Status values, fixes capitalization-only name differences in place, applies default colors and fills empty descriptions. It never deletes, reorders or touches custom values, and refuses to run on cached field metadata (#917)
- `gh pmu intake --list` as an explicit listing mode; `--list` and `--apply` are mutually exclusive (#918)
- `config verify` prints an advisory `REQUIRED STATUS VALUES MISSING` alert when `.gh-pmu.json` lacks any of the 9 required Status values. It reads the local config only and never changes the exit code (#917)
- `auto-filed` standard label for issues filed by the hall-monitor, created by `gh pmu label sync` (#915)
- Default Status field definitions (`Backlog`, `Up next`, `Ready`, `In progress`, `In review`, `QA required`, `Done`, `Parking Lot`, `Notes`) with colors and descriptions in `internal/defaults/defaults.yml` (#917)

### Changed
- `gh pmu intake` finds candidates with a repository-scoped search that reports each issue's project membership, replacing a full board scan filtered client-side. Measured on this project: 12.2s → 1.2s (#918)
- `gh pmu intake --apply` adds issues with aliased batch mutations (50 per request) and applies field values in batches, reporting `Added N issue(s) to project (M failed)` (#918)
- An issue belonging to more than 20 projects is excluded from intake with a warning, since its membership cannot be confirmed from the first page (#918)
- `gh pmu init` on an existing project adds missing required Status values and fixes capitalization instead of failing validation; a failure rolls back the init (#917)
- Terms re-acceptance is required only on a major version change, in either direction. Missing acceptance, dev-build acceptance and unparseable versions still require acceptance (#919)
- `docs/configuration.md` documents all 9 Status aliases (#917)

### Documentation
- Proposals: MCP Server Front End (#913) and Fail-Loud CLI Contract (#914)
- Design decisions for intake membership from issue project items (#918) and Status option reconcile (#917)
- Charter refresh: Key Entities, updated Architecture, Tech-Stack and Test-Strategy, declared test suites
- IDPF-Praxis framework upgraded to 0.103.0

## [1.5.3] - 2026-08-25

Released as a patch rather than the minor `recommend-version.js` suggested, for
the third release running. The script classifies #905 as a feature because the
issue carries the `enhancement` label, and on the merits it has a case — the
`version` key now rewrites itself, and the YAML migration path is gone. It is
still stabilization work: `version` recorded which binary *created* a config and
then drifted permanently, and `MigrateYAML` was the vestigial carrier of that
one-shot stamp. Nothing here adds a capability.

Four of the five issues are correctness fixes in code that decides whether an
issue may close, or whether a config write loses data.

### Added
- `.gh-pmu.json`'s top-level `version` is refreshed to the running binary's version whenever it is stale. The check runs on every non-exempt command; the *write* happens only on mismatch, so an up-to-date config is never rewritten and the file's mtime is left alone (#905)
- `Config.Save` preserves top-level keys the running binary does not model, rather than dropping them. Unknown keys are captured on load and spliced back on write, so a config written by a newer gh-pmu survives a round-trip through an older one (#910)

### Fixed
- `stripCodeBlocks` tested the *trimmed* line for a fence delimiter, so a fence indented by one to three spaces toggled fence state where CommonMark says it opens a block, and a four-space-indented line inside a list was read as a fence. Delimiters are now matched against the raw line under CommonMark's rules: 0–3 spaces of indent, a closer at least as long as its opener, and blockquote prefixes handled (#907)
- `isIndentedCode` carried a carve-out exempting any line that looked like a checkbox from indented-code stripping, so a checkbox nested under a list item was counted while its neighbours were not. Indented code is now discriminated by list context — whether the line continues a shallower list item — instead of by the presence of a `- [ ]` glyph (#908)
- Blockquoted content is stripped before checkbox counting. The three checkbox patterns anchor to `^\s*[-*+]` and `\s` does not match `>`, so a quoted `- [ ]` was already invisible to the count; the rule makes that exclusion a stated mechanism rather than an accident of regex anchoring, and a regression test fails loudly if a future change teaches the patterns a `>` prefix (#911)

### Changed
- `version` joins `driftExcludedKeys`, so the config integrity check no longer reports a tool-written version stamp as unauthorized local modification (#905)
- `README.md` pointed at `.gh-pmu.yml` in its documentation table. The YAML config has not been read since `FindConfigFile` stopped falling back to it, and #905 removed the last code path that touched the file at all (#905)

### Removed
- `config.MigrateYAML` and its invocation from `PersistentPreRunE`. It short-circuited to a no-op unless a legacy `.gh-pmu.yml` sat beside the JSON, and deleted that file on first hit, so it fired at most once per repository ever. A repository still carrying a `.gh-pmu.yml` is no longer auto-migrated (#905)

### Compatibility
- Repurposing `version` as a last-written stamp forecloses its original use as a staleness *signal*. #700 introduced the field so an upgrade could detect that `init` needed re-running; that consumer was never built, and now cannot be — the field always equals the running binary. Init-staleness detection would need a second field. Recorded in `Construction/Design-Decisions/2026-08-20-config-version-refresh-semantics.md` (#905)
- Unknown-key preservation is a splice into the encoded struct, not a merge into one map, specifically so key order in existing files is left untouched. A merge would have silently re-alphabetized every `.gh-pmu.json` on its next write. Byte-for-byte output equality against the pre-change binary is asserted by a golden test (#910)

### Documentation
- Proposal: Multi-Provider Support, covering a GitLab provider behind the existing GitHub-shaped interfaces (#909)
- IDPF-Praxis framework upgraded to 0.97.0

## [1.5.2] - 2026-08-19

Released as a patch rather than the minor `recommend-version.js` suggested,
following the same reasoning as 1.5.1: the v1.5.x line is scoped to stabilizing
v1.5.0's correctness work. `config verify --resolve-view` is a new flag and is
technically additive, but it exists to make the drift report correct rather than
to add a capability.

### Added
- `gh pmu config verify --resolve-view` resolves the Backlog board view number via `repositoryOwner` and persists it as `project.view` in `.gh-pmu.json` (#901)

### Fixed
- `project.view` is excluded from the config drift comparison, so a tool-written view no longer reads as unauthorized modification (#901)
- `Save` no longer writes an empty `"release": {}` key into `.gh-pmu.json`. `Config.Release` was a non-pointer struct tagged `omitempty`, which `encoding/json` does not omit, so the key reappeared after every write (#902)

### Removed
- The `release` config section and its `tracks`/`artifacts`/`coverage` sub-config, along with the six types and eleven getters over them. Nothing outside the config package's own tests read any of it; #504 removed `release.active` and `cache.releases` but left the enclosing block behind (#902)
- `docs/configuration.md` no longer documents a `release` section. It described a coverage gate "for /prepare-release" in stale YAML syntax; the gate script hardcodes its own threshold and never read the config (#902)

### Compatibility
- A `.gh-pmu.json` written by an older gh-pmu that still carries a populated `release` block loads without error or warning — `Load` uses plain `json.Unmarshal`, which ignores unknown keys. Covered by a regression test (#902)


## [1.5.1] - 2026-07-28

Released as a patch rather than the minor `recommend-version.js` suggested. The
new `@me` support is technically additive, but the v1.5.x line is scoped to
stabilizing v1.5.0's correctness and error-surfacing work, and that is what this
is: a flag that silently produced wrong results now produces right ones.

### Added
- `--assignee @me` resolves to the authenticated GitHub login on every command that accepts the flag — `create`, `sub create`, `list`, `filter`, and `intake` — plus `assignees:` entries in `create --from-file` payloads. Previously `@me` was passed through verbatim to `user(login:)`, which returns `NOT_FOUND` for it (#895)

### Fixed
- An unresolvable `--assignee` now fails the command instead of warning and continuing. Resolution runs before the `createIssue` mutation, so nothing is created — previously a typo'd or renamed login produced a successfully-created but unassigned issue with exit code 0 (#895)
- `@me` behaved inconsistently across `list` code paths: correct under the Search API, which resolves the `assignee:@me` qualifier server-side, but silently matching nothing under the client-side fallback. Both paths now resolve before matching, and the Search path returns the same results as before (#895)
- `sub create` echoed raw assignee values in its confirmation, printing `Assignees: @@me` while assigning the authenticated account (#895)

### Changed
- **Breaking (unexercised):** an invalid explicit `--assignee` login aborts with exit 1 where it previously warned and proceeded. This reverses #872 finding 4 — see `Construction/Design-Decisions/2026-07-28-assignee-resolution-failure-aborts.md`. No known caller supplies an explicit login; `@me` is the only value this project has passed, and it now resolves rather than failing (#895)
- Partial and total assignee-resolution failures share exit 1 and are distinguished by message, following the `subRemoveBatchError` precedent — `"1 of 3 assignees could not be resolved: ghost"` versus `"all 3 assignees could not be resolved"` (#895)

## [1.5.0] - 2026-07-23

### Added
- Pagination for user-facing fetches — labels, comments, projects, and project field values no longer silently truncate at fixed caps (labels 100, comments 50, projects 20, fieldValues 20); truncated internal fetches now emit warnings and caps are documented (#860)
- `GetProjectItemFieldValue` paginates project items and reports whether the item was found (#860)
- `Config.GetFieldNameOr` for caller-controlled field-name fallback (#863)

### Fixed
- Cross-project field contamination: `view` and batch issue fetches scope Status/Priority field values to the configured project instead of collecting from all projects an issue belongs to (#856)
- Zero-value NUMBER/TEXT project field updates are serialized instead of silently dropped (`omitempty` removed from `ProjectV2FieldValue`) (#857)
- Non-idempotent mutations (issue/comment/label creation) are no longer retried on 5xx, eliminating duplicate side effects; idempotent reads still retry (#858)
- Raw GraphQL client resolves its endpoint via go-gh — GHES and `GH_HOST` configurations now work (#859)
- `queries.go` response handling: integer error-path segments no longer abort whole batches; `ListProjects`/`GetProject` correctly attribute user-path failures (#861)
- Branch membership model unified between `branch add` and `branch close`; `add`/`remove`/`current` disambiguate when multiple active branch trackers exist instead of picking arbitrarily (#862)
- `create` and `close --update-status` resolve Status/Priority field names from `.gh-pmu.json` `fields` mappings instead of hardcoding `"Status"`/`"Priority"` (#863)
- `create --from-file` honors `--branch`/`--title`/`--template` flags, runs IDPF validation, and parses the file once (#864)
- Config-integrity gaps closed: strict mode is no longer self-referential, monorepo baseline uses the correct HEAD pathspec, and `init` detects a parent config from subdirectories (#865)
- Checkbox validation matches `* [ ]`, `+ [ ]`, and uppercase `[X]` — acceptance-criteria gates can no longer be bypassed by bullet style (#866)
- Silent no-op flags now work or fail loudly: `intake --apply` space form rejected, `sub create --inherit-assignees`/`--inherit-milestone` honored, dead `move --force` confirmation guard removed, `edit` with empty `--body-file`/`--body-stdin` errors instead of succeeding silently (#867)
- Read-command output correctness: `board` surfaces unrecognized statuses and validates `--state`, `list --limit` applies after client-side filters, `view` keys multi-issue results by (owner, repo, number), `history` refuses repo-root targets regardless of cwd (#868)
- `ResolveFieldValue` is case-insensitive; `MigrateYAML` verifies the written JSON before deleting the legacy YAML (#869)
- `Spinner` is race-free and restartable — no more data race on message or panic on Start after Stop (#870)
- cmd-layer silent failures now reported with partial-failure exit codes across `triage`, `branch`, `sub`, `split`, `list`, `move`, `comment`, and framework detection (#871)
- API-layer error swallowing eliminated: batch parsers, sub-issue counts, and label/assignee lookups propagate failures instead of returning empty data (#872)
- `config.Save` takes the config directory instead of silently discarding a file path's basename (#874)
- `sub` write paths guard against unresolved/empty issue node ids (#889)
- `GetProjectItemFieldValue` selects field name through `ProjectV2FieldCommon` — no more invalid GraphQL against the field-configuration union (#888)
- `comment --body-stdin` errors on input over 1MB instead of silently truncating
- `SummaryBox` key padding uses visible width (ANSI-aware)

### Changed
- Six commands share a single repository-resolution helper, unifying `--repo` validation and config fallback (#873)
- Internal refactors: generic cursor-pagination helper, shared ProjectV2 field-value helpers, shared IDPF required-field validation between init paths (#874)

### Testing
- Test-quality overhaul: retired the dormant integration/UAT layer, replaced ~35 assertion-free and tautological tests with real assertions, added command-level scenario tests on a wrapper harness with mock transport (#875, #876, #878, #880, #881, #882, #884, #891)
- GraphQL operations validated offline against a vendored GitHub schema with provenance tracking (#884)
- E2E suite hardened with exclusion/AND assertions and read-back verification; fixture owner updated for the `rubrical-works` → `rubrical-worker` account rename
- CI compile-checks tag-gated test suites; documented as the deliberate CI ceiling (#890)

## [1.4.10] - 2026-05-14

### Fixed
- `gh pmu` commands that translate project field names to option IDs (`create --status`, `move --status`, `move --priority`, `move --branch`, etc.) no longer fail when GitHub's `ProjectV2.fields` GraphQL resolver returns HTTP 500. When the bulk resolver crashes — observed 2026-05-14 after GitHub auto-provisioned new `Created`/`Closed`/`Updated` timestamp fields whose backing rows fail the `ProjectV2FieldCommon` `NON_NULL` contract — `GetProjectFields` now falls back to the `metadata.fields[]` cache in `.gh-pmu.json` and emits a single stderr warning. Commands keep working transparently. (#853)
- `gh pmu init` no longer aborts when the bulk fields resolver is crashing. It now falls back to per-name `ProjectV2.field(name:)` lookups against a documented standard ProjectV2 field set (Title, Assignees, Status, Labels, Linked pull requests, Milestone, Repository, Reviewers, Parent issue, Sub-issues progress, Priority, Size, Estimate, Start date, Target date, Branch), populates `metadata.fields[]` with what's reachable, and reports any unreadable fields. (#853)

### Added
- `internal/api.ErrFieldResolverUnavailable` sentinel + `internal/api.IsFieldResolverUnavailable` classifier. Detection routes: `errors.Is` against the sentinel, HTTP 500 via `httpStatusCoder`, message contains "Something went wrong while executing", or "non-200 OK status code: 500" (shurcoolGraphQL wire format). 502/503/504 remain transient and retryable via `IsTransient5xx`. (#853)
- `--no-cache-fallback` global flag — disables the cached-metadata fallback; resolver errors propagate unchanged (preserves the prior fail-loud behaviour for CI/strict automation). (#853)
- `--refresh-cached-fields` global flag — when fallback engages, refreshes each cached field via per-name `ProjectV2.field(name:)` queries (the resolver path that survives the rollout bug). Partial failures retain the cached value and log a count. (#853)
- `internal/api.FallbackOptions` + `internal/api.SetFallbackOptions` / `CurrentFallbackOptions` for programmatic control of the resilience layer. (#853)
- `internal/api.Client.FetchFieldsByNames(projectID, names)` + exported `internal/api.StandardProjectV2FieldNames` constant — used by `gh pmu init`'s recovery path; reusable for callers that need a resilient field fetch by name. (#853)
- `Resilience` section in README documenting the fallback behaviour, warning text, and flags. CHARTER `In Scope` updated with the new resilience commitment. (#853)

### Changed
- `getProjectFieldsPage` now wraps errors matching `IsFieldResolverUnavailable` with `errors.Join(ErrFieldResolverUnavailable, originalErr)` so callers can detect the resolver-crash class via `errors.Is` while preserving the original error chain (and trace IDs) for user-facing diagnostics. (#853)

## [1.4.9] - 2026-05-05

### Fixed
- `gh pmu init --source-project` now refuses by default when `.gh-pmu.json` already declares a project number — protects against silently replacing the project pointer when the user actually wanted to re-link via `--project <existing>`. New `--force` flag opts in to the explicit-replace path. (#847)
- `init` is now atomic: any hard failure after destination-project creation (field validation, refetch, config write) deletes the just-created project so no orphan is left on the org, and `.gh-pmu.json` is only written as the very last step. Previously a transient 5xx mid-init left an empty project on the org and a `.gh-pmu.json` rewritten to point at it. (#847)
- `init` hard failures emit a structured trailer on stderr (`destinationProjectNumber=N`, `failedStep=...`, `configRewritten=bool`) so callers like px-manager can branch on machine-readable fields instead of screen-scraping. (#847)
- All gh-pmu commands now retry transparently on transient GitHub HTTP errors — `IsRetryable` classifier covers 502/503/504 in addition to 429/rate-limited 403, and the retry decorator is installed at API client construction so every command (`init`, `edit`, `view`, etc.) inherits it without per-call-site wrapping. Closes a long-standing latent gap where typed-GraphQL errors from go-gh's underlying `shurcoolGraphQL` weren't matched by the rate-limit classifier. (#849)

### Added
- `internal/api.IsTransient5xx` and `internal/api.IsRetryable` classifiers; `internal/api.DeleteProject` mutation for init rollback. (#847, #849)
- `--force` flag on `gh pmu init` (overrides the `--source-project` guard described above). (#847)

### Changed
- `WithRetry` now uses `IsRetryable` (rate-limit ∪ transient 5xx) and applies positive-bias jitter to default retry delays to spread coordinated-client retries. `Retry-After` header still overrides default delay exactly. (#849)

## [1.4.8] - 2026-04-29

### Fixed
- `gh pmu init --project` (connect to existing project) now creates optional fields listed in `defaults.Fields.CreateIfMissing` (Priority, Branch) when they are absent on the project, mirroring the `--source-project` path. Previously the existing-project path skipped the create-if-missing loop entirely, leaving connected projects without a Branch field and no `branch` mapping in the generated `.gh-pmu.json` (#844)

### Changed
- Extract `ensureOptionalProjectFields` helper used by both `runInitNonInteractive` and `runInitExistingProject` so the create-if-missing behavior has a single implementation across init paths (#844)

## [1.4.7] - 2026-04-23

### Fixed
- `gh pmu view` on branch trackers: strip the trailing `### Sub-Issues` placeholder section (dead text from the pre-fix `/create-branch` body template) so the rendered output no longer shows a redundant/empty section after the top-block sub-issue list. Sections with real content are preserved unchanged (#838)

### Added
- `gh pmu init` now emits `fields.branch: { field: "Branch" }` in the generated `.gh-pmu.json` when the target project has a Branch field, so config-path callers (`cmd/branch.go` readers of `cfg.Fields["branch"]`) resolve the same field as `ResolveBranchFieldName` (#841)
- Re-init preserves a user-renamed `fields.branch.field` alias instead of clobbering it with the project's canonical field name (#841)

### Changed
- `.claude/commands/create-branch.md` body template no longer writes the `### Sub-Issues` placeholder section into newly created branch trackers (#838)

## [1.4.6] - 2026-04-15

### Fixed
- Subcommand `--help` usage line now renders `gh pmu <cmd>` instead of `gh <cmd>` (#831)

### Security
- Validate `owner`, `repo`, `label`, and GraphQL node-ID inputs before interpolation into raw GraphQL query strings; reject characters outside the GitHub-permitted alphabet at the API-client boundary rather than relying on coincidental Go/GraphQL escape overlap (#832, #835)
- Applied boundary validation across `SearchRepositoryIssues`, `GetRepositoryID`, `GetIssue`, `GetLabel`, `GetSubIssues`, `GetSubIssueCounts`, `GetSubIssuesBatch`, `GetRepositoryIssues`, `GetOpenIssuesByLabel(s)`, `GetClosedIssuesByLabel`, `GetParentIssue(Batch)`, `GetIssueComments`, `GetIssuesWithProjectFieldsBatch`, `GetProjectItemsByIssues`, `ListLabels`, `getLabelID(s)`, `CreateLabel`, `UpdateLabel` (#835)

### Performance
- `gh pmu intake --apply` now pre-fetches project fields once per invocation and uses `SetProjectItemFieldWithFields` instead of the N×3 `SetProjectItemField` pattern; API calls scale with `1 + 2N` instead of `1 + 3N` for N issues with status + priority defaults (#833)

### Changed
- `gh pmu config verify` surfaces `ComputeChecksum` / `LoadChecksum` I/O errors to stderr instead of silently discarding them; remote critical-alert path now writes to `cmd.ErrOrStderr()` so alerts are captured by test fixtures that respect cobra's stderr redirection (#834)

## [1.4.5] - 2026-03-27

### Fixed
- Replace anonymous struct with named `LinkProjectV2ToRepositoryInput` type in `LinkProjectToRepository` — fixes "Missing type definition for variable: $input" GraphQL error during `gh pmu init` (#824)

### Added
- `--project` flag on `gh pmu init` to connect to an existing GitHub ProjectV2 without copying from a template (#825)

### Changed
- Deprecate `--non-interactive` flag on `gh pmu init` — flag is a no-op and will be removed in a future release (#826)

### Documentation
- Update `docs/commands.md` with `init` flag reference (`--project`, `--source-project`, `--repo`, etc.)
- Fix stale `.gh-pmu.yml` references to `.gh-pmu.json`
- Update README with missing commands and framework integration
- Remove irrelevant files and stale docs references

## [1.4.4] - 2026-03-26

### Security
- Remove redundant double-escaping in GraphQL label queries (`getLabelIDs`)
- Add `io.LimitReader` for unbounded `io.ReadAll` calls (10MB API responses, 1MB stdin)
- Validate git command arguments against safe character pattern (`validateGitRef`)
- Escape markdown special chars in history table output (`escapeMarkdown`)
- Use `url.PathEscape` for URL construction in history HTML output
- Add 30s timeout to jq command execution in `view`
- Add size validation (1MB) for template file reads in `create`
- Isolate test transport/auth overrides from production code path

### Performance
- Add default max limit (10,000) for `GetProjectItems` pagination
- Replace O(n^2) bubble sort with `sort.Slice` in branch version sorting
- Use `json.NewDecoder` for streaming stdin parse in filter command
- Hoist `uncheckedItemRegex` to module-level compiled variable
- Replace `joinFeatures` string concatenation loop with `strings.Join`
- Replace O(n^2) label deduplication with map-based set
- Pre-allocate filter slices with capacity hints

### Fixed
- Surface silent JSON unmarshal failures in batch operations with warnings
- Log warnings for field/label creation failures in `init` command
- Add debug-level logging for silent pre-run hook failures
- Validate repo format splits reject empty owner/repo parts
- Return partial-failure error when sub-issue linking fails (was silent success)
- Log warnings for `GH_PM_PROJECT_NUMBER` parse failures
- Collect all label operation errors in `edit` instead of failing on first
- Log warnings for `time.Parse` errors in history output

### Added
- Unit tests for `validateGitRef`, `escapeMarkdown`, `io.LimitReader` boundary behavior
- Unit tests for `sortBranchesByVersionDesc` correctness
- Unit tests for `GetProjectItems` default pagination limit
- Unit tests for streaming JSON parse equivalence
- Unit tests for error handling behavior changes (date parse, repo validation)
- E2E test suite README with run instructions

## [1.4.3] - 2026-03-25

### Security
- Updated `github.com/cli/go-gh/v2` from v2.11.1 to v2.12.1 — fixes vulnerability where compromised GitHub Enterprise Server could execute arbitrary commands via CLI extensions (Dependabot alert #1)

## [1.4.2] - 2026-03-25

### Changed
- Removed all interactive/stdin prompts — all commands now require `--yes` or `--force` for non-interactive use
- Removed `--web` flag and `OpenInBrowser` from `create` command
- Removed `--editor` flag and `openEditorForBody` from `create` command
- Removed `--interactive` flag from `triage` command
- Replaced `exec.Command` gh subprocess calls with go-gh REST/GraphQL clients in `create`, `close`, `mutations`, and `history` commands
- Excluded gosec G204 for git subprocess calls and documented gosec suppressions
- Removed dead code from `init` command (`detectRepository`, `autoCreateProject`, `runIntakeForInit`)

### Fixed
- Guard `uintptr`-to-`int` conversion in `getTerminalWidth` (gosec G115)

### Added
- Unit tests for `close.go` GraphQL mutation paths
- Unit tests for `history.go` REST client (`getCommitComments`)
- Unit tests for `doRawGraphQL` client construction

## [1.4.1] - 2026-03-25

### Added
- Gosec security scanning workflow with SARIF upload to GitHub Security tab (#787)
- Critical config field drift detection in `config verify` — alerts when `project.owner`, `project.number`, or `repositories[0]` change from HEAD with a prominent boxed warning on stderr (#792)
- Gosec badge in README

### Changed
- `gh pmu init` now writes only `.gh-pmu.json` — no longer creates `.gh-pmu.yml` (#791)
- `DetectFramework()` and `loadExistingFramework()` read from `.gh-pmu.json` instead of `.gh-pmu.yml`
- `FindConfigFile()` YAML fallback removed; `ConfigFileNameYAML` constant removed
- All error messages now reference `.gh-pmu.json` instead of `.gh-pmu.yml`
- `MigrateYAML()` retained as safety net for users upgrading from pre-v1.4.0

### Fixed
- Tightened file write permissions to `0600` and directory permissions to `0750` (gosec G306/G301/G302) (#787)
- Handled previously ignored errors in `cmd/create.go` and `cmd/list.go` (gosec G104)
- Excluded gosec G304 (file inclusion via variable) — expected for CLI tools that read user-supplied paths

## [1.4.0] - 2026-03-25

### Added
- `config verify` now includes unchanged top-level sections alongside changed ones when drift is detected (#783)

### Changed
- One-time migration: when `.gh-pmu.json` exists and `.gh-pmu.yml` is present, the legacy YAML config is automatically deleted and the JSON config version is updated to the current CLI version (#782)
- `Save()` no longer writes a `.gh-pmu.yml` companion file — JSON is the sole config format
- Upgraded IDPF framework to v0.70.0

## [1.3.2] - 2026-03-22

### Changed
- `--force` on `gh pmu move` now implies `--yes` for the checkbox confirmation prompt, preventing hangs in non-interactive environments (#778)

## [1.3.1] - 2026-03-17

### Added
- Config integrity check: daily comparison of local `.gh-pmu.json` against git HEAD (#773)
- `gh pmu config verify` subcommand for on-demand integrity verification
- `--remote` flag for origin/main comparison in config verify
- Strict mode (`configIntegrity: "strict"`) to block commands on config drift
- SHA-256 checksum file (`.gh-pmu.checksum`) for fast config validation
- Daily throttle with ISO 8601 midnight boundary for integrity checks
- Auto-update checksum on config save (accept, field commands)

## [1.3.0] - 2026-03-15

### Added
- `gh pmu label` command group for managing repository labels (#767)
  - `label sync` — sync standard labels from defaults.yml
  - `label list` — list all labels with standard/custom indicator
  - `label add` — create a label
  - `label update` — update a label's color or description
  - `label delete` — delete a label
- Cursor-based pagination for `GetProjectItemID` (#752)
- Shared `resolveLabelIDs` extracted from `CreateIssue` variants (#751)

### Fixed
- `matchesTriageQuery` now evaluates positive and negative labels independently (#763)
- Sequential `TrimPrefix` double-strip in create command (#756)
- `Header` padding uses `visibleWidth` instead of `len` (#754)
- `ensureGitignore` file locking on Windows (#753)
- `IsAuthError` false-positives on strings containing "401" (#750)
- `os.Stdin.Stat()` nil panic in filter command (#757)
- `NewClient`/`NewClientWithOptions` return error instead of nil-gql guards (#749)
- E2E init tests clean up created projects and fix config detection

### Changed
- Route all `run*WithDeps` output through `cmd.OutOrStdout()` (#766)
- Decompose `collectSubIssuesRecursive` into focused functions (#762)
- Make `protectRepoRoot` thread-safe with `atomic.Bool` (#760)
- Replace `TrimSuffix` chain with keyword lookup map in `parseCommitReferences` (#759)
- Replace pipe delimiter with null byte in git log parsing (#758)
- Inject `io.Reader` for stdin in comment command (#755)
- Guard nil `subIssuesMap` in recursive move batch fallback (#761)
- Guard test globals with `sync.Mutex` to fix data race (#748)
- Upgraded hub framework to v0.62.1

## [1.2.1] - 2026-03-11

### Changed
- Rebranded "Rubrical Systems" to "Rubrical Works" across source, tests, terms, and changelog (#745)
- Updated README IDPF-Praxis links from `idpf-praxis-dist` to `idpf-praxis` (#745)

## [1.2.0] - 2026-03-09

### Changed
- Rebranded from "Project Management Unified" to "Praxis Management Utility" — PMU acronym unchanged (#698)
- Renamed organization from rubrical-studios to rubrical-works (#736)
- Made `.gh-pmu.json` the primary config file with `.gh-pmu.yml` as fallback (#737)
- Updated `gh pmu init` display strings to reference `.gh-pmu.json` (#735)
- Updated LICENSE copyright to rubrical-works 2026 (#698)

### Optimized
- `gh pmu list --state all` uses dual search calls (open + closed) instead of full project scan (#740)
- `gh pmu filter` eliminates dead-code `GetProjectItems` fallback; errors when no repos configured (#741)
- `gh pmu board --state all` uses dual search calls instead of full project scan (#742)
- `gh pmu move --recursive` resolves sub-issues via targeted API instead of full project scan (#743)
- `gh pmu branch current` optimized with active label lookup and `--json` support; removed `--refresh` (#738)
- `gh pmu branch close` optimized to use tracker sub-issues instead of full project scan (#739)

## [1.1.0] - 2026-03-03

### Added
- Multi-issue support for `gh pmu view` (#725)
  - Accepts variadic issue numbers: `gh pmu view 42 43 44`
  - `--json` returns a JSON array for multiple issues, single object for one (backward compatible)
  - `--jq` works with multi-issue JSON array output
  - Non-JSON output shows issues sequentially with separator
  - Invalid issue numbers report errors without blocking valid issues
  - `--body-stdout`, `--body-file`, and `--web` restricted to single issue
- Batch GraphQL API methods for O(1) multi-issue fetching (#725)
  - `GetIssuesWithProjectFieldsBatch` — fetch N issues + project fields in one query
  - `GetParentIssueBatch` — fetch parent issues for N issues in one query

### Changed
- Renamed "Rubrical Studios" to "Rubrical Works" in `--version` output and embedded terms (#726)

## [1.0.5] - 2026-03-02

### Fixed
- `gh pmu move --backlog` now sets status to Backlog in addition to clearing the branch field (#721)
  - Previously only cleared branch association; status remained unchanged
  - Explicit `--status` flag takes precedence when combined with `--backlog`

## [1.0.4] - 2026-03-01

### Changed
- Removed "Board" suffix from project title during `gh pmu init` (#718)
  - `runInitNonInteractive()` and `autoCreateProject()` now use the repository name directly as the project title
  - Extracted `deriveProjectTitle()` helper for testability

## [1.0.3] - 2026-03-01

### Changed
- Renamed `--project` flag to `--source-project` in `gh pmu init --non-interactive` (#714)
  - Non-interactive mode now creates a new project by copying from the source project template
  - Links repository to the newly created project automatically
  - Config file records the new project number (not the source)
- Fixed `CopyProjectV2` GraphQL mutation to use named input type (#714)

## [1.0.2] - 2026-02-27

### Added
- 403/429 HTTP rate limit detection with automatic retry and exponential backoff (#710)
  - Interface-based duck typing for go-gh's `HTTPError` (avoids package name collision)
  - `IsRateLimited()` detects HTTP 429, 403 with rate-limit messaging, and sentinel errors
  - `GetRetryAfter()` extracts Retry-After header value from errors
  - `WithRetry()`/`WithRetryDelays()` with configurable exponential backoff (1s, 2s, 4s, 8s)
  - Non-rate-limit 403 errors (permission denied) are not retried
- Copyright line in `--version` output (#712)
  - `gh pmu --version` now shows `Rubrical Works (c) 2026` below version string
- Design decision documentation for interface-based HTTP error detection (#710)

### Fixed
- Acceptance section preserved during `gh pmu init` unless major/minor version change (#709)
  - Previously, acceptance was cleared on every init regardless of version change type
  - Now uses `RequiresReAcceptance()` to only clear on major/minor bumps

### Changed
- Updated IDPF framework to v0.54.0

## [1.0.1] - 2026-02-24

### Added
- `.gh-pmu.json` companion config file with dual-file sync (#706)
  - `config.Save()` writes both `.gh-pmu.yml` and `.gh-pmu.json` on every save
  - `gh pmu init` creates both files
  - `FindConfigFile()` falls back to `.gh-pmu.json` if YAML is missing
  - YAML remains authoritative when both files exist
  - All Config structs carry `json` tags alongside `yaml` tags

### Changed
- Acceptance gate now displays full terms text and `--yes` hint on stderr (#705)
- Accept command Long description references "Praxis Management Utility" (#704)
- Terms text updated: title includes "GitHub Praxis Management Utility" (#704)
- Added Rubrical Works copyright to terms text (#704)
- Updated IDPF framework to v0.49.1

## [1.0.0] - 2026-02-23

### Added
- Source-level version constant in `internal/version/version.go` (#700)
  - `/prepare-release` updates the constant during release workflow
  - `gh pmu --version` reads from source constant with ldflags fallback
- Version field in `.gh-pmu.yml` config (#700)
  - `gh pmu init` writes the current version to config
  - Enables upgrade detection (compare config version vs installed version)
  - Backward compatible: existing configs without version load without error
- `pending` label added to standard labels collection (#701)
  - Color: `#D93F0B` (orange)
  - Indicates at least one `/review-issue` pass was made but not yet resolved

### Changed
- Updated IDPF framework to v0.46.2

## [0.16.0] - 2026-02-20

### Added
- Terms and conditions acceptance gate (#694)
  - `gh pmu accept` command with `--yes` flag for non-interactive acceptance
  - All commands (except `init`, `accept`, `--help`, `--version`) require acceptance
  - Acceptance stored in `.gh-pmu.yml` with user, date, and version fields
  - Re-acceptance triggered on major/minor version bumps (not patches)
  - Terms text embedded in binary via `go:embed`
  - Shared acceptance: one user's acceptance covers all repository collaborators
  - Dev builds skip the acceptance gate

### Changed
- Updated IDPF framework commands and config to v0.46.1

## [0.15.2] - 2026-02-16

### Fixed
- Replace hollow tests in `mutations_test.go` with behavioral validations (#692)
  - `TestCreateIssueInput_OptionalFields` replaced with label ID pass-through test
  - `TestAddIssueToProject_Success` now validates returned item ID
  - `TestCreateIssue_Success` now validates all mutation response fields
  - `TestDeleteProjectField_EmptyFieldID` converted to input validation test
- Remove dead `GetIssuesByRelease`/`GetIssuesByPatch` functions (#692)
  - Zero callers, ignored version parameters, removed from interface and mock
- Rewrite hollow `client_test.go` tests with behavioral assertions (#685)
- Remove 17 compiler-tautology struct tests from `mutations_test.go` (#686)
- Extract and test batch sub-issue parsing logic (#686)
- Rewrite hollow `BodyStdinSetsBodyFile` test to invoke function (#687)
- Replace `extractCommentID` no-op with GraphQL `databaseId` (#687)

### Added
- Integration tests for edit labels and move batch/branch/backlog (#690)

### Changed
- Updated IDPF framework commands and config to v0.44.0

## [0.15.1] - 2026-02-12

### Added
- Auto-manage `assigned` label on branch field changes (#682)
  - `gh pmu move --branch` adds `assigned` label to issues
  - `gh pmu move --backlog` removes `assigned` label from open issues
  - `gh pmu branch close` removes `assigned` label from open incomplete issues
  - `gh pmu branch remove` removes `assigned` label from open issues
  - Label auto-created if missing (uses standard label from `defaults.yml`)
  - Bulk move applies label to all specified issues

### Changed
- Updated IDPF framework commands and config to v0.42.2

## [0.15.0] - 2026-02-12

### Added
- `tech-debt` standard label added to `gh pmu init` (#671)
- `active` standard label for branch tracker identification (#677)
- `reviewed` standard label for issues passing review workflow (#679)
- `assigned` standard label for issues assigned to branches

### Fixed
- Windows CLI length limit for GraphQL queries (#673)
  - Refactored all `-f query=` call sites to use `--input -` with stdin
  - Matches existing `executeBatchMutation()` pattern
- Removed Claude reminder injection from issue bodies (#676)
  - `prependClaudeReminder()` function and all call sites removed
  - Workflow rules enforced via `.claude/rules/` instead

### Changed
- Migrated to IDPF hub-based installation (v0.42.0 → v0.42.1)

## [0.14.3] - 2026-01-29

### Fixed
- `gh pmu intake --apply` now works without a value (#667)
  - Flag accepts optional value using Cobra's `NoOptDefVal`
  - When used without value, applies config defaults as documented
- `gh pmu view --json=status` now returns actual project status (#668)
  - Added `status`, `priority`, `branch` as direct JSON fields
  - Previously returned `null` even when issue had valid project status
  - Fields extract values from `fieldValues` map for convenience

## [0.14.2] - 2026-01-27

### Fixed
- GraphQL union type error in `deleteProjectV2Field` mutation return selection (#661)
  - Changed return struct to use `ClientMutationID` instead of `ProjectV2Field.ID`
  - Avoids "Selections can't be made directly on unions" error during `gh pmu init`

### Changed
- Updated IDPF framework to v0.33.3

## [0.14.1] - 2026-01-27

### Added
- `--body-file` / `-F` flag for `gh pmu sub create` command (#660)
  - Provides consistency with `gh pmu create` and `gh issue create`
  - Supports reading body from file or stdin with `-`

### Fixed
- GraphQL variable type missing in `deleteProjectV2Field` mutation (#661)
  - The shurcooL/graphql library requires named types to generate variable declarations
  - Added `DeleteProjectV2FieldInput` struct following existing patterns

### Changed
- Updated IDPF framework to v0.33.2

## [0.14.0] - 2026-01-26

### Added
- Auto-create project from IDPF Kanban template during `gh pmu init` (#656)
  - When no projects exist and IDPF framework selected, offers to create from template #30
  - Automatically links repository and runs intake to populate board
- New API methods: `CopyProjectFromTemplate`, `GetOwnerID`, `LinkProjectToRepository`

### Changed
- **BREAKING:** Removed all microsprint support (#654)
  - `gh pmu microsprint` command removed
  - `--microsprint` flag removed from `move` and `create` commands
  - Microsprint field automatically removed during `gh pmu init`
- Fixed root command `Use` field from `gh-pmu` to `gh pmu` for correct help text (#654)

### Fixed
- E2E tests updated for IDPF branch and body validation requirements

## [0.13.11] - 2026-01-24

### Added
- `--json` and `--jq` flags for `view` command with project field support (#647)
- `--json-fields` flag to list available JSON fields for view command (#647)
- `--template` flag for `view` command with helpful error directing to `gh issue view` (#650)
- Confirmation prompt when `--force` bypasses checkbox validation on move command (#648)
- IDPF workflow warning after `--force` bypass: "WARNING: Workflow rules may have been violated" (#648)

### Changed
- `--yes` flag on move command now also skips `--force` confirmation prompt (#648)

## [0.13.10] - 2026-01-24

### Changed
- Microsprint commands are now deprecated and will be removed in a future release (#645)
  - All `gh pmu microsprint` subcommands display deprecation message and exit
  - No operations are performed

### Fixed
- Added test coverage for Claude workflow reminder in IDPF projects (#644)

## [0.13.9] - 2026-01-24

### Added
- Claude workflow reminder automatically prepended to issue bodies in IDPF projects (#638)

## [0.13.8] - 2026-01-24

### Changed
- Error message for unchecked checkboxes now includes Claude workflow reminder (#635)

## [0.13.7] - 2026-01-23

### Added
- `--repo` flag for `board` command to view boards from different repositories (#632)
- Kanban-style project focus documented in CLI help and README (#631)
- IDPF-Praxis framework integration note in documentation (#631)

## [0.13.6] - 2026-01-22

### Added
- Four new standard labels for workflow states (#628)
  - `emergency` - P0 emergency issue requiring immediate attention
  - `approval-required` - Requires explicit approval before proceeding
  - `blocked` - Issue is blocked by external dependency
  - `scope-creep` - Issue has grown beyond original scope

## [0.13.5] - 2026-01-22

### Added
- Three new standard labels: `security-required`, `legal-required`, `docs-required` (#626)
- `--state` flag for `board` command to filter by issue state (open/closed/all) (#620)
- E2E test coverage for release→branch migration (#625)

### Changed
- Label validation now enforces standard labels only (#626)
  - Non-standard labels cause errors with helpful message listing available options
  - Standard labels auto-created on demand with correct color and description
- Optimized `board` command using Search API for better performance (#620)
- Optimized `triage` command using Search API for label filtering (#621)
- Optimized `branch current` and `branch close` with two-phase query (#623)
- Refactored `intake` command to use SearchRepositoryIssues for consistency (#622)

### Fixed
- Complete release→branch terminology migration (#614)
- Empty else branch lint warning in triage command

## [0.13.4] - 2026-01-21

### Added
- Non-interactive mode for `gh pmu init` command (#609)
  - `--non-interactive` flag disables UI and requires all flags
  - `--project` flag for project number
  - `--repo` flag for repository (owner/repo format)
  - `--owner` flag for project owner (optional, inferred from repo)
  - `--framework` flag for framework type (defaults to IDPF)
  - `--yes` flag to auto-confirm prompts
- `test-plan` label added to standard labels (#611)

### Fixed
- E2E tests using invalid `--json` flag without field arguments

## [0.13.3] - 2026-01-21

### Added
- `qa-required` label to standard labels (#607)
- `--json` field selection for `view` and `list` commands (#599)
  - `--json` alone lists available fields
  - `--json=field1,field2` returns only specified fields
  - `--jq` flag for filtering JSON output

### Removed
- Microsprint cache in favor of direct tracker issue lookup (#602)
  - Removed `--refresh` flag from `microsprint list`
  - Simplified configuration by removing cache section
- Deprecated `gh pmu release` command (#509)
  - Use `gh pmu branch` instead
  - Removed `--release` flag from `move` command

### Fixed
- IDPF link in README.md now points to correct repository

## [0.13.2] - 2026-01-20

### Changed
- Renamed project text field from "Release" to "Branch" in defaults (#603)
- Updated all command help text to reference "Branch" field instead of "Release"
- Added backward compatibility for existing projects with "Release" field
  - Commands check for "Branch" field first, fall back to "Release"
  - No migration required for existing projects

## [0.13.1] - 2026-01-19

### Added
- Auto-create default labels when assigning non-existent labels (#596)
  - Labels defined in `defaults.yml` are automatically created if missing from repository
  - Warns user when label doesn't exist and isn't in defaults
- Added `prd`, `bug`, and `enhancement` labels to default label set (#595)
- `GetLabel()` method in defaults package for label lookup

### Changed
- Epic label color changed to lighter green (#3fb950) for better visibility

## [0.13.0] - 2026-01-19

### Added
- State filtering for `GetProjectItems` and `GetProjectItemsForBoard` - filter by issue state (OPEN/CLOSED) (#560)
- Auto-create custom fields and labels during `init` command (#587)
- E2E test infrastructure with command integration tests (#582)
- Label verification tests for branch and microsprint trackers

### Fixed
- Cursor-based pagination for `GetRepositoryIssues` to handle large result sets (#557)
- Cursor-based pagination for `GetIssuesByLabel` functions (#558)
- Pagination fallback in `GetSubIssuesBatch` for epics with >100 sub-issues (#559)
- Microsprint add retry for GitHub API eventual consistency
- Use Release field name instead of Branch during init

### Changed
- Extract `buildBatchMutationRequest` for improved testability (#554)
- Update framework to v0.26.3

## [0.12.1] - 2026-01-16

### Fixed
- Batch mutation HTTP 400 error when using `gh pmu move --branch` or `--status` - GraphQL request body now uses valid JSON format (#551)

## [0.12.0] - 2026-01-15

### Added
- Batch field mutations for `move` command - reduces API calls from O(N) to O(N/50) (#543)
- Batch sub-issue queries for recursive operations - fetches sub-issues per level instead of per-issue (#542)
- `GetProjectItemsByIssues` API method for targeted issue queries (#541)
- `GetSubIssuesBatch` API method for batch sub-issue fetching (#542)
- `BatchUpdateProjectItemFields` API method for batch field updates (#543)
- Repository-scoped filtering for `branch current`, `branch close`, `filter`, and `intake` commands (#545, #546, #547, #548)
- Benchmark tests for performance-critical code paths

### Changed
- `filter` command now uses targeted queries when issue URLs are available in stdin (#545)
- `intake` command uses repository filter for single-repo configurations (#548)
- Updated IDPF framework to v0.25.0

### Fixed
- Hook error message now references `--branch` instead of deprecated `--release`
- Removed unused `skippedIssues` variable in move command

## [0.11.1] - 2026-01-12

### Added
- `--state` filter flag for `list` command to filter by issue state (open/closed) (#522)
- `--remove-label` flag for `edit` command to remove labels from issues (#519)
- `--dry-run` flag for `branch close` and `microsprint close` commands (#527)
- DATE field type support in `SetProjectItemField` for date-based project fields (#518)
- Progress indicator for recursive move operations showing per-issue status (#520)
- Pagination support for `GetSubIssues` query to handle 50+ sub-issues (#521)
- Rate limiting protection with exponential backoff for bulk operations (#525)
- Status field value validation before setting, with helpful error messages (#523)

### Fixed
- `setNumberField` now parses value parameter instead of always setting 0 (#513)
- `--label` flag in `edit` command now actually adds labels (#528)
- `--interactive` flag removed from `create` command (was unimplemented stub) (#514)
- `branch close` now reports correct count of moved issues (#515)
- `GetSubIssueCounts` failures now warn and include items instead of silent exclusion (#516)
- Consolidated duplicate `openInBrowser` implementations into `internal/ui` (#517)
- Help text updated from "release" to "branch" terminology throughout (#512)
- Error messages updated to reference "branch" instead of "release" (#511)

### Changed
- `IsIDPF()` now uses case-insensitive prefix matching for framework detection (#524)
- Coverage threshold lowered from 80% to 70%

## [0.11.0] - 2026-01-08

### Changed
- Rename `gh pmu release` command to `gh pmu branch` (#508)
  - All subcommands remain the same: `start`, `current`, `add`, `remove`, `close`, `list`
  - Tracker issues now use `branch` label instead of `release`
- Change tracker label from `release` to `branch` (#505)
  - Migration support: `gh pmu init` automatically migrates legacy labels
- Remove unused `release.active` config section (#504)
  - Active releases now tracked via `branch` label on tracker issues
  - Simplifies config file and reduces maintenance overhead

### Documentation
- Update `docs/commands.md` to reflect `branch` command rename

## [0.10.1] - 2026-01-05

### Added
- `gh pmu comment` subcommand for adding comments to issues (#491)
  - `--body` / `-b` flag for inline comment text
  - `--body-file` / `-F` flag to read comment from file
  - `--body-stdin` flag to read comment from stdin
  - `--repo` / `-R` flag for cross-repository comments
- `--body-stdin` flag for `gh pmu create` command (#491)
  - Reads body content from stdin
  - Mutually exclusive with `--body` and `--body-file`
- `--repo` / `-R` flag for `gh pmu edit` command (#492)
  - Enables editing issues in repositories other than the configured default

### Changed
- `gh pmu move` help text now clearly indicates multiple issue support (#494)
  - Usage changed from `<issue-number>...` to `<issue-number> [issue-number...]`
  - Short description updated to "Update project fields for multiple issues at once"

### Fixed
- Document root cause of GitHub Project automation moving issues back to "In Progress" (#498)
  - "Pull request linked to issue" workflow triggers on `Fixes #XXX` cross-references
  - Recommended solution: Disable the automation or use `Refs #XXX` in PR bodies

### Documentation
- Migrate USER-EXTENSION content into prepare-release.md (#499)
  - Added Documentation Review checklist to post-analysis
  - Added Handle Incomplete Issues and Lint Gate to pre-validation
  - Added Coverage Configuration example to post-validation
  - Added Important Rules (6 guardrails) to pre-tag
  - Added Cleanup Assets and Post-Release Reminder to post-tag
- Add `edit` and `comment` commands to docs/commands.md
- Add `--body-stdin` flag documentation for `create` command

## [0.10.0] - 2026-01-03

### Added
- `--body-stdout` flag for `gh pmu view` command (#487)
  - Outputs raw issue body (markdown) directly to stdout
  - Enables AI assistants to capture body without temp file
- `--body-stdin` flag for `gh pmu edit` command (#488)
  - Reads body content from stdin (raw markdown)
  - Mutually exclusive with `--body` and `--body-file`
  - Supports piped input: `echo "$BODY" | gh pmu edit 123 --body-stdin`

### Changed
- Streamlined body editing workflow reduces tool calls from 5+ to 2-3 (#486)
- Update internal framework to v0.20.1 with charter support

## [0.9.7] - 2025-12-30

### Added
- `gh pmu edit` command with body-file support (#480)
  - `--body-file` / `-F` flag to read body from file
  - `--title` / `-t` flag to update issue title
  - `--body` / `-b` flag for inline body text
  - `--label` / `-l` flag to add labels (repeatable)
  - Completes body-file round-trip workflow: view → edit file → update
- Lint gate in `/prepare-release` workflow (#478)
  - Runs `golangci-lint` before tagging to prevent failed releases
  - New script: `.claude/scripts/prepare-release/lint.js`

### Changed
- `gh pmu release close` now defaults to current release (#479)
  - No argument required when exactly one release is active
  - Shows error with list when multiple releases exist
- Update framework to v0.17.1

## [0.9.6] - 2025-12-29

### Added
- `--body-file` / `-b` flag to `view` command (#474)
  - Writes issue body to `tmp/issue-{number}.md` for easy editing
  - Creates `tmp/` directory automatically if it doesn't exist
  - Outputs file path to stdout for scripting workflows

### Changed
- Migrate internal framework to v0.17.0

## [0.9.5] - 2025-12-28

### Changed
- Reorganize `.claude/scripts/` into purpose-based subdirectories (#469)
  - `close-release/` - Release monitoring scripts
  - `framework/` - Commit analysis, version recommendation, CI waiting
  - `open-release/` - Config verification
  - `prepare-release/` - Coverage analysis
  - `shared/` - Common utilities (lib, sprint scripts, etc.)
- Bump internal framework version to 0.16.0

### Added
- `/prepare-beta` command for beta release workflow
- `.claude/extensions/` directory for custom extension points

## [0.9.4] - 2025-12-26

### Fixed
- `gh pmu release close` no longer generates artifacts (#466)
  - Artifact generation moved to `/prepare-release` Phase 2
  - Prevents overwriting detailed release notes with auto-generated versions
  - Removed REQ-020 tests (AC-020-1 through AC-020-4)

## [0.9.3] - 2025-12-26

### Added
- Sprint and release management commands for Claude Code integration
  - `/assign-release`, `/close-release`, `/open-release`, `/switch-release`
  - `/plan-sprint`, `/end-sprint`, `/sprint-status`, `/sprint-retro`, `/transfer-issue`
- Beta deployment workflow proposal (#458)
  - Side-by-side `gh pmu` + `gh pmub` installation design
  - Prerelease versioning scheme (`v{current}+beta.N`)

### Changed
- `/prepare-release` command restored with full automation (#459)
  - Merged old automation scripts into new 4-phase structure
  - Added `--skip-coverage` flag support
  - Restored: config verification, commit analysis, CI waiting, release monitoring
  - Preserved: lifecycle diagram, `/close-release` integration

## [0.9.2] - 2025-12-23

### Added
- `/prepare-release` workflow now updates GitHub release notes from CHANGELOG (#439)
  - Parses CHANGELOG.md and updates corresponding GitHub release body
  - Cleans up old release assets, keeping only 3 most recent tagged releases
  - New scripts: `update-release-notes.js`, `cleanup-release-assets.js`

### Performance
- `gh pmu move` now caches project fields before bulk updates (#451)
  - Eliminates N+1 API calls when updating multiple fields per issue
  - 68-80% reduction in API calls for typical multi-field updates

### Fixed
- `release current` now shows accurate issue count (#449)
  - Uses project items with field filtering instead of label-based query
  - Fixes discrepancy between `release current` and `list --release current`


## [0.9.1] - 2025-12-23

### Fixed
- `gh pmu init` now detects all status and priority field values from project metadata (#442)
  - Previously hardcoded values missed custom options like "Parking Lot"
  - Now dynamically generates field mappings from actual project fields
  - Handles emoji prefixes (e.g., "🅿️ Parking Lot" → `parking_lot` alias)

## [0.9.0] - 2025-12-23

### Added
- Local caching for `release list` and `microsprint list` commands (#434)
  - Cached data stored in `.gh-pmu.yml` under `cache:` section
  - Cache automatically updated on `start`, `close`, `reopen` commands
  - Use `--refresh` flag to force API fetch and update cache
  - ~6x performance improvement (~118ms cached vs ~780ms uncached)
- Coverage gate for `/prepare-release` workflow
  - Analyzes patch coverage against changed lines since last tag
  - Configurable threshold in `.gh-pmu.yml` under `release.coverage.threshold`

### Fixed
- Test isolation to prevent `.gh-pmu.yml` corruption (#436)
  - All tests that call `cfg.Save()` now use isolated temp directories
  - Added canary test to detect future isolation failures

## [0.8.6] - 2025-12-22

### Fixed
- GraphQL mutation "Missing type definition for variable" error for close/reopen/update operations (#425)
  - shurcooL graphql library requires named struct types for input variables
  - Added `CloseIssueInput`, `ReopenIssueInput`, `UpdateIssueInput` named types
- Temp file utilities now use project root directory instead of OS temp (#427)
  - `config.CreateTempFile()` creates files in `tmp/` alongside `.gh-pmu.yml`
  - Directory automatically added to `.gitignore`

### Changed
- Release close now skips issues in "Parking Lot" status (#426)
  - Parking Lot issues are listed separately in output but not moved to backlog
  - Prevents accidentally moving parked work when closing releases

## [0.8.5] - 2025-12-22

### Fixed
- GraphQL type mismatch in `GetRepositoryIssues` - now uses `IssueState` enum instead of `String` (#422)
- Checkbox validation now ignores checkboxes inside code blocks (#419)
  - Fenced code blocks (``` or ~~~) and indented code blocks are excluded
  - Example checkboxes in documentation no longer trigger validation failures
- Data race in mock handler prevented with mutex (#421)

### Changed
- Test coverage improvements with wrapper tests and TESTING.md documentation (#414, #417)

## [0.8.4] - 2025-12-22

### Added
- Microsprint tracker issues now include descriptive body explaining purpose and commands (#407)
  - Similar to release tracker bodies added in v0.8.2

### Fixed
- License attribution moved to NOTICE file per Apache 2.0 requirements (#402, #403)
- Codecov integration improvements for accurate diff coverage (#393, #394)

### Changed
- Improved test coverage for cmd package with mock client implementations (#395-#401, #405, #406)

## [0.8.3] - 2025-12-21

### Performance
- `list --has-sub-issues` optimized from N+1 API calls to single batch query per repository (#389)
- `close --update-status` optimized with direct issue-to-project-item lookup (3 calls → 2 calls) (#390)

### Added
- README badges: CI, Codecov, Go Report Card, Release, License (#388)

### Changed
- Coverage reporting now via Codecov badge instead of committed reports (#388)
- Removed `coverage` job from CI workflow (#388)
- Deleted `coverage/` directory from repository (#388)

## [0.8.2] - 2025-12-21

### Added
- Release tracker issues now include descriptive body with linked issues (#386)

### Performance
- `gh pmu view` command optimized from ~4.5s to ~0.8s (6x faster) (#386)

## [0.8.1] - 2025-12-21

### Fixed
- `release list` now finds releases with any branch name format (#376)
  - Previously only matched titles starting with "Release: v"
  - Now matches all "Release: " prefixed titles (e.g., "Release: release/v1.0.0")
- Release tests no longer corrupt repository `.gh-pmu.yml` file (#378)
  - Tests now use isolated temp directories for config operations
- Documentation incorrectly claimed `duplicate` close reason exists (#379)
  - Removed non-existent `duplicate`/`dupe` aliases from docs
  - Added note explaining GitHub API only supports `completed` and `not_planned`

### Changed
- CI pipeline now verifies config file integrity after tests
  - Detects test corruption patterns (testowner, testrepo values)
  - Fails build if tests modify `.gh-pmu.yml`

## [0.8.0] - 2025-12-21

### Added
- `gh pmu validation` command group for status transition validation
  - `validation rules` - Display configured transition rules
  - `validation check <from> <to>` - Test if a transition is allowed
  - `validation enable/disable` - Toggle validation enforcement
- Status transition validation integrated into `move`, `create`, `release`, and `microsprint` commands
  - Configurable via `validation` section in `.gh-pmu.yml`
  - Default rules enforce logical workflow progressions
  - `--force` flag available to bypass validation when needed
- `GetAllowedTransitions` API query for retrieving valid status transitions
- Extended configuration schema with `validation` section for custom transition rules
- CodeQL security scanning workflow for automated vulnerability detection

### Fixed
- Config file no longer overwritten by field tests
- Add bounds checking for int to int32 conversion in GraphQL queries
- Remove unused code and fix empty branch lint errors

## [0.7.5] - 2025-12-19

### Added
- `--release` flag to `move` command - set Release field directly (#349)
- `--microsprint` flag to `move` command with `--sprint` alias (#349)
- `--backlog` flag to `move` command - clear Release and Microsprint fields (#349)

### Changed
- **BREAKING**: `release start` now requires `--branch` flag instead of `--version` (#346, #348)
  - Migration: `--version 2.0.0` → `--branch release/v2.0.0`
  - Branch name used literally for tracker title, Release field, and artifact directory
  - Supports any branch convention: `release/v2.0.0`, `patch/v1.9.1`, `hotfix-auth-bypass`

### Removed
- **BREAKING**: Removed entire `patch` command group (~1750 lines) (#346, #348)
  - Migration: `gh pmu patch start --version 1.9.1` → `gh pmu release start --branch patch/v1.9.1`
  - Migration: `gh pmu patch add 42` → `gh pmu release add 42`
  - Migration: `gh pmu patch close --tag` → `gh pmu release close --tag`

### Fixed
- Sub-issues no longer inherit parent labels by default (#359)

## [0.7.4] - 2025-12-19

### Changed
- Remove `--name` and `--track` flags from `release start` command (#345)
  - Simplifies release command interface
  - Interactive mode still available for version selection

### Fixed
- Improve git command error messages (#347)
  - `GitAdd`, `GitTag`, `GitCommit` now capture and return stderr on failure
  - Users see actual git error messages instead of generic "exit status 1"

## [0.7.3] - 2025-12-15

### Added
- Multiple issue support for `move` command (#343)
  - Accept multiple issue numbers: `gh pmu move 42 43 44 --status done`
  - Sequential processing to avoid API rate limits
  - Graceful error handling - individual failures don't stop remaining issues
  - Summary output for bulk operations

### Fixed
- Add retry logic for transient HTTP errors in release scripts


## [0.7.2] - 2025-12-15

### Fixed
- Correct GraphQL field name for `CreateProjectV2Field` mutation

## [0.7.1] - 2025-12-15

### Added
- Additional `gh issue create` options in `gh pmu create` command (#325)
  - `--body-file` / `-F` - Read body text from file (use "-" for stdin)
  - `--editor` / `-e` - Open editor to compose body
  - `--template` / `-T` - Use issue template from `.github/ISSUE_TEMPLATE/`
  - `--web` / `-w` - Open browser after creating issue
- Mutual exclusivity validation for body input options

## [0.7.0] - 2025-12-15

### Added
- **Release track configuration** - Configure track prefix for release artifacts (#321)
  - `release.tracks[].prefix` in `.gh-pmu.yml` for artifact directory naming
  - Supports both versioned (`v1.2.0/`) and prefixed (`main-v1.2.0/`) directory structures
- **Configurable artifact generation** - Control what artifacts are generated during release close (#323)
  - `release.artifacts.directory` - Custom base directory (default: "Releases")
  - `release.artifacts.release_notes` - Toggle release-notes.md generation
  - `release.artifacts.changelog` - Toggle changelog.md generation
- **Patch version validation** - Validate patch versions and enforce label constraints (#322)
  - Error if issue has `breaking-change` label (incompatible with patch releases)
  - Warning if issue lacks `bug`/`fix`/`hotfix` label
  - Validates version is a valid patch increment from latest git tag
- **Init command enhancements** - Improved project initialization workflow
  - Auto-create `Release` and `Microsprint` project fields if missing
  - Auto-create `release` and `microsprint` repository labels if missing
  - Sync active releases from existing tracker issues to `release.active[]`
- **Interactive version selection** - Prompt for version during `release start`
  - Shows latest git tag for reference
  - Validates semver format before proceeding

### Fixed
- Remove unused `name` field from `patchStartOptions` struct
- Fix ineffectual assignment to `fields` in init command

## [0.6.0] - 2025-12-14

### Added
- `gh pmu microsprint` command group for AI-assisted development workflows
  - `microsprint start` - Start a new microsprint with auto-generated naming (YYYY-MM-DD-a pattern)
  - `microsprint add <issue>` - Add issue to current microsprint via Text field
  - `microsprint remove <issue>` - Remove issue from microsprint
  - `microsprint current` - Show current active microsprint details
  - `microsprint close` - Close microsprint with artifact generation (review.md, retro.md)
  - `microsprint list` - List microsprint history (open and closed)
  - `microsprint resolve` - Resolve multiple active microsprint conflicts
- Interactive retrospective prompts during `microsprint close`
  - Prompts for: What Went Well, What Could Be Improved, Action Items
  - `--skip-retro` flag to generate empty template without prompts
  - `--commit` flag to auto-commit generated artifacts
- Team-wide microsprint model with multi-user support
  - Join/Work without/Cancel prompts when another user has active microsprint
  - Confirmation prompt before closing another user's microsprint
- `gh pmu release` command group for version-based deployment workflows (IDPF-Structured)
  - `release start --version` - Start a new release with semver validation
  - `release add/remove <issue>` - Manage issues in release
  - `release current` - Show current release details
  - `release close` - Close release with artifact generation
  - `release list` - List release history
- `gh pmu patch` command group for hotfix deployment workflows (IDPF-LTS)
  - Same subcommand structure as release command
- Integration with existing `move` and `create` commands via `--microsprint` flag
- Artifact generation in `Microsprints/{name}/` directory

### Fixed
- Dynamic date handling in microsprint tests for reliable CI across timezones

### Documentation
- Added upgrade instructions to README

## [0.5.3] - 2025-12-12

### Added
- `--repo` / `-R` flag to `view` command for explicit repository specification
- `--repo` / `-R` flag to `close`, `split`, and `sub remove` commands
  - Completes `--repo` flag support across all commands
  - Enables consistent cross-repository workflows

### Documentation
- Restructured README with concise format and linked How-To guides
- New documentation files: configuration.md, commands.md, sub-issues.md, batch-operations.md, development.md
- Added CONTRIBUTING.md for contributor guidelines
- Added "Unique Capabilities" section highlighting gh-pmu specific flags

## [0.5.2] - 2025-12-12

### Added
- `--repo` / `-R` flag to `list` command for filtering by repository (#268)
  - Provides consistency with `move --repo` and `sub list --repo`
  - Flag takes precedence over config file setting
- `filter` subcommand for piping and filtering issue data (#267)
  - Reads JSON from stdin (piped from `gh issue list --json ...`)
  - Filters by project field values (`--status`, `--priority`)
  - Additional filters: `--assignee`, `--label`
  - Output as table (default) or JSON (`--json`)
  - Example: `gh issue list -R owner/repo --json number,title | gh pmu filter --status ready`

## [0.5.1] - 2025-12-11

### Added
- `--repo` / `-R` flag to `sub list` command for explicit repository specification (#266)
  - Follows same pattern as `move --repo` and `sub add --repo`
  - Enables querying sub-issues from any repository without changing config

## [0.5.0] - 2025-12-11

### Added
- `gh pmu field` command - Manage custom project fields
  - `field create <name>` - Create project fields (text, number, date, single_select)
  - `field list` - List all fields in the project
  - Supports single-select fields with custom options via `--option` flag
  - Auto-updates `.gh-pmu.yml` metadata after field creation
- `--repo` / `-R` flag to `move` command for explicit repository specification (#265)
  - Matches behavior of `sub add` and other commands
  - Enables cross-repo workflows and CI/CD scripting

## [0.4.5] - 2025-12-10

### Fixed
- Help text no longer claims unavailable "project templating" feature (#261)
  - Updated description to: "gh-pmu streamlines GitHub project workflows with unified issue tracking and sub-issue hierarchy."

## [0.4.4] - 2025-12-10

### Added
- `--browser` flag to open history in web browser (#259)
  - Generates styled HTML with GitHub dark theme
  - Clickable commit hashes and issue references
  - Works on Windows, macOS, and Linux
- `--files` flag to show affected files in directory history (#260)
  - Compact view now shows file count: `(3 files)` suffix
  - Full file list displayed with `--files` flag

## [0.4.3] - 2025-12-10

### Added
- Commit body and GitHub comments in detailed single-file history view (#258)
  - Shows full commit message body (multi-line description after subject)
  - Fetches and displays GitHub commit comments via API
  - Common trailers (Co-Authored-By, Signed-off-by) filtered from display

## [0.4.2] - 2025-12-10

### Added
- Config file search now walks up directory tree (#257)
  - Run `gh pmu` commands from any subdirectory of a repo with `.gh-pmu.yml`
  - Similar behavior to how git finds `.git` directories

## [0.4.1] - 2025-12-10

### Added
- Detailed single-file history view with expanded commit information (#256)
  - Shows full commit messages, relative timestamps ("3 days ago"), and line change stats (+/-)
  - Automatically activates for single file paths
  - `--compact` flag to use original compact format for single files

## [0.4.0] - 2025-12-10

### Added
- `gh pmu history` command - Show git commit history for files/directories with issue references (#255)
  - Parse issue/PR references from commit messages (#123, fixes #456)
  - Infer change type from commit prefixes (Fix:, Add:, feat:, etc.)
  - Lipgloss styled terminal output with color-coded change types
  - JSON output format (`--json`)
  - Markdown file output (`--output`) to History/ directory
  - Safety protections: 25 file limit, repo root guard (`--force` to override)
  - Filter options: `--since`, `--limit`

### Dependencies
- Added `github.com/charmbracelet/lipgloss` v1.1.0 for styled terminal output

## [0.3.1] - 2025-12-09

### Changed
- Repository moved from `scooter-indie/gh-pmu` to `rubrical-works/gh-pmu` (#254)
- Installation command updated: `gh extension install rubrical-works/gh-pmu`
- Go module path updated to `github.com/rubrical-works/gh-pmu`

### Note for Existing Users
Existing installations will continue to work via GitHub redirect. For a clean setup, reinstall:
```bash
gh extension remove gh-pmu
gh extension install rubrical-works/gh-pmu
```

## [0.3.0] - 2025-12-08

### Added
- `gh pmu board` command - Interactive terminal-based project board view (#250)
- `gh pmu close` command with reason aliases (completed, not_planned, duplicate, reopened) (#249)
- Integration testing infrastructure with `internal/testutil` package
- Comprehensive integration tests for all commands (list, view, create, move, intake, triage, split, sub add/create/list/remove, init)
- GitHub Actions workflow for integration tests

### Fixed
- `os.Chmod` error handling for errcheck linter compliance
- CI workflow improvements for gh extension installation and authentication

### Changed
- Disabled integration tests workflow (pending test environment fixes)

### Documentation
- Added shell construct limitations guide to `gh-workflow.md` (#252)
- Added integration testing guide for test fixtures

## [0.2.13] - 2025-12-04

### Added
- 31 command enhancements (#94-124)

### Fixed
- Use `t.Fatal` for nil checks to satisfy staticcheck

## [0.2.12] - 2025-12-04

### Fixed
- Staticcheck lint error in triage tests (SA5011) - use `t.Fatal` for nil checks

### Notes
- v0.2.11 release failed to build binaries due to lint error; this release includes all v0.2.11 changes plus the fix

## [0.2.11] - 2025-12-04

### Added
- `--repo` / `-R` flag to `triage` command for targeting specific repositories (#91)

### Fixed
- `GetProjectItems` now uses cursor-based pagination to fetch all items (#90)
  - Previously limited to first 100 items, causing "issue not in project" errors for large projects

### Documentation
- Clarified distinction between labels and project fields in `gh-workflow.md`
- Added pagination integration test scenario to backlog (IT-2.4)

## [0.2.10] - 2025-12-04

### Added
- Comprehensive tests for `cmd/intake.go` output functions (100% coverage)
- Comprehensive tests for `cmd/split.go` output functions (100% coverage)
- Comprehensive tests for `cmd/init.go` helper functions (75-100% coverage)
- Comprehensive tests for `internal/ui/ui.go` spinner methods (96.9% coverage)
- Comprehensive tests for `cmd/view.go` output functions
- Comprehensive tests for `cmd/create.go` runCreate function
- Comprehensive tests for `cmd/move.go` core logic
- Comprehensive tests for `cmd/sub.go` output functions
- Comprehensive tests for `internal/api/mutations.go`
- Comprehensive tests for `cmd/triage.go` command
- Integration testing proposal (`Proposal/PROPOSAL-Automated-Testing.md`)
- Integration testing backlog with 23 stories, 90 story points

### Changed
- Test coverage increased from ~15% to 63.6%
- Fixed golangci-lint errcheck warnings for `os.Chdir` in deferred calls
- Renamed test fixtures from `.gh-pm.yml` to `.gh-pmu.yml`

### Coverage (v0.2.10)
| Package | Coverage |
|---------|----------|
| `internal/api` | 96.6% |
| `internal/config` | 97.0% |
| `internal/ui` | 96.9% |
| `cmd` | 51.2% |
| **Total** | **63.6%** |

## [0.2.9] - 2025-12-03

### Added
- Comprehensive test coverage for triage command

### Changed
- Format all Go files with `gofmt -s`

## [0.2.8] - 2025-12-03

### Added
- Generate Markdown coverage report instead of HTML (`coverage/README.md`)

### Changed
- Remove HTML coverage report in favor of Markdown

## [0.2.7] - 2025-12-03

### Added
- Coverage report generation on releases

### Fixed
- Use `-short` flag in coverage tests to skip auth-dependent tests
- Calculate box width using visible text length (strip ANSI codes)
- Use rune count for visible width calculation in box formatting

## [0.2.6] - 2025-12-03

### Fixed
- Use binary format in goreleaser for gh extension compatibility
- Add Windows support to goreleaser config

## [0.2.5] - 2025-12-03

### Changed
- Consolidate CI workflows with sequential execution

### Fixed
- Format ui.go and remove unused noColor field in Spinner

## [0.2.4] - 2025-12-03

### Added
- Enhanced init UX with project discovery and styled output (Story 1.13)
  - Auto-detect repository from git remote
  - Query GitHub API for associated projects
  - Present numbered list for project selection
  - Styled output with spinners, boxes, and color coding

### Fixed
- Correct gh-sub-issue attribution to yahsan2
- Format cmd/init.go

## [0.2.3] - 2025-12-03

### Changed
- Switch to goreleaser for releases

## [0.2.2] - 2025-12-03

### Fixed
- Simplify release workflow - use default build

## [0.2.1] - 2025-12-03

### Fixed
- init command now creates complete config file (#71)
- Use t.Fatal for nil flag checks (SA5011)
- Address golangci-lint errors
- Format code and downgrade deps for Go 1.22/1.23 compatibility
- Set go.mod to 1.22 for CI compatibility

## [0.2.0] - 2025-12-03

### Added
- Mirror gh-pm CI/CD workflows (#69)
- Release workflow for gh extension install

### Changed
- Rename to gh-pmu for shorter command

### Fixed
- Skip auth-dependent tests in CI
- Use binary format for gh extension install compatibility

## [0.1.0] - 2025-12-03

### Added - Initial Release
- **Project scaffolding**
  - Go module with Cobra CLI framework
  - Makefile with build, test, lint targets
  - GitHub Actions for CI/CD

- **Configuration package** (`internal/config`)
  - Load `.gh-pmu.yml` with Viper
  - Validate required fields
  - Support field aliases
  - Cache project metadata from GitHub API

- **GitHub API client** (`internal/api`)
  - GraphQL client using go-gh library
  - Support for `sub_issues` and `issue_types` feature headers
  - Queries: GetProject, GetProjectFields, GetProjectItems, GetIssue, GetSubIssues, GetParentIssue, GetRepositoryIssues
  - Mutations: CreateIssue, AddIssueToProject, SetProjectItemField, AddSubIssue, RemoveSubIssue

- **Project Management Commands**
  - `gh pmu init` - Interactive project configuration setup
  - `gh pmu list` - List issues with project field values
  - `gh pmu view` - View issue with all project fields and sub-issues
  - `gh pmu create` - Create issue with project fields pre-populated
  - `gh pmu move` - Update issue project fields

- **Sub-Issue Commands**
  - `gh pmu sub add` - Link existing issue as sub-issue
  - `gh pmu sub create` - Create new sub-issue under parent
  - `gh pmu sub list` - List sub-issues with completion count
  - `gh pmu sub remove` - Unlink sub-issue from parent

- **Batch Operations**
  - `gh pmu intake` - Find and add untracked issues to project
  - `gh pmu triage` - Bulk update issues based on config rules
  - `gh pmu split` - Create sub-issues from checklist or arguments

- **Enhanced Integration**
  - Cross-repository sub-issues
  - Sub-issue progress tracking (progress bar in view)
  - Recursive operations (`--recursive` flag for move)

### Attribution
- Based on [gh-pm](https://github.com/yahsan2/gh-pm) and [gh-sub-issue](https://github.com/yahsan2/gh-sub-issue) by [@yahsan2](https://github.com/yahsan2)

---

## Development History

### Pre-release (2025-11-30 to 2025-12-02)

- 2025-11-30: Initial repository setup, README.md
- 2025-12-01: Add proposal document
- 2025-12-02: Add Agile PRD, product backlog, code integration inventory
- 2025-12-02: Complete Sprint 1 (Foundation), Sprint 2 (Core Commands), Sprint 3 (Batch Operations)
- 2025-12-03: Complete Sprint 5 (Enhanced Integration)
- 2025-12-03: Remove Epic 2 & 4 (GitHub API limitations - no view creation API)

---

## Sprint Summary

| Sprint | Focus | Status |
|--------|-------|--------|
| Sprint 1 | Foundation, init, list | Complete |
| Sprint 2 | view, create, move, sub-issues | Complete |
| Sprint 3 | intake, triage, split | Complete |
| Sprint 4 | Epic 2 (Templates) | Removed - API limitations |
| Sprint 5 | Enhanced Integration | Complete |
| Sprint 6 | Test Coverage | Complete (63.6%) |

---

## API Limitations Discovered

### GitHub Projects V2 API
- **No `createProjectV2View` mutation** - Views cannot be created programmatically
- **Status field reserved** - New projects have a default Status field that cannot be replaced
- **Workflows not accessible** - Project automation workflows cannot be read or written via API

These limitations led to removing Epic 2 (Project Templates) from scope.

[Unreleased]: https://github.com/rubrical-works/gh-pmu/compare/v0.15.1...HEAD
[0.15.1]: https://github.com/rubrical-works/gh-pmu/compare/v0.15.0...v0.15.1
[0.8.3]: https://github.com/rubrical-works/gh-pmu/compare/v0.8.2...v0.8.3
[0.8.2]: https://github.com/rubrical-works/gh-pmu/compare/v0.8.1...v0.8.2
[0.8.1]: https://github.com/rubrical-works/gh-pmu/compare/v0.8.0...v0.8.1
[0.8.0]: https://github.com/rubrical-works/gh-pmu/compare/v0.7.5...v0.8.0
[0.7.5]: https://github.com/rubrical-works/gh-pmu/compare/v0.7.4...v0.7.5
[0.7.4]: https://github.com/rubrical-works/gh-pmu/compare/v0.7.3...v0.7.4
[0.7.3]: https://github.com/rubrical-works/gh-pmu/compare/v0.7.2...v0.7.3
[0.7.2]: https://github.com/rubrical-works/gh-pmu/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/rubrical-works/gh-pmu/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/rubrical-works/gh-pmu/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/rubrical-works/gh-pmu/compare/v0.5.3...v0.6.0
[0.5.3]: https://github.com/rubrical-works/gh-pmu/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/rubrical-works/gh-pmu/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/rubrical-works/gh-pmu/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/rubrical-works/gh-pmu/compare/v0.4.5...v0.5.0
[0.4.5]: https://github.com/rubrical-works/gh-pmu/compare/v0.4.4...v0.4.5
[0.4.4]: https://github.com/rubrical-works/gh-pmu/compare/v0.4.3...v0.4.4
[0.4.3]: https://github.com/rubrical-works/gh-pmu/compare/v0.4.2...v0.4.3
[0.4.2]: https://github.com/rubrical-works/gh-pmu/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/rubrical-works/gh-pmu/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/rubrical-works/gh-pmu/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/rubrical-works/gh-pmu/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/rubrical-works/gh-pmu/compare/v0.2.13...v0.3.0
[0.2.13]: https://github.com/rubrical-works/gh-pmu/compare/v0.2.12...v0.2.13
[0.2.12]: https://github.com/rubrical-works/gh-pmu/compare/v0.2.11...v0.2.12
[0.2.11]: https://github.com/rubrical-works/gh-pmu/compare/v0.2.10...v0.2.11
[0.2.10]: https://github.com/rubrical-works/gh-pmu/compare/v0.2.9...v0.2.10
[0.2.9]: https://github.com/rubrical-works/gh-pmu/compare/v0.2.8...v0.2.9
[0.2.8]: https://github.com/rubrical-works/gh-pmu/compare/v0.2.7...v0.2.8
[0.2.7]: https://github.com/rubrical-works/gh-pmu/compare/v0.2.6...v0.2.7
[0.2.6]: https://github.com/rubrical-works/gh-pmu/compare/v0.2.5...v0.2.6
[0.2.5]: https://github.com/rubrical-works/gh-pmu/compare/v0.2.4...v0.2.5
[0.2.4]: https://github.com/rubrical-works/gh-pmu/compare/v0.2.3...v0.2.4
[0.2.3]: https://github.com/rubrical-works/gh-pmu/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/rubrical-works/gh-pmu/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/rubrical-works/gh-pmu/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/rubrical-works/gh-pmu/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/rubrical-works/gh-pmu/releases/tag/v0.1.0
