# Command Reference

Complete reference for all gh-pmu commands.

## Overview

```
gh pmu [command]

Project Management:
  init        Initialize configuration
  list        List issues with project metadata
  view        View issue with project fields
  create      Create issue with project fields
  edit        Edit issue body and metadata
  comment     Add comment to an issue
  move        Update issue project fields
  close       Close issue with optional reason
  board       View project board in terminal
  field       Manage custom project fields

Sub-Issue Management:
  sub add     Link existing issue as sub-issue
  sub create  Create new sub-issue under parent
  sub list    List sub-issues of a parent
  sub remove  Unlink sub-issue from parent

Batch Operations:
  intake      Find and add untracked issues to project
  triage      Bulk update issues based on config rules
  split       Create sub-issues from checklist or arguments

Utilities:
  filter      Filter piped issue JSON by project fields
  history     Show git commit history with issue references

Label Management:
  label sync    Sync standard labels from defaults
  label list    List all repository labels
  label add     Create a label
  label update  Update a label
  label delete  Delete a label

Configuration:
  config verify  Verify config integrity against git HEAD

Workflow Commands:
  branch      Manage branches for development workflows
  validation  Manage status transition validation rules
  accept      Accept terms and conditions

Flags:
  -h, --help      help for gh-pmu
  -v, --version   version for gh-pmu
```

---

## Project Management Commands

### init

Initialize project configuration by copying from a template project or connecting to an existing one.

```bash
# Copy from a source project template
gh pmu init --source-project 30 --repo owner/repo

# Connect to an existing project
gh pmu init --project 42 --repo owner/repo

# Refresh metadata only
gh pmu init --refresh
```

| Flag | Description |
|------|-------------|
| `--source-project N` | Source project number to copy from |
| `--project N` | Existing project number to connect to |
| `--repo owner/repo` | Repository to configure (required) |
| `--owner` | Project owner (defaults to repo owner) |
| `--framework` | Framework type: `IDPF` (default) or `none` |
| `--yes` | Auto-confirm prompts (overwrite existing config) |

`--project` and `--source-project` are mutually exclusive.

### list

List issues with project metadata.

```bash
# List all issues in project
gh pmu list

# Filter by status (🆕 unique to gh-pmu)
gh pmu list --status in_progress

# Filter by priority (🆕 unique to gh-pmu)
gh pmu list --priority p0

# Filter to parent issues only (🆕 unique to gh-pmu)
gh pmu list --has-sub-issues

# Combine filters
gh pmu list --status ready --priority p1

# JSON output (list available fields)
gh pmu list --json

# JSON output with specific fields (use = syntax)
gh pmu list --json=number,title,state

# JSON output with jq filtering
gh pmu list --json=number,title,state --jq '.items[].number'

# Specify repository
gh pmu list --repo owner/other-repo
```

**Flags unique to gh-pmu:**
| Flag | Purpose |
|------|---------|
| `--status` | Filter by project status field |
| `--priority` | Filter by project priority field |
| `--has-sub-issues` | Show only parent issues |

**Flags matching `gh issue list`:**
| Flag | Purpose |
|------|---------|
| `--assignee` / `-a` | Filter by assignee login; `@me` resolves to your authenticated login (see [Assignee Resolution](#assignee-resolution)) |

**Output:**
```
#   Title                          Status        Priority
42  Add user authentication        In progress   P1
43  Fix login redirect             Ready         P0
45  Update documentation           Backlog       P2
```

### view

View issue with project fields and sub-issue progress.

```bash
# View issue
gh pmu view 42

# JSON output (list available fields)
gh pmu view 42 --json

# JSON output with specific fields (use = syntax)
gh pmu view 42 --json=number,title,body,state

# JSON output with jq filtering
gh pmu view 42 --json=number,title --jq '.number'

# Specify repository
gh pmu view 42 --repo owner/other-repo
```

**Output:**
```
#42 Add user authentication
Status: In progress | Priority: P1 | Size: M

Labels: enhancement, backend
Assignees: @developer

Sub-issues: [████████░░] 80% (4/5)
  ✓ #46 Design auth flow
  ✓ #47 Implement JWT tokens
  ✓ #48 Add login endpoint
  ✓ #49 Add logout endpoint
  ○ #50 Write tests

https://github.com/myorg/app/issues/42
```

### create

Create issue with project fields set in one command.

```bash
# Basic creation
gh pmu create --title "New feature"

# With project fields (🆕 unique to gh-pmu)
gh pmu create --title "Fix bug" --status ready --priority p0

# Create from YAML/JSON file (🆕 unique to gh-pmu)
gh pmu create --from-file issue.yml

# With body
gh pmu create --title "Add caching" --body "Implement Redis caching for API"

# With body from file
gh pmu create --title "Add caching" --body-file description.md

# Use issue template
gh pmu create --title "Bug report" --template bug

# With labels
gh pmu create --title "Security fix" --label bug --label security
```

**Flags unique to gh-pmu:**
| Flag | Purpose |
|------|---------|
| `--status` | Set project status field on create |
| `--priority` | Set project priority field on create |
| `--branch` | Assign to branch (use 'current' for active) |
| `--from-file` | Create issue from YAML/JSON file |

**Flags matching `gh issue create`:**
| Flag | Purpose |
|------|---------|
| `--body-file` / `-F` | Read body text from file |
| `--body-stdin` | Read body text from standard input |
| `--template` / `-T` | Use issue template from `.github/ISSUE_TEMPLATE/` |
| `--assignee` / `-a` | Assign users (repeatable); `@me` resolves to your authenticated login (see [Assignee Resolution](#assignee-resolution)) |

**Output:**
```
✓ Created issue #51: New feature
  • Status → Backlog
  • Priority → P2
🔗 https://github.com/myorg/app/issues/51
```

### edit

Edit issue body and metadata.

```bash
# Edit issue body interactively
gh pmu edit 42

# Export body to file for editing
gh pmu edit 42 --body-file issue.md

# Update body from file
gh pmu edit 42 -F issue.md

# Read body from stdin
gh pmu edit 42 --body-stdin

# Output body to stdout (for piping)
gh pmu view 42 --body-stdout

# Edit issue in different repository
gh pmu edit 42 --repo owner/other-repo
```

**Flags:**
| Flag | Purpose |
|------|---------|
| `--body-file` / `-F` | Read body from file, or export to file if used alone |
| `--body-stdin` | Read body from standard input |
| `--repo` / `-R` | Specify repository (owner/repo format) |

**Output:**
```
✓ Updated issue #42
🔗 https://github.com/myorg/app/issues/42
```

### comment

Add a comment to an issue.

```bash
# Add comment with inline body
gh pmu comment 42 --body "This looks good!"

# Add comment from file
gh pmu comment 42 -F comment.md

# Add comment from stdin (useful for piping)
echo "LGTM" | gh pmu comment 42 --body-stdin

# Comment on issue in different repository
gh pmu comment 42 --body "Fixed in main" --repo owner/other-repo
```

**Flags:**
| Flag | Purpose |
|------|---------|
| `--body` / `-b` | Comment body text |
| `--body-file` / `-F` | Read comment body from file |
| `--body-stdin` | Read comment body from standard input |
| `--repo` / `-R` | Specify repository (owner/repo format) |

**Note:** Exactly one of `--body`, `--body-file`, or `--body-stdin` is required.

**Output:**
```
✓ Added comment to issue #42
🔗 https://github.com/myorg/app/issues/42
```

### move

Update issue project fields. **🆕 This entire command is unique to gh-pmu.**

```bash
# Update status
gh pmu move 42 --status in_review

# Update multiple fields
gh pmu move 42 --status done --priority p0

# Move multiple issues at once (🆕 unique)
gh pmu move 42 43 44 --status done

# Recursive update
gh pmu move 42 --status done --priority p0

# Recursive update - cascade to all sub-issues (🆕 unique)
gh pmu move 42 --status done --recursive

# Preview changes without applying (🆕 unique)
gh pmu move 42 --status done --recursive --dry-run

# Limit recursion depth (🆕 unique)
gh pmu move 42 --status in_progress --recursive --depth 2

# Skip confirmation prompt (🆕 unique)
gh pmu move 42 --status done --recursive --yes

# Specify repository
gh pmu move 42 --status done --repo owner/other-repo
```

**Flags unique to gh-pmu:**
| Flag | Purpose |
|------|---------|
| `--recursive` | Apply changes to all sub-issues |
| `--dry-run` | Preview what would change |
| `--depth` | Limit recursion depth (default 10) |
| `--yes` | Skip confirmation for recursive ops |

**Label automation:**
- `--branch` adds the `assigned` label to issues (auto-created if missing)
- `--backlog` removes the `assigned` label from open issues

**Output:**
```
✓ Updated issue #42: Add user authentication
  • Status → In review
🔗 https://github.com/myorg/app/issues/42
```

**Recursive output:**
```
✓ Updated #42 Epic: Auth → Done
✓ Updated #43 Login flow → Done
✓ Updated #44 Password reset → Done
```

### close

Close issue with optional state reason.

```bash
# Close as completed (default)
gh pmu close 42

# Close as not planned
gh pmu close 42 --reason not_planned

# Update project status to 'done' before closing (🆕 unique to gh-pmu)
gh pmu close 42 --update-status

# Combine: update status and close with reason
gh pmu close 42 --reason completed --update-status

# Specify repository
gh pmu close 42 --repo owner/other-repo
```

**Flags unique to gh-pmu:**
| Flag | Purpose |
|------|---------|
| `--update-status` | Move issue to 'done' status before closing |

**Reason aliases:**
| Alias | GitHub State Reason |
|-------|---------------------|
| `completed` | completed |
| `not_planned`, `wontfix` | not_planned |

> **Note:** GitHub API doesn't have a `duplicate` close reason. To close duplicates, use:
> `gh pmu close 42 --reason not_planned` and link to the original issue in a comment.

**Output:**
```
✓ Closed issue #42: Add user authentication (completed)
🔗 https://github.com/myorg/app/issues/42
```

### board

View project board in terminal.

```bash
# Show board
gh pmu board

# Show board for a different repository
gh pmu board --repo owner/other-repo

# Filter by status or priority
gh pmu board --status in_progress
gh pmu board --priority p0

# Limit issues per column
gh pmu board --limit 5

# Output as JSON
gh pmu board --json
```

**Output:**
```
┌──────────┬───────────────┬─────────────┬────────┐
│ Backlog  │ Ready         │ In Progress │ Done   │
├──────────┼───────────────┼─────────────┼────────┤
│ #52 Add  │ #43 Fix login │ #42 Auth    │ #41    │
│ #53 Docs │               │ #44 API     │ #40    │
│          │               │             │ #39    │
└──────────┴───────────────┴─────────────┴────────┘
```

### field

Manage project fields.

```bash
# List all fields
gh pmu field list

# Create a new single-select field
gh pmu field create --name "Sprint" --type single_select --options "Sprint 1,Sprint 2,Sprint 3"
```

**Output:**
```
Name        Type           Options
Status      SINGLE_SELECT  Backlog, Ready, In progress, In review, Done
Priority    SINGLE_SELECT  P0, P1, P2
Size        SINGLE_SELECT  XS, S, M, L, XL
Estimate    NUMBER         -
Start date  DATE           -
```

---

## Sub-Issue Commands

See [Sub-Issues Guide](sub-issues.md) for detailed workflows.

### sub add

Link existing issue as sub-issue.

```bash
gh pmu sub add 10 15    # Issue 15 becomes sub-issue of 10

# Specify repository
gh pmu sub add 10 15 --repo owner/other-repo
```

### sub create

Create new sub-issue under parent. **🆕 This entire command is unique to gh-pmu.**

```bash
gh pmu sub create --parent 10 --title "Subtask 1"

# With project fields
gh pmu sub create --parent 10 --title "Subtask" --status ready --priority p1

# Cross-repository
gh pmu sub create --parent 10 --title "Backend task" --repo owner/backend

# Control inheritance from parent (🆕 unique flags)
gh pmu sub create --parent 10 --title "Task" --no-inherit-labels
gh pmu sub create --parent 10 --title "Task" --inherit-assignees
```

**Flags unique to gh-pmu:**
| Flag | Purpose |
|------|---------|
| `--inherit-labels` | Copy labels from parent (default: false) |
| `--inherit-milestone` | Copy milestone from parent (default: true) |
| `--inherit-assignees` | Copy assignees from parent (default: false) |

**Flags matching `gh issue create`:**
| Flag | Purpose |
|------|---------|
| `--assignee` / `-a` | Assign users (repeatable); `@me` resolves to your authenticated login (see [Assignee Resolution](#assignee-resolution)) |

Explicit `--assignee` values are merged with any inherited from the parent via
`--inherit-assignees`, explicit first, deduplicated. Inheritance is skipped for
cross-repository sub-issues, since account membership does not carry across
repositories.

### sub list

List sub-issues of a parent.

```bash
gh pmu sub list 10

# JSON output
gh pmu sub list 10 --json
```

**Output:**
```
Parent: #10 Epic: User Management

Sub-issues:
  ✓ #11 Design user schema
  ✓ #12 Implement CRUD endpoints
  ○ #13 Add validation
  ○ #14 Write tests

Progress: [████████░░] 50% (2/4)
```

### sub remove

Unlink sub-issue from parent.

```bash
gh pmu sub remove 10 15

# Specify repository
gh pmu sub remove 10 15 --repo owner/other-repo
```

---

## Batch Operations

See [Batch Operations Guide](batch-operations.md) for detailed workflows.

### intake

Find and add untracked issues to project.

```bash
# List untracked issues
gh pmu intake --list

# Preview untracked issues
gh pmu intake --dry-run

# Add to project with defaults
gh pmu intake --apply

# Only issues assigned to you
gh pmu intake --assignee @me --dry-run
```

A mode is required: `--list`, `--dry-run` or `--apply`. With none, `intake` prints
its help and makes no API calls. `--json`, `--label` and `--assignee` modify a mode
but do not select one. `--list` and `--apply` cannot be combined.

`--assignee` is repeatable and matches issues carrying **any** of the supplied
logins. `@me` resolves to your authenticated login — see
[Assignee Resolution](#assignee-resolution).

### triage

Bulk update issues based on config rules.

```bash
# Preview rule effects
gh pmu triage untracked --dry-run

# Apply rule
gh pmu triage untracked --apply
```

### split

Create sub-issues from checklist or arguments. **🆕 This entire command is unique to gh-pmu.**

```bash
# From checklist in issue body (🆕 unique)
gh pmu split 42 --from body

# From external file (🆕 unique)
gh pmu split 42 --from tasks.md

# From arguments
gh pmu split 42 "Task 1" "Task 2" "Task 3"

# Preview without creating (🆕 unique)
gh pmu split 42 --from body --dry-run

# With status for new sub-issues
gh pmu split 42 --from body --status ready

# Specify repository
gh pmu split 42 --from body --repo owner/other-repo
```

**Flags unique to gh-pmu:**
| Flag | Purpose |
|------|---------|
| `--from` | Source: 'body' (issue body) or file path |
| `--dry-run` | Preview what would be created |

---

## Utilities

### filter

Filter piped issue JSON by project fields.

```bash
# Filter by status
gh issue list --json number,title | gh pmu filter --status ready

# Filter with JSON output
gh issue list --json number,title | gh pmu filter --status in_progress --json

# From another repository
gh issue list -R owner/repo --json number,title | gh pmu filter --priority p0

# Filter to your own issues
gh issue list --json number,title,assignees | gh pmu filter --assignee @me
```

`--assignee` matches against the `assignees` field of the piped JSON, so include
it in the `gh issue list --json` field list. `@me` resolves to your authenticated
login — see [Assignee Resolution](#assignee-resolution).

### history

Show git commit history with issue references.

```bash
# Current directory
gh pmu history

# Specific path
gh pmu history src/

# Limit results
gh pmu history --limit 20
```

**Output:**
```
abc1234 feat: Add login endpoint (#42)
def5678 fix: Handle null user (#43)
ghi9012 docs: Update API reference
```

---

## Label Management

Commands for managing repository labels without running a full init.

### label sync

Sync standard labels from `defaults.yml` to the repository.

```bash
# Preview changes without applying
gh pmu label sync --dry-run

# Create missing standard labels
gh pmu label sync

# Also update color/description of existing labels
gh pmu label sync --update
```

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview changes without applying |
| `--update` | Update color/description of existing labels |
| `-R, --repo` | Repository (owner/repo format) |

### label list

List all labels in the repository with a standard/custom indicator.

```bash
gh pmu label list
```

Labels matching entries in `defaults.yml` are marked as "standard". Others are marked as "custom".

### label add

Create a new label in the repository.

```bash
gh pmu label add my-label --color ff0000 --description "My custom label"
```

| Flag | Description |
|------|-------------|
| `--color` | Label color (hex without #) |
| `--description` | Label description |
| `-R, --repo` | Repository (owner/repo format) |

### label update

Update an existing label's color or description.

```bash
gh pmu label update my-label --color 00ff00
gh pmu label update my-label --description "Updated description"
```

| Flag | Description |
|------|-------------|
| `--color` | New label color (hex without #) |
| `--description` | New label description |
| `-R, --repo` | Repository (owner/repo format) |

### label delete

Delete a label from the repository.

```bash
gh pmu label delete my-label
```

| Flag | Description |
|------|-------------|
| `-R, --repo` | Repository (owner/repo format) |

---

## Workflow Commands

Commands for managing development workflows.

### branch

Manage branches for development workflows (release, patch, feature branches).

```bash
# Start a new branch (creates git branch and tracker issue)
gh pmu branch start --name release/v2.0.0

# Start a patch branch
gh pmu branch start --name patch/v1.9.1

# Start a hotfix branch
gh pmu branch start --name hotfix-auth-bypass

# Assign issues to current branch
gh pmu move 42 --branch current

# View current branch
gh pmu branch current

# Close branch (closes tracker, optional tag)
gh pmu branch close

# List branch history
gh pmu branch list
gh pmu branch list
```

**Notes:**
- The `--name` flag is required and specifies the branch name to create
- Branch name is used for tracker title, Branch field, and artifact directory
- Supports any branch naming convention: `release/v2.0.0`, `patch/v1.9.1`, `hotfix-auth-bypass`
- `branch close` and `branch remove` automatically remove the `assigned` label from open issues

### validation

Manage status transition validation rules. Ensures issues follow a logical workflow progression.

```bash
# View configured transition rules
gh pmu validation rules

# Check if a transition is allowed
gh pmu validation check backlog in_progress    # ✓ Allowed
gh pmu validation check done backlog           # ✗ Not allowed

# Enable/disable validation
gh pmu validation enable
gh pmu validation disable
```

**Notes:**
- Validation is configured in the `validation` section of `.gh-pmu.json`
- When enabled, `move`, `create`, and workflow commands enforce transition rules
- Use `--force` flag on `move` command to bypass validation when needed

**Default transition rules:**
```
backlog → ready, in_progress
ready → in_progress, backlog
in_progress → in_review, ready, backlog
in_review → done, in_progress
done → (no transitions allowed by default)
```

### accept

Accept terms and conditions. Required before using any other command (except `init` and `--help`/`--version`).

```bash
# Interactive acceptance (displays terms, prompts for confirmation)
gh pmu accept

# Non-interactive acceptance (for CI/automation)
gh pmu accept --yes

# Accept in a specific directory
gh pmu accept --dir /path/to/repo
```

**Flags:**
| Flag | Purpose |
|------|---------|
| `--yes` | Accept terms (required) |
| `--dir` | Directory to search for config (default: current directory) |

**Notes:**
- Acceptance is stored in `.gh-pmu.json` and shared across collaborators
- Re-acceptance is required when the major or minor version changes (patch updates do not require re-acceptance)
- The `init`, `accept`, `--help`, and `--version` commands are exempt from the acceptance gate

**Output:**
```
Terms and Conditions
────────────────────
[terms text displayed]

✓ Terms accepted
  Accepted by: your-name
  Accepted on: 2026-02-20
  Version: 0.16.0
```

---

### config verify

Verify config integrity against the last committed version in git HEAD.

```bash
# Basic integrity check
gh pmu config verify

# Include remote (origin/main) comparison
gh pmu config verify --remote

# Check config in a specific directory
gh pmu config verify --dir /path/to/repo

# Resolve and persist the Backlog board view number
gh pmu config verify --resolve-view
```

**Flags:**
| Flag | Purpose |
|------|---------|
| `--remote` | Also compare against origin/main |
| `--dir` | Directory to search for config (default: current directory) |
| `--resolve-view` | Resolve the Backlog board view number and persist it as `project.view` |

**Daily Auto-Check:**
On the first `gh pmu` command each day, the tool automatically compares local `.gh-pmu.json` against HEAD and warns if drift is detected. This check is throttled to once per day.

**Strict Mode:**
Add `"configIntegrity": "strict"` to `.gh-pmu.json` to block command execution when config drift is detected.

**Checksum File:**
A `.gh-pmu.checksum` file (gitignored) stores the SHA-256 hash of the known-good config. Updated automatically when config is written via `gh pmu accept`, `gh pmu field`, or `gh pmu config verify --resolve-view`. There is no command that re-blesses the checksum after a deliberate hand-edit; `config verify` reports the mismatch but does not clear it.

---

## Global Flags

These flags work with most commands:

| Flag | Description |
|------|-------------|
| `--repo owner/repo` | Specify repository (overrides config) |
| `--json` | Output in JSON format |
| `--help` | Show command help |
| `--no-cache-fallback` | Disable cached-metadata fallback when GitHub's `ProjectV2.fields` resolver returns 5xx — propagate the error unchanged (fail-loud, for CI/strict automation). See the [Resilience](../README.md#resilience) section. |
| `--refresh-cached-fields` | When the cache-fallback engages, refresh each cached field via per-name `ProjectV2.field(name:)` queries. Partial failures retain the cached value. |

## Assignee Resolution

`--assignee` / `-a` is accepted by `create`, `sub create`, `list`, `filter`, and
`intake`, and by `assignees:` entries in a `create --from-file` payload. Every one
of them resolves the value the same way:

| Value | Behavior |
|-------|----------|
| `@me` | Resolved to your authenticated GitHub login — the value `gh api user --jq .login` returns. |
| Any other value | Used as given, once confirmed to name a real GitHub account. |
| Unresolvable value | The command fails with exit 1 and creates nothing. |

```bash
# Assign to yourself without knowing your login
gh pmu create --title "Fix login redirect" --assignee @me

# Filter to your own issues
gh pmu list --assignee @me

# Explicit logins work as before
gh pmu create --title "Handoff" --assignee octocat
```

**Your git identity is not your GitHub identity.** `@me` resolves through the
GitHub API, not `git config user.name`. If the two differ — a common case — only
`@me` or your actual GitHub login will work.

**Unresolvable assignees abort the command.** A typo'd, deleted, or renamed
account fails the whole invocation rather than being skipped, because resolution
runs before the issue is created:

```
$ gh pmu create --title "Test" --assignee ghost
Error: assignee "ghost" could not be resolved: ... Could not resolve to a User
with the login of 'ghost'.
$ echo $?
1
```

Nothing is created when this happens, so there is no partially-configured issue
to clean up. When several assignees are supplied, the whole set fails together
and the message distinguishes partial from total:
`1 of 3 assignees could not be resolved: ghost` versus
`all 3 assignees could not be resolved`.

GitHub does **not** redirect renamed accounts in this lookup, so a login that
worked before an account rename will fail afterward.

Repeated mentions of the same value cost one lookup per invocation, not one per
occurrence.

## See Also

- [Configuration Guide](configuration.md) - Setup and field aliases
- [Workflows Guide](workflows.md) - Branch-based workflows
- [Sub-Issues Guide](sub-issues.md) - Hierarchy management
- [Batch Operations](batch-operations.md) - Intake, triage, split
- [gh vs gh pmu](gh-comparison.md) - When to use which
