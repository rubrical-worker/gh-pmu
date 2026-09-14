//go:build integration

package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/testutil"
)

// TestRunIntake_Integration_NoUntracked tests the case when all issues are tracked
func TestRunIntake_Integration_NoUntracked(t *testing.T) {
	testutil.RequireTestEnv(t)

	// All seed issues are already in the project, so intake should show no untracked
	result := testutil.RunCommand(t, "intake", "--list")

	testutil.AssertExitCode(t, result, 0)
	// Should indicate all issues tracked (seed issues #1-6 are in project)
	// Note: If there are any untracked issues, this test may fail
}

// TestRunIntake_Integration_DryRun tests --dry-run shows what would be added
func TestRunIntake_Integration_DryRun(t *testing.T) {
	env := testutil.RequireTestEnv(t)

	// Create an issue NOT in the project (using gh directly, not gh pmu create)
	title := fmt.Sprintf("Test Issue - IntakeDryRun - %d", testUniqueID())
	cmd := exec.Command("gh", "issue", "create",
		"--repo", env.GetTestRepo(),
		"--title", title,
		"--body", "Test issue for intake dry-run",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to create test issue: %v\nOutput: %s", err, output)
	}
	issueNum := testutil.ExtractIssueNumber(t, string(output))
	defer testutil.DeleteTestIssue(t, issueNum)

	// Run intake --dry-run
	result := testutil.RunCommand(t, "intake", "--dry-run")

	testutil.AssertExitCode(t, result, 0)
	testutil.AssertContains(t, result.Stdout, "Would add")
	testutil.AssertContains(t, result.Stdout, fmt.Sprintf("#%d", issueNum))
}

// TestRunIntake_Integration_Apply tests --apply adds issues to project
func TestRunIntake_Integration_Apply(t *testing.T) {
	env := testutil.RequireTestEnv(t)

	// Create an issue NOT in the project
	title := fmt.Sprintf("Test Issue - IntakeApply - %d", testUniqueID())
	cmd := exec.Command("gh", "issue", "create",
		"--repo", env.GetTestRepo(),
		"--title", title,
		"--body", "Test issue for intake apply",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to create test issue: %v\nOutput: %s", err, output)
	}
	issueNum := testutil.ExtractIssueNumber(t, string(output))
	defer testutil.DeleteTestIssue(t, issueNum)

	// Run intake --apply
	result := testutil.RunCommand(t, "intake", "--apply", "")

	testutil.AssertExitCode(t, result, 0)
	testutil.AssertContains(t, result.Stdout, "Added")

	// Verify issue is now in project via view
	viewResult := testutil.RunCommand(t, "view", fmt.Sprintf("%d", issueNum))
	testutil.AssertExitCode(t, viewResult, 0)
	testutil.AssertContains(t, viewResult.Stdout, title)
}

// TestRunIntake_Integration_ApplyWithFields tests --apply with field values
func TestRunIntake_Integration_ApplyWithFields(t *testing.T) {
	env := testutil.RequireTestEnv(t)

	// Create an issue NOT in the project
	title := fmt.Sprintf("Test Issue - IntakeApplyFields - %d", testUniqueID())
	cmd := exec.Command("gh", "issue", "create",
		"--repo", env.GetTestRepo(),
		"--title", title,
		"--body", "Test issue for intake apply with fields",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to create test issue: %v\nOutput: %s", err, output)
	}
	issueNum := testutil.ExtractIssueNumber(t, string(output))
	defer testutil.DeleteTestIssue(t, issueNum)

	// Run intake --apply with specific fields
	result := testutil.RunCommand(t, "intake", "--apply", "status:in_progress,priority:p0")

	testutil.AssertExitCode(t, result, 0)
	testutil.AssertContains(t, result.Stdout, "Added")

	// Verify fields are set
	testutil.AssertIssueFields(t, issueNum, map[string]string{
		"status":   "In progress",
		"priority": "P0",
	})
}

// TestRunIntake_Integration_JSONOutput tests --json output format
func TestRunIntake_Integration_JSONOutput(t *testing.T) {
	env := testutil.RequireTestEnv(t)

	// Create an issue NOT in the project
	title := fmt.Sprintf("Test Issue - IntakeJSON - %d", testUniqueID())
	cmd := exec.Command("gh", "issue", "create",
		"--repo", env.GetTestRepo(),
		"--title", title,
		"--body", "Test issue for intake JSON",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to create test issue: %v\nOutput: %s", err, output)
	}
	issueNum := testutil.ExtractIssueNumber(t, string(output))
	defer testutil.DeleteTestIssue(t, issueNum)

	// Run intake --json
	result := testutil.RunCommand(t, "intake", "--list", "--json")

	testutil.AssertExitCode(t, result, 0)

	// Parse JSON output
	var jsonOutput struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
		Issues []struct {
			Number     int    `json:"number"`
			Title      string `json:"title"`
			Repository string `json:"repository"`
		} `json:"issues"`
	}

	err = json.Unmarshal([]byte(result.Stdout), &jsonOutput)
	if err != nil {
		t.Fatalf("failed to parse JSON: %v\nOutput: %s", err, result.Stdout)
	}

	if jsonOutput.Count == 0 {
		t.Error("expected at least one untracked issue in JSON output")
	}

	// Clean up by adding to project (so future tests don't see it)
	testutil.RunCommand(t, "intake", "--apply", "")
}

// TestRunIntake_Integration_JSONEmpty tests --json output when no untracked issues
func TestRunIntake_Integration_JSONEmpty(t *testing.T) {
	testutil.RequireTestEnv(t)

	// First ensure all issues are tracked
	testutil.RunCommand(t, "intake", "--apply", "")

	// Run intake --json
	result := testutil.RunCommand(t, "intake", "--list", "--json")

	testutil.AssertExitCode(t, result, 0)

	// Parse JSON output
	var jsonOutput struct {
		Count  int           `json:"count"`
		Issues []interface{} `json:"issues"`
	}

	err := json.Unmarshal([]byte(result.Stdout), &jsonOutput)
	if err != nil {
		t.Fatalf("failed to parse JSON: %v\nOutput: %s", err, result.Stdout)
	}

	// Count should be 0 and issues should be empty
	if jsonOutput.Count != 0 {
		t.Errorf("expected count 0, got %d", jsonOutput.Count)
	}
}

// TestRunIntake_Integration_ListUntracked tests listing untracked issues
func TestRunIntake_Integration_ListUntracked(t *testing.T) {
	env := testutil.RequireTestEnv(t)

	// Create an issue NOT in the project
	title := fmt.Sprintf("Test Issue - IntakeList - %d", testUniqueID())
	cmd := exec.Command("gh", "issue", "create",
		"--repo", env.GetTestRepo(),
		"--title", title,
		"--body", "Test issue for intake list",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to create test issue: %v\nOutput: %s", err, output)
	}
	issueNum := testutil.ExtractIssueNumber(t, string(output))
	defer testutil.DeleteTestIssue(t, issueNum)

	// Run intake (no flags - just list)
	result := testutil.RunCommand(t, "intake", "--list")

	testutil.AssertExitCode(t, result, 0)
	testutil.AssertContains(t, result.Stdout, "untracked issue")
	testutil.AssertContains(t, result.Stdout, fmt.Sprintf("#%d", issueNum))
	testutil.AssertContains(t, result.Stdout, "Use --apply")
}
