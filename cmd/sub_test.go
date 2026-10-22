package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
)

func TestSubCommand_Exists(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub command should exist: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("sub")) {
		t.Error("Expected help output to mention 'sub'")
	}
}

func TestSubAddCommand_Exists(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "add", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub add command should exist: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("add")) {
		t.Error("Expected help output to mention 'add'")
	}
}

func TestSubAddCommand_RequiresTwoArgs(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "add", "123"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when only one argument provided")
	}
}

func TestSubAddCommand_RequiresParentAndChild(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "add"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when no arguments provided")
	}
}

func TestSubCreateCommand_Exists(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "create", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub create command should exist: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("create")) {
		t.Error("Expected help output to mention 'create'")
	}
}

func TestSubCreateCommand_HasParentFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "create"})
	if err != nil {
		t.Fatalf("sub create command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("parent")
	if flag == nil {
		t.Fatal("Expected --parent flag to exist")
	}
}

func TestSubCreateCommand_HasTitleFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "create"})
	if err != nil {
		t.Fatalf("sub create command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("title")
	if flag == nil {
		t.Fatal("Expected --title flag to exist")
	}
}

func TestSubCreateCommand_RequiresParentFlag(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "create", "--title", "Test"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when --parent not provided")
	}
}

func TestSubCreateCommand_RequiresTitleFlag(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "create", "--parent", "123"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when --title not provided")
	}
}

// TestSubCreate_DryRunWritesNothing: --dry-run resolves the parent and prints
// the plan without sending CreateIssue or AddSubIssue (#929). Offline: the
// scenario harness redirects every request to a local mock server.
func TestSubCreate_DryRunWritesNothing(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetIssue", gqlData(map[string]interface{}{
		"repository": map[string]interface{}{
			"issue": map[string]interface{}{
				"id": "parent-node", "number": 10, "title": "Parent epic", "state": "OPEN",
				"url":       "https://github.com/test-org/test-repo/issues/10",
				"author":    map[string]interface{}{"login": "someone"},
				"assignees": map[string]interface{}{"nodes": []interface{}{}},
				"labels":    map[string]interface{}{"nodes": []interface{}{}},
				"milestone": map[string]interface{}{"title": ""},
			},
		},
	}))
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubCreateCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--parent", "10", "--title", "Dry child", "--label", "task", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("sub create --dry-run: unexpected error: %v\n%s", err, buf.String())
	}

	for _, op := range []string{"CreateIssue", "AddSubIssue"} {
		if n := len(handler.requestsFor(op)); n != 0 {
			t.Errorf("dry run sent %d %s mutation(s); ops: %v", n, op, handler.operationNames())
		}
	}
	for _, want := range []string{"Dry run", "test-org/test-repo", "Parent epic", "Dry child", "task"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("expected %q in dry-run output, got:\n%s", want, buf.String())
		}
	}
}

// The sub-issue exists and is linked once created; failing to add it to
// --project printed a warning and exited 0. It is a partial success, and
// re-running would create a duplicate (#938).
func TestSubCreate_ProjectAddFailureIsPartialSuccess(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondToFunc("GetIssue", issueByNumberFixture(map[int]string{10: "Parent epic"}))
	handler.respondTo("GetRepositoryID", gqlData(map[string]interface{}{
		"repository": map[string]interface{}{"id": "repo-node-1"},
	}))
	handler.respondTo("CreateIssue", gqlData(map[string]interface{}{
		"createIssue": map[string]interface{}{
			"issue": map[string]interface{}{
				"id": "issue-node-new", "number": 21, "title": "Child", "body": "", "state": "OPEN",
				"url": "https://github.com/test-org/test-repo/issues/21",
			},
		},
	}))
	handler.respondTo("AddSubIssue", gqlData(map[string]interface{}{
		"addSubIssue": map[string]interface{}{
			"issue":    map[string]interface{}{"id": "issue-10"},
			"subIssue": map[string]interface{}{"id": "issue-node-new"},
		},
	}))
	projectMissing := map[string]interface{}{"errors": []interface{}{map[string]interface{}{"message": "Could not resolve to a ProjectV2 with the number 5."}}}
	handler.respondTo("GetUserProject", projectMissing)
	handler.respondTo("GetOrgProject", projectMissing)

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubCreateCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := runSubCreate(cmd, &subCreateOptions{parent: "10", title: "Child", project: 5})
	if !api.IsPartialSuccess(err) {
		t.Fatalf("expected a partial-success error when adding to the project fails, got: %v (output %q)", err, buf.String())
	}
	if !strings.Contains(err.Error(), "#21") || !strings.Contains(err.Error(), "project 5") {
		t.Errorf("error should name the created issue and the project step, got: %v", err)
	}
}

func TestSubListCommand_Exists(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "list", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub list command should exist: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("list")) {
		t.Error("Expected help output to mention 'list'")
	}
}

func TestSubListCommand_RequiresParentArg(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "list"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when parent issue not provided")
	}
}

func TestSubListCommand_HasJSONFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "list"})
	if err != nil {
		t.Fatalf("sub list command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("json")
	if flag == nil {
		t.Fatal("Expected --json flag to exist")
	}
}

func TestSubRemoveCommand_Exists(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "remove", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub remove command should exist: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("remove")) {
		t.Error("Expected help output to mention 'remove'")
	}
}

func TestSubRemoveCommand_RequiresTwoArgs(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "remove", "123"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when only one argument provided")
	}
}

func TestSubRemoveCommand_RequiresParentAndChild(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "remove"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when no arguments provided")
	}
}

// Cross-repository sub-issue tests

func TestSubCreateCommand_HasRepoFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "create"})
	if err != nil {
		t.Fatalf("sub create command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("repo")
	if flag == nil {
		t.Fatal("Expected --repo flag to exist")
	}

	// Verify short flag
	if flag.Shorthand != "R" {
		t.Errorf("Expected --repo shorthand to be 'R', got '%s'", flag.Shorthand)
	}
}

func TestSubCreateCommand_RepoFlagHelpText(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "create", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub create help failed: %v", err)
	}

	output := buf.String()
	// Verify cross-repo example is shown
	if !bytes.Contains([]byte(output), []byte("--repo owner/repo2")) {
		t.Error("Expected help to show cross-repo example with --repo flag")
	}
}

func TestSubAddCommand_HelpShowsCrossRepoExample(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "add", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub add help failed: %v", err)
	}

	output := buf.String()
	// Verify cross-repo format is documented
	if !bytes.Contains([]byte(output), []byte("owner/repo#")) {
		t.Error("Expected help to document owner/repo#number format for cross-repo")
	}
}

func TestSubAddCommand_HasRepoFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "add"})
	if err != nil {
		t.Fatalf("sub add command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("repo")
	if flag == nil {
		t.Fatal("Expected --repo flag to exist")
	}

	// Verify short flag
	if flag.Shorthand != "R" {
		t.Errorf("Expected --repo shorthand to be 'R', got '%s'", flag.Shorthand)
	}
}

func TestSubAddCommand_AcceptsURLs(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "add", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub add help failed: %v", err)
	}

	output := buf.String()
	// Verify URL format is documented
	if !bytes.Contains([]byte(output), []byte("https://github.com/")) {
		t.Error("Expected help to document GitHub URL format")
	}
}

// Additional newSubAddCommand Flag Tests (IT-3.2)

func TestSubAddCommand_TooManyArgs(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "add", "1", "2", "3"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when too many arguments provided")
	}
}

func TestSubAddCommand_RepoFlagType(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "add"})
	if err != nil {
		t.Fatalf("sub add command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("repo")
	if flag == nil {
		t.Fatal("Expected --repo flag to exist")
	}

	// Verify it's a string flag
	if flag.Value.Type() != "string" {
		t.Errorf("Expected --repo to be string, got %s", flag.Value.Type())
	}
}

func TestSubAddCommand_RepoFlagInHelp(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "add", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub add --help failed: %v", err)
	}

	output := buf.String()
	// Verify --repo is documented
	if !strings.Contains(output, "--repo") {
		t.Error("Expected help to mention --repo flag")
	}
	// Verify shorthand is documented
	if !strings.Contains(output, "-R") {
		t.Error("Expected help to mention -R shorthand")
	}
	// Verify owner/repo format is mentioned
	if !strings.Contains(output, "owner/repo") {
		t.Error("Expected help to mention owner/repo format")
	}
}

// ============================================================================
// subCreateOptions Tests
// ============================================================================

func TestSubCreateOptions_Defaults(t *testing.T) {
	// Get the command to verify default values
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "create"})
	if err != nil {
		t.Fatalf("sub create command not found: %v", err)
	}

	// Check inherit-labels default (should be false - sub-issues should not inherit parent labels)
	inheritLabels := subCmd.Flags().Lookup("inherit-labels")
	if inheritLabels == nil {
		t.Fatal("Expected --inherit-labels flag to exist")
	}
	if inheritLabels.DefValue != "false" {
		t.Errorf("Expected --inherit-labels default to be 'false', got '%s'", inheritLabels.DefValue)
	}

	// Check inherit-assignees default (should be false)
	inheritAssign := subCmd.Flags().Lookup("inherit-assignees")
	if inheritAssign == nil {
		t.Fatal("Expected --inherit-assignees flag to exist")
	}
	if inheritAssign.DefValue != "false" {
		t.Errorf("Expected --inherit-assignees default to be 'false', got '%s'", inheritAssign.DefValue)
	}

	// Check inherit-milestone default (should be true)
	inheritMilestone := subCmd.Flags().Lookup("inherit-milestone")
	if inheritMilestone == nil {
		t.Fatal("Expected --inherit-milestone flag to exist")
	}
	if inheritMilestone.DefValue != "true" {
		t.Errorf("Expected --inherit-milestone default to be 'true', got '%s'", inheritMilestone.DefValue)
	}
}

// #867 finding 2: --inherit-assignees / --inherit-milestone were defined but
// never read, so parent assignees/milestone were never inherited despite the
// advertised (and default-true for milestone) behavior. These tests exercise the
// extracted resolution helpers that runSubCreate now wires in.

func TestResolveInheritedAssignees(t *testing.T) {
	parent := []api.Actor{{Login: "alice"}, {Login: "bob"}}

	tests := []struct {
		name        string
		explicit    []string
		inherit     bool
		isCrossRepo bool
		expected    []string
	}{
		{"inherit off keeps explicit only", []string{"carol"}, false, false, []string{"carol"}},
		{"inherit on merges parent", []string{"carol"}, true, false, []string{"carol", "alice", "bob"}},
		{"inherit on cross-repo skips parent", []string{"carol"}, true, true, []string{"carol"}},
		{"inherit on no explicit", nil, true, false, []string{"alice", "bob"}},
		{"dedupes overlap", []string{"alice"}, true, false, []string{"alice", "bob"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveInheritedAssignees(tt.explicit, parent, tt.inherit, tt.isCrossRepo)
			if len(got) != len(tt.expected) {
				t.Fatalf("resolveInheritedAssignees() = %v, want %v", got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("resolveInheritedAssignees()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestResolveInheritedMilestone(t *testing.T) {
	parent := &api.Milestone{Title: "Sprint 5"}

	tests := []struct {
		name        string
		explicit    string
		parent      *api.Milestone
		inherit     bool
		isCrossRepo bool
		expected    string
	}{
		{"explicit wins", "Sprint 9", parent, true, false, "Sprint 9"},
		{"inherit parent when empty", "", parent, true, false, "Sprint 5"},
		{"inherit off yields empty", "", parent, false, false, ""},
		{"cross-repo skips parent", "", parent, true, true, ""},
		{"nil parent yields empty", "", nil, true, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveInheritedMilestone(tt.explicit, tt.parent, tt.inherit, tt.isCrossRepo)
			if got != tt.expected {
				t.Errorf("resolveInheritedMilestone() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSubRemoveBatchError(t *testing.T) {
	// #871 finding 5: batch `sub remove` previously returned nil on partial failure
	// (error only when successCount == 0), inconsistent with the single-child path.
	// Any failure must now yield a non-nil error.
	tests := []struct {
		name                 string
		success, fail, total int
		wantErr              bool
	}{
		{"all succeeded", 3, 0, 3, false},
		{"partial failure", 2, 1, 3, true},
		{"all failed", 0, 3, 3, true},
		{"none processed", 0, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := subRemoveBatchError(tt.success, tt.fail, tt.total)
			if (err != nil) != tt.wantErr {
				t.Errorf("subRemoveBatchError(%d,%d,%d) err=%v, wantErr=%v", tt.success, tt.fail, tt.total, err, tt.wantErr)
			}
		})
	}
}

func TestSubCreateCommand_HasBodyFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "create"})
	if err != nil {
		t.Fatalf("sub create command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("body")
	if flag == nil {
		t.Fatal("Expected --body flag to exist")
	}

	if flag.Shorthand != "b" {
		t.Errorf("Expected --body shorthand to be 'b', got '%s'", flag.Shorthand)
	}
}

func TestSubCreateCommand_HasBodyFileFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "create"})
	if err != nil {
		t.Fatalf("sub create command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("body-file")
	if flag == nil {
		t.Fatal("Expected --body-file flag to exist")
	}

	if flag.Shorthand != "F" {
		t.Errorf("Expected --body-file shorthand to be 'F', got '%s'", flag.Shorthand)
	}
}

func TestSubCreate_BodyAndBodyFileMutualExclusivity(t *testing.T) {
	// Create temp config
	config := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"]}`
	dir := createTempConfig(t, config)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create temp body file
	tmpfile, err := os.CreateTemp("", "body-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	_, _ = tmpfile.WriteString("Body from file")
	_ = tmpfile.Close()
	defer os.Remove(tmpfile.Name())

	cmd := NewRootCommand()
	cmd.SetArgs([]string{
		"sub", "create",
		"--parent", "1",
		"--title", "Test",
		"--body", "inline body",
		"--body-file", tmpfile.Name(),
	})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err = cmd.Execute()

	if err == nil {
		t.Fatal("Expected error when using --body and --body-file together")
	}
	if !strings.Contains(err.Error(), "cannot use --body and --body-file together") {
		t.Errorf("Expected mutual exclusivity error, got: %v", err)
	}
}

func TestSubCreate_BodyFileNotFound(t *testing.T) {
	// Create temp config
	config := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"]}`
	dir := createTempConfig(t, config)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{
		"sub", "create",
		"--parent", "1",
		"--title", "Test",
		"--body-file", "nonexistent-file-that-does-not-exist.md",
	})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()

	if err == nil {
		t.Fatal("Expected error when body file doesn't exist")
	}
	if !strings.Contains(err.Error(), "failed to read body file") {
		t.Errorf("Expected 'failed to read body file' error, got: %v", err)
	}
}

// ============================================================================
// outputSubListJSON Tests
// ============================================================================

func TestOutputSubListJSON_EmptyList(t *testing.T) {
	parent := &api.Issue{
		Number: 10,
		Title:  "Parent Issue",
	}
	subIssues := []api.SubIssue{}

	var buf bytes.Buffer
	err := outputSubListJSON(&buf, subIssues, parent)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := buf.Bytes()

	var result SubListJSONOutput
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	if result.Parent.Number != 10 {
		t.Errorf("Expected parent number 10, got %d", result.Parent.Number)
	}
	if result.Parent.Title != "Parent Issue" {
		t.Errorf("Expected parent title 'Parent Issue', got '%s'", result.Parent.Title)
	}
	if result.Summary.Total != 0 {
		t.Errorf("Expected total 0, got %d", result.Summary.Total)
	}
	if result.Summary.Open != 0 {
		t.Errorf("Expected open 0, got %d", result.Summary.Open)
	}
	if result.Summary.Closed != 0 {
		t.Errorf("Expected closed 0, got %d", result.Summary.Closed)
	}
	if len(result.SubIssues) != 0 {
		t.Errorf("Expected 0 sub-issues, got %d", len(result.SubIssues))
	}
}

func TestOutputSubListJSON_SummaryCounts(t *testing.T) {
	parent := &api.Issue{
		Number: 10,
		Title:  "Parent Issue",
	}
	subIssues := []api.SubIssue{
		{Number: 1, Title: "Open 1", State: "OPEN", Repository: api.Repository{Owner: "owner", Name: "repo"}},
		{Number: 2, Title: "Open 2", State: "OPEN", Repository: api.Repository{Owner: "owner", Name: "repo"}},
		{Number: 3, Title: "Closed 1", State: "CLOSED", Repository: api.Repository{Owner: "owner", Name: "repo"}},
	}

	var buf bytes.Buffer
	err := outputSubListJSON(&buf, subIssues, parent)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := buf.Bytes()

	var result SubListJSONOutput
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Verify summary counts match acceptance criteria
	if result.Summary.Total != 3 {
		t.Errorf("Expected total=3, got %d", result.Summary.Total)
	}
	if result.Summary.Open != 2 {
		t.Errorf("Expected open=2, got %d", result.Summary.Open)
	}
	if result.Summary.Closed != 1 {
		t.Errorf("Expected closed=1, got %d", result.Summary.Closed)
	}
}

func TestOutputSubListJSON_RepositoryField(t *testing.T) {
	parent := &api.Issue{
		Number: 10,
		Title:  "Parent Issue",
	}
	subIssues := []api.SubIssue{
		{
			Number:     1,
			Title:      "Sub in other repo",
			State:      "OPEN",
			URL:        "https://github.com/other/repo/issues/1",
			Repository: api.Repository{Owner: "other", Name: "repo"},
		},
		{
			Number:     2,
			Title:      "Sub with empty repo",
			State:      "OPEN",
			URL:        "https://github.com/owner/repo/issues/2",
			Repository: api.Repository{Owner: "", Name: ""},
		},
	}

	var buf bytes.Buffer
	err := outputSubListJSON(&buf, subIssues, parent)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := buf.Bytes()

	var result SubListJSONOutput
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	if len(result.SubIssues) != 2 {
		t.Fatalf("Expected 2 sub-issues, got %d", len(result.SubIssues))
	}

	// First sub-issue should have full repo
	if result.SubIssues[0].Repository != "other/repo" {
		t.Errorf("Expected repository 'other/repo', got '%s'", result.SubIssues[0].Repository)
	}

	// Second sub-issue with empty repo should have empty string
	if result.SubIssues[1].Repository != "" {
		t.Errorf("Expected empty repository, got '%s'", result.SubIssues[1].Repository)
	}
}

func TestOutputSubListJSON_AllFields(t *testing.T) {
	parent := &api.Issue{
		Number: 42,
		Title:  "Epic Issue",
	}
	subIssues := []api.SubIssue{
		{
			Number:     100,
			Title:      "Task One",
			State:      "OPEN",
			URL:        "https://github.com/owner/repo/issues/100",
			Repository: api.Repository{Owner: "owner", Name: "repo"},
		},
	}

	var buf bytes.Buffer
	err := outputSubListJSON(&buf, subIssues, parent)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := buf.Bytes()

	var result SubListJSONOutput
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Verify all fields are present
	sub := result.SubIssues[0]
	if sub.Number != 100 {
		t.Errorf("Expected number 100, got %d", sub.Number)
	}
	if sub.Title != "Task One" {
		t.Errorf("Expected title 'Task One', got '%s'", sub.Title)
	}
	if sub.State != "OPEN" {
		t.Errorf("Expected state 'OPEN', got '%s'", sub.State)
	}
	if sub.URL != "https://github.com/owner/repo/issues/100" {
		t.Errorf("Expected URL 'https://github.com/owner/repo/issues/100', got '%s'", sub.URL)
	}
	if sub.Repository != "owner/repo" {
		t.Errorf("Expected repository 'owner/repo', got '%s'", sub.Repository)
	}
}

// ============================================================================
// outputSubListTable Tests
// ============================================================================

func TestOutputSubListTable_EmptyList(t *testing.T) {
	parent := &api.Issue{
		Number: 10,
		Title:  "Parent Issue",
		Repository: api.Repository{
			Owner: "owner",
			Name:  "repo",
		},
	}
	subIssues := []api.SubIssue{}

	var buf bytes.Buffer
	err := outputSubListTable(&buf, subIssues, parent)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	outputStr := buf.String()

	if !strings.Contains(outputStr, "No sub-issues found") {
		t.Error("Expected 'No sub-issues found' message for empty list")
	}
}

func TestOutputSubListTable_SingleRepo(t *testing.T) {
	parent := &api.Issue{
		Number: 10,
		Title:  "Parent Issue",
		Repository: api.Repository{
			Owner: "owner",
			Name:  "repo",
		},
	}
	subIssues := []api.SubIssue{
		{Number: 1, Title: "Task 1", State: "OPEN", Repository: api.Repository{Owner: "owner", Name: "repo"}},
		{Number: 2, Title: "Task 2", State: "CLOSED", Repository: api.Repository{Owner: "owner", Name: "repo"}},
	}

	var buf bytes.Buffer
	err := outputSubListTable(&buf, subIssues, parent)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	outputStr := buf.String()

	// Should show parent info
	if !strings.Contains(outputStr, "#10") {
		t.Error("Expected parent issue number in output")
	}
	if !strings.Contains(outputStr, "Parent Issue") {
		t.Error("Expected parent title in output")
	}

	// Should show sub-issues without repo prefix (same repo)
	if !strings.Contains(outputStr, "#1") {
		t.Error("Expected sub-issue #1 in output")
	}
	if !strings.Contains(outputStr, "#2") {
		t.Error("Expected sub-issue #2 in output")
	}

	// Should show progress
	if !strings.Contains(outputStr, "1/2 complete") {
		t.Error("Expected progress '1/2 complete' in output")
	}

	// Check state indicators
	if !strings.Contains(outputStr, "[ ]") {
		t.Error("Expected open indicator '[ ]' in output")
	}
	if !strings.Contains(outputStr, "[x]") {
		t.Error("Expected closed indicator '[x]' in output")
	}
}

func TestOutputSubListTable_CrossRepo(t *testing.T) {
	parent := &api.Issue{
		Number: 10,
		Title:  "Parent Issue",
		Repository: api.Repository{
			Owner: "owner",
			Name:  "repo",
		},
	}
	subIssues := []api.SubIssue{
		{Number: 1, Title: "Same repo task", State: "OPEN", Repository: api.Repository{Owner: "owner", Name: "repo"}},
		{Number: 100, Title: "Cross repo task", State: "OPEN", Repository: api.Repository{Owner: "other", Name: "project"}},
	}

	var buf bytes.Buffer
	err := outputSubListTable(&buf, subIssues, parent)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	outputStr := buf.String()

	// Should show repo info for cross-repo sub-issues
	if !strings.Contains(outputStr, "other/project#100") {
		t.Error("Expected cross-repo sub-issue to show 'other/project#100'")
	}

	// Should also show repo for same-repo when there are cross-repo issues
	if !strings.Contains(outputStr, "owner/repo#1") {
		t.Error("Expected same-repo sub-issue to show 'owner/repo#1' when cross-repo exists")
	}
}

func TestOutputSubListTable_ProgressCalculation(t *testing.T) {
	parent := &api.Issue{
		Number:     10,
		Title:      "Parent Issue",
		Repository: api.Repository{Owner: "owner", Name: "repo"},
	}

	tests := []struct {
		name     string
		subs     []api.SubIssue
		expected string
	}{
		{
			name: "all open",
			subs: []api.SubIssue{
				{Number: 1, State: "OPEN", Repository: api.Repository{Owner: "owner", Name: "repo"}},
				{Number: 2, State: "OPEN", Repository: api.Repository{Owner: "owner", Name: "repo"}},
				{Number: 3, State: "OPEN", Repository: api.Repository{Owner: "owner", Name: "repo"}},
			},
			expected: "0/3 complete",
		},
		{
			name: "all closed",
			subs: []api.SubIssue{
				{Number: 1, State: "CLOSED", Repository: api.Repository{Owner: "owner", Name: "repo"}},
				{Number: 2, State: "CLOSED", Repository: api.Repository{Owner: "owner", Name: "repo"}},
			},
			expected: "2/2 complete",
		},
		{
			name: "mixed",
			subs: []api.SubIssue{
				{Number: 1, State: "CLOSED", Repository: api.Repository{Owner: "owner", Name: "repo"}},
				{Number: 2, State: "OPEN", Repository: api.Repository{Owner: "owner", Name: "repo"}},
				{Number: 3, State: "CLOSED", Repository: api.Repository{Owner: "owner", Name: "repo"}},
				{Number: 4, State: "OPEN", Repository: api.Repository{Owner: "owner", Name: "repo"}},
			},
			expected: "2/4 complete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := outputSubListTable(&buf, tt.subs, parent)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			outputStr := buf.String()

			if !strings.Contains(outputStr, tt.expected) {
				t.Errorf("Expected progress '%s' in output, got: %s", tt.expected, outputStr)
			}
		})
	}
}

// ============================================================================
// Additional Command Flag Tests
// ============================================================================

func TestSubCreateCommand_FlagShorthands(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "create"})
	if err != nil {
		t.Fatalf("sub create command not found: %v", err)
	}

	tests := []struct {
		flag      string
		shorthand string
	}{
		{"parent", "p"},
		{"title", "t"},
		{"body", "b"},
		{"repo", "R"},
		{"label", "l"},
		{"assignee", "a"},
		{"milestone", "m"},
	}

	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			flag := subCmd.Flags().Lookup(tt.flag)
			if flag == nil {
				t.Fatalf("Expected --%s flag to exist", tt.flag)
			}
			if flag.Shorthand != tt.shorthand {
				t.Errorf("Expected --%s shorthand to be '%s', got '%s'", tt.flag, tt.shorthand, flag.Shorthand)
			}
		})
	}
}

func TestSubCreateCommand_HasProjectFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "create"})
	if err != nil {
		t.Fatalf("sub create command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("project")
	if flag == nil {
		t.Fatal("Expected --project flag to exist")
	}
}

func TestSubCreateCommand_HasLabelFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "create"})
	if err != nil {
		t.Fatalf("sub create command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("label")
	if flag == nil {
		t.Fatal("Expected --label flag to exist")
	}
	if flag.Shorthand != "l" {
		t.Errorf("Expected --label shorthand 'l', got '%s'", flag.Shorthand)
	}
}

func TestSubCreateCommand_HasAssigneeFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "create"})
	if err != nil {
		t.Fatalf("sub create command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("assignee")
	if flag == nil {
		t.Fatal("Expected --assignee flag to exist")
	}
	if flag.Shorthand != "a" {
		t.Errorf("Expected --assignee shorthand 'a', got '%s'", flag.Shorthand)
	}
}

func TestSubCreateCommand_HasMilestoneFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "create"})
	if err != nil {
		t.Fatalf("sub create command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("milestone")
	if flag == nil {
		t.Fatal("Expected --milestone flag to exist")
	}
	if flag.Shorthand != "m" {
		t.Errorf("Expected --milestone shorthand 'm', got '%s'", flag.Shorthand)
	}
}

// ============================================================================
// Sub List Command Enhancement Tests (#118-120, #124)
// ============================================================================

func TestSubListCommand_HasStateFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "list"})
	if err != nil {
		t.Fatalf("sub list command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("state")
	if flag == nil {
		t.Fatal("Expected --state flag to exist")
	}
	if flag.Shorthand != "s" {
		t.Errorf("Expected --state shorthand 's', got '%s'", flag.Shorthand)
	}
	if flag.DefValue != "all" {
		t.Errorf("Expected --state default 'all', got '%s'", flag.DefValue)
	}
}

func TestSubListCommand_HasLimitFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "list"})
	if err != nil {
		t.Fatalf("sub list command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("limit")
	if flag == nil {
		t.Fatal("Expected --limit flag to exist")
	}
	if flag.Shorthand != "n" {
		t.Errorf("Expected --limit shorthand 'n', got '%s'", flag.Shorthand)
	}
	if flag.DefValue != "0" {
		t.Errorf("Expected --limit default '0', got '%s'", flag.DefValue)
	}
}

func TestSubListCommand_HasRelationFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "list"})
	if err != nil {
		t.Fatalf("sub list command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("relation")
	if flag == nil {
		t.Fatal("Expected --relation flag to exist")
	}
	if flag.DefValue != "children" {
		t.Errorf("Expected --relation default 'children', got '%s'", flag.DefValue)
	}
}

func TestSubListCommand_HelpDocumentsNewFlags(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "list", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub list help failed: %v", err)
	}

	output := buf.String()

	// Check that new flags are documented
	tests := []string{
		"-s, --state",
		"-n, --limit",
		"--relation",
		"open",
		"closed",
		"parent",
		"siblings",
	}

	for _, expected := range tests {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected help to contain '%s'", expected)
		}
	}
}

func TestSubListCommand_HasRepoFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "list"})
	if err != nil {
		t.Fatalf("sub list command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("repo")
	if flag == nil {
		t.Fatal("Expected --repo flag to exist")
	}

	// Verify short flag
	if flag.Shorthand != "R" {
		t.Errorf("Expected --repo shorthand to be 'R', got '%s'", flag.Shorthand)
	}

	// Verify it's a string flag
	if flag.Value.Type() != "string" {
		t.Errorf("Expected --repo to be string, got %s", flag.Value.Type())
	}
}

func TestSubListCommand_RepoFlagInHelp(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "list", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub list --help failed: %v", err)
	}

	output := buf.String()

	// Verify --repo is documented
	if !strings.Contains(output, "--repo") {
		t.Error("Expected help to mention --repo flag")
	}
	// Verify shorthand is documented
	if !strings.Contains(output, "-R") {
		t.Error("Expected help to mention -R shorthand")
	}
	// Verify owner/repo format is mentioned
	if !strings.Contains(output, "owner/repo") {
		t.Error("Expected help to mention owner/repo format")
	}
}

// ============================================================================
// filterSubIssuesByState Tests
// ============================================================================

func TestFilterSubIssuesByState_All(t *testing.T) {
	subIssues := []api.SubIssue{
		{Number: 1, State: "OPEN"},
		{Number: 2, State: "CLOSED"},
		{Number: 3, State: "OPEN"},
	}

	filtered := filterSubIssuesByState(subIssues, "all")
	if len(filtered) != 3 {
		t.Errorf("Expected 3 results for 'all', got %d", len(filtered))
	}
}

func TestFilterSubIssuesByState_Open(t *testing.T) {
	subIssues := []api.SubIssue{
		{Number: 1, State: "OPEN"},
		{Number: 2, State: "CLOSED"},
		{Number: 3, State: "OPEN"},
	}

	filtered := filterSubIssuesByState(subIssues, "open")
	if len(filtered) != 2 {
		t.Errorf("Expected 2 results for 'open', got %d", len(filtered))
	}

	for _, sub := range filtered {
		if sub.State != "OPEN" {
			t.Errorf("Expected all filtered issues to be OPEN, got %s", sub.State)
		}
	}
}

func TestFilterSubIssuesByState_Closed(t *testing.T) {
	subIssues := []api.SubIssue{
		{Number: 1, State: "OPEN"},
		{Number: 2, State: "CLOSED"},
		{Number: 3, State: "OPEN"},
		{Number: 4, State: "CLOSED"},
	}

	filtered := filterSubIssuesByState(subIssues, "closed")
	if len(filtered) != 2 {
		t.Errorf("Expected 2 results for 'closed', got %d", len(filtered))
	}

	for _, sub := range filtered {
		if sub.State != "CLOSED" {
			t.Errorf("Expected all filtered issues to be CLOSED, got %s", sub.State)
		}
	}
}

func TestFilterSubIssuesByState_Empty(t *testing.T) {
	subIssues := []api.SubIssue{}

	filtered := filterSubIssuesByState(subIssues, "open")
	if len(filtered) != 0 {
		t.Errorf("Expected 0 results for empty input, got %d", len(filtered))
	}
}

// ============================================================================
// outputSubListJSONExtended Tests
// ============================================================================

func TestOutputSubListJSONExtended_ChildrenOnly(t *testing.T) {
	result := SubListResult{
		Issue: &api.Issue{
			Number: 10,
			Title:  "Test Issue",
			State:  "OPEN",
			URL:    "https://github.com/owner/repo/issues/10",
		},
		Children: []api.SubIssue{
			{Number: 1, Title: "Child 1", State: "OPEN", Repository: api.Repository{Owner: "owner", Name: "repo"}},
			{Number: 2, Title: "Child 2", State: "CLOSED", Repository: api.Repository{Owner: "owner", Name: "repo"}},
		},
	}

	var buf bytes.Buffer
	err := outputSubListJSONExtended(&buf, result, "children")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var jsonResult SubListJSONExtended
	if err := json.Unmarshal(buf.Bytes(), &jsonResult); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	if jsonResult.Issue.Number != 10 {
		t.Errorf("Expected issue number 10, got %d", jsonResult.Issue.Number)
	}
	if len(jsonResult.Children) != 2 {
		t.Errorf("Expected 2 children, got %d", len(jsonResult.Children))
	}
	if jsonResult.Parent != nil {
		t.Error("Expected no parent in output")
	}
	if jsonResult.Summary.Total != 2 {
		t.Errorf("Expected total 2, got %d", jsonResult.Summary.Total)
	}
	if jsonResult.Summary.Open != 1 {
		t.Errorf("Expected open 1, got %d", jsonResult.Summary.Open)
	}
	if jsonResult.Summary.Closed != 1 {
		t.Errorf("Expected closed 1, got %d", jsonResult.Summary.Closed)
	}
}

func TestOutputSubListJSONExtended_WithParent(t *testing.T) {
	result := SubListResult{
		Issue: &api.Issue{
			Number: 10,
			Title:  "Test Issue",
			State:  "OPEN",
			URL:    "https://github.com/owner/repo/issues/10",
		},
		Parent: &api.Issue{
			Number: 5,
			Title:  "Parent Issue",
			State:  "OPEN",
			URL:    "https://github.com/owner/repo/issues/5",
		},
	}

	var buf bytes.Buffer
	err := outputSubListJSONExtended(&buf, result, "parent")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var jsonResult SubListJSONExtended
	if err := json.Unmarshal(buf.Bytes(), &jsonResult); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	if jsonResult.Parent == nil {
		t.Fatal("Expected parent in output")
	}
	if jsonResult.Parent.Number != 5 {
		t.Errorf("Expected parent number 5, got %d", jsonResult.Parent.Number)
	}
	if jsonResult.Parent.Title != "Parent Issue" {
		t.Errorf("Expected parent title 'Parent Issue', got '%s'", jsonResult.Parent.Title)
	}
}

func TestOutputSubListJSONExtended_WithSiblings(t *testing.T) {
	result := SubListResult{
		Issue: &api.Issue{
			Number: 10,
			Title:  "Test Issue",
			State:  "OPEN",
			URL:    "https://github.com/owner/repo/issues/10",
		},
		Parent: &api.Issue{
			Number: 5,
			Title:  "Parent Issue",
			State:  "OPEN",
			URL:    "https://github.com/owner/repo/issues/5",
		},
		Siblings: []api.SubIssue{
			{Number: 11, Title: "Sibling 1", State: "OPEN", Repository: api.Repository{Owner: "owner", Name: "repo"}},
			{Number: 12, Title: "Sibling 2", State: "OPEN", Repository: api.Repository{Owner: "owner", Name: "repo"}},
		},
	}

	var buf bytes.Buffer
	err := outputSubListJSONExtended(&buf, result, "siblings")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var jsonResult SubListJSONExtended
	if err := json.Unmarshal(buf.Bytes(), &jsonResult); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	if len(jsonResult.Siblings) != 2 {
		t.Errorf("Expected 2 siblings, got %d", len(jsonResult.Siblings))
	}
}

// ============================================================================
// outputSubListTableExtended Tests
// ============================================================================

func TestOutputSubListTableExtended_ChildrenOnly(t *testing.T) {
	result := SubListResult{
		Issue: &api.Issue{
			Number: 10,
			Title:  "Test Issue",
			State:  "OPEN",
			URL:    "https://github.com/owner/repo/issues/10",
			Repository: api.Repository{
				Owner: "owner",
				Name:  "repo",
			},
		},
		Children: []api.SubIssue{
			{Number: 1, Title: "Child 1", State: "OPEN", Repository: api.Repository{Owner: "owner", Name: "repo"}},
			{Number: 2, Title: "Child 2", State: "CLOSED", Repository: api.Repository{Owner: "owner", Name: "repo"}},
		},
	}

	var buf bytes.Buffer
	err := outputSubListTableExtended(&buf, result, "children")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	outputStr := buf.String()

	// Should show issue info
	if !strings.Contains(outputStr, "#10") {
		t.Error("Expected issue number in output")
	}

	// Should show children section
	if !strings.Contains(outputStr, "Children:") {
		t.Error("Expected 'Children:' section in output")
	}

	// Should show progress
	if !strings.Contains(outputStr, "1/2 complete") {
		t.Error("Expected progress '1/2 complete' in output")
	}

	// Should NOT show parent or siblings sections
	if strings.Contains(outputStr, "Parent:") {
		t.Error("Unexpected 'Parent:' section in output")
	}
	if strings.Contains(outputStr, "Siblings:") {
		t.Error("Unexpected 'Siblings:' section in output")
	}
}

func TestOutputSubListTableExtended_ParentRelation(t *testing.T) {
	result := SubListResult{
		Issue: &api.Issue{
			Number: 10,
			Title:  "Test Issue",
			State:  "OPEN",
			URL:    "https://github.com/owner/repo/issues/10",
		},
		Parent: &api.Issue{
			Number: 5,
			Title:  "Parent Issue",
			State:  "OPEN",
			URL:    "https://github.com/owner/repo/issues/5",
		},
	}

	var buf bytes.Buffer
	err := outputSubListTableExtended(&buf, result, "parent")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	outputStr := buf.String()

	// Should show parent section
	if !strings.Contains(outputStr, "Parent:") {
		t.Error("Expected 'Parent:' section in output")
	}
	if !strings.Contains(outputStr, "#5") {
		t.Error("Expected parent number #5 in output")
	}
	if !strings.Contains(outputStr, "Parent Issue") {
		t.Error("Expected parent title in output")
	}
}

func TestOutputSubListTableExtended_AllRelations(t *testing.T) {
	result := SubListResult{
		Issue: &api.Issue{
			Number:     10,
			Title:      "Test Issue",
			State:      "OPEN",
			URL:        "https://github.com/owner/repo/issues/10",
			Repository: api.Repository{Owner: "owner", Name: "repo"},
		},
		Parent: &api.Issue{
			Number: 5,
			Title:  "Parent Issue",
			State:  "OPEN",
			URL:    "https://github.com/owner/repo/issues/5",
		},
		Children: []api.SubIssue{
			{Number: 20, Title: "Child 1", State: "OPEN", Repository: api.Repository{Owner: "owner", Name: "repo"}},
		},
		Siblings: []api.SubIssue{
			{Number: 11, Title: "Sibling 1", State: "CLOSED", Repository: api.Repository{Owner: "owner", Name: "repo"}},
		},
	}

	var buf bytes.Buffer
	err := outputSubListTableExtended(&buf, result, "all")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	outputStr := buf.String()

	// Should show all sections
	if !strings.Contains(outputStr, "Parent:") {
		t.Error("Expected 'Parent:' section in output")
	}
	if !strings.Contains(outputStr, "Children:") {
		t.Error("Expected 'Children:' section in output")
	}
	if !strings.Contains(outputStr, "Siblings:") {
		t.Error("Expected 'Siblings:' section in output")
	}
}

func TestOutputSubListTableExtended_NoParent(t *testing.T) {
	result := SubListResult{
		Issue: &api.Issue{
			Number: 10,
			Title:  "Test Issue",
			State:  "OPEN",
			URL:    "https://github.com/owner/repo/issues/10",
		},
		Parent: nil, // No parent
	}

	var buf bytes.Buffer
	err := outputSubListTableExtended(&buf, result, "siblings")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	outputStr := buf.String()

	// Should indicate no parent
	if !strings.Contains(outputStr, "No parent issue") {
		t.Error("Expected 'No parent issue' message in output")
	}
}

// ============================================================================
// Sub Remove Command Enhancement Tests (#121-122)
// ============================================================================

func TestSubRemoveCommand_HasForceFlag(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "remove"})
	if err != nil {
		t.Fatalf("sub remove command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("force")
	if flag == nil {
		t.Fatal("Expected --force flag to exist")
	}
	if flag.Shorthand != "f" {
		t.Errorf("Expected --force shorthand 'f', got '%s'", flag.Shorthand)
	}
	if flag.DefValue != "false" {
		t.Errorf("Expected --force default 'false', got '%s'", flag.DefValue)
	}
}

func TestSubRemoveCommand_AcceptsMultipleChildren(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "remove"})
	if err != nil {
		t.Fatalf("sub remove command not found: %v", err)
	}

	// Check that Args allows minimum of 2 arguments (parent + at least one child)
	// The command should accept more than 2 args
	argsFunc := subCmd.Args
	if argsFunc == nil {
		t.Fatal("Expected Args function to be set")
	}

	// Test that 2 args is valid
	err = argsFunc(subCmd, []string{"10", "15"})
	if err != nil {
		t.Errorf("Expected 2 args to be valid, got error: %v", err)
	}

	// Test that 3 args is valid (parent + 2 children)
	err = argsFunc(subCmd, []string{"10", "15", "16"})
	if err != nil {
		t.Errorf("Expected 3 args to be valid, got error: %v", err)
	}

	// Test that 5 args is valid (parent + 4 children)
	err = argsFunc(subCmd, []string{"10", "15", "16", "17", "18"})
	if err != nil {
		t.Errorf("Expected 5 args to be valid, got error: %v", err)
	}

	// Test that 1 arg is invalid
	err = argsFunc(subCmd, []string{"10"})
	if err == nil {
		t.Error("Expected 1 arg to be invalid")
	}

	// Test that 0 args is invalid
	err = argsFunc(subCmd, []string{})
	if err == nil {
		t.Error("Expected 0 args to be invalid")
	}
}

func TestSubRemoveCommand_HelpDocumentsBatchRemoval(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "remove", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub remove help failed: %v", err)
	}

	output := buf.String()

	// Check that help mentions multiple children
	if !strings.Contains(output, "child-issue>...") {
		t.Error("Expected help to show <child-issue>... indicating multiple args")
	}

	// Check that help shows batch example
	if !strings.Contains(output, "15 16 17") {
		t.Error("Expected help to show batch removal example with multiple issues")
	}

	// Check that --force flag is documented
	if !strings.Contains(output, "-f, --force") {
		t.Error("Expected help to document --force flag")
	}
}

func TestSubRemoveCommand_UsageDescription(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "remove"})
	if err != nil {
		t.Fatalf("sub remove command not found: %v", err)
	}

	// Check that the short description mentions links (plural)
	if !strings.Contains(subCmd.Short, "links") {
		t.Error("Expected short description to mention 'links' (plural)")
	}

	// Check that the long description mentions batch operation
	if !strings.Contains(subCmd.Long, "Multiple") || !strings.Contains(subCmd.Long, "batch") {
		t.Error("Expected long description to mention batch operation")
	}
}

// Additional newSubRemoveCommand Flag Tests (IT-3.3)

func TestSubRemoveCommand_ForceFlagType(t *testing.T) {
	cmd := NewRootCommand()
	subCmd, _, err := cmd.Find([]string{"sub", "remove"})
	if err != nil {
		t.Fatalf("sub remove command not found: %v", err)
	}

	flag := subCmd.Flags().Lookup("force")
	if flag == nil {
		t.Fatal("Expected --force flag to exist")
	}

	// Verify it's a boolean flag
	if flag.Value.Type() != "bool" {
		t.Errorf("Expected --force to be bool, got %s", flag.Value.Type())
	}
}

func TestSubRemoveCommand_ForceFlagInHelp(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "remove", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub remove --help failed: %v", err)
	}

	output := buf.String()
	// Verify --force is documented
	if !strings.Contains(output, "--force") {
		t.Error("Expected help to mention --force flag")
	}
	// Verify shorthand is documented
	if !strings.Contains(output, "-f") {
		t.Error("Expected help to mention -f shorthand")
	}
	// Verify description mentions confirmation
	if !strings.Contains(output, "confirmation") || !strings.Contains(output, "Skip") {
		t.Error("Expected help to mention skipping confirmation prompts")
	}
}

func TestSubRemoveCommand_HelpShowsHashPrefix(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "remove", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub remove --help failed: %v", err)
	}

	output := buf.String()
	// Verify # prefix examples are shown
	if !strings.Contains(output, "#10") || !strings.Contains(output, "#15") {
		t.Error("Expected help to show # prefix examples")
	}
}

func TestSubRemoveCommand_HelpShowsCrossRepoExample(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"sub", "remove", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sub remove --help failed: %v", err)
	}

	output := buf.String()
	// Verify owner/repo# format is shown
	if !strings.Contains(output, "owner/repo#") {
		t.Error("Expected help to show owner/repo#number format")
	}
}
