# Batch Operations Guide

Process multiple issues at once with intake, triage, and split commands.

## Overview

| Command | Purpose |
|---------|---------|
| `intake` | Find issues not yet added to the project |
| `triage` | Apply rules to update multiple issues |
| `split` | Break one issue into multiple sub-issues |

## Intake

Find and add untracked issues to your project board.

`intake` needs a mode: `--list`, `--dry-run` or `--apply`. Run without one, it prints its help instead of querying GitHub.

### List Untracked Issues

```bash
gh pmu intake --list
gh pmu intake --list --json
```

### Preview Untracked Issues

```bash
gh pmu intake --dry-run
```

**Output:**
```
Found 3 untracked issues:

#45 Bug: Login fails on mobile
#46 Add dark mode support
#47 Update dependencies

Run with --apply to add these to the project.
```

### Add to Project

```bash
gh pmu intake --apply
```

**Output:**
```
✓ Added #45 to project (Status: Backlog, Priority: P2)
✓ Added #46 to project (Status: Backlog, Priority: P2)
✓ Added #47 to project (Status: Backlog, Priority: P2)

Added 3 issues to project.
```

Issues are added with defaults from `.gh-pmu.json`:

```yaml
defaults:
  priority: p2
  status: backlog
```

## Triage

Apply configured rules to update multiple issues at once.

### Configure Rules

Add triage rules to `.gh-pmu.json`:

```yaml
triage:
  # Rule: Flag stale issues
  stale:
    query: "is:issue is:open updated:<2024-06-01"
    apply:
      labels:
        - needs-attention

  # Rule: High priority bugs
  critical-bugs:
    query: "is:issue is:open label:bug label:critical"
    apply:
      fields:
        priority: p0
        status: ready
```

### Run Triage Rules

**Preview (dry run):**
```bash
gh pmu triage stale --dry-run
```

**Apply changes:**
```bash
gh pmu triage stale --apply
```

### Rule Configuration

| Field | Description |
|-------|-------------|
| `query` | GitHub search query to find issues |
| `apply.labels` | Labels to add automatically |
| `apply.fields` | Project fields to set |

**Query examples:**
```yaml
# Old issues
query: "is:issue is:open updated:<2024-01-01"

# By label combination
query: "is:issue is:open label:bug label:production"

# By assignee
query: "is:issue is:open assignee:@me"
```

## Split

Break a single issue into multiple sub-issues.

### From Checklist

If an issue body contains a checklist:

```markdown
## Tasks
- [ ] Design API schema
- [ ] Implement endpoints
- [ ] Add validation
- [ ] Write tests
```

Convert to sub-issues:

```bash
gh pmu split 42 --from body
```

**Output:**
```
Splitting #42: API Implementation

Found 4 checklist items:
  • Design API schema
  • Implement endpoints
  • Add validation
  • Write tests

? Create sub-issues? (Y/n) y

✓ Created #43: Design API schema (linked to #42)
✓ Created #44: Implement endpoints (linked to #42)
✓ Created #45: Add validation (linked to #42)
✓ Created #46: Write tests (linked to #42)

Created 4 sub-issues.
```

### From Arguments

Create sub-issues from command line:

```bash
gh pmu split 42 "Task 1" "Task 2" "Task 3"
```

**Output:**
```
✓ Created #43: Task 1 (linked to #42)
✓ Created #44: Task 2 (linked to #42)
✓ Created #45: Task 3 (linked to #42)
```

### With Project Fields

Set status/priority for new sub-issues:

```bash
gh pmu split 42 --from body --status ready --priority p1
```

### Cross-Repository

Create sub-issues in a different repository:

```bash
gh pmu split 42 --from body --repo myorg/backend
```

## Workflows

### New Repository Onboarding

When adding a repository to a project:

```bash
# 1. Add repo to config
# Edit .gh-pmu.json to add repository

# 2. Find existing issues
gh pmu intake --dry-run

# 3. Add to project with defaults
gh pmu intake --apply

# 4. Triage by priority
gh pmu triage critical-bugs --apply
```

### Weekly Triage

Regular maintenance workflow:

```bash
# Check for untracked issues
gh pmu intake --dry-run

# Add new issues
gh pmu intake --apply

# Apply stale issue rules
gh pmu triage stale --apply

# Check critical bugs
gh pmu triage critical-bugs --apply
```

### Sprint Planning

Break epics into stories:

```bash
# Create epic with checklist of planned work
gh pmu create --title "Epic: User Dashboard" --body "
- [ ] Design mockups
- [ ] Implement header component
- [ ] Implement stats widget
- [ ] Implement activity feed
- [ ] Add tests
"
# Created #100

# Convert to trackable sub-issues
gh pmu split 100 --from body --status backlog

# Prioritize stories
gh pmu move 101 --priority p0 --status ready
gh pmu move 102 --priority p1 --status ready
```

## Best Practices

1. **Always preview first**
   - Use `--dry-run` before `--apply`
   - Review what will change

2. **Use specific queries**
   - Narrow queries reduce false positives
   - Combine labels for precision

3. **Set sensible defaults**
   - Configure defaults in `.gh-pmu.json`
   - New issues land in the right place

4. **Use dry-run before applying**
   - Preview changes with `--dry-run` before committing
   - Use `--apply` to execute after review

5. **Split early**
   - Convert checklists to sub-issues for better tracking
   - Each sub-issue gets independent status

## See Also

- [Configuration Guide](configuration.md) - Triage rules setup
- [Sub-Issues Guide](sub-issues.md) - Hierarchy management
- [Commands Reference](commands.md) - All command details
