package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	graphql "github.com/cli/shurcooL-graphql"
)

func TestSafeGraphQLInt_ValidValues(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"zero", 0, 0},
		{"positive", 100, 100},
		{"negative", -100, -100},
		{"max int32", 2147483647, 2147483647},
		{"min int32", -2147483648, -2147483648},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := safeGraphQLInt(tt.input)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if int(result) != tt.expected {
				t.Errorf("safeGraphQLInt(%d) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSafeGraphQLInt_Overflow(t *testing.T) {
	// Values that exceed int32 range (only possible on 64-bit systems)
	largeValue := int(2147483648) // max int32 + 1
	_, err := safeGraphQLInt(largeValue)
	if err == nil {
		t.Error("Expected error for value exceeding int32 max")
	}
}

func TestSplitRepoName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "valid owner/repo format",
			input:    "rubrical-works/gh-pmu",
			expected: []string{"rubrical-works", "gh-pmu"},
		},
		{
			name:     "no slash returns nil",
			input:    "noslash",
			expected: nil,
		},
		{
			name:     "empty string returns nil",
			input:    "",
			expected: nil,
		},
		{
			name:     "slash at beginning",
			input:    "/repo",
			expected: []string{"", "repo"},
		},
		{
			name:     "slash at end",
			input:    "owner/",
			expected: []string{"owner", ""},
		},
		{
			name:     "multiple slashes returns first split only",
			input:    "owner/repo/extra",
			expected: []string{"owner", "repo/extra"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitRepoName(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("splitRepoName(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// GetProject Tests with Mocking - User vs Org fallback
// ============================================================================

// queryMockClient is a simple mock that tracks query names and can return errors
type queryMockClient struct {
	queryCalls  []string
	mutateCalls []string
	queryFunc   func(name string, query interface{}, variables map[string]interface{}) error
	mutateFunc  func(name string, mutation interface{}, variables map[string]interface{}) error
}

func (m *queryMockClient) Query(name string, query interface{}, variables map[string]interface{}) error {
	m.queryCalls = append(m.queryCalls, name)
	if m.queryFunc != nil {
		return m.queryFunc(name, query, variables)
	}
	return nil
}

func (m *queryMockClient) Mutate(name string, mutation interface{}, variables map[string]interface{}) error {
	m.mutateCalls = append(m.mutateCalls, name)
	if m.mutateFunc != nil {
		return m.mutateFunc(name, mutation, variables)
	}
	return nil
}

func TestGetProject_UserSucceeds(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetUserProject" {
				// Populate user project response using reflection
				v := reflect.ValueOf(query).Elem()
				user := v.FieldByName("User")
				projectV2 := user.FieldByName("ProjectV2")
				projectV2.FieldByName("ID").SetString("proj-123")
				projectV2.FieldByName("Number").SetInt(1)
				projectV2.FieldByName("Title").SetString("Test Project")
				projectV2.FieldByName("URL").SetString("https://github.com/users/owner/projects/1")
				projectV2.FieldByName("Closed").SetBool(false)
				return nil
			}
			return errors.New("unexpected query")
		},
	}

	client := NewClientWithGraphQL(mock)
	project, err := client.GetProject("owner", 1)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if project == nil {
		t.Fatal("Expected project to be returned")
	}
	if project.ID != "proj-123" {
		t.Errorf("Expected project ID 'proj-123', got '%s'", project.ID)
	}
	if project.Owner.Type != "User" {
		t.Errorf("Expected owner type 'User', got '%s'", project.Owner.Type)
	}
	if len(mock.queryCalls) != 1 || mock.queryCalls[0] != "GetUserProject" {
		t.Errorf("Expected only GetUserProject query, got: %v", mock.queryCalls)
	}
}

func TestGetProject_UserFailsOrgSucceeds(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetUserProject" {
				return errors.New("user not found")
			}
			if name == "GetOrgProject" {
				// Populate org project response
				v := reflect.ValueOf(query).Elem()
				org := v.FieldByName("Organization")
				projectV2 := org.FieldByName("ProjectV2")
				projectV2.FieldByName("ID").SetString("org-proj-456")
				projectV2.FieldByName("Number").SetInt(2)
				projectV2.FieldByName("Title").SetString("Org Project")
				projectV2.FieldByName("URL").SetString("https://github.com/orgs/myorg/projects/2")
				projectV2.FieldByName("Closed").SetBool(false)
				return nil
			}
			return errors.New("unexpected query")
		},
	}

	client := NewClientWithGraphQL(mock)
	project, err := client.GetProject("myorg", 2)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if project == nil {
		t.Fatal("Expected project to be returned")
	}
	if project.ID != "org-proj-456" {
		t.Errorf("Expected project ID 'org-proj-456', got '%s'", project.ID)
	}
	if project.Owner.Type != "Organization" {
		t.Errorf("Expected owner type 'Organization', got '%s'", project.Owner.Type)
	}
	// Should have tried user first, then org
	if len(mock.queryCalls) != 2 {
		t.Errorf("Expected 2 query calls, got: %v", mock.queryCalls)
	}
}

func TestGetProject_BothFail(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetUserProject" {
				return errors.New("user not found")
			}
			if name == "GetOrgProject" {
				return errors.New("org not found")
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	project, err := client.GetProject("unknown", 1)

	if err == nil {
		t.Fatal("Expected error when both user and org fail")
	}
	if project != nil {
		t.Error("Expected nil project when error occurs")
	}
	if !strings.Contains(err.Error(), "failed to get project") {
		t.Errorf("Expected 'failed to get project' error, got: %v", err)
	}
}

// ============================================================================
// ResolveBacklogViewNumber Tests (#901)
// ============================================================================

// viewsMockClient populates the GetProjectViews query struct by reflection,
// one canned page per call, mirroring queryMockClient's approach.
type viewsMockClient struct {
	// pages is consumed one entry per Query call, so a two-entry slice
	// exercises the cursor loop.
	pages     []viewsPage
	callCount int
	cursors   []interface{}
	ownerNull bool
	projNull  bool
	err       error
}

type viewsPage struct {
	views       []ProjectView
	hasNextPage bool
	endCursor   string
}

func (m *viewsMockClient) Query(name string, query interface{}, variables map[string]interface{}) error {
	m.callCount++
	m.cursors = append(m.cursors, variables["cursor"])
	if m.err != nil {
		return m.err
	}

	root := reflect.ValueOf(query).Elem().FieldByName("RepositoryOwner")
	if m.ownerNull {
		return nil // owner absent leaves __typename empty
	}
	root.FieldByName("TypeName").SetString("Organization")

	proj := root.FieldByName("ProjectV2Owner").FieldByName("ProjectV2")
	if m.projNull {
		return nil // project absent leaves ID empty
	}
	proj.FieldByName("ID").SetString("PVT_kwDOTest")

	idx := m.callCount - 1
	if idx >= len(m.pages) {
		return fmt.Errorf("unexpected extra page request (call %d)", m.callCount)
	}
	page := m.pages[idx]

	views := proj.FieldByName("Views")
	nodes := views.FieldByName("Nodes")
	slice := reflect.MakeSlice(nodes.Type(), len(page.views), len(page.views))
	for i, v := range page.views {
		n := slice.Index(i)
		n.FieldByName("ID").SetString(v.ID)
		n.FieldByName("Number").SetInt(int64(v.Number))
		n.FieldByName("Name").SetString(v.Name)
		n.FieldByName("Layout").SetString(v.Layout)
	}
	nodes.Set(slice)

	pi := views.FieldByName("PageInfo")
	pi.FieldByName("HasNextPage").SetBool(page.hasNextPage)
	pi.FieldByName("EndCursor").SetString(page.endCursor)
	return nil
}

func (m *viewsMockClient) Mutate(name string, mutation interface{}, variables map[string]interface{}) error {
	return nil
}

func TestResolveBacklogViewNumber_PicksFirstBoardLayoutBacklog(t *testing.T) {
	// A TABLE_LAYOUT "Backlog" sits at an earlier position than the board.
	// Position order alone would pick the wrong view — layout must filter first.
	mock := &viewsMockClient{pages: []viewsPage{{views: []ProjectView{
		{ID: "v1", Number: 1, Name: "Backlog", Layout: "TABLE_LAYOUT"},
		{ID: "v2", Number: 4, Name: "Roadmap", Layout: "ROADMAP_LAYOUT"},
		{ID: "v3", Number: 2, Name: "Backlog", Layout: "BOARD_LAYOUT"},
		{ID: "v4", Number: 3, Name: "Backlog", Layout: "BOARD_LAYOUT"},
	}}}}

	number, found, err := NewClientWithGraphQL(mock).ResolveBacklogViewNumber("owner", 11)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !found {
		t.Fatal("Expected a Backlog board view to be found")
	}
	// Deliberately not 1: proves no default and no sort by number.
	if number != 2 {
		t.Errorf("Expected view number 2 (first BOARD_LAYOUT Backlog in position order), got %d", number)
	}
}

func TestResolveBacklogViewNumber_NameMatchingTrimsAndFolds(t *testing.T) {
	tests := []struct {
		name      string
		viewName  string
		wantFound bool
	}{
		{name: "exact", viewName: "Backlog", wantFound: true},
		{name: "leading and trailing space", viewName: "  Backlog  ", wantFound: true},
		{name: "uppercase", viewName: "BACKLOG", wantFound: true},
		{name: "lowercase", viewName: "backlog", wantFound: true},
		{name: "tab padded", viewName: "\tBacklog\t", wantFound: true},
		// Equality, not prefix or substring — a differently-named board is not the Backlog.
		{name: "prefix is not a match", viewName: "Backlog Archive", wantFound: false},
		{name: "suffix is not a match", viewName: "Old Backlog", wantFound: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &viewsMockClient{pages: []viewsPage{{views: []ProjectView{
				{ID: "v1", Number: 7, Name: tt.viewName, Layout: "BOARD_LAYOUT"},
			}}}}

			number, found, err := NewClientWithGraphQL(mock).ResolveBacklogViewNumber("owner", 11)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if found != tt.wantFound {
				t.Errorf("View named %q: found = %v, want %v", tt.viewName, found, tt.wantFound)
			}
			if !tt.wantFound && number != 0 {
				t.Errorf("Expected view number 0 when not found, got %d", number)
			}
		})
	}
}

func TestResolveBacklogViewNumber_NoBacklogViewIsNotAnError(t *testing.T) {
	// There is no createProjectV2View mutation, so a missing Backlog view is
	// reported and never repaired. It must not read as a failure, and must not
	// fall back to 1.
	mock := &viewsMockClient{pages: []viewsPage{{views: []ProjectView{
		{ID: "v1", Number: 1, Name: "Board", Layout: "BOARD_LAYOUT"},
		{ID: "v2", Number: 2, Name: "Backlog", Layout: "TABLE_LAYOUT"},
	}}}}

	number, found, err := NewClientWithGraphQL(mock).ResolveBacklogViewNumber("owner", 11)

	if err != nil {
		t.Fatalf("Expected no error for a board without a Backlog view, got: %v", err)
	}
	if found {
		t.Error("Expected found = false when no BOARD_LAYOUT Backlog view exists")
	}
	if number != 0 {
		t.Errorf("Expected view number 0, got %d — must not default to 1", number)
	}
}

func TestResolveBacklogViewNumber_EmptyViewList(t *testing.T) {
	mock := &viewsMockClient{pages: []viewsPage{{views: nil}}}

	number, found, err := NewClientWithGraphQL(mock).ResolveBacklogViewNumber("owner", 11)

	if err != nil {
		t.Fatalf("Expected no error for an empty view list, got: %v", err)
	}
	if found || number != 0 {
		t.Errorf("Expected (0, false) for an empty view list, got (%d, %v)", number, found)
	}
}

func TestResolveBacklogViewNumber_FindsMatchOnSecondPage(t *testing.T) {
	// The match lives past the first page boundary; stopping at page 1 would
	// silently report "no Backlog view" for a board that has one.
	mock := &viewsMockClient{pages: []viewsPage{
		{
			views:       []ProjectView{{ID: "v1", Number: 1, Name: "Table", Layout: "TABLE_LAYOUT"}},
			hasNextPage: true,
			endCursor:   "CURSOR_PAGE_2",
		},
		{
			views: []ProjectView{{ID: "v2", Number: 9, Name: "Backlog", Layout: "BOARD_LAYOUT"}},
		},
	}}

	number, found, err := NewClientWithGraphQL(mock).ResolveBacklogViewNumber("owner", 11)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !found || number != 9 {
		t.Errorf("Expected (9, true) from the second page, got (%d, %v)", number, found)
	}
	if mock.callCount != 2 {
		t.Errorf("Expected 2 page requests, got %d", mock.callCount)
	}
	// First request must send a null cursor, second the previous page's endCursor.
	if mock.cursors[0] != (*graphql.String)(nil) {
		t.Errorf("Expected a null cursor on the first request, got %#v", mock.cursors[0])
	}
	if got, want := mock.cursors[1], graphql.String("CURSOR_PAGE_2"); got != want {
		t.Errorf("Expected cursor %q on the second request, got %#v", want, got)
	}
}

func TestResolveBacklogViewNumber_StopsPagingOnceMatched(t *testing.T) {
	// A match on page 1 must not trigger a second round-trip even when the
	// connection reports more pages.
	mock := &viewsMockClient{pages: []viewsPage{
		{
			views:       []ProjectView{{ID: "v1", Number: 3, Name: "Backlog", Layout: "BOARD_LAYOUT"}},
			hasNextPage: true,
			endCursor:   "CURSOR_PAGE_2",
		},
	}}

	number, found, err := NewClientWithGraphQL(mock).ResolveBacklogViewNumber("owner", 11)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !found || number != 3 {
		t.Errorf("Expected (3, true), got (%d, %v)", number, found)
	}
	if mock.callCount != 1 {
		t.Errorf("Expected paging to stop after the match, got %d requests", mock.callCount)
	}
}

func TestResolveBacklogViewNumber_UnknownLayoutDoesNotMatch(t *testing.T) {
	// ProjectV2ViewLayout is a closed 3-value enum today. If GitHub rolls out a
	// fourth, the comparison must simply not match rather than panic or select it.
	mock := &viewsMockClient{pages: []viewsPage{{views: []ProjectView{
		{ID: "v1", Number: 1, Name: "Backlog", Layout: "TIMELINE_LAYOUT"},
		{ID: "v2", Number: 2, Name: "Backlog", Layout: ""},
	}}}}

	number, found, err := NewClientWithGraphQL(mock).ResolveBacklogViewNumber("owner", 11)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if found || number != 0 {
		t.Errorf("Expected an unknown layout not to match, got (%d, %v)", number, found)
	}
}

func TestResolveBacklogViewNumber_DistinctNotFoundOutcomes(t *testing.T) {
	// Collapsing these makes a typo'd owner look like a missing view. Each must
	// stay separable by the caller.
	t.Run("missing owner", func(t *testing.T) {
		mock := &viewsMockClient{ownerNull: true}
		_, _, err := NewClientWithGraphQL(mock).ResolveBacklogViewNumber("nosuchlogin", 11)
		if !errors.Is(err, ErrOwnerNotFound) {
			t.Fatalf("Expected ErrOwnerNotFound, got: %v", err)
		}
		if errors.Is(err, ErrProjectNotFound) {
			t.Error("Missing owner must not also report as a missing project")
		}
	})

	t.Run("missing project", func(t *testing.T) {
		mock := &viewsMockClient{projNull: true}
		_, _, err := NewClientWithGraphQL(mock).ResolveBacklogViewNumber("owner", 9999)
		if !errors.Is(err, ErrProjectNotFound) {
			t.Fatalf("Expected ErrProjectNotFound, got: %v", err)
		}
		if errors.Is(err, ErrOwnerNotFound) {
			t.Error("Missing project must not also report as a missing owner")
		}
	})

	t.Run("missing backlog view is not an error", func(t *testing.T) {
		mock := &viewsMockClient{pages: []viewsPage{{views: []ProjectView{
			{ID: "v1", Number: 1, Name: "Board", Layout: "BOARD_LAYOUT"},
		}}}}
		_, found, err := NewClientWithGraphQL(mock).ResolveBacklogViewNumber("owner", 11)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
		if found {
			t.Error("Expected found = false")
		}
	})
}

func TestResolveBacklogViewNumber_ScopeErrorIsDistinctFromMissingOwner(t *testing.T) {
	// A token without read:project gets a permission error, not a null owner.
	// Reporting it as "no such owner" sends the user chasing a typo.
	mock := &viewsMockClient{err: errors.New(
		"Your token has not been granted the required scopes to execute this query. " +
			"The 'id' field requires one of the following scopes: ['read:project']")}

	_, _, err := NewClientWithGraphQL(mock).ResolveBacklogViewNumber("owner", 11)

	if err == nil {
		t.Fatal("Expected an error for a token missing read:project")
	}
	if errors.Is(err, ErrOwnerNotFound) || errors.Is(err, ErrProjectNotFound) {
		t.Errorf("Scope failure must not collapse into a not-found outcome, got: %v", err)
	}
	if !strings.Contains(err.Error(), "read:project") {
		t.Errorf("Expected the scope requirement to survive into the message, got: %v", err)
	}
}

func TestResolveBacklogViewNumber_TransportErrorPropagates(t *testing.T) {
	// A response carrying errors[] alongside data must not be read as an empty
	// view list — that would report "no Backlog view" for a transient failure.
	mock := &viewsMockClient{err: errors.New("502 Bad Gateway")}

	number, found, err := NewClientWithGraphQL(mock).ResolveBacklogViewNumber("owner", 11)

	if err == nil {
		t.Fatal("Expected the transport error to propagate")
	}
	if found || number != 0 {
		t.Errorf("Expected (0, false) alongside the error, got (%d, %v)", number, found)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("Expected the underlying error to survive, got: %v", err)
	}
}

func TestResolveBacklogViewNumber_SendsOwnerAndNumberAsVariables(t *testing.T) {
	// Owner and number are user-controlled and must travel as GraphQL variables,
	// never interpolated into the document text (#832, #835).
	mock := &viewsMockClient{pages: []viewsPage{{views: []ProjectView{
		{ID: "v1", Number: 1, Name: "Backlog", Layout: "BOARD_LAYOUT"},
	}}}}

	var captured map[string]interface{}
	var opName string
	wrapper := &queryMockClient{queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
		captured = variables
		opName = name
		return mock.Query(name, query, variables)
	}}

	if _, _, err := NewClientWithGraphQL(wrapper).ResolveBacklogViewNumber("rubrical-worker", 11); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if opName != "GetProjectViews" {
		t.Errorf("Expected operation name GetProjectViews, got %q", opName)
	}
	if got, want := captured["owner"], graphql.String("rubrical-worker"); got != want {
		t.Errorf("Expected owner passed as a variable %q, got %#v", want, got)
	}
	if got, want := captured["number"], graphql.Int(11); got != want {
		t.Errorf("Expected number passed as a variable %v, got %#v", want, got)
	}
}

func TestResolveBacklogViewNumber_RejectsMalformedOwnerBeforeSending(t *testing.T) {
	// Owners carrying GraphQL syntax are refused by validateOwner before a
	// document is built, so nothing reaches the wire at all. That is a stronger
	// guarantee than parameterisation alone, and worth pinning: dropping the
	// guard would still pass the variables test above.
	for _, owner := range []string{`weird"owner`, "owner) { __schema", "", "own er"} {
		t.Run(owner, func(t *testing.T) {
			mock := &viewsMockClient{}

			_, _, err := NewClientWithGraphQL(mock).ResolveBacklogViewNumber(owner, 11)

			if err == nil {
				t.Fatalf("Expected owner %q to be rejected", owner)
			}
			if mock.callCount != 0 {
				t.Errorf("Expected no request to be sent for owner %q, got %d", owner, mock.callCount)
			}
			if errors.Is(err, ErrOwnerNotFound) {
				t.Error("A malformed owner is a caller error, not a not-found outcome")
			}
		})
	}
}

// ============================================================================
// GetRepositoryIssues State Mapping Tests
// ============================================================================

func TestGetRepositoryIssues_StateMapping(t *testing.T) {
	tests := []struct {
		name           string
		inputState     string
		expectedStates []IssueState
	}{
		{
			name:           "open state",
			inputState:     "open",
			expectedStates: []IssueState{IssueStateOpen},
		},
		{
			name:           "closed state",
			inputState:     "closed",
			expectedStates: []IssueState{IssueStateClosed},
		},
		{
			name:           "all state",
			inputState:     "all",
			expectedStates: []IssueState{IssueStateOpen, IssueStateClosed},
		},
		{
			name:           "empty state defaults to all",
			inputState:     "",
			expectedStates: []IssueState{IssueStateOpen, IssueStateClosed},
		},
		{
			name:           "custom state passed through",
			inputState:     "CUSTOM",
			expectedStates: []IssueState{"CUSTOM"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedStates []IssueState
			mock := &queryMockClient{
				queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
					if name == "GetRepositoryIssues" {
						// Capture the states that were passed - verify IssueState type
						if states, ok := variables["states"].([]IssueState); ok {
							capturedStates = states
						} else {
							t.Errorf("states variable is not []IssueState, got %T", variables["states"])
						}
					}
					return nil
				},
			}

			client := NewClientWithGraphQL(mock)
			_, _ = client.GetRepositoryIssues("owner", "repo", tt.inputState)

			// Verify the function was called
			if len(mock.queryCalls) != 1 || mock.queryCalls[0] != "GetRepositoryIssues" {
				t.Errorf("Expected GetRepositoryIssues query, got: %v", mock.queryCalls)
			}

			// Verify the captured states match expected
			if len(capturedStates) != len(tt.expectedStates) {
				t.Errorf("Expected %d states, got %d", len(tt.expectedStates), len(capturedStates))
				return
			}
			for i, expected := range tt.expectedStates {
				if capturedStates[i] != expected {
					t.Errorf("State[%d] = %v, want %v", i, capturedStates[i], expected)
				}
			}
		})
	}
}

func TestGetRepositoryIssues_QueryError(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("network error")
		},
	}

	client := NewClientWithGraphQL(mock)
	issues, err := client.GetRepositoryIssues("owner", "repo", "open")

	if err == nil {
		t.Fatal("Expected error when query fails")
	}
	if issues != nil {
		t.Error("Expected nil issues when error occurs")
	}
	if !strings.Contains(err.Error(), "failed to get issues") {
		t.Errorf("Expected 'failed to get issues' error, got: %v", err)
	}
}

// ============================================================================
// GetParentIssue Tests
// ============================================================================

func TestGetParentIssue_NoParent(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			// Don't populate parent - leave ID empty
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	parent, err := client.GetParentIssue("owner", "repo", 1)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if parent != nil {
		t.Error("Expected nil parent when issue has no parent")
	}
}

func TestGetParentIssue_HasParent(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetParentIssue" {
				v := reflect.ValueOf(query).Elem()
				repo := v.FieldByName("Repository")
				issue := repo.FieldByName("Issue")
				parent := issue.FieldByName("Parent")
				parent.FieldByName("ID").SetString("parent-123")
				parent.FieldByName("Number").SetInt(42)
				parent.FieldByName("Title").SetString("Parent Issue")
				parent.FieldByName("State").SetString("OPEN")
				parent.FieldByName("URL").SetString("https://github.com/owner/repo/issues/42")
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	parent, err := client.GetParentIssue("owner", "repo", 1)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if parent == nil {
		t.Fatal("Expected parent issue to be returned")
	}
	if parent.ID != "parent-123" {
		t.Errorf("Expected parent ID 'parent-123', got '%s'", parent.ID)
	}
	if parent.Number != 42 {
		t.Errorf("Expected parent number 42, got %d", parent.Number)
	}
}

func TestGetParentIssue_QueryError(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("query failed")
		},
	}

	client := NewClientWithGraphQL(mock)
	parent, err := client.GetParentIssue("owner", "repo", 1)

	if err == nil {
		t.Fatal("Expected error when query fails")
	}
	if parent != nil {
		t.Error("Expected nil parent when error occurs")
	}
	if !strings.Contains(err.Error(), "failed to get parent issue") {
		t.Errorf("Expected 'failed to get parent issue' error, got: %v", err)
	}
}

// ============================================================================
// GetSubIssues Tests
// ============================================================================

func TestGetSubIssues_QueryError(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("query failed")
		},
	}

	client := NewClientWithGraphQL(mock)
	subIssues, err := client.GetSubIssues("owner", "repo", 1)

	if err == nil {
		t.Fatal("Expected error when query fails")
	}
	if subIssues != nil {
		t.Error("Expected nil subIssues when error occurs")
	}
	if !strings.Contains(err.Error(), "failed to get sub-issues") {
		t.Errorf("Expected 'failed to get sub-issues' error, got: %v", err)
	}
}

func TestGetSubIssues_EmptyResult(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			// Don't populate any sub-issues
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	subIssues, err := client.GetSubIssues("owner", "repo", 1)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(subIssues) != 0 {
		t.Errorf("Expected empty subIssues, got %d", len(subIssues))
	}
}

// ============================================================================
// GetSubIssueCounts Tests
// ============================================================================

func TestGetSubIssueCounts_EmptyInput(t *testing.T) {
	client, cErr := NewClient()
	if cErr != nil {
		t.Skipf("Skipping - requires auth: %v", cErr)
	}
	counts, err := client.GetSubIssueCounts("owner", "repo", []int{})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(counts) != 0 {
		t.Errorf("Expected empty counts map, got %d entries", len(counts))
	}
}

// ============================================================================
// GetSubIssuesBatch Tests
// ============================================================================
// Note: GetSubIssuesBatch uses exec.Command("gh", ...) which makes full pagination
// testing difficult without E2E tests. The empty input test validates basic behavior,
// while pagination fallback behavior is verified through E2E tests with large epics.

func TestGetSubIssuesBatch_EmptyInput(t *testing.T) {
	client, cErr := NewClient()
	if cErr != nil {
		t.Skipf("Skipping - requires auth: %v", cErr)
	}
	result, err := client.GetSubIssuesBatch("owner", "repo", []int{})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("Expected empty result map, got %d entries", len(result))
	}
}

// ============================================================================
// parseSubIssueCountsResponse Tests
// ============================================================================

func TestParseSubIssueCountsResponse_MultipleIssues(t *testing.T) {
	jsonData := []byte(`{
		"data": {
			"repository": {
				"i0": {"subIssues": {"totalCount": 3}},
				"i1": {"subIssues": {"totalCount": 0}},
				"i2": {"subIssues": {"totalCount": 7}}
			}
		}
	}`)
	numbers := []int{10, 20, 30}

	result, err := parseSubIssueCountsResponse(jsonData, numbers)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("Expected 3 entries, got %d", len(result))
	}
	if result[10] != 3 {
		t.Errorf("Expected issue 10 count=3, got %d", result[10])
	}
	if result[20] != 0 {
		t.Errorf("Expected issue 20 count=0, got %d", result[20])
	}
	if result[30] != 7 {
		t.Errorf("Expected issue 30 count=7, got %d", result[30])
	}
}

func TestParseSubIssueCountsResponse_MissingAlias(t *testing.T) {
	// If an alias is missing from the response, count should default to 0
	jsonData := []byte(`{
		"data": {
			"repository": {
				"i0": {"subIssues": {"totalCount": 5}}
			}
		}
	}`)
	numbers := []int{10, 20}

	result, err := parseSubIssueCountsResponse(jsonData, numbers)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result[10] != 5 {
		t.Errorf("Expected issue 10 count=5, got %d", result[10])
	}
	if result[20] != 0 {
		t.Errorf("Expected issue 20 count=0 (missing alias), got %d", result[20])
	}
}

func TestParseSubIssueCountsResponse_InvalidJSON(t *testing.T) {
	jsonData := []byte(`not valid json`)
	numbers := []int{10}

	_, err := parseSubIssueCountsResponse(jsonData, numbers)
	if err == nil {
		t.Fatal("Expected error for invalid JSON")
	}
}

func TestParseSubIssueCountsResponse_GraphQLErrors(t *testing.T) {
	// #872 finding 1: a GraphQL errors array must be inspected, not ignored —
	// otherwise every issue silently gets count 0, indistinguishable from "no
	// sub-issues".
	jsonData := []byte(`{"data":{"repository":{}},"errors":[{"message":"rate limited"}]}`)
	numbers := []int{1, 2}

	_, err := parseSubIssueCountsResponse(jsonData, numbers)
	if err == nil {
		t.Fatal("expected error when GraphQL errors array is present, got nil")
	}
}

// ============================================================================
// parseSubIssuesBatchResponse Tests
// ============================================================================

func TestParseSubIssuesBatchResponse_MalformedItemWarns(t *testing.T) {
	// #872 finding 2: a malformed per-item response must warn (not silently map to
	// an empty list that reads as "no sub-issues"). The item is still treated as
	// empty so the batch continues.
	data := []byte(`{"data":{"repository":{"i0":{"subIssues":"not-an-object"}}}}`)

	oldErr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	result, _, err := parseSubIssuesBatchResponse(data, []int{5})

	_ = w.Close()
	os.Stderr = oldErr
	var buf strings.Builder
	_, _ = io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("expected no top-level error for a malformed item, got: %v", err)
	}
	if len(result[5]) != 0 {
		t.Errorf("expected empty sub-issue list for malformed item, got %d", len(result[5]))
	}
	if !strings.Contains(buf.String(), "Warning") {
		t.Errorf("expected a stderr warning for the malformed item, got: %q", buf.String())
	}
}

func TestParseSubIssuesBatchResponse_MultipleIssues(t *testing.T) {
	jsonData := []byte(`{
		"data": {
			"repository": {
				"i0": {
					"subIssues": {
						"nodes": [
							{"id": "id1", "number": 101, "title": "Sub A", "state": "OPEN", "url": "https://github.com/o/r/issues/101", "repository": {"name": "r", "owner": {"login": "o"}}},
							{"id": "id2", "number": 102, "title": "Sub B", "state": "CLOSED", "url": "https://github.com/o/r/issues/102", "repository": {"name": "r", "owner": {"login": "o"}}}
						],
						"pageInfo": {"hasNextPage": false}
					}
				},
				"i1": {
					"subIssues": {
						"nodes": [],
						"pageInfo": {"hasNextPage": false}
					}
				}
			}
		}
	}`)
	numbers := []int{10, 20}

	result, needsPagination, err := parseSubIssuesBatchResponse(jsonData, numbers)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(needsPagination) != 0 {
		t.Errorf("Expected no pagination needed, got %v", needsPagination)
	}
	if len(result[10]) != 2 {
		t.Fatalf("Expected 2 sub-issues for issue 10, got %d", len(result[10]))
	}
	if result[10][0].Number != 101 || result[10][0].Title != "Sub A" {
		t.Errorf("Expected sub-issue 101 'Sub A', got %d %q", result[10][0].Number, result[10][0].Title)
	}
	if result[10][1].State != "CLOSED" {
		t.Errorf("Expected sub-issue 102 CLOSED, got %q", result[10][1].State)
	}
	if len(result[20]) != 0 {
		t.Errorf("Expected 0 sub-issues for issue 20, got %d", len(result[20]))
	}
}

func TestParseSubIssuesBatchResponse_PaginationDetection(t *testing.T) {
	jsonData := []byte(`{
		"data": {
			"repository": {
				"i0": {
					"subIssues": {
						"nodes": [
							{"id": "id1", "number": 101, "title": "Sub A", "state": "OPEN", "url": "u", "repository": {"name": "r", "owner": {"login": "o"}}}
						],
						"pageInfo": {"hasNextPage": true}
					}
				},
				"i1": {
					"subIssues": {
						"nodes": [],
						"pageInfo": {"hasNextPage": false}
					}
				}
			}
		}
	}`)
	numbers := []int{10, 20}

	result, needsPagination, err := parseSubIssuesBatchResponse(jsonData, numbers)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(needsPagination) != 1 || needsPagination[0] != 10 {
		t.Errorf("Expected pagination needed for issue 10, got %v", needsPagination)
	}
	// Issue 10 should still have partial results from the batch
	if len(result[10]) != 1 {
		t.Errorf("Expected 1 partial sub-issue for issue 10, got %d", len(result[10]))
	}
	if len(result[20]) != 0 {
		t.Errorf("Expected 0 sub-issues for issue 20, got %d", len(result[20]))
	}
}

func TestParseSubIssuesBatchResponse_MissingAlias(t *testing.T) {
	jsonData := []byte(`{
		"data": {
			"repository": {
				"i0": {
					"subIssues": {
						"nodes": [{"id": "id1", "number": 101, "title": "Sub", "state": "OPEN", "url": "u", "repository": {"name": "r", "owner": {"login": "o"}}}],
						"pageInfo": {"hasNextPage": false}
					}
				}
			}
		}
	}`)
	numbers := []int{10, 20}

	result, _, err := parseSubIssuesBatchResponse(jsonData, numbers)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(result[20]) != 0 {
		t.Errorf("Expected empty sub-issues for missing alias, got %d", len(result[20]))
	}
}

func TestParseSubIssuesBatchResponse_GraphQLErrors(t *testing.T) {
	jsonData := []byte(`{
		"data": {"repository": {}},
		"errors": [{"message": "Something went wrong"}]
	}`)
	numbers := []int{10}

	_, _, err := parseSubIssuesBatchResponse(jsonData, numbers)
	if err == nil {
		t.Fatal("Expected error for GraphQL errors in response")
	}
}

func TestParseSubIssuesBatchResponse_InvalidJSON(t *testing.T) {
	jsonData := []byte(`not valid json`)
	numbers := []int{10}

	_, _, err := parseSubIssuesBatchResponse(jsonData, numbers)
	if err == nil {
		t.Fatal("Expected error for invalid JSON")
	}
}

// ============================================================================
// GetProjectItemIDForIssue Tests
// ============================================================================

func TestGetProjectItemIDForIssue_QueryError(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("query failed")
		},
	}

	client := NewClientWithGraphQL(mock)
	_, err := client.GetProjectItemIDForIssue("project-id", "owner", "repo", 1)

	if err == nil {
		t.Fatal("Expected error for query failure")
	}
}

func TestGetProjectItemIDForIssue_NotInProject(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			// Return empty project items - issue is not in any project
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	_, err := client.GetProjectItemIDForIssue("project-id", "owner", "repo", 1)

	if err == nil {
		t.Fatal("Expected error when issue is not in project")
	}
	if !strings.Contains(err.Error(), "not in the project") {
		t.Errorf("Expected 'not in the project' error, got: %v", err)
	}
}

// ============================================================================
// GetIssue Tests
// ============================================================================

func TestGetIssue_QueryError(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("query failed")
		},
	}

	client := NewClientWithGraphQL(mock)
	issue, err := client.GetIssue("owner", "repo", 1)

	if err == nil {
		t.Fatal("Expected error when query fails")
	}
	if issue != nil {
		t.Error("Expected nil issue when error occurs")
	}
	if !strings.Contains(err.Error(), "failed to get issue") {
		t.Errorf("Expected 'failed to get issue' error, got: %v", err)
	}
}

// ============================================================================
// GetProjectItems Tests
// ============================================================================

func TestGetProjectItems_QueryError(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("query failed")
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItems("proj-id", nil)

	if err == nil {
		t.Fatal("Expected error when query fails")
	}
	if items != nil {
		t.Error("Expected nil items when error occurs")
	}
	if !strings.Contains(err.Error(), "failed to get project items") {
		t.Errorf("Expected 'failed to get project items' error, got: %v", err)
	}
}

func TestGetProjectItems_EmptyResult(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItems("proj-id", nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("Expected empty items, got %d", len(items))
	}
}

// ============================================================================
// ListProjects Tests with Mocking
// ============================================================================

func TestListProjects_UserSucceeds(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "ListUserProjects" {
				v := reflect.ValueOf(query).Elem()
				user := v.FieldByName("User")
				projectsV2 := user.FieldByName("ProjectsV2")
				nodes := projectsV2.FieldByName("Nodes")

				// Create a slice with one project
				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)
				newNode := reflect.New(nodeType).Elem()
				newNode.FieldByName("ID").SetString("proj-1")
				newNode.FieldByName("Number").SetInt(1)
				newNode.FieldByName("Title").SetString("User Project")
				newNode.FieldByName("URL").SetString("https://github.com/users/owner/projects/1")
				newNode.FieldByName("Closed").SetBool(false)
				newNodes.Index(0).Set(newNode)
				nodes.Set(newNodes)
				return nil
			}
			return errors.New("unexpected query")
		},
	}

	client := NewClientWithGraphQL(mock)
	projects, err := client.ListProjects("owner")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("Expected 1 project, got %d", len(projects))
	}
	if projects[0].Title != "User Project" {
		t.Errorf("Expected title 'User Project', got '%s'", projects[0].Title)
	}
}

// TestListProjects_UserErrorFallsToOrg verifies the org fallback triggers when
// the user path FAILS (as GitHub does for an org login: "Could not resolve to a
// User"). Post-#861, a *successful* user query — even with zero projects — no
// longer falls through to org (see TestListProjects_ZeroProjectUser); only a
// user-path error does.
func TestListProjects_UserErrorFallsToOrg(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "ListUserProjects" {
				// An org login cannot resolve as a user — GitHub returns an error.
				return errors.New("Could not resolve to a User with the login of 'myorg'")
			}
			if name == "ListOrgProjects" {
				v := reflect.ValueOf(query).Elem()
				org := v.FieldByName("Organization")
				projectsV2 := org.FieldByName("ProjectsV2")
				nodes := projectsV2.FieldByName("Nodes")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)
				newNode := reflect.New(nodeType).Elem()
				newNode.FieldByName("ID").SetString("org-proj-1")
				newNode.FieldByName("Number").SetInt(1)
				newNode.FieldByName("Title").SetString("Org Project")
				newNode.FieldByName("URL").SetString("https://github.com/orgs/myorg/projects/1")
				newNode.FieldByName("Closed").SetBool(false)
				newNodes.Index(0).Set(newNode)
				nodes.Set(newNodes)
				return nil
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	projects, err := client.ListProjects("myorg")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("Expected 1 project, got %d", len(projects))
	}
	if projects[0].Title != "Org Project" {
		t.Errorf("Expected title 'Org Project', got '%s'", projects[0].Title)
	}
}

func TestListProjects_SkipsClosedProjects(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "ListUserProjects" {
				v := reflect.ValueOf(query).Elem()
				user := v.FieldByName("User")
				projectsV2 := user.FieldByName("ProjectsV2")
				nodes := projectsV2.FieldByName("Nodes")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 2, 2)

				// Open project
				openNode := reflect.New(nodeType).Elem()
				openNode.FieldByName("ID").SetString("proj-1")
				openNode.FieldByName("Number").SetInt(1)
				openNode.FieldByName("Title").SetString("Open Project")
				openNode.FieldByName("Closed").SetBool(false)
				newNodes.Index(0).Set(openNode)

				// Closed project
				closedNode := reflect.New(nodeType).Elem()
				closedNode.FieldByName("ID").SetString("proj-2")
				closedNode.FieldByName("Number").SetInt(2)
				closedNode.FieldByName("Title").SetString("Closed Project")
				closedNode.FieldByName("Closed").SetBool(true)
				newNodes.Index(1).Set(closedNode)

				nodes.Set(newNodes)
				return nil
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	projects, err := client.ListProjects("owner")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("Expected 1 project (open only), got %d", len(projects))
	}
	if projects[0].Title != "Open Project" {
		t.Errorf("Expected open project, got '%s'", projects[0].Title)
	}
}

func TestListProjects_BothFail(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "ListUserProjects" {
				return errors.New("user not found")
			}
			if name == "ListOrgProjects" {
				return errors.New("org not found")
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	projects, err := client.ListProjects("unknown")

	if err == nil {
		t.Fatal("Expected error when both user and org fail")
	}
	if projects != nil {
		t.Error("Expected nil projects when error occurs")
	}
	if !strings.Contains(err.Error(), "failed to list projects") {
		t.Errorf("Expected 'failed to list projects' error, got: %v", err)
	}
}

// ============================================================================
// GetProjectFields Additional Tests
// ============================================================================

func TestGetProjectFields_QueryError(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("query failed")
		},
	}

	client := NewClientWithGraphQL(mock)
	fields, err := client.GetProjectFields("proj-id")

	if err == nil {
		t.Fatal("Expected error when query fails")
	}
	if fields != nil {
		t.Error("Expected nil fields when error occurs")
	}
	if !strings.Contains(err.Error(), "failed to get project fields") {
		t.Errorf("Expected 'failed to get project fields' error, got: %v", err)
	}
	// Generic query failure must NOT be classified as resolver-unavailable —
	// callers need to distinguish "the project doesn't exist" from "the new
	// timestamp-fields rollout broke the resolver."
	if errors.Is(err, ErrFieldResolverUnavailable) {
		t.Error("Generic query failure must not match ErrFieldResolverUnavailable")
	}
}

// FetchFieldsByNames tests — exercised by gh pmu init's resolver-crash
// fallback (see #853 AC7). Verifies the per-name path collects what it
// can and reports the rest as missing.

func TestFetchFieldsByNames_AllResolve(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name != "GetProjectFieldByName" {
				t.Errorf("Expected query named 'GetProjectFieldByName'; got %q", name)
			}
			// Mimic the live shape: TypeName=ProjectV2Field, populate that struct
			v := reflect.ValueOf(query).Elem()
			field := v.FieldByName("Node").FieldByName("ProjectV2").FieldByName("Field")
			field.FieldByName("TypeName").SetString("ProjectV2Field")
			pf := field.FieldByName("ProjectV2Field")
			pf.FieldByName("ID").SetString("id-" + string(variables["name"].(graphql.String)))
			pf.FieldByName("Name").SetString(string(variables["name"].(graphql.String)))
			pf.FieldByName("DataType").SetString("TEXT")
			return nil
		},
	}
	client := NewClientWithGraphQL(mock)
	got, missing := client.FetchFieldsByNames("proj-id", []string{"Title", "Branch"})

	if len(got) != 2 {
		t.Fatalf("Expected 2 resolved fields; got %d", len(got))
	}
	if len(missing) != 0 {
		t.Errorf("Expected no missing names; got %v", missing)
	}
	if got[0].Name != "Title" || got[1].Name != "Branch" {
		t.Errorf("Expected order preserved; got %+v", got)
	}
}

func TestFetchFieldsByNames_PartialMissing(t *testing.T) {
	// One name resolves, one fails with a fetcher-level error, one returns
	// "type didn't match" (treated as missing). All three outcomes must be
	// surfaced without dropping the resolved entry.
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			n := string(variables["name"].(graphql.String))
			v := reflect.ValueOf(query).Elem()
			field := v.FieldByName("Node").FieldByName("ProjectV2").FieldByName("Field")
			switch n {
			case "Title":
				field.FieldByName("TypeName").SetString("ProjectV2Field")
				pf := field.FieldByName("ProjectV2Field")
				pf.FieldByName("ID").SetString("id-Title")
				pf.FieldByName("Name").SetString("Title")
				pf.FieldByName("DataType").SetString("TITLE")
				return nil
			case "Iteration":
				// Unsupported type — TypeName won't match either fragment.
				field.FieldByName("TypeName").SetString("ProjectV2IterationField")
				return nil
			case "Broken":
				return errors.New("GraphQL: Something went wrong while executing your query")
			}
			return errors.New("unexpected name")
		},
	}
	client := NewClientWithGraphQL(mock)
	got, missing := client.FetchFieldsByNames("proj-id", []string{"Title", "Iteration", "Broken"})

	if len(got) != 1 || got[0].Name != "Title" {
		t.Errorf("Expected only Title resolved; got %+v", got)
	}
	if len(missing) != 2 {
		t.Fatalf("Expected 2 missing; got %d (%v)", len(missing), missing)
	}
	if missing[0] != "Iteration" || missing[1] != "Broken" {
		t.Errorf("Expected missing names preserved in order; got %v", missing)
	}
}

func TestGetProjectFields_ResolverUnavailableWrapsSentinel(t *testing.T) {
	// When the GraphQL client returns the canonical GitHub 5xx body phrasing
	// AND no cached metadata is available, GetProjectFields surfaces an error
	// that satisfies errors.Is(err, ErrFieldResolverUnavailable). This is the
	// sentinel-propagation contract callers rely on when distinguishing
	// "resolver crashed and we couldn't recover" from other failure modes.
	// Cache loader is stubbed to "no cache" so the fallback path can't engage.
	resetFieldsCacheWarningForTesting()
	restore := SetCachedFieldsLoaderForTesting(func() ([]ProjectField, error) {
		return nil, nil
	})
	defer restore()

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("GraphQL: Something went wrong while executing your query on 2026-05-14T18:07:30Z. Please include `E290:183153:9C0F68F:2723794C:6A060F61` when reporting this issue.")
		},
	}

	client := NewClientWithGraphQL(mock)
	fields, err := client.GetProjectFields("proj-id")

	if err == nil {
		t.Fatal("Expected error when resolver returns 5xx and cache is empty")
	}
	if fields != nil {
		t.Error("Expected nil fields when resolver fails and cache is empty")
	}
	if !errors.Is(err, ErrFieldResolverUnavailable) {
		t.Errorf("Expected errors.Is(err, ErrFieldResolverUnavailable) to be true; got error: %v", err)
	}
	// Original error chain must be preserved through the empty-cache error so
	// users see the trace ID etc.
	if !strings.Contains(err.Error(), "E290:183153") {
		t.Errorf("Expected wrapped error to preserve original message (trace ID); got: %v", err)
	}
}

func TestGetProjectFields_Pagination(t *testing.T) {
	callCount := 0
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectFields" {
				callCount++
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				proj := node.FieldByName("ProjectV2")
				fieldsConn := proj.FieldByName("Fields")
				nodes := fieldsConn.FieldByName("Nodes")
				pageInfo := fieldsConn.FieldByName("PageInfo")
				nodeType := nodes.Type().Elem()

				if callCount == 1 {
					// First page - return field 1, indicate more pages
					newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)
					field1 := reflect.New(nodeType).Elem()
					field1.FieldByName("TypeName").SetString("ProjectV2SingleSelectField")
					selectField := field1.FieldByName("ProjectV2SingleSelectField")
					selectField.FieldByName("ID").SetString("field-1")
					selectField.FieldByName("Name").SetString("Status")
					selectField.FieldByName("DataType").SetString("SINGLE_SELECT")
					newNodes.Index(0).Set(field1)
					nodes.Set(newNodes)
					pageInfo.FieldByName("HasNextPage").SetBool(true)
					pageInfo.FieldByName("EndCursor").SetString("cursor-1")
				} else {
					// Second page - return field 2, no more pages
					newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)
					field2 := reflect.New(nodeType).Elem()
					field2.FieldByName("TypeName").SetString("ProjectV2Field")
					textField := field2.FieldByName("ProjectV2Field")
					textField.FieldByName("ID").SetString("field-2")
					textField.FieldByName("Name").SetString("Title")
					textField.FieldByName("DataType").SetString("TEXT")
					newNodes.Index(0).Set(field2)
					nodes.Set(newNodes)
					pageInfo.FieldByName("HasNextPage").SetBool(false)
					pageInfo.FieldByName("EndCursor").SetString("")
				}
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	fields, err := client.GetProjectFields("proj-id")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 API calls for pagination, got %d", callCount)
	}
	if len(fields) != 2 {
		t.Fatalf("Expected 2 fields from pagination, got %d", len(fields))
	}
	if fields[0].Name != "Status" {
		t.Errorf("Expected first field 'Status', got '%s'", fields[0].Name)
	}
	if fields[1].Name != "Title" {
		t.Errorf("Expected second field 'Title', got '%s'", fields[1].Name)
	}
}

// ============================================================================
// GetIssue Tests - Improved Coverage
// ============================================================================

func TestGetIssue_Success(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetIssue" {
				v := reflect.ValueOf(query).Elem()
				repo := v.FieldByName("Repository")
				issue := repo.FieldByName("Issue")
				issue.FieldByName("ID").SetString("issue-123")
				issue.FieldByName("Number").SetInt(42)
				issue.FieldByName("Title").SetString("Test Issue")
				issue.FieldByName("Body").SetString("Issue body")
				issue.FieldByName("State").SetString("OPEN")
				issue.FieldByName("URL").SetString("https://github.com/owner/repo/issues/42")

				// Set author
				author := issue.FieldByName("Author")
				author.FieldByName("Login").SetString("testuser")

				// Set milestone
				milestone := issue.FieldByName("Milestone")
				milestone.FieldByName("Title").SetString("v1.0")
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	issue, err := client.GetIssue("owner", "repo", 42)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if issue == nil {
		t.Fatal("Expected issue to be returned")
	}
	if issue.ID != "issue-123" {
		t.Errorf("Expected ID 'issue-123', got '%s'", issue.ID)
	}
	if issue.Number != 42 {
		t.Errorf("Expected number 42, got %d", issue.Number)
	}
	if issue.Title != "Test Issue" {
		t.Errorf("Expected title 'Test Issue', got '%s'", issue.Title)
	}
	if issue.Author.Login != "testuser" {
		t.Errorf("Expected author 'testuser', got '%s'", issue.Author.Login)
	}
	if issue.Milestone == nil || issue.Milestone.Title != "v1.0" {
		t.Error("Expected milestone with title 'v1.0'")
	}
	if issue.Repository.Owner != "owner" {
		t.Errorf("Expected repository owner 'owner', got '%s'", issue.Repository.Owner)
	}
}

func TestGetIssue_WithAssignees(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetIssue" {
				v := reflect.ValueOf(query).Elem()
				repo := v.FieldByName("Repository")
				issue := repo.FieldByName("Issue")
				issue.FieldByName("ID").SetString("issue-123")
				issue.FieldByName("Number").SetInt(1)
				issue.FieldByName("Title").SetString("Test")
				issue.FieldByName("State").SetString("OPEN")

				// Set assignees
				assignees := issue.FieldByName("Assignees")
				nodes := assignees.FieldByName("Nodes")
				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 2, 2)

				node1 := reflect.New(nodeType).Elem()
				node1.FieldByName("Login").SetString("user1")
				newNodes.Index(0).Set(node1)

				node2 := reflect.New(nodeType).Elem()
				node2.FieldByName("Login").SetString("user2")
				newNodes.Index(1).Set(node2)

				nodes.Set(newNodes)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	issue, err := client.GetIssue("owner", "repo", 1)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(issue.Assignees) != 2 {
		t.Fatalf("Expected 2 assignees, got %d", len(issue.Assignees))
	}
	if issue.Assignees[0].Login != "user1" {
		t.Errorf("Expected first assignee 'user1', got '%s'", issue.Assignees[0].Login)
	}
}

func TestGetIssue_WithLabels(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetIssue" {
				v := reflect.ValueOf(query).Elem()
				repo := v.FieldByName("Repository")
				issue := repo.FieldByName("Issue")
				issue.FieldByName("ID").SetString("issue-123")
				issue.FieldByName("Number").SetInt(1)
				issue.FieldByName("Title").SetString("Test")
				issue.FieldByName("State").SetString("OPEN")

				// Set labels
				labels := issue.FieldByName("Labels")
				nodes := labels.FieldByName("Nodes")
				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 2, 2)

				node1 := reflect.New(nodeType).Elem()
				node1.FieldByName("Name").SetString("bug")
				node1.FieldByName("Color").SetString("d73a4a")
				newNodes.Index(0).Set(node1)

				node2 := reflect.New(nodeType).Elem()
				node2.FieldByName("Name").SetString("enhancement")
				node2.FieldByName("Color").SetString("a2eeef")
				newNodes.Index(1).Set(node2)

				nodes.Set(newNodes)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	issue, err := client.GetIssue("owner", "repo", 1)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(issue.Labels) != 2 {
		t.Fatalf("Expected 2 labels, got %d", len(issue.Labels))
	}
	if issue.Labels[0].Name != "bug" {
		t.Errorf("Expected first label 'bug', got '%s'", issue.Labels[0].Name)
	}
}

func TestGetIssue_NoMilestone(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetIssue" {
				v := reflect.ValueOf(query).Elem()
				repo := v.FieldByName("Repository")
				issue := repo.FieldByName("Issue")
				issue.FieldByName("ID").SetString("issue-123")
				issue.FieldByName("Number").SetInt(1)
				issue.FieldByName("Title").SetString("Test")
				issue.FieldByName("State").SetString("OPEN")
				// Don't set milestone title - leave empty
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	issue, err := client.GetIssue("owner", "repo", 1)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if issue.Milestone != nil {
		t.Error("Expected nil milestone when not set")
	}
}

// ============================================================================
// GetProjectItems Tests - Improved Coverage
// ============================================================================

func TestGetProjectItems_WithItems(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)
				newNode := reflect.New(nodeType).Elem()

				newNode.FieldByName("ID").SetString("item-1")

				// Set content
				content := newNode.FieldByName("Content")
				content.FieldByName("TypeName").SetString("Issue")

				issueContent := content.FieldByName("Issue")
				issueContent.FieldByName("ID").SetString("issue-123")
				issueContent.FieldByName("Number").SetInt(42)
				issueContent.FieldByName("Title").SetString("Test Issue")
				issueContent.FieldByName("State").SetString("OPEN")
				issueContent.FieldByName("URL").SetString("https://github.com/owner/repo/issues/42")

				// Set repository
				issueRepo := issueContent.FieldByName("Repository")
				issueRepo.FieldByName("NameWithOwner").SetString("owner/repo")

				newNodes.Index(0).Set(newNode)
				nodes.Set(newNodes)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItems("proj-id", nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
	if items[0].Issue == nil {
		t.Fatal("Expected issue in item")
	}
	if items[0].Issue.Number != 42 {
		t.Errorf("Expected issue number 42, got %d", items[0].Issue.Number)
	}
	if items[0].Issue.Repository.Owner != "owner" {
		t.Errorf("Expected repository owner 'owner', got '%s'", items[0].Issue.Repository.Owner)
	}
}

func TestGetProjectItems_WithFilter(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 2, 2)

				// Item 1 - matches filter
				node1 := reflect.New(nodeType).Elem()
				node1.FieldByName("ID").SetString("item-1")
				content1 := node1.FieldByName("Content")
				content1.FieldByName("TypeName").SetString("Issue")
				issue1 := content1.FieldByName("Issue")
				issue1.FieldByName("ID").SetString("issue-1")
				issue1.FieldByName("Number").SetInt(1)
				issue1.FieldByName("Title").SetString("Match")
				issue1.FieldByName("State").SetString("OPEN")
				repo1 := issue1.FieldByName("Repository")
				repo1.FieldByName("NameWithOwner").SetString("owner/repo")
				newNodes.Index(0).Set(node1)

				// Item 2 - doesn't match filter
				node2 := reflect.New(nodeType).Elem()
				node2.FieldByName("ID").SetString("item-2")
				content2 := node2.FieldByName("Content")
				content2.FieldByName("TypeName").SetString("Issue")
				issue2 := content2.FieldByName("Issue")
				issue2.FieldByName("ID").SetString("issue-2")
				issue2.FieldByName("Number").SetInt(2)
				issue2.FieldByName("Title").SetString("No Match")
				issue2.FieldByName("State").SetString("OPEN")
				repo2 := issue2.FieldByName("Repository")
				repo2.FieldByName("NameWithOwner").SetString("other/repo")
				newNodes.Index(1).Set(node2)

				nodes.Set(newNodes)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItems("proj-id", &ProjectItemsFilter{Repository: "owner/repo"})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Expected 1 item after filter, got %d", len(items))
	}
	if items[0].Issue.Title != "Match" {
		t.Errorf("Expected issue title 'Match', got '%s'", items[0].Issue.Title)
	}
}

func TestGetProjectItems_WithStateFilter(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 3, 3)

				// Item 1 - OPEN issue
				node1 := reflect.New(nodeType).Elem()
				node1.FieldByName("ID").SetString("item-1")
				content1 := node1.FieldByName("Content")
				content1.FieldByName("TypeName").SetString("Issue")
				issue1 := content1.FieldByName("Issue")
				issue1.FieldByName("ID").SetString("issue-1")
				issue1.FieldByName("Number").SetInt(1)
				issue1.FieldByName("Title").SetString("Open Issue")
				issue1.FieldByName("State").SetString("OPEN")
				repo1 := issue1.FieldByName("Repository")
				repo1.FieldByName("NameWithOwner").SetString("owner/repo")
				newNodes.Index(0).Set(node1)

				// Item 2 - CLOSED issue
				node2 := reflect.New(nodeType).Elem()
				node2.FieldByName("ID").SetString("item-2")
				content2 := node2.FieldByName("Content")
				content2.FieldByName("TypeName").SetString("Issue")
				issue2 := content2.FieldByName("Issue")
				issue2.FieldByName("ID").SetString("issue-2")
				issue2.FieldByName("Number").SetInt(2)
				issue2.FieldByName("Title").SetString("Closed Issue")
				issue2.FieldByName("State").SetString("CLOSED")
				repo2 := issue2.FieldByName("Repository")
				repo2.FieldByName("NameWithOwner").SetString("owner/repo")
				newNodes.Index(1).Set(node2)

				// Item 3 - Another OPEN issue
				node3 := reflect.New(nodeType).Elem()
				node3.FieldByName("ID").SetString("item-3")
				content3 := node3.FieldByName("Content")
				content3.FieldByName("TypeName").SetString("Issue")
				issue3 := content3.FieldByName("Issue")
				issue3.FieldByName("ID").SetString("issue-3")
				issue3.FieldByName("Number").SetInt(3)
				issue3.FieldByName("Title").SetString("Another Open")
				issue3.FieldByName("State").SetString("OPEN")
				repo3 := issue3.FieldByName("Repository")
				repo3.FieldByName("NameWithOwner").SetString("owner/repo")
				newNodes.Index(2).Set(node3)

				nodes.Set(newNodes)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	openState := "OPEN"
	items, err := client.GetProjectItems("proj-id", &ProjectItemsFilter{State: &openState})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Expected 2 open items, got %d", len(items))
	}
	if items[0].Issue.Title != "Open Issue" {
		t.Errorf("Expected first issue title 'Open Issue', got '%s'", items[0].Issue.Title)
	}
	if items[1].Issue.Title != "Another Open" {
		t.Errorf("Expected second issue title 'Another Open', got '%s'", items[1].Issue.Title)
	}
}

func TestGetProjectItems_SkipsNonIssues(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 2, 2)

				// Item 1 - Draft issue (should be skipped)
				node1 := reflect.New(nodeType).Elem()
				node1.FieldByName("ID").SetString("item-1")
				content1 := node1.FieldByName("Content")
				content1.FieldByName("TypeName").SetString("DraftIssue")
				newNodes.Index(0).Set(node1)

				// Item 2 - Real issue
				node2 := reflect.New(nodeType).Elem()
				node2.FieldByName("ID").SetString("item-2")
				content2 := node2.FieldByName("Content")
				content2.FieldByName("TypeName").SetString("Issue")
				issue2 := content2.FieldByName("Issue")
				issue2.FieldByName("ID").SetString("issue-2")
				issue2.FieldByName("Number").SetInt(2)
				issue2.FieldByName("Title").SetString("Real Issue")
				issue2.FieldByName("State").SetString("OPEN")
				repo2 := issue2.FieldByName("Repository")
				repo2.FieldByName("NameWithOwner").SetString("owner/repo")
				newNodes.Index(1).Set(node2)

				nodes.Set(newNodes)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItems("proj-id", nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Expected 1 item (draft skipped), got %d", len(items))
	}
	if items[0].Issue.Title != "Real Issue" {
		t.Errorf("Expected 'Real Issue', got '%s'", items[0].Issue.Title)
	}
}

func TestGetProjectItems_WithFieldValues(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)
				newNode := reflect.New(nodeType).Elem()

				newNode.FieldByName("ID").SetString("item-1")
				content := newNode.FieldByName("Content")
				content.FieldByName("TypeName").SetString("Issue")
				issue := content.FieldByName("Issue")
				issue.FieldByName("ID").SetString("issue-1")
				issue.FieldByName("Number").SetInt(1)
				issue.FieldByName("Title").SetString("Test")
				issue.FieldByName("State").SetString("OPEN")
				repo := issue.FieldByName("Repository")
				repo.FieldByName("NameWithOwner").SetString("owner/repo")

				// Set field values
				fieldValues := newNode.FieldByName("FieldValues")
				fvNodes := fieldValues.FieldByName("Nodes")
				fvNodeType := fvNodes.Type().Elem()
				newFvNodes := reflect.MakeSlice(fvNodes.Type(), 2, 2)

				// Single select field value
				fv1 := reflect.New(fvNodeType).Elem()
				fv1.FieldByName("TypeName").SetString("ProjectV2ItemFieldSingleSelectValue")
				singleSelect := fv1.FieldByName("ProjectV2ItemFieldSingleSelectValue")
				singleSelect.FieldByName("Name").SetString("In Progress")
				singleSelectField := singleSelect.FieldByName("Field")
				singleSelectFieldInner := singleSelectField.FieldByName("ProjectV2SingleSelectField")
				singleSelectFieldInner.FieldByName("Name").SetString("Status")
				newFvNodes.Index(0).Set(fv1)

				// Text field value
				fv2 := reflect.New(fvNodeType).Elem()
				fv2.FieldByName("TypeName").SetString("ProjectV2ItemFieldTextValue")
				textValue := fv2.FieldByName("ProjectV2ItemFieldTextValue")
				textValue.FieldByName("Text").SetString("Some notes")
				textField := textValue.FieldByName("Field")
				textFieldInner := textField.FieldByName("ProjectV2Field")
				textFieldInner.FieldByName("Name").SetString("Notes")
				newFvNodes.Index(1).Set(fv2)

				fvNodes.Set(newFvNodes)
				newNodes.Index(0).Set(newNode)
				nodes.Set(newNodes)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItems("proj-id", nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
	if len(items[0].FieldValues) != 2 {
		t.Fatalf("Expected 2 field values, got %d", len(items[0].FieldValues))
	}

	// Check Status field
	foundStatus := false
	foundNotes := false
	for _, fv := range items[0].FieldValues {
		if fv.Field == "Status" && fv.Value == "In Progress" {
			foundStatus = true
		}
		if fv.Field == "Notes" && fv.Value == "Some notes" {
			foundNotes = true
		}
	}
	if !foundStatus {
		t.Error("Expected Status field with value 'In Progress'")
	}
	if !foundNotes {
		t.Error("Expected Notes field with value 'Some notes'")
	}
}

func TestGetProjectItems_WithAssignees(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)
				newNode := reflect.New(nodeType).Elem()

				newNode.FieldByName("ID").SetString("item-1")
				content := newNode.FieldByName("Content")
				content.FieldByName("TypeName").SetString("Issue")
				issue := content.FieldByName("Issue")
				issue.FieldByName("ID").SetString("issue-1")
				issue.FieldByName("Number").SetInt(1)
				issue.FieldByName("Title").SetString("Test")
				issue.FieldByName("State").SetString("OPEN")
				repo := issue.FieldByName("Repository")
				repo.FieldByName("NameWithOwner").SetString("owner/repo")

				// Set assignees
				assignees := issue.FieldByName("Assignees")
				assigneeNodes := assignees.FieldByName("Nodes")
				assigneeNodeType := assigneeNodes.Type().Elem()
				newAssigneeNodes := reflect.MakeSlice(assigneeNodes.Type(), 1, 1)
				assignee := reflect.New(assigneeNodeType).Elem()
				assignee.FieldByName("Login").SetString("testuser")
				newAssigneeNodes.Index(0).Set(assignee)
				assigneeNodes.Set(newAssigneeNodes)

				newNodes.Index(0).Set(newNode)
				nodes.Set(newNodes)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItems("proj-id", nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
	if len(items[0].Issue.Assignees) != 1 {
		t.Fatalf("Expected 1 assignee, got %d", len(items[0].Issue.Assignees))
	}
	if items[0].Issue.Assignees[0].Login != "testuser" {
		t.Errorf("Expected assignee 'testuser', got '%s'", items[0].Issue.Assignees[0].Login)
	}
}

func TestGetProjectItems_WithLabels(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)
				newNode := reflect.New(nodeType).Elem()

				newNode.FieldByName("ID").SetString("item-1")
				content := newNode.FieldByName("Content")
				content.FieldByName("TypeName").SetString("Issue")
				issue := content.FieldByName("Issue")
				issue.FieldByName("ID").SetString("issue-1")
				issue.FieldByName("Number").SetInt(1)
				issue.FieldByName("Title").SetString("Test")
				issue.FieldByName("State").SetString("OPEN")
				repo := issue.FieldByName("Repository")
				repo.FieldByName("NameWithOwner").SetString("owner/repo")

				// Set labels
				labels := issue.FieldByName("Labels")
				labelNodes := labels.FieldByName("Nodes")
				labelNodeType := labelNodes.Type().Elem()
				newLabelNodes := reflect.MakeSlice(labelNodes.Type(), 2, 2)
				label1 := reflect.New(labelNodeType).Elem()
				label1.FieldByName("Name").SetString("epic")
				label2 := reflect.New(labelNodeType).Elem()
				label2.FieldByName("Name").SetString("bug")
				newLabelNodes.Index(0).Set(label1)
				newLabelNodes.Index(1).Set(label2)
				labelNodes.Set(newLabelNodes)

				newNodes.Index(0).Set(newNode)
				nodes.Set(newNodes)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItems("proj-id", nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
	if len(items[0].Issue.Labels) != 2 {
		t.Fatalf("Expected 2 labels, got %d", len(items[0].Issue.Labels))
	}
	if items[0].Issue.Labels[0].Name != "epic" {
		t.Errorf("Expected first label 'epic', got '%s'", items[0].Issue.Labels[0].Name)
	}
	if items[0].Issue.Labels[1].Name != "bug" {
		t.Errorf("Expected second label 'bug', got '%s'", items[0].Issue.Labels[1].Name)
	}
}

// ============================================================================
// GetSubIssues Tests - Improved Coverage
// ============================================================================

func TestGetSubIssues_Success(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetSubIssues" {
				v := reflect.ValueOf(query).Elem()
				repo := v.FieldByName("Repository")
				issue := repo.FieldByName("Issue")
				subIssues := issue.FieldByName("SubIssues")
				nodes := subIssues.FieldByName("Nodes")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 2, 2)

				// Sub-issue 1
				node1 := reflect.New(nodeType).Elem()
				node1.FieldByName("ID").SetString("sub-1")
				node1.FieldByName("Number").SetInt(10)
				node1.FieldByName("Title").SetString("Sub-issue 1")
				node1.FieldByName("State").SetString("OPEN")
				node1.FieldByName("URL").SetString("https://github.com/owner/repo/issues/10")
				repo1 := node1.FieldByName("Repository")
				repo1.FieldByName("Name").SetString("repo")
				owner1 := repo1.FieldByName("Owner")
				owner1.FieldByName("Login").SetString("owner")
				newNodes.Index(0).Set(node1)

				// Sub-issue 2
				node2 := reflect.New(nodeType).Elem()
				node2.FieldByName("ID").SetString("sub-2")
				node2.FieldByName("Number").SetInt(11)
				node2.FieldByName("Title").SetString("Sub-issue 2")
				node2.FieldByName("State").SetString("CLOSED")
				node2.FieldByName("URL").SetString("https://github.com/owner/repo/issues/11")
				repo2 := node2.FieldByName("Repository")
				repo2.FieldByName("Name").SetString("repo")
				owner2 := repo2.FieldByName("Owner")
				owner2.FieldByName("Login").SetString("owner")
				newNodes.Index(1).Set(node2)

				nodes.Set(newNodes)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	subIssues, err := client.GetSubIssues("owner", "repo", 1)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(subIssues) != 2 {
		t.Fatalf("Expected 2 sub-issues, got %d", len(subIssues))
	}
	if subIssues[0].Number != 10 {
		t.Errorf("Expected first sub-issue number 10, got %d", subIssues[0].Number)
	}
	if subIssues[1].State != "CLOSED" {
		t.Errorf("Expected second sub-issue state 'CLOSED', got '%s'", subIssues[1].State)
	}
}

func TestGetSubIssues_Pagination(t *testing.T) {
	callCount := 0
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetSubIssues" {
				callCount++
				v := reflect.ValueOf(query).Elem()
				repo := v.FieldByName("Repository")
				issue := repo.FieldByName("Issue")
				subIssues := issue.FieldByName("SubIssues")
				nodes := subIssues.FieldByName("Nodes")
				pageInfo := subIssues.FieldByName("PageInfo")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)

				node := reflect.New(nodeType).Elem()
				if callCount == 1 {
					// First page
					node.FieldByName("ID").SetString("sub-1")
					node.FieldByName("Number").SetInt(10)
					node.FieldByName("Title").SetString("Sub-issue 1")
					node.FieldByName("State").SetString("OPEN")
					node.FieldByName("URL").SetString("https://github.com/owner/repo/issues/10")
					repoField := node.FieldByName("Repository")
					repoField.FieldByName("Name").SetString("repo")
					ownerField := repoField.FieldByName("Owner")
					ownerField.FieldByName("Login").SetString("owner")
					pageInfo.FieldByName("HasNextPage").SetBool(true)
					pageInfo.FieldByName("EndCursor").SetString("cursor1")
				} else {
					// Second page
					node.FieldByName("ID").SetString("sub-2")
					node.FieldByName("Number").SetInt(11)
					node.FieldByName("Title").SetString("Sub-issue 2")
					node.FieldByName("State").SetString("CLOSED")
					node.FieldByName("URL").SetString("https://github.com/owner/repo/issues/11")
					repoField := node.FieldByName("Repository")
					repoField.FieldByName("Name").SetString("repo")
					ownerField := repoField.FieldByName("Owner")
					ownerField.FieldByName("Login").SetString("owner")
					pageInfo.FieldByName("HasNextPage").SetBool(false)
					pageInfo.FieldByName("EndCursor").SetString("")
				}
				newNodes.Index(0).Set(node)
				nodes.Set(newNodes)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	subIssues, err := client.GetSubIssues("owner", "repo", 1)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 API calls for pagination, got %d", callCount)
	}
	if len(subIssues) != 2 {
		t.Fatalf("Expected 2 sub-issues from pagination, got %d", len(subIssues))
	}
	if subIssues[0].Number != 10 {
		t.Errorf("Expected first sub-issue number 10, got %d", subIssues[0].Number)
	}
	if subIssues[1].Number != 11 {
		t.Errorf("Expected second sub-issue number 11, got %d", subIssues[1].Number)
	}
}

// ============================================================================
// GetRepositoryIssues Tests - Improved Coverage
// ============================================================================

func TestGetRepositoryIssues_Success(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetRepositoryIssues" {
				v := reflect.ValueOf(query).Elem()
				repo := v.FieldByName("Repository")
				issues := repo.FieldByName("Issues")
				nodes := issues.FieldByName("Nodes")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 2, 2)

				node1 := reflect.New(nodeType).Elem()
				node1.FieldByName("ID").SetString("issue-1")
				node1.FieldByName("Number").SetInt(1)
				node1.FieldByName("Title").SetString("First Issue")
				node1.FieldByName("State").SetString("OPEN")
				node1.FieldByName("URL").SetString("https://github.com/owner/repo/issues/1")
				newNodes.Index(0).Set(node1)

				node2 := reflect.New(nodeType).Elem()
				node2.FieldByName("ID").SetString("issue-2")
				node2.FieldByName("Number").SetInt(2)
				node2.FieldByName("Title").SetString("Second Issue")
				node2.FieldByName("State").SetString("CLOSED")
				node2.FieldByName("URL").SetString("https://github.com/owner/repo/issues/2")
				newNodes.Index(1).Set(node2)

				nodes.Set(newNodes)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	issues, err := client.GetRepositoryIssues("owner", "repo", "all")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("Expected 2 issues, got %d", len(issues))
	}
	if issues[0].Title != "First Issue" {
		t.Errorf("Expected first issue title 'First Issue', got '%s'", issues[0].Title)
	}
	if issues[0].Repository.Owner != "owner" {
		t.Errorf("Expected repository owner 'owner', got '%s'", issues[0].Repository.Owner)
	}
}

func TestGetRepositoryIssues_Pagination(t *testing.T) {
	// Track which page we're on
	callCount := 0

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetRepositoryIssues" {
				callCount++
				v := reflect.ValueOf(query).Elem()
				repo := v.FieldByName("Repository")
				issues := repo.FieldByName("Issues")
				nodes := issues.FieldByName("Nodes")
				pageInfoField := issues.FieldByName("PageInfo")

				nodeType := nodes.Type().Elem()

				if callCount == 1 {
					// First page - return issues 1-2 with hasNextPage=true
					newNodes := reflect.MakeSlice(nodes.Type(), 2, 2)

					node1 := reflect.New(nodeType).Elem()
					node1.FieldByName("ID").SetString("issue-1")
					node1.FieldByName("Number").SetInt(1)
					node1.FieldByName("Title").SetString("Issue 1")
					node1.FieldByName("State").SetString("OPEN")
					node1.FieldByName("URL").SetString("https://github.com/owner/repo/issues/1")
					newNodes.Index(0).Set(node1)

					node2 := reflect.New(nodeType).Elem()
					node2.FieldByName("ID").SetString("issue-2")
					node2.FieldByName("Number").SetInt(2)
					node2.FieldByName("Title").SetString("Issue 2")
					node2.FieldByName("State").SetString("OPEN")
					node2.FieldByName("URL").SetString("https://github.com/owner/repo/issues/2")
					newNodes.Index(1).Set(node2)

					nodes.Set(newNodes)

					// Set PageInfo for first page
					pageInfoField.FieldByName("HasNextPage").SetBool(true)
					pageInfoField.FieldByName("EndCursor").SetString("cursor-1")
				} else if callCount == 2 {
					// Second page - return issue 3 with hasNextPage=false
					newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)

					node3 := reflect.New(nodeType).Elem()
					node3.FieldByName("ID").SetString("issue-3")
					node3.FieldByName("Number").SetInt(3)
					node3.FieldByName("Title").SetString("Issue 3")
					node3.FieldByName("State").SetString("OPEN")
					node3.FieldByName("URL").SetString("https://github.com/owner/repo/issues/3")
					newNodes.Index(0).Set(node3)

					nodes.Set(newNodes)

					// Set PageInfo for last page
					pageInfoField.FieldByName("HasNextPage").SetBool(false)
					pageInfoField.FieldByName("EndCursor").SetString("")
				}
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	issues, err := client.GetRepositoryIssues("owner", "repo", "open")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 API calls for pagination, got %d", callCount)
	}
	if len(issues) != 3 {
		t.Fatalf("Expected 3 issues from 2 pages, got %d", len(issues))
	}
	if issues[0].Title != "Issue 1" {
		t.Errorf("Expected first issue title 'Issue 1', got '%s'", issues[0].Title)
	}
	if issues[2].Title != "Issue 3" {
		t.Errorf("Expected third issue title 'Issue 3', got '%s'", issues[2].Title)
	}
}

// ============================================================================
// GetProjectItems Pagination Tests
// ============================================================================

func TestGetProjectItems_Pagination_MultiplePages(t *testing.T) {
	// Track which page we're on
	callCount := 0

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				callCount++
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")
				pageInfoField := items.FieldByName("PageInfo")

				nodeType := nodes.Type().Elem()

				if callCount == 1 {
					// First page - return items 1-2 with hasNextPage=true
					newNodes := reflect.MakeSlice(nodes.Type(), 2, 2)

					node1 := reflect.New(nodeType).Elem()
					node1.FieldByName("ID").SetString("item-1")
					content1 := node1.FieldByName("Content")
					content1.FieldByName("TypeName").SetString("Issue")
					issue1 := content1.FieldByName("Issue")
					issue1.FieldByName("ID").SetString("issue-1")
					issue1.FieldByName("Number").SetInt(1)
					issue1.FieldByName("Title").SetString("Issue 1")
					issue1.FieldByName("State").SetString("OPEN")
					repo1 := issue1.FieldByName("Repository")
					repo1.FieldByName("NameWithOwner").SetString("owner/repo")
					newNodes.Index(0).Set(node1)

					node2 := reflect.New(nodeType).Elem()
					node2.FieldByName("ID").SetString("item-2")
					content2 := node2.FieldByName("Content")
					content2.FieldByName("TypeName").SetString("Issue")
					issue2 := content2.FieldByName("Issue")
					issue2.FieldByName("ID").SetString("issue-2")
					issue2.FieldByName("Number").SetInt(2)
					issue2.FieldByName("Title").SetString("Issue 2")
					issue2.FieldByName("State").SetString("OPEN")
					repo2 := issue2.FieldByName("Repository")
					repo2.FieldByName("NameWithOwner").SetString("owner/repo")
					newNodes.Index(1).Set(node2)

					nodes.Set(newNodes)

					// Set pagination info - more pages available
					pageInfoField.FieldByName("HasNextPage").SetBool(true)
					pageInfoField.FieldByName("EndCursor").SetString("cursor-page-1")
				} else if callCount == 2 {
					// Second page - return item 3 with hasNextPage=false
					newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)

					node3 := reflect.New(nodeType).Elem()
					node3.FieldByName("ID").SetString("item-3")
					content3 := node3.FieldByName("Content")
					content3.FieldByName("TypeName").SetString("Issue")
					issue3 := content3.FieldByName("Issue")
					issue3.FieldByName("ID").SetString("issue-3")
					issue3.FieldByName("Number").SetInt(3)
					issue3.FieldByName("Title").SetString("Issue 3")
					issue3.FieldByName("State").SetString("OPEN")
					repo3 := issue3.FieldByName("Repository")
					repo3.FieldByName("NameWithOwner").SetString("owner/repo")
					newNodes.Index(0).Set(node3)

					nodes.Set(newNodes)

					// Set pagination info - no more pages
					pageInfoField.FieldByName("HasNextPage").SetBool(false)
					pageInfoField.FieldByName("EndCursor").SetString("")
				}
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItems("proj-id", nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 API calls for pagination, got %d", callCount)
	}
	if len(items) != 3 {
		t.Fatalf("Expected 3 items from 2 pages, got %d", len(items))
	}
	if items[0].Issue.Number != 1 {
		t.Errorf("Expected first issue number 1, got %d", items[0].Issue.Number)
	}
	if items[2].Issue.Number != 3 {
		t.Errorf("Expected third issue number 3, got %d", items[2].Issue.Number)
	}
}

func TestGetProjectItems_Pagination_SinglePage(t *testing.T) {
	callCount := 0

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				callCount++
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")
				pageInfoField := items.FieldByName("PageInfo")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)

				node1 := reflect.New(nodeType).Elem()
				node1.FieldByName("ID").SetString("item-1")
				content1 := node1.FieldByName("Content")
				content1.FieldByName("TypeName").SetString("Issue")
				issue1 := content1.FieldByName("Issue")
				issue1.FieldByName("ID").SetString("issue-1")
				issue1.FieldByName("Number").SetInt(1)
				issue1.FieldByName("Title").SetString("Only Issue")
				issue1.FieldByName("State").SetString("OPEN")
				repo1 := issue1.FieldByName("Repository")
				repo1.FieldByName("NameWithOwner").SetString("owner/repo")
				newNodes.Index(0).Set(node1)

				nodes.Set(newNodes)

				// No more pages
				pageInfoField.FieldByName("HasNextPage").SetBool(false)
				pageInfoField.FieldByName("EndCursor").SetString("")
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItems("proj-id", nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 API call (single page), got %d", callCount)
	}
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
}

func TestGetProjectItems_Pagination_CursorPropagation(t *testing.T) {
	var receivedCursors []interface{}

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				// Track the cursor value passed
				receivedCursors = append(receivedCursors, variables["cursor"])

				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")
				pageInfoField := items.FieldByName("PageInfo")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)

				node1 := reflect.New(nodeType).Elem()
				node1.FieldByName("ID").SetString("item-1")
				content1 := node1.FieldByName("Content")
				content1.FieldByName("TypeName").SetString("Issue")
				issue1 := content1.FieldByName("Issue")
				issue1.FieldByName("ID").SetString("issue-1")
				issue1.FieldByName("Number").SetInt(1)
				issue1.FieldByName("Title").SetString("Issue")
				issue1.FieldByName("State").SetString("OPEN")
				repo1 := issue1.FieldByName("Repository")
				repo1.FieldByName("NameWithOwner").SetString("owner/repo")
				newNodes.Index(0).Set(node1)

				nodes.Set(newNodes)

				// Return different cursors based on call
				if len(receivedCursors) == 1 {
					pageInfoField.FieldByName("HasNextPage").SetBool(true)
					pageInfoField.FieldByName("EndCursor").SetString("expected-cursor-123")
				} else {
					pageInfoField.FieldByName("HasNextPage").SetBool(false)
					pageInfoField.FieldByName("EndCursor").SetString("")
				}
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	_, err := client.GetProjectItems("proj-id", nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(receivedCursors) != 2 {
		t.Fatalf("Expected 2 calls, got %d", len(receivedCursors))
	}

	// First call should have nil cursor (the nil pointer type)
	if receivedCursors[0] != nil {
		// Check if it's a typed nil
		rv := reflect.ValueOf(receivedCursors[0])
		if !rv.IsNil() {
			t.Errorf("First call should have nil cursor, got %v", receivedCursors[0])
		}
	}

	// Second call should have the cursor from first page
	// The cursor is passed as graphql.String which is a string type alias
	cursorVal := reflect.ValueOf(receivedCursors[1])
	if cursorVal.Kind() == reflect.String {
		if cursorVal.String() != "expected-cursor-123" {
			t.Errorf("Second call should have cursor 'expected-cursor-123', got %v", receivedCursors[1])
		}
	} else {
		t.Errorf("Second call cursor should be string type, got %T: %v", receivedCursors[1], receivedCursors[1])
	}
}

func TestGetProjectItems_Pagination_ErrorOnSecondPage(t *testing.T) {
	callCount := 0

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				callCount++

				if callCount == 2 {
					return errors.New("API error on second page")
				}

				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")
				pageInfoField := items.FieldByName("PageInfo")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)

				node1 := reflect.New(nodeType).Elem()
				node1.FieldByName("ID").SetString("item-1")
				content1 := node1.FieldByName("Content")
				content1.FieldByName("TypeName").SetString("Issue")
				issue1 := content1.FieldByName("Issue")
				issue1.FieldByName("ID").SetString("issue-1")
				issue1.FieldByName("Number").SetInt(1)
				issue1.FieldByName("Title").SetString("Issue")
				issue1.FieldByName("State").SetString("OPEN")
				repo1 := issue1.FieldByName("Repository")
				repo1.FieldByName("NameWithOwner").SetString("owner/repo")
				newNodes.Index(0).Set(node1)

				nodes.Set(newNodes)

				// Indicate there's another page
				pageInfoField.FieldByName("HasNextPage").SetBool(true)
				pageInfoField.FieldByName("EndCursor").SetString("cursor-1")
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItems("proj-id", nil)

	if err == nil {
		t.Fatal("Expected error when second page fails")
	}
	if !strings.Contains(err.Error(), "API error on second page") {
		t.Errorf("Expected error message about second page, got: %v", err)
	}
	if items != nil {
		t.Errorf("Expected nil items on error, got %d items", len(items))
	}
}

func TestGetProjectItems_Pagination_WithFilter(t *testing.T) {
	callCount := 0

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				callCount++
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")
				pageInfoField := items.FieldByName("PageInfo")

				nodeType := nodes.Type().Elem()

				if callCount == 1 {
					// First page - 2 items, one matches filter
					newNodes := reflect.MakeSlice(nodes.Type(), 2, 2)

					node1 := reflect.New(nodeType).Elem()
					node1.FieldByName("ID").SetString("item-1")
					content1 := node1.FieldByName("Content")
					content1.FieldByName("TypeName").SetString("Issue")
					issue1 := content1.FieldByName("Issue")
					issue1.FieldByName("ID").SetString("issue-1")
					issue1.FieldByName("Number").SetInt(1)
					issue1.FieldByName("Title").SetString("Match 1")
					issue1.FieldByName("State").SetString("OPEN")
					repo1 := issue1.FieldByName("Repository")
					repo1.FieldByName("NameWithOwner").SetString("target/repo")
					newNodes.Index(0).Set(node1)

					node2 := reflect.New(nodeType).Elem()
					node2.FieldByName("ID").SetString("item-2")
					content2 := node2.FieldByName("Content")
					content2.FieldByName("TypeName").SetString("Issue")
					issue2 := content2.FieldByName("Issue")
					issue2.FieldByName("ID").SetString("issue-2")
					issue2.FieldByName("Number").SetInt(2)
					issue2.FieldByName("Title").SetString("Other Repo")
					issue2.FieldByName("State").SetString("OPEN")
					repo2 := issue2.FieldByName("Repository")
					repo2.FieldByName("NameWithOwner").SetString("other/repo")
					newNodes.Index(1).Set(node2)

					nodes.Set(newNodes)
					pageInfoField.FieldByName("HasNextPage").SetBool(true)
					pageInfoField.FieldByName("EndCursor").SetString("cursor-1")
				} else {
					// Second page - 1 item matching filter
					newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)

					node3 := reflect.New(nodeType).Elem()
					node3.FieldByName("ID").SetString("item-3")
					content3 := node3.FieldByName("Content")
					content3.FieldByName("TypeName").SetString("Issue")
					issue3 := content3.FieldByName("Issue")
					issue3.FieldByName("ID").SetString("issue-3")
					issue3.FieldByName("Number").SetInt(3)
					issue3.FieldByName("Title").SetString("Match 2")
					issue3.FieldByName("State").SetString("OPEN")
					repo3 := issue3.FieldByName("Repository")
					repo3.FieldByName("NameWithOwner").SetString("target/repo")
					newNodes.Index(0).Set(node3)

					nodes.Set(newNodes)
					pageInfoField.FieldByName("HasNextPage").SetBool(false)
					pageInfoField.FieldByName("EndCursor").SetString("")
				}
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItems("proj-id", &ProjectItemsFilter{Repository: "target/repo"})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Expected 2 items matching filter across pages, got %d", len(items))
	}
	if items[0].Issue.Title != "Match 1" {
		t.Errorf("Expected first item 'Match 1', got '%s'", items[0].Issue.Title)
	}
	if items[1].Issue.Title != "Match 2" {
		t.Errorf("Expected second item 'Match 2', got '%s'", items[1].Issue.Title)
	}
}

// ============================================================================
// SearchRepositoryIssues Tests
// ============================================================================

func TestSearchRepositoryIssues_QueryBuilding(t *testing.T) {
	tests := []struct {
		name           string
		filters        SearchFilters
		expectedParts  []string
		unexpectedPart string
	}{
		{
			name:          "default state is open",
			filters:       SearchFilters{},
			expectedParts: []string{"repo:owner/repo", "is:issue", "is:open"},
		},
		{
			name:          "explicit open state",
			filters:       SearchFilters{State: "open"},
			expectedParts: []string{"repo:owner/repo", "is:issue", "is:open"},
		},
		{
			name:           "closed state",
			filters:        SearchFilters{State: "closed"},
			expectedParts:  []string{"repo:owner/repo", "is:issue", "is:closed"},
			unexpectedPart: "is:open",
		},
		{
			name:           "all state - no state filter",
			filters:        SearchFilters{State: "all"},
			expectedParts:  []string{"repo:owner/repo", "is:issue"},
			unexpectedPart: "is:open",
		},
		{
			name:          "with labels",
			filters:       SearchFilters{Labels: []string{"bug", "urgent"}},
			expectedParts: []string{"repo:owner/repo", "is:issue", "label:\"bug\"", "label:\"urgent\""},
		},
		{
			name:          "with assignee",
			filters:       SearchFilters{Assignee: "alice"},
			expectedParts: []string{"repo:owner/repo", "is:issue", "assignee:alice"},
		},
		{
			name:          "with search text",
			filters:       SearchFilters{Search: "login error"},
			expectedParts: []string{"repo:owner/repo", "is:issue", "login error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedQuery string
			mock := &queryMockClient{
				queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
					if name == "SearchIssues" {
						if q, ok := variables["query"]; ok {
							capturedQuery = string(q.(graphql.String))
						}
					}
					return nil
				},
			}

			client := NewClientWithGraphQL(mock)
			_, _ = client.SearchRepositoryIssues("owner", "repo", tt.filters, 0)

			for _, part := range tt.expectedParts {
				if !strings.Contains(capturedQuery, part) {
					t.Errorf("Expected query to contain %q, got: %s", part, capturedQuery)
				}
			}
			if tt.unexpectedPart != "" && strings.Contains(capturedQuery, tt.unexpectedPart) {
				t.Errorf("Expected query NOT to contain %q, got: %s", tt.unexpectedPart, capturedQuery)
			}
		})
	}
}

func TestSearchRepositoryIssues_QueryError(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("search failed")
		},
	}

	client := NewClientWithGraphQL(mock)
	issues, err := client.SearchRepositoryIssues("owner", "repo", SearchFilters{}, 0)

	if err == nil {
		t.Fatal("Expected error when query fails")
	}
	if issues != nil {
		t.Error("Expected nil issues when error occurs")
	}
	if !strings.Contains(err.Error(), "failed to search issues") {
		t.Errorf("Expected 'failed to search issues' error, got: %v", err)
	}
}

// TestSearchRepositoryIssues_RejectsInvalidOwner (#835) asserts that owner/
// repo inputs are validated before being interpolated into the GitHub search
// query string. Previously `fmt.Sprintf("repo:%s/%s", owner, repo)` would
// silently accept whitespace or colons and inject extra search qualifiers.
func TestSearchRepositoryIssues_RejectsInvalidOwner(t *testing.T) {
	var called bool
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			called = true
			return nil
		},
	}
	client := NewClientWithGraphQL(mock)

	_, err := client.SearchRepositoryIssues("foo is:closed", "repo", SearchFilters{}, 0)
	if err == nil {
		t.Fatal("expected validator to reject owner with whitespace")
	}
	if !strings.Contains(err.Error(), "owner contains invalid characters") {
		t.Errorf("expected validator error mentioning owner, got: %v", err)
	}
	if called {
		t.Error("graphql Query should not be invoked when validation fails")
	}
}

// TestSearchRepositoryIssues_RejectsInvalidRepo covers the mirror case.
func TestSearchRepositoryIssues_RejectsInvalidRepo(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return nil
		},
	}
	client := NewClientWithGraphQL(mock)

	_, err := client.SearchRepositoryIssues("owner", "bar author:someone", SearchFilters{}, 0)
	if err == nil {
		t.Fatal("expected validator to reject repo with whitespace")
	}
	if !strings.Contains(err.Error(), "repo contains invalid characters") {
		t.Errorf("expected validator error mentioning repo, got: %v", err)
	}
}

func TestSearchRepositoryIssues_EmptyResult(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	issues, err := client.SearchRepositoryIssues("owner", "repo", SearchFilters{}, 0)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("Expected empty issues, got %d", len(issues))
	}
}

func TestSearchRepositoryIssues_WithLimit(t *testing.T) {
	callCount := 0
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "SearchIssues" {
				callCount++
				v := reflect.ValueOf(query).Elem()
				search := v.FieldByName("Search")
				nodes := search.FieldByName("Nodes")
				pageInfo := search.FieldByName("PageInfo")

				nodeType := nodes.Type().Elem()
				// Return 5 issues per page
				newNodes := reflect.MakeSlice(nodes.Type(), 5, 5)
				for i := 0; i < 5; i++ {
					node := reflect.New(nodeType).Elem()
					node.FieldByName("TypeName").SetString("Issue")
					issue := node.FieldByName("Issue")
					issue.FieldByName("ID").SetString("issue-" + string(rune('0'+i)))
					issue.FieldByName("Number").SetInt(int64(i + 1))
					issue.FieldByName("Title").SetString("Issue " + string(rune('0'+i)))
					issue.FieldByName("State").SetString("OPEN")
					newNodes.Index(i).Set(node)
				}
				nodes.Set(newNodes)

				// More pages available
				pageInfo.FieldByName("HasNextPage").SetBool(true)
				pageInfo.FieldByName("EndCursor").SetString("cursor")
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	issues, err := client.SearchRepositoryIssues("owner", "repo", SearchFilters{}, 3)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Should stop after getting 3 issues even though more available
	if len(issues) != 3 {
		t.Errorf("Expected 3 issues with limit, got %d", len(issues))
	}
	if callCount != 1 {
		t.Errorf("Expected 1 API call with limit, got %d", callCount)
	}
}

// ============================================================================
// SearchIntakeCandidates Tests (#918)
// ============================================================================

// intakeSearchMock answers SearchIntakeCandidates pages from canned JSON, one
// entry per page. Keys match the query struct's field names case-insensitively.
func intakeSearchMock(t *testing.T, pages ...string) (*queryMockClient, *[]map[string]interface{}) {
	t.Helper()
	var calls []map[string]interface{}
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name != "SearchIntakeCandidates" {
				t.Fatalf("unexpected operation %q", name)
			}
			calls = append(calls, variables)
			if len(calls) > len(pages) {
				t.Fatalf("unexpected page request %d", len(calls))
			}
			return json.Unmarshal([]byte(pages[len(calls)-1]), query)
		},
	}
	return mock, &calls
}

func TestSearchIntakeCandidates_ClassifiesMembership(t *testing.T) {
	page := `{"search":{"nodes":[
		{"typeName":"Issue","issue":{"id":"I_tracked","number":1,"title":"Tracked","state":"OPEN","url":"u1",
			"repository":{"nameWithOwner":"owner/repo"},
			"projectItems":{"nodes":[{"project":{"id":"PVT_other"}},{"project":{"id":"PVT_target"}}],"pageInfo":{"hasNextPage":false}}}},
		{"typeName":"Issue","issue":{"id":"I_untracked","number":2,"title":"Untracked","state":"OPEN","url":"u2",
			"repository":{"nameWithOwner":"owner/repo"},
			"assignees":{"nodes":[{"login":"alice"}]},
			"labels":{"nodes":[{"name":"bug"}]},
			"projectItems":{"nodes":[{"project":{"id":"PVT_other"}}],"pageInfo":{"hasNextPage":false}}}},
		{"typeName":"PullRequest","issue":{}}
	],"pageInfo":{"hasNextPage":false,"endCursor":""}}}`
	mock, _ := intakeSearchMock(t, page)
	client := NewClientWithGraphQL(mock)

	candidates, err := client.SearchIntakeCandidates("owner", "repo", nil, "PVT_target")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 issue candidates (pull request skipped), got %d", len(candidates))
	}

	tracked, untracked := candidates[0], candidates[1]
	if !tracked.InProject || tracked.MembershipUnknown {
		t.Errorf("#1 should be InProject and known, got %+v", tracked)
	}
	if untracked.InProject || untracked.MembershipUnknown {
		t.Errorf("#2 should be untracked and known, got %+v", untracked)
	}
	if untracked.Issue.Title != "Untracked" || untracked.Issue.URL != "u2" || untracked.Issue.State != "OPEN" {
		t.Errorf("issue fields not mapped: %+v", untracked.Issue)
	}
	if untracked.Issue.Repository.Owner != "owner" || untracked.Issue.Repository.Name != "repo" {
		t.Errorf("repository not mapped: %+v", untracked.Issue.Repository)
	}
	if len(untracked.Issue.Assignees) != 1 || untracked.Issue.Assignees[0].Login != "alice" {
		t.Errorf("assignees not mapped: %+v", untracked.Issue.Assignees)
	}
	if len(untracked.Issue.Labels) != 1 || untracked.Issue.Labels[0].Name != "bug" {
		t.Errorf("labels not mapped: %+v", untracked.Issue.Labels)
	}
}

// TestSearchIntakeCandidates_MoreThanTwentyProjects covers the truncated page:
// found on the first page is still a definite answer, not found is unknown.
func TestSearchIntakeCandidates_MoreThanTwentyProjects(t *testing.T) {
	page := `{"search":{"nodes":[
		{"typeName":"Issue","issue":{"id":"I_found","number":1,
			"projectItems":{"nodes":[{"project":{"id":"PVT_target"}}],"pageInfo":{"hasNextPage":true}}}},
		{"typeName":"Issue","issue":{"id":"I_unknown","number":2,
			"projectItems":{"nodes":[{"project":{"id":"PVT_other"}}],"pageInfo":{"hasNextPage":true}}}}
	],"pageInfo":{"hasNextPage":false}}}`
	mock, _ := intakeSearchMock(t, page)
	client := NewClientWithGraphQL(mock)

	candidates, err := client.SearchIntakeCandidates("owner", "repo", nil, "PVT_target")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if !candidates[0].InProject || candidates[0].MembershipUnknown {
		t.Errorf("#1 found on a truncated page should be InProject and known, got %+v", candidates[0])
	}
	if candidates[1].InProject || !candidates[1].MembershipUnknown {
		t.Errorf("#2 absent from a truncated page should be MembershipUnknown, got %+v", candidates[1])
	}
}

func TestSearchIntakeCandidates_PaginatesAndBuildsQuery(t *testing.T) {
	page1 := `{"search":{"nodes":[{"typeName":"Issue","issue":{"id":"I_1","number":1}}],
		"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}`
	page2 := `{"search":{"nodes":[{"typeName":"Issue","issue":{"id":"I_2","number":2}}],
		"pageInfo":{"hasNextPage":false}}}`
	mock, calls := intakeSearchMock(t, page1, page2)
	client := NewClientWithGraphQL(mock)

	candidates, err := client.SearchIntakeCandidates("owner", "repo", []string{"bug"}, "PVT_target")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected candidates from both pages, got %d", len(candidates))
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 page requests, got %d", len(*calls))
	}
	query := string((*calls)[0]["query"].(graphql.String))
	for _, part := range []string{"repo:owner/repo", "is:issue", "is:open", `label:"bug"`} {
		if !strings.Contains(query, part) {
			t.Errorf("search query %q missing %q", query, part)
		}
	}
	if cursor, ok := (*calls)[1]["cursor"].(graphql.String); !ok || string(cursor) != "c1" {
		t.Errorf("second page should pass cursor c1, got %v", (*calls)[1]["cursor"])
	}
}

// TestSearchIntakeCandidates_QueryIsSlim pins the fields intake does not use
// out of the document (#918: body, author and milestone were fetched and discarded).
func TestSearchIntakeCandidates_QueryIsSlim(t *testing.T) {
	typ := reflect.TypeOf(intakeSearchNode{})
	issue, ok := typ.FieldByName("Issue")
	if !ok {
		t.Fatal("intakeSearchNode has no Issue field")
	}
	for _, unwanted := range []string{"Body", "Author", "Milestone"} {
		if _, present := issue.Type.FieldByName(unwanted); present {
			t.Errorf("intake search query must not request %s", unwanted)
		}
	}
}

func TestSearchIntakeCandidates_Errors(t *testing.T) {
	client := NewClientWithGraphQL(&queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("boom")
		},
	})
	if _, err := client.SearchIntakeCandidates("owner", "repo", nil, "PVT_target"); err == nil ||
		!strings.Contains(err.Error(), "failed to search intake candidates") {
		t.Errorf("expected wrapped query error, got %v", err)
	}
	if _, err := client.SearchIntakeCandidates("bad owner", "repo", nil, "PVT_target"); err == nil {
		t.Error("expected owner validation error")
	}
	if _, err := client.SearchIntakeCandidates("owner", "repo", nil, ""); err == nil {
		t.Error("expected error for empty project ID")
	}
}

// ============================================================================
// GetProjectFieldsForIssues Tests
// ============================================================================

func TestGetProjectFieldsForIssues_EmptyInput(t *testing.T) {
	client, cErr := NewClient()
	if cErr != nil {
		t.Skipf("Skipping - requires auth: %v", cErr)
	}
	result, err := client.GetProjectFieldsForIssues("proj-id", []string{})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result map")
	}
	if len(result) != 0 {
		t.Errorf("Expected empty result map, got %d entries", len(result))
	}
}

// ============================================================================
// GetProjectItems With Limit Tests
// ============================================================================

func TestGetProjectItems_WithLimit_EarlyTermination(t *testing.T) {
	callCount := 0
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				callCount++
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")
				pageInfoField := items.FieldByName("PageInfo")

				nodeType := nodes.Type().Elem()
				// Return 5 items per page
				newNodes := reflect.MakeSlice(nodes.Type(), 5, 5)
				for i := 0; i < 5; i++ {
					newNode := reflect.New(nodeType).Elem()
					newNode.FieldByName("ID").SetString("item-" + string(rune('0'+i)))
					content := newNode.FieldByName("Content")
					content.FieldByName("TypeName").SetString("Issue")
					issue := content.FieldByName("Issue")
					issue.FieldByName("ID").SetString("issue-" + string(rune('0'+i)))
					issue.FieldByName("Number").SetInt(int64(i + 1))
					issue.FieldByName("Title").SetString("Issue")
					issue.FieldByName("State").SetString("OPEN")
					repo := issue.FieldByName("Repository")
					repo.FieldByName("NameWithOwner").SetString("owner/repo")
					newNodes.Index(i).Set(newNode)
				}
				nodes.Set(newNodes)

				// More pages available
				pageInfoField.FieldByName("HasNextPage").SetBool(true)
				pageInfoField.FieldByName("EndCursor").SetString("cursor")
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItems("proj-id", &ProjectItemsFilter{Limit: 3})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Should stop after getting 3 items even though more available
	if len(items) != 3 {
		t.Errorf("Expected 3 items with limit, got %d", len(items))
	}
	if callCount != 1 {
		t.Errorf("Expected 1 API call with early termination, got %d", callCount)
	}
}

func TestGetProjectItems_WithLimit_ExactMatch(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")
				pageInfoField := items.FieldByName("PageInfo")

				nodeType := nodes.Type().Elem()
				// Return exactly 3 items
				newNodes := reflect.MakeSlice(nodes.Type(), 3, 3)
				for i := 0; i < 3; i++ {
					newNode := reflect.New(nodeType).Elem()
					newNode.FieldByName("ID").SetString("item-" + string(rune('0'+i)))
					content := newNode.FieldByName("Content")
					content.FieldByName("TypeName").SetString("Issue")
					issue := content.FieldByName("Issue")
					issue.FieldByName("ID").SetString("issue-" + string(rune('0'+i)))
					issue.FieldByName("Number").SetInt(int64(i + 1))
					issue.FieldByName("Title").SetString("Issue")
					issue.FieldByName("State").SetString("OPEN")
					repo := issue.FieldByName("Repository")
					repo.FieldByName("NameWithOwner").SetString("owner/repo")
					newNodes.Index(i).Set(newNode)
				}
				nodes.Set(newNodes)

				pageInfoField.FieldByName("HasNextPage").SetBool(false)
				pageInfoField.FieldByName("EndCursor").SetString("")
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItems("proj-id", &ProjectItemsFilter{Limit: 3})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("Expected 3 items, got %d", len(items))
	}
}

func TestGetProjectItems_WithLimit_Zero(t *testing.T) {
	callCount := 0
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				callCount++
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")
				pageInfoField := items.FieldByName("PageInfo")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 2, 2)
				for i := 0; i < 2; i++ {
					newNode := reflect.New(nodeType).Elem()
					newNode.FieldByName("ID").SetString("item-" + string(rune('0'+i)))
					content := newNode.FieldByName("Content")
					content.FieldByName("TypeName").SetString("Issue")
					issue := content.FieldByName("Issue")
					issue.FieldByName("ID").SetString("issue-" + string(rune('0'+i)))
					issue.FieldByName("Number").SetInt(int64(i + 1))
					issue.FieldByName("Title").SetString("Issue")
					issue.FieldByName("State").SetString("OPEN")
					repo := issue.FieldByName("Repository")
					repo.FieldByName("NameWithOwner").SetString("owner/repo")
					newNodes.Index(i).Set(newNode)
				}
				nodes.Set(newNodes)

				if callCount == 1 {
					pageInfoField.FieldByName("HasNextPage").SetBool(true)
					pageInfoField.FieldByName("EndCursor").SetString("cursor")
				} else {
					pageInfoField.FieldByName("HasNextPage").SetBool(false)
					pageInfoField.FieldByName("EndCursor").SetString("")
				}
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	// Limit 0 means no limit - should fetch all pages
	items, err := client.GetProjectItems("proj-id", &ProjectItemsFilter{Limit: 0})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Should fetch all items from both pages (4 < default max of 10000)
	if len(items) != 4 {
		t.Errorf("Expected 4 items with no limit, got %d", len(items))
	}
	if callCount != 2 {
		t.Errorf("Expected 2 API calls with no limit, got %d", callCount)
	}
}

func TestGetProjectItems_DefaultMaxLimit(t *testing.T) {
	// Verify that Limit: 0 applies default max (10000) instead of truly unlimited
	// We test this indirectly: with a nil filter, early termination at limit should work
	callCount := 0
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItems" {
				callCount++
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")
				pageInfoField := items.FieldByName("PageInfo")

				nodeType := nodes.Type().Elem()
				newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)
				newNode := reflect.New(nodeType).Elem()
				newNode.FieldByName("ID").SetString("item-1")
				content := newNode.FieldByName("Content")
				content.FieldByName("TypeName").SetString("Issue")
				issue := content.FieldByName("Issue")
				issue.FieldByName("ID").SetString("issue-1")
				issue.FieldByName("Number").SetInt(1)
				issue.FieldByName("Title").SetString("Test")
				issue.FieldByName("State").SetString("OPEN")
				repo := issue.FieldByName("Repository")
				repo.FieldByName("NameWithOwner").SetString("owner/repo")
				newNodes.Index(0).Set(newNode)
				nodes.Set(newNodes)

				pageInfoField.FieldByName("HasNextPage").SetBool(false)
				pageInfoField.FieldByName("EndCursor").SetString("")
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	// nil filter should apply default max limit of 10000
	items, err := client.GetProjectItems("proj-id", nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("Expected 1 item, got %d", len(items))
	}
}

// ============================================================================
// GetProjectItemsMinimal Tests
// ============================================================================

func TestGetProjectItemsMinimal_QueryError(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New("query failed")
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItemsMinimal("proj-id", nil)

	if err == nil {
		t.Fatal("Expected error when query fails")
	}
	if items != nil {
		t.Error("Expected nil items when error occurs")
	}
	if !strings.Contains(err.Error(), "failed to get minimal project items") {
		t.Errorf("Expected 'failed to get minimal project items' error, got: %v", err)
	}
}

func TestGetProjectItemsMinimal_EmptyResult(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItemsMinimal("proj-id", nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("Expected empty items, got %d", len(items))
	}
}

func TestGetProjectItemsMinimal_WithItems(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItemsMinimal" {
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")

				// Create one test item
				nodeType := nodes.Type().Elem()
				newSlice := reflect.MakeSlice(nodes.Type(), 1, 1)
				node0 := reflect.New(nodeType).Elem()

				// Set Content
				content := node0.FieldByName("Content")
				content.FieldByName("TypeName").SetString("Issue")
				issue := content.FieldByName("Issue")
				issue.FieldByName("ID").SetString("issue-123")
				issue.FieldByName("Number").SetInt(42)
				issue.FieldByName("State").SetString("OPEN")
				repo := issue.FieldByName("Repository")
				repo.FieldByName("NameWithOwner").SetString("owner/repo")

				// Set FieldValues
				fieldValues := node0.FieldByName("FieldValues")
				fvNodes := fieldValues.FieldByName("Nodes")
				fvNodeType := fvNodes.Type().Elem()
				fvSlice := reflect.MakeSlice(fvNodes.Type(), 1, 1)
				fv0 := reflect.New(fvNodeType).Elem()
				fv0.FieldByName("TypeName").SetString("ProjectV2ItemFieldTextValue")
				textVal := fv0.FieldByName("ProjectV2ItemFieldTextValue")
				textVal.FieldByName("Text").SetString("release/v1.0.0")
				textField := textVal.FieldByName("Field")
				innerField := textField.FieldByName("ProjectV2Field")
				innerField.FieldByName("Name").SetString("Branch")
				fvSlice.Index(0).Set(fv0)
				fvNodes.Set(fvSlice)

				newSlice.Index(0).Set(node0)
				nodes.Set(newSlice)

				// Set PageInfo
				pageInfo := items.FieldByName("PageInfo")
				pageInfo.FieldByName("HasNextPage").SetBool(false)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItemsMinimal("proj-id", nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
	if items[0].IssueNumber != 42 {
		t.Errorf("Expected issue number 42, got %d", items[0].IssueNumber)
	}
	if items[0].IssueState != "OPEN" {
		t.Errorf("Expected state OPEN, got %s", items[0].IssueState)
	}
	if items[0].Repository != "owner/repo" {
		t.Errorf("Expected repository owner/repo, got %s", items[0].Repository)
	}
	if len(items[0].FieldValues) != 1 {
		t.Fatalf("Expected 1 field value, got %d", len(items[0].FieldValues))
	}
	if items[0].FieldValues[0].Field != "Branch" {
		t.Errorf("Expected field name Branch, got %s", items[0].FieldValues[0].Field)
	}
}

func TestGetProjectItemsMinimal_WithFilter(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItemsMinimal" {
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")

				// Create two test items with different repositories
				nodeType := nodes.Type().Elem()
				newSlice := reflect.MakeSlice(nodes.Type(), 2, 2)

				for i := 0; i < 2; i++ {
					node := reflect.New(nodeType).Elem()
					content := node.FieldByName("Content")
					content.FieldByName("TypeName").SetString("Issue")
					issue := content.FieldByName("Issue")
					issue.FieldByName("ID").SetString(fmt.Sprintf("issue-%d", i))
					issue.FieldByName("Number").SetInt(int64(i + 1))
					issue.FieldByName("State").SetString("OPEN")
					repo := issue.FieldByName("Repository")
					if i == 0 {
						repo.FieldByName("NameWithOwner").SetString("owner/repo")
					} else {
						repo.FieldByName("NameWithOwner").SetString("other/repo")
					}
					newSlice.Index(i).Set(node)
				}
				nodes.Set(newSlice)

				pageInfo := items.FieldByName("PageInfo")
				pageInfo.FieldByName("HasNextPage").SetBool(false)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	filter := &ProjectItemsFilter{Repository: "owner/repo"}
	items, err := client.GetProjectItemsMinimal("proj-id", filter)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Should only return the item matching the filter
	if len(items) != 1 {
		t.Fatalf("Expected 1 item after filtering, got %d", len(items))
	}
	if items[0].Repository != "owner/repo" {
		t.Errorf("Expected repository owner/repo, got %s", items[0].Repository)
	}
}

func TestGetProjectItemsMinimal_SkipsNonIssues(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetProjectItemsMinimal" {
				v := reflect.ValueOf(query).Elem()
				node := v.FieldByName("Node")
				projectV2 := node.FieldByName("ProjectV2")
				items := projectV2.FieldByName("Items")
				nodes := items.FieldByName("Nodes")

				// Create two items - one Issue, one DraftIssue
				nodeType := nodes.Type().Elem()
				newSlice := reflect.MakeSlice(nodes.Type(), 2, 2)

				// Issue item
				node0 := reflect.New(nodeType).Elem()
				content0 := node0.FieldByName("Content")
				content0.FieldByName("TypeName").SetString("Issue")
				issue := content0.FieldByName("Issue")
				issue.FieldByName("ID").SetString("issue-1")
				issue.FieldByName("Number").SetInt(1)
				issue.FieldByName("State").SetString("OPEN")
				repo := issue.FieldByName("Repository")
				repo.FieldByName("NameWithOwner").SetString("owner/repo")
				newSlice.Index(0).Set(node0)

				// DraftIssue item (should be skipped)
				node1 := reflect.New(nodeType).Elem()
				content1 := node1.FieldByName("Content")
				content1.FieldByName("TypeName").SetString("DraftIssue")
				newSlice.Index(1).Set(node1)

				nodes.Set(newSlice)

				pageInfo := items.FieldByName("PageInfo")
				pageInfo.FieldByName("HasNextPage").SetBool(false)
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	items, err := client.GetProjectItemsMinimal("proj-id", nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Should only return 1 item (the Issue, not the DraftIssue)
	if len(items) != 1 {
		t.Fatalf("Expected 1 item (skipping DraftIssue), got %d", len(items))
	}
}

// ============================================================================
// GetIssuesByLabel Pagination Tests
// ============================================================================

func TestGetOpenIssuesByLabel_Pagination(t *testing.T) {
	callCount := 0

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetIssuesByLabel" {
				callCount++
				v := reflect.ValueOf(query).Elem()
				repo := v.FieldByName("Repository")
				issues := repo.FieldByName("Issues")
				nodes := issues.FieldByName("Nodes")
				pageInfoField := issues.FieldByName("PageInfo")

				nodeType := nodes.Type().Elem()

				if callCount == 1 {
					// First page - return issues 1-2 with hasNextPage=true
					newNodes := reflect.MakeSlice(nodes.Type(), 2, 2)

					node1 := reflect.New(nodeType).Elem()
					node1.FieldByName("ID").SetString("issue-1")
					node1.FieldByName("Number").SetInt(1)
					node1.FieldByName("Title").SetString("Issue 1")
					node1.FieldByName("State").SetString("OPEN")
					node1.FieldByName("URL").SetString("https://github.com/owner/repo/issues/1")
					// Set empty Labels
					labelsField := node1.FieldByName("Labels")
					labelsNodes := labelsField.FieldByName("Nodes")
					labelsNodes.Set(reflect.MakeSlice(labelsNodes.Type(), 0, 0))
					newNodes.Index(0).Set(node1)

					node2 := reflect.New(nodeType).Elem()
					node2.FieldByName("ID").SetString("issue-2")
					node2.FieldByName("Number").SetInt(2)
					node2.FieldByName("Title").SetString("Issue 2")
					node2.FieldByName("State").SetString("OPEN")
					node2.FieldByName("URL").SetString("https://github.com/owner/repo/issues/2")
					labelsField2 := node2.FieldByName("Labels")
					labelsNodes2 := labelsField2.FieldByName("Nodes")
					labelsNodes2.Set(reflect.MakeSlice(labelsNodes2.Type(), 0, 0))
					newNodes.Index(1).Set(node2)

					nodes.Set(newNodes)

					pageInfoField.FieldByName("HasNextPage").SetBool(true)
					pageInfoField.FieldByName("EndCursor").SetString("cursor-1")
				} else if callCount == 2 {
					// Second page - return issue 3 with hasNextPage=false
					newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)

					node3 := reflect.New(nodeType).Elem()
					node3.FieldByName("ID").SetString("issue-3")
					node3.FieldByName("Number").SetInt(3)
					node3.FieldByName("Title").SetString("Issue 3")
					node3.FieldByName("State").SetString("OPEN")
					node3.FieldByName("URL").SetString("https://github.com/owner/repo/issues/3")
					labelsField3 := node3.FieldByName("Labels")
					labelsNodes3 := labelsField3.FieldByName("Nodes")
					labelsNodes3.Set(reflect.MakeSlice(labelsNodes3.Type(), 0, 0))
					newNodes.Index(0).Set(node3)

					nodes.Set(newNodes)

					pageInfoField.FieldByName("HasNextPage").SetBool(false)
					pageInfoField.FieldByName("EndCursor").SetString("")
				}
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	issues, err := client.GetOpenIssuesByLabel("owner", "repo", "bug")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 API calls for pagination, got %d", callCount)
	}
	if len(issues) != 3 {
		t.Fatalf("Expected 3 issues from 2 pages, got %d", len(issues))
	}
	if issues[0].Title != "Issue 1" {
		t.Errorf("Expected first issue title 'Issue 1', got '%s'", issues[0].Title)
	}
	if issues[2].Title != "Issue 3" {
		t.Errorf("Expected third issue title 'Issue 3', got '%s'", issues[2].Title)
	}
}

func TestGetClosedIssuesByLabel_Pagination(t *testing.T) {
	callCount := 0

	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetIssuesByLabel" {
				callCount++
				v := reflect.ValueOf(query).Elem()
				repo := v.FieldByName("Repository")
				issues := repo.FieldByName("Issues")
				nodes := issues.FieldByName("Nodes")
				pageInfoField := issues.FieldByName("PageInfo")

				nodeType := nodes.Type().Elem()

				if callCount == 1 {
					// First page - return issues 1-2 with hasNextPage=true
					newNodes := reflect.MakeSlice(nodes.Type(), 2, 2)

					node1 := reflect.New(nodeType).Elem()
					node1.FieldByName("ID").SetString("issue-1")
					node1.FieldByName("Number").SetInt(1)
					node1.FieldByName("Title").SetString("Closed Issue 1")
					node1.FieldByName("State").SetString("CLOSED")
					node1.FieldByName("URL").SetString("https://github.com/owner/repo/issues/1")
					labelsField := node1.FieldByName("Labels")
					labelsNodes := labelsField.FieldByName("Nodes")
					labelsNodes.Set(reflect.MakeSlice(labelsNodes.Type(), 0, 0))
					newNodes.Index(0).Set(node1)

					node2 := reflect.New(nodeType).Elem()
					node2.FieldByName("ID").SetString("issue-2")
					node2.FieldByName("Number").SetInt(2)
					node2.FieldByName("Title").SetString("Closed Issue 2")
					node2.FieldByName("State").SetString("CLOSED")
					node2.FieldByName("URL").SetString("https://github.com/owner/repo/issues/2")
					labelsField2 := node2.FieldByName("Labels")
					labelsNodes2 := labelsField2.FieldByName("Nodes")
					labelsNodes2.Set(reflect.MakeSlice(labelsNodes2.Type(), 0, 0))
					newNodes.Index(1).Set(node2)

					nodes.Set(newNodes)

					pageInfoField.FieldByName("HasNextPage").SetBool(true)
					pageInfoField.FieldByName("EndCursor").SetString("cursor-1")
				} else if callCount == 2 {
					// Second page - return issue 3 with hasNextPage=false
					newNodes := reflect.MakeSlice(nodes.Type(), 1, 1)

					node3 := reflect.New(nodeType).Elem()
					node3.FieldByName("ID").SetString("issue-3")
					node3.FieldByName("Number").SetInt(3)
					node3.FieldByName("Title").SetString("Closed Issue 3")
					node3.FieldByName("State").SetString("CLOSED")
					node3.FieldByName("URL").SetString("https://github.com/owner/repo/issues/3")
					labelsField3 := node3.FieldByName("Labels")
					labelsNodes3 := labelsField3.FieldByName("Nodes")
					labelsNodes3.Set(reflect.MakeSlice(labelsNodes3.Type(), 0, 0))
					newNodes.Index(0).Set(node3)

					nodes.Set(newNodes)

					pageInfoField.FieldByName("HasNextPage").SetBool(false)
					pageInfoField.FieldByName("EndCursor").SetString("")
				}
			}
			return nil
		},
	}

	client := NewClientWithGraphQL(mock)
	issues, err := client.GetClosedIssuesByLabel("owner", "repo", "bug")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 API calls for pagination, got %d", callCount)
	}
	if len(issues) != 3 {
		t.Fatalf("Expected 3 issues from 2 pages, got %d", len(issues))
	}
	if issues[0].Title != "Closed Issue 1" {
		t.Errorf("Expected first issue title 'Closed Issue 1', got '%s'", issues[0].Title)
	}
	if issues[2].Title != "Closed Issue 3" {
		t.Errorf("Expected third issue title 'Closed Issue 3', got '%s'", issues[2].Title)
	}
}

// ============================================================================
// buildGraphQLRequestBody Tests
// ============================================================================

func TestBuildGraphQLRequestBody_SimpleQuery(t *testing.T) {
	query := `query { repository(owner: "owner", name: "repo") { issue(number: 1) { id } } }`
	body, err := buildGraphQLRequestBody(query)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify it's valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("Failed to parse body as JSON: %v", err)
	}

	// Verify query field is present and matches
	if parsed["query"] != query {
		t.Errorf("Expected query field to match input, got %q", parsed["query"])
	}
}

func TestBuildGraphQLRequestBody_EmptyQuery(t *testing.T) {
	body, err := buildGraphQLRequestBody("")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("Failed to parse body as JSON: %v", err)
	}

	if parsed["query"] != "" {
		t.Errorf("Expected empty query field, got %q", parsed["query"])
	}
}

func TestBuildGraphQLRequestBody_SpecialCharacters(t *testing.T) {
	// Query with special characters that could break CLI argument passing
	query := `query { repository(owner: "test\"org", name: "my-repo") { issue(number: 1) { body } } }`
	body, err := buildGraphQLRequestBody(query)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify it's valid JSON (json.Marshal handles escaping)
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("Failed to parse body as JSON: %v", err)
	}

	if parsed["query"] != query {
		t.Errorf("Expected query to preserve special characters")
	}
}

func TestBuildGraphQLRequestBody_LargePayload(t *testing.T) {
	// Build a query that would exceed Windows' ~32KB CLI limit
	// Each alias is ~600 chars, so 60 aliases exceeds 32KB
	var queryParts []string
	for i := 0; i < 60; i++ {
		queryParts = append(queryParts, fmt.Sprintf(`i%d: issue(number: %d) {
			id
			number
			title
			body
			state
			url
			repository { nameWithOwner }
			assignees(first: 10) { nodes { login } }
			labels(first: 20) { nodes { name } }
			projectItems(first: 20) {
				nodes {
					id
					project { id }
					fieldValues(first: 20) {
						nodes {
							__typename
							... on ProjectV2ItemFieldSingleSelectValue {
								name
								field { ... on ProjectV2SingleSelectField { name } }
							}
							... on ProjectV2ItemFieldTextValue {
								text
								field { ... on ProjectV2Field { name } }
							}
						}
					}
				}
			}
		}`, i, i+1))
	}
	query := fmt.Sprintf(`query { repository(owner: "test", name: "repo") { %s } }`,
		strings.Join(queryParts, " "))

	// Verify the query exceeds 32KB (Windows CLI limit)
	if len(query) < 32000 {
		t.Fatalf("Test query should exceed 32KB, got %d bytes", len(query))
	}

	body, err := buildGraphQLRequestBody(query)
	if err != nil {
		t.Fatalf("Unexpected error building large request body: %v", err)
	}

	// Verify it's valid JSON even at large size
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("Failed to parse large body as JSON: %v", err)
	}

	if parsed["query"] != query {
		t.Error("Large query was not preserved in JSON body")
	}
}

func TestBuildGraphQLRequestBody_WithExtraHeaders(t *testing.T) {
	// Verify the request body is the same regardless of headers
	// (headers are passed separately via exec.Command args, not in the body)
	query := `query { repository(owner: "o", name: "r") { issue(number: 1) { subIssues { totalCount } } } }`
	body, err := buildGraphQLRequestBody(query)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("Failed to parse body as JSON: %v", err)
	}

	// Should only have "query" key, no extra fields
	if len(parsed) != 1 {
		t.Errorf("Expected exactly 1 key in request body, got %d", len(parsed))
	}
}

// ============================================================================
// parseIssuesBatchResponse Tests
// ============================================================================

func TestParseIssuesBatchResponse_MultipleIssues(t *testing.T) {
	data := []byte(`{
		"data": {
			"repository": {
				"i0": {
					"id": "ID1", "number": 42, "title": "First", "body": "body1",
					"state": "OPEN", "url": "https://example.com/42",
					"author": {"login": "user1"},
					"assignees": {"nodes": [{"login": "dev1"}]},
					"labels": {"nodes": [{"name": "bug", "color": "d73a4a"}]},
					"milestone": {"title": "v1.0"},
					"projectItems": {"nodes": []}
				},
				"i1": {
					"id": "ID2", "number": 43, "title": "Second", "body": "body2",
					"state": "CLOSED", "url": "https://example.com/43",
					"author": {"login": "user2"},
					"assignees": {"nodes": []},
					"labels": {"nodes": []},
					"milestone": {"title": ""},
					"projectItems": {"nodes": []}
				}
			}
		}
	}`)

	issues, fieldValues, issueErrors, err := parseIssuesBatchResponse(data, []int{42, 43}, "", "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issueErrors) != 0 {
		t.Errorf("expected no issue errors, got %d", len(issueErrors))
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
	if issues[42].Title != "First" {
		t.Errorf("expected title 'First', got %q", issues[42].Title)
	}
	if issues[43].Title != "Second" {
		t.Errorf("expected title 'Second', got %q", issues[43].Title)
	}
	if issues[42].Author.Login != "user1" {
		t.Errorf("expected author 'user1', got %q", issues[42].Author.Login)
	}
	if len(issues[42].Assignees) != 1 {
		t.Errorf("expected 1 assignee for issue 42, got %d", len(issues[42].Assignees))
	}
	if len(issues[42].Labels) != 1 {
		t.Errorf("expected 1 label for issue 42, got %d", len(issues[42].Labels))
	}
	if issues[42].Milestone == nil || issues[42].Milestone.Title != "v1.0" {
		t.Errorf("expected milestone 'v1.0' for issue 42")
	}
	if issues[43].Milestone != nil {
		t.Errorf("expected nil milestone for issue 43")
	}
	// fieldValues should exist (may be empty)
	if _, ok := fieldValues[42]; !ok {
		t.Error("expected fieldValues entry for issue 42")
	}
}

func TestParseIssuesBatchResponse_NullIssue(t *testing.T) {
	data := []byte(`{
		"data": {
			"repository": {
				"i0": {
					"id": "ID1", "number": 42, "title": "Valid", "body": "",
					"state": "OPEN", "url": "https://example.com/42",
					"author": {"login": "user1"},
					"assignees": {"nodes": []},
					"labels": {"nodes": []},
					"milestone": {"title": ""},
					"projectItems": {"nodes": []}
				},
				"i1": null
			}
		},
		"errors": [{"message": "Could not resolve to an issue", "path": ["repository", "i1"]}]
	}`)

	issues, _, issueErrors, err := parseIssuesBatchResponse(data, []int{42, 99}, "", "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 valid issue, got %d", len(issues))
	}
	if _, ok := issues[42]; !ok {
		t.Error("expected issue 42 in results")
	}
	if issueErrors[99] == nil {
		t.Error("expected error for issue 99")
	}
}

func TestParseIssuesBatchResponse_WithFieldValues(t *testing.T) {
	data := []byte(`{
		"data": {
			"repository": {
				"i0": {
					"id": "ID1", "number": 42, "title": "Test", "body": "",
					"state": "OPEN", "url": "https://example.com/42",
					"author": {"login": "user1"},
					"assignees": {"nodes": []},
					"labels": {"nodes": []},
					"milestone": {"title": ""},
					"projectItems": {"nodes": [{
						"fieldValues": {"nodes": [
							{"__typename": "ProjectV2ItemFieldSingleSelectValue", "name": "In progress", "text": "", "field": {"name": "Status"}},
							{"__typename": "ProjectV2ItemFieldTextValue", "name": "", "text": "feature/x", "field": {"name": "Branch"}}
						]}
					}]}
				}
			}
		}
	}`)

	_, fieldValues, _, err := parseIssuesBatchResponse(data, []int{42}, "", "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fieldValues[42]) != 2 {
		t.Fatalf("expected 2 field values, got %d", len(fieldValues[42]))
	}
}

// TestParseIssuesBatchResponse_FiltersByProject covers #856: an issue on more
// than one project board must surface only the configured project's field
// values, not a foreign board's Status/Priority.
func TestParseIssuesBatchResponse_FiltersByProject(t *testing.T) {
	data := []byte(`{
		"data": {
			"repository": {
				"i0": {
					"id": "ID1", "number": 42, "title": "Multi-board", "body": "",
					"state": "OPEN", "url": "https://example.com/42",
					"author": {"login": "user1"},
					"assignees": {"nodes": []},
					"labels": {"nodes": []},
					"milestone": {"title": ""},
					"projectItems": {"nodes": [
						{
							"project": {"id": "PVT_configured"},
							"fieldValues": {"nodes": [
								{"__typename": "ProjectV2ItemFieldSingleSelectValue", "name": "In progress", "text": "", "field": {"name": "Status"}}
							]}
						},
						{
							"project": {"id": "PVT_other"},
							"fieldValues": {"nodes": [
								{"__typename": "ProjectV2ItemFieldSingleSelectValue", "name": "Done", "text": "", "field": {"name": "Status"}},
								{"__typename": "ProjectV2ItemFieldSingleSelectValue", "name": "P1", "text": "", "field": {"name": "Priority"}}
							]}
						}
					]}
				}
			}
		}
	}`)

	_, fieldValues, _, err := parseIssuesBatchResponse(data, []int{42}, "PVT_configured", "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := fieldValues[42]
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 field value from the configured project, got %d: %+v", len(got), got)
	}
	if got[0].Field != "Status" || got[0].Value != "In progress" {
		t.Errorf("expected Status=In progress from the configured project, got %+v", got[0])
	}
	for _, fv := range got {
		if fv.Value == "Done" || fv.Value == "P1" {
			t.Errorf("foreign project field leaked into results: %+v", fv)
		}
	}
}

// TestParseIssuesBatchResponse_EmptyProjectIDKeepsAll verifies the backward-compat
// guard: an empty projectID disables filtering (returns all project items) so
// callers that cannot resolve a project ID keep the prior behavior.
func TestParseIssuesBatchResponse_EmptyProjectIDKeepsAll(t *testing.T) {
	data := []byte(`{
		"data": {
			"repository": {
				"i0": {
					"id": "ID1", "number": 42, "title": "Multi-board", "body": "",
					"state": "OPEN", "url": "https://example.com/42",
					"author": {"login": "user1"},
					"assignees": {"nodes": []},
					"labels": {"nodes": []},
					"milestone": {"title": ""},
					"projectItems": {"nodes": [
						{"project": {"id": "PVT_a"}, "fieldValues": {"nodes": [
							{"__typename": "ProjectV2ItemFieldSingleSelectValue", "name": "In progress", "text": "", "field": {"name": "Status"}}
						]}},
						{"project": {"id": "PVT_b"}, "fieldValues": {"nodes": [
							{"__typename": "ProjectV2ItemFieldSingleSelectValue", "name": "Done", "text": "", "field": {"name": "Status"}}
						]}}
					]}
				}
			}
		}
	}`)

	_, fieldValues, _, err := parseIssuesBatchResponse(data, []int{42}, "", "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fieldValues[42]) != 2 {
		t.Fatalf("expected all 2 field values when projectID is empty, got %d", len(fieldValues[42]))
	}
}

// ============================================================================
// parseParentIssueBatchResponse Tests
// ============================================================================

func TestParseParentIssueBatchResponse_WithParent(t *testing.T) {
	data := []byte(`{
		"data": {
			"repository": {
				"i0": {
					"parent": {"id": "PID1", "number": 10, "title": "Epic", "state": "OPEN", "url": "https://example.com/10"}
				},
				"i1": {
					"parent": null
				}
			}
		}
	}`)

	result, err := parseParentIssueBatchResponse(data, []int{42, 43})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[42] == nil {
		t.Fatal("expected parent for issue 42")
	}
	if result[42].Number != 10 {
		t.Errorf("expected parent number 10, got %d", result[42].Number)
	}
	if result[43] != nil {
		t.Errorf("expected nil parent for issue 43, got %v", result[43])
	}
}

func TestParseParentIssueBatchResponse_EmptyInput(t *testing.T) {
	result, err := parseParentIssueBatchResponse([]byte(`{"data":{"repository":{}}}`), []int{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

// mockRawGraphQL implements RawGraphQLDoer for testing doRawGraphQL.
type mockRawGraphQL struct {
	response  []byte
	err       error
	lastQuery string
	lastHdrs  map[string]string
}

func (m *mockRawGraphQL) DoRaw(query string, headers map[string]string) ([]byte, error) {
	m.lastQuery = query
	m.lastHdrs = headers
	return m.response, m.err
}

func (m *mockRawGraphQL) DoRawBody(body []byte, headers map[string]string) ([]byte, error) {
	m.lastQuery = string(body)
	m.lastHdrs = headers
	return m.response, m.err
}

func TestDoRawGraphQL_Success(t *testing.T) {
	mock := &mockRawGraphQL{
		response: []byte(`{"data":{"repository":{"issue":{"title":"Test"}}}}`),
	}
	client := &Client{rawGQL: mock}

	output, err := client.doRawGraphQL("query { repository { issue { title } } }", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(output) != `{"data":{"repository":{"issue":{"title":"Test"}}}}` {
		t.Errorf("unexpected output: %s", string(output))
	}
	if mock.lastQuery != "query { repository { issue { title } } }" {
		t.Errorf("query not passed correctly: %s", mock.lastQuery)
	}
}

func TestDoRawGraphQL_WithHeaders(t *testing.T) {
	mock := &mockRawGraphQL{
		response: []byte(`{"data":{}}`),
	}
	client := &Client{rawGQL: mock}

	hdrs := map[string]string{"X-Github-Next": "sub_issues"}
	_, err := client.doRawGraphQL("query { test }", hdrs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.lastHdrs["X-Github-Next"] != "sub_issues" {
		t.Errorf("headers not passed: %v", mock.lastHdrs)
	}
}

func TestDoRawGraphQL_Error(t *testing.T) {
	mock := &mockRawGraphQL{
		err: fmt.Errorf("network error"),
	}
	client := &Client{rawGQL: mock}

	_, err := client.doRawGraphQL("query { test }", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "network error") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoRawGraphQL_NilClient(t *testing.T) {
	client := &Client{rawGQL: nil}

	_, err := client.doRawGraphQL("query { test }", nil)
	if err == nil {
		t.Fatal("expected error for nil rawGQL")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoRawGraphQLBody_Success(t *testing.T) {
	mock := &mockRawGraphQL{
		response: []byte(`{"data":{"u0":{"projectV2Item":{"id":"item-1"}}}}`),
	}
	client := &Client{rawGQL: mock}

	body := []byte(`{"query":"mutation { u0: updateProjectV2ItemFieldValue(...) }","variables":{}}`)
	output, err := client.doRawGraphQLBody(body, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(output) != `{"data":{"u0":{"projectV2Item":{"id":"item-1"}}}}` {
		t.Errorf("unexpected output: %s", string(output))
	}
	if mock.lastQuery != string(body) {
		t.Errorf("body not passed correctly")
	}
}

func TestDoRawGraphQLBody_NilClient(t *testing.T) {
	client := &Client{rawGQL: nil}

	_, err := client.doRawGraphQLBody([]byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error for nil rawGQL")
	}
}

// ============================================================================
// #861: response-handling / attribution
// ============================================================================

// TestParseIssuesBatchResponse_IntegerPathSegment covers #861 case 1: a GraphQL
// error whose path contains a list index (integer) must not abort the whole
// batch. Before the fix, Path []string made json.Unmarshal fail for the entire
// response.
func TestParseIssuesBatchResponse_IntegerPathSegment(t *testing.T) {
	data := []byte(`{
		"data": {
			"repository": {
				"i0": null,
				"i1": {
					"id": "ID2", "number": 43, "title": "Second", "body": "",
					"state": "OPEN", "url": "https://example.com/43",
					"author": {"login": "u"}, "assignees": {"nodes": []},
					"labels": {"nodes": []}, "milestone": {"title": ""},
					"projectItems": {"nodes": []}
				}
			}
		},
		"errors": [
			{"message": "boom deep in a list", "path": ["repository", "i0", "projectItems", "nodes", 3, "fieldValues"]}
		]
	}`)

	issues, _, issueErrors, err := parseIssuesBatchResponse(data, []int{42, 43}, "", "owner", "repo")
	if err != nil {
		t.Fatalf("integer path segment must not abort the batch, got top-level error: %v", err)
	}
	if _, ok := issues[43]; !ok {
		t.Errorf("expected the healthy issue 43 to still parse")
	}
	if issueErrors[42] == nil {
		t.Errorf("expected issue 42 (the errored alias i0) to be mapped to issueErrors")
	}
}

// TestListProjects_ZeroProjectUser covers #861 case 2: a user with zero open
// projects must yield an empty list, not an error from the org fallback.
func TestListProjects_ZeroProjectUser(t *testing.T) {
	mock := &mockGraphQLClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "ListUserProjects" {
				return nil // success, zero open projects (empty result)
			}
			if name == "ListOrgProjects" {
				return errors.New("Could not resolve to an Organization with the login of 'someuser'")
			}
			return nil
		},
	}
	client := NewClientWithGraphQL(mock)

	projects, err := client.ListProjects("someuser")
	if err != nil {
		t.Fatalf("zero-project user must return an empty list, got error: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("expected empty project list, got %d", len(projects))
	}
}

// TestGetProject_PreservesBothErrors covers #861 case 3: when both the user and
// org paths fail, the returned error must preserve both causes, not just the org
// failure.
func TestGetProject_PreservesBothErrors(t *testing.T) {
	mock := &mockGraphQLClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name == "GetUserProject" {
				return errors.New("user-path transient boom")
			}
			if name == "GetOrgProject" {
				return errors.New("Could not resolve to an Organization")
			}
			return nil
		},
	}
	client := NewClientWithGraphQL(mock)

	_, err := client.GetProject("someowner", 5)
	if err == nil {
		t.Fatal("expected an error when both paths fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "user-path transient boom") {
		t.Errorf("expected the user-path error to be preserved, got: %v", msg)
	}
	if !strings.Contains(msg, "Could not resolve to an Organization") {
		t.Errorf("expected the org-path error to be preserved, got: %v", msg)
	}
}

// ============================================================================
// #860: pagination (AC1/AC5)
// ============================================================================

// fillConnectionPage populates a GraphQL connection reflect.Value (a struct with
// Nodes and PageInfo fields) with `count` zero-valued nodes and the given
// PageInfo, so pagination-loop tests can drive multi-page responses without
// spelling out every node field.
func fillConnectionPage(conn reflect.Value, count int, hasNext bool, cursor string) {
	nodes := conn.FieldByName("Nodes")
	nodes.Set(reflect.MakeSlice(nodes.Type(), count, count))
	pi := conn.FieldByName("PageInfo")
	pi.FieldByName("HasNextPage").SetBool(hasNext)
	pi.FieldByName("EndCursor").SetString(cursor)
}

func TestGetOpenIssuesByLabels_Pagination(t *testing.T) {
	calls := 0
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name != "GetIssuesByLabels" {
				return nil
			}
			calls++
			conn := reflect.ValueOf(query).Elem().FieldByName("Repository").FieldByName("Issues")
			if calls == 1 {
				fillConnectionPage(conn, 100, true, "c1")
			} else {
				fillConnectionPage(conn, 20, false, "")
			}
			return nil
		},
	}
	client := NewClientWithGraphQL(mock)
	issues, err := client.GetOpenIssuesByLabels("owner", "repo", []string{"branch"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 paginated calls, got %d", calls)
	}
	if len(issues) != 120 {
		t.Errorf("expected 120 issues across 2 pages, got %d", len(issues))
	}
}

func TestGetIssueComments_Pagination(t *testing.T) {
	calls := 0
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name != "GetIssueComments" {
				return nil
			}
			calls++
			conn := reflect.ValueOf(query).Elem().FieldByName("Repository").FieldByName("Issue").FieldByName("Comments")
			if calls == 1 {
				fillConnectionPage(conn, 100, true, "c1")
			} else {
				fillConnectionPage(conn, 5, false, "")
			}
			return nil
		},
	}
	client := NewClientWithGraphQL(mock)
	comments, err := client.GetIssueComments("owner", "repo", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 paginated calls, got %d", calls)
	}
	if len(comments) != 105 {
		t.Errorf("expected 105 comments across 2 pages, got %d", len(comments))
	}
}

func TestListUserProjects_Pagination(t *testing.T) {
	calls := 0
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name != "ListUserProjects" {
				return nil
			}
			calls++
			conn := reflect.ValueOf(query).Elem().FieldByName("User").FieldByName("ProjectsV2")
			if calls == 1 {
				fillConnectionPage(conn, 100, true, "c1")
			} else {
				fillConnectionPage(conn, 7, false, "")
			}
			return nil
		},
	}
	client := NewClientWithGraphQL(mock)
	projects, err := client.listUserProjects("someuser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 paginated calls, got %d", calls)
	}
	if len(projects) != 107 {
		t.Errorf("expected 107 projects across 2 pages, got %d", len(projects))
	}
}

func TestListOrgProjects_Pagination(t *testing.T) {
	calls := 0
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name != "ListOrgProjects" {
				return nil
			}
			calls++
			conn := reflect.ValueOf(query).Elem().FieldByName("Organization").FieldByName("ProjectsV2")
			if calls == 1 {
				fillConnectionPage(conn, 100, true, "c1")
			} else {
				fillConnectionPage(conn, 3, false, "")
			}
			return nil
		},
	}
	client := NewClientWithGraphQL(mock)
	projects, err := client.listOrgProjects("someorg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 paginated calls, got %d", calls)
	}
	if len(projects) != 103 {
		t.Errorf("expected 103 projects across 2 pages, got %d", len(projects))
	}
}

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what was
// written. Used to assert #860 truncation warnings.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)
	return string(out)
}

// TestGetProjectItemIDForIssue_TruncationWarning covers #860 AC2: when the issue
// belongs to more than the fetched cap of project items and the target is not
// among them, a truncation warning is emitted so the "not in the project" result
// is not silently misleading.
func TestGetProjectItemIDForIssue_TruncationWarning(t *testing.T) {
	mock := &queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			if name != "GetProjectItemIDForIssue" {
				return nil
			}
			conn := reflect.ValueOf(query).Elem().FieldByName("Repository").FieldByName("Issue").FieldByName("ProjectItems")
			// One item, in a different project, and there are more pages.
			nodes := conn.FieldByName("Nodes")
			nodes.Set(reflect.MakeSlice(nodes.Type(), 1, 1))
			nodes.Index(0).FieldByName("ID").SetString("PVTI_other")
			nodes.Index(0).FieldByName("Project").FieldByName("ID").SetString("PVT_other")
			conn.FieldByName("PageInfo").FieldByName("HasNextPage").SetBool(true)
			return nil
		},
	}
	client := NewClientWithGraphQL(mock)

	var id string
	var callErr error
	stderr := captureStderr(t, func() {
		id, callErr = client.GetProjectItemIDForIssue("PVT_configured", "owner", "repo", 7)
	})

	if callErr == nil {
		t.Fatal("expected a 'not in the project' error")
	}
	if id != "" {
		t.Errorf("expected empty item id, got %q", id)
	}
	if !strings.Contains(stderr, "Warning") || !strings.Contains(stderr, "more than 20 projects") {
		t.Errorf("expected a truncation warning on stderr, got: %q", stderr)
	}
}
