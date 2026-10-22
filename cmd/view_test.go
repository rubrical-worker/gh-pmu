package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/spf13/cobra"
)

// mockViewClient implements viewClient for testing
type mockViewClient struct {
	issue       *api.Issue
	fieldValues []api.FieldValue
	subIssues   []api.SubIssue
	parentIssue *api.Issue
	comments    []api.Comment

	// Error injection
	getIssueErr                  error
	getIssueWithProjectFieldsErr error
	getSubIssuesErr              error
	getParentIssueErr            error
	getIssueCommentsErr          error
	getSubIssuesBatchErr         error
	getParentIssueBatchErr       error

	// Multi-issue support
	issues          map[int]*api.Issue
	fieldValuesMap  map[int][]api.FieldValue
	subIssuesMap    map[int][]api.SubIssue
	parentIssuesMap map[int]*api.Issue
	issueErrors     map[int]error

	// Repo-scoped issues for multi-repo collision tests: "owner/repo" -> number -> issue
	issuesByRepo map[string]map[int]*api.Issue
}

func newMockViewClient() *mockViewClient {
	return &mockViewClient{
		issue: &api.Issue{
			Number: 42,
			Title:  "Test Issue",
			State:  "OPEN",
			URL:    "https://github.com/owner/repo/issues/42",
			Author: api.Actor{Login: "testuser"},
		},
		fieldValues: []api.FieldValue{},
		subIssues:   []api.SubIssue{},
	}
}

func (m *mockViewClient) GetIssue(owner, repo string, number int) (*api.Issue, error) {
	if m.getIssueErr != nil {
		return nil, m.getIssueErr
	}
	return m.issue, nil
}

func (m *mockViewClient) GetIssueWithProjectFields(projectID, owner, repo string, number int) (*api.Issue, []api.FieldValue, error) {
	if m.getIssueWithProjectFieldsErr != nil {
		return nil, nil, m.getIssueWithProjectFieldsErr
	}
	return m.issue, m.fieldValues, nil
}

func (m *mockViewClient) GetSubIssues(owner, repo string, number int) ([]api.SubIssue, error) {
	if m.getSubIssuesErr != nil {
		return nil, m.getSubIssuesErr
	}
	return m.subIssues, nil
}

func (m *mockViewClient) GetParentIssue(owner, repo string, number int) (*api.Issue, error) {
	if m.getParentIssueErr != nil {
		return nil, m.getParentIssueErr
	}
	return m.parentIssue, nil
}

func (m *mockViewClient) GetIssueComments(owner, repo string, number int) ([]api.Comment, error) {
	if m.getIssueCommentsErr != nil {
		return nil, m.getIssueCommentsErr
	}
	return m.comments, nil
}

func (m *mockViewClient) GetIssuesWithProjectFieldsBatch(projectID, owner, repo string, numbers []int) (map[int]*api.Issue, map[int][]api.FieldValue, map[int]error, error) {
	issues := make(map[int]*api.Issue)
	fvs := make(map[int][]api.FieldValue)
	errs := make(map[int]error)
	repoIssues := m.issues
	if m.issuesByRepo != nil {
		repoIssues = m.issuesByRepo[owner+"/"+repo]
	}
	for _, n := range numbers {
		if m.issueErrors != nil {
			if e, ok := m.issueErrors[n]; ok {
				errs[n] = e
				continue
			}
		}
		if repoIssues != nil {
			if iss, ok := repoIssues[n]; ok {
				issues[n] = iss
			}
		}
		if m.fieldValuesMap != nil {
			if fv, ok := m.fieldValuesMap[n]; ok {
				fvs[n] = fv
			}
		}
	}
	return issues, fvs, errs, nil
}

func (m *mockViewClient) GetSubIssuesBatch(owner, repo string, numbers []int) (map[int][]api.SubIssue, error) {
	if m.getSubIssuesBatchErr != nil {
		return nil, m.getSubIssuesBatchErr
	}
	result := make(map[int][]api.SubIssue)
	for _, n := range numbers {
		if m.subIssuesMap != nil {
			if subs, ok := m.subIssuesMap[n]; ok {
				result[n] = subs
			}
		}
	}
	return result, nil
}

func (m *mockViewClient) GetParentIssueBatch(owner, repo string, numbers []int) (map[int]*api.Issue, error) {
	if m.getParentIssueBatchErr != nil {
		return nil, m.getParentIssueBatchErr
	}
	result := make(map[int]*api.Issue)
	for _, n := range numbers {
		if m.parentIssuesMap != nil {
			if parent, ok := m.parentIssuesMap[n]; ok {
				result[n] = parent
			}
		}
	}
	return result, nil
}

// ============================================================================
// runViewWithDeps Tests
// ============================================================================

// A discarded sub-issue, parent or comment fetch error made a failed call look
// like "none": view printed no sub-issues and exited 0 (#939). The issue is
// still shown, and the failure is reported as a partial success.
func TestRunViewWithDeps_RelatedFetchFailureIsReported(t *testing.T) {
	cases := []struct {
		name   string
		inject func(*mockViewClient)
		opts   viewOptions
		step   string
	}{
		{"sub-issues", func(m *mockViewClient) { m.getSubIssuesErr = errors.New("sub-issues 502") }, viewOptions{}, "sub-issues 502"},
		{"parent", func(m *mockViewClient) { m.getParentIssueErr = errors.New("parent 502") }, viewOptions{}, "parent 502"},
		{"comments", func(m *mockViewClient) { m.getIssueCommentsErr = errors.New("comments 502") }, viewOptions{comments: true}, "comments 502"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := newMockViewClient()
			tc.inject(mock)
			cmd := &cobra.Command{}
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			opts := tc.opts

			err := runViewWithDeps(cmd, &opts, mock, "owner", "repo", 42)

			if !api.IsPartialSuccess(err) || !strings.Contains(err.Error(), tc.step) {
				t.Fatalf("expected a partial-success error carrying %q, got: %v", tc.step, err)
			}
			if !strings.Contains(buf.String(), "Test Issue") {
				t.Errorf("the issue should still be shown, got: %s", buf.String())
			}
		})
	}
}

func TestRunViewMulti_RelatedBatchFetchFailureIsReported(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inject func(*mockViewClient)
	}{
		{"sub-issues", func(m *mockViewClient) { m.getSubIssuesBatchErr = errors.New("batch 502") }},
		{"parents", func(m *mockViewClient) { m.getParentIssueBatchErr = errors.New("batch 502") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := newMultiMockViewClient()
			tc.inject(mock)
			cmd := &cobra.Command{}
			cmd.SetOut(new(bytes.Buffer))
			refs := []viewIssueRef{{owner: "o", repo: "r", number: 42}, {owner: "o", repo: "r", number: 43}}

			err := runViewMulti(cmd, &viewOptions{jsonFields: "number"}, mock, refs, nil)
			if !api.IsPartialSuccess(err) || !strings.Contains(err.Error(), "batch 502") {
				t.Fatalf("expected a partial-success error carrying the batch failure, got: %v", err)
			}
		})
	}
}

func TestRunViewWithDeps_Success(t *testing.T) {
	mock := newMockViewClient()
	mock.issue = &api.Issue{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "testuser"},
	}
	mock.fieldValues = []api.FieldValue{
		{Field: "Status", Value: "In Progress"},
	}

	cmd := newViewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &viewOptions{}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunViewWithDeps_GetIssueError(t *testing.T) {
	mock := newMockViewClient()
	mock.getIssueWithProjectFieldsErr = errors.New("issue not found")

	cmd := newViewCommand()
	opts := &viewOptions{}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 42)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get issue") {
		t.Errorf("expected 'failed to get issue' error, got: %v", err)
	}
}

func TestRunViewWithDeps_JSONOutput(t *testing.T) {
	mock := newMockViewClient()
	mock.issue = &api.Issue{
		Number: 42,
		Title:  "JSON Test Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "testuser"},
	}

	cmd := newViewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &viewOptions{jsonFields: "number,title,state,body,url,author,assignees,labels,fieldValues"}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunViewWithDeps_WithSubIssues(t *testing.T) {
	mock := newMockViewClient()
	mock.issue = &api.Issue{
		Number: 42,
		Title:  "Parent Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "testuser"},
	}
	mock.subIssues = []api.SubIssue{
		{Number: 43, Title: "Sub 1", State: "CLOSED"},
		{Number: 44, Title: "Sub 2", State: "OPEN"},
	}

	cmd := newViewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &viewOptions{}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunViewWithDeps_WithParentIssue(t *testing.T) {
	mock := newMockViewClient()
	mock.issue = &api.Issue{
		Number: 43,
		Title:  "Sub-Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/43",
		Author: api.Actor{Login: "testuser"},
	}
	mock.parentIssue = &api.Issue{
		Number: 42,
		Title:  "Parent Issue",
		URL:    "https://github.com/owner/repo/issues/42",
	}

	cmd := newViewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &viewOptions{}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 43)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunViewWithDeps_WithComments(t *testing.T) {
	mock := newMockViewClient()
	mock.issue = &api.Issue{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "testuser"},
	}
	mock.comments = []api.Comment{
		{Author: "user1", Body: "Comment 1", CreatedAt: "2024-01-01T10:00:00Z"},
		{Author: "user2", Body: "Comment 2", CreatedAt: "2024-01-02T11:00:00Z"},
	}

	cmd := newViewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &viewOptions{comments: true}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestViewCommand_Exists(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"view", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("view command should exist: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("view")) {
		t.Error("Expected help output to mention 'view'")
	}
}

func TestViewCommand_RequiresIssueNumber(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"view"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when issue number not provided")
	}
}

func TestViewCommand_HasJSONFlag(t *testing.T) {
	cmd := NewRootCommand()
	viewCmd, _, err := cmd.Find([]string{"view"})
	if err != nil {
		t.Fatalf("view command not found: %v", err)
	}

	flag := viewCmd.Flags().Lookup("json")
	if flag == nil {
		t.Fatal("Expected --json flag to exist")
	}
}

// TestViewCommand_RuntimeErrorOmitsUsage: a failure inside RunE (here, no
// configuration) prints the error without the usage dump (#927).
func TestViewCommand_RuntimeErrorOmitsUsage(t *testing.T) {
	chdirNoConfig(t)
	out, err := executeForErrorOutput(t, "view", "42")
	if err == nil {
		t.Fatal("Expected a runtime error with no configuration")
	}
	if !strings.Contains(out, "failed to load configuration") {
		t.Errorf("Expected the runtime error in the output, got:\n%s", out)
	}
	if strings.Contains(out, "Usage:") {
		t.Errorf("Expected no usage block after a runtime error, got:\n%s", out)
	}
}

// TestViewCommand_ArgumentErrorKeepsUsage: an argument error keeps the usage
// block with the Use line, plus Examples whenever view defines them (#927).
func TestViewCommand_ArgumentErrorKeepsUsage(t *testing.T) {
	out, err := executeForErrorOutput(t, "view")
	if err == nil {
		t.Fatal("Expected an argument error for view with no issue number")
	}
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "gh pmu view <issue-number>") {
		t.Errorf("Expected the usage block with the Use line, got:\n%s", out)
	}
	viewCmd, _, _ := NewRootCommand().Find([]string{"view"})
	if viewCmd.Example != "" && strings.Count(out, "Examples:") != 1 {
		t.Errorf("Expected exactly one Examples section, got:\n%s", out)
	}
}

func TestViewCommand_JSONSpaceFormMatchesEqualsForm(t *testing.T) {
	spaced := newViewCommand()
	if err := parseJSONFlagCommand(t, spaced, []string{"42", "--json", "status,title"}); err != nil {
		t.Fatalf("--json status,title: unexpected error: %v", err)
	}
	equals := newViewCommand()
	if err := parseJSONFlagCommand(t, equals, []string{"42", "--json=status,title"}); err != nil {
		t.Fatalf("--json=status,title: unexpected error: %v", err)
	}
	a, _ := spaced.Flags().GetString("json")
	b, _ := equals.Flags().GetString("json")
	if a != b || a != "status,title" {
		t.Errorf("space form parsed %q, equals form parsed %q; want both \"status,title\"", a, b)
	}
	if got := spaced.Flags().Args(); len(got) != 1 || got[0] != "42" {
		t.Errorf("space form positional args = %v, want [42]: the field list must not leak into issue numbers", got)
	}
}

func TestViewCommand_JSONValueIsRequired(t *testing.T) {
	if err := newViewCommand().ParseFlags([]string{"42", "--json"}); err == nil {
		t.Error("Expected bare --json to be rejected; use --json-fields to list fields")
	}
	err := parseJSONFlagCommand(t, newViewCommand(), []string{"42", "--json="})
	if err == nil {
		t.Fatal("Expected --json= with no fields to be rejected rather than silently printing text")
	}
	if !strings.Contains(err.Error(), "--json-fields") {
		t.Errorf("Expected error to point at --json-fields, got: %v", err)
	}
}

func TestViewCommand_HasCommentsFlag(t *testing.T) {
	cmd := NewRootCommand()
	viewCmd, _, err := cmd.Find([]string{"view"})
	if err != nil {
		t.Fatalf("view command not found: %v", err)
	}

	flag := viewCmd.Flags().Lookup("comments")
	if flag == nil {
		t.Fatal("Expected --comments flag to exist")
	}

	// Check shorthand
	if flag.Shorthand != "c" {
		t.Errorf("Expected --comments shorthand to be 'c', got %s", flag.Shorthand)
	}
}

func TestViewCommand_HasRepoFlag(t *testing.T) {
	cmd := NewRootCommand()
	viewCmd, _, err := cmd.Find([]string{"view"})
	if err != nil {
		t.Fatalf("view command not found: %v", err)
	}

	flag := viewCmd.Flags().Lookup("repo")
	if flag == nil {
		t.Fatal("Expected --repo flag to exist")
	}

	// Check shorthand
	if flag.Shorthand != "R" {
		t.Errorf("Expected --repo shorthand to be 'R', got %s", flag.Shorthand)
	}

	// Check type
	if flag.Value.Type() != "string" {
		t.Errorf("Expected --repo to be string, got %s", flag.Value.Type())
	}
}

func TestViewCommand_AcceptsIssueNumber(t *testing.T) {
	cmd := NewRootCommand()
	viewCmd, _, err := cmd.Find([]string{"view"})
	if err != nil {
		t.Fatalf("view command not found: %v", err)
	}

	// Verify the command accepts exactly 1 argument
	if viewCmd.Args == nil {
		t.Error("Expected Args validator to be set")
	}
}

func TestViewCommand_ParsesIssueNumber(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		wantErr bool
	}{
		{"valid number", "123", false},
		{"with hash", "#123", false},
		{"invalid string", "abc", true},
		{"negative number", "-1", true},
		{"zero", "0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseIssueNumber(tt.arg)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseIssueNumber(%q) error = %v, wantErr %v", tt.arg, err, tt.wantErr)
			}
		})
	}
}

func TestViewCommand_ParsesIssueReference(t *testing.T) {
	tests := []struct {
		name       string
		arg        string
		wantOwner  string
		wantRepo   string
		wantNumber int
		wantErr    bool
	}{
		{"number only", "123", "", "", 123, false},
		{"with hash", "#123", "", "", 123, false},
		{"full reference", "owner/repo#123", "owner", "repo", 123, false},
		{"invalid", "invalid", "", "", 0, true},
		// URL formats
		{"https URL", "https://github.com/owner/repo/issues/123", "owner", "repo", 123, false},
		{"http URL", "http://github.com/owner/repo/issues/123", "owner", "repo", 123, false},
		{"URL with anchor", "https://github.com/owner/repo/issues/123#issuecomment-456", "owner", "repo", 123, false},
		{"invalid URL - not issues", "https://github.com/owner/repo/pulls/123", "", "", 0, true},
		{"invalid URL - too short", "https://github.com/owner", "", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, number, err := parseIssueReference(tt.arg)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseIssueReference(%q) error = %v, wantErr %v", tt.arg, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if owner != tt.wantOwner {
					t.Errorf("parseIssueReference(%q) owner = %v, want %v", tt.arg, owner, tt.wantOwner)
				}
				if repo != tt.wantRepo {
					t.Errorf("parseIssueReference(%q) repo = %v, want %v", tt.arg, repo, tt.wantRepo)
				}
				if number != tt.wantNumber {
					t.Errorf("parseIssueReference(%q) number = %v, want %v", tt.arg, number, tt.wantNumber)
				}
			}
		})
	}
}

// Progress bar tests

func TestRenderProgressBar(t *testing.T) {
	tests := []struct {
		name      string
		completed int
		total     int
		width     int
		want      string
	}{
		{"empty", 0, 10, 10, "[░░░░░░░░░░]"},
		{"half", 5, 10, 10, "[█████░░░░░]"},
		{"full", 10, 10, 10, "[██████████]"},
		{"quarter", 1, 4, 8, "[██░░░░░░]"},
		{"zero total", 0, 0, 10, "[░░░░░░░░░░]"},
		{"60 percent", 3, 5, 10, "[██████░░░░]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderProgressBar(tt.completed, tt.total, tt.width)
			if got != tt.want {
				t.Errorf("renderProgressBar(%d, %d, %d) = %q, want %q",
					tt.completed, tt.total, tt.width, got, tt.want)
			}
		})
	}
}

func TestRenderProgressBar_OverflowProtection(t *testing.T) {
	// Test that completed > total doesn't overflow
	result := renderProgressBar(15, 10, 10)
	// Should cap at full
	if result != "[██████████]" {
		t.Errorf("renderProgressBar with overflow should cap at full, got %q", result)
	}
}

// ============================================================================
// outputViewTable Tests
// ============================================================================

// createViewTestCmd creates a cobra command for testing view output
func createViewTestCmd(buf *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	return cmd
}

func TestOutputViewTable_BasicIssue(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 42,
		Title:  "Test Issue Title",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "testuser"},
	}

	err := outputViewTable(cmd, issue, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("outputViewTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Test Issue Title #42") {
		t.Errorf("expected output to contain title and number, got:\n%s", output)
	}
	if !strings.Contains(output, "State: OPEN") {
		t.Errorf("expected output to contain state, got:\n%s", output)
	}
	if !strings.Contains(output, "https://github.com/owner/repo/issues/42") {
		t.Errorf("expected output to contain URL, got:\n%s", output)
	}
	if !strings.Contains(output, "Author: @testuser") {
		t.Errorf("expected output to contain author, got:\n%s", output)
	}
}

func TestOutputViewTable_WithAssignees(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "author"},
		Assignees: []api.Actor{
			{Login: "user1"},
			{Login: "user2"},
		},
	}

	err := outputViewTable(cmd, issue, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("outputViewTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Assignees: @user1, @user2") {
		t.Errorf("expected output to list assignees, got:\n%s", output)
	}
}

func TestOutputViewTable_WithLabels(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "author"},
		Labels: []api.Label{
			{Name: "bug"},
			{Name: "priority:high"},
		},
	}

	err := outputViewTable(cmd, issue, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("outputViewTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Labels: bug, priority:high") {
		t.Errorf("expected output to list labels, got:\n%s", output)
	}
}

func TestOutputViewTable_WithMilestone(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number:    42,
		Title:     "Test Issue",
		State:     "OPEN",
		URL:       "https://github.com/owner/repo/issues/42",
		Author:    api.Actor{Login: "author"},
		Milestone: &api.Milestone{Title: "v1.0.0"},
	}

	err := outputViewTable(cmd, issue, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("outputViewTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Milestone: v1.0.0") {
		t.Errorf("expected output to show milestone, got:\n%s", output)
	}
}

func TestOutputViewTable_WithFieldValues(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "author"},
	}

	fieldValues := []api.FieldValue{
		{Field: "Status", Value: "In Progress"},
		{Field: "Priority", Value: "High"},
	}

	err := outputViewTable(cmd, issue, fieldValues, nil, nil, nil)
	if err != nil {
		t.Fatalf("outputViewTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Project Fields:") {
		t.Errorf("expected output to contain project fields header, got:\n%s", output)
	}
	if !strings.Contains(output, "Status: In Progress") {
		t.Errorf("expected output to contain Status field value, got:\n%s", output)
	}
	if !strings.Contains(output, "Priority: High") {
		t.Errorf("expected output to contain Priority field value, got:\n%s", output)
	}
}

func TestOutputViewTable_WithParentIssue(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 42,
		Title:  "Sub-Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "author"},
	}

	parentIssue := &api.Issue{
		Number: 10,
		Title:  "Parent Issue",
		URL:    "https://github.com/owner/repo/issues/10",
	}

	err := outputViewTable(cmd, issue, nil, nil, parentIssue, nil)
	if err != nil {
		t.Fatalf("outputViewTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Parent Issue: #10 - Parent Issue") {
		t.Errorf("expected output to show parent issue reference, got:\n%s", output)
	}
}

func TestOutputViewTable_WithSubIssues(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 42,
		Title:  "Parent Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "author"},
		Repository: api.Repository{
			Owner: "owner",
			Name:  "repo",
		},
	}

	subIssues := []api.SubIssue{
		{Number: 43, Title: "Sub 1", State: "CLOSED", URL: "https://github.com/owner/repo/issues/43"},
		{Number: 44, Title: "Sub 2", State: "OPEN", URL: "https://github.com/owner/repo/issues/44"},
		{Number: 45, Title: "Sub 3", State: "CLOSED", URL: "https://github.com/owner/repo/issues/45"},
	}

	err := outputViewTable(cmd, issue, nil, subIssues, nil, nil)
	if err != nil {
		t.Fatalf("outputViewTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Sub-Issues:") {
		t.Errorf("expected output to contain sub-issues header, got:\n%s", output)
	}
	if !strings.Contains(output, "[x] #43 - Sub 1") {
		t.Errorf("expected closed sub-issue #43 rendered as [x], got:\n%s", output)
	}
	if !strings.Contains(output, "[ ] #44 - Sub 2") {
		t.Errorf("expected open sub-issue #44 rendered as [ ], got:\n%s", output)
	}
	if !strings.Contains(output, "[x] #45 - Sub 3") {
		t.Errorf("expected closed sub-issue #45 rendered as [x], got:\n%s", output)
	}
	// 2 of 3 closed = 66%
	if !strings.Contains(output, "2 of 3 sub-issues complete (66%)") {
		t.Errorf("expected progress summary '2 of 3 ... (66%%)', got:\n%s", output)
	}
}

func TestOutputViewTable_WithCrossRepoSubIssues(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 42,
		Title:  "Parent Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "author"},
		Repository: api.Repository{
			Owner: "owner",
			Name:  "repo",
		},
	}

	subIssues := []api.SubIssue{
		{
			Number: 43,
			Title:  "Same Repo Sub",
			State:  "OPEN",
			URL:    "https://github.com/owner/repo/issues/43",
			Repository: api.Repository{
				Owner: "owner",
				Name:  "repo",
			},
		},
		{
			Number: 10,
			Title:  "Cross Repo Sub",
			State:  "CLOSED",
			URL:    "https://github.com/owner/other-repo/issues/10",
			Repository: api.Repository{
				Owner: "owner",
				Name:  "other-repo",
			},
		},
	}

	err := outputViewTable(cmd, issue, nil, subIssues, nil, nil)
	if err != nil {
		t.Fatalf("outputViewTable() error = %v", err)
	}

	output := buf.String()
	// Same-repo sub renders without repo prefix
	if !strings.Contains(output, "[ ] #43 - Same Repo Sub") {
		t.Errorf("expected same-repo sub-issue without repo prefix, got:\n%s", output)
	}
	// Cross-repo sub renders with owner/repo prefix
	if !strings.Contains(output, "[x] owner/other-repo#10 - Cross Repo Sub") {
		t.Errorf("expected cross-repo sub-issue with repo prefix, got:\n%s", output)
	}
	// 1 of 2 closed = 50%
	if !strings.Contains(output, "1 of 2 sub-issues complete (50%)") {
		t.Errorf("expected progress summary '1 of 2 ... (50%%)', got:\n%s", output)
	}
}

func TestOutputViewTable_WithBody(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "author"},
		Body:   "This is the issue body with some content.\n\nMultiple paragraphs.",
	}

	err := outputViewTable(cmd, issue, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("outputViewTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "This is the issue body with some content.") {
		t.Errorf("expected output to contain issue body, got:\n%s", output)
	}
	if !strings.Contains(output, "Multiple paragraphs.") {
		t.Errorf("expected output to contain full multi-paragraph body, got:\n%s", output)
	}
}

func TestOutputViewTable_FullIssue(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number:    42,
		Title:     "Full Featured Issue",
		State:     "OPEN",
		URL:       "https://github.com/owner/repo/issues/42",
		Body:      "Issue body content",
		Author:    api.Actor{Login: "author"},
		Assignees: []api.Actor{{Login: "dev1"}, {Login: "dev2"}},
		Labels:    []api.Label{{Name: "bug"}, {Name: "urgent"}},
		Milestone: &api.Milestone{Title: "v2.0"},
		Repository: api.Repository{
			Owner: "owner",
			Name:  "repo",
		},
	}

	fieldValues := []api.FieldValue{
		{Field: "Status", Value: "In Progress"},
		{Field: "Priority", Value: "P1"},
	}

	subIssues := []api.SubIssue{
		{Number: 43, Title: "Task 1", State: "CLOSED"},
		{Number: 44, Title: "Task 2", State: "OPEN"},
	}

	parentIssue := &api.Issue{
		Number: 10,
		Title:  "Epic",
		URL:    "https://github.com/owner/repo/issues/10",
	}

	err := outputViewTable(cmd, issue, fieldValues, subIssues, parentIssue, nil)
	if err != nil {
		t.Fatalf("outputViewTable() error = %v", err)
	}

	output := buf.String()
	// Header + core identity
	if !strings.Contains(output, "Full Featured Issue #42") {
		t.Errorf("expected output to contain title and number, got:\n%s", output)
	}
	// Assignees and labels
	if !strings.Contains(output, "Assignees: @dev1, @dev2") {
		t.Errorf("expected output to list assignees, got:\n%s", output)
	}
	if !strings.Contains(output, "Labels: bug, urgent") {
		t.Errorf("expected output to list labels, got:\n%s", output)
	}
	// Milestone
	if !strings.Contains(output, "Milestone: v2.0") {
		t.Errorf("expected output to show milestone, got:\n%s", output)
	}
	// Project fields
	if !strings.Contains(output, "Status: In Progress") || !strings.Contains(output, "Priority: P1") {
		t.Errorf("expected output to show project fields, got:\n%s", output)
	}
	// Parent issue
	if !strings.Contains(output, "Parent Issue: #10 - Epic") {
		t.Errorf("expected output to show parent issue, got:\n%s", output)
	}
	// Sub-issues + progress (1 of 2 closed = 50%)
	if !strings.Contains(output, "[x] #43 - Task 1") || !strings.Contains(output, "[ ] #44 - Task 2") {
		t.Errorf("expected output to list sub-issues with state markers, got:\n%s", output)
	}
	if !strings.Contains(output, "1 of 2 sub-issues complete (50%)") {
		t.Errorf("expected progress summary '1 of 2 ... (50%%)', got:\n%s", output)
	}
	// Body
	if !strings.Contains(output, "Issue body content") {
		t.Errorf("expected output to contain body, got:\n%s", output)
	}
}

// ============================================================================
// outputViewJSON Tests
// ============================================================================

func TestOutputViewJSON_BasicIssue(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "testuser"},
	}

	opts := &viewOptions{jsonFields: "number,title,state,body,url,author,assignees,labels,fieldValues"}
	err := outputViewJSON(cmd, opts, issue, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("outputViewJSON() error = %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("expected valid JSON, got error: %v\noutput: %s", err, buf.String())
	}
	if decoded["number"].(float64) != 42 {
		t.Errorf("expected number 42, got %v", decoded["number"])
	}
	if decoded["title"] != "Test Issue" {
		t.Errorf("expected title 'Test Issue', got %v", decoded["title"])
	}
	if decoded["state"] != "OPEN" {
		t.Errorf("expected state 'OPEN', got %v", decoded["state"])
	}
	if decoded["author"] != "testuser" {
		t.Errorf("expected author 'testuser', got %v", decoded["author"])
	}
}

func TestOutputViewJSON_WithAllFields(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number:    42,
		Title:     "Full Issue",
		State:     "OPEN",
		Body:      "Issue description",
		URL:       "https://github.com/owner/repo/issues/42",
		Author:    api.Actor{Login: "author"},
		Assignees: []api.Actor{{Login: "dev1"}, {Login: "dev2"}},
		Labels:    []api.Label{{Name: "bug"}, {Name: "priority:high"}},
		Milestone: &api.Milestone{Title: "v1.0"},
	}

	fieldValues := []api.FieldValue{
		{Field: "Status", Value: "In Progress"},
		{Field: "Priority", Value: "High"},
	}

	opts := &viewOptions{jsonFields: "number,title,state,body,url,author,assignees,labels,milestone,fieldValues"}
	err := outputViewJSON(cmd, opts, issue, fieldValues, nil, nil, nil)
	if err != nil {
		t.Fatalf("outputViewJSON() error = %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("expected valid JSON, got error: %v\noutput: %s", err, buf.String())
	}
	if decoded["title"] != "Full Issue" {
		t.Errorf("expected title 'Full Issue', got %v", decoded["title"])
	}
	if decoded["body"] != "Issue description" {
		t.Errorf("expected body 'Issue description', got %v", decoded["body"])
	}
	if decoded["milestone"] != "v1.0" {
		t.Errorf("expected milestone 'v1.0', got %v", decoded["milestone"])
	}
	assignees, ok := decoded["assignees"].([]interface{})
	if !ok || len(assignees) != 2 || assignees[0] != "dev1" || assignees[1] != "dev2" {
		t.Errorf("expected assignees [dev1 dev2], got %v", decoded["assignees"])
	}
	labels, ok := decoded["labels"].([]interface{})
	if !ok || len(labels) != 2 || labels[0] != "bug" || labels[1] != "priority:high" {
		t.Errorf("expected labels [bug priority:high], got %v", decoded["labels"])
	}
	fv, ok := decoded["fieldValues"].(map[string]interface{})
	if !ok || fv["Status"] != "In Progress" || fv["Priority"] != "High" {
		t.Errorf("expected fieldValues Status/Priority, got %v", decoded["fieldValues"])
	}
}

func TestOutputViewJSON_WithSubIssues(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 42,
		Title:  "Parent Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "author"},
	}

	subIssues := []api.SubIssue{
		{Number: 43, Title: "Sub 1", State: "CLOSED", URL: "https://github.com/owner/repo/issues/43"},
		{Number: 44, Title: "Sub 2", State: "OPEN", URL: "https://github.com/owner/repo/issues/44"},
		{Number: 45, Title: "Sub 3", State: "CLOSED", URL: "https://github.com/owner/repo/issues/45"},
	}

	opts := &viewOptions{jsonFields: "number,title,state,body,url,author,assignees,labels,fieldValues,subIssues,subProgress"}
	err := outputViewJSON(cmd, opts, issue, nil, subIssues, nil, nil)
	if err != nil {
		t.Fatalf("outputViewJSON() error = %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("expected valid JSON, got error: %v\noutput: %s", err, buf.String())
	}
	subs, ok := decoded["subIssues"].([]interface{})
	if !ok || len(subs) != 3 {
		t.Fatalf("expected 3 sub-issues, got %v", decoded["subIssues"])
	}
	first := subs[0].(map[string]interface{})
	if first["number"].(float64) != 43 || first["title"] != "Sub 1" || first["state"] != "CLOSED" {
		t.Errorf("expected first sub-issue #43 'Sub 1' CLOSED, got %v", first)
	}
	prog, ok := decoded["subProgress"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected subProgress present, got %v", decoded["subProgress"])
	}
	// 2 closed of 3 = 66%
	if prog["total"].(float64) != 3 || prog["completed"].(float64) != 2 || prog["percentage"].(float64) != 66 {
		t.Errorf("expected progress total=3 completed=2 percentage=66, got %v", prog)
	}
}

func TestOutputViewJSON_WithParentIssue(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 42,
		Title:  "Sub-Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "author"},
	}

	parentIssue := &api.Issue{
		Number: 10,
		Title:  "Parent Issue",
		URL:    "https://github.com/owner/repo/issues/10",
	}

	opts := &viewOptions{jsonFields: "number,title,state,body,url,author,assignees,labels,fieldValues,parentIssue"}
	err := outputViewJSON(cmd, opts, issue, nil, nil, parentIssue, nil)
	if err != nil {
		t.Fatalf("outputViewJSON() error = %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("expected valid JSON, got error: %v\noutput: %s", err, buf.String())
	}
	parent, ok := decoded["parentIssue"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected parentIssue present, got %v", decoded["parentIssue"])
	}
	if parent["number"].(float64) != 10 || parent["title"] != "Parent Issue" {
		t.Errorf("expected parent #10 'Parent Issue', got %v", parent)
	}
}

func TestOutputViewJSON_SubIssueProgress(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 42,
		Title:  "Parent Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "author"},
	}

	// 3 closed out of 5 = 60%
	subIssues := []api.SubIssue{
		{Number: 1, Title: "Task 1", State: "CLOSED"},
		{Number: 2, Title: "Task 2", State: "CLOSED"},
		{Number: 3, Title: "Task 3", State: "OPEN"},
		{Number: 4, Title: "Task 4", State: "CLOSED"},
		{Number: 5, Title: "Task 5", State: "OPEN"},
	}

	opts := &viewOptions{jsonFields: "number,title,state,body,url,author,assignees,labels,fieldValues,subIssues,subProgress"}
	err := outputViewJSON(cmd, opts, issue, nil, subIssues, nil, nil)
	if err != nil {
		t.Fatalf("outputViewJSON() error = %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("expected valid JSON, got error: %v\noutput: %s", err, buf.String())
	}
	prog, ok := decoded["subProgress"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected subProgress present, got %v", decoded["subProgress"])
	}
	// 3 closed of 5 = 60%
	if prog["total"].(float64) != 5 {
		t.Errorf("expected total 5, got %v", prog["total"])
	}
	if prog["completed"].(float64) != 3 {
		t.Errorf("expected completed 3, got %v", prog["completed"])
	}
	if prog["percentage"].(float64) != 60 {
		t.Errorf("expected percentage 60, got %v", prog["percentage"])
	}
}

func TestOutputViewTable_WithComments(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "author"},
	}

	comments := []api.Comment{
		{Author: "user1", Body: "First comment", CreatedAt: "2024-01-01T10:00:00Z"},
		{Author: "user2", Body: "Second comment", CreatedAt: "2024-01-02T11:00:00Z"},
	}

	err := outputViewTable(cmd, issue, nil, nil, nil, comments)
	if err != nil {
		t.Fatalf("outputViewTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Comments (2):") {
		t.Errorf("expected comments count header, got:\n%s", output)
	}
	if !strings.Contains(output, "@user1 commented on 2024-01-01T10:00:00Z:") {
		t.Errorf("expected first comment attribution, got:\n%s", output)
	}
	if !strings.Contains(output, "First comment") {
		t.Errorf("expected first comment body, got:\n%s", output)
	}
	if !strings.Contains(output, "@user2 commented on 2024-01-02T11:00:00Z:") {
		t.Errorf("expected second comment attribution, got:\n%s", output)
	}
	if !strings.Contains(output, "Second comment") {
		t.Errorf("expected second comment body, got:\n%s", output)
	}
}

func TestOutputViewJSON_WithComments(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "author"},
	}

	comments := []api.Comment{
		{Author: "user1", Body: "First comment", CreatedAt: "2024-01-01T10:00:00Z"},
		{Author: "user2", Body: "Second comment", CreatedAt: "2024-01-02T11:00:00Z"},
	}

	opts := &viewOptions{jsonFields: "number,title,state,body,url,author,assignees,labels,fieldValues,comments"}
	err := outputViewJSON(cmd, opts, issue, nil, nil, nil, comments)
	if err != nil {
		t.Fatalf("outputViewJSON() error = %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("expected valid JSON, got error: %v\noutput: %s", err, buf.String())
	}
	cs, ok := decoded["comments"].([]interface{})
	if !ok || len(cs) != 2 {
		t.Fatalf("expected 2 comments, got %v", decoded["comments"])
	}
	c0 := cs[0].(map[string]interface{})
	if c0["author"] != "user1" || c0["body"] != "First comment" || c0["createdAt"] != "2024-01-01T10:00:00Z" {
		t.Errorf("expected first comment user1/First comment, got %v", c0)
	}
	c1 := cs[1].(map[string]interface{})
	if c1["author"] != "user2" || c1["body"] != "Second comment" {
		t.Errorf("expected second comment user2/Second comment, got %v", c1)
	}
}

// ============================================================================
// ViewJSONOutput Structure Tests
// ============================================================================

// TestViewJSONOutput_Structure and the sub-progress/parent-issue tests below
// build their output with the real buildViewJSONOutput and assert on what it
// produced, so a regression in the mapping (dropped assignees, mis-derived
// progress, unset parent) fails the test. Previously they hand-built a
// ViewJSONOutput and round-tripped it through encoding/json, which only ever
// exercised the standard library.
func TestViewJSONOutput_Structure(t *testing.T) {
	issue := &api.Issue{
		Number:    42,
		Title:     "Test Issue",
		State:     "OPEN",
		Body:      "Issue body",
		URL:       "https://github.com/owner/repo/issues/42",
		Author:    api.Actor{Login: "testuser"},
		Assignees: []api.Actor{{Login: "user1"}, {Login: "user2"}},
		Labels:    []api.Label{{Name: "bug"}, {Name: "urgent"}},
		Milestone: &api.Milestone{Title: "v1.0"},
	}
	fieldValues := []api.FieldValue{
		{Field: "Status", Value: "In Progress"},
		{Field: "Priority", Value: "High"},
	}

	output := buildViewJSONOutput(issue, fieldValues, nil, nil, nil)

	if output.Number != 42 {
		t.Errorf("Expected Number 42, got %d", output.Number)
	}
	if output.Title != "Test Issue" {
		t.Errorf("Expected Title 'Test Issue', got %s", output.Title)
	}
	if output.State != "OPEN" {
		t.Errorf("Expected State 'OPEN', got %s", output.State)
	}
	if output.Body != "Issue body" {
		t.Errorf("Expected Body 'Issue body', got %s", output.Body)
	}
	if output.Author != "testuser" {
		t.Errorf("Expected Author 'testuser', got %s", output.Author)
	}
	if output.Milestone != "v1.0" {
		t.Errorf("Expected Milestone 'v1.0', got %s", output.Milestone)
	}
	if len(output.Assignees) != 2 || output.Assignees[0] != "user1" || output.Assignees[1] != "user2" {
		t.Errorf("Expected assignees [user1 user2], got %v", output.Assignees)
	}
	if len(output.Labels) != 2 || output.Labels[0] != "bug" || output.Labels[1] != "urgent" {
		t.Errorf("Expected labels [bug urgent], got %v", output.Labels)
	}
	if output.FieldValues["Status"] != "In Progress" {
		t.Errorf("Expected Status 'In Progress', got %s", output.FieldValues["Status"])
	}
	if output.FieldValues["Priority"] != "High" {
		t.Errorf("Expected Priority 'High', got %s", output.FieldValues["Priority"])
	}

	// Absent relationships must stay absent.
	if output.SubProgress != nil {
		t.Errorf("Expected no SubProgress without sub-issues, got %+v", output.SubProgress)
	}
	if output.ParentIssue != nil {
		t.Errorf("Expected no ParentIssue when none passed, got %+v", output.ParentIssue)
	}

	// The mapped struct must survive encoding as the command emits it.
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("Failed to marshal buildViewJSONOutput result: %v", err)
	}
	var parsed ViewJSONOutput
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal emitted JSON: %v", err)
	}
	if parsed.Number != 42 || parsed.FieldValues["Status"] != "In Progress" {
		t.Errorf("Emitted JSON lost data: %+v", parsed)
	}
}

func TestViewJSONOutput_EmptyAssigneesAndLabelsAreArrays(t *testing.T) {
	issue := &api.Issue{Number: 1, Title: "Bare", State: "OPEN", URL: "url"}

	output := buildViewJSONOutput(issue, nil, nil, nil, nil)

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// buildViewJSONOutput uses make(...) so these encode as [] rather than null.
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if _, ok := raw["assignees"].([]interface{}); !ok {
		t.Errorf("Expected assignees to encode as an array, got %T", raw["assignees"])
	}
	if _, ok := raw["labels"].([]interface{}); !ok {
		t.Errorf("Expected labels to encode as an array, got %T", raw["labels"])
	}
	if output.Milestone != "" {
		t.Errorf("Expected empty milestone when issue has none, got %q", output.Milestone)
	}
}

func TestViewJSONOutput_WithSubProgress(t *testing.T) {
	issue := &api.Issue{Number: 42, Title: "Parent", State: "OPEN", URL: "https://example.com"}
	subIssues := []api.SubIssue{
		{Number: 1, Title: "Sub 1", State: "CLOSED", URL: "url1"},
		{Number: 2, Title: "Sub 2", State: "OPEN", URL: "url2"},
	}

	output := buildViewJSONOutput(issue, nil, subIssues, nil, nil)

	if len(output.SubIssues) != 2 {
		t.Fatalf("Expected 2 sub-issues, got %d", len(output.SubIssues))
	}
	if output.SubIssues[0].Number != 1 || output.SubIssues[0].Title != "Sub 1" {
		t.Errorf("Expected first sub-issue #1 'Sub 1', got %+v", output.SubIssues[0])
	}

	if output.SubProgress == nil {
		t.Fatal("Expected SubProgress to be present")
	}
	// Progress is derived from sub-issue state, not supplied.
	if output.SubProgress.Total != 2 {
		t.Errorf("Expected Total 2, got %d", output.SubProgress.Total)
	}
	if output.SubProgress.Completed != 1 {
		t.Errorf("Expected Completed 1 (one CLOSED), got %d", output.SubProgress.Completed)
	}
	if output.SubProgress.Percentage != 50 {
		t.Errorf("Expected 50%% progress, got %d%%", output.SubProgress.Percentage)
	}
}

func TestViewJSONOutput_SubProgressAllClosed(t *testing.T) {
	issue := &api.Issue{Number: 42, Title: "Parent", State: "OPEN", URL: "url"}
	subIssues := []api.SubIssue{
		{Number: 1, Title: "Sub 1", State: "CLOSED"},
		{Number: 2, Title: "Sub 2", State: "CLOSED"},
	}

	output := buildViewJSONOutput(issue, nil, subIssues, nil, nil)

	if output.SubProgress == nil {
		t.Fatal("Expected SubProgress to be present")
	}
	if output.SubProgress.Completed != 2 || output.SubProgress.Percentage != 100 {
		t.Errorf("Expected 2 completed at 100%%, got %d at %d%%",
			output.SubProgress.Completed, output.SubProgress.Percentage)
	}
}

func TestViewJSONOutput_WithParentIssue(t *testing.T) {
	issue := &api.Issue{Number: 42, Title: "Sub-Issue", State: "OPEN", URL: "https://example.com"}
	parent := &api.Issue{Number: 10, Title: "Parent Issue", URL: "https://example.com/10"}

	output := buildViewJSONOutput(issue, nil, nil, parent, nil)

	if output.ParentIssue == nil {
		t.Fatal("Expected ParentIssue to be present")
	}
	if output.ParentIssue.Number != 10 {
		t.Errorf("Expected parent number 10, got %d", output.ParentIssue.Number)
	}
	if output.ParentIssue.Title != "Parent Issue" {
		t.Errorf("Expected parent title 'Parent Issue', got %s", output.ParentIssue.Title)
	}
	if output.ParentIssue.URL != "https://example.com/10" {
		t.Errorf("Expected parent url to match, got %s", output.ParentIssue.URL)
	}
}

func TestViewJSONOutput_WithComments(t *testing.T) {
	issue := &api.Issue{Number: 42, Title: "Commented", State: "OPEN", URL: "url"}
	comments := []api.Comment{
		{Author: "alice", Body: "First comment", CreatedAt: "2026-01-01T00:00:00Z"},
		{Author: "bob", Body: "Second comment", CreatedAt: "2026-01-02T00:00:00Z"},
	}

	output := buildViewJSONOutput(issue, nil, nil, nil, comments)

	if len(output.Comments) != 2 {
		t.Fatalf("Expected 2 comments, got %d", len(output.Comments))
	}
	if output.Comments[0].Author != "alice" || output.Comments[0].Body != "First comment" {
		t.Errorf("Expected first comment from alice, got %+v", output.Comments[0])
	}
	if output.Comments[1].CreatedAt != "2026-01-02T00:00:00Z" {
		t.Errorf("Expected second comment timestamp to map, got %q", output.Comments[1].CreatedAt)
	}
}

func TestSubIssueJSON_Structure(t *testing.T) {
	sub := SubIssueJSON{
		Number: 43,
		Title:  "Sub-Issue Title",
		State:  "CLOSED",
		URL:    "https://github.com/owner/repo/issues/43",
	}

	data, err := json.Marshal(sub)
	if err != nil {
		t.Fatalf("Failed to marshal SubIssueJSON: %v", err)
	}

	jsonStr := string(data)
	expectedFields := []string{"number", "title", "state", "url"}
	for _, field := range expectedFields {
		if !bytes.Contains(data, []byte(field)) {
			t.Errorf("Expected JSON to contain field %q, got: %s", field, jsonStr)
		}
	}
}

// TestSubProgressJSON_Structure verifies the sub-progress block as the view
// command actually emits it: derived by buildViewJSONOutput from sub-issue
// state, then encoded. It previously round-tripped a hand-built struct, which
// asserted nothing about how progress is computed or named on the wire.
func TestSubProgressJSON_Structure(t *testing.T) {
	subIssues := make([]api.SubIssue, 0, 10)
	for i := 1; i <= 10; i++ {
		state := "OPEN"
		if i <= 6 {
			state = "CLOSED"
		}
		subIssues = append(subIssues, api.SubIssue{Number: i, Title: "Sub", State: state})
	}

	issue := &api.Issue{Number: 1, Title: "Parent", State: "OPEN", URL: "url"}
	output := buildViewJSONOutput(issue, nil, subIssues, nil, nil)

	if output.SubProgress == nil {
		t.Fatal("Expected SubProgress to be present")
	}
	if output.SubProgress.Total != 10 {
		t.Errorf("Expected Total 10, got %d", output.SubProgress.Total)
	}
	if output.SubProgress.Completed != 6 {
		t.Errorf("Expected Completed 6, got %d", output.SubProgress.Completed)
	}
	if output.SubProgress.Percentage != 60 {
		t.Errorf("Expected Percentage 60, got %d", output.SubProgress.Percentage)
	}

	data, err := json.Marshal(output.SubProgress)
	if err != nil {
		t.Fatalf("Failed to marshal SubProgress: %v", err)
	}
	for _, field := range []string{"total", "completed", "percentage"} {
		if !bytes.Contains(data, []byte(field)) {
			t.Errorf("Expected emitted JSON to contain field %q, got: %s", field, data)
		}
	}
}

// ============================================================================
// Body File Tests
// ============================================================================

func TestViewCommand_HasBodyFileFlag(t *testing.T) {
	cmd := NewRootCommand()
	viewCmd, _, err := cmd.Find([]string{"view"})
	if err != nil {
		t.Fatalf("view command not found: %v", err)
	}

	flag := viewCmd.Flags().Lookup("body-file")
	if flag == nil {
		t.Fatal("Expected --body-file flag to exist")
	}

	// Check shorthand
	if flag.Shorthand != "b" {
		t.Errorf("Expected --body-file shorthand to be 'b', got %s", flag.Shorthand)
	}
}

func TestWriteBodyToFile_Success(t *testing.T) {
	// Create a temp directory for testing
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	var buf bytes.Buffer
	err := writeBodyToFile(&buf, 42, "Test body content\n\nWith multiple lines.")
	if err != nil {
		t.Fatalf("writeBodyToFile() error = %v", err)
	}

	// Verify file was created
	filePath := filepath.Join("tmp", "issue-42.md")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	expected := "Test body content\n\nWith multiple lines."
	if string(content) != expected {
		t.Errorf("File content = %q, want %q", string(content), expected)
	}
}

func TestWriteBodyToFile_CreatesTmpDirectory(t *testing.T) {
	// Create a temp directory for testing
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Verify tmp doesn't exist
	if _, err := os.Stat("tmp"); !os.IsNotExist(err) {
		t.Fatal("tmp directory should not exist before test")
	}

	var buf bytes.Buffer
	err := writeBodyToFile(&buf, 123, "Body content")
	if err != nil {
		t.Fatalf("writeBodyToFile() error = %v", err)
	}

	// Verify tmp directory was created
	info, err := os.Stat("tmp")
	if err != nil {
		t.Fatalf("tmp directory should exist after writeBodyToFile: %v", err)
	}
	if !info.IsDir() {
		t.Error("tmp should be a directory")
	}
}

func TestWriteBodyToFile_EmptyBody(t *testing.T) {
	// Create a temp directory for testing
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	var buf bytes.Buffer
	err := writeBodyToFile(&buf, 99, "")
	if err != nil {
		t.Fatalf("writeBodyToFile() error = %v", err)
	}

	// Verify file was created with empty content
	filePath := filepath.Join("tmp", "issue-99.md")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	if string(content) != "" {
		t.Errorf("File content = %q, want empty string", string(content))
	}
}

func TestRunViewWithDeps_BodyFile(t *testing.T) {
	// Create a temp directory for testing
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	mock := newMockViewClient()
	mock.issue = &api.Issue{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "testuser"},
		Body:   "This is the issue body for testing.",
	}

	cmd := newViewCommand()
	opts := &viewOptions{bodyFile: true}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file was created with correct content
	filePath := filepath.Join("tmp", "issue-42.md")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	expected := "This is the issue body for testing."
	if string(content) != expected {
		t.Errorf("File content = %q, want %q", string(content), expected)
	}
}

// ============================================================================
// Body Stdout Tests
// ============================================================================

func TestViewCommand_HasBodyStdoutFlag(t *testing.T) {
	cmd := NewRootCommand()
	viewCmd, _, err := cmd.Find([]string{"view"})
	if err != nil {
		t.Fatalf("view command not found: %v", err)
	}

	flag := viewCmd.Flags().Lookup("body-stdout")
	if flag == nil {
		t.Fatal("Expected --body-stdout flag to exist")
	}

	// body-stdout has no shorthand (b is taken by body-file)
	if flag.Shorthand != "" {
		t.Errorf("Expected --body-stdout to have no shorthand, got %s", flag.Shorthand)
	}
}

func TestRunViewWithDeps_BodyStdout(t *testing.T) {
	mock := newMockViewClient()
	mock.issue = &api.Issue{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "testuser"},
		Body:   "This is the issue body for stdout testing.",
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newViewCommand()
	opts := &viewOptions{bodyStdout: true}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 42)

	// Restore stdout and read captured output
	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "This is the issue body for stdout testing."
	if buf.String() != expected {
		t.Errorf("stdout output = %q, want %q", buf.String(), expected)
	}
}

func TestRunViewWithDeps_BodyStdout_EmptyBody(t *testing.T) {
	mock := newMockViewClient()
	mock.issue = &api.Issue{
		Number: 99,
		Title:  "Empty Body Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/99",
		Author: api.Actor{Login: "testuser"},
		Body:   "",
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newViewCommand()
	opts := &viewOptions{bodyStdout: true}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 99)

	// Restore stdout and read captured output
	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("stdout output = %q, want empty string", buf.String())
	}
}

// ============================================================================
// Flag Mutual Exclusivity Tests
// ============================================================================

func TestRunViewWithDeps_JSONWithBodyFileError(t *testing.T) {
	mock := newMockViewClient()
	cmd := newViewCommand()

	opts := &viewOptions{jsonFields: "title", bodyFile: true}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 42)

	if err == nil {
		t.Fatal("expected error for --json with --body-file")
	}
	if !strings.Contains(err.Error(), "cannot use --json with --body-file") {
		t.Errorf("expected mutual exclusivity error, got: %v", err)
	}
}

func TestRunViewWithDeps_JSONWithBodyStdoutError(t *testing.T) {
	mock := newMockViewClient()
	cmd := newViewCommand()

	opts := &viewOptions{jsonFields: "title", bodyStdout: true}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 42)

	if err == nil {
		t.Fatal("expected error for --json with --body-stdout")
	}
	if !strings.Contains(err.Error(), "cannot use --json with --body-stdout") {
		t.Errorf("expected mutual exclusivity error, got: %v", err)
	}
}

func TestRunViewWithDeps_JSONWithCommentsError(t *testing.T) {
	mock := newMockViewClient()
	cmd := newViewCommand()

	opts := &viewOptions{jsonFields: "title", comments: true}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 42)

	if err == nil {
		t.Fatal("expected error for --json with --comments")
	}
	if !strings.Contains(err.Error(), "cannot use --json with --comments") {
		t.Errorf("expected mutual exclusivity error, got: %v", err)
	}
}

func TestRunViewWithDeps_JQWithoutJSONError(t *testing.T) {
	mock := newMockViewClient()
	cmd := newViewCommand()

	opts := &viewOptions{jq: ".title"}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 42)

	if err == nil {
		t.Fatal("expected error for --jq without --json")
	}
	if !strings.Contains(err.Error(), "--jq requires --json") {
		t.Errorf("expected jq requires json error, got: %v", err)
	}
}

func TestRunViewWithDeps_TemplateNotSupportedError(t *testing.T) {
	mock := newMockViewClient()
	cmd := newViewCommand()

	opts := &viewOptions{template: "{{.title}}"}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 42)

	if err == nil {
		t.Fatal("expected error for --template")
	}
	// Should mention --template is not supported
	if !strings.Contains(err.Error(), "--template is not supported") {
		t.Errorf("expected '--template is not supported' error, got: %v", err)
	}
	// Should mention gh issue view as alternative
	if !strings.Contains(err.Error(), "gh issue view") {
		t.Errorf("expected error to mention 'gh issue view', got: %v", err)
	}
	// Should mention --jq for project fields
	if !strings.Contains(err.Error(), "--jq") {
		t.Errorf("expected error to mention '--jq', got: %v", err)
	}
}

// ============================================================================
// JSON Output with Project Fields Tests
// ============================================================================

func TestRunViewWithDeps_JSONWithProjectFields(t *testing.T) {
	mock := newMockViewClient()
	mock.issue = &api.Issue{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "testuser"},
	}
	mock.fieldValues = []api.FieldValue{
		{Field: "Status", Value: "In Progress"},
		{Field: "Priority", Value: "P1"},
		{Field: "Branch", Value: "release/v1.0"},
	}

	cmd := newViewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &viewOptions{jsonFields: "fieldValues"}
	err := runViewWithDeps(cmd, opts, mock, "owner", "repo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Note: JSON is written to stdout, not the buffer
	// We verify no error occurred and the function completed
}

func TestBuildViewJSONOutput_FieldValues(t *testing.T) {
	issue := &api.Issue{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/42",
		Author: api.Actor{Login: "testuser"},
	}
	fieldValues := []api.FieldValue{
		{Field: "Status", Value: "In Progress"},
		{Field: "Priority", Value: "P1"},
		{Field: "Branch", Value: "release/v1.0"},
	}

	output := buildViewJSONOutput(issue, fieldValues, nil, nil, nil)

	if output.FieldValues["Status"] != "In Progress" {
		t.Errorf("expected Status 'In Progress', got %q", output.FieldValues["Status"])
	}
	if output.FieldValues["Priority"] != "P1" {
		t.Errorf("expected Priority 'P1', got %q", output.FieldValues["Priority"])
	}
	if output.FieldValues["Branch"] != "release/v1.0" {
		t.Errorf("expected Branch 'release/v1.0', got %q", output.FieldValues["Branch"])
	}
}

func TestFilterViewJSONFields_SelectsRequestedFields(t *testing.T) {
	output := ViewJSONOutput{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		Body:   "Body text",
		URL:    "https://example.com",
		Author: "testuser",
	}

	result := filterViewJSONFields(output, []string{"number", "title"})

	if result["number"] != 42 {
		t.Errorf("expected number 42, got %v", result["number"])
	}
	if result["title"] != "Test Issue" {
		t.Errorf("expected title 'Test Issue', got %v", result["title"])
	}
	if _, exists := result["body"]; exists {
		t.Error("expected body to be excluded")
	}
	if _, exists := result["state"]; exists {
		t.Error("expected state to be excluded")
	}
}

func TestParseJSONFields_CommaSeparated(t *testing.T) {
	result := parseJSONFields("number,title,state", viewAvailableFields)

	if len(result) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(result))
	}
	if result[0] != "number" || result[1] != "title" || result[2] != "state" {
		t.Errorf("unexpected fields: %v", result)
	}
}

func TestParseJSONFields_EmptyReturnsAll(t *testing.T) {
	result := parseJSONFields("", viewAvailableFields)

	if len(result) != len(viewAvailableFields) {
		t.Errorf("expected all %d fields, got %d", len(viewAvailableFields), len(result))
	}
}

// ============================================================================
// Project Field Shorthand Tests (Issue #668)
// ============================================================================

func TestViewAvailableFields_IncludesProjectFields(t *testing.T) {
	// Verify status, priority, branch are in available fields
	projectFields := []string{"status", "priority", "branch"}
	for _, field := range projectFields {
		found := false
		for _, available := range viewAvailableFields {
			if available == field {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q to be in viewAvailableFields", field)
		}
	}
}

func TestFilterViewJSONFields_StatusFromFieldValues(t *testing.T) {
	output := ViewJSONOutput{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://example.com",
		Author: "testuser",
		FieldValues: map[string]string{
			"Status":   "In Progress",
			"Priority": "P1",
		},
	}

	result := filterViewJSONFields(output, []string{"status"})

	if result["status"] != "In Progress" {
		t.Errorf("expected status 'In Progress', got %v", result["status"])
	}
}

func TestFilterViewJSONFields_PriorityFromFieldValues(t *testing.T) {
	output := ViewJSONOutput{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://example.com",
		Author: "testuser",
		FieldValues: map[string]string{
			"Status":   "In Progress",
			"Priority": "P1",
		},
	}

	result := filterViewJSONFields(output, []string{"priority"})

	if result["priority"] != "P1" {
		t.Errorf("expected priority 'P1', got %v", result["priority"])
	}
}

func TestFilterViewJSONFields_BranchFromFieldValues(t *testing.T) {
	output := ViewJSONOutput{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://example.com",
		Author: "testuser",
		FieldValues: map[string]string{
			"Status": "In Progress",
			"Branch": "release/v1.0",
		},
	}

	result := filterViewJSONFields(output, []string{"branch"})

	if result["branch"] != "release/v1.0" {
		t.Errorf("expected branch 'release/v1.0', got %v", result["branch"])
	}
}

func TestFilterViewJSONFields_StatusNullWhenNotInProject(t *testing.T) {
	output := ViewJSONOutput{
		Number:      42,
		Title:       "Test Issue",
		State:       "OPEN",
		URL:         "https://example.com",
		Author:      "testuser",
		FieldValues: map[string]string{}, // No project fields
	}

	result := filterViewJSONFields(output, []string{"status"})

	if result["status"] != nil {
		t.Errorf("expected status nil when not in project, got %v", result["status"])
	}
}

func TestFilterViewJSONFields_PriorityNullWhenNotSet(t *testing.T) {
	output := ViewJSONOutput{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://example.com",
		Author: "testuser",
		FieldValues: map[string]string{
			"Status": "In Progress",
			// Priority not set
		},
	}

	result := filterViewJSONFields(output, []string{"priority"})

	if result["priority"] != nil {
		t.Errorf("expected priority nil when not set, got %v", result["priority"])
	}
}

func TestFilterViewJSONFields_MultipleProjectFields(t *testing.T) {
	output := ViewJSONOutput{
		Number: 42,
		Title:  "Test Issue",
		State:  "OPEN",
		URL:    "https://example.com",
		Author: "testuser",
		FieldValues: map[string]string{
			"Status":   "Done",
			"Priority": "P0",
			"Branch":   "patch/v1.1.5",
		},
	}

	result := filterViewJSONFields(output, []string{"status", "priority", "branch"})

	if result["status"] != "Done" {
		t.Errorf("expected status 'Done', got %v", result["status"])
	}
	if result["priority"] != "P0" {
		t.Errorf("expected priority 'P0', got %v", result["priority"])
	}
	if result["branch"] != "patch/v1.1.5" {
		t.Errorf("expected branch 'patch/v1.1.5', got %v", result["branch"])
	}
}

func TestFilterViewJSONFields_AllStatusValues(t *testing.T) {
	// Test that all standard status values work
	statuses := []string{"Backlog", "In Progress", "In Review", "Done"}
	for _, status := range statuses {
		output := ViewJSONOutput{
			Number: 42,
			Title:  "Test Issue",
			State:  "OPEN",
			URL:    "https://example.com",
			Author: "testuser",
			FieldValues: map[string]string{
				"Status": status,
			},
		}

		result := filterViewJSONFields(output, []string{"status"})

		if result["status"] != status {
			t.Errorf("expected status %q, got %v", status, result["status"])
		}
	}
}

// ============================================================================
// Multi-Issue Tests
// ============================================================================

func newMultiMockViewClient() *mockViewClient {
	return &mockViewClient{
		issues: map[int]*api.Issue{
			42: {Number: 42, Title: "First Issue", State: "OPEN", URL: "https://github.com/o/r/issues/42", Author: api.Actor{Login: "user1"}},
			43: {Number: 43, Title: "Second Issue", State: "CLOSED", URL: "https://github.com/o/r/issues/43", Author: api.Actor{Login: "user2"}},
		},
		fieldValuesMap: map[int][]api.FieldValue{
			42: {{Field: "Status", Value: "In progress"}},
			43: {{Field: "Status", Value: "Done"}},
		},
		subIssuesMap:    map[int][]api.SubIssue{},
		parentIssuesMap: map[int]*api.Issue{},
	}
}

func TestRunViewMulti_JSONArray(t *testing.T) {
	mock := newMultiMockViewClient()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := &cobra.Command{}
	cmd.SetOut(w)
	opts := &viewOptions{jsonFields: "number,title,state"}
	refs := []viewIssueRef{
		{owner: "o", repo: "r", number: 42},
		{owner: "o", repo: "r", number: 43},
	}

	err := runViewMulti(cmd, opts, mock, refs, nil)

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	var result []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("expected valid JSON array, got error: %v\noutput: %s", err, output)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 items in array, got %d", len(result))
	}

	if result[0]["number"].(float64) != 42 {
		t.Errorf("expected first issue number 42, got %v", result[0]["number"])
	}
	if result[1]["number"].(float64) != 43 {
		t.Errorf("expected second issue number 43, got %v", result[1]["number"])
	}
}

func TestRunViewMulti_TableWithSeparator(t *testing.T) {
	mock := newMultiMockViewClient()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := &cobra.Command{}
	cmd.SetOut(w)
	opts := &viewOptions{}
	refs := []viewIssueRef{
		{owner: "o", repo: "r", number: 42},
		{owner: "o", repo: "r", number: 43},
	}

	err := runViewMulti(cmd, opts, mock, refs, nil)

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Should contain both issue titles
	if !strings.Contains(output, "First Issue") {
		t.Error("expected output to contain 'First Issue'")
	}
	if !strings.Contains(output, "Second Issue") {
		t.Error("expected output to contain 'Second Issue'")
	}
	// Should contain separator
	if !strings.Contains(output, "\u2550") {
		t.Error("expected output to contain separator line")
	}
}

func TestRunViewMulti_SameNumberDifferentRepos(t *testing.T) {
	// #868 finding 3: viewing two same-numbered issues from different repos must
	// not collide — keying result maps by number alone lets the second repo's data
	// overwrite the first's, and refOrder then emits the same issue twice.
	mock := &mockViewClient{
		issuesByRepo: map[string]map[int]*api.Issue{
			"o1/r1": {5: {Number: 5, Title: "Repo1 Issue Five", State: "OPEN", URL: "https://github.com/o1/r1/issues/5", Author: api.Actor{Login: "u1"}}},
			"o2/r2": {5: {Number: 5, Title: "Repo2 Issue Five", State: "OPEN", URL: "https://github.com/o2/r2/issues/5", Author: api.Actor{Login: "u2"}}},
		},
		fieldValuesMap:  map[int][]api.FieldValue{},
		subIssuesMap:    map[int][]api.SubIssue{},
		parentIssuesMap: map[int]*api.Issue{},
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := &cobra.Command{}
	cmd.SetOut(w)
	opts := &viewOptions{}
	refs := []viewIssueRef{
		{owner: "o1", repo: "r1", number: 5},
		{owner: "o2", repo: "r2", number: 5},
	}

	err := runViewMulti(cmd, opts, mock, refs, nil)

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Repo1 Issue Five") {
		t.Errorf("expected repo1's issue #5 in output, got: %s", output)
	}
	if !strings.Contains(output, "Repo2 Issue Five") {
		t.Errorf("expected repo2's issue #5 in output (collision would drop it), got: %s", output)
	}
}

func TestRunViewMulti_InvalidIssuePartialSuccess(t *testing.T) {
	mock := newMultiMockViewClient()
	mock.issueErrors = map[int]error{
		99: errors.New("issue not found"),
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Capture stderr too
	oldErr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	cmd := &cobra.Command{}
	cmd.SetOut(w)
	opts := &viewOptions{jsonFields: "number,title"}
	refs := []viewIssueRef{
		{owner: "o", repo: "r", number: 42},
		{owner: "o", repo: "r", number: 99},
	}

	err := runViewMulti(cmd, opts, mock, refs, nil)

	w.Close()
	wErr.Close()
	os.Stdout = old
	os.Stderr = oldErr
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	var errBuf bytes.Buffer
	_, _ = io.Copy(&errBuf, rErr)

	// #938: the loaded issues are still printed, but a view that could not load
	// every requested issue is a partial success, not a clean exit.
	if !api.IsPartialSuccess(err) || !strings.Contains(err.Error(), "#99") {
		t.Fatalf("expected a partial-success error naming #99, got: %v", err)
	}

	// Should output valid JSON with just issue 42
	var result []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON array: %v\noutput: %s", err, buf.String())
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 item (partial success), got %d", len(result))
	}
	if result[0]["number"].(float64) != 42 {
		t.Errorf("expected issue 42, got %v", result[0]["number"])
	}

	// Should warn on stderr about issue 99
	if !strings.Contains(errBuf.String(), "#99") {
		t.Errorf("expected stderr warning about #99, got: %s", errBuf.String())
	}
}

func TestRunViewMulti_BodyStdoutError(t *testing.T) {
	mock := newMultiMockViewClient()
	cmd := &cobra.Command{}
	opts := &viewOptions{bodyStdout: true}
	refs := []viewIssueRef{
		{owner: "o", repo: "r", number: 42},
		{owner: "o", repo: "r", number: 43},
	}

	err := runViewMulti(cmd, opts, mock, refs, nil)
	if err == nil {
		t.Fatal("expected error for --body-stdout with multiple issues")
	}
	if !strings.Contains(err.Error(), "only supported for single issue") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRunViewMulti_BodyFileError(t *testing.T) {
	mock := newMultiMockViewClient()
	cmd := &cobra.Command{}
	opts := &viewOptions{bodyFile: true}
	refs := []viewIssueRef{
		{owner: "o", repo: "r", number: 42},
		{owner: "o", repo: "r", number: 43},
	}

	err := runViewMulti(cmd, opts, mock, refs, nil)
	if err == nil {
		t.Fatal("expected error for --body-file with multiple issues")
	}
	if !strings.Contains(err.Error(), "only supported for single issue") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRunViewMulti_JQWithArray(t *testing.T) {
	mock := newMultiMockViewClient()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := &cobra.Command{}
	cmd.SetOut(w)
	opts := &viewOptions{jsonFields: "number,title", jq: ".[].number"}
	refs := []viewIssueRef{
		{owner: "o", repo: "r", number: 42},
		{owner: "o", repo: "r", number: 43},
	}

	err := runViewMulti(cmd, opts, mock, refs, nil)

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := strings.TrimSpace(buf.String())
	// jq .[].number should output "42\n43"
	if !strings.Contains(output, "42") || !strings.Contains(output, "43") {
		t.Errorf("expected jq output to contain 42 and 43, got: %s", output)
	}
}

func TestViewCommand_AcceptsMultipleArgs(t *testing.T) {
	cmd := newViewCommand()
	// The args validator should accept 2+ args
	err := cmd.Args(cmd, []string{"42", "43"})
	if err != nil {
		t.Errorf("expected no error for multiple args, got: %v", err)
	}
}

func TestViewCommand_AcceptsSingleArg(t *testing.T) {
	cmd := newViewCommand()
	err := cmd.Args(cmd, []string{"42"})
	if err != nil {
		t.Errorf("expected no error for single arg, got: %v", err)
	}
}

func TestViewCommand_RejectsZeroArgs(t *testing.T) {
	cmd := newViewCommand()
	err := cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error for zero args")
	}
}

// ============================================================================
// stripEmptySubIssuesSection + branch-tracker rendering (Issue #838)
// ============================================================================

func TestStripEmptySubIssuesSection_RemovesPlaceholder(t *testing.T) {
	body := "## Branch: foo\n\n### Workflow\n- bar\n\n### Sub-Issues\n\nIssues assigned to this branch appear as sub-issues below.\n"
	got := stripEmptySubIssuesSection(body)
	if strings.Contains(got, "### Sub-Issues") {
		t.Errorf("expected `### Sub-Issues` heading stripped, got:\n%s", got)
	}
	if strings.Contains(got, "Issues assigned to this branch appear as sub-issues below.") {
		t.Errorf("expected placeholder text stripped, got:\n%s", got)
	}
	if !strings.Contains(got, "### Workflow") {
		t.Errorf("expected preceding content preserved, got:\n%s", got)
	}
}

func TestStripEmptySubIssuesSection_NoHeading(t *testing.T) {
	body := "## Branch: foo\n\n### Workflow\n- bar\n"
	got := stripEmptySubIssuesSection(body)
	if got != body {
		t.Errorf("expected unchanged body, got:\n%s", got)
	}
}

func TestStripEmptySubIssuesSection_HeadingWithRealContent_Preserved(t *testing.T) {
	body := "## Branch: foo\n\n### Sub-Issues\n\n- #42 something real\n- #43 also real\n"
	got := stripEmptySubIssuesSection(body)
	if got != body {
		t.Errorf("expected body with real sub-issue content to be preserved unchanged, got:\n%s", got)
	}
}

// AC 5 (a): branch tracker with 0 sub-issues — placeholder must not appear in output.
func TestOutputViewTable_BranchTracker_NoSubIssues_HidesPlaceholder(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 211,
		Title:  "Branch tracker isd/0.13.0",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/211",
		Author: api.Actor{Login: "author"},
		Labels: []api.Label{{Name: "branch"}},
		Body:   "## Branch: isd/0.13.0\n\n### Workflow\n- stuff\n\n### Sub-Issues\n\nIssues assigned to this branch appear as sub-issues below.\n",
	}

	if err := outputViewTable(cmd, issue, nil, nil, nil, nil); err != nil {
		t.Fatalf("outputViewTable error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Issues assigned to this branch appear as sub-issues below.") {
		t.Errorf("expected placeholder text absent from rendered output, got:\n%s", out)
	}
	if strings.Contains(out, "### Sub-Issues") {
		t.Errorf("expected `### Sub-Issues` heading absent from rendered output, got:\n%s", out)
	}
}

// AC 5 (b): branch tracker with ≥1 sub-issue — top block renders list, placeholder absent.
func TestOutputViewTable_BranchTracker_WithSubIssues_HidesPlaceholder(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createViewTestCmd(buf)

	issue := &api.Issue{
		Number: 211,
		Title:  "Branch tracker isd/0.13.0",
		State:  "OPEN",
		URL:    "https://github.com/owner/repo/issues/211",
		Author: api.Actor{Login: "author"},
		Labels: []api.Label{{Name: "branch"}},
		Body:   "## Branch: isd/0.13.0\n\n### Workflow\n- stuff\n\n### Sub-Issues\n\nIssues assigned to this branch appear as sub-issues below.\n",
	}
	subs := []api.SubIssue{
		{Number: 210, Title: "First", State: "OPEN"},
	}

	if err := outputViewTable(cmd, issue, nil, subs, nil, nil); err != nil {
		t.Fatalf("outputViewTable error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Issues assigned to this branch appear as sub-issues below.") {
		t.Errorf("expected placeholder text absent from rendered output, got:\n%s", out)
	}
	// Top-block list (regression guard — AC 4) still renders.
	if !strings.Contains(out, "Sub-Issues:") {
		t.Errorf("expected top-block `Sub-Issues:` header to still render, got:\n%s", out)
	}
	if !strings.Contains(out, "#210") {
		t.Errorf("expected sub-issue #210 to still render, got:\n%s", out)
	}
}
