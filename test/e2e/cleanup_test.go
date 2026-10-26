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

// TestCleanupE2EIssues cleans up any leftover E2E test issues and orphaned projects.
// This test only runs when E2E_CLEANUP=true is set. It covers every fixture
// owner, selected or not, since orphans outlive the run that made them.
func TestCleanupE2EIssues(t *testing.T) {
	if os.Getenv("E2E_CLEANUP") != "true" {
		t.Skip("E2E_CLEANUP not set to true, skipping cleanup")
	}

	for _, f := range allFixtures {
		cleanupE2EIssues(t, f.Repo)
		cleanupOrphanedProjects(t, f)
	}
}

// cleanupE2EIssues finds and deletes all issues with [E2E] prefix in repo.
func cleanupE2EIssues(t *testing.T, repo string) {
	t.Helper()

	// Find all open issues with [E2E] prefix
	issues := findE2EIssues(t, repo)

	if len(issues) == 0 {
		t.Logf("No E2E test issues found to clean up in %s", repo)
		return
	}

	t.Logf("Found %d E2E test issues to clean up in %s", len(issues), repo)

	for _, issue := range issues {
		closeAndDeleteIssue(t, repo, issue.Number, issue.Title)
	}

	t.Logf("Cleanup complete: removed %d issues", len(issues))
}

// e2eIssue represents a test issue to clean up
type e2eIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

// findE2EIssues searches for issues with [E2E] prefix in repo.
func findE2EIssues(t *testing.T, repo string) []e2eIssue {
	t.Helper()

	// Use gh issue list to find [E2E] issues
	output, _, err := runGH("issue", "list",
		"--repo", repo,
		"--search", "[E2E] in:title",
		"--state", "all",
		"--limit", "100",
		"--json", "number,title",
	)
	if err != nil {
		t.Logf("Warning: failed to list E2E issues in %s: %v", repo, err)
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
func closeAndDeleteIssue(t *testing.T, repo string, number int, title string) {
	t.Helper()

	t.Logf("Removing issue #%d: %s", number, title)

	// Close the issue
	if stdout, stderr, err := runGH("issue", "close",
		"--repo", repo,
		strconv.Itoa(number),
		"--reason", "not planned",
	); err != nil {
		t.Logf("  Warning: failed to close #%d: %v\n  Output: %s%s", number, err, stdout, stderr)
	}

	// Delete the issue (requires admin permissions)
	if _, _, err := runGH("issue", "delete",
		"--repo", repo,
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

// isOrphanProject reports whether p is a project an init test created under
// f's owner and failed to delete: init titles its projects after the
// repository, and the fixture's template project is never an orphan.
func isOrphanProject(f Fixture, p e2eProject) bool {
	return p.Title == f.RepoName() && p.Number != f.TemplateProject
}

// cleanupOrphanedProjects finds and deletes projects created by init E2E tests
// under f's owner, matched by title (#940). An owner whose template project is
// not recorded yet is left alone: without it there is nothing to exclude.
func cleanupOrphanedProjects(t *testing.T, f Fixture) {
	t.Helper()

	if f.TemplateProject == 0 {
		t.Logf("Skipping orphan cleanup for %s: template project not recorded", f.Owner)
		return
	}

	output, _, err := runGH("project", "list",
		"--owner", f.Owner,
		"--format", "json",
		"--limit", "100",
	)
	if err != nil {
		t.Logf("Warning: failed to list projects for %s: %v", f.Owner, err)
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
		if isOrphanProject(f, p) {
			orphans = append(orphans, p)
		}
	}

	if len(orphans) == 0 {
		t.Logf("No orphaned test projects found for %s", f.Owner)
		return
	}

	t.Logf("Found %d orphaned test projects to clean up for %s", len(orphans), f.Owner)
	for _, p := range orphans {
		t.Logf("Deleting orphaned project #%d: %s", p.Number, p.Title)
		deleteFixtureProject(t, f, p.Number)
	}
}

// runCleanupAfterTest is a helper that can be deferred in tests to clean up
// a specific user-fixture issue after the test completes.
func runCleanupAfterTest(t *testing.T, issueNum int) {
	t.Helper()
	runCleanupAfterTestIn(t, userFixture.Repo, issueNum)
}

// runCleanupAfterTestIn cleans up an issue a test created in repo.
func runCleanupAfterTestIn(t *testing.T, repo string, issueNum int) {
	t.Helper()

	if issueNum <= 0 {
		return
	}

	// Get issue title for logging
	output, _, _ := runGH("issue", "view",
		"--repo", repo,
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

	closeAndDeleteIssue(t, repo, issueNum, title)
}
