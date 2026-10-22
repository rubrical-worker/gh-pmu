package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/spf13/cobra"
)

// mockListClient implements listClient for testing
type mockListClient struct {
	fakeAssigneeResolver
	project               *api.Project
	projectItems          []api.ProjectItem
	openIssuesByLabel     []api.Issue
	subIssueCounts        map[int]int
	searchResults         []api.Issue
	searchResultsByState  map[string][]api.Issue // state -> results (for dual-call tests)
	projectFieldsForIssue map[string][]api.FieldValue

	// Call tracking
	searchCalls           []api.SearchFilters
	searchLimits          []int
	getProjectItemsCalled bool
	projectItemsFilter    *api.ProjectItemsFilter

	// Error injection
	getProjectErr               error
	getProjectItemsErr          error
	getOpenIssuesByLabelErr     error
	getSubIssueCountsErr        error
	searchRepositoryIssuesErr   error
	getProjectFieldsForIssueErr error
}

func newMockListClient() *mockListClient {
	return &mockListClient{
		project: &api.Project{
			ID:    "proj-1",
			Title: "Test Project",
			URL:   "https://github.com/orgs/test/projects/1",
		},
		projectItems:          []api.ProjectItem{},
		subIssueCounts:        make(map[int]int),
		searchResults:         []api.Issue{},
		projectFieldsForIssue: make(map[string][]api.FieldValue),
	}
}

func (m *mockListClient) GetProject(owner string, number int) (*api.Project, error) {
	if m.getProjectErr != nil {
		return nil, m.getProjectErr
	}
	return m.project, nil
}

func (m *mockListClient) GetProjectItems(projectID string, filter *api.ProjectItemsFilter) ([]api.ProjectItem, error) {
	m.getProjectItemsCalled = true
	m.projectItemsFilter = filter
	if m.getProjectItemsErr != nil {
		return nil, m.getProjectItemsErr
	}
	return m.projectItems, nil
}

func (m *mockListClient) GetOpenIssuesByLabel(owner, repo, label string) ([]api.Issue, error) {
	if m.getOpenIssuesByLabelErr != nil {
		return nil, m.getOpenIssuesByLabelErr
	}
	return m.openIssuesByLabel, nil
}

func (m *mockListClient) GetSubIssueCounts(owner, repo string, numbers []int) (map[int]int, error) {
	if m.getSubIssueCountsErr != nil {
		return nil, m.getSubIssueCountsErr
	}
	return m.subIssueCounts, nil
}

func (m *mockListClient) SearchRepositoryIssues(owner, repo string, filters api.SearchFilters, limit int) ([]api.Issue, error) {
	m.searchCalls = append(m.searchCalls, filters)
	m.searchLimits = append(m.searchLimits, limit)
	if m.searchRepositoryIssuesErr != nil {
		return nil, m.searchRepositoryIssuesErr
	}
	// Support state-based results for dual-call tests
	if m.searchResultsByState != nil {
		if results, ok := m.searchResultsByState[filters.State]; ok {
			return results, nil
		}
	}
	return m.searchResults, nil
}

func (m *mockListClient) GetProjectFieldsForIssues(projectID string, issueIDs []string) (map[string][]api.FieldValue, error) {
	if m.getProjectFieldsForIssueErr != nil {
		return nil, m.getProjectFieldsForIssueErr
	}
	return m.projectFieldsForIssue, nil
}

// ============================================================================
// runListWithDeps Tests
// ============================================================================

func TestRunListWithDeps_Success(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID:    "item-1",
			Issue: &api.Issue{Number: 1, Title: "Issue 1", State: "OPEN"},
			FieldValues: []api.FieldValue{
				{Field: "Status", Value: "Backlog"},
			},
		},
		{
			ID:    "item-2",
			Issue: &api.Issue{Number: 2, Title: "Issue 2", State: "OPEN"},
			FieldValues: []api.FieldValue{
				{Field: "Status", Value: "In Progress"},
			},
		},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		Fields: map[string]config.Field{
			"status": {
				Field:  "Status",
				Values: map[string]string{"backlog": "Backlog", "in_progress": "In Progress"},
			},
		},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunListWithDeps_GetProjectError(t *testing.T) {
	mock := newMockListClient()
	mock.getProjectErr = errors.New("project not found")

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	opts := &listOptions{}
	err := runListWithDeps(cmd, opts, cfg, mock)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get project") {
		t.Errorf("expected 'failed to get project' error, got: %v", err)
	}
}

func TestRunListWithDeps_GetProjectItemsError(t *testing.T) {
	mock := newMockListClient()
	mock.getProjectItemsErr = errors.New("API error")

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	opts := &listOptions{}
	err := runListWithDeps(cmd, opts, cfg, mock)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get project items") {
		t.Errorf("expected 'failed to get project items' error, got: %v", err)
	}
}

func TestRunListWithDeps_WithStatusFilter(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID:    "item-1",
			Issue: &api.Issue{Number: 1, Title: "Backlog Issue"},
			FieldValues: []api.FieldValue{
				{Field: "Status", Value: "Backlog"},
			},
		},
		{
			ID:    "item-2",
			Issue: &api.Issue{Number: 2, Title: "Progress Issue"},
			FieldValues: []api.FieldValue{
				{Field: "Status", Value: "In Progress"},
			},
		},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		Fields: map[string]config.Field{
			"status": {
				Field:  "Status",
				Values: map[string]string{"backlog": "Backlog"},
			},
		},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{status: "backlog"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Backlog Issue") {
		t.Errorf("expected matching 'Backlog Issue' (status=Backlog) in output, got: %s", output)
	}
	if strings.Contains(output, "Progress Issue") {
		t.Errorf("expected non-matching 'Progress Issue' (status=In Progress) to be filtered out, got: %s", output)
	}
}

func TestRunListWithDeps_JSONOutput(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID:    "item-1",
			Issue: &api.Issue{Number: 42, Title: "JSON Test Issue", State: "OPEN"},
			FieldValues: []api.FieldValue{
				{Field: "Status", Value: "Backlog"},
			},
		},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		Fields: map[string]config.Field{
			"status": {
				Field:  "Status",
				Values: map[string]string{"backlog": "Backlog"},
			},
		},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{jsonFields: "number,title,state,url,repository,assignees,fieldValues"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunListWithDeps_EmptyProject(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No issues found") {
		t.Error("expected 'No issues found' in output")
	}
}

func TestRunListWithDeps_InvalidRepoFormat(t *testing.T) {
	mock := newMockListClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	opts := &listOptions{repo: "invalid-no-slash"}
	err := runListWithDeps(cmd, opts, cfg, mock)

	if err == nil {
		t.Fatal("expected error for invalid repo format")
	}
	if !strings.Contains(err.Error(), "invalid --repo format") {
		t.Errorf("expected 'invalid --repo format' error, got: %v", err)
	}
}

func TestRunListWithDeps_EmptyOwnerRepoFormat(t *testing.T) {
	mock := newMockListClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	opts := &listOptions{repo: "/repo"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err == nil {
		t.Fatal("expected error for empty owner in repo format")
	}
	if !strings.Contains(err.Error(), "invalid --repo format") {
		t.Errorf("expected 'invalid --repo format' error, got: %v", err)
	}
}

func TestRunListWithDeps_EmptyRepoNameFormat(t *testing.T) {
	mock := newMockListClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	opts := &listOptions{repo: "owner/"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err == nil {
		t.Fatal("expected error for empty repo name in repo format")
	}
	if !strings.Contains(err.Error(), "invalid --repo format") {
		t.Errorf("expected 'invalid --repo format' error, got: %v", err)
	}
}

func TestRunListWithDeps_ValidRepoFormat(t *testing.T) {
	mock := newMockListClient()
	mock.project = &api.Project{ID: "proj-1", Title: "Test"}
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	opts := &listOptions{repo: "owner/repo"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	// Should not error on valid repo format (may error on no items, that's fine)
	if err != nil && strings.Contains(err.Error(), "invalid --repo format") {
		t.Errorf("valid repo format should not produce format error, got: %v", err)
	}
}

func TestRunListWithDeps_WithPriorityFilter(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID:    "item-1",
			Issue: &api.Issue{Number: 1, Title: "P1 Issue"},
			FieldValues: []api.FieldValue{
				{Field: "Priority", Value: "P1"},
			},
		},
		{
			ID:    "item-2",
			Issue: &api.Issue{Number: 2, Title: "P2 Issue"},
			FieldValues: []api.FieldValue{
				{Field: "Priority", Value: "P2"},
			},
		},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		Fields: map[string]config.Field{
			"priority": {
				Field:  "Priority",
				Values: map[string]string{"p1": "P1", "p2": "P2"},
			},
		},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{priority: "p1"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "P1 Issue") {
		t.Errorf("expected matching 'P1 Issue' (priority=P1) in output, got: %s", output)
	}
	if strings.Contains(output, "P2 Issue") {
		t.Errorf("expected non-matching 'P2 Issue' (priority=P2) to be filtered out, got: %s", output)
	}
}

func TestRunListWithDeps_WithAssigneeFilter(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID: "item-1",
			Issue: &api.Issue{
				Number:    1,
				Title:     "Alice's Issue",
				Assignees: []api.Actor{{Login: "alice"}},
			},
		},
		{
			ID: "item-2",
			Issue: &api.Issue{
				Number:    2,
				Title:     "Bob's Issue",
				Assignees: []api.Actor{{Login: "bob"}},
			},
		},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{assignee: "alice"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Alice's Issue") {
		t.Errorf("expected matching \"Alice's Issue\" (assignee alice) in output, got: %s", output)
	}
	if strings.Contains(output, "Bob's Issue") {
		t.Errorf("expected non-matching \"Bob's Issue\" (assignee bob) to be filtered out, got: %s", output)
	}
}

func TestRunListWithDeps_WithLabelFilter(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID: "item-1",
			Issue: &api.Issue{
				Number: 1,
				Title:  "Bug Issue",
				Labels: []api.Label{{Name: "bug"}},
			},
		},
		{
			ID: "item-2",
			Issue: &api.Issue{
				Number: 2,
				Title:  "Feature Issue",
				Labels: []api.Label{{Name: "enhancement"}},
			},
		},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{label: "bug"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Bug Issue") {
		t.Errorf("expected matching 'Bug Issue' (label bug) in output, got: %s", output)
	}
	if strings.Contains(output, "Feature Issue") {
		t.Errorf("expected non-matching 'Feature Issue' (label enhancement) to be filtered out, got: %s", output)
	}
}

func TestRunListWithDeps_WithSearchFilter(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID: "item-1",
			Issue: &api.Issue{
				Number: 1,
				Title:  "Fix login bug",
				Body:   "Authentication issue",
			},
		},
		{
			ID: "item-2",
			Issue: &api.Issue{
				Number: 2,
				Title:  "Add feature",
				Body:   "New functionality",
			},
		},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{search: "login"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Fix login bug") {
		t.Errorf("expected matching 'Fix login bug' (search login) in output, got: %s", output)
	}
	if strings.Contains(output, "Add feature") {
		t.Errorf("expected non-matching 'Add feature' to be filtered out, got: %s", output)
	}
}

func TestRunListWithDeps_WithReleaseFilter(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID:    "item-1",
			Issue: &api.Issue{Number: 1, Title: "Target Release Item"},
			FieldValues: []api.FieldValue{
				{Field: "Release", Value: "v1.0.0"},
			},
		},
		{
			ID:    "item-2",
			Issue: &api.Issue{Number: 2, Title: "Skipped Milestone Item"},
			FieldValues: []api.FieldValue{
				{Field: "Release", Value: "v2.0.0"},
			},
		},
	}

	// No repositories configured so items flow through GetProjectItems and the
	// branch (Release) field filter is exercised client-side on the fixtures above.
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{branch: "v1.0.0"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Target Release Item") {
		t.Errorf("expected matching 'Target Release Item' (Release=v1.0.0) in output, got: %s", output)
	}
	if strings.Contains(output, "Skipped Milestone Item") {
		t.Errorf("expected non-matching 'Skipped Milestone Item' (Release=v2.0.0) to be filtered out, got: %s", output)
	}
}

func TestRunListWithDeps_WithReleaseCurrentFilter(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID:    "item-1",
			Issue: &api.Issue{Number: 1, Title: "Release Issue"},
			FieldValues: []api.FieldValue{
				{Field: "Release", Value: "v1.0.0"},
			},
		},
	}
	mock.openIssuesByLabel = []api.Issue{
		{Number: 100, Title: "Release: v1.0.0"},
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newListCommand()
	opts := &listOptions{branch: "current"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunListWithDeps_WithNoReleaseFilter(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID:    "item-1",
			Issue: &api.Issue{Number: 1, Title: "Has Release"},
			FieldValues: []api.FieldValue{
				{Field: "Release", Value: "v1.0.0"},
			},
		},
		{
			ID:    "item-2",
			Issue: &api.Issue{Number: 2, Title: "No Release"},
			FieldValues: []api.FieldValue{
				{Field: "Status", Value: "Backlog"},
			},
		},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{noBranch: true}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No Release") {
		t.Errorf("expected matching 'No Release' (empty branch field) in output, got: %s", output)
	}
	if strings.Contains(output, "Has Release") {
		t.Errorf("expected non-matching 'Has Release' (Release=v1.0.0) to be filtered out, got: %s", output)
	}
}

func TestRunListWithDeps_WithHasSubIssuesFilter(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID: "item-1",
			Issue: &api.Issue{
				Number: 1,
				Title:  "Parent Issue",
				Repository: api.Repository{
					Owner: "owner",
					Name:  "repo",
				},
			},
		},
		{
			ID: "item-2",
			Issue: &api.Issue{
				Number: 2,
				Title:  "Child Issue",
				Repository: api.Repository{
					Owner: "owner",
					Name:  "repo",
				},
			},
		},
	}
	mock.subIssueCounts = map[int]int{
		1: 2, // Issue 1 has 2 sub-issues
		2: 0, // Issue 2 has no sub-issues
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{hasSubIssues: true}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Parent Issue") {
		t.Errorf("expected matching 'Parent Issue' (2 sub-issues) in output, got: %s", output)
	}
	if strings.Contains(output, "Child Issue") {
		t.Errorf("expected non-matching 'Child Issue' (0 sub-issues) to be filtered out, got: %s", output)
	}
}

func TestRunListWithDeps_WithLimit(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Title: "Issue 1"}},
		{ID: "item-2", Issue: &api.Issue{Number: 2, Title: "Issue 2"}},
		{ID: "item-3", Issue: &api.Issue{Number: 3, Title: "Issue 3"}},
		{ID: "item-4", Issue: &api.Issue{Number: 4, Title: "Issue 4"}},
		{ID: "item-5", Issue: &api.Issue{Number: 5, Title: "Issue 5"}},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{limit: 3}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{"Issue 1", "Issue 2", "Issue 3"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q within limit in output, got: %s", want, output)
		}
	}
	for _, notWant := range []string{"Issue 4", "Issue 5"} {
		if strings.Contains(output, notWant) {
			t.Errorf("expected %q beyond limit to be excluded, got: %s", notWant, output)
		}
	}
}

func TestRunListWithDeps_OverFetchesWhenClientSideFilterActive(t *testing.T) {
	// #868 finding 2: a client-side-only filter (status) must not have the API
	// fetch capped at --limit, or `--status done --limit 5` fetches only 5 issues
	// of any status and can return 0. Over-fetch, apply --limit after filtering.
	mock := newMockListClient()
	mock.searchResults = []api.Issue{
		{Number: 1, Title: "Issue 1", State: "OPEN"},
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"test-org/test-repo"},
		Fields: map[string]config.Field{
			"status": {Field: "Status", Values: map[string]string{"done": "Done"}},
		},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{status: "done", limit: 5}
	if err := runListWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.searchLimits) == 0 {
		t.Fatal("expected SearchRepositoryIssues to be called")
	}
	if mock.searchLimits[0] != 0 {
		t.Errorf("expected unlimited fetch (0) when --status filter active, got limit %d", mock.searchLimits[0])
	}
}

func TestRunListWithDeps_UsesLimitWhenNoClientSideFilter(t *testing.T) {
	// Companion: without client-side filters, the fetch is still capped at --limit
	// (the optimization must not regress).
	mock := newMockListClient()
	mock.searchResults = []api.Issue{
		{Number: 1, Title: "Issue 1", State: "OPEN"},
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"test-org/test-repo"},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{limit: 5}
	if err := runListWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.searchLimits) == 0 {
		t.Fatal("expected SearchRepositoryIssues to be called")
	}
	if mock.searchLimits[0] != 5 {
		t.Errorf("expected fetch capped at --limit 5 with no client-side filter, got %d", mock.searchLimits[0])
	}
}

func TestRunListWithDeps_BranchCurrentNoRepo_Errors(t *testing.T) {
	// #871 finding 7: --branch current with no repository to resolve against must
	// error, not silently filter by the literal string "current".
	mock := newMockListClient()
	cfg := &config.Config{Project: config.Project{Owner: "test-org", Number: 1}}
	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{branch: "current"}
	if err := runListWithDeps(cmd, opts, cfg, mock); err == nil {
		t.Fatal("expected error resolving --branch current with no repository configured")
	}
}

func TestRunListWithDeps_BranchCurrentLookupError_Errors(t *testing.T) {
	// #871 finding 7: a branch-tracker lookup failure must surface, not degrade.
	mock := newMockListClient()
	mock.getOpenIssuesByLabelErr = errors.New("api unavailable")
	cfg := &config.Config{Project: config.Project{Owner: "test-org", Number: 1}, Repositories: []string{"o/r"}}
	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{branch: "current"}
	if err := runListWithDeps(cmd, opts, cfg, mock); err == nil {
		t.Fatal("expected error when --branch current lookup fails")
	}
}

func TestRunListWithDeps_BranchCurrentNoTracker_Errors(t *testing.T) {
	// #871 finding 7: no matching active branch tracker must error.
	mock := newMockListClient()
	mock.openIssuesByLabel = []api.Issue{}
	cfg := &config.Config{Project: config.Project{Owner: "test-org", Number: 1}, Repositories: []string{"o/r"}}
	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{branch: "current"}
	if err := runListWithDeps(cmd, opts, cfg, mock); err == nil {
		t.Fatal("expected error when no active branch tracker found for --branch current")
	}
}

func TestRunListWithDeps_BranchCurrentResolves(t *testing.T) {
	// Positive: --branch current resolves to the active tracker's branch name.
	mock := newMockListClient()
	mock.openIssuesByLabel = []api.Issue{{Number: 100, Title: "Branch: release/v2.0.0", State: "OPEN"}}
	cfg := &config.Config{Project: config.Project{Owner: "test-org", Number: 1}, Repositories: []string{"o/r"}}
	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{branch: "current"}
	if err := runListWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("unexpected error resolving --branch current to an active tracker: %v", err)
	}
}

func TestRunListWithDeps_WithRepoFlag(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{Number: 1, Title: "Issue 1"}},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	opts := &listOptions{repo: "owner/repo"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunListWithDeps_ReleaseWithParentheses(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID:    "item-1",
			Issue: &api.Issue{Number: 1, Title: "Release Issue"},
			FieldValues: []api.FieldValue{
				{Field: "Release", Value: "v1.0.0"},
			},
		},
	}
	mock.openIssuesByLabel = []api.Issue{
		{Number: 100, Title: "Release: v1.0.0 (Phoenix)"},
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"owner/repo"},
	}

	cmd := newListCommand()
	opts := &listOptions{branch: "current"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListCommand_Exists(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"list", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("list command should exist: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("list")) {
		t.Error("Expected help output to mention 'list'")
	}
}

func TestListCommand_HasStatusFlag(t *testing.T) {
	cmd := NewRootCommand()
	listCmd, _, err := cmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("list command not found: %v", err)
	}

	flag := listCmd.Flags().Lookup("status")
	if flag == nil {
		t.Fatal("Expected --status flag to exist")
	}
}

func TestListCommand_HasAssigneeFlag(t *testing.T) {
	cmd := NewRootCommand()
	listCmd, _, err := cmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("list command not found: %v", err)
	}

	flag := listCmd.Flags().Lookup("assignee")
	if flag == nil {
		t.Fatal("Expected --assignee flag to exist")
	}
	if flag.Shorthand != "a" {
		t.Errorf("Expected shorthand 'a', got '%s'", flag.Shorthand)
	}
}

func TestListCommand_HasLabelFlag(t *testing.T) {
	cmd := NewRootCommand()
	listCmd, _, err := cmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("list command not found: %v", err)
	}

	flag := listCmd.Flags().Lookup("label")
	if flag == nil {
		t.Fatal("Expected --label flag to exist")
	}
	if flag.Shorthand != "l" {
		t.Errorf("Expected shorthand 'l', got '%s'", flag.Shorthand)
	}
}

func TestListCommand_HasSearchFlag(t *testing.T) {
	cmd := NewRootCommand()
	listCmd, _, err := cmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("list command not found: %v", err)
	}

	flag := listCmd.Flags().Lookup("search")
	if flag == nil {
		t.Fatal("Expected --search flag to exist")
	}
	if flag.Shorthand != "q" {
		t.Errorf("Expected shorthand 'q', got '%s'", flag.Shorthand)
	}
}

func TestListCommand_HasLimitFlag(t *testing.T) {
	cmd := NewRootCommand()
	listCmd, _, err := cmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("list command not found: %v", err)
	}

	flag := listCmd.Flags().Lookup("limit")
	if flag == nil {
		t.Fatal("Expected --limit flag to exist")
	}
	if flag.Shorthand != "n" {
		t.Errorf("Expected shorthand 'n', got '%s'", flag.Shorthand)
	}
	if flag.Value.Type() != "int" {
		t.Errorf("Expected --limit to be int, got %s", flag.Value.Type())
	}
}

func TestListCommand_HasPriorityFlag(t *testing.T) {
	cmd := NewRootCommand()
	listCmd, _, err := cmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("list command not found: %v", err)
	}

	flag := listCmd.Flags().Lookup("priority")
	if flag == nil {
		t.Fatal("Expected --priority flag to exist")
	}
}

func TestListCommand_HasJSONFlag(t *testing.T) {
	cmd := NewRootCommand()
	listCmd, _, err := cmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("list command not found: %v", err)
	}

	flag := listCmd.Flags().Lookup("json")
	if flag == nil {
		t.Fatal("Expected --json flag to exist")
	}
}

// parseJSONFlagCommand parses args into cmd's flags and runs its PreRunE, the
// point where --json is validated before any config load or API call (#926).
// It never calls RunE, so these tests stay offline.
func parseJSONFlagCommand(t *testing.T, cmd *cobra.Command, args []string) error {
	t.Helper()
	if err := cmd.ParseFlags(args); err != nil {
		return err
	}
	if err := cmd.ValidateArgs(cmd.Flags().Args()); err != nil {
		return err
	}
	if cmd.PreRunE == nil {
		t.Fatalf("%s has no PreRunE; --json is not validated before the command runs", cmd.Name())
	}
	return cmd.PreRunE(cmd, cmd.Flags().Args())
}

func TestListCommand_JSONValueIsRequired(t *testing.T) {
	flag := newListCommand().Flags().Lookup("json")
	if flag.NoOptDefVal != "" {
		t.Errorf("--json NoOptDefVal = %q, want none: an optional value makes `--json status` drop the field (#926)", flag.NoOptDefVal)
	}
	if err := newListCommand().ParseFlags([]string{"--json"}); err == nil {
		t.Error("Expected bare --json to be rejected; field listing moved to --json-fields")
	}
}

func TestListCommand_JSONSpaceFormMatchesEqualsForm(t *testing.T) {
	spaced := newListCommand()
	if err := parseJSONFlagCommand(t, spaced, []string{"--json", "number,title"}); err != nil {
		t.Fatalf("--json number,title: unexpected error: %v", err)
	}
	equals := newListCommand()
	if err := parseJSONFlagCommand(t, equals, []string{"--json=number,title"}); err != nil {
		t.Fatalf("--json=number,title: unexpected error: %v", err)
	}
	a, _ := spaced.Flags().GetString("json")
	b, _ := equals.Flags().GetString("json")
	if a != b || a != "number,title" {
		t.Errorf("space form parsed %q, equals form parsed %q; want both \"number,title\"", a, b)
	}
}

func TestListCommand_JSONSpaceFormUnknownFieldExitsNonZero(t *testing.T) {
	err := parseJSONFlagCommand(t, newListCommand(), []string{"--json", "status", "--limit", "1"})
	if err == nil {
		t.Fatal("Expected `list --json status` to fail: status is not a list --json field")
	}
	if !strings.Contains(err.Error(), "--json=status") {
		t.Errorf("Expected error to name --json=status, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--json-fields") {
		t.Errorf("Expected error to point at --json-fields, got: %v", err)
	}
}

func TestListCommand_EmptyJSONValueRejected(t *testing.T) {
	err := parseJSONFlagCommand(t, newListCommand(), []string{"--json="})
	if err == nil {
		t.Fatal("Expected --json= with no fields to be rejected rather than silently printing the table")
	}
	if !strings.Contains(err.Error(), "--json-fields") {
		t.Errorf("Expected error to point at --json-fields, got: %v", err)
	}
}

func TestListCommand_RejectsStrayPositional(t *testing.T) {
	if err := parseJSONFlagCommand(t, newListCommand(), []string{"status"}); err == nil {
		t.Error("Expected a stray positional argument to be rejected")
	}
}

func TestListCommand_JSONFieldsListsFields(t *testing.T) {
	cmd := newListCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--json-fields"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list --json-fields: unexpected error: %v", err)
	}
	for _, field := range listCommandJSONFields {
		if !strings.Contains(buf.String(), field) {
			t.Errorf("Expected --json-fields output to include %q, got:\n%s", field, buf.String())
		}
	}
}

func TestListCommand_HasSubIssuesFlag(t *testing.T) {
	cmd := NewRootCommand()
	listCmd, _, err := cmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("list command not found: %v", err)
	}

	flag := listCmd.Flags().Lookup("has-sub-issues")
	if flag == nil {
		t.Fatal("Expected --has-sub-issues flag to exist")
	}

	// Verify it's a boolean flag
	if flag.Value.Type() != "bool" {
		t.Errorf("Expected --has-sub-issues to be bool, got %s", flag.Value.Type())
	}
}

func TestListCommand_HasSubIssuesHelpText(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"list", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("list help failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("has-sub-issues")) {
		t.Error("Expected help to mention --has-sub-issues flag")
	}
}

func TestListCommand_HasRepoFlag(t *testing.T) {
	cmd := NewRootCommand()
	listCmd, _, err := cmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("list command not found: %v", err)
	}

	flag := listCmd.Flags().Lookup("repo")
	if flag == nil {
		t.Fatal("Expected --repo flag to exist")
	}
	if flag.Shorthand != "R" {
		t.Errorf("Expected shorthand 'R', got '%s'", flag.Shorthand)
	}
	if flag.Value.Type() != "string" {
		t.Errorf("Expected --repo to be string, got %s", flag.Value.Type())
	}
}

func TestListCommand_RepoFlagInHelp(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"list", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("list help failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "--repo") {
		t.Error("Expected help to mention --repo flag")
	}
	if !strings.Contains(output, "owner/repo") {
		t.Error("Expected help to mention owner/repo format")
	}
}

// ============================================================================
// filterByFieldValue Tests
// ============================================================================

func TestFilterByFieldValue(t *testing.T) {
	tests := []struct {
		name      string
		items     []api.ProjectItem
		fieldName string
		value     string
		wantCount int
	}{
		{
			name: "exact match",
			items: []api.ProjectItem{
				{
					ID: "1",
					FieldValues: []api.FieldValue{
						{Field: "Status", Value: "In Progress"},
					},
				},
				{
					ID: "2",
					FieldValues: []api.FieldValue{
						{Field: "Status", Value: "Done"},
					},
				},
			},
			fieldName: "Status",
			value:     "In Progress",
			wantCount: 1,
		},
		{
			name: "case-insensitive field name",
			items: []api.ProjectItem{
				{
					ID: "1",
					FieldValues: []api.FieldValue{
						{Field: "Status", Value: "Backlog"},
					},
				},
			},
			fieldName: "status",
			value:     "Backlog",
			wantCount: 1,
		},
		{
			name: "case-insensitive value",
			items: []api.ProjectItem{
				{
					ID: "1",
					FieldValues: []api.FieldValue{
						{Field: "Status", Value: "In Progress"},
					},
				},
			},
			fieldName: "Status",
			value:     "in progress",
			wantCount: 1,
		},
		{
			name: "no match",
			items: []api.ProjectItem{
				{
					ID: "1",
					FieldValues: []api.FieldValue{
						{Field: "Status", Value: "Done"},
					},
				},
			},
			fieldName: "Status",
			value:     "In Progress",
			wantCount: 0,
		},
		{
			name:      "empty items",
			items:     []api.ProjectItem{},
			fieldName: "Status",
			value:     "Done",
			wantCount: 0,
		},
		{
			name: "multiple matches",
			items: []api.ProjectItem{
				{
					ID: "1",
					FieldValues: []api.FieldValue{
						{Field: "Priority", Value: "P1"},
					},
				},
				{
					ID: "2",
					FieldValues: []api.FieldValue{
						{Field: "Priority", Value: "P1"},
					},
				},
				{
					ID: "3",
					FieldValues: []api.FieldValue{
						{Field: "Priority", Value: "P2"},
					},
				},
			},
			fieldName: "Priority",
			value:     "P1",
			wantCount: 2,
		},
		{
			name: "item with multiple fields",
			items: []api.ProjectItem{
				{
					ID: "1",
					FieldValues: []api.FieldValue{
						{Field: "Status", Value: "Done"},
						{Field: "Priority", Value: "P1"},
					},
				},
			},
			fieldName: "Priority",
			value:     "P1",
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterByFieldValue(tt.items, tt.fieldName, tt.value)
			if len(result) != tt.wantCount {
				t.Errorf("filterByFieldValue() returned %d items, want %d", len(result), tt.wantCount)
			}
		})
	}
}

// ============================================================================
// getFieldValue Tests
// ============================================================================

func TestGetFieldValue(t *testing.T) {
	tests := []struct {
		name      string
		item      api.ProjectItem
		fieldName string
		want      string
	}{
		{
			name: "field exists",
			item: api.ProjectItem{
				FieldValues: []api.FieldValue{
					{Field: "Status", Value: "In Progress"},
				},
			},
			fieldName: "Status",
			want:      "In Progress",
		},
		{
			name: "field missing",
			item: api.ProjectItem{
				FieldValues: []api.FieldValue{
					{Field: "Status", Value: "Done"},
				},
			},
			fieldName: "Priority",
			want:      "",
		},
		{
			name: "case-insensitive lookup",
			item: api.ProjectItem{
				FieldValues: []api.FieldValue{
					{Field: "Priority", Value: "P0"},
				},
			},
			fieldName: "priority",
			want:      "P0",
		},
		{
			name: "multiple fields returns first match",
			item: api.ProjectItem{
				FieldValues: []api.FieldValue{
					{Field: "Status", Value: "Done"},
					{Field: "Priority", Value: "P1"},
					{Field: "Size", Value: "M"},
				},
			},
			fieldName: "Size",
			want:      "M",
		},
		{
			name:      "empty field values",
			item:      api.ProjectItem{},
			fieldName: "Status",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFieldValue(tt.item, tt.fieldName)
			if result != tt.want {
				t.Errorf("getFieldValue() = %q, want %q", result, tt.want)
			}
		})
	}
}

// ============================================================================
// outputTable Tests
// ============================================================================

// createTestCmd creates a cobra command with output set to a buffer
func createTestCmd(buf *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	return cmd
}

func TestOutputTable_EmptyItems(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createTestCmd(buf)

	err := outputTable(cmd, []api.ProjectItem{})
	if err != nil {
		t.Fatalf("outputTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No issues found") {
		t.Errorf("Expected 'No issues found', got: %s", output)
	}
}

func TestOutputTable_TitleTruncation(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createTestCmd(buf)

	longTitle := "This is a very long title that exceeds fifty characters and should be truncated"
	items := []api.ProjectItem{
		{
			ID: "1",
			Issue: &api.Issue{
				Number: 1,
				Title:  longTitle,
				State:  "OPEN",
			},
			FieldValues: []api.FieldValue{
				{Field: "Status", Value: "Done"},
			},
		},
	}

	err := outputTable(cmd, items)
	if err != nil {
		t.Fatalf("outputTable() error = %v", err)
	}

	output := buf.String()
	wantTruncated := longTitle[:47] + "..."
	if !strings.Contains(output, wantTruncated) {
		t.Errorf("expected truncated title %q in output, got: %s", wantTruncated, output)
	}
	if strings.Contains(output, longTitle) {
		t.Errorf("expected full title to be truncated, but found it in full: %s", output)
	}
}

func TestOutputTable_WithAssignees(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createTestCmd(buf)

	items := []api.ProjectItem{
		{
			ID: "1",
			Issue: &api.Issue{
				Number: 42,
				Title:  "Test Issue",
				State:  "OPEN",
				Assignees: []api.Actor{
					{Login: "user1"},
					{Login: "user2"},
				},
			},
			FieldValues: []api.FieldValue{
				{Field: "Status", Value: "In Progress"},
				{Field: "Priority", Value: "P1"},
			},
		},
	}

	err := outputTable(cmd, items)
	if err != nil {
		t.Fatalf("outputTable() error = %v", err)
	}

	// Note: outputTable uses tabwriter, which replaces tab separators with
	// padding spaces, so assert on individual column words and cell values
	// rather than a tab-joined header string.
	output := buf.String()
	for _, want := range []string{"NUMBER", "TITLE", "STATUS", "PRIORITY", "ASSIGNEES", "#42", "Test Issue", "In Progress", "P1", "user1, user2"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q, got: %s", want, output)
		}
	}
}

func TestOutputTable_NoAssignees(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createTestCmd(buf)

	items := []api.ProjectItem{
		{
			ID: "1",
			Issue: &api.Issue{
				Number: 1,
				Title:  "No Assignee Issue",
				State:  "OPEN",
			},
		},
	}

	err := outputTable(cmd, items)
	if err != nil {
		t.Fatalf("outputTable() error = %v", err)
	}
}

func TestOutputTable_NilIssue(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createTestCmd(buf)

	items := []api.ProjectItem{
		{ID: "1", Issue: nil},
		{
			ID: "2",
			Issue: &api.Issue{
				Number: 1,
				Title:  "Valid Issue",
				State:  "OPEN",
			},
		},
	}

	err := outputTable(cmd, items)
	if err != nil {
		t.Fatalf("outputTable() error = %v", err)
	}
}

// ============================================================================
// outputJSON Tests
// ============================================================================

func TestOutputJSON_EmptyItems(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createTestCmd(buf)

	// outputJSON writes to os.Stdout, not cmd buffer
	// But we can verify structure by checking for error
	err := outputJSON(cmd, []api.ProjectItem{})
	if err != nil {
		t.Fatalf("outputJSON() error = %v", err)
	}
}

func TestOutputJSON_WithItems(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createTestCmd(buf)

	items := []api.ProjectItem{
		{
			ID: "1",
			Issue: &api.Issue{
				Number: 42,
				Title:  "Test Issue",
				State:  "OPEN",
				URL:    "https://github.com/owner/repo/issues/42",
				Repository: api.Repository{
					Owner: "owner",
					Name:  "repo",
				},
				Assignees: []api.Actor{
					{Login: "user1"},
				},
			},
			FieldValues: []api.FieldValue{
				{Field: "Status", Value: "In Progress"},
				{Field: "Priority", Value: "P1"},
			},
		},
	}

	err := outputJSON(cmd, items)
	if err != nil {
		t.Fatalf("outputJSON() error = %v", err)
	}

	var got JSONOutput
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("expected valid JSON output, got parse error %v for: %s", err, buf.String())
	}
	if len(got.Items) != 1 {
		t.Fatalf("expected 1 item in JSON output, got %d: %s", len(got.Items), buf.String())
	}
	item := got.Items[0]
	if item.Number != 42 {
		t.Errorf("expected number 42, got %d", item.Number)
	}
	if item.Title != "Test Issue" {
		t.Errorf("expected title 'Test Issue', got %q", item.Title)
	}
	if item.State != "OPEN" {
		t.Errorf("expected state 'OPEN', got %q", item.State)
	}
	if item.URL != "https://github.com/owner/repo/issues/42" {
		t.Errorf("expected URL to be populated, got %q", item.URL)
	}
	if item.Repository != "owner/repo" {
		t.Errorf("expected repository 'owner/repo', got %q", item.Repository)
	}
	if len(item.Assignees) != 1 || item.Assignees[0] != "user1" {
		t.Errorf("expected assignees [user1], got %v", item.Assignees)
	}
	if item.FieldValues["Status"] != "In Progress" {
		t.Errorf("expected Status 'In Progress', got %q", item.FieldValues["Status"])
	}
	if item.FieldValues["Priority"] != "P1" {
		t.Errorf("expected Priority 'P1', got %q", item.FieldValues["Priority"])
	}
}

func TestOutputJSON_NilIssue(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := createTestCmd(buf)

	items := []api.ProjectItem{
		{ID: "1", Issue: nil},
	}

	err := outputJSON(cmd, items)
	if err != nil {
		t.Fatalf("outputJSON() error = %v", err)
	}
}

func TestJSONOutput_Structure(t *testing.T) {
	// Test that JSONOutput struct has expected fields
	output := JSONOutput{
		Items: []JSONItem{
			{
				Number:     1,
				Title:      "Test",
				State:      "OPEN",
				URL:        "https://example.com",
				Repository: "owner/repo",
				Assignees:  []string{"user1"},
				FieldValues: map[string]string{
					"Status": "Done",
				},
			},
		},
	}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("Failed to marshal JSONOutput: %v", err)
	}

	// Verify it can be unmarshaled back
	var parsed JSONOutput
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal JSONOutput: %v", err)
	}

	if len(parsed.Items) != 1 {
		t.Errorf("Expected 1 item, got %d", len(parsed.Items))
	}
	if parsed.Items[0].Number != 1 {
		t.Errorf("Expected number 1, got %d", parsed.Items[0].Number)
	}
	if parsed.Items[0].FieldValues["Status"] != "Done" {
		t.Errorf("Expected Status=Done, got %s", parsed.Items[0].FieldValues["Status"])
	}
}

func TestJSONItem_AllFields(t *testing.T) {
	item := JSONItem{
		Number:      42,
		Title:       "Test Issue",
		State:       "OPEN",
		URL:         "https://github.com/owner/repo/issues/42",
		Repository:  "owner/repo",
		Assignees:   []string{"user1", "user2"},
		FieldValues: map[string]string{"Status": "Done", "Priority": "P1"},
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("Failed to marshal JSONItem: %v", err)
	}

	jsonStr := string(data)
	expectedFields := []string{"number", "title", "state", "url", "repository", "assignees", "fieldValues"}
	for _, field := range expectedFields {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("Expected JSON to contain field %q", field)
		}
	}
}

// ============================================================================
// filterByAssignee Tests
// ============================================================================

func TestFilterByAssignee(t *testing.T) {
	tests := []struct {
		name      string
		items     []api.ProjectItem
		assignee  string
		wantCount int
	}{
		{
			name: "exact match",
			items: []api.ProjectItem{
				{
					ID: "1",
					Issue: &api.Issue{
						Number:    1,
						Title:     "Test 1",
						Assignees: []api.Actor{{Login: "user1"}},
					},
				},
				{
					ID: "2",
					Issue: &api.Issue{
						Number:    2,
						Title:     "Test 2",
						Assignees: []api.Actor{{Login: "user2"}},
					},
				},
			},
			assignee:  "user1",
			wantCount: 1,
		},
		{
			name: "case-insensitive",
			items: []api.ProjectItem{
				{
					ID: "1",
					Issue: &api.Issue{
						Number:    1,
						Title:     "Test 1",
						Assignees: []api.Actor{{Login: "User1"}},
					},
				},
			},
			assignee:  "user1",
			wantCount: 1,
		},
		{
			name: "multiple assignees on issue",
			items: []api.ProjectItem{
				{
					ID: "1",
					Issue: &api.Issue{
						Number:    1,
						Title:     "Test 1",
						Assignees: []api.Actor{{Login: "user1"}, {Login: "user2"}},
					},
				},
			},
			assignee:  "user2",
			wantCount: 1,
		},
		{
			name: "no match",
			items: []api.ProjectItem{
				{
					ID: "1",
					Issue: &api.Issue{
						Number:    1,
						Title:     "Test 1",
						Assignees: []api.Actor{{Login: "user1"}},
					},
				},
			},
			assignee:  "user3",
			wantCount: 0,
		},
		{
			name: "nil issue",
			items: []api.ProjectItem{
				{ID: "1", Issue: nil},
			},
			assignee:  "user1",
			wantCount: 0,
		},
		{
			name:      "empty items",
			items:     []api.ProjectItem{},
			assignee:  "user1",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterByAssignee(tt.items, tt.assignee)
			if len(result) != tt.wantCount {
				t.Errorf("filterByAssignee() returned %d items, want %d", len(result), tt.wantCount)
			}
		})
	}
}

// ============================================================================
// filterByLabel Tests
// ============================================================================

func TestFilterByLabel(t *testing.T) {
	tests := []struct {
		name      string
		items     []api.ProjectItem
		label     string
		wantCount int
	}{
		{
			name: "exact match",
			items: []api.ProjectItem{
				{
					ID: "1",
					Issue: &api.Issue{
						Number: 1,
						Title:  "Test 1",
						Labels: []api.Label{{Name: "bug"}},
					},
				},
				{
					ID: "2",
					Issue: &api.Issue{
						Number: 2,
						Title:  "Test 2",
						Labels: []api.Label{{Name: "enhancement"}},
					},
				},
			},
			label:     "bug",
			wantCount: 1,
		},
		{
			name: "case-insensitive",
			items: []api.ProjectItem{
				{
					ID: "1",
					Issue: &api.Issue{
						Number: 1,
						Title:  "Test 1",
						Labels: []api.Label{{Name: "Bug"}},
					},
				},
			},
			label:     "bug",
			wantCount: 1,
		},
		{
			name: "multiple labels on issue",
			items: []api.ProjectItem{
				{
					ID: "1",
					Issue: &api.Issue{
						Number: 1,
						Title:  "Test 1",
						Labels: []api.Label{{Name: "bug"}, {Name: "priority-high"}},
					},
				},
			},
			label:     "priority-high",
			wantCount: 1,
		},
		{
			name: "no match",
			items: []api.ProjectItem{
				{
					ID: "1",
					Issue: &api.Issue{
						Number: 1,
						Title:  "Test 1",
						Labels: []api.Label{{Name: "bug"}},
					},
				},
			},
			label:     "enhancement",
			wantCount: 0,
		},
		{
			name: "nil issue",
			items: []api.ProjectItem{
				{ID: "1", Issue: nil},
			},
			label:     "bug",
			wantCount: 0,
		},
		{
			name:      "empty items",
			items:     []api.ProjectItem{},
			label:     "bug",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterByLabel(tt.items, tt.label)
			if len(result) != tt.wantCount {
				t.Errorf("filterByLabel() returned %d items, want %d", len(result), tt.wantCount)
			}
		})
	}
}

// ============================================================================
// filterBySearch Tests
// ============================================================================

// ============================================================================
// filterByHasSubIssues Tests
// ============================================================================

// Note: filterByHasSubIssues requires a real API client to call GetSubIssues.
// These tests cover the function's behavior for edge cases and structural patterns.
// Full integration testing with actual GitHub API is done in integration tests.

func TestFilterByHasSubIssues_FunctionSignatureCheck(t *testing.T) {
	// Verify the function has the expected signature
	var _ func(*cobra.Command, listClient, []api.ProjectItem) ([]api.ProjectItem, bool) = filterByHasSubIssues
}

func TestFilterByHasSubIssues_EmptyItems(t *testing.T) {
	// Create a minimal client just for structure testing
	// Note: This won't make real API calls since items is empty
	client, err := api.NewClient()
	if err != nil {
		t.Skip("Could not create API client - likely not authenticated")
	}

	cmd := &cobra.Command{}

	// Empty items should return empty result without any API calls
	result, failed := filterByHasSubIssues(cmd, client, []api.ProjectItem{})

	if len(result) != 0 {
		t.Errorf("Expected empty result for empty input, got %d items", len(result))
	}
	if failed {
		t.Error("Expected no failure for empty input")
	}
}

func TestFilterByHasSubIssues_NilIssueItems(t *testing.T) {
	// Create a minimal client
	client, err := api.NewClient()
	if err != nil {
		t.Skip("Could not create API client - likely not authenticated")
	}

	cmd := &cobra.Command{}

	// Items with nil Issue should be skipped (no API calls made)
	items := []api.ProjectItem{
		{ID: "1", Issue: nil},
		{ID: "2", Issue: nil},
	}

	result, failed := filterByHasSubIssues(cmd, client, items)

	if len(result) != 0 {
		t.Errorf("Expected empty result for items with nil Issue, got %d items", len(result))
	}
	if failed {
		t.Error("Expected no failure for items with nil Issue")
	}
}

func TestFilterByHasSubIssues_APIFailureIncludesItemsAndWarns(t *testing.T) {
	// Create a mock client that returns an error for GetSubIssueCounts
	mock := newMockListClient()
	mock.getSubIssueCountsErr = errors.New("API rate limit exceeded")

	// Create a command with captured stderr
	cmd := &cobra.Command{}
	errBuf := new(bytes.Buffer)
	cmd.SetErr(errBuf)

	// Items with valid issues
	items := []api.ProjectItem{
		{
			ID: "1",
			Issue: &api.Issue{
				Number: 42,
				Repository: api.Repository{
					Owner: "owner",
					Name:  "repo",
				},
			},
		},
		{
			ID: "2",
			Issue: &api.Issue{
				Number: 43,
				Repository: api.Repository{
					Owner: "owner",
					Name:  "repo",
				},
			},
		},
	}

	result, failed := filterByHasSubIssues(cmd, mock, items)

	// Should have failure flag set
	if !failed {
		t.Error("Expected failure flag to be true when API fails")
	}

	// Should include items from failed repos (not silently exclude)
	if len(result) != 2 {
		t.Errorf("Expected 2 items to be included on API failure, got %d", len(result))
	}

	// Should have warning in stderr
	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "Warning: could not fetch sub-issue counts for owner/repo") {
		t.Errorf("Expected warning in stderr, got: %s", errOutput)
	}
	if !strings.Contains(errOutput, "API rate limit exceeded") {
		t.Errorf("Expected error message in warning, got: %s", errOutput)
	}
}

// ============================================================================
// filterByEmptyField Tests
// ============================================================================

func TestFilterByEmptyField(t *testing.T) {
	tests := []struct {
		name      string
		items     []api.ProjectItem
		fieldName string
		wantCount int
	}{
		{
			name: "field is empty",
			items: []api.ProjectItem{
				{
					ID: "1",
					FieldValues: []api.FieldValue{
						{Field: "Status", Value: "Done"},
					},
				},
				{
					ID: "2",
					FieldValues: []api.FieldValue{
						{Field: "Status", Value: "Done"},
						{Field: "Release", Value: ""},
					},
				},
			},
			fieldName: "Release",
			wantCount: 2,
		},
		{
			name: "field has value",
			items: []api.ProjectItem{
				{
					ID: "1",
					FieldValues: []api.FieldValue{
						{Field: "Release", Value: "v1.0.0"},
					},
				},
			},
			fieldName: "Release",
			wantCount: 0,
		},
		{
			name: "mixed - some with value some without",
			items: []api.ProjectItem{
				{
					ID: "1",
					FieldValues: []api.FieldValue{
						{Field: "Release", Value: "v1.0.0"},
					},
				},
				{
					ID: "2",
					FieldValues: []api.FieldValue{
						{Field: "Status", Value: "Done"},
					},
				},
				{
					ID: "3",
					FieldValues: []api.FieldValue{
						{Field: "Release", Value: ""},
					},
				},
			},
			fieldName: "Release",
			wantCount: 2,
		},
		{
			name: "case insensitive field name",
			items: []api.ProjectItem{
				{
					ID: "1",
					FieldValues: []api.FieldValue{
						{Field: "Release", Value: "v1.0.0"},
					},
				},
			},
			fieldName: "release",
			wantCount: 0,
		},
		{
			name:      "empty items",
			items:     []api.ProjectItem{},
			fieldName: "Release",
			wantCount: 0,
		},
		{
			name: "no field values at all",
			items: []api.ProjectItem{
				{ID: "1"},
				{ID: "2"},
			},
			fieldName: "Release",
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterByEmptyField(tt.items, tt.fieldName)
			if len(result) != tt.wantCount {
				t.Errorf("filterByEmptyField() returned %d items, want %d", len(result), tt.wantCount)
			}
		})
	}
}

func TestFilterBySearch(t *testing.T) {
	tests := []struct {
		name      string
		items     []api.ProjectItem
		search    string
		wantCount int
	}{
		{
			name: "match in title",
			items: []api.ProjectItem{
				{
					ID: "1",
					Issue: &api.Issue{
						Number: 1,
						Title:  "Fix login bug",
						Body:   "Some body text",
					},
				},
				{
					ID: "2",
					Issue: &api.Issue{
						Number: 2,
						Title:  "Add feature",
						Body:   "Feature description",
					},
				},
			},
			search:    "login",
			wantCount: 1,
		},
		{
			name: "match in body",
			items: []api.ProjectItem{
				{
					ID: "1",
					Issue: &api.Issue{
						Number: 1,
						Title:  "Some title",
						Body:   "Fix the authentication flow",
					},
				},
			},
			search:    "authentication",
			wantCount: 1,
		},
		{
			name: "case-insensitive",
			items: []api.ProjectItem{
				{
					ID: "1",
					Issue: &api.Issue{
						Number: 1,
						Title:  "Fix LOGIN Bug",
						Body:   "",
					},
				},
			},
			search:    "login",
			wantCount: 1,
		},
		{
			name: "partial match",
			items: []api.ProjectItem{
				{
					ID: "1",
					Issue: &api.Issue{
						Number: 1,
						Title:  "Authentication error",
						Body:   "",
					},
				},
			},
			search:    "auth",
			wantCount: 1,
		},
		{
			name: "no match",
			items: []api.ProjectItem{
				{
					ID: "1",
					Issue: &api.Issue{
						Number: 1,
						Title:  "Fix bug",
						Body:   "Bug description",
					},
				},
			},
			search:    "feature",
			wantCount: 0,
		},
		{
			name: "nil issue",
			items: []api.ProjectItem{
				{ID: "1", Issue: nil},
			},
			search:    "test",
			wantCount: 0,
		},
		{
			name:      "empty items",
			items:     []api.ProjectItem{},
			search:    "test",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterBySearch(tt.items, tt.search)
			if len(result) != tt.wantCount {
				t.Errorf("filterBySearch() returned %d items, want %d", len(result), tt.wantCount)
			}
		})
	}
}

func TestRunListWithDeps_WithStateFilterOpen(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID:    "item-1",
			Issue: &api.Issue{Number: 1, Title: "Open Issue", State: "OPEN"},
		},
		{
			ID:    "item-2",
			Issue: &api.Issue{Number: 2, Title: "Closed Issue", State: "CLOSED"},
		},
		{
			ID:    "item-3",
			Issue: &api.Issue{Number: 3, Title: "Another Open", State: "OPEN"},
		},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{state: "open"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Open Issue") || !strings.Contains(output, "Another Open") {
		t.Errorf("expected OPEN issues 'Open Issue' and 'Another Open' in output, got: %s", output)
	}
	if strings.Contains(output, "Closed Issue") {
		t.Errorf("expected CLOSED 'Closed Issue' to be filtered out, got: %s", output)
	}
}

func TestRunListWithDeps_WithStateFilterClosed(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID:    "item-1",
			Issue: &api.Issue{Number: 1, Title: "Open Issue", State: "OPEN"},
		},
		{
			ID:    "item-2",
			Issue: &api.Issue{Number: 2, Title: "Closed Issue", State: "CLOSED"},
		},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{state: "closed"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Closed Issue") {
		t.Errorf("expected CLOSED 'Closed Issue' in output, got: %s", output)
	}
	if strings.Contains(output, "Open Issue") {
		t.Errorf("expected OPEN 'Open Issue' to be filtered out, got: %s", output)
	}
}

func TestRunListWithDeps_WithStateFilterInvalid(t *testing.T) {
	mock := newMockListClient()

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newListCommand()
	opts := &listOptions{state: "invalid"}
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err == nil {
		t.Fatal("expected error for invalid state value")
	}

	expectedErr := `invalid --state value: expected 'open', 'closed', or 'all', got "invalid"`
	if err.Error() != expectedErr {
		t.Errorf("expected error %q, got %q", expectedErr, err.Error())
	}
}

func TestListCommand_HasStateFlag(t *testing.T) {
	cmd := NewRootCommand()
	listCmd, _, err := cmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("list command not found: %v", err)
	}

	flag := listCmd.Flags().Lookup("state")
	if flag == nil {
		t.Fatal("Expected --state flag to exist")
	}

	// Verify it's a string flag
	if flag.Value.Type() != "string" {
		t.Errorf("Expected --state to be string, got %s", flag.Value.Type())
	}
}

func TestFilterByState(t *testing.T) {
	tests := []struct {
		name      string
		items     []api.ProjectItem
		state     string
		wantCount int
	}{
		{
			name: "filter open issues",
			items: []api.ProjectItem{
				{ID: "1", Issue: &api.Issue{Number: 1, State: "OPEN"}},
				{ID: "2", Issue: &api.Issue{Number: 2, State: "CLOSED"}},
				{ID: "3", Issue: &api.Issue{Number: 3, State: "OPEN"}},
			},
			state:     "open",
			wantCount: 2,
		},
		{
			name: "filter closed issues",
			items: []api.ProjectItem{
				{ID: "1", Issue: &api.Issue{Number: 1, State: "OPEN"}},
				{ID: "2", Issue: &api.Issue{Number: 2, State: "CLOSED"}},
				{ID: "3", Issue: &api.Issue{Number: 3, State: "CLOSED"}},
			},
			state:     "closed",
			wantCount: 2,
		},
		{
			name: "case-insensitive - uppercase input",
			items: []api.ProjectItem{
				{ID: "1", Issue: &api.Issue{Number: 1, State: "OPEN"}},
				{ID: "2", Issue: &api.Issue{Number: 2, State: "CLOSED"}},
			},
			state:     "OPEN",
			wantCount: 1,
		},
		{
			name: "case-insensitive - mixed case input",
			items: []api.ProjectItem{
				{ID: "1", Issue: &api.Issue{Number: 1, State: "CLOSED"}},
				{ID: "2", Issue: &api.Issue{Number: 2, State: "CLOSED"}},
			},
			state:     "Closed",
			wantCount: 2,
		},
		{
			name: "skip nil issues",
			items: []api.ProjectItem{
				{ID: "1", Issue: &api.Issue{Number: 1, State: "OPEN"}},
				{ID: "2", Issue: nil},
			},
			state:     "open",
			wantCount: 1,
		},
		{
			name:      "empty items",
			items:     []api.ProjectItem{},
			state:     "open",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterByState(tt.items, tt.state)
			if len(result) != tt.wantCount {
				t.Errorf("filterByState() returned %d items, want %d", len(result), tt.wantCount)
			}
		})
	}
}

// ============================================================================
// Open-First Query Strategy Tests
// ============================================================================

func TestRunListWithDeps_WithStateAll_UsesDualSearchCalls(t *testing.T) {
	mock := newMockListClient()
	mock.searchResultsByState = map[string][]api.Issue{
		"open": {
			{
				ID:         "issue-1",
				Number:     1,
				Title:      "Open Issue",
				State:      "OPEN",
				Repository: api.Repository{Owner: "test-org", Name: "repo"},
			},
		},
		"closed": {
			{
				ID:         "issue-2",
				Number:     2,
				Title:      "Closed Issue",
				State:      "CLOSED",
				Repository: api.Repository{Owner: "test-org", Name: "repo"},
			},
		},
	}
	mock.projectFieldsForIssue = map[string][]api.FieldValue{
		"issue-1": {{Field: "Status", Value: "In Progress"}, {Field: "Priority", Value: "P1"}},
		"issue-2": {{Field: "Status", Value: "Done"}, {Field: "Priority", Value: "P2"}},
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"test-org/repo"},
	}
	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{state: "all"}

	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should have made two search calls (open + closed), not GetProjectItems
	if mock.getProjectItemsCalled {
		t.Error("Expected dual SearchRepositoryIssues calls, but GetProjectItems was called")
	}
	if len(mock.searchCalls) != 2 {
		t.Fatalf("Expected 2 SearchRepositoryIssues calls, got %d", len(mock.searchCalls))
	}

	// Verify the two calls were for open and closed states
	states := map[string]bool{}
	for _, call := range mock.searchCalls {
		states[call.State] = true
	}
	if !states["open"] || !states["closed"] {
		t.Errorf("Expected calls for 'open' and 'closed' states, got: %v", mock.searchCalls)
	}
}

func TestRunListWithDeps_WithStateAll_NoRepoFilter_FallsBackToGetProjectItems(t *testing.T) {
	mock := newMockListClient()
	mock.projectItems = []api.ProjectItem{
		{
			ID:    "item-1",
			Issue: &api.Issue{Number: 1, Title: "Open Issue", State: "OPEN"},
		},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		// No repositories configured
	}
	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &listOptions{state: "all"}

	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Without repo filter, should fall back to GetProjectItems
	if !mock.getProjectItemsCalled {
		t.Error("Expected GetProjectItems to be called when no repo filter is set")
	}
	if len(mock.searchCalls) != 0 {
		t.Errorf("Expected no SearchRepositoryIssues calls, got %d", len(mock.searchCalls))
	}
}

func TestRunListWithDeps_WithStateAll_DualSearch_MergesResults(t *testing.T) {
	mock := newMockListClient()
	mock.searchResultsByState = map[string][]api.Issue{
		"open": {
			{ID: "issue-1", Number: 1, Title: "Open Issue", State: "OPEN", Repository: api.Repository{Owner: "test-org", Name: "repo"}},
			{ID: "issue-3", Number: 3, Title: "Another Open", State: "OPEN", Repository: api.Repository{Owner: "test-org", Name: "repo"}},
		},
		"closed": {
			{ID: "issue-2", Number: 2, Title: "Closed Issue", State: "CLOSED", Repository: api.Repository{Owner: "test-org", Name: "repo"}},
		},
	}
	mock.projectFieldsForIssue = map[string][]api.FieldValue{
		"issue-1": {{Field: "Status", Value: "In Progress"}},
		"issue-2": {{Field: "Status", Value: "Done"}},
		"issue-3": {{Field: "Status", Value: "Backlog"}},
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"test-org/repo"},
		Fields: map[string]config.Field{
			"status": {
				Field:  "Status",
				Values: map[string]string{"in_progress": "In Progress"},
			},
		},
	}
	cmd := newListCommand()

	// Use JSON output to capture results via cmd.OutOrStdout()
	opts := &listOptions{state: "all", status: "in_progress", jsonFields: "number,title,state"}

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify dual search was used
	if mock.getProjectItemsCalled {
		t.Error("Expected dual SearchRepositoryIssues calls, but GetProjectItems was called")
	}
	if len(mock.searchCalls) != 2 {
		t.Fatalf("Expected 2 search calls, got %d", len(mock.searchCalls))
	}

	// Verify merged results are filtered correctly (only in_progress issue)
	output := buf.String()
	if !strings.Contains(output, "Open Issue") {
		t.Errorf("Expected 'Open Issue' (in_progress) in output, got: %s", output)
	}
	if strings.Contains(output, "Closed Issue") {
		t.Errorf("Should not contain 'Closed Issue' (status=Done), got: %s", output)
	}
	if strings.Contains(output, "Another Open") {
		t.Errorf("Should not contain 'Another Open' (status=Backlog), got: %s", output)
	}
}

func TestRunListWithDeps_SearchApiPath_UsesSearchRepositoryIssues(t *testing.T) {
	mock := newMockListClient()
	mock.searchResults = []api.Issue{
		{
			ID:         "issue-1",
			Number:     1,
			Title:      "Open Issue from Search",
			State:      "OPEN",
			Repository: api.Repository{Owner: "test-org", Name: "repo"},
		},
	}
	mock.projectFieldsForIssue = map[string][]api.FieldValue{
		"issue-1": {{Field: "Status", Value: "In Progress"}},
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"test-org/repo"},
	}
	cmd := newListCommand()

	opts := &listOptions{}

	// Should use SearchRepositoryIssues path (default state is open, repo is configured)
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// The fact that it succeeded means it used the search API path
	// because projectItems is empty but searchResults has data
}

func TestRunListWithDeps_AtMeResolvedBeforeSearchQualifier(t *testing.T) {
	// #895 AC10: the search path already handled @me, because GitHub resolves the
	// "assignee:@me" qualifier server-side. Resolving first must not regress that
	// — the qualifier now carries the login, which selects the same issues.
	mock := newMockListClient()
	mock.viewerLogin = "rubrical-worker"
	mock.searchResults = []api.Issue{}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"test-org/repo"},
	}
	cmd := newListCommand()
	opts := &listOptions{assignee: "@me"}

	if err := runListWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.searchCalls) == 0 {
		t.Fatal("expected the search path to be used when a repository filter is configured")
	}
	if got := mock.searchCalls[0].Assignee; got != "rubrical-worker" {
		t.Errorf("expected the resolved login in the search qualifier, got %q", got)
	}
}

func TestRunListWithDeps_AtMeResolvedForClientSideFilter(t *testing.T) {
	// The counterpart: with no repository filter there is no search API, and the
	// fallback compares logins literally. This is the path where @me silently
	// matched nothing before #895.
	mock := newMockListClient()
	mock.viewerLogin = "rubrical-worker"
	mock.projectItems = []api.ProjectItem{
		{ID: "item-1", Issue: &api.Issue{ID: "i1", Number: 1, Title: "Mine",
			Assignees: []api.Actor{{Login: "rubrical-worker"}}}},
		{ID: "item-2", Issue: &api.Issue{ID: "i2", Number: 2, Title: "Theirs",
			Assignees: []api.Actor{{Login: "octocat"}}}},
	}

	cfg := &config.Config{Project: config.Project{Owner: "test-org", Number: 1}}
	cmd := newListCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	opts := &listOptions{assignee: "@me"}

	if err := runListWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Mine") {
		t.Errorf("expected the issue assigned to the authenticated user, got: %q", out)
	}
	if strings.Contains(out, "Theirs") {
		t.Errorf("expected another user's issue to be filtered out, got: %q", out)
	}
}

func TestRunListWithDeps_SearchApiPath_WithClosedState(t *testing.T) {
	mock := newMockListClient()
	mock.searchResults = []api.Issue{
		{
			ID:         "issue-1",
			Number:     1,
			Title:      "Closed Issue",
			State:      "CLOSED",
			Repository: api.Repository{Owner: "test-org", Name: "repo"},
		},
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"test-org/repo"},
	}
	cmd := newListCommand()

	opts := &listOptions{state: "closed"}

	// Should use SearchRepositoryIssues path for --state closed with repo configured
	err := runListWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// The fact that it succeeded means it used the search API path
}

func TestRunListWithDeps_SearchApiPath_Error(t *testing.T) {
	mock := newMockListClient()
	mock.searchRepositoryIssuesErr = errors.New("search failed")

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"test-org/repo"},
	}
	cmd := newListCommand()
	opts := &listOptions{}

	err := runListWithDeps(cmd, opts, cfg, mock)
	if err == nil {
		t.Fatal("Expected error when search fails")
	}
	if !strings.Contains(err.Error(), "failed to search issues") {
		t.Errorf("Expected 'failed to search issues' error, got: %v", err)
	}
}

func TestRunListWithDeps_EnrichError(t *testing.T) {
	mock := newMockListClient()
	mock.searchResults = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Issue", State: "OPEN"},
	}
	mock.getProjectFieldsForIssueErr = errors.New("enrich failed")

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"test-org/repo"},
	}
	cmd := newListCommand()
	opts := &listOptions{}

	err := runListWithDeps(cmd, opts, cfg, mock)
	if err == nil {
		t.Fatal("Expected error when enrichment fails")
	}
	if !strings.Contains(err.Error(), "failed to enrich issues") {
		t.Errorf("Expected 'failed to enrich issues' error, got: %v", err)
	}
}

// ============================================================================
// enrichIssuesWithProjectFields Tests
// ============================================================================

func TestEnrichIssuesWithProjectFields_EmptyInput(t *testing.T) {
	mock := newMockListClient()
	items, err := enrichIssuesWithProjectFields(mock, "proj-id", []api.Issue{})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("Expected nil or empty items, got %d", len(items))
	}
}

func TestEnrichIssuesWithProjectFields_WithFieldValues(t *testing.T) {
	mock := newMockListClient()
	mock.projectFieldsForIssue = map[string][]api.FieldValue{
		"issue-1": {
			{Field: "Status", Value: "Done"},
			{Field: "Priority", Value: "P1"},
		},
	}

	issues := []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Test Issue"},
	}

	items, err := enrichIssuesWithProjectFields(mock, "proj-id", issues)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
	if len(items[0].FieldValues) != 2 {
		t.Errorf("Expected 2 field values, got %d", len(items[0].FieldValues))
	}
}

func TestEnrichIssuesWithProjectFields_NoFieldValues(t *testing.T) {
	mock := newMockListClient()
	// Empty projectFieldsForIssue - no field values returned

	issues := []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Test Issue"},
	}

	items, err := enrichIssuesWithProjectFields(mock, "proj-id", issues)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
	if len(items[0].FieldValues) != 0 {
		t.Errorf("Expected 0 field values, got %d", len(items[0].FieldValues))
	}
}
