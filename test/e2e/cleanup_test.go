//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

const testRepo = "rubrical-worker/gh-pmu-e2e-test"

// TestCleanupE2EIssues cleans up any leftover E2E test issues and orphaned projects.
// This test only runs when E2E_CLEANUP=true is set.
func TestCleanupE2EIssues(t *testing.T) {
	if os.Getenv("E2E_CLEANUP") != "true" {
		t.Skip("E2E_CLEANUP not set to true, skipping cleanup")
	}

	cleanupE2EIssues(t)
	cleanupOrphanedProjects(t)
}

// cleanupE2EIssues finds and deletes all issues with [E2E] prefix.
func cleanupE2EIssues(t *testing.T) {
	t.Helper()

	// Find all open issues with [E2E] prefix
	issues := findE2EIssues(t)

	if len(issues) == 0 {
		t.Log("No E2E test issues found to clean up")
		return
	}

	t.Logf("Found %d E2E test issues to clean up", len(issues))

	for _, issue := range issues {
		closeAndDeleteIssue(t, issue.Number, issue.Title)
	}

	t.Logf("Cleanup complete: removed %d issues", len(issues))
}

// e2eIssue represents a test issue to clean up
type e2eIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

// findE2EIssues searches for issues with [E2E] prefix in the test repository.
func findE2EIssues(t *testing.T) []e2eIssue {
	t.Helper()

	// Use gh issue list to find [E2E] issues
	output, _, err := runGH("issue", "list",
		"--repo", testRepo,
		"--search", "[E2E] in:title",
		"--state", "all",
		"--limit", "100",
		"--json", "number,title",
	)
	if err != nil {
		t.Logf("Warning: failed to list E2E issues: %v", err)
		return nil
	}

	var issues []e2eIssue
	if err := json.Unmarshal(output, &issues); err != nil {
		t.Logf("Warning: failed to parse issue list: %v", err)
		return nil
	}

	// Filter to only issues with [E2E] prefix (search might include false positives)
	var filtered []e2eIssue
	for _, issue := range issues {
		if strings.HasPrefix(issue.Title, "[E2E]") {
			filtered = append(filtered, issue)
		}
	}

	return filtered
}

// closeAndDeleteIssue closes and deletes a single issue, logging the action.
func closeAndDeleteIssue(t *testing.T, number int, title string) {
	t.Helper()

	t.Logf("Removing issue #%d: %s", number, title)

	// Close the issue
	if stdout, stderr, err := runGH("issue", "close",
		"--repo", testRepo,
		strconv.Itoa(number),
		"--reason", "not planned",
	); err != nil {
		t.Logf("  Warning: failed to close #%d: %v\n  Output: %s%s", number, err, stdout, stderr)
	}

	// Delete the issue (requires admin permissions)
	if _, _, err := runGH("issue", "delete",
		"--repo", testRepo,
		strconv.Itoa(number),
		"--yes",
	); err != nil {
		// Log but don't fail - deletion may not be available
		t.Logf("  Note: could not delete #%d (may require admin): %v", number, err)
	} else {
		t.Logf("  Deleted issue #%d", number)
	}
}

// e2eProject represents a test project to clean up
type e2eProject struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

// cleanupOrphanedProjects finds and deletes projects created by init E2E tests.
// The real test project is #41; anything named "gh-pmu-e2e-test" with a higher
// number is an orphan from a previous test run.
func cleanupOrphanedProjects(t *testing.T) {
	t.Helper()

	output, _, err := runGH("project", "list",
		"--owner", "rubrical-worker",
		"--format", "json",
		"--limit", "100",
	)
	if err != nil {
		t.Logf("Warning: failed to list projects: %v", err)
		return
	}

	var resp struct {
		Projects []e2eProject `json:"projects"`
	}
	if err := json.Unmarshal(output, &resp); err != nil {
		t.Logf("Warning: failed to parse project list: %v", err)
		return
	}

	var orphans []e2eProject
	for _, p := range resp.Projects {
		if p.Title == "gh-pmu-e2e-test" && p.Number != 41 {
			orphans = append(orphans, p)
		}
	}

	if len(orphans) == 0 {
		t.Log("No orphaned test projects found")
		return
	}

	t.Logf("Found %d orphaned test projects to clean up", len(orphans))
	for _, p := range orphans {
		t.Logf("Deleting orphaned project #%d: %s", p.Number, p.Title)
		if stdout, stderr, err := runGH("project", "delete",
			strconv.Itoa(p.Number),
			"--owner", "rubrical-worker",
		); err != nil {
			t.Logf("  Warning: failed to delete project #%d: %v\n  Output: %s%s", p.Number, err, stdout, stderr)
		} else {
			t.Logf("  Deleted project #%d", p.Number)
		}
	}
}

// runCleanupAfterTest is a helper that can be deferred in tests to clean up
// a specific issue after the test completes.
func runCleanupAfterTest(t *testing.T, issueNum int) {
	t.Helper()

	if issueNum <= 0 {
		return
	}

	// Get issue title for logging
	output, _, _ := runGH("issue", "view",
		"--repo", testRepo,
		strconv.Itoa(issueNum),
		"--json", "title",
	)

	var result struct {
		Title string `json:"title"`
	}
	json.Unmarshal(output, &result)

	title := result.Title
	if title == "" {
		title = fmt.Sprintf("Issue #%d", issueNum)
	}

	closeAndDeleteIssue(t, issueNum, title)
}
