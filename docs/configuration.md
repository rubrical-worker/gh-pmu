# Configuration Guide

gh-pmu uses a `.gh-pmu.json` file in your repository root to configure project connections, field aliases, and triage rules.

> **Note:** gh-pmu is designed for Kanban-style GitHub Projects with status-based columns (e.g., Backlog, In Progress, In Review, Done). If your project uses a different board style, some features may not apply.

## Quick Setup

Run the interactive setup:

```bash
gh pmu init
```

This creates `.gh-pmu.json` with your project settings and fetches field metadata.

## Configuration Reference

### Version

The top-level `version` field records the gh-pmu version that generated the config. Written automatically by `gh pmu init`.

```yaml
version: "1.0.0"
```

Used for upgrade detection — when the installed version differs from the config version, `init` should be re-run to apply any new defaults.

### Project Settings

```yaml
project:
  name: my-project       # Display name
  owner: your-username   # GitHub user or org
  number: 1              # Project number (from URL)
```

### Repositories

List repositories that use this project board:

```yaml
repositories:
  - your-username/repo-one
  - your-username/repo-two
```

### Defaults

Default values applied when creating issues:

```yaml
defaults:
  priority: p2           # Default priority alias
  status: backlog        # Default status alias
  labels:
    - enhancement        # Labels added to all new issues (optional)
```

### Field Aliases

Map short aliases to actual project field values. Use aliases in commands instead of full field names:

```yaml
fields:
  priority:
    field: Priority      # Actual field name in project
    values:
      p0: P0             # alias: Actual Value
      p1: P1
      p2: P2
  status:
    field: Status
    values:
      backlog: Backlog
      up_next: Up next
      ready: Ready
      in_progress: In progress
      in_review: In review
      qa_required: QA required
      done: Done
      parking_lot: Parking Lot
      notes: Notes
```

**Usage:**
```bash
# Use alias (lowercase, underscore)
gh pmu move 42 --status in_progress --priority p1

# Instead of actual values
gh pmu move 42 --status "In progress" --priority "P1"
```

### Triage Rules

Define rules for batch processing issues:

```yaml
triage:
  # Rule name
  untracked:
    # GitHub search query
    query: "is:issue is:open -label:triaged"

    # Fields/labels to apply automatically
    apply:
      labels:
        - triaged
      fields:
        status: backlog
        priority: p2

    # Fields to prompt for interactively
    interactive:
      status: true
      priority: false

  # Another rule example
  stale:
    query: "is:issue is:open updated:<2024-01-01"
    apply:
      labels:
        - needs-triage
```

**Usage:**
```bash
# Preview what would be affected
gh pmu triage untracked --dry-run

# Apply the rule
gh pmu triage untracked --apply
```

### Branch Configuration

Branch workflow artifacts are not configured here. `gh pmu init` auto-creates
the Branch field and its labels if they are missing, and release artifact and
coverage-gate behavior belongs to the `/prepare-release` workflow rather than
to `.gh-pmu.json`.

### Validation (IDPF Framework)

When `framework` is set to an IDPF variant (e.g., `IDPF`, `IDPF-Agile`), automatic validation is enabled:

**Rules enforced:**
- **Body required**: Issues must have content before moving to `in_review` or `done`
- **Checkboxes**: All checkboxes must be checked before `in_review` or `done`
- **Branch assignment**: Issues must have a branch before moving from `backlog` to `ready` or `in_progress`

**Bypassing validation:**
- Use `gh pmu move --force` to bypass checkbox validation
- Body and branch requirements cannot be bypassed

**Disable validation:**
- Remove the `framework` field or set to a non-IDPF value

### How gh-pmu Writes This File

Two behaviors change `.gh-pmu.json` without you editing it.

**The `version` key is a stamp, not a setting.** It records the gh-pmu version
that last wrote the file. Every non-exempt command compares it against the
running binary and rewrites it when the two differ — so it tells you what wrote
your config, not what created it. A matching version is *not* rewritten, so an
up-to-date config keeps its mtime and its line endings. `version` is excluded
from the drift comparison for this reason; a tool-written stamp is not
unauthorized modification. Do not use this field to detect whether `gh pmu init`
needs re-running — it always equals the running binary, so it can never report
staleness.

**Top-level keys gh-pmu does not recognize are preserved.** A config written by
a newer gh-pmu, or carrying a key from an older one, survives a round-trip
through a binary that knows nothing about it: unknown keys are read on load and
written back in place, keeping their original position in the file. Only keys
gh-pmu models are validated — an unrecognized key is carried, not checked.

### Config Integrity

Optional integrity checking for `.gh-pmu.json`. When enabled in strict mode, commands are blocked if the local config differs from the committed version.

```json
{
  "configIntegrity": "strict"
}
```

| Value | Behavior |
|-------|----------|
| *(not set)* | Daily check warns on drift, does not block |
| `"strict"` | Daily check blocks command execution on drift |

**Related files:**
- `.gh-pmu.checksum` — SHA-256 hash of known-good config (gitignored, auto-updated)
- `.gh-pmu-integrity-check.json` — Throttle state for daily checks (gitignored)

**Manual check:** `gh pmu config verify` (see [Commands](commands.md#config-verify))

### Acceptance (Auto-Generated)

The `acceptance` section is written by `gh pmu accept` when terms are accepted. Do not edit manually:

```yaml
acceptance:
  accepted: true
  user: your-name         # Git user who accepted
  date: "2026-02-20"      # Date of acceptance
  version: "0.16.0"       # Version at time of acceptance
```

**Notes:**
- Acceptance is required before using any command (except `init`, `accept`, `--help`, `--version`)
- Stored in `.gh-pmu.json` so acceptance is shared across collaborators
- Re-acceptance is triggered on major or minor version bumps (not patch)
- Run `gh pmu accept` to accept or re-accept terms

### Metadata (Auto-Generated)

The `metadata` section is auto-generated by `gh pmu init`. Do not edit manually:

```yaml
metadata:
  project:
    id: PVT_xxx          # Project node ID
  fields:
    - name: Status
      id: PVTSSF_xxx
      data_type: SINGLE_SELECT
      options:
        - name: Backlog
          id: abc123
```

To refresh metadata after changing project fields:

```bash
gh pmu init --refresh
```

## Complete Example

```yaml
project:
  name: my-app
  owner: myorg
  number: 5

repositories:
  - myorg/frontend
  - myorg/backend
  - myorg/docs

defaults:
  priority: p2
  status: backlog

fields:
  priority:
    field: Priority
    values:
      p0: P0
      p1: P1
      p2: P2
  status:
    field: Status
    values:
      backlog: Backlog
      up_next: Up next
      ready: Ready
      in_progress: In progress
      in_review: In review
      qa_required: QA required
      done: Done
      parking_lot: Parking Lot
      notes: Notes
  size:
    field: Size
    values:
      xs: XS
      s: S
      m: M
      l: L
      xl: XL

triage:
  estimate:
    query: "is:issue is:open -has:estimate"
    apply: {}
    interactive:
      estimate: true

metadata:
  # ... auto-generated ...
```

## Tips

- **Commit `.gh-pmu.json`** to share configuration with your team
- **Use aliases consistently** across your workflow for faster typing
- **Run `init --refresh`** after adding new project fields
- **Multiple triage rules** let you handle different scenarios (untracked, stale, needs-review)

---

## See Also

- [Commands Reference](commands.md) - Full command documentation
- [Workflows Guide](workflows.md) - Branch workflows
- [Sub-Issues Guide](sub-issues.md) - Hierarchy management
- [Batch Operations](batch-operations.md) - Intake, triage, split
