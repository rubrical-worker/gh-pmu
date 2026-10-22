//go:build integration

package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/testutil"
)

// TestRunList_Integration_ListAll tests listing all items from the project
func TestRunList_Integration_ListAll(t *testing.T) {
	testutil.RequireTestEnv(t)

	result := testutil.RunCommand(t, "list")

	testutil.AssertExitCode(t, result, 0)

	// Should contain seed issues from test fixtures
	testutil.AssertContains(t, result.Stdout, "Seed Issue 1")
	testutil.AssertContains(t, result.Stdout, "NUMBER")
	testutil.AssertContains(t, result.Stdout, "TITLE")
	testutil.AssertContains(t, result.Stdout, "STATUS")
	testutil.AssertContains(t, result.Stdout, "PRIORITY")
}

// TestRunList_Integration_FilterByStatus tests filtering by --status flag
func TestRunList_Integration_FilterByStatus(t *testing.T) {
	testutil.RequireTestEnv(t)

	// Test filtering by "Backlog" status
	result := testutil.RunCommand(t, "list", "--status", "backlog")

	testutil.AssertExitCode(t, result, 0)

	// Should contain Backlog issues
	testutil.AssertContains(t, result.Stdout, "Backlog")

	// Seed Issue 1 is Backlog and must appear...
	testutil.AssertContains(t, result.Stdout, "Seed Issue 1")
	// ...Seed Issue 2 is In progress and must NOT appear. Without this exclusion a
	// silently-ignored --status would still pass the inclusion-only check.
	testutil.AssertNotContains(t, result.Stdout, "Seed Issue 2")
}

// TestRunList_Integration_FilterByStatusInProgress tests filtering by in_progress status
func TestRunList_Integration_FilterByStatusInProgress(t *testing.T) {
	testutil.RequireTestEnv(t)

	result := testutil.RunCommand(t, "list", "--status", "in_progress")

	testutil.AssertExitCode(t, result, 0)

	// Should contain "In progress" issues
	// Seed Issue 2 and Seed Issue 4 are "In progress"
	testutil.AssertContains(t, result.Stdout, "In progress")

	// Seed Issue 1 is Backlog and must NOT appear under the in_progress filter.
	testutil.AssertNotContains(t, result.Stdout, "Seed Issue 1")
}

// TestRunList_Integration_FilterByPriority tests filtering by --priority flag
func TestRunList_Integration_FilterByPriority(t *testing.T) {
	testutil.RequireTestEnv(t)

	// Test filtering by P0 priority
	result := testutil.RunCommand(t, "list", "--priority", "p0")

	testutil.AssertExitCode(t, result, 0)

	// Seed Issue 1 is P0 and must appear...
	testutil.AssertContains(t, result.Stdout, "Seed Issue 1")
	testutil.AssertContains(t, result.Stdout, "P0")
	// ...Seed Issue 2 is P1 and must NOT appear under the P0 filter.
	testutil.AssertNotContains(t, result.Stdout, "Seed Issue 2")
}

// TestRunList_Integration_FilterByPriorityP1 tests filtering by P1 priority
func TestRunList_Integration_FilterByPriorityP1(t *testing.T) {
	testutil.RequireTestEnv(t)

	result := testutil.RunCommand(t, "list", "--priority", "p1")

	testutil.AssertExitCode(t, result, 0)

	// Seed Issues 2, 4, 5 are P1
	testutil.AssertContains(t, result.Stdout, "P1")

	// Seed Issue 1 is P0 and must NOT appear under the P1 filter.
	testutil.AssertNotContains(t, result.Stdout, "Seed Issue 1")
}

// TestRunList_Integration_FilterByPriorityP2 tests filtering by P2 priority
func TestRunList_Integration_FilterByPriorityP2(t *testing.T) {
	testutil.RequireTestEnv(t)

	result := testutil.RunCommand(t, "list", "--priority", "p2")

	testutil.AssertExitCode(t, result, 0)

	// Seed Issues 3, 6 are P2
	testutil.AssertContains(t, result.Stdout, "P2")

	// Seed Issue 1 is P0 and must NOT appear under the P2 filter.
	testutil.AssertNotContains(t, result.Stdout, "Seed Issue 1")
}

// TestRunList_Integration_JSONOutput tests --json output format
func TestRunList_Integration_JSONOutput(t *testing.T) {
	testutil.RequireTestEnv(t)

	result := testutil.RunCommand(t, "list", "--json=number,title,state,url,repository,assignees,fieldValues")

	testutil.AssertExitCode(t, result, 0)

	// Parse JSON output
	var output struct {
		Items []struct {
			Number      int               `json:"number"`
			Title       string            `json:"title"`
			State       string            `json:"state"`
			URL         string            `json:"url"`
			Repository  string            `json:"repository"`
			Assignees   []string          `json:"assignees"`
			FieldValues map[string]string `json:"fieldValues"`
		} `json:"items"`
	}

	err := json.Unmarshal([]byte(result.Stdout), &output)
	if err != nil {
		t.Fatalf("failed to parse JSON output: %v\nOutput: %s", err, result.Stdout)
	}

	// Should have items
	if len(output.Items) == 0 {
		t.Error("expected items in JSON output, got none")
	}

	// Verify structure of first item
	found := false
	for _, item := range output.Items {
		if strings.Contains(item.Title, "Seed Issue") {
			found = true
			if item.Number == 0 {
				t.Error("expected non-zero issue number")
			}
			if item.URL == "" {
				t.Error("expected non-empty URL")
			}
			if item.Repository == "" {
				t.Error("expected non-empty repository")
			}
			break
		}
	}

	if !found {
		t.Error("expected to find a Seed Issue in JSON output")
	}
}

// TestRunList_Integration_EmptyResult tests empty result handling
func TestRunList_Integration_EmptyResult(t *testing.T) {
	testutil.RequireTestEnv(t)

	// Filter by a status that doesn't exist in seed data
	// "Ready" status has no seed issues
	result := testutil.RunCommand(t, "list", "--status", "ready")

	testutil.AssertExitCode(t, result, 0)

	// Should show "No issues found" message
	testutil.AssertContains(t, result.Stdout, "No issues found")
}

// TestRunList_Integration_HasSubIssues tests --has-sub-issues filter
func TestRunList_Integration_HasSubIssues(t *testing.T) {
	testutil.RequireTestEnv(t)

	result := testutil.RunCommand(t, "list", "--has-sub-issues")

	testutil.AssertExitCode(t, result, 0)

	// Seed Issue 4 has sub-issue #5 and must appear...
	testutil.AssertContains(t, result.Stdout, "Seed Issue 4")

	// ...Seed Issue 1 is a plain backlog issue with no sub-issues. It is a member of
	// the unfiltered project list, so its absence here confirms --has-sub-issues
	// actually excludes issues that have none (rather than being a no-op filter).
	testutil.AssertNotContains(t, result.Stdout, "Seed Issue 1")
}

// TestRunList_Integration_CombinedFilters tests combining multiple filters
func TestRunList_Integration_CombinedFilters(t *testing.T) {
	testutil.RequireTestEnv(t)

	// Filter by status=in_progress AND priority=p1
	result := testutil.RunCommand(t, "list", "--status", "in_progress", "--priority", "p1")

	testutil.AssertExitCode(t, result, 0)

	// Seed Issues 2 and 4 are both In progress + P1
	testutil.AssertContains(t, result.Stdout, "In progress")
	testutil.AssertContains(t, result.Stdout, "P1")

	// Seed Issue 1 (Backlog + P0) matches neither filter and must NOT appear —
	// a no-op combined filter would leak it.
	testutil.AssertNotContains(t, result.Stdout, "Seed Issue 1")
}

// TestRunList_Integration_Limit tests --limit flag
func TestRunList_Integration_Limit(t *testing.T) {
	testutil.RequireTestEnv(t)

	result := testutil.RunCommand(t, "list", "--limit", "2")

	testutil.AssertExitCode(t, result, 0)

	// Count lines (excluding header)
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	// First line is header, so we expect at most 3 lines (header + 2 items)
	if len(lines) > 3 {
		t.Errorf("expected at most 3 lines with --limit 2, got %d", len(lines))
	}
}

// TestRunList_Integration_Search tests --search flag
func TestRunList_Integration_Search(t *testing.T) {
	testutil.RequireTestEnv(t)

	result := testutil.RunCommand(t, "list", "--search", "Parent")

	testutil.AssertExitCode(t, result, 0)

	// Should find "Seed Issue 4: Parent with Sub-issues"
	testutil.AssertContains(t, result.Stdout, "Seed Issue 4")
}
