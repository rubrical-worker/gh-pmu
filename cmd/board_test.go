package cmd

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
)

// mockBoardClient implements boardClient for testing
type mockBoardClient struct {
	project       *api.Project
	boardItems    []api.BoardItem
	issues        []api.Issue
	issuesByState map[string][]api.Issue // state -> results (for dual-call tests)
	fieldsByID    map[string][]api.FieldValue

	// Call tracking
	searchCalls                 []api.SearchFilters
	getBoardItemsForBoardCalled bool

	// Error injection
	getProjectErr      error
	getBoardItemsErr   error
	searchIssuesErr    error
	getFieldsForIssues error
}

func newMockBoardClient() *mockBoardClient {
	return &mockBoardClient{
		project: &api.Project{
			ID:    "proj-1",
			Title: "Test Project",
			URL:   "https://github.com/orgs/test/projects/1",
		},
		boardItems: []api.BoardItem{},
	}
}

func (m *mockBoardClient) GetProject(owner string, number int) (*api.Project, error) {
	if m.getProjectErr != nil {
		return nil, m.getProjectErr
	}
	return m.project, nil
}

func (m *mockBoardClient) GetProjectItemsForBoard(projectID string, filter *api.BoardItemsFilter) ([]api.BoardItem, error) {
	m.getBoardItemsForBoardCalled = true
	if m.getBoardItemsErr != nil {
		return nil, m.getBoardItemsErr
	}
	return m.boardItems, nil
}

func (m *mockBoardClient) SearchRepositoryIssues(owner, repo string, filters api.SearchFilters, limit int) ([]api.Issue, error) {
	m.searchCalls = append(m.searchCalls, filters)
	if m.searchIssuesErr != nil {
		return nil, m.searchIssuesErr
	}
	if m.issuesByState != nil {
		if results, ok := m.issuesByState[filters.State]; ok {
			return results, nil
		}
	}
	return m.issues, nil
}

func (m *mockBoardClient) GetProjectFieldsForIssues(projectID string, issueIDs []string) (map[string][]api.FieldValue, error) {
	if m.getFieldsForIssues != nil {
		return nil, m.getFieldsForIssues
	}
	if m.fieldsByID == nil {
		return make(map[string][]api.FieldValue), nil
	}
	return m.fieldsByID, nil
}

// ============================================================================
// runBoardWithDeps Tests
// ============================================================================

func TestRunBoardWithDeps_Success(t *testing.T) {
	mock := newMockBoardClient()
	mock.boardItems = []api.BoardItem{
		{Number: 1, Title: "Test Issue 1", Status: "Backlog"},
		{Number: 2, Title: "Test Issue 2", Status: "In Progress"},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		Fields: map[string]config.Field{
			"status": {
				Field: "Status",
				Values: map[string]string{
					"backlog":     "Backlog",
					"in_progress": "In Progress",
				},
			},
		},
	}

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &boardOptions{}
	err := runBoardWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Backlog") {
		t.Error("expected Backlog in output")
	}
}

func TestRunBoardWithDeps_GetProjectError(t *testing.T) {
	mock := newMockBoardClient()
	mock.getProjectErr = errors.New("project not found")

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newBoardCommand()
	opts := &boardOptions{}
	err := runBoardWithDeps(cmd, opts, cfg, mock)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get project") {
		t.Errorf("expected 'failed to get project' error, got: %v", err)
	}
}

func TestRunBoardWithDeps_GetProjectItemsError(t *testing.T) {
	mock := newMockBoardClient()
	mock.getBoardItemsErr = errors.New("API error")

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newBoardCommand()
	opts := &boardOptions{}
	err := runBoardWithDeps(cmd, opts, cfg, mock)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get project items") {
		t.Errorf("expected 'failed to get project items' error, got: %v", err)
	}
}

func TestRunBoardWithDeps_WithStatusFilter(t *testing.T) {
	mock := newMockBoardClient()
	mock.boardItems = []api.BoardItem{
		{Number: 1, Title: "Backlog Issue", Status: "Backlog"},
		{Number: 2, Title: "In Progress Issue", Status: "In Progress"},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		Fields: map[string]config.Field{
			"status": {
				Field: "Status",
				Values: map[string]string{
					"backlog":     "Backlog",
					"in_progress": "In Progress",
				},
			},
		},
	}

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &boardOptions{status: "backlog"}
	err := runBoardWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Should only show Backlog column
	if !strings.Contains(output, "Backlog") {
		t.Error("expected Backlog in output")
	}
}

func TestRunBoardWithDeps_InvalidStateErrors(t *testing.T) {
	// #868 finding 1b: an invalid --state must fail loudly rather than silently
	// producing an empty board.
	mock := newMockBoardClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &boardOptions{state: "bogus"}
	err := runBoardWithDeps(cmd, opts, cfg, mock)
	if err == nil {
		t.Fatal("expected error for invalid --state, got nil")
	}
	if !strings.Contains(err.Error(), "state") {
		t.Errorf("expected error to mention 'state', got: %v", err)
	}
}

func TestRunBoardWithDeps_SurfacesUnrecognizedStatus(t *testing.T) {
	// #868 finding 1a: items whose status is not a configured column must not be
	// silently dropped from output.
	mock := newMockBoardClient()
	mock.boardItems = []api.BoardItem{
		{Number: 1, Title: "Backlog Issue", Status: "Backlog"},
		{Number: 3, Title: "Weird Issue", Status: "Weird Status"},
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

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &boardOptions{noBorder: true}
	err := runBoardWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Weird Status") {
		t.Errorf("expected unrecognized status 'Weird Status' to be surfaced, got: %s", output)
	}
	if !strings.Contains(output, "#3") {
		t.Errorf("expected unrecognized-status issue #3 to appear, got: %s", output)
	}
}

func TestRunBoardWithDeps_WithPriorityFilter(t *testing.T) {
	mock := newMockBoardClient()
	mock.boardItems = []api.BoardItem{
		{Number: 1, Title: "P1 Issue", Status: "Backlog", Priority: "P1"},
		{Number: 2, Title: "P2 Issue", Status: "Backlog", Priority: "P2"},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		Fields: map[string]config.Field{
			"status": {
				Field:  "Status",
				Values: map[string]string{"backlog": "Backlog"},
			},
			"priority": {
				Field:  "Priority",
				Values: map[string]string{"p1": "P1", "p2": "P2"},
			},
		},
	}

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &boardOptions{priority: "p1"}
	err := runBoardWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "P1 Issue") {
		t.Error("expected P1 Issue in output")
	}
	if strings.Contains(output, "P2 Issue") {
		t.Error("P2 Issue should be filtered out")
	}
}

func TestRunBoardWithDeps_JSONOutput(t *testing.T) {
	mock := newMockBoardClient()
	mock.boardItems = []api.BoardItem{
		{Number: 42, Title: "JSON Test Issue", Status: "Backlog"},
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

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &boardOptions{json: true}
	err := runBoardWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"number": 42`) {
		t.Error("expected issue number in JSON output")
	}
	if !strings.Contains(output, `"status": "Backlog"`) {
		t.Error("expected status in JSON output")
	}
}

func TestRunBoardWithDeps_EmptyProject(t *testing.T) {
	mock := newMockBoardClient()
	mock.boardItems = []api.BoardItem{}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		Fields: map[string]config.Field{
			"status": {
				Field:  "Status",
				Values: map[string]string{"backlog": "Backlog"},
			},
		},
	}

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &boardOptions{}
	err := runBoardWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Should show empty columns
	if !strings.Contains(output, "(empty)") && !strings.Contains(output, "Backlog (0)") {
		t.Error("expected empty indicator in output")
	}
}

// An item whose field values were truncated may show an empty Status or
// Priority; the board must say so rather than present it as unset (#934).
func TestRunBoardWithDeps_WarnsOnTruncatedFieldValues(t *testing.T) {
	mock := newMockBoardClient()
	mock.boardItems = []api.BoardItem{
		{Number: 7, Title: "Many fields", Status: "Backlog", FieldValuesTruncated: true},
		{Number: 8, Title: "Few fields", Status: "Backlog"},
	}

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		Fields: map[string]config.Field{
			"status": {Field: "Status", Values: map[string]string{"backlog": "Backlog"}},
		},
	}

	cmd := newBoardCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := runBoardWithDeps(cmd, &boardOptions{}, cfg, mock); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	warning := errOut.String()
	if !strings.Contains(warning, "#7") || !strings.Contains(strings.ToLower(warning), "truncated") {
		t.Errorf("expected a truncation warning naming #7 on stderr, got: %q", warning)
	}
	if strings.Contains(warning, "#8") {
		t.Errorf("warning should not name #8, got: %q", warning)
	}
	if strings.Contains(out.String(), "truncated") {
		t.Errorf("warning belongs on stderr, not stdout")
	}
}

func TestNewBoardCommand(t *testing.T) {
	cmd := newBoardCommand()

	// Verify command basics
	if cmd.Use != "board" {
		t.Errorf("unexpected Use: %s", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("Short description should not be empty")
	}

	// Verify flags exist
	flags := []struct {
		name      string
		shorthand string
	}{
		{"status", "s"},
		{"priority", "p"},
		{"state", ""},
		{"limit", "n"},
		{"no-border", ""},
		{"json", ""},
		{"repo", "R"},
	}

	for _, f := range flags {
		flag := cmd.Flags().Lookup(f.name)
		if flag == nil {
			t.Errorf("flag --%s not found", f.name)
			continue
		}
		if f.shorthand != "" && flag.Shorthand != f.shorthand {
			t.Errorf("flag --%s shorthand = %q, want %q", f.name, flag.Shorthand, f.shorthand)
		}
	}
}

func TestGetStatusColumns_FromConfig(t *testing.T) {
	cfg := &config.Config{
		Fields: map[string]config.Field{
			"status": {
				Field: "Status",
				Values: map[string]string{
					"backlog":     "Backlog",
					"in_progress": "In progress",
					"done":        "Done",
				},
			},
		},
	}

	columns := getStatusColumns(cfg)

	// Should have 3 columns
	if len(columns) != 3 {
		t.Errorf("expected 3 columns, got %d", len(columns))
	}

	// Verify order follows preferredOrder
	expectedOrder := []string{"backlog", "in_progress", "done"}
	for i, expected := range expectedOrder {
		if i >= len(columns) {
			break
		}
		if columns[i].alias != expected {
			t.Errorf("column %d: expected alias %q, got %q", i, expected, columns[i].alias)
		}
	}
}

func TestGetStatusColumns_Fallback(t *testing.T) {
	cfg := &config.Config{
		Fields: map[string]config.Field{},
	}

	columns := getStatusColumns(cfg)

	// Should have default fallback columns
	if len(columns) == 0 {
		t.Error("expected fallback columns, got none")
	}

	// Check that backlog is first
	if columns[0].alias != "backlog" {
		t.Errorf("expected first column to be 'backlog', got %q", columns[0].alias)
	}
}

func TestGroupBoardItemsByStatus(t *testing.T) {
	columns := []statusColumn{
		{alias: "backlog", value: "Backlog"},
		{alias: "done", value: "Done"},
	}

	items := []api.BoardItem{
		{Number: 1, Title: "Issue 1", Status: "Backlog"},
		{Number: 2, Title: "Issue 2", Status: "Done"},
		{Number: 3, Title: "Issue 3", Status: "Backlog"},
	}

	grouped := groupBoardItemsByStatus(items, columns)

	if len(grouped["Backlog"]) != 2 {
		t.Errorf("expected 2 items in Backlog, got %d", len(grouped["Backlog"]))
	}

	if len(grouped["Done"]) != 1 {
		t.Errorf("expected 1 item in Done, got %d", len(grouped["Done"]))
	}
}

func TestGroupBoardItemsByStatus_EmptyStatus(t *testing.T) {
	columns := []statusColumn{
		{alias: "backlog", value: "Backlog"},
	}

	items := []api.BoardItem{
		{Number: 1, Title: "Issue 1", Status: ""}, // empty status goes to "(none)"
		{Number: 2, Title: "Issue 2", Status: "Backlog"},
	}

	grouped := groupBoardItemsByStatus(items, columns)

	if len(grouped["Backlog"]) != 1 {
		t.Errorf("expected 1 item in Backlog, got %d", len(grouped["Backlog"]))
	}

	if len(grouped["(none)"]) != 1 {
		t.Errorf("expected 1 item in (none), got %d", len(grouped["(none)"]))
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long string", 10, "this is..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
		{"", 5, ""},
	}

	for _, tt := range tests {
		result := truncateString(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

func TestTruncateString_Runes(t *testing.T) {
	// #868 finding 1c: truncation must count runes, not bytes, so multi-byte
	// UTF-8 titles are not sliced mid-rune.
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{"multibyte under limit unchanged", "café", 10, "café"},
		{"multibyte exactly at limit", "café", 4, "café"},
		{"multibyte truncated by rune", "café latte here", 8, "café ..."},
		{"cjk under limit unchanged", "日本語", 5, "日本語"},
		{"cjk truncated by rune", "日本語のテスト", 4, "日..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateString(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
			// Result must never exceed maxLen runes.
			if got := len([]rune(result)); got > tt.maxLen {
				t.Errorf("truncateString(%q, %d) produced %d runes, exceeds maxLen", tt.input, tt.maxLen, got)
			}
		})
	}
}

func TestOutputBoardSimple(t *testing.T) {
	columns := []statusColumn{
		{alias: "backlog", value: "Backlog"},
		{alias: "done", value: "Done"},
	}

	grouped := map[string][]api.BoardItem{
		"Backlog": {
			{Number: 1, Title: "Test Issue", Status: "Backlog"},
		},
		"Done": {},
	}

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := outputBoardSimple(cmd, grouped, columns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Check that headers are present
	if !strings.Contains(output, "## Backlog (1)") {
		t.Error("expected Backlog header with count")
	}

	if !strings.Contains(output, "## Done (0)") {
		t.Error("expected Done header with count")
	}

	// Check that issue is listed
	if !strings.Contains(output, "#1 Test Issue") {
		t.Error("expected issue #1 in output")
	}

	// Check that empty column shows (empty)
	if !strings.Contains(output, "(empty)") {
		t.Error("expected (empty) for Done column")
	}
}

func TestOutputBoardJSON(t *testing.T) {
	columns := []statusColumn{
		{alias: "backlog", value: "Backlog"},
	}

	grouped := map[string][]api.BoardItem{
		"Backlog": {
			{Number: 42, Title: "JSON Test", Status: "Backlog", Priority: "P1"},
		},
	}

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := outputBoardJSON(cmd, grouped, columns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Check JSON structure
	if !strings.Contains(output, `"status": "Backlog"`) {
		t.Error("expected status in JSON")
	}

	if !strings.Contains(output, `"number": 42`) {
		t.Error("expected issue number in JSON")
	}

	if !strings.Contains(output, `"title": "JSON Test"`) {
		t.Error("expected issue title in JSON")
	}

	if !strings.Contains(output, `"priority": "P1"`) {
		t.Error("expected priority in JSON")
	}
}

func TestOutputBoardBox(t *testing.T) {
	columns := []statusColumn{
		{alias: "backlog", value: "Backlog"},
	}

	grouped := map[string][]api.BoardItem{
		"Backlog": {
			{Number: 1, Title: "Box Test", Status: "Backlog"},
		},
	}

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := outputBoardBox(cmd, grouped, columns, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Check box characters are present
	if !strings.Contains(output, "┌") {
		t.Error("expected top-left corner")
	}

	if !strings.Contains(output, "┐") {
		t.Error("expected top-right corner")
	}

	if !strings.Contains(output, "└") {
		t.Error("expected bottom-left corner")
	}

	if !strings.Contains(output, "┘") {
		t.Error("expected bottom-right corner")
	}

	// Check content
	if !strings.Contains(output, "Backlog") {
		t.Error("expected Backlog header")
	}

	if !strings.Contains(output, "#1") {
		t.Error("expected issue #1")
	}
}

// ============================================================================
// Search API Optimization Tests
// ============================================================================

func TestRunBoardWithDeps_UsesSearchAPIWhenRepoConfigured(t *testing.T) {
	mock := newMockBoardClient()
	// Set up issues for Search API path
	mock.issues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Search Issue", State: "OPEN"},
	}
	mock.fieldsByID = map[string][]api.FieldValue{
		"issue-1": {
			{Field: "Status", Value: "Backlog"},
			{Field: "Priority", Value: "P1"},
		},
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"test-org/test-repo"},
		Fields: map[string]config.Field{
			"status": {
				Field:  "Status",
				Values: map[string]string{"backlog": "Backlog"},
			},
		},
	}

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	// state defaults to "open", so Search API should be used
	opts := &boardOptions{state: "open"}
	err := runBoardWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Search Issue") {
		t.Error("expected Search Issue in output from Search API path")
	}
}

func TestRunBoardWithDeps_StateAll_UsesDualSearchCalls(t *testing.T) {
	mock := newMockBoardClient()
	mock.issuesByState = map[string][]api.Issue{
		"open": {
			{ID: "issue-1", Number: 1, Title: "Open Issue", State: "OPEN", Repository: api.Repository{Owner: "test-org", Name: "test-repo"}},
		},
		"closed": {
			{ID: "issue-2", Number: 2, Title: "Closed Issue", State: "CLOSED", Repository: api.Repository{Owner: "test-org", Name: "test-repo"}},
		},
	}
	mock.fieldsByID = map[string][]api.FieldValue{
		"issue-1": {{Field: "Status", Value: "Backlog"}, {Field: "Priority", Value: "P1"}},
		"issue-2": {{Field: "Status", Value: "Done"}, {Field: "Priority", Value: "P2"}},
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"test-org/test-repo"},
		Fields: map[string]config.Field{
			"status": {
				Field:  "Status",
				Values: map[string]string{"backlog": "Backlog", "done": "Done"},
			},
		},
	}

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &boardOptions{state: "all"}
	err := runBoardWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should use dual search calls, NOT GetProjectItemsForBoard
	if mock.getBoardItemsForBoardCalled {
		t.Error("Expected dual SearchRepositoryIssues calls, but GetProjectItemsForBoard was called")
	}
	if len(mock.searchCalls) != 2 {
		t.Fatalf("Expected 2 SearchRepositoryIssues calls, got %d", len(mock.searchCalls))
	}

	states := map[string]bool{}
	for _, call := range mock.searchCalls {
		states[call.State] = true
	}
	if !states["open"] || !states["closed"] {
		t.Errorf("Expected calls for 'open' and 'closed' states, got: %v", mock.searchCalls)
	}

	output := buf.String()
	if !strings.Contains(output, "Open Issue") {
		t.Errorf("Expected 'Open Issue' in output, got: %s", output)
	}
	if !strings.Contains(output, "Closed Issue") {
		t.Errorf("Expected 'Closed Issue' in output, got: %s", output)
	}
}

func TestRunBoardWithDeps_StateAll_NoRepoFilter_FallsBack(t *testing.T) {
	mock := newMockBoardClient()
	mock.boardItems = []api.BoardItem{
		{Number: 1, Title: "Fallback Issue", Status: "Backlog", State: "OPEN"},
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

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &boardOptions{state: "all"}
	err := runBoardWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Without repo, should fall back to GetProjectItemsForBoard
	if !mock.getBoardItemsForBoardCalled {
		t.Error("Expected GetProjectItemsForBoard when no repo filter is set")
	}
	if len(mock.searchCalls) != 0 {
		t.Errorf("Expected no search calls, got %d", len(mock.searchCalls))
	}
}

func TestRunBoardWithDeps_StateFilterInFallbackPath(t *testing.T) {
	mock := newMockBoardClient()
	// No repo configured - forces fallback path
	mock.boardItems = []api.BoardItem{
		{Number: 1, Title: "Open Issue", Status: "Backlog", State: "OPEN"},
		{Number: 2, Title: "Closed Issue", Status: "Backlog", State: "CLOSED"},
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{}, // No repo - forces fallback
		Fields: map[string]config.Field{
			"status": {
				Field:  "Status",
				Values: map[string]string{"backlog": "Backlog"},
			},
		},
	}

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	// state=open in fallback path should filter client-side
	opts := &boardOptions{state: "open"}
	err := runBoardWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Open Issue") {
		t.Error("expected Open Issue in output")
	}
	if strings.Contains(output, "Closed Issue") {
		t.Error("Closed Issue should be filtered out when state=open")
	}
}

func TestRunBoardWithDeps_SearchAPIError(t *testing.T) {
	mock := newMockBoardClient()
	mock.searchIssuesErr = errors.New("search API error")

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"test-org/test-repo"},
	}

	cmd := newBoardCommand()
	opts := &boardOptions{state: "open"}
	err := runBoardWithDeps(cmd, opts, cfg, mock)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to search issues") {
		t.Errorf("expected 'failed to search issues' error, got: %v", err)
	}
}

func TestRunBoardWithDeps_EnrichError(t *testing.T) {
	mock := newMockBoardClient()
	mock.issues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Test", State: "OPEN"},
	}
	mock.getFieldsForIssues = errors.New("enrich error")

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"test-org/test-repo"},
	}

	cmd := newBoardCommand()
	opts := &boardOptions{state: "open"}
	err := runBoardWithDeps(cmd, opts, cfg, mock)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to enrich issues") {
		t.Errorf("expected 'failed to enrich issues' error, got: %v", err)
	}
}

func TestFilterBoardItemsByState(t *testing.T) {
	items := []api.BoardItem{
		{Number: 1, Title: "Open 1", State: "OPEN"},
		{Number: 2, Title: "Closed 1", State: "CLOSED"},
		{Number: 3, Title: "Open 2", State: "OPEN"},
	}

	tests := []struct {
		state         string
		expectedCount int
	}{
		{"open", 2},
		{"OPEN", 2},
		{"closed", 1},
		{"CLOSED", 1},
	}

	for _, tt := range tests {
		filtered := filterBoardItemsByState(items, tt.state)
		if len(filtered) != tt.expectedCount {
			t.Errorf("filterBoardItemsByState(state=%q): got %d items, want %d", tt.state, len(filtered), tt.expectedCount)
		}
	}
}

func TestEnrichIssuesToBoardItems(t *testing.T) {
	mock := newMockBoardClient()
	mock.fieldsByID = map[string][]api.FieldValue{
		"issue-1": {
			{Field: "Status", Value: "In Progress"},
			{Field: "Priority", Value: "P0"},
		},
		"issue-2": {
			{Field: "Status", Value: "Done"},
		},
	}

	issues := []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Issue One", State: "OPEN"},
		{ID: "issue-2", Number: 2, Title: "Issue Two", State: "CLOSED"},
	}

	items, err := enrichIssuesToBoardItems(mock, "proj-1", issues, "test-org/test-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// Check first item
	if items[0].Number != 1 {
		t.Errorf("item 0 number = %d, want 1", items[0].Number)
	}
	if items[0].Status != "In Progress" {
		t.Errorf("item 0 status = %q, want 'In Progress'", items[0].Status)
	}
	if items[0].Priority != "P0" {
		t.Errorf("item 0 priority = %q, want 'P0'", items[0].Priority)
	}
	if items[0].Repository != "test-org/test-repo" {
		t.Errorf("item 0 repository = %q, want 'test-org/test-repo'", items[0].Repository)
	}

	// Check second item
	if items[1].Status != "Done" {
		t.Errorf("item 1 status = %q, want 'Done'", items[1].Status)
	}
	if items[1].Priority != "" {
		t.Errorf("item 1 priority = %q, want empty", items[1].Priority)
	}
}

func TestEnrichIssuesToBoardItems_EmptyInput(t *testing.T) {
	mock := newMockBoardClient()

	items, err := enrichIssuesToBoardItems(mock, "proj-1", []api.Issue{}, "test-org/test-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if items != nil {
		t.Errorf("expected nil for empty input, got %v", items)
	}
}

// ============================================================================
// --repo Flag Tests
// ============================================================================

func TestRunBoardWithDeps_RepoFlagOverridesConfig(t *testing.T) {
	mock := newMockBoardClient()
	// Set up issues for Search API path
	mock.issues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "Repo Flag Issue", State: "OPEN"},
	}
	mock.fieldsByID = map[string][]api.FieldValue{
		"issue-1": {
			{Field: "Status", Value: "Backlog"},
		},
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{"test-org/config-repo"}, // Config repo
		Fields: map[string]config.Field{
			"status": {
				Field:  "Status",
				Values: map[string]string{"backlog": "Backlog"},
			},
		},
	}

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	// Use --repo flag to override config
	opts := &boardOptions{
		repo:  "other-org/other-repo",
		state: "open",
	}
	err := runBoardWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Repo Flag Issue") {
		t.Error("expected Repo Flag Issue in output")
	}
}

func TestRunBoardWithDeps_RepoFlagInvalidFormat(t *testing.T) {
	mock := newMockBoardClient()

	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newBoardCommand()
	opts := &boardOptions{
		repo: "invalid-format", // Missing owner/repo format
	}
	err := runBoardWithDeps(cmd, opts, cfg, mock)

	if err == nil {
		t.Fatal("expected error for invalid repo format, got nil")
	}
	if !strings.Contains(err.Error(), "invalid --repo format") {
		t.Errorf("expected 'invalid --repo format' error, got: %v", err)
	}
}

func TestRunBoardWithDeps_RepoFlagWithNoConfigRepo(t *testing.T) {
	mock := newMockBoardClient()
	// Set up issues for Search API path
	mock.issues = []api.Issue{
		{ID: "issue-1", Number: 1, Title: "No Config Repo Issue", State: "OPEN"},
	}
	mock.fieldsByID = map[string][]api.FieldValue{
		"issue-1": {
			{Field: "Status", Value: "Backlog"},
		},
	}

	cfg := &config.Config{
		Project:      config.Project{Owner: "test-org", Number: 1},
		Repositories: []string{}, // No repo in config
		Fields: map[string]config.Field{
			"status": {
				Field:  "Status",
				Values: map[string]string{"backlog": "Backlog"},
			},
		},
	}

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	// Use --repo flag when no config repo exists
	opts := &boardOptions{
		repo:  "flag-org/flag-repo",
		state: "open",
	}
	err := runBoardWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No Config Repo Issue") {
		t.Error("expected No Config Repo Issue in output")
	}
}

func TestSafeFdToInt(t *testing.T) {
	tests := []struct {
		name string
		fd   uintptr
		want int
		ok   bool
	}{
		{"zero", 0, 0, true},
		{"normal fd", 1, 1, true},
		{"typical fd", 42, 42, true},
		{"max safe value", uintptr(math.MaxInt), math.MaxInt, true},
		{"overflow", uintptr(math.MaxInt) + 1, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := safeFdToInt(tt.fd)
			if ok != tt.ok {
				t.Errorf("safeFdToInt(%d) ok = %v, want %v", tt.fd, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("safeFdToInt(%d) = %d, want %d", tt.fd, got, tt.want)
			}
		})
	}
}
