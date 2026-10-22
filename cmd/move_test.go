package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/spf13/cobra"
)

func TestRunMoveWithDeps_RecursiveEnrichmentFailureWarns(t *testing.T) {
	// #871 finding 8: when the recursive enrichment query (GetProjectItemsByIssues)
	// fails, the failure was swallowed and the affected sub-issues were misreported
	// as "(not in project, will skip)". A warning must now be emitted.
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.issues["testowner/testrepo#1"] = &api.Issue{ID: "issue-1", Number: 1, Title: "Parent Issue"}
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	mock.subIssues["testowner/testrepo#1"] = []api.SubIssue{
		{ID: "issue-2", Number: 2, Title: "Sub", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	mock.getProjectItemsByIssuesErr = fmt.Errorf("enrichment API failure")

	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	oldErr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	opts := &moveOptions{status: "in_progress", recursive: true, yes: true, depth: 10}
	err := runMoveWithDeps(cmd, []string{"1"}, opts, cfg, mock)

	_ = w.Close()
	os.Stderr = oldErr
	var errBuf bytes.Buffer
	_, _ = io.Copy(&errBuf, r)

	if !strings.Contains(strings.ToLower(errBuf.String()), "enrich") {
		t.Errorf("expected a stderr warning about failed sub-issue enrichment, got: %q", errBuf.String())
	}
	// #938: the parent was moved but the sub-issues could not be, so the
	// command is a partial success rather than a clean exit.
	if !api.IsPartialSuccess(err) {
		t.Errorf("expected a partial-success error, got: %v", err)
	}
}

// A sub-issue tree that cannot be listed was skipped with only a warning, so a
// recursive move exited 0 having moved none of the subtree (#938).
func TestRunMoveWithDeps_RecursiveListingFailureIsPartialSuccess(t *testing.T) {
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.issues["testowner/testrepo#1"] = &api.Issue{ID: "issue-1", Number: 1, Title: "Parent Issue"}
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	mock.getSubIssuesBatchErr = fmt.Errorf("batch failed")
	mock.getSubIssuesErr = fmt.Errorf("sub-issue listing failed")

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	oldErr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	opts := &moveOptions{status: "in_progress", recursive: true, yes: true, depth: 10}
	err := runMoveWithDeps(cmd, []string{"1"}, opts, testMoveConfig(), mock)

	_ = w.Close()
	os.Stderr = oldErr

	if !api.IsPartialSuccess(err) || !strings.Contains(err.Error(), "#1") {
		t.Errorf("expected a partial-success error naming #1's sub-issues, got: %v", err)
	}
}

func TestCollectSubIssuesRecursive_ListingFailureReturnsError(t *testing.T) {
	mock := newMockMoveClient()
	mock.getSubIssuesBatchErr = fmt.Errorf("batch failed")
	mock.getSubIssuesErr = fmt.Errorf("sub-issue listing failed")

	oldErr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	_, err := collectSubIssuesRecursive(mock, "testowner", "testrepo", 1, map[string]string{}, map[string][]api.FieldValue{}, map[string]*api.Issue{}, 1, 10)
	_ = w.Close()
	os.Stderr = oldErr

	if err == nil || !strings.Contains(err.Error(), "#1") {
		t.Fatalf("expected an error naming #1's unlisted sub-issues, got: %v", err)
	}
}

// mockMoveClient implements moveClient for testing
type mockMoveClient struct {
	issues        map[string]*api.Issue // "owner/repo#number" -> Issue
	project       *api.Project
	projectFields []api.ProjectField
	projectItems  []api.ProjectItem
	subIssues     map[string][]api.SubIssue // "owner/repo#number" -> SubIssues
	fieldUpdates  []fieldUpdate             // track field updates for verification

	// Microsprint support
	openIssuesByLabel map[string][]api.Issue // label -> issues

	// Label tracking
	addLabelCalls    []labelCall // track AddLabelToIssue calls
	removeLabelCalls []labelCall // track RemoveLabelFromIssue calls

	// Call counters for caching verification
	getProjectFieldsCalls        int
	getProjectItemsCalls         int
	getProjectItemsByIssuesCalls int
	lastIssueRefs                []api.IssueRef // track refs passed to GetProjectItemsByIssues
	getSubIssuesCalls            int            // track per-issue calls
	getSubIssuesBatchCalls       int            // track batch calls
	batchUpdateCalls             int            // track batch mutation calls

	// Batch update configuration
	batchUpdateResults []api.BatchUpdateResult // custom results for BatchUpdateProjectItemFields
	batchUpdateErr     error                   // error to return from BatchUpdateProjectItemFields

	// Error injection
	getIssueErr                error
	getProjectErr              error
	getProjectFieldsErr        error
	getProjectItemsErr         error
	getProjectItemsByIssuesErr error
	getSubIssuesErr            error
	getSubIssuesBatchErr       error
	setProjectItemErr          error
	setProjectItemErrFor       map[string]error // itemID -> error
	getOpenIssuesByLabelErr    error
	addLabelErr                error
	removeLabelErr             error
}

type labelCall struct {
	owner     string
	repo      string
	issueID   string
	labelName string
}

type fieldUpdate struct {
	projectID string
	itemID    string
	fieldName string
	value     string
}

func newMockMoveClient() *mockMoveClient {
	return &mockMoveClient{
		issues:               make(map[string]*api.Issue),
		subIssues:            make(map[string][]api.SubIssue),
		openIssuesByLabel:    make(map[string][]api.Issue),
		setProjectItemErrFor: make(map[string]error),
	}
}

func (m *mockMoveClient) GetIssue(owner, repo string, number int) (*api.Issue, error) {
	if m.getIssueErr != nil {
		return nil, m.getIssueErr
	}
	key := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	if issue, ok := m.issues[key]; ok {
		return issue, nil
	}
	return nil, fmt.Errorf("issue not found: %s", key)
}

func (m *mockMoveClient) GetProject(owner string, number int) (*api.Project, error) {
	if m.getProjectErr != nil {
		return nil, m.getProjectErr
	}
	if m.project != nil {
		return m.project, nil
	}
	return nil, fmt.Errorf("project not found")
}

func (m *mockMoveClient) GetProjectItems(projectID string, filter *api.ProjectItemsFilter) ([]api.ProjectItem, error) {
	m.getProjectItemsCalls++
	if m.getProjectItemsErr != nil {
		return nil, m.getProjectItemsErr
	}
	return m.projectItems, nil
}

func (m *mockMoveClient) GetProjectItemsByIssues(projectID string, refs []api.IssueRef) ([]api.ProjectItem, error) {
	m.getProjectItemsByIssuesCalls++
	m.lastIssueRefs = refs
	if m.getProjectItemsByIssuesErr != nil {
		return nil, m.getProjectItemsByIssuesErr
	}
	// Filter projectItems to only return items matching the requested refs
	var result []api.ProjectItem
	for _, ref := range refs {
		for _, item := range m.projectItems {
			if item.Issue != nil &&
				item.Issue.Repository.Owner == ref.Owner &&
				item.Issue.Repository.Name == ref.Repo &&
				item.Issue.Number == ref.Number {
				result = append(result, item)
				break
			}
		}
	}
	return result, nil
}

func (m *mockMoveClient) GetSubIssues(owner, repo string, number int) ([]api.SubIssue, error) {
	m.getSubIssuesCalls++
	if m.getSubIssuesErr != nil {
		return nil, m.getSubIssuesErr
	}
	key := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	result := m.subIssues[key]
	return result, nil
}

func (m *mockMoveClient) GetSubIssuesBatch(owner, repo string, numbers []int) (map[int][]api.SubIssue, error) {
	m.getSubIssuesBatchCalls++
	if m.getSubIssuesBatchErr != nil {
		return nil, m.getSubIssuesBatchErr
	}
	if m.getSubIssuesErr != nil {
		return nil, m.getSubIssuesErr
	}
	result := make(map[int][]api.SubIssue)
	for _, num := range numbers {
		key := fmt.Sprintf("%s/%s#%d", owner, repo, num)
		result[num] = m.subIssues[key]
	}
	return result, nil
}

func (m *mockMoveClient) SetProjectItemField(projectID, itemID, fieldName, value string) error {
	if m.setProjectItemErr != nil {
		return m.setProjectItemErr
	}
	if err, ok := m.setProjectItemErrFor[itemID]; ok {
		return err
	}
	m.fieldUpdates = append(m.fieldUpdates, fieldUpdate{
		projectID: projectID,
		itemID:    itemID,
		fieldName: fieldName,
		value:     value,
	})
	return nil
}

func (m *mockMoveClient) SetProjectItemFieldWithFields(projectID, itemID, fieldName, value string, fields []api.ProjectField) error {
	// Delegate to the same logic as SetProjectItemField for testing
	return m.SetProjectItemField(projectID, itemID, fieldName, value)
}

func (m *mockMoveClient) BatchUpdateProjectItemFields(projectID string, updates []api.FieldUpdate, fields []api.ProjectField) ([]api.BatchUpdateResult, error) {
	m.batchUpdateCalls++

	if m.batchUpdateErr != nil {
		return nil, m.batchUpdateErr
	}

	// If custom results are set, return them
	if m.batchUpdateResults != nil {
		return m.batchUpdateResults, nil
	}

	// Default: simulate successful updates and track them
	var results []api.BatchUpdateResult
	for _, update := range updates {
		// Track the update
		m.fieldUpdates = append(m.fieldUpdates, fieldUpdate{
			projectID: projectID,
			itemID:    update.ItemID,
			fieldName: update.FieldName,
			value:     update.Value,
		})

		// Check if there's a specific error for this item
		if err, ok := m.setProjectItemErrFor[update.ItemID]; ok {
			results = append(results, api.BatchUpdateResult{
				ItemID:    update.ItemID,
				FieldName: update.FieldName,
				Success:   false,
				Error:     err.Error(),
			})
		} else {
			results = append(results, api.BatchUpdateResult{
				ItemID:    update.ItemID,
				FieldName: update.FieldName,
				Success:   true,
			})
		}
	}

	return results, nil
}

func (m *mockMoveClient) GetProjectFields(projectID string) ([]api.ProjectField, error) {
	m.getProjectFieldsCalls++
	if m.getProjectFieldsErr != nil {
		return nil, m.getProjectFieldsErr
	}
	if m.projectFields != nil {
		return m.projectFields, nil
	}
	// Return default test fields
	return []api.ProjectField{
		{ID: "STATUS_FIELD", Name: "Status", DataType: "SINGLE_SELECT", Options: []api.FieldOption{
			{ID: "OPT_BACKLOG", Name: "Backlog"},
			{ID: "OPT_IN_PROGRESS", Name: "In progress"},
			{ID: "OPT_DONE", Name: "Done"},
		}},
		{ID: "PRIORITY_FIELD", Name: "Priority", DataType: "SINGLE_SELECT", Options: []api.FieldOption{
			{ID: "OPT_P0", Name: "P0"},
			{ID: "OPT_P1", Name: "P1"},
			{ID: "OPT_P2", Name: "P2"},
		}},
		{ID: "MICROSPRINT_FIELD", Name: "Microsprint", DataType: "TEXT"},
		{ID: "RELEASE_FIELD", Name: "Release", DataType: "TEXT"},
	}, nil
}

func (m *mockMoveClient) GetOpenIssuesByLabel(owner, repo, label string) ([]api.Issue, error) {
	if m.getOpenIssuesByLabelErr != nil {
		return nil, m.getOpenIssuesByLabelErr
	}
	return m.openIssuesByLabel[label], nil
}

func (m *mockMoveClient) AddLabelToIssue(owner, repo, issueID, labelName string) error {
	m.addLabelCalls = append(m.addLabelCalls, labelCall{
		owner:     owner,
		repo:      repo,
		issueID:   issueID,
		labelName: labelName,
	})
	return m.addLabelErr
}

func (m *mockMoveClient) RemoveLabelFromIssue(owner, repo, issueID, labelName string) error {
	m.removeLabelCalls = append(m.removeLabelCalls, labelCall{
		owner:     owner,
		repo:      repo,
		issueID:   issueID,
		labelName: labelName,
	})
	return m.removeLabelErr
}

// Test helpers

func testMoveConfig() *config.Config {
	return &config.Config{
		Project: config.Project{
			Owner:  "testowner",
			Number: 1,
		},
		Repositories: []string{"testowner/testrepo"},
		Fields: map[string]config.Field{
			"status": {
				Field: "Status",
				Values: map[string]string{
					"backlog":     "Backlog",
					"in_progress": "In Progress",
					"done":        "Done",
					"todo":        "Todo",
				},
			},
			"priority": {
				Field: "Priority",
				Values: map[string]string{
					"high":   "High",
					"medium": "Medium",
					"low":    "Low",
				},
			},
		},
	}
}

func setupMockWithIssue(number int, title string, itemID string) *mockMoveClient {
	mock := newMockMoveClient()
	mock.project = &api.Project{
		ID:     "proj-1",
		Number: 1,
		Title:  "Test Project",
	}
	mock.issues[fmt.Sprintf("testowner/testrepo#%d", number)] = &api.Issue{
		ID:     fmt.Sprintf("issue-%d", number),
		Number: number,
		Title:  title,
		State:  "OPEN",
		Repository: api.Repository{
			Owner: "testowner",
			Name:  "testrepo",
		},
	}
	mock.projectItems = []api.ProjectItem{
		{
			ID: itemID,
			Issue: &api.Issue{
				ID:     fmt.Sprintf("issue-%d", number),
				Number: number,
				State:  "OPEN",
				Repository: api.Repository{
					Owner: "testowner",
					Name:  "testrepo",
				},
			},
		},
	}
	return mock
}

// ============================================================================
// Command Flag Tests
// ============================================================================

func TestMoveCommand_Exists(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"move", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("move command should exist: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("move")) {
		t.Error("Expected help output to mention 'move'")
	}
}

func TestMoveCommand_RequiresIssueNumber(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"move"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when issue number not provided")
	}
}

func TestMoveCommand_HasStatusFlag(t *testing.T) {
	cmd := NewRootCommand()
	moveCmd, _, err := cmd.Find([]string{"move"})
	if err != nil {
		t.Fatalf("move command not found: %v", err)
	}

	flag := moveCmd.Flags().Lookup("status")
	if flag == nil {
		t.Fatal("Expected --status flag to exist")
	}
}

func TestMoveCommand_HasPriorityFlag(t *testing.T) {
	cmd := NewRootCommand()
	moveCmd, _, err := cmd.Find([]string{"move"})
	if err != nil {
		t.Fatalf("move command not found: %v", err)
	}

	flag := moveCmd.Flags().Lookup("priority")
	if flag == nil {
		t.Fatal("Expected --priority flag to exist")
	}
}

func TestMoveCommand_RequiresAtLeastOneFlag(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"move", "123"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when no field flags provided")
	}
}

func TestMoveCommand_HasExample(t *testing.T) {
	moveCmd, _, err := NewRootCommand().Find([]string{"move"})
	if err != nil {
		t.Fatalf("move command not found: %v", err)
	}
	if !strings.Contains(moveCmd.Example, "gh pmu move 42 --status in_progress") {
		t.Errorf("Expected move to define an Example with an invocation, got %q", moveCmd.Example)
	}
}

// TestMoveCommand_RuntimeErrorOmitsUsage: an error returned from RunE prints
// the error alone — no usage dump burying the cause (#927).
func TestMoveCommand_RuntimeErrorOmitsUsage(t *testing.T) {
	out, err := executeForErrorOutput(t, "move", "123")
	if err == nil {
		t.Fatal("Expected an error when no field flags are provided")
	}
	if !strings.Contains(out, "at least one of --status") {
		t.Errorf("Expected the runtime error in the output, got:\n%s", out)
	}
	if strings.Contains(out, "Usage:") {
		t.Errorf("Expected no usage block after a runtime error, got:\n%s", out)
	}
}

// TestMoveCommand_ArgumentErrorShowsUsageAndExamplesOnce: an argument error
// keeps the usage block, whose Examples section comes from the Example field
// and is not duplicated by an appended line (#927).
func TestMoveCommand_ArgumentErrorShowsUsageAndExamplesOnce(t *testing.T) {
	out, err := executeForErrorOutput(t, "move")
	if err == nil {
		t.Fatal("Expected an argument error for move with no issue number")
	}
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "gh pmu move <issue-number>") {
		t.Errorf("Expected the usage block with the Use line, got:\n%s", out)
	}
	if strings.Count(out, "Examples:") != 1 {
		t.Errorf("Expected exactly one Examples section, got %d:\n%s", strings.Count(out, "Examples:"), out)
	}
	if strings.Count(out, "gh pmu move 42 --status in_progress") != 1 {
		t.Errorf("Expected the example invocation exactly once, got:\n%s", out)
	}
}

func TestMoveCommand_HasRecursiveFlag(t *testing.T) {
	cmd := NewRootCommand()
	moveCmd, _, err := cmd.Find([]string{"move"})
	if err != nil {
		t.Fatalf("move command not found: %v", err)
	}

	flag := moveCmd.Flags().Lookup("recursive")
	if flag == nil {
		t.Fatal("Expected --recursive flag to exist")
	}

	if flag.Shorthand != "r" {
		t.Errorf("Expected --recursive shorthand to be 'r', got '%s'", flag.Shorthand)
	}
}

func TestMoveCommand_HasDepthFlag(t *testing.T) {
	cmd := NewRootCommand()
	moveCmd, _, err := cmd.Find([]string{"move"})
	if err != nil {
		t.Fatalf("move command not found: %v", err)
	}

	flag := moveCmd.Flags().Lookup("depth")
	if flag == nil {
		t.Fatal("Expected --depth flag to exist")
	}

	if flag.DefValue != "10" {
		t.Errorf("Expected --depth default to be 10, got '%s'", flag.DefValue)
	}
}

func TestMoveCommand_HasDryRunFlag(t *testing.T) {
	cmd := NewRootCommand()
	moveCmd, _, err := cmd.Find([]string{"move"})
	if err != nil {
		t.Fatalf("move command not found: %v", err)
	}

	flag := moveCmd.Flags().Lookup("dry-run")
	if flag == nil {
		t.Fatal("Expected --dry-run flag to exist")
	}
}

func TestMoveCommand_HasYesFlag(t *testing.T) {
	cmd := NewRootCommand()
	moveCmd, _, err := cmd.Find([]string{"move"})
	if err != nil {
		t.Fatalf("move command not found: %v", err)
	}

	flag := moveCmd.Flags().Lookup("yes")
	if flag == nil {
		t.Fatal("Expected --yes flag to exist")
	}

	if flag.Shorthand != "y" {
		t.Errorf("Expected --yes shorthand to be 'y', got '%s'", flag.Shorthand)
	}
}

func TestMoveCommand_YesFlagHelpDoesNotGateForce(t *testing.T) {
	// #867 finding 3: --force implies confirmation (issue #778, AC-778-1), so the
	// `!opts.yes && !opts.force` guard was unreachable dead code. The --yes help
	// text must not claim to gate --force — that was the documented contradiction.
	cmd := NewRootCommand()
	moveCmd, _, err := cmd.Find([]string{"move"})
	if err != nil {
		t.Fatalf("move command not found: %v", err)
	}
	flag := moveCmd.Flags().Lookup("yes")
	if flag == nil {
		t.Fatal("Expected --yes flag to exist")
	}
	if strings.Contains(flag.Usage, "--force") {
		t.Errorf("--yes help must not claim to gate --force (force self-confirms); got %q", flag.Usage)
	}
	if !strings.Contains(flag.Usage, "--recursive") {
		t.Errorf("--yes help should still mention --recursive; got %q", flag.Usage)
	}
}

func TestMoveCommand_RecursiveHelpText(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"move", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("move help failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("--recursive")) {
		t.Error("Expected help to mention --recursive flag")
	}
	if !bytes.Contains([]byte(output), []byte("sub-issues")) {
		t.Error("Expected help to mention sub-issues")
	}
}

func TestMoveCommand_HelpHasRecursiveExamples(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"move", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("move help failed: %v", err)
	}

	output := buf.String()

	// Verify recursive examples are documented
	tests := []struct {
		name     string
		expected string
	}{
		{"basic recursive example", "--status in_progress --recursive"},
		{"dry-run example", "--recursive --dry-run"},
		{"yes flag example", "--recursive --yes"},
		{"depth flag example", "--recursive --depth"},
	}

	for _, tt := range tests {
		if !strings.Contains(output, tt.expected) {
			t.Errorf("Expected help to contain %s example: %s", tt.name, tt.expected)
		}
	}
}

// ============================================================================
// runMoveWithDeps Tests
// ============================================================================

func TestRunMoveWithDeps_InvalidIssueReference(t *testing.T) {
	mock := newMockMoveClient()
	cfg := testMoveConfig()
	cfg.Repositories = []string{} // No repos configured

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	// Invalid issue reference with no repos
	err := runMoveWithDeps(cmd, []string{"invalid"}, opts, cfg, mock)
	if err == nil {
		t.Error("Expected error for invalid issue reference")
	}
}

func TestRunMoveWithDeps_NoRepoConfigured(t *testing.T) {
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	cfg := testMoveConfig()
	cfg.Repositories = []string{}

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)
	if err == nil {
		t.Error("Expected error when no repository configured")
	}
	if !strings.Contains(err.Error(), "no valid issues") {
		t.Errorf("Expected 'no valid issues' error, got: %v", err)
	}
}

func TestMoveCommand_HasRepoFlag(t *testing.T) {
	cmd := NewRootCommand()
	moveCmd, _, err := cmd.Find([]string{"move"})
	if err != nil {
		t.Fatalf("move command not found: %v", err)
	}

	flag := moveCmd.Flags().Lookup("repo")
	if flag == nil {
		t.Fatal("Expected --repo flag to exist")
	}

	if flag.Shorthand != "R" {
		t.Errorf("Expected --repo shorthand to be 'R', got '%s'", flag.Shorthand)
	}
}

func TestRunMoveWithDeps_RepoFlagOverridesConfig(t *testing.T) {
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.issues["other/repo#456"] = &api.Issue{
		ID:     "issue-456",
		Number: 456,
		Title:  "Issue in other repo",
		Repository: api.Repository{
			Owner: "other",
			Name:  "repo",
		},
	}
	mock.projectItems = []api.ProjectItem{
		{
			ID: "item-456",
			Issue: &api.Issue{
				Number: 456,
				Repository: api.Repository{
					Owner: "other",
					Name:  "repo",
				},
			},
		},
	}
	cfg := testMoveConfig() // Uses testowner/testrepo

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Use --repo flag to override config
	opts := &moveOptions{status: "in_progress", repo: "other/repo"}

	err := runMoveWithDeps(cmd, []string{"456"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(mock.fieldUpdates) != 1 {
		t.Fatalf("Expected 1 field update, got %d", len(mock.fieldUpdates))
	}
}

func TestRunMoveWithDeps_InvalidRepoFlagFormat(t *testing.T) {
	mock := newMockMoveClient()
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", repo: "invalid-format"}

	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)
	if err == nil {
		t.Error("Expected error for invalid --repo format")
	}
	if !strings.Contains(err.Error(), "invalid --repo format") {
		t.Errorf("Expected 'invalid --repo format' error, got: %v", err)
	}
}

func TestRunMoveWithDeps_RepoFlagWithNoConfiguredRepos(t *testing.T) {
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.issues["other/repo#123"] = &api.Issue{
		ID:     "issue-123",
		Number: 123,
		Title:  "Test Issue",
		Repository: api.Repository{
			Owner: "other",
			Name:  "repo",
		},
	}
	mock.projectItems = []api.ProjectItem{
		{
			ID: "item-123",
			Issue: &api.Issue{
				Number: 123,
				Repository: api.Repository{
					Owner: "other",
					Name:  "repo",
				},
			},
		},
	}
	cfg := testMoveConfig()
	cfg.Repositories = []string{} // No repos configured

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// --repo flag should work even without configured repos
	opts := &moveOptions{status: "in_progress", repo: "other/repo"}

	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(mock.fieldUpdates) != 1 {
		t.Fatalf("Expected 1 field update, got %d", len(mock.fieldUpdates))
	}
}

func TestRunMoveWithDeps_InvalidRepoFormat(t *testing.T) {
	mock := newMockMoveClient()
	cfg := testMoveConfig()
	cfg.Repositories = []string{"invalid-repo-format"} // Missing slash

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)
	if err == nil {
		t.Error("Expected error for invalid repo format")
	}
}

func TestRunMoveWithDeps_GetIssueFails(t *testing.T) {
	mock := newMockMoveClient()
	mock.getIssueErr = fmt.Errorf("API error")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)
	if err == nil {
		t.Error("Expected error when GetIssue fails")
	}
}

func TestRunMoveWithDeps_GetProjectFails(t *testing.T) {
	mock := newMockMoveClient()
	mock.issues["testowner/testrepo#123"] = &api.Issue{
		ID:     "issue-123",
		Number: 123,
		Title:  "Test Issue",
	}
	mock.getProjectErr = fmt.Errorf("project API error")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)
	if err == nil {
		t.Error("Expected error when GetProject fails")
	}
}

func TestRunMoveWithDeps_GetProjectItemsFails(t *testing.T) {
	mock := newMockMoveClient()
	mock.issues["testowner/testrepo#123"] = &api.Issue{
		ID:     "issue-123",
		Number: 123,
		Title:  "Test Issue",
	}
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.getProjectItemsErr = fmt.Errorf("items API error")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)
	if err == nil {
		t.Error("Expected error when GetProjectItems fails")
	}
}

func TestRunMoveWithDeps_IssueNotInProject(t *testing.T) {
	mock := newMockMoveClient()
	mock.issues["testowner/testrepo#123"] = &api.Issue{
		ID:     "issue-123",
		Number: 123,
		Title:  "Test Issue",
	}
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.projectItems = []api.ProjectItem{} // Empty - issue not in project
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)
	if err == nil {
		t.Error("Expected error when issue not in project")
	}
	if !strings.Contains(err.Error(), "no valid issues") {
		t.Errorf("Expected 'no valid issues' error, got: %v", err)
	}
}

func TestRunMoveWithDeps_SingleIssueStatusUpdate(t *testing.T) {
	mock := setupMockWithIssue(123, "Test Issue", "item-123")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify field update
	if len(mock.fieldUpdates) != 1 {
		t.Fatalf("Expected 1 field update, got %d", len(mock.fieldUpdates))
	}
	update := mock.fieldUpdates[0]
	if update.fieldName != "Status" {
		t.Errorf("Expected fieldName 'Status', got '%s'", update.fieldName)
	}
	if update.value != "In Progress" {
		t.Errorf("Expected value 'In Progress', got '%s'", update.value)
	}
}

func TestRunMoveWithDeps_SingleIssuePriorityUpdate(t *testing.T) {
	mock := setupMockWithIssue(123, "Test Issue", "item-123")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{priority: "high"}

	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(mock.fieldUpdates) != 1 {
		t.Fatalf("Expected 1 field update, got %d", len(mock.fieldUpdates))
	}
	update := mock.fieldUpdates[0]
	if update.fieldName != "Priority" {
		t.Errorf("Expected fieldName 'Priority', got '%s'", update.fieldName)
	}
	if update.value != "High" {
		t.Errorf("Expected value 'High', got '%s'", update.value)
	}
}

func TestRunMoveWithDeps_BothStatusAndPriority(t *testing.T) {
	mock := setupMockWithIssue(123, "Test Issue", "item-123")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "done", priority: "low"}

	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(mock.fieldUpdates) != 2 {
		t.Fatalf("Expected 2 field updates, got %d", len(mock.fieldUpdates))
	}
}

func TestRunMoveWithDeps_DryRunNoChanges(t *testing.T) {
	mock := setupMockWithIssue(123, "Test Issue", "item-123")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", dryRun: true}

	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Dry run should not make any changes - this is the key assertion
	if len(mock.fieldUpdates) != 0 {
		t.Errorf("Expected no field updates in dry run, got %d", len(mock.fieldUpdates))
	}
}

func TestRunMoveWithDeps_StatusUpdateFails(t *testing.T) {
	mock := setupMockWithIssue(123, "Test Issue", "item-123")
	// Use setProjectItemErrFor for batch-compatible error injection
	mock.setProjectItemErrFor = map[string]error{
		"item-123": fmt.Errorf("update failed"),
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	// Should return error when update fails (hasErrors = true)
	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)
	if err == nil {
		t.Fatal("Expected error when update fails")
	}
	if !strings.Contains(err.Error(), "could not be updated") {
		t.Errorf("Expected 'could not be updated' error, got: %v", err)
	}
}

func TestRunMoveWithDeps_FullIssueReference(t *testing.T) {
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.issues["other/repo#456"] = &api.Issue{
		ID:     "issue-456",
		Number: 456,
		Title:  "Other Repo Issue",
		Repository: api.Repository{
			Owner: "other",
			Name:  "repo",
		},
	}
	mock.projectItems = []api.ProjectItem{
		{
			ID: "item-456",
			Issue: &api.Issue{
				Number: 456,
				Repository: api.Repository{
					Owner: "other",
					Name:  "repo",
				},
			},
		},
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	err := runMoveWithDeps(cmd, []string{"other/repo#456"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(mock.fieldUpdates) != 1 {
		t.Fatalf("Expected 1 field update, got %d", len(mock.fieldUpdates))
	}
}

// ============================================================================
// Recursive Operation Tests
// ============================================================================

func TestRunMoveWithDeps_RecursiveCollectSubIssues(t *testing.T) {
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}

	// Parent issue
	mock.issues["testowner/testrepo#1"] = &api.Issue{
		ID:     "issue-1",
		Number: 1,
		Title:  "Parent Issue",
		Repository: api.Repository{
			Owner: "testowner",
			Name:  "testrepo",
		},
	}

	// Project items for parent and sub-issues
	mock.projectItems = []api.ProjectItem{
		{
			ID: "item-1",
			Issue: &api.Issue{
				Number: 1,
				Repository: api.Repository{
					Owner: "testowner",
					Name:  "testrepo",
				},
			},
		},
		{
			ID: "item-2",
			Issue: &api.Issue{
				Number: 2,
				Repository: api.Repository{
					Owner: "testowner",
					Name:  "testrepo",
				},
			},
		},
		{
			ID: "item-3",
			Issue: &api.Issue{
				Number: 3,
				Repository: api.Repository{
					Owner: "testowner",
					Name:  "testrepo",
				},
			},
		},
	}

	// Sub-issues - these are returned when GetSubIssues is called for issue #1
	mock.subIssues["testowner/testrepo#1"] = []api.SubIssue{
		{
			ID:     "issue-2",
			Number: 2,
			Title:  "Sub Issue 1",
			Repository: api.Repository{
				Owner: "testowner",
				Name:  "testrepo",
			},
		},
		{
			ID:     "issue-3",
			Number: 3,
			Title:  "Sub Issue 2",
			Repository: api.Repository{
				Owner: "testowner",
				Name:  "testrepo",
			},
		},
	}

	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", recursive: true, yes: true, depth: 10}

	err := runMoveWithDeps(cmd, []string{"1"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should update parent + 2 sub-issues = 3 issues
	// Each issue gets 1 status update
	if len(mock.fieldUpdates) != 3 {
		t.Errorf("Expected 3 field updates (1 parent + 2 sub-issues), got %d. Updates: %+v", len(mock.fieldUpdates), mock.fieldUpdates)
	}
}

func TestRunMoveWithDeps_RecursiveDryRun(t *testing.T) {
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}

	mock.issues["testowner/testrepo#1"] = &api.Issue{
		ID:     "issue-1",
		Number: 1,
		Title:  "Parent Issue",
	}

	mock.projectItems = []api.ProjectItem{
		{
			ID: "item-1",
			Issue: &api.Issue{
				Number: 1,
				Repository: api.Repository{
					Owner: "testowner",
					Name:  "testrepo",
				},
			},
		},
	}

	mock.subIssues["testowner/testrepo#1"] = []api.SubIssue{
		{
			ID:     "issue-2",
			Number: 2,
			Title:  "Sub Issue",
			Repository: api.Repository{
				Owner: "testowner",
				Name:  "testrepo",
			},
		},
	}

	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", recursive: true, dryRun: true, depth: 10}

	err := runMoveWithDeps(cmd, []string{"1"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Dry run should not make any changes - this is the key assertion
	if len(mock.fieldUpdates) != 0 {
		t.Errorf("Expected no field updates in dry run, got %d", len(mock.fieldUpdates))
	}
}

func TestRunMoveWithDeps_RecursiveSubIssueNotInProject(t *testing.T) {
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}

	mock.issues["testowner/testrepo#1"] = &api.Issue{
		ID:     "issue-1",
		Number: 1,
		Title:  "Parent Issue",
	}

	// Only parent is in project, sub-issue is not
	mock.projectItems = []api.ProjectItem{
		{
			ID: "item-1",
			Issue: &api.Issue{
				Number: 1,
				Repository: api.Repository{
					Owner: "testowner",
					Name:  "testrepo",
				},
			},
		},
	}

	mock.subIssues["testowner/testrepo#1"] = []api.SubIssue{
		{
			ID:     "issue-2",
			Number: 2,
			Title:  "Sub Issue Not In Project",
			Repository: api.Repository{
				Owner: "testowner",
				Name:  "testrepo",
			},
		},
	}

	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", recursive: true, yes: true, depth: 10}

	err := runMoveWithDeps(cmd, []string{"1"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Only parent should be updated (sub-issue skipped because not in project)
	if len(mock.fieldUpdates) != 1 {
		t.Errorf("Expected 1 field update (parent only), got %d", len(mock.fieldUpdates))
	}
}

func TestRunMoveWithDeps_RecursiveGetSubIssuesFails(t *testing.T) {
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}

	mock.issues["testowner/testrepo#1"] = &api.Issue{
		ID:     "issue-1",
		Number: 1,
		Title:  "Parent Issue",
	}

	mock.projectItems = []api.ProjectItem{
		{
			ID: "item-1",
			Issue: &api.Issue{
				Number: 1,
				Repository: api.Repository{
					Owner: "testowner",
					Name:  "testrepo",
				},
			},
		},
	}

	mock.getSubIssuesErr = fmt.Errorf("sub-issues API error")

	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", recursive: true, yes: true, depth: 10}

	err := runMoveWithDeps(cmd, []string{"1"}, opts, cfg, mock)
	// GetSubIssues failure degrades gracefully: the parent issue is still
	// updated. But the subtree was not moved, so the command is a partial
	// success rather than a clean exit (#938).
	if len(mock.fieldUpdates) == 0 {
		t.Error("the parent issue should still be updated")
	}
	if !api.IsPartialSuccess(err) {
		t.Errorf("expected a partial-success error, got: %v", err)
	}
}

func TestRunMoveWithDeps_RecursiveProgressOutput(t *testing.T) {
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}

	mock.issues["testowner/testrepo#1"] = &api.Issue{
		ID:     "issue-1",
		Number: 1,
		Title:  "Parent Issue",
	}

	mock.projectItems = []api.ProjectItem{
		{
			ID: "item-1",
			Issue: &api.Issue{
				Number: 1,
				Repository: api.Repository{
					Owner: "testowner",
					Name:  "testrepo",
				},
			},
		},
		{
			ID: "item-2",
			Issue: &api.Issue{
				Number: 2,
				Repository: api.Repository{
					Owner: "testowner",
					Name:  "testrepo",
				},
			},
		},
	}

	mock.subIssues["testowner/testrepo#1"] = []api.SubIssue{
		{
			ID:     "issue-2",
			Number: 2,
			Title:  "Sub Issue",
			Repository: api.Repository{
				Owner: "testowner",
				Name:  "testrepo",
			},
		},
	}

	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", recursive: true, yes: true, depth: 10}

	err := runMoveWithDeps(cmd, []string{"1"}, opts, cfg, mock)

	output := buf.String()

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify progress output contains expected format
	if !strings.Contains(output, "Updating #1...") {
		t.Errorf("Expected progress output 'Updating #1...', got: %s", output)
	}
	if !strings.Contains(output, "done") {
		t.Errorf("Expected 'done' in progress output, got: %s", output)
	}
	if !strings.Contains(output, "Summary:") {
		t.Errorf("Expected 'Summary:' in output, got: %s", output)
	}
	if !strings.Contains(output, "2 updated") {
		t.Errorf("Expected '2 updated' in summary, got: %s", output)
	}
}

// ============================================================================
// collectSubIssuesRecursive Tests
// ============================================================================

func TestCollectSubIssuesRecursive_RespectsDepthLimit(t *testing.T) {
	mock := newMockMoveClient()

	// Create a deep hierarchy: 1 -> 2 -> 3 -> 4 -> 5
	mock.subIssues["testowner/testrepo#1"] = []api.SubIssue{
		{Number: 2, Title: "Level 1", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	mock.subIssues["testowner/testrepo#2"] = []api.SubIssue{
		{Number: 3, Title: "Level 2", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	mock.subIssues["testowner/testrepo#3"] = []api.SubIssue{
		{Number: 4, Title: "Level 3", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	mock.subIssues["testowner/testrepo#4"] = []api.SubIssue{
		{Number: 5, Title: "Level 4", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}

	itemIDMap := map[string]string{
		"testowner/testrepo#2": "item-2",
		"testowner/testrepo#3": "item-3",
		"testowner/testrepo#4": "item-4",
		"testowner/testrepo#5": "item-5",
	}

	// Collect with maxDepth=2 (should get levels 1 and 2, i.e., issues 2 and 3)
	itemFieldsMap := make(map[string][]api.FieldValue)
	itemDataMap := make(map[string]*api.Issue)
	result, err := collectSubIssuesRecursive(mock, "testowner", "testrepo", 1, itemIDMap, itemFieldsMap, itemDataMap, 1, 2)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 issues (depth 1 and 2), got %d", len(result))
	}

	// Verify depths
	depths := make(map[int]int) // issue number -> depth
	for _, info := range result {
		depths[info.Number] = info.Depth
	}

	if depths[2] != 1 {
		t.Errorf("Expected issue #2 at depth 1, got %d", depths[2])
	}
	if depths[3] != 2 {
		t.Errorf("Expected issue #3 at depth 2, got %d", depths[3])
	}
}

func TestCollectSubIssuesRecursive_HandlesEmptySubIssues(t *testing.T) {
	mock := newMockMoveClient()
	// No sub-issues for any issue

	itemIDMap := map[string]string{}
	itemFieldsMap := make(map[string][]api.FieldValue)
	itemDataMap := make(map[string]*api.Issue)

	result, err := collectSubIssuesRecursive(mock, "testowner", "testrepo", 1, itemIDMap, itemFieldsMap, itemDataMap, 1, 10)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected 0 sub-issues, got %d", len(result))
	}
}

func TestCollectSubIssuesRecursive_HandlesCrossRepoSubIssues(t *testing.T) {
	mock := newMockMoveClient()

	// Parent in repo A has sub-issue in repo B
	mock.subIssues["owner-a/repo-a#1"] = []api.SubIssue{
		{
			Number: 100,
			Title:  "Cross-repo sub-issue",
			Repository: api.Repository{
				Owner: "owner-b",
				Name:  "repo-b",
			},
		},
	}

	itemIDMap := map[string]string{
		"owner-b/repo-b#100": "item-100",
	}
	itemFieldsMap := make(map[string][]api.FieldValue)
	itemDataMap := make(map[string]*api.Issue)

	result, err := collectSubIssuesRecursive(mock, "owner-a", "repo-a", 1, itemIDMap, itemFieldsMap, itemDataMap, 1, 10)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 sub-issue, got %d", len(result))
	}

	// Verify cross-repo handling
	if result[0].Owner != "owner-b" {
		t.Errorf("Expected owner 'owner-b', got '%s'", result[0].Owner)
	}
	if result[0].Repo != "repo-b" {
		t.Errorf("Expected repo 'repo-b', got '%s'", result[0].Repo)
	}
	if result[0].Number != 100 {
		t.Errorf("Expected number 100, got %d", result[0].Number)
	}
}

func TestCollectSubIssuesRecursive_InheritsRepoWhenEmpty(t *testing.T) {
	mock := newMockMoveClient()

	// Sub-issue with empty repository (should inherit parent's repo)
	mock.subIssues["testowner/testrepo#1"] = []api.SubIssue{
		{
			Number: 2,
			Title:  "Same-repo sub-issue",
			Repository: api.Repository{
				Owner: "", // Empty - should inherit
				Name:  "", // Empty - should inherit
			},
		},
	}

	itemIDMap := map[string]string{
		"testowner/testrepo#2": "item-2",
	}
	itemFieldsMap := make(map[string][]api.FieldValue)
	itemDataMap := make(map[string]*api.Issue)

	result, err := collectSubIssuesRecursive(mock, "testowner", "testrepo", 1, itemIDMap, itemFieldsMap, itemDataMap, 1, 10)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 sub-issue, got %d", len(result))
	}

	// Should inherit parent's repo
	if result[0].Owner != "testowner" {
		t.Errorf("Expected owner 'testowner', got '%s'", result[0].Owner)
	}
	if result[0].Repo != "testrepo" {
		t.Errorf("Expected repo 'testrepo', got '%s'", result[0].Repo)
	}
}

func TestCollectSubIssuesRecursive_SubIssueNotInProject(t *testing.T) {
	mock := newMockMoveClient()

	mock.subIssues["testowner/testrepo#1"] = []api.SubIssue{
		{
			Number: 2,
			Title:  "Not in project",
			Repository: api.Repository{
				Owner: "testowner",
				Name:  "testrepo",
			},
		},
	}

	// Empty itemIDMap - sub-issue not in project
	itemIDMap := map[string]string{}
	itemFieldsMap := make(map[string][]api.FieldValue)
	itemDataMap := make(map[string]*api.Issue)

	result, err := collectSubIssuesRecursive(mock, "testowner", "testrepo", 1, itemIDMap, itemFieldsMap, itemDataMap, 1, 10)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 sub-issue, got %d", len(result))
	}

	// ItemID should be empty since not in project
	if result[0].ItemID != "" {
		t.Errorf("Expected empty ItemID, got '%s'", result[0].ItemID)
	}
}

func TestCollectSubIssuesRecursive_MaxDepthZero(t *testing.T) {
	mock := newMockMoveClient()

	mock.subIssues["testowner/testrepo#1"] = []api.SubIssue{
		{Number: 2, Title: "Sub", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}

	itemIDMap := map[string]string{"testowner/testrepo#2": "item-2"}
	itemFieldsMap := make(map[string][]api.FieldValue)
	itemDataMap := make(map[string]*api.Issue)

	// maxDepth=0, currentDepth=1 -> should return nothing
	result, err := collectSubIssuesRecursive(mock, "testowner", "testrepo", 1, itemIDMap, itemFieldsMap, itemDataMap, 1, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected 0 sub-issues with maxDepth=0, got %d", len(result))
	}
}

// =============================================================================
// =============================================================================
// REQ-006: Branch Flag Tests
// =============================================================================

func TestRunMoveWithDeps_BranchExplicitValue(t *testing.T) {
	mock := setupMockWithIssue(42, "Test Issue", "item-42")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{branch: "v1.2.0"}

	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify Release field was set
	found := false
	for _, update := range mock.fieldUpdates {
		if update.fieldName == "Release" && update.value == "v1.2.0" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected Release field to be set to 'v1.2.0', updates: %+v", mock.fieldUpdates)
	}
}

func TestRunMoveWithDeps_ReleaseCurrent(t *testing.T) {
	mock := setupMockWithIssue(42, "Test Issue", "item-42")
	// Add active release
	mock.openIssuesByLabel["branch"] = []api.Issue{
		{
			ID:     "TRACKER_200",
			Number: 200,
			Title:  "Release: v1.3.0",
			State:  "OPEN",
		},
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{branch: "current"}

	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify Release field was set to active branch name
	found := false
	for _, update := range mock.fieldUpdates {
		if update.fieldName == "Release" && update.value == "v1.3.0" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected Release field to be set to 'v1.3.0', updates: %+v", mock.fieldUpdates)
	}
}

func TestRunMoveWithDeps_ReleaseCurrentNoActive(t *testing.T) {
	mock := setupMockWithIssue(42, "Test Issue", "item-42")
	// No active branch
	mock.openIssuesByLabel["branch"] = []api.Issue{}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{branch: "current"}

	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)
	if err == nil {
		t.Fatal("Expected error when no active branch")
	}

	if !strings.Contains(err.Error(), "no active branch") {
		t.Errorf("Expected error to mention 'no active branch', got: %v", err)
	}
}

// =============================================================================
// REQ-007: Backlog Flag Tests
// =============================================================================

func TestMoveCommand_HasBacklogFlag(t *testing.T) {
	cmd := NewRootCommand()
	moveCmd, _, err := cmd.Find([]string{"move"})
	if err != nil {
		t.Fatalf("move command not found: %v", err)
	}

	flag := moveCmd.Flags().Lookup("backlog")
	if flag == nil {
		t.Fatal("Expected --backlog flag to exist")
	}
}

func TestRunMoveWithDeps_BacklogClearsFields(t *testing.T) {
	mock := setupMockWithIssue(42, "Test Issue", "item-42")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{backlog: true}

	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify Release field was cleared (set to empty)
	releaseCleared := false
	for _, update := range mock.fieldUpdates {
		if update.fieldName == "Release" && update.value == "" {
			releaseCleared = true
		}
	}
	if !releaseCleared {
		t.Error("Expected Release field to be cleared")
	}
}

func TestRunMoveWithDeps_BacklogWithStatus(t *testing.T) {
	mock := setupMockWithIssue(42, "Test Issue", "item-42")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// --backlog can be combined with --status
	opts := &moveOptions{backlog: true, status: "todo"}

	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify Status was set and Release was cleared
	statusSet := false
	releaseCleared := false
	for _, update := range mock.fieldUpdates {
		if update.fieldName == "Status" && update.value == "Todo" {
			statusSet = true
		}
		if update.fieldName == "Release" && update.value == "" {
			releaseCleared = true
		}
	}
	if !statusSet {
		t.Error("Expected Status field to be set")
	}
	if !releaseCleared {
		t.Error("Expected Release field to be cleared")
	}
}

func TestRunMoveWithDeps_BacklogSetsStatusToBacklog(t *testing.T) {
	mock := setupMockWithIssue(42, "Test Issue", "item-42")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// --backlog alone (no explicit --status) should set Status to Backlog
	opts := &moveOptions{backlog: true, yes: true}

	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify Status was set to Backlog
	statusSet := false
	for _, update := range mock.fieldUpdates {
		if update.fieldName == "Status" && update.value == "Backlog" {
			statusSet = true
		}
	}
	if !statusSet {
		t.Error("Expected Status field to be set to 'Backlog' when --backlog is used")
	}
}

func TestRunMoveWithDeps_BacklogExplicitStatusOverrides(t *testing.T) {
	mock := setupMockWithIssue(42, "Test Issue", "item-42")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// --backlog with explicit --status should use the explicit status, not Backlog
	opts := &moveOptions{backlog: true, status: "done"}

	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify Status was set to Done (not Backlog)
	statusSetToDone := false
	statusSetToBacklog := false
	for _, update := range mock.fieldUpdates {
		if update.fieldName == "Status" && update.value == "Done" {
			statusSetToDone = true
		}
		if update.fieldName == "Status" && update.value == "Backlog" {
			statusSetToBacklog = true
		}
	}
	if !statusSetToDone {
		t.Error("Expected Status field to be set to 'Done' (explicit --status)")
	}
	if statusSetToBacklog {
		t.Error("Status should NOT be set to 'Backlog' when --status explicitly overrides")
	}
}

// ============================================================================
// Caching Behavior Tests
// ============================================================================

func TestRunMoveWithDeps_CachesProjectFields(t *testing.T) {
	// ARRANGE - Setup mock with issue
	mock := setupMockWithIssue(123, "Test Issue", "item-123")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Update BOTH status AND priority to trigger multiple field updates
	opts := &moveOptions{status: "in_progress", priority: "high"}

	// ACT
	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// GetProjectFields should only be called ONCE due to caching
	if mock.getProjectFieldsCalls != 1 {
		t.Errorf("Expected GetProjectFields to be called once (caching), got %d calls", mock.getProjectFieldsCalls)
	}

	// Both fields should still be updated
	if len(mock.fieldUpdates) != 2 {
		t.Errorf("Expected 2 field updates, got %d", len(mock.fieldUpdates))
	}
}

func TestRunMoveWithDeps_GetProjectFieldsErrorReturnsError(t *testing.T) {
	// ARRANGE
	mock := setupMockWithIssue(123, "Test Issue", "item-123")
	mock.getProjectFieldsErr = fmt.Errorf("failed to get fields")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	// ACT
	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get project fields") {
		t.Errorf("Expected 'failed to get project fields' error, got: %v", err)
	}
}

func TestRunMoveWithDeps_InvalidStatusReturnsError(t *testing.T) {
	// ARRANGE
	mock := setupMockWithIssue(123, "Test Issue", "item-123")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "nonexistent"}

	// ACT
	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatal("Expected error for invalid status, got nil")
	}
	if !strings.Contains(err.Error(), "invalid status value") {
		t.Errorf("Expected 'invalid status value' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Available values:") {
		t.Errorf("Expected error to list available values, got: %v", err)
	}
}

func TestRunMoveWithDeps_InvalidPriorityReturnsError(t *testing.T) {
	// ARRANGE
	mock := setupMockWithIssue(123, "Test Issue", "item-123")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{priority: "invalid"}

	// ACT
	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatal("Expected error for invalid priority, got nil")
	}
	if !strings.Contains(err.Error(), "invalid priority value") {
		t.Errorf("Expected 'invalid priority value' error, got: %v", err)
	}
}

// ============================================================================
// Targeted Query Optimization Tests (Issue #541)
// ============================================================================

func TestRunMoveWithDeps_UsesTargetedQuery(t *testing.T) {
	// ARRANGE - Verify that non-recursive move uses GetProjectItemsByIssues
	mock := setupMockWithIssue(123, "Test Issue", "item-123")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	// ACT
	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if mock.getProjectItemsByIssuesCalls != 1 {
		t.Errorf("Expected GetProjectItemsByIssues to be called once, got %d", mock.getProjectItemsByIssuesCalls)
	}
	if mock.getProjectItemsCalls != 0 {
		t.Errorf("Expected GetProjectItems to NOT be called for non-recursive move, got %d calls", mock.getProjectItemsCalls)
	}
	if len(mock.lastIssueRefs) != 1 {
		t.Errorf("Expected 1 issue ref, got %d", len(mock.lastIssueRefs))
	}
	if mock.lastIssueRefs[0].Number != 123 {
		t.Errorf("Expected issue ref number 123, got %d", mock.lastIssueRefs[0].Number)
	}
}

func TestRunMoveWithDeps_TargetedQuery_MultipleIssues(t *testing.T) {
	// ARRANGE - Verify targeted query with multiple issues
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-2", Issue: &api.Issue{Number: 2, Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-3", Issue: &api.Issue{Number: 3, Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", yes: true}

	// ACT
	err := runMoveWithDeps(cmd, []string{"1", "2", "3"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if mock.getProjectItemsByIssuesCalls != 1 {
		t.Errorf("Expected GetProjectItemsByIssues to be called once, got %d", mock.getProjectItemsByIssuesCalls)
	}
	if len(mock.lastIssueRefs) != 3 {
		t.Errorf("Expected 3 issue refs, got %d", len(mock.lastIssueRefs))
	}
	if len(mock.fieldUpdates) != 3 {
		t.Errorf("Expected 3 field updates, got %d", len(mock.fieldUpdates))
	}
}

func TestRunMoveWithDeps_RecursiveUsesTargetedQuery(t *testing.T) {
	// ARRANGE - Verify that recursive move uses GetProjectItemsByIssues (targeted)
	// instead of GetProjectItems (full scan)
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Title: "Parent", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-2", Issue: &api.Issue{Number: 2, Title: "Child", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	mock.subIssues = map[string][]api.SubIssue{
		"testowner/testrepo#1": {{Number: 2, Title: "Child", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", recursive: true, yes: true, depth: 10}

	// ACT
	err := runMoveWithDeps(cmd, []string{"1"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Should use GetProjectItemsByIssues (targeted), NOT GetProjectItems (full scan)
	if mock.getProjectItemsCalls != 0 {
		t.Errorf("Expected GetProjectItems to NOT be called for recursive mode, got %d calls", mock.getProjectItemsCalls)
	}
	if mock.getProjectItemsByIssuesCalls == 0 {
		t.Error("Expected GetProjectItemsByIssues to be called for recursive mode")
	}
	// Should have updated both parent and child
	if len(mock.fieldUpdates) != 2 {
		t.Errorf("Expected 2 field updates (parent + child), got %d", len(mock.fieldUpdates))
	}
}

func TestRunMoveWithDeps_TargetedQuery_FallbackToFullFetch(t *testing.T) {
	// ARRANGE - Verify fallback to full fetch when targeted query fails
	mock := setupMockWithIssue(123, "Test Issue", "item-123")
	mock.getProjectItemsByIssuesErr = fmt.Errorf("targeted query failed")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	// ACT
	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error - should have fallen back to full fetch: %v", err)
	}
	if mock.getProjectItemsByIssuesCalls != 1 {
		t.Errorf("Expected GetProjectItemsByIssues to be called once (and fail), got %d", mock.getProjectItemsByIssuesCalls)
	}
	if mock.getProjectItemsCalls != 1 {
		t.Errorf("Expected GetProjectItems to be called once (fallback), got %d", mock.getProjectItemsCalls)
	}
}

func TestRunMoveWithDeps_TargetedQuery_IssueNotInProject(t *testing.T) {
	// ARRANGE - Issue exists but not in project
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	// projectItems is empty - issue 123 is not in project
	mock.projectItems = []api.ProjectItem{}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	// ACT
	err := runMoveWithDeps(cmd, []string{"123"}, opts, cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatal("Expected error for issue not in project")
	}
	if !strings.Contains(err.Error(), "no valid issues") {
		t.Errorf("Expected 'no valid issues' error, got: %v", err)
	}
}

// ============================================================================
// Benchmark Tests (Issue #541)
// ============================================================================

// BenchmarkMoveCommand_TargetedQuery benchmarks the targeted query approach
// Note: This uses mocked API calls so it measures code path overhead, not actual API latency
func BenchmarkMoveCommand_TargetedQuery(b *testing.B) {
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		mock.getProjectItemsByIssuesCalls = 0
		mock.getProjectItemsCalls = 0
		mock.fieldUpdates = nil
		_ = runMoveWithDeps(cmd, []string{"1"}, opts, cfg, mock)
	}
}

// BenchmarkMoveCommand_FullFetch benchmarks the full fetch approach (recursive mode)
// Note: This uses mocked API calls so it measures code path overhead, not actual API latency
func BenchmarkMoveCommand_FullFetch(b *testing.B) {
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Title: "Parent", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Use recursive mode to force full fetch
	opts := &moveOptions{status: "in_progress", recursive: true}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		mock.getProjectItemsByIssuesCalls = 0
		mock.getProjectItemsCalls = 0
		mock.fieldUpdates = nil
		_ = runMoveWithDeps(cmd, []string{"1"}, opts, cfg, mock)
	}
}

// ============================================================================
// Batch Sub-Issue Optimization Tests (Issue #542)
// ============================================================================

func TestCollectSubIssuesRecursive_UsesBatchQuery(t *testing.T) {
	// ARRANGE - Verify batch method is called for sub-issue fetching
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Title: "Parent", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-2", Issue: &api.Issue{Number: 2, Title: "Child 1", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-3", Issue: &api.Issue{Number: 3, Title: "Child 2", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	mock.subIssues = map[string][]api.SubIssue{
		"testowner/testrepo#1": {
			{Number: 2, Title: "Child 1", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
			{Number: 3, Title: "Child 2", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
		},
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", recursive: true, yes: true, depth: 10}

	// ACT
	err := runMoveWithDeps(cmd, []string{"1"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Level-by-level batching: 1 call for parent level, 1 call for children level (to check for grandchildren)
	if mock.getSubIssuesBatchCalls != 2 {
		t.Errorf("Expected GetSubIssuesBatch to be called twice (once per level), got %d", mock.getSubIssuesBatchCalls)
	}
	// Per-issue GetSubIssues should not be called when batch succeeds
	if mock.getSubIssuesCalls != 0 {
		t.Errorf("Expected GetSubIssues to NOT be called when batch succeeds, got %d calls", mock.getSubIssuesCalls)
	}
}

func TestCollectSubIssuesRecursive_BatchReducesAPICalls(t *testing.T) {
	// ARRANGE - 3-level hierarchy should use O(depth) batch calls, not O(N) per-issue calls
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Title: "Epic", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-2", Issue: &api.Issue{Number: 2, Title: "Story 1", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-3", Issue: &api.Issue{Number: 3, Title: "Story 2", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-4", Issue: &api.Issue{Number: 4, Title: "Task 1", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-5", Issue: &api.Issue{Number: 5, Title: "Task 2", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	mock.subIssues = map[string][]api.SubIssue{
		"testowner/testrepo#1": {
			{Number: 2, Title: "Story 1", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
			{Number: 3, Title: "Story 2", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
		},
		"testowner/testrepo#2": {
			{Number: 4, Title: "Task 1", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
		},
		"testowner/testrepo#3": {
			{Number: 5, Title: "Task 2", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
		},
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", recursive: true, yes: true, depth: 10}

	// ACT
	err := runMoveWithDeps(cmd, []string{"1"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// With batch fetching: 2 batch calls (level 1: epic->stories, level 2: stories->tasks)
	// Without batch fetching: 4 per-issue calls (1+2+1 for each parent)
	if mock.getSubIssuesBatchCalls > 3 {
		t.Errorf("Expected at most 3 batch calls for 3-level hierarchy, got %d", mock.getSubIssuesBatchCalls)
	}
	// Should update all 5 issues (1 epic + 2 stories + 2 tasks)
	if len(mock.fieldUpdates) != 5 {
		t.Errorf("Expected 5 field updates, got %d", len(mock.fieldUpdates))
	}
}

func TestCollectSubIssuesRecursive_DeepHierarchy(t *testing.T) {
	// ARRANGE - Deep hierarchy respects depth limit with batching
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Title: "Level 0", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-2", Issue: &api.Issue{Number: 2, Title: "Level 1", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-3", Issue: &api.Issue{Number: 3, Title: "Level 2", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-4", Issue: &api.Issue{Number: 4, Title: "Level 3", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	mock.subIssues = map[string][]api.SubIssue{
		"testowner/testrepo#1": {{Number: 2, Title: "Level 1", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		"testowner/testrepo#2": {{Number: 3, Title: "Level 2", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		"testowner/testrepo#3": {{Number: 4, Title: "Level 3", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Limit depth to 2 levels
	opts := &moveOptions{status: "in_progress", recursive: true, depth: 2, yes: true}

	// ACT
	err := runMoveWithDeps(cmd, []string{"1"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Should update: #1 (root) + #2 (level 1) + #3 (level 2) = 3 issues
	// #4 should NOT be updated (level 3 exceeds depth limit of 2)
	if len(mock.fieldUpdates) != 3 {
		t.Errorf("Expected 3 field updates with depth=2, got %d", len(mock.fieldUpdates))
	}
}

// ============================================================================
// Batch Mutation Optimization Tests (Issue #543)
// ============================================================================

func TestRunMoveWithDeps_UsesBatchMutations(t *testing.T) {
	// ARRANGE - Verify batch mutation method is called for multiple issues
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Title: "Issue 1", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-2", Issue: &api.Issue{Number: 2, Title: "Issue 2", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-3", Issue: &api.Issue{Number: 3, Title: "Issue 3", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", yes: true}

	// ACT
	err := runMoveWithDeps(cmd, []string{"1", "2", "3"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Should use batch mutation instead of per-issue calls
	if mock.batchUpdateCalls != 1 {
		t.Errorf("Expected 1 batch update call, got %d", mock.batchUpdateCalls)
	}
	// Should have 3 field updates (one Status update per issue)
	if len(mock.fieldUpdates) != 3 {
		t.Errorf("Expected 3 field updates, got %d", len(mock.fieldUpdates))
	}
}

func TestRunMoveWithDeps_BatchMutations_MultipleFields(t *testing.T) {
	// ARRANGE - Multiple fields per issue should be batched together
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Title: "Issue 1", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-2", Issue: &api.Issue{Number: 2, Title: "Issue 2", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Set both status and priority
	opts := &moveOptions{status: "in_progress", priority: "high", yes: true}

	// ACT
	err := runMoveWithDeps(cmd, []string{"1", "2"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Should use single batch call for all updates
	if mock.batchUpdateCalls != 1 {
		t.Errorf("Expected 1 batch update call, got %d", mock.batchUpdateCalls)
	}
	// Should have 4 field updates (2 issues × 2 fields)
	if len(mock.fieldUpdates) != 4 {
		t.Errorf("Expected 4 field updates (2 issues × 2 fields), got %d", len(mock.fieldUpdates))
	}
}

func TestRunMoveWithDeps_BatchMutations_PartialFailure(t *testing.T) {
	// ARRANGE - Some updates fail, others succeed
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Title: "Issue 1", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-2", Issue: &api.Issue{Number: 2, Title: "Issue 2", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	// Set up partial failure - item-2 fails
	mock.setProjectItemErrFor = map[string]error{
		"item-2": fmt.Errorf("simulated failure"),
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", yes: true}

	// ACT
	err := runMoveWithDeps(cmd, []string{"1", "2"}, opts, cfg, mock)

	// ASSERT
	// Should return error due to partial failure
	if err == nil {
		t.Fatal("Expected error due to partial failure")
	}
	// Batch was called
	if mock.batchUpdateCalls != 1 {
		t.Errorf("Expected 1 batch update call, got %d", mock.batchUpdateCalls)
	}
}

func TestRunMoveWithDeps_BatchMutations_FallbackOnError(t *testing.T) {
	// ARRANGE - Batch fails entirely, should fall back to sequential
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Title: "Issue 1", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	// Set up batch to fail
	mock.batchUpdateErr = fmt.Errorf("batch failed")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", yes: true}

	// ACT
	err := runMoveWithDeps(cmd, []string{"1"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Should have tried batch once
	if mock.batchUpdateCalls != 1 {
		t.Errorf("Expected 1 batch update call, got %d", mock.batchUpdateCalls)
	}
	// Should have fallen back to sequential - check fieldUpdates from SetProjectItemField
	// Note: fieldUpdates is populated by both batch and sequential, so we check the count
	if len(mock.fieldUpdates) != 1 {
		t.Errorf("Expected 1 field update from fallback, got %d", len(mock.fieldUpdates))
	}
}

func TestRunMoveWithDeps_BatchMutations_ReducedAPICalls(t *testing.T) {
	// ARRANGE - 10 issues with 3 fields each = 30 updates in 1 batch call
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}

	// Create 10 issues
	var items []api.ProjectItem
	var issueNums []string
	for i := 1; i <= 10; i++ {
		items = append(items, api.ProjectItem{
			ID: fmt.Sprintf("item-%d", i),
			Issue: &api.Issue{
				Number:     i,
				Title:      fmt.Sprintf("Issue %d", i),
				Repository: api.Repository{Owner: "testowner", Name: "testrepo"},
			},
		})
		issueNums = append(issueNums, fmt.Sprintf("%d", i))
	}
	mock.projectItems = items
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Set 3 fields
	opts := &moveOptions{status: "in_progress", priority: "high", branch: "v1.0.0", yes: true}

	// ACT
	err := runMoveWithDeps(cmd, issueNums, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Should use single batch call (30 updates < 50 batch size)
	if mock.batchUpdateCalls != 1 {
		t.Errorf("Expected 1 batch update call for 30 updates, got %d", mock.batchUpdateCalls)
	}
	// Should have 30 field updates (10 issues × 3 fields)
	if len(mock.fieldUpdates) != 30 {
		t.Errorf("Expected 30 field updates (10 issues × 3 fields), got %d", len(mock.fieldUpdates))
	}
}

// =============================================================================
// REQ-648: --force Confirmation Prompt Tests
// =============================================================================

// testIDPFMoveConfig returns a config with IDPF framework enabled
// When the active-branch fetch failed, the discarded error left the release
// list empty and validation allowed any branch — a mistyped --branch was
// written to the item. The transition must fail instead (#939).
func TestRunMoveWithDeps_ActiveBranchFetchFailureFailsTransition(t *testing.T) {
	mock := setupMockWithIssueAndBody(42, "Test Issue", "", "item-42")
	mock.getOpenIssuesByLabelErr = fmt.Errorf("GraphQL: secondary rate limit")
	cfg := testIDPFMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", branch: "release/v9.9.9", yes: true}
	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)

	if err == nil || !strings.Contains(err.Error(), "secondary rate limit") {
		t.Fatalf("expected the transition to fail with the fetch error, got: %v", err)
	}
	if len(mock.fieldUpdates) != 0 {
		t.Errorf("no field may be written when branches could not be verified, got %v", mock.fieldUpdates)
	}
}

func testIDPFMoveConfig() *config.Config {
	cfg := testMoveConfig()
	cfg.Framework = "IDPF"
	return cfg
}

// setupMockWithIssueAndBody creates a mock client with an issue that has a body
func setupMockWithIssueAndBody(number int, title, body, itemID string) *mockMoveClient {
	mock := newMockMoveClient()
	mock.project = &api.Project{
		ID:     "proj-1",
		Number: 1,
		Title:  "Test Project",
	}
	mock.issues[fmt.Sprintf("testowner/testrepo#%d", number)] = &api.Issue{
		ID:     fmt.Sprintf("issue-%d", number),
		Number: number,
		Title:  title,
		Body:   body,
		Repository: api.Repository{
			Owner: "testowner",
			Name:  "testrepo",
		},
	}
	mock.projectItems = []api.ProjectItem{
		{
			ID: itemID,
			Issue: &api.Issue{
				Number: number,
				Title:  title,
				Body:   body,
				Repository: api.Repository{
					Owner: "testowner",
					Name:  "testrepo",
				},
			},
		},
	}
	return mock
}

// AC-648-1: Given --force --yes, Then confirmation prompt is skipped
func TestRunMoveWithDeps_ForceYesSkipsConfirmation(t *testing.T) {
	// ARRANGE - Issue with unchecked checkboxes to trigger force warning
	body := "## Acceptance Criteria\n- [ ] Unchecked item\n- [ ] Another unchecked"
	mock := setupMockWithIssueAndBody(42, "Test Issue", body, "item-42")
	// Add active branch for branch assignment requirement
	mock.openIssuesByLabel["branch"] = []api.Issue{
		{ID: "BRANCH_1", Number: 100, Title: "Branch: release/v1.0.0", State: "OPEN"},
	}
	cfg := testIDPFMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// --force --yes with status=done to trigger checkbox validation
	opts := &moveOptions{status: "done", force: true, yes: true}

	// ACT
	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := buf.String()
	// Should NOT prompt for confirmation (no "Proceed anyway?" in output)
	if strings.Contains(output, "Proceed anyway?") {
		t.Error("Expected --yes to skip confirmation prompt, but 'Proceed anyway?' was found in output")
	}
	// Should still show warning about bypassing
	if !strings.Contains(output, "Warning: --force bypasses checkbox validation") {
		t.Error("Expected warning about --force bypassing validation")
	}
	// Should have made the update
	if len(mock.fieldUpdates) == 0 {
		t.Error("Expected field updates to be made with --force --yes")
	}
}

// AC-778-1: Given --force without --yes, Then confirmation prompt is skipped (--force implies --yes)
func TestRunMoveWithDeps_ForceAloneSkipsConfirmation(t *testing.T) {
	// ARRANGE - Issue with unchecked checkboxes to trigger force warning
	body := "## Acceptance Criteria\n- [ ] Unchecked item\n- [ ] Another unchecked"
	mock := setupMockWithIssueAndBody(42, "Test Issue", body, "item-42")
	mock.openIssuesByLabel["branch"] = []api.Issue{
		{ID: "BRANCH_1", Number: 100, Title: "Branch: release/v1.0.0", State: "OPEN"},
	}
	cfg := testIDPFMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// --force WITHOUT --yes
	opts := &moveOptions{status: "done", force: true, yes: false}

	// ACT
	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := buf.String()
	// Should NOT prompt for confirmation (--force implies --yes)
	if strings.Contains(output, "Proceed anyway?") {
		t.Error("Expected --force alone to skip confirmation prompt, but 'Proceed anyway?' was found in output")
	}
	// Should still show warning about bypassing
	if !strings.Contains(output, "Warning: --force bypasses checkbox validation") {
		t.Error("Expected warning about --force bypassing validation")
	}
	// Should have made the update
	if len(mock.fieldUpdates) == 0 {
		t.Error("Expected field updates to be made with --force alone")
	}
}

// AC-648-2: Given IDPF project with --force bypass, Then WARNING is displayed after update
func TestRunMoveWithDeps_IDPFProjectOutputsWarningAfterForceBypass(t *testing.T) {
	// ARRANGE - Issue with unchecked checkboxes
	body := "## Acceptance Criteria\n- [ ] Unchecked item"
	mock := setupMockWithIssueAndBody(42, "Test Issue", body, "item-42")
	// Add active branch for branch assignment requirement
	mock.openIssuesByLabel["branch"] = []api.Issue{
		{ID: "BRANCH_1", Number: 100, Title: "Branch: release/v1.0.0", State: "OPEN"},
	}
	cfg := testIDPFMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "done", force: true, yes: true}

	// ACT
	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := buf.String()
	// IDPF project should show workflow warning after force bypass
	if !strings.Contains(output, "WARNING: Workflow rules may have been violated") {
		t.Errorf("Expected IDPF warning after force bypass, got output: %s", output)
	}
}

// AC-648-3: Given non-IDPF project with --force, Then no warning is displayed
func TestRunMoveWithDeps_NonIDPFProjectNoWarning(t *testing.T) {
	// ARRANGE - Issue with unchecked checkboxes but non-IDPF project
	body := "## Acceptance Criteria\n- [ ] Unchecked item"
	mock := setupMockWithIssueAndBody(42, "Test Issue", body, "item-42")
	cfg := testMoveConfig() // Non-IDPF (no Framework set, defaults to empty)
	cfg.Framework = "none"  // Explicitly non-IDPF

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "done", force: true, yes: true}

	// ACT
	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := buf.String()
	// Non-IDPF project should NOT show workflow warning
	if strings.Contains(output, "WARNING: Workflow rules may have been violated") {
		t.Errorf("Expected no IDPF warning for non-IDPF project, but got output: %s", output)
	}
	// Also no confirmation prompt for non-IDPF
	if strings.Contains(output, "Warning: --force bypasses checkbox validation") {
		t.Errorf("Expected no force warning for non-IDPF project, but got output: %s", output)
	}
}

// AC-648-4: Given --force with unchecked items, Then unchecked items are shown in prompt
func TestRunMoveWithDeps_ForceShowsUncheckedItemsInWarning(t *testing.T) {
	// ARRANGE - Issue with specific unchecked checkboxes
	body := "## Acceptance Criteria\n- [ ] Item A\n- [x] Done item\n- [ ] Item B"
	mock := setupMockWithIssueAndBody(42, "Test Issue", body, "item-42")
	mock.openIssuesByLabel["branch"] = []api.Issue{
		{ID: "BRANCH_1", Number: 100, Title: "Branch: release/v1.0.0", State: "OPEN"},
	}
	cfg := testIDPFMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "done", force: true, yes: true}

	// ACT
	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := buf.String()
	// Should show the count of unchecked checkboxes in warning
	if !strings.Contains(output, "#42 has 2 unchecked checkbox(es)") {
		t.Errorf("Expected warning to show unchecked checkbox count, got output: %s", output)
	}
}

// AC-648-5: Given --force dry-run, Then no changes are made but validation passes
func TestRunMoveWithDeps_ForceDryRunNoChanges(t *testing.T) {
	// ARRANGE - Issue with unchecked checkboxes
	body := "## Acceptance Criteria\n- [ ] Unchecked item"
	mock := setupMockWithIssueAndBody(42, "Test Issue", body, "item-42")
	mock.openIssuesByLabel["branch"] = []api.Issue{
		{ID: "BRANCH_1", Number: 100, Title: "Branch: release/v1.0.0", State: "OPEN"},
	}
	cfg := testIDPFMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// dry-run with --force - no changes should be made
	opts := &moveOptions{status: "done", force: true, dryRun: true}

	// ACT
	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Dry-run should not make any field updates
	if len(mock.fieldUpdates) != 0 {
		t.Errorf("Expected no field updates in dry-run mode, got %d", len(mock.fieldUpdates))
	}
}

// AC-648-6: Given --force with no unchecked checkboxes, Then no confirmation needed
func TestRunMoveWithDeps_ForceNoUncheckedNoWarning(t *testing.T) {
	// ARRANGE - Issue with all checkboxes checked
	body := "## Acceptance Criteria\n- [x] Done item\n- [x] Another done item"
	mock := setupMockWithIssueAndBody(42, "Test Issue", body, "item-42")
	mock.openIssuesByLabel["branch"] = []api.Issue{
		{ID: "BRANCH_1", Number: 100, Title: "Branch: release/v1.0.0", State: "OPEN"},
	}
	cfg := testIDPFMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "done", force: true, yes: true}

	// ACT
	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := buf.String()
	// With all checkboxes checked, --force doesn't bypass anything, so no warning
	if strings.Contains(output, "Warning: --force bypasses checkbox validation") {
		t.Errorf("Expected no bypass warning when all checkboxes are checked, got output: %s", output)
	}
	// Also no workflow violation warning since nothing was bypassed
	if strings.Contains(output, "WARNING: Workflow rules may have been violated") {
		t.Errorf("Expected no workflow warning when no bypass occurred, got output: %s", output)
	}
}

// =============================================================================
// Assigned Label Tests
// =============================================================================

func TestRunMoveWithDeps_BranchAddsAssignedLabel(t *testing.T) {
	mock := setupMockWithIssue(42, "Test Issue", "item-42")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{branch: "release/v1.0"}

	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify AddLabelToIssue was called with 'assigned'
	if len(mock.addLabelCalls) != 1 {
		t.Fatalf("Expected 1 AddLabelToIssue call, got %d", len(mock.addLabelCalls))
	}
	call := mock.addLabelCalls[0]
	if call.labelName != "assigned" {
		t.Errorf("Expected label 'assigned', got %q", call.labelName)
	}
	if call.issueID != "issue-42" {
		t.Errorf("Expected issueID 'issue-42', got %q", call.issueID)
	}
	if call.owner != "testowner" || call.repo != "testrepo" {
		t.Errorf("Expected owner/repo testowner/testrepo, got %s/%s", call.owner, call.repo)
	}
}

func TestRunMoveWithDeps_BranchCurrentAddsAssignedLabel(t *testing.T) {
	mock := setupMockWithIssue(42, "Test Issue", "item-42")
	mock.openIssuesByLabel["branch"] = []api.Issue{
		{
			ID:     "TRACKER_200",
			Number: 200,
			Title:  "Branch: release/v1.3.0",
			State:  "OPEN",
		},
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{branch: "current"}

	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify AddLabelToIssue was called
	if len(mock.addLabelCalls) != 1 {
		t.Fatalf("Expected 1 AddLabelToIssue call, got %d", len(mock.addLabelCalls))
	}
	if mock.addLabelCalls[0].labelName != "assigned" {
		t.Errorf("Expected label 'assigned', got %q", mock.addLabelCalls[0].labelName)
	}
}

func TestRunMoveWithDeps_BacklogRemovesAssignedLabel(t *testing.T) {
	mock := setupMockWithIssue(42, "Test Issue", "item-42")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{backlog: true}

	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify RemoveLabelFromIssue was called with 'assigned'
	if len(mock.removeLabelCalls) != 1 {
		t.Fatalf("Expected 1 RemoveLabelFromIssue call, got %d", len(mock.removeLabelCalls))
	}
	call := mock.removeLabelCalls[0]
	if call.labelName != "assigned" {
		t.Errorf("Expected label 'assigned', got %q", call.labelName)
	}
	if call.issueID != "issue-42" {
		t.Errorf("Expected issueID 'issue-42', got %q", call.issueID)
	}
}

func TestRunMoveWithDeps_BacklogSkipsClosedIssues(t *testing.T) {
	mock := newMockMoveClient()
	mock.project = &api.Project{
		ID:     "proj-1",
		Number: 1,
		Title:  "Test Project",
	}
	mock.issues["testowner/testrepo#42"] = &api.Issue{
		ID:     "issue-42",
		Number: 42,
		Title:  "Closed Issue",
		State:  "CLOSED",
		Repository: api.Repository{
			Owner: "testowner",
			Name:  "testrepo",
		},
	}
	mock.projectItems = []api.ProjectItem{
		{
			ID: "item-42",
			Issue: &api.Issue{
				ID:     "issue-42",
				Number: 42,
				State:  "CLOSED",
				Repository: api.Repository{
					Owner: "testowner",
					Name:  "testrepo",
				},
			},
		},
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{backlog: true}

	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify RemoveLabelFromIssue was NOT called for closed issue
	if len(mock.removeLabelCalls) != 0 {
		t.Errorf("Expected 0 RemoveLabelFromIssue calls for closed issue, got %d", len(mock.removeLabelCalls))
	}
}

func TestRunMoveWithDeps_BulkBranchAddsAssignedLabelToAll(t *testing.T) {
	mock := newMockMoveClient()
	mock.project = &api.Project{
		ID:     "proj-1",
		Number: 1,
		Title:  "Test Project",
	}
	// Set up 3 issues
	for _, num := range []int{1, 2, 3} {
		key := fmt.Sprintf("testowner/testrepo#%d", num)
		mock.issues[key] = &api.Issue{
			ID:     fmt.Sprintf("issue-%d", num),
			Number: num,
			Title:  fmt.Sprintf("Issue %d", num),
			State:  "OPEN",
			Repository: api.Repository{
				Owner: "testowner",
				Name:  "testrepo",
			},
		}
	}
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{ID: "issue-1", Number: 1, State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-2", Issue: &api.Issue{ID: "issue-2", Number: 2, State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
		{ID: "item-3", Issue: &api.Issue{ID: "issue-3", Number: 3, State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}}},
	}
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{branch: "release/v1.0", yes: true}

	err := runMoveWithDeps(cmd, []string{"1", "2", "3"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify AddLabelToIssue was called for each issue
	if len(mock.addLabelCalls) != 3 {
		t.Fatalf("Expected 3 AddLabelToIssue calls, got %d", len(mock.addLabelCalls))
	}
	for i, call := range mock.addLabelCalls {
		if call.labelName != "assigned" {
			t.Errorf("Call %d: expected label 'assigned', got %q", i, call.labelName)
		}
	}
}

func TestRunMoveWithDeps_LabelErrorIsNonBlocking(t *testing.T) {
	mock := setupMockWithIssue(42, "Test Issue", "item-42")
	mock.addLabelErr = fmt.Errorf("label API error")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{branch: "release/v1.0"}

	// Should not return error even though label operation failed
	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Label error should be non-blocking, got: %v", err)
	}
}

func TestRunMoveWithDeps_StatusOnlyDoesNotTouchLabels(t *testing.T) {
	mock := setupMockWithIssue(42, "Test Issue", "item-42")
	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress"}

	err := runMoveWithDeps(cmd, []string{"42"}, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify no label calls when only status is changed
	if len(mock.addLabelCalls) != 0 {
		t.Errorf("Expected 0 AddLabelToIssue calls for status-only change, got %d", len(mock.addLabelCalls))
	}
	if len(mock.removeLabelCalls) != 0 {
		t.Errorf("Expected 0 RemoveLabelFromIssue calls for status-only change, got %d", len(mock.removeLabelCalls))
	}
}

func TestRunMoveWithDeps_RecursiveBatchFallback_NilMap(t *testing.T) {
	// Regression: GetSubIssuesBatch returns nil map on error,
	// fallback path must not panic when writing to subIssuesMap.
	mock := newMockMoveClient()
	mock.project = &api.Project{ID: "proj-1", Number: 1, Title: "Test Project"}

	mock.projectItems = []api.ProjectItem{
		{
			ID: "item-1",
			Issue: &api.Issue{
				Number:     1,
				Title:      "Parent Issue",
				Repository: api.Repository{Owner: "testowner", Name: "testrepo"},
			},
		},
	}

	// Sub-issues available for individual fetch (fallback)
	mock.subIssues["testowner/testrepo#1"] = []api.SubIssue{
		{
			ID:         "issue-2",
			Number:     2,
			Title:      "Sub Issue",
			Repository: api.Repository{Owner: "testowner", Name: "testrepo"},
		},
	}

	// Batch fails (returns nil map) but individual GetSubIssues succeeds
	mock.getSubIssuesBatchErr = fmt.Errorf("batch not supported")

	cfg := testMoveConfig()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &moveOptions{status: "in_progress", recursive: true, yes: true, depth: 10}

	// ACT — should not panic
	err := runMoveWithDeps(cmd, []string{"1"}, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Batch was attempted (once per recursion level)
	if mock.getSubIssuesBatchCalls < 1 {
		t.Errorf("Expected at least 1 batch call, got %d", mock.getSubIssuesBatchCalls)
	}
	// Individual fallback was used
	if mock.getSubIssuesCalls < 1 {
		t.Errorf("Expected at least 1 individual GetSubIssues call, got %d", mock.getSubIssuesCalls)
	}
	// Parent should be updated (sub-issue may be skipped if not in project)
	if len(mock.fieldUpdates) < 1 {
		t.Errorf("Expected at least 1 field update (parent), got %d", len(mock.fieldUpdates))
	}
}
