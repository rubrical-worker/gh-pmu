package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
)

// mockIntakeClient implements intakeClient for testing
type mockIntakeClient struct {
	fakeAssigneeResolver
	project          *api.Project
	projectItems     []api.ProjectItem
	repositoryIssues []api.Issue

	// unknownMembership marks issue IDs whose project membership could not be
	// confirmed (member of more projects than one projectItems page returns).
	unknownMembership map[string]bool

	// Call tracking
	intakeSearchCalls []intakeSearchCall

	// Error injection
	getProjectErr             error
	searchRepositoryIssuesErr error
	getProjectFieldsErr       error

	// Bulk field-setting tracking (#833)
	projectFields         []api.ProjectField
	getProjectFieldsCalls int
	lastFieldsPassed      []api.ProjectField

	// Batched apply (#918). Added items get ID "item-<issueID>".
	batchAddCalls    [][]string
	batchUpdateCalls [][]api.FieldUpdate
	addFailures      map[string]string // issueID -> error
	updateFailures   map[string]string // itemID -> error
}

func (m *mockIntakeClient) BatchAddIssuesToProject(projectID string, issueIDs []string) ([]api.BatchAddResult, error) {
	m.batchAddCalls = append(m.batchAddCalls, issueIDs)
	results := make([]api.BatchAddResult, 0, len(issueIDs))
	for _, id := range issueIDs {
		if msg, failed := m.addFailures[id]; failed {
			results = append(results, api.BatchAddResult{IssueID: id, Error: msg})
			continue
		}
		results = append(results, api.BatchAddResult{IssueID: id, ItemID: "item-" + id, Success: true})
	}
	return results, nil
}

func (m *mockIntakeClient) BatchUpdateProjectItemFields(projectID string, updates []api.FieldUpdate, fields []api.ProjectField) ([]api.BatchUpdateResult, error) {
	m.batchUpdateCalls = append(m.batchUpdateCalls, updates)
	m.lastFieldsPassed = fields
	results := make([]api.BatchUpdateResult, 0, len(updates))
	for _, u := range updates {
		if msg, failed := m.updateFailures[u.ItemID]; failed {
			results = append(results, api.BatchUpdateResult{ItemID: u.ItemID, FieldName: u.FieldName, Error: msg})
			continue
		}
		results = append(results, api.BatchUpdateResult{ItemID: u.ItemID, FieldName: u.FieldName, Success: true})
	}
	return results, nil
}

type intakeSearchCall struct {
	owner, repo string
	labels      []string
	projectID   string
}

func newMockIntakeClient() *mockIntakeClient {
	return &mockIntakeClient{
		project: &api.Project{
			ID:    "proj-1",
			Title: "Test Project",
		},
		projectItems:     []api.ProjectItem{},
		repositoryIssues: []api.Issue{},
	}
}

func (m *mockIntakeClient) GetProject(owner string, number int) (*api.Project, error) {
	if m.getProjectErr != nil {
		return nil, m.getProjectErr
	}
	return m.project, nil
}

// SearchIntakeCandidates answers from repositoryIssues, treating an issue as on
// the project when projectItems (the simulated board) contains it.
func (m *mockIntakeClient) SearchIntakeCandidates(owner, repo string, labels []string, projectID string) ([]api.IntakeCandidate, error) {
	m.intakeSearchCalls = append(m.intakeSearchCalls, intakeSearchCall{owner: owner, repo: repo, labels: labels, projectID: projectID})
	if m.searchRepositoryIssuesErr != nil {
		return nil, m.searchRepositoryIssuesErr
	}
	onBoard := make(map[string]bool)
	for _, item := range m.projectItems {
		if item.Issue != nil {
			onBoard[item.Issue.ID] = true
		}
	}
	candidates := make([]api.IntakeCandidate, 0, len(m.repositoryIssues))
	for _, issue := range m.repositoryIssues {
		candidates = append(candidates, api.IntakeCandidate{
			Issue:             issue,
			InProject:         onBoard[issue.ID],
			MembershipUnknown: m.unknownMembership[issue.ID],
		})
	}
	return candidates, nil
}

func (m *mockIntakeClient) GetProjectFields(projectID string) ([]api.ProjectField, error) {
	m.getProjectFieldsCalls++
	if m.getProjectFieldsErr != nil {
		return nil, m.getProjectFieldsErr
	}
	return m.projectFields, nil
}

func TestIntakeCommand(t *testing.T) {
	t.Run("has correct command structure", func(t *testing.T) {
		cmd := newIntakeCommand()

		if cmd.Use != "intake" {
			t.Errorf("expected Use to be 'intake', got %s", cmd.Use)
		}

		if cmd.Short == "" {
			t.Error("expected Short description to be set")
		}

		// Check aliases
		if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "in" {
			t.Error("expected 'in' alias")
		}
	})

	t.Run("has required flags", func(t *testing.T) {
		cmd := newIntakeCommand()

		// Check --apply flag
		applyFlag := cmd.Flags().Lookup("apply")
		if applyFlag == nil {
			t.Fatal("expected --apply flag")
		}
		if applyFlag.Shorthand != "a" {
			t.Errorf("expected --apply shorthand 'a', got %s", applyFlag.Shorthand)
		}
		// Verify NoOptDefVal is set to allow --apply without a value
		if applyFlag.NoOptDefVal == "" {
			t.Error("expected --apply NoOptDefVal to be set (allow usage without value)")
		}

		// Check --dry-run flag
		dryRunFlag := cmd.Flags().Lookup("dry-run")
		if dryRunFlag == nil {
			t.Error("expected --dry-run flag")
		}

		// Check --json flag
		jsonFlag := cmd.Flags().Lookup("json")
		if jsonFlag == nil {
			t.Error("expected --json flag")
		}

		// Check --label flag
		labelFlag := cmd.Flags().Lookup("label")
		if labelFlag == nil {
			t.Fatal("expected --label flag")
		}
		if labelFlag.Shorthand != "l" {
			t.Errorf("expected --label shorthand 'l', got %s", labelFlag.Shorthand)
		}

		// Check --assignee flag
		assigneeFlag := cmd.Flags().Lookup("assignee")
		if assigneeFlag == nil {
			t.Error("expected --assignee flag")
		}
	})

	t.Run("rejects stray positional arguments", func(t *testing.T) {
		// #867 finding 1: --apply has NoOptDefVal, so `--apply status:...` leaves
		// `status:...` as a positional argument. Without an Args validator it was
		// silently ignored; NoArgs makes it fail loudly.
		cmd := newIntakeCommand()
		if cmd.Args == nil {
			t.Fatal("expected intake to set an Args validator (cobra.NoArgs)")
		}
		if err := cmd.Args(cmd, []string{"status:backlog,priority:p1"}); err == nil {
			t.Error("expected error for stray positional argument, got nil")
		}
		if err := cmd.Args(cmd, []string{}); err != nil {
			t.Errorf("expected no error with zero positional args, got: %v", err)
		}
	})

	t.Run("example uses --apply= form for field values", func(t *testing.T) {
		// #867 finding 1: space-separated `--apply status:...` is silently ignored,
		// so the help text must show the `--apply=status:...` form instead.
		cmd := newIntakeCommand()
		if strings.Contains(cmd.Example, "--apply status:") {
			t.Error("Example must not show space-separated `--apply status:...` (silently ignored)")
		}
		if !strings.Contains(cmd.Example, "--apply=status:") {
			t.Error("Example should show the `--apply=status:...` form")
		}
	})

	t.Run("command is registered in root", func(t *testing.T) {
		root := NewRootCommand()
		buf := new(bytes.Buffer)
		root.SetOut(buf)
		root.SetArgs([]string{"intake", "--help"})
		err := root.Execute()
		if err != nil {
			t.Errorf("intake command not registered: %v", err)
		}
	})
}

func TestIntakeOptions(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		opts := &intakeOptions{}

		if opts.apply != "" {
			t.Error("apply should be empty string by default")
		}
		if opts.dryRun {
			t.Error("dryRun should be false by default")
		}
		if opts.json {
			t.Error("json should be false by default")
		}
		if len(opts.label) > 0 {
			t.Error("label should be empty by default")
		}
		if len(opts.assignee) > 0 {
			t.Error("assignee should be empty by default")
		}
	})
}

func TestOutputIntakeTable(t *testing.T) {
	t.Run("displays issues in table format", func(t *testing.T) {
		cmd := newIntakeCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)

		issues := []api.Issue{
			{
				Number:     1,
				Title:      "First issue",
				State:      "OPEN",
				Repository: api.Repository{Owner: "owner", Name: "repo"},
			},
			{
				Number:     2,
				Title:      "Second issue",
				State:      "OPEN",
				Repository: api.Repository{Owner: "owner", Name: "repo"},
			},
		}

		err := outputIntakeTable(cmd, issues)
		if err != nil {
			t.Fatalf("outputIntakeTable failed: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "NUMBER") || !strings.Contains(output, "TITLE") ||
			!strings.Contains(output, "REPOSITORY") || !strings.Contains(output, "STATE") {
			t.Errorf("expected table header with NUMBER/TITLE/REPOSITORY/STATE, got: %s", output)
		}
		for _, want := range []string{"#1", "First issue", "#2", "Second issue", "owner/repo", "OPEN"} {
			if !strings.Contains(output, want) {
				t.Errorf("expected output to contain %q, got: %s", want, output)
			}
		}
	})

	t.Run("truncates long titles to 50 chars", func(t *testing.T) {
		cmd := newIntakeCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)

		// Create issue with 60-character title
		longTitle := strings.Repeat("A", 60)
		issues := []api.Issue{
			{
				Number:     1,
				Title:      longTitle,
				State:      "OPEN",
				Repository: api.Repository{Owner: "owner", Name: "repo"},
			},
		}

		err := outputIntakeTable(cmd, issues)
		if err != nil {
			t.Fatalf("outputIntakeTable failed with long title: %v", err)
		}

		output := buf.String()
		wantTruncated := longTitle[:47] + "..."
		if !strings.Contains(output, wantTruncated) {
			t.Errorf("expected truncated title %q, got: %s", wantTruncated, output)
		}
		if strings.Contains(output, longTitle) {
			t.Errorf("expected full title to be truncated, but found it in full: %s", output)
		}
	})

	t.Run("handles empty issue list", func(t *testing.T) {
		cmd := newIntakeCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		issues := []api.Issue{}

		err := outputIntakeTable(cmd, issues)
		if err != nil {
			t.Fatalf("outputIntakeTable failed with empty list: %v", err)
		}

		// Header row is always emitted; no data rows for an empty list.
		output := buf.String()
		if !strings.Contains(output, "NUMBER") || !strings.Contains(output, "TITLE") {
			t.Errorf("expected table header even for empty list, got: %s", output)
		}
		if strings.Contains(output, "#") {
			t.Errorf("expected no issue rows for empty list, got: %s", output)
		}
	})
}

func TestOutputIntakeJSON(t *testing.T) {
	t.Run("outputs correct JSON structure with dry-run status", func(t *testing.T) {
		cmd := newIntakeCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)

		issues := []api.Issue{
			{
				Number:     42,
				Title:      "Test issue",
				State:      "OPEN",
				URL:        "https://github.com/owner/repo/issues/42",
				Repository: api.Repository{Owner: "owner", Name: "repo"},
			},
		}

		err := outputIntakeJSON(cmd, issues, "dry-run")
		if err != nil {
			t.Fatalf("outputIntakeJSON failed: %v", err)
		}

		var decoded intakeJSONOutput
		if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
			t.Fatalf("failed to decode JSON output: %v (raw: %s)", err, buf.String())
		}
		if decoded.Status != "dry-run" {
			t.Errorf("expected status 'dry-run', got %q", decoded.Status)
		}
		if decoded.Count != 1 {
			t.Errorf("expected count 1, got %d", decoded.Count)
		}
		if len(decoded.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(decoded.Issues))
		}
		got := decoded.Issues[0]
		if got.Number != 42 || got.Title != "Test issue" || got.State != "OPEN" {
			t.Errorf("unexpected issue fields: %+v", got)
		}
		if got.URL != "https://github.com/owner/repo/issues/42" {
			t.Errorf("expected URL to be preserved, got %q", got.URL)
		}
		if got.Repository != "owner/repo" {
			t.Errorf("expected repository 'owner/repo', got %q", got.Repository)
		}
	})

	t.Run("status field matches input status", func(t *testing.T) {
		// Test that various status values are preserved in the encoded output
		statuses := []string{"dry-run", "applied", "untracked"}
		for _, status := range statuses {
			cmd := newIntakeCommand()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			issues := []api.Issue{}

			err := outputIntakeJSON(cmd, issues, status)
			if err != nil {
				t.Fatalf("outputIntakeJSON failed with status %q: %v", status, err)
			}

			var decoded intakeJSONOutput
			if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
				t.Fatalf("failed to decode JSON output: %v (raw: %s)", err, buf.String())
			}
			if decoded.Status != status {
				t.Errorf("expected status %q in output, got %q", status, decoded.Status)
			}
		}
	})

	t.Run("count matches issues length", func(t *testing.T) {
		cmd := newIntakeCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)

		issues := []api.Issue{
			{Number: 1, Title: "Issue 1", Repository: api.Repository{Owner: "o", Name: "r"}},
			{Number: 2, Title: "Issue 2", Repository: api.Repository{Owner: "o", Name: "r"}},
			{Number: 3, Title: "Issue 3", Repository: api.Repository{Owner: "o", Name: "r"}},
		}

		err := outputIntakeJSON(cmd, issues, "test")
		if err != nil {
			t.Fatalf("outputIntakeJSON failed: %v", err)
		}

		var decoded intakeJSONOutput
		if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
			t.Fatalf("failed to decode JSON output: %v (raw: %s)", err, buf.String())
		}
		if decoded.Count != 3 {
			t.Errorf("expected count 3, got %d", decoded.Count)
		}
		if len(decoded.Issues) != 3 {
			t.Errorf("expected 3 issues in output, got %d", len(decoded.Issues))
		}
	})
}

func TestIntakeJSONOutput_Structure(t *testing.T) {
	t.Run("marshals to correct JSON format", func(t *testing.T) {
		output := intakeJSONOutput{
			Status: "dry-run",
			Count:  2,
			Issues: []intakeJSONIssue{
				{
					Number:     1,
					Title:      "First",
					State:      "OPEN",
					URL:        "https://github.com/owner/repo/issues/1",
					Repository: "owner/repo",
				},
				{
					Number:     2,
					Title:      "Second",
					State:      "OPEN",
					URL:        "https://github.com/owner/repo/issues/2",
					Repository: "owner/repo",
				},
			},
		}

		data, err := json.Marshal(output)
		if err != nil {
			t.Fatalf("Failed to marshal intakeJSONOutput: %v", err)
		}

		// Unmarshal and verify
		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to unmarshal JSON: %v", err)
		}

		if result["status"] != "dry-run" {
			t.Errorf("Expected status 'dry-run', got %v", result["status"])
		}

		if int(result["count"].(float64)) != 2 {
			t.Errorf("Expected count 2, got %v", result["count"])
		}

		issues, ok := result["issues"].([]interface{})
		if !ok {
			t.Fatal("Expected issues to be an array")
		}
		if len(issues) != 2 {
			t.Errorf("Expected 2 issues, got %d", len(issues))
		}
	})

	t.Run("intakeJSONIssue includes all fields", func(t *testing.T) {
		issue := intakeJSONIssue{
			Number:     42,
			Title:      "Test Issue",
			State:      "OPEN",
			URL:        "https://github.com/owner/repo/issues/42",
			Repository: "owner/repo",
		}

		data, err := json.Marshal(issue)
		if err != nil {
			t.Fatalf("Failed to marshal intakeJSONIssue: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to unmarshal JSON: %v", err)
		}

		expectedFields := []string{"number", "title", "state", "url", "repository"}
		for _, field := range expectedFields {
			if _, exists := result[field]; !exists {
				t.Errorf("Expected field %q to exist in JSON output", field)
			}
		}
	})
}

func TestFilterIntakeByLabel(t *testing.T) {
	issues := []api.Issue{
		{
			Number: 1,
			Title:  "Bug issue",
			Labels: []api.Label{{Name: "bug"}, {Name: "urgent"}},
		},
		{
			Number: 2,
			Title:  "Feature issue",
			Labels: []api.Label{{Name: "feature"}},
		},
		{
			Number: 3,
			Title:  "No labels",
			Labels: []api.Label{},
		},
	}

	t.Run("filters by single label", func(t *testing.T) {
		filtered := filterIntakeByLabel(issues, []string{"bug"})
		if len(filtered) != 1 {
			t.Errorf("Expected 1 issue, got %d", len(filtered))
		}
		if filtered[0].Number != 1 {
			t.Errorf("Expected issue #1, got #%d", filtered[0].Number)
		}
	})

	t.Run("filters by multiple labels (OR)", func(t *testing.T) {
		filtered := filterIntakeByLabel(issues, []string{"bug", "feature"})
		if len(filtered) != 2 {
			t.Errorf("Expected 2 issues, got %d", len(filtered))
		}
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		filtered := filterIntakeByLabel(issues, []string{"BUG"})
		if len(filtered) != 1 {
			t.Errorf("Expected 1 issue with case-insensitive match, got %d", len(filtered))
		}
	})

	t.Run("returns empty for non-matching label", func(t *testing.T) {
		filtered := filterIntakeByLabel(issues, []string{"nonexistent"})
		if len(filtered) != 0 {
			t.Errorf("Expected 0 issues, got %d", len(filtered))
		}
	})
}

func TestFilterIntakeByAssignee(t *testing.T) {
	issues := []api.Issue{
		{
			Number:    1,
			Title:     "Assigned to alice",
			Assignees: []api.Actor{{Login: "alice"}},
		},
		{
			Number:    2,
			Title:     "Assigned to bob",
			Assignees: []api.Actor{{Login: "bob"}},
		},
		{
			Number:    3,
			Title:     "Assigned to both",
			Assignees: []api.Actor{{Login: "alice"}, {Login: "bob"}},
		},
		{
			Number:    4,
			Title:     "No assignees",
			Assignees: []api.Actor{},
		},
	}

	t.Run("filters by single assignee", func(t *testing.T) {
		filtered := filterIntakeByAssignee(issues, []string{"alice"})
		if len(filtered) != 2 {
			t.Errorf("Expected 2 issues assigned to alice, got %d", len(filtered))
		}
	})

	t.Run("filters by multiple assignees (OR)", func(t *testing.T) {
		filtered := filterIntakeByAssignee(issues, []string{"alice", "bob"})
		if len(filtered) != 3 {
			t.Errorf("Expected 3 issues, got %d", len(filtered))
		}
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		filtered := filterIntakeByAssignee(issues, []string{"ALICE"})
		if len(filtered) != 2 {
			t.Errorf("Expected 2 issues with case-insensitive match, got %d", len(filtered))
		}
	})

	t.Run("returns empty for non-matching assignee", func(t *testing.T) {
		filtered := filterIntakeByAssignee(issues, []string{"charlie"})
		if len(filtered) != 0 {
			t.Errorf("Expected 0 issues, got %d", len(filtered))
		}
	})
}

func TestParseApplyFields(t *testing.T) {
	t.Run("parses single field", func(t *testing.T) {
		result := parseApplyFields("status:backlog")
		if len(result) != 1 {
			t.Errorf("Expected 1 field, got %d", len(result))
		}
		if result["status"] != "backlog" {
			t.Errorf("Expected status=backlog, got %s", result["status"])
		}
	})

	t.Run("parses multiple fields", func(t *testing.T) {
		result := parseApplyFields("status:backlog,priority:p1")
		if len(result) != 2 {
			t.Errorf("Expected 2 fields, got %d", len(result))
		}
		if result["status"] != "backlog" {
			t.Errorf("Expected status=backlog, got %s", result["status"])
		}
		if result["priority"] != "p1" {
			t.Errorf("Expected priority=p1, got %s", result["priority"])
		}
	})

	t.Run("handles empty string", func(t *testing.T) {
		result := parseApplyFields("")
		if len(result) != 0 {
			t.Errorf("Expected 0 fields, got %d", len(result))
		}
	})

	t.Run("handles whitespace", func(t *testing.T) {
		result := parseApplyFields(" status : backlog , priority : p1 ")
		if result["status"] != "backlog" {
			t.Errorf("Expected status=backlog, got %s", result["status"])
		}
		if result["priority"] != "p1" {
			t.Errorf("Expected priority=p1, got %s", result["priority"])
		}
	})

	t.Run("ignores invalid pairs", func(t *testing.T) {
		result := parseApplyFields("status:backlog,invalid,priority:p1")
		if len(result) != 2 {
			t.Errorf("Expected 2 fields (ignoring invalid), got %d", len(result))
		}
	})

	t.Run("handles trailing comma", func(t *testing.T) {
		result := parseApplyFields("status:backlog,")
		if len(result) != 1 {
			t.Errorf("Expected 1 field, got %d", len(result))
		}
	})
}

// ============================================================================
// runIntakeWithDeps Tests
// ============================================================================

func TestRunIntakeWithDeps_GetProjectError(t *testing.T) {
	mock := newMockIntakeClient()
	mock.getProjectErr = errors.New("project not found")
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newIntakeCommand()
	opts := &intakeOptions{}
	err := runIntakeWithDeps(cmd, opts, cfg, mock)

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to get project") {
		t.Errorf("expected 'failed to get project' error, got: %v", err)
	}
}

func TestRunIntakeWithDeps_SearchErrorWarnsAndContinues(t *testing.T) {
	mock := newMockIntakeClient()
	mock.searchRepositoryIssuesErr = errors.New("search failed")
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newIntakeCommand()
	var stderr bytes.Buffer
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(&stderr)
	opts := &intakeOptions{}
	if err := runIntakeWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("a per-repository search failure should warn, not abort: %v", err)
	}
	if !strings.Contains(stderr.String(), "failed to get issues from owner/repo") {
		t.Errorf("expected per-repository warning, got: %s", stderr.String())
	}
}

// TestIntakeClient_HasNoProjectBoardScan pins #918: intake determines membership
// from each issue's projectItems, so the client it depends on offers no way to
// page the whole project board.
func TestIntakeClient_HasNoProjectBoardScan(t *testing.T) {
	iface := reflect.TypeOf((*intakeClient)(nil)).Elem()
	if _, ok := iface.MethodByName("GetProjectItems"); ok {
		t.Error("intakeClient must not include GetProjectItems (full project board scan, #918)")
	}
	if _, ok := iface.MethodByName("SearchIntakeCandidates"); !ok {
		t.Error("intakeClient should resolve membership via SearchIntakeCandidates")
	}
}

func TestRunIntakeWithDeps_UsesMembershipSearchPerRepo(t *testing.T) {
	mock := newMockIntakeClient()
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo1", "owner/repo2"},
	}

	cmd := newIntakeCommand()
	cmd.SetOut(new(bytes.Buffer))
	opts := &intakeOptions{label: []string{"bug"}}
	if err := runIntakeWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.intakeSearchCalls) != 2 {
		t.Fatalf("expected one membership search per repository, got %d", len(mock.intakeSearchCalls))
	}
	for i, want := range []string{"repo1", "repo2"} {
		call := mock.intakeSearchCalls[i]
		if call.owner != "owner" || call.repo != want || call.projectID != "proj-1" {
			t.Errorf("call %d = %+v, want owner/%s on proj-1", i, call, want)
		}
		if len(call.labels) != 1 || call.labels[0] != "bug" {
			t.Errorf("call %d labels = %v, want [bug]", i, call.labels)
		}
	}
}

// TestRunIntakeWithDeps_MembershipUnknownWarnsAndExcludes: an issue whose
// membership could not be confirmed is neither listed as untracked nor silently
// dropped — it is named on stderr.
func TestRunIntakeWithDeps_MembershipUnknownWarnsAndExcludes(t *testing.T) {
	mock := newMockIntakeClient()
	mock.repositoryIssues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Busy Issue", State: "OPEN"},
		{ID: "issue-2", Number: 2, Title: "Plain Issue", State: "OPEN"},
	}
	mock.unknownMembership = map[string]bool{"issue-1": true}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newIntakeCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	opts := &intakeOptions{json: true}
	if err := runIntakeWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stderr.String(), "#1") || !strings.Contains(stderr.String(), "more than 20 projects") {
		t.Errorf("expected a warning naming #1 and the 20-project limit, got: %s", stderr.String())
	}
	var out intakeJSONOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, stdout.String())
	}
	if out.Count != 1 || out.Issues[0].Number != 2 {
		t.Errorf("expected only #2 listed as untracked, got %+v", out)
	}
}

func TestRunIntakeWithDeps_FilterByAssigneeAtMe(t *testing.T) {
	mock := newMockIntakeClient()
	mock.repositoryIssues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Mine", Assignees: []api.Actor{{Login: defaultFakeViewerLogin}}},
		{ID: "issue-2", Number: 2, Title: "Theirs", Assignees: []api.Actor{{Login: "bob"}}},
	}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newIntakeCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	opts := &intakeOptions{assignee: []string{"@me"}, json: true}
	if err := runIntakeWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out intakeJSONOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if out.Count != 1 || out.Issues[0].Number != 1 {
		t.Errorf("@me should resolve and match only #1, got %+v", out)
	}
}

func TestRunIntakeWithDeps_AllIssuesTracked(t *testing.T) {
	mock := newMockIntakeClient()
	mock.projectItems = []api.ProjectItem{
		{Issue: &api.Issue{ID: "issue-1", Number: 1}},
	}
	mock.repositoryIssues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Tracked Issue"},
	}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newIntakeCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &intakeOptions{}
	err := runIntakeWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "All issues are already tracked") {
		t.Errorf("expected 'All issues are already tracked' message, got: %s", output)
	}
}

func TestRunIntakeWithDeps_FindsUntrackedIssues(t *testing.T) {
	mock := newMockIntakeClient()
	mock.projectItems = []api.ProjectItem{} // No tracked issues
	mock.repositoryIssues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Untracked Issue", State: "OPEN"},
	}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newIntakeCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &intakeOptions{}
	err := runIntakeWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Found 1 untracked issue") {
		t.Errorf("expected 'Found 1 untracked issue' message, got: %s", output)
	}
}

func TestRunIntakeWithDeps_DryRun(t *testing.T) {
	mock := newMockIntakeClient()
	mock.repositoryIssues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Untracked Issue", State: "OPEN"},
	}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newIntakeCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &intakeOptions{dryRun: true}
	err := runIntakeWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Would add 1 issue") {
		t.Errorf("expected 'Would add 1 issue' message, got: %s", output)
	}
}

func TestRunIntakeWithDeps_JSONOutput(t *testing.T) {
	mock := newMockIntakeClient()
	mock.repositoryIssues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Untracked Issue", State: "OPEN"},
	}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newIntakeCommand()
	opts := &intakeOptions{json: true}
	err := runIntakeWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// JSON output goes to stdout - verified by no error
}

func TestRunIntakeWithDeps_FilterByLabel(t *testing.T) {
	mock := newMockIntakeClient()
	// Simulate server-side label filtering - only return issues matching the label filter
	mock.repositoryIssues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Bug Issue", Labels: []api.Label{{Name: "bug"}}},
	}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newIntakeCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &intakeOptions{label: []string{"bug"}}
	err := runIntakeWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the membership search received the label filter
	if len(mock.intakeSearchCalls) != 1 {
		t.Fatalf("expected 1 SearchIntakeCandidates call, got %d", len(mock.intakeSearchCalls))
	}
	if labels := mock.intakeSearchCalls[0].labels; len(labels) != 1 || labels[0] != "bug" {
		t.Errorf("expected labels [bug], got %v", labels)
	}

	output := buf.String()
	if !strings.Contains(output, "Found 1 untracked") {
		t.Errorf("expected 'Found 1 untracked' (filtered), got: %s", output)
	}
}

func TestRunIntakeWithDeps_FilterByAssignee(t *testing.T) {
	mock := newMockIntakeClient()
	mock.repositoryIssues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Alice Issue", Assignees: []api.Actor{{Login: "alice"}}},
		{ID: "issue-2", Number: 2, Title: "Bob Issue", Assignees: []api.Actor{{Login: "bob"}}},
	}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newIntakeCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &intakeOptions{assignee: []string{"alice"}}
	err := runIntakeWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Found 1 untracked") {
		t.Errorf("expected 'Found 1 untracked' (filtered), got: %s", output)
	}
}

func TestRunIntakeWithDeps_InvalidRepoFormat(t *testing.T) {
	mock := newMockIntakeClient()
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"invalid-no-slash"},
	}

	cmd := newIntakeCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &intakeOptions{}
	err := runIntakeWithDeps(cmd, opts, cfg, mock)
	// Should not error, just warn
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Either warning about invalid repo format or "all tracked" message is acceptable
	_ = output
}

func TestRunIntakeWithDeps_AllTrackedJSON(t *testing.T) {
	mock := newMockIntakeClient()
	mock.projectItems = []api.ProjectItem{
		{Issue: &api.Issue{ID: "issue-1", Number: 1}},
	}
	mock.repositoryIssues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Tracked Issue"},
	}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newIntakeCommand()
	opts := &intakeOptions{json: true}
	err := runIntakeWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// JSON output with empty issues goes to stdout - verified by no error
}

// =============================================================================
// Membership Classification
// =============================================================================

// Test that tracked issues are excluded and untracked ones listed
func TestRunIntakeWithDeps_SingleRepo_CorrectResults(t *testing.T) {
	mock := newMockIntakeClient()
	// Project has one tracked issue
	mock.projectItems = []api.ProjectItem{
		{Issue: &api.Issue{ID: "issue-1", Number: 1}},
	}
	// Repo has two issues - one tracked, one not
	mock.repositoryIssues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Tracked Issue"},
		{ID: "issue-2", Number: 2, Title: "Untracked Issue"},
	}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	var buf bytes.Buffer
	cmd := newIntakeCommand()
	cmd.SetOut(&buf)
	opts := &intakeOptions{}
	err := runIntakeWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Should find 1 untracked issue (table output goes to os.Stdout, only summary goes to cmd.Out)
	if !strings.Contains(output, "1 untracked") {
		t.Errorf("Expected output to contain '1 untracked', got: %s", output)
	}
	if !strings.Contains(output, "#2") || strings.Contains(output, "#1 ") {
		t.Errorf("Expected only #2 listed, got: %s", output)
	}
}

// =============================================================================
// --apply Without Value Tests (Issue #667)
// =============================================================================

// Test that --apply flag can be used without a value (NoOptDefVal)
func TestRunIntakeWithDeps_ApplyWithoutValue(t *testing.T) {
	mock := newMockIntakeClient()
	mock.repositoryIssues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Untracked Issue", State: "OPEN"},
	}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
		Defaults: config.Defaults{
			Status:   "backlog",
			Priority: "p2",
		},
	}

	// Simulate --apply being set without a value (NoOptDefVal triggers this)
	cmd := newIntakeCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	// Mark the apply flag as changed (as Cobra does when --apply is used)
	_ = cmd.Flags().Set("apply", " ") // NoOptDefVal value
	opts := &intakeOptions{apply: " "}

	err := runIntakeWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Added 1 issue") {
		t.Errorf("expected 'Added 1 issue' message, got: %s", output)
	}
}

// Test that --apply with explicit fields continues to work
func TestRunIntakeWithDeps_ApplyWithExplicitFields(t *testing.T) {
	mock := newMockIntakeClient()
	mock.repositoryIssues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Untracked Issue", State: "OPEN"},
	}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newIntakeCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	// Simulate --apply status:backlog,priority:p1
	_ = cmd.Flags().Set("apply", "status:backlog,priority:p1")
	opts := &intakeOptions{apply: "status:backlog,priority:p1"}

	err := runIntakeWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Added 1 issue") {
		t.Errorf("expected 'Added 1 issue' message, got: %s", output)
	}
}

// =============================================================================
// #833: Bulk Field-Setting Tests
// =============================================================================

// TestRunIntakeWithDeps_ApplyFetchesProjectFieldsOnce asserts that `intake --apply`
// fetches project fields exactly once regardless of issue count.
func TestRunIntakeWithDeps_ApplyFetchesProjectFieldsOnce(t *testing.T) {
	mock := newMockIntakeClient()
	mock.projectFields = []api.ProjectField{
		{ID: "f1", Name: "Status", DataType: "SINGLE_SELECT"},
		{ID: "f2", Name: "Priority", DataType: "SINGLE_SELECT"},
	}
	// 20 untracked issues
	for i := 1; i <= 20; i++ {
		mock.repositoryIssues = append(mock.repositoryIssues, api.Issue{
			ID:     "issue-" + string(rune('a'+i%26)),
			Number: i,
			Title:  "Issue",
			State:  "OPEN",
		})
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
		Defaults: config.Defaults{
			Status:   "backlog",
			Priority: "p2",
		},
	}

	cmd := newIntakeCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	_ = cmd.Flags().Set("apply", " ")
	opts := &intakeOptions{apply: " "}

	if err := runIntakeWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.getProjectFieldsCalls != 1 {
		t.Errorf("GetProjectFields should be called exactly once for 20 issues, got %d", mock.getProjectFieldsCalls)
	}
	// #918: one batched add for all 20 issues, one batched update carrying
	// 20 issues × 2 default fields — not a round trip per issue per field.
	if len(mock.batchAddCalls) != 1 || len(mock.batchAddCalls[0]) != 20 {
		t.Fatalf("expected 1 batched add of 20 issues, got %d calls", len(mock.batchAddCalls))
	}
	if len(mock.batchUpdateCalls) != 1 || len(mock.batchUpdateCalls[0]) != 40 {
		t.Fatalf("expected 1 batched update of 40 field updates, got %d calls", len(mock.batchUpdateCalls))
	}
	first := mock.batchUpdateCalls[0][:2]
	if first[0].ItemID != first[1].ItemID || first[0].ItemID != "item-"+mock.batchAddCalls[0][0] {
		t.Errorf("field updates should target the added item IDs, got %+v", first)
	}
}

func TestRunIntakeWithDeps_ApplyBuildsFieldUpdates(t *testing.T) {
	mock := newMockIntakeClient()
	mock.repositoryIssues = []api.Issue{{ID: "issue-1", Number: 1, Title: "One", State: "OPEN"}}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
		Defaults:     config.Defaults{Status: "backlog", Priority: "p2"},
		Fields: map[string]config.Field{
			"status":   {Field: "Status", Values: map[string]string{"backlog": "Backlog", "ready": "Ready"}},
			"priority": {Field: "Priority", Values: map[string]string{"p1": "P1", "p2": "P2"}},
		},
	}

	cmd := newIntakeCommand()
	cmd.SetOut(new(bytes.Buffer))
	// status comes from --apply, priority falls back to the config default,
	// Area is passed through as a generic field.
	_ = cmd.Flags().Set("apply", "status:ready,Area:cli")
	opts := &intakeOptions{apply: "status:ready,Area:cli"}
	if err := runIntakeWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.batchUpdateCalls) != 1 {
		t.Fatalf("expected 1 batched update, got %d", len(mock.batchUpdateCalls))
	}
	got := map[string]string{}
	for _, u := range mock.batchUpdateCalls[0] {
		if u.ItemID != "item-issue-1" {
			t.Errorf("update targets %q, want item-issue-1", u.ItemID)
		}
		got[u.FieldName] = u.Value
	}
	want := map[string]string{"Status": "Ready", "Priority": "P2", "Area": "cli"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("field updates = %v, want %v", got, want)
	}
}

// TestRunIntakeWithDeps_ApplyPartialBatchFailure: one issue fails to add, one is
// added but a field fails; each is reported on stderr and counted as failed.
func TestRunIntakeWithDeps_ApplyPartialBatchFailure(t *testing.T) {
	mock := newMockIntakeClient()
	mock.repositoryIssues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Ok", State: "OPEN"},
		{ID: "issue-2", Number: 2, Title: "Add fails", State: "OPEN"},
		{ID: "issue-3", Number: 3, Title: "Field fails", State: "OPEN"},
	}
	mock.addFailures = map[string]string{"issue-2": "permission denied"}
	mock.updateFailures = map[string]string{"item-issue-3": "option not found"}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
		Defaults:     config.Defaults{Status: "backlog"},
	}

	cmd := newIntakeCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	_ = cmd.Flags().Set("apply", " ")
	opts := &intakeOptions{apply: " "}
	if err := runIntakeWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stderr.String(), "Failed to add #2: permission denied") {
		t.Errorf("expected add failure for #2 on stderr, got: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "failed to set Status on #3: option not found") {
		t.Errorf("expected field failure for #3 on stderr, got: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Added 1 issue(s) to project (2 failed)") {
		t.Errorf("expected summary counting both failures, got: %s", stdout.String())
	}
	for _, u := range mock.batchUpdateCalls[0] {
		if u.ItemID == "item-issue-2" {
			t.Error("an issue that failed to add must not receive field updates")
		}
	}
}

// TestRunIntakeWithDeps_ApplyPassesPrefetchedFields asserts bulk calls receive the prefetched fields.
func TestRunIntakeWithDeps_ApplyPassesPrefetchedFields(t *testing.T) {
	mock := newMockIntakeClient()
	mock.projectFields = []api.ProjectField{
		{ID: "f1", Name: "Status", DataType: "SINGLE_SELECT"},
	}
	mock.repositoryIssues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "One", State: "OPEN"},
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newIntakeCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("apply", "status:backlog")
	opts := &intakeOptions{apply: "status:backlog"}

	if err := runIntakeWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.batchUpdateCalls) != 1 {
		t.Fatalf("expected 1 batched field update call, got %d", len(mock.batchUpdateCalls))
	}
	if len(mock.lastFieldsPassed) != 1 || mock.lastFieldsPassed[0].Name != "Status" {
		t.Errorf("expected prefetched Status field to be passed, got: %+v", mock.lastFieldsPassed)
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

// BenchmarkIntake_ClassifyCandidates measures membership classification over N
// open issues, half already on the project. Since #918 intake cost scales with
// open issues, not with the size of the project board.
func BenchmarkIntake_ClassifyCandidates(b *testing.B) {
	const issueCount = 500
	mock := newMockIntakeClient()
	for i := 0; i < issueCount; i++ {
		issue := api.Issue{ID: fmt.Sprintf("issue-%d", i), Number: i + 1, Title: "Issue", State: "OPEN"}
		mock.repositoryIssues = append(mock.repositoryIssues, issue)
		if i%2 == 0 {
			mock.projectItems = append(mock.projectItems, api.ProjectItem{Issue: &api.Issue{ID: issue.ID}})
		}
	}
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newIntakeCommand()
	cmd.SetOut(io.Discard)
	opts := &intakeOptions{json: true}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = runIntakeWithDeps(cmd, opts, cfg, mock)
	}

	b.ReportMetric(float64(issueCount), "issues_classified")
}
