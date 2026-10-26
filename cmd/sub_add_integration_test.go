//go:build integration

package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/testutil"
)

// TestRunSubAdd_Integration_LinkExistingIssues tests linking existing issues as sub-issues
func TestRunSubAdd_Integration_LinkExistingIssues(t *testing.T) {
	testutil.RequireTestEnv(t)

	// Create parent issue
	parentTitle := fmt.Sprintf("Test SubAdd Parent - Link - %d", subAddTestID())
	parentResult := testutil.RunCommand(t, "create", "--title", parentTitle)
	testutil.AssertExitCode(t, parentResult, 0)

	parentNum := testutil.ExtractIssueNumber(t, parentResult.Stdout)
	defer testutil.DeleteTestIssue(t, parentNum)

	// Create child issue
	childTitle := fmt.Sprintf("Test SubAdd Child - Link - %d", subAddTestID())
	childResult := testutil.RunCommand(t, "create", "--title", childTitle)
	testutil.AssertExitCode(t, childResult, 0)

	childNum := testutil.ExtractIssueNumber(t, childResult.Stdout)
	defer testutil.DeleteTestIssue(t, childNum)

	// Link child as sub-issue of parent
	result := testutil.RunCommand(t, "sub", "add", fmt.Sprintf("%d", parentNum), fmt.Sprintf("%d", childNum))
	testutil.AssertExitCode(t, result, 0)

	// Verify success message
	testutil.AssertContains(t, result.Stdout, "Linked issue")
	testutil.AssertContains(t, result.Stdout, fmt.Sprintf("#%d", childNum))
	testutil.AssertContains(t, result.Stdout, fmt.Sprintf("#%d", parentNum))

	// Verify the link exists by listing sub-issues
	listResult := testutil.RunCommand(t, "sub", "list", fmt.Sprintf("%d", parentNum))
	testutil.AssertExitCode(t, listResult, 0)
	testutil.AssertContains(t, listResult.Stdout, childTitle)
}

// TestRunSubAdd_Integration_LinkWithHashPrefix tests linking with # prefix on issue numbers
func TestRunSubAdd_Integration_LinkWithHashPrefix(t *testing.T) {
	testutil.RequireTestEnv(t)

	// Create parent issue
	parentTitle := fmt.Sprintf("Test SubAdd Parent - Hash - %d", subAddTestID())
	parentResult := testutil.RunCommand(t, "create", "--title", parentTitle)
	testutil.AssertExitCode(t, parentResult, 0)

	parentNum := testutil.ExtractIssueNumber(t, parentResult.Stdout)
	defer testutil.DeleteTestIssue(t, parentNum)

	// Create child issue
	childTitle := fmt.Sprintf("Test SubAdd Child - Hash - %d", subAddTestID())
	childResult := testutil.RunCommand(t, "create", "--title", childTitle)
	testutil.AssertExitCode(t, childResult, 0)

	childNum := testutil.ExtractIssueNumber(t, childResult.Stdout)
	defer testutil.DeleteTestIssue(t, childNum)

	// Link using # prefix
	result := testutil.RunCommand(t, "sub", "add", fmt.Sprintf("#%d", parentNum), fmt.Sprintf("#%d", childNum))
	testutil.AssertExitCode(t, result, 0)

	// Verify success
	testutil.AssertContains(t, result.Stdout, "Linked issue")
}

// TestRunSubAdd_Integration_AlreadyLinkedError tests re-linking to the same
// parent: reported as already linked and exit 0 since #945 (name kept for history)
func TestRunSubAdd_Integration_AlreadyLinkedError(t *testing.T) {
	testutil.RequireTestEnv(t)

	// Create parent issue
	parentTitle := fmt.Sprintf("Test SubAdd Parent - Duplicate - %d", subAddTestID())
	parentResult := testutil.RunCommand(t, "create", "--title", parentTitle)
	testutil.AssertExitCode(t, parentResult, 0)

	parentNum := testutil.ExtractIssueNumber(t, parentResult.Stdout)
	defer testutil.DeleteTestIssue(t, parentNum)

	// Create child issue
	childTitle := fmt.Sprintf("Test SubAdd Child - Duplicate - %d", subAddTestID())
	childResult := testutil.RunCommand(t, "create", "--title", childTitle)
	testutil.AssertExitCode(t, childResult, 0)

	childNum := testutil.ExtractIssueNumber(t, childResult.Stdout)
	defer testutil.DeleteTestIssue(t, childNum)

	// Link child as sub-issue of parent (first time)
	result := testutil.RunCommand(t, "sub", "add", fmt.Sprintf("%d", parentNum), fmt.Sprintf("%d", childNum))
	testutil.AssertExitCode(t, result, 0)

	// Link again to the same parent: since #945 that is the outcome asked for,
	// reported as already linked, not a failure.
	duplicateResult := testutil.RunCommand(t, "sub", "add", fmt.Sprintf("%d", parentNum), fmt.Sprintf("%d", childNum))

	testutil.AssertExitCode(t, duplicateResult, 0)
	testutil.AssertContains(t, duplicateResult.Stdout, "already a sub-issue")
}

// TestRunSubAdd_Integration_ParentNotFound tests error when parent issue doesn't exist
func TestRunSubAdd_Integration_ParentNotFound(t *testing.T) {
	testutil.RequireTestEnv(t)

	// Create a valid child issue
	childTitle := fmt.Sprintf("Test SubAdd Child - ParentNotFound - %d", subAddTestID())
	childResult := testutil.RunCommand(t, "create", "--title", childTitle)
	testutil.AssertExitCode(t, childResult, 0)

	childNum := testutil.ExtractIssueNumber(t, childResult.Stdout)
	defer testutil.DeleteTestIssue(t, childNum)

	// Try to link to a non-existent parent
	result := testutil.RunCommand(t, "sub", "add", "999999", fmt.Sprintf("%d", childNum))

	// Should fail
	if result.ExitCode == 0 {
		t.Error("Expected non-zero exit code for non-existent parent")
	}

	testutil.AssertContains(t, result.Stderr, "failed to get parent issue")
}

// TestRunSubAdd_Integration_ChildNotFound tests error when child issue doesn't exist
func TestRunSubAdd_Integration_ChildNotFound(t *testing.T) {
	testutil.RequireTestEnv(t)

	// Create a valid parent issue
	parentTitle := fmt.Sprintf("Test SubAdd Parent - ChildNotFound - %d", subAddTestID())
	parentResult := testutil.RunCommand(t, "create", "--title", parentTitle)
	testutil.AssertExitCode(t, parentResult, 0)

	parentNum := testutil.ExtractIssueNumber(t, parentResult.Stdout)
	defer testutil.DeleteTestIssue(t, parentNum)

	// Try to link a non-existent child
	result := testutil.RunCommand(t, "sub", "add", fmt.Sprintf("%d", parentNum), "999999")

	// Should fail
	if result.ExitCode == 0 {
		t.Error("Expected non-zero exit code for non-existent child")
	}

	testutil.AssertContains(t, result.Stderr, "failed to get child issue")
}

// TestRunSubAdd_Integration_MultipleLinks tests linking multiple children to same parent
func TestRunSubAdd_Integration_MultipleLinks(t *testing.T) {
	testutil.RequireTestEnv(t)

	// Create parent issue
	parentTitle := fmt.Sprintf("Test SubAdd Parent - Multiple - %d", subAddTestID())
	parentResult := testutil.RunCommand(t, "create", "--title", parentTitle)
	testutil.AssertExitCode(t, parentResult, 0)

	parentNum := testutil.ExtractIssueNumber(t, parentResult.Stdout)
	defer testutil.DeleteTestIssue(t, parentNum)

	// Create first child issue
	child1Title := fmt.Sprintf("Test SubAdd Child1 - Multiple - %d", subAddTestID())
	child1Result := testutil.RunCommand(t, "create", "--title", child1Title)
	testutil.AssertExitCode(t, child1Result, 0)

	child1Num := testutil.ExtractIssueNumber(t, child1Result.Stdout)
	defer testutil.DeleteTestIssue(t, child1Num)

	// Create second child issue
	child2Title := fmt.Sprintf("Test SubAdd Child2 - Multiple - %d", subAddTestID())
	child2Result := testutil.RunCommand(t, "create", "--title", child2Title)
	testutil.AssertExitCode(t, child2Result, 0)

	child2Num := testutil.ExtractIssueNumber(t, child2Result.Stdout)
	defer testutil.DeleteTestIssue(t, child2Num)

	// Link first child
	result1 := testutil.RunCommand(t, "sub", "add", fmt.Sprintf("%d", parentNum), fmt.Sprintf("%d", child1Num))
	testutil.AssertExitCode(t, result1, 0)

	// Link second child
	result2 := testutil.RunCommand(t, "sub", "add", fmt.Sprintf("%d", parentNum), fmt.Sprintf("%d", child2Num))
	testutil.AssertExitCode(t, result2, 0)

	// Verify both children are linked
	listResult := testutil.RunCommand(t, "sub", "list", fmt.Sprintf("%d", parentNum))
	testutil.AssertExitCode(t, listResult, 0)
	testutil.AssertContains(t, listResult.Stdout, child1Title)
	testutil.AssertContains(t, listResult.Stdout, child2Title)
	testutil.AssertContains(t, listResult.Stdout, "0/2 complete")
}

// TestRunSubAdd_Integration_OutputShowsTitles tests that output shows issue titles
func TestRunSubAdd_Integration_OutputShowsTitles(t *testing.T) {
	testutil.RequireTestEnv(t)

	// Create parent issue
	parentTitle := fmt.Sprintf("Test SubAdd Parent - Titles - %d", subAddTestID())
	parentResult := testutil.RunCommand(t, "create", "--title", parentTitle)
	testutil.AssertExitCode(t, parentResult, 0)

	parentNum := testutil.ExtractIssueNumber(t, parentResult.Stdout)
	defer testutil.DeleteTestIssue(t, parentNum)

	// Create child issue
	childTitle := fmt.Sprintf("Test SubAdd Child - Titles - %d", subAddTestID())
	childResult := testutil.RunCommand(t, "create", "--title", childTitle)
	testutil.AssertExitCode(t, childResult, 0)

	childNum := testutil.ExtractIssueNumber(t, childResult.Stdout)
	defer testutil.DeleteTestIssue(t, childNum)

	// Link child as sub-issue
	result := testutil.RunCommand(t, "sub", "add", fmt.Sprintf("%d", parentNum), fmt.Sprintf("%d", childNum))
	testutil.AssertExitCode(t, result, 0)

	// Verify output shows both titles
	testutil.AssertContains(t, result.Stdout, "Parent:")
	testutil.AssertContains(t, result.Stdout, parentTitle)
	testutil.AssertContains(t, result.Stdout, "Child:")
	testutil.AssertContains(t, result.Stdout, childTitle)
}

// subAddTestID returns a unique identifier for sub add test issues
var subAddTestCounter int

func subAddTestID() int {
	subAddTestCounter++
	return subAddTestCounter + int(strings.Count("sub-add-integration", "a")*1000000)
}
