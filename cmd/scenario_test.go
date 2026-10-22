package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
)

// Command-level scenario tests (#884).
//
// These drive the real command stack — run* -> config -> api.Client -> real
// shurcooL GraphQL plumbing -> redirectTransport -> mock GraphQL server — and
// assert on rendered output. That is the layer the interface-level mocks in
// list_test.go / view_test.go cannot reach: those substitute the client, so
// they never exercise query construction or response decoding.
//
// The wrapper tests in wrapper_test.go configure no responses and assert only
// "not a config error", so they pass against an empty mock. Everything here
// feeds realistic fixtures and asserts on content.
//
// No live GitHub API calls: setupTestEnvironment installs a redirectTransport
// pointing at an httptest server (#884 AC5).

// ---------------------------------------------------------------------------
// Fixture builders
// ---------------------------------------------------------------------------

// gqlData wraps a payload in the GraphQL {"data": ...} envelope.
func gqlData(payload map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"data": payload}
}

// userProjectFixture is the GetUserProject response. setupTestEnvironment
// writes a config with owner "test-org" / number 1, and GetProject tries the
// user projectV2 first, so this short-circuits before getOrgProject.
func userProjectFixture(projectID, title string) map[string]interface{} {
	return gqlData(map[string]interface{}{
		"user": map[string]interface{}{
			"projectV2": map[string]interface{}{
				"id":     projectID,
				"number": 1,
				"title":  title,
				"url":    "https://github.com/orgs/test-org/projects/1",
				"closed": false,
			},
		},
	})
}

// issueNode builds one node of a SearchIssues response. The inline fragment
// (`... on Issue`) flattens into the node object, so these fields sit
// alongside __typename rather than nested under it.
func issueNode(number int, title, state string, labels, assignees []string) map[string]interface{} {
	labelNodes := make([]interface{}, 0, len(labels))
	for _, l := range labels {
		labelNodes = append(labelNodes, map[string]interface{}{"name": l, "color": "ededed"})
	}
	assigneeNodes := make([]interface{}, 0, len(assignees))
	for _, a := range assignees {
		assigneeNodes = append(assigneeNodes, map[string]interface{}{"login": a})
	}

	return map[string]interface{}{
		"__typename": "Issue",
		"id":         "issue-node-id",
		"number":     number,
		"title":      title,
		"body":       "",
		"state":      state,
		"url":        "https://github.com/test-org/test-repo/issues/" + strconv.Itoa(number),
		"repository": map[string]interface{}{"nameWithOwner": "test-org/test-repo"},
		"author":     map[string]interface{}{"login": "author-login"},
		"assignees":  map[string]interface{}{"nodes": assigneeNodes},
		"labels":     map[string]interface{}{"nodes": labelNodes},
		"milestone":  nil,
	}
}

// searchIssuesFixture is the SearchIssues response carrying the given nodes.
func searchIssuesFixture(nodes ...map[string]interface{}) map[string]interface{} {
	asAny := make([]interface{}, 0, len(nodes))
	for _, n := range nodes {
		asAny = append(asAny, n)
	}
	return gqlData(map[string]interface{}{
		"search": map[string]interface{}{
			"nodes":    asAny,
			"pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": ""},
		},
	})
}

// batchIssueLookupFixture is the response to the aliased batch document that
// move issues to resolve issues and their project items:
//
//	query { r0: repository(...) { i0_0: issue(number: 42) { ... } } }
//
// It carries no operation name, so scenarios must register it with
// respondToQueryContaining.
func batchIssueLookupFixture(number int, title, state, projectID, itemID, status string) map[string]interface{} {
	fieldValues := []interface{}{}
	if status != "" {
		fieldValues = append(fieldValues, map[string]interface{}{
			"__typename": "ProjectV2ItemFieldSingleSelectValue",
			"name":       status,
			"field":      map[string]interface{}{"name": "Status"},
		})
	}

	return gqlData(map[string]interface{}{
		"r0": map[string]interface{}{
			"i0_0": map[string]interface{}{
				"id":         "issue-" + strconv.Itoa(number),
				"number":     number,
				"title":      title,
				"body":       "",
				"state":      state,
				"url":        "https://github.com/test-org/test-repo/issues/" + strconv.Itoa(number),
				"repository": map[string]interface{}{"nameWithOwner": "test-org/test-repo"},
				"assignees":  map[string]interface{}{"nodes": []interface{}{}},
				"labels":     map[string]interface{}{"nodes": []interface{}{}},
				"projectItems": map[string]interface{}{"nodes": []interface{}{
					map[string]interface{}{
						"id":          itemID,
						"project":     map[string]interface{}{"id": projectID},
						"fieldValues": map[string]interface{}{"nodes": fieldValues},
					},
				}},
			},
		},
	})
}

// projectFieldsFixture is the GetProjectFields response, giving move the
// Status field id and its selectable options.
func projectFieldsFixture() map[string]interface{} {
	return gqlData(map[string]interface{}{
		"node": map[string]interface{}{
			"fields": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"__typename": "ProjectV2SingleSelectField",
						"id":         "field-status",
						"name":       "Status",
						"dataType":   "SINGLE_SELECT",
						"options": []interface{}{
							map[string]interface{}{"id": "opt-backlog", "name": "Backlog"},
							map[string]interface{}{"id": "opt-inprog", "name": "In Progress"},
							map[string]interface{}{"id": "opt-done", "name": "Done"},
						},
					},
				},
			},
		},
	})
}

// ---------------------------------------------------------------------------
// create scenarios (mutation path)
// ---------------------------------------------------------------------------

// TestScenario_Create_CreatesIssueAndAppliesProjectFields drives the full
// create chain: resolve repo id -> CreateIssue -> look up project ->
// AddProjectV2ItemById -> UpdateProjectV2ItemFieldValue with the resolved
// status option.
//
// Assertions are on the mutation payloads rather than rendered output:
// runCreate reports via fmt.Printf to os.Stdout (create.go), not
// cmd.OutOrStdout(), so its output is not capturable through the cobra writer
// — the same limitation #871 tracks for triage. The payloads are the stronger
// assertion regardless: they are what actually reaches GitHub.
func TestScenario_Create_CreatesIssueAndAppliesProjectFields(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetRepositoryID", gqlData(map[string]interface{}{
		"repository": map[string]interface{}{"id": "repo-node-1"},
	}))
	handler.respondTo("CreateIssue", gqlData(map[string]interface{}{
		"createIssue": map[string]interface{}{
			"issue": map[string]interface{}{
				"id":     "issue-node-1",
				"number": 77,
				"title":  "Scenario created issue",
				"url":    "https://github.com/test-org/test-repo/issues/77",
			},
		},
	}))
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Test Project"))
	handler.respondTo("GetProjectFields", projectFieldsFixture())
	handler.respondTo("AddProjectV2ItemById", gqlData(map[string]interface{}{
		"addProjectV2ItemById": map[string]interface{}{
			"item": map[string]interface{}{"id": "item-new-1"},
		},
	}))
	// Shape matches the mutation's selection set (clientMutationId). A field
	// write that fails to decode is a partial success since #929, so this
	// fixture must be what the client actually unmarshals.
	handler.respondTo("UpdateProjectV2ItemFieldValue", gqlData(map[string]interface{}{
		"updateProjectV2ItemFieldValue": map[string]interface{}{"clientMutationId": nil},
	}))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newCreateCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &createOptions{
		title:  "Scenario created issue",
		body:   "Scenario body",
		status: "in_progress",
	}
	if err := runCreate(cmd, opts); err != nil {
		t.Fatalf("runCreate() error = %v", err)
	}

	// The issue itself must carry the title and body through to the mutation.
	created := handler.requestsFor("CreateIssue")
	if len(created) != 1 {
		t.Fatalf("expected exactly one CreateIssue mutation, got %d (ops: %v)",
			len(created), handler.operationNames())
	}
	input, ok := created[0].Variables["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected input object, got %v", created[0].Variables)
	}
	if input["title"] != "Scenario created issue" {
		t.Errorf("expected title to reach the mutation, got %v", input["title"])
	}
	if input["body"] != "Scenario body" {
		t.Errorf("expected body to reach the mutation, got %v", input["body"])
	}
	if input["repositoryId"] != "repo-node-1" {
		t.Errorf("expected the resolved repository id, got %v", input["repositoryId"])
	}

	// The new issue must be added to the configured project.
	added := handler.requestsFor("AddProjectV2ItemById")
	if len(added) != 1 {
		t.Fatalf("expected the issue to be added to the project once, got %d", len(added))
	}
	addInput, _ := added[0].Variables["input"].(map[string]interface{})
	if addInput["projectId"] != "proj-1" {
		t.Errorf("expected projectId 'proj-1', got %v", addInput["projectId"])
	}
	if addInput["contentId"] != "issue-node-1" {
		t.Errorf("expected the created issue's node id as contentId, got %v", addInput["contentId"])
	}

	// And the status alias must resolve to the option id, as in move.
	updates := handler.requestsFor("UpdateProjectV2ItemFieldValue")
	if len(updates) == 0 {
		t.Fatal("expected a field update for --status")
	}
	upInput, _ := updates[0].Variables["input"].(map[string]interface{})
	value, ok := upInput["value"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected value object, got %v", upInput["value"])
	}
	if value["singleSelectOptionId"] != "opt-inprog" {
		t.Errorf("expected 'in_progress' to resolve to option id 'opt-inprog', got %v",
			value["singleSelectOptionId"])
	}
}

// TestScenario_Create_NoStatusSkipsFieldUpdate covers the branch where no
// project field is requested: the issue is still created and tracked, but no
// field mutation is sent.
func TestScenario_Create_NoStatusSkipsFieldUpdate(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetRepositoryID", gqlData(map[string]interface{}{
		"repository": map[string]interface{}{"id": "repo-node-1"},
	}))
	handler.respondTo("CreateIssue", gqlData(map[string]interface{}{
		"createIssue": map[string]interface{}{
			"issue": map[string]interface{}{
				"id":     "issue-node-1",
				"number": 78,
				"title":  "No status",
				"url":    "https://github.com/test-org/test-repo/issues/78",
			},
		},
	}))
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Test Project"))
	handler.respondTo("GetProjectFields", projectFieldsFixture())
	handler.respondTo("AddProjectV2ItemById", gqlData(map[string]interface{}{
		"addProjectV2ItemById": map[string]interface{}{
			"item": map[string]interface{}{"id": "item-new-2"},
		},
	}))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newCreateCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runCreate(cmd, &createOptions{title: "No status", body: "b"}); err != nil {
		t.Fatalf("runCreate() error = %v", err)
	}

	if got := handler.requestsFor("CreateIssue"); len(got) != 1 {
		t.Errorf("expected the issue to still be created, got %d CreateIssue calls", len(got))
	}
	if got := handler.requestsFor("UpdateProjectV2ItemFieldValue"); len(got) != 0 {
		t.Errorf("expected no field update without --status, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// No-live-traffic guard (#884 AC5)
// ---------------------------------------------------------------------------

// TestScenario_AllTrafficReachesMockServer is the empirical counterpart to the
// structural guarantee that setupTestEnvironment installs a redirectTransport:
// it proves the command's requests actually arrived at the mock rather than
// leaving the process. If the transport hook regressed, the command would talk
// to api.github.com and the handler would record nothing.
func TestScenario_AllTrafficReachesMockServer(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Test Project"))
	handler.respondTo("SearchIssues", searchIssuesFixture(
		issueNode(101, "Tracked", "OPEN", nil, nil),
	))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runList(cmd, &listOptions{}); err != nil {
		t.Fatalf("runList() error = %v", err)
	}

	if len(handler.operationNames()) == 0 {
		t.Fatal("mock server received no requests — traffic escaped the test transport")
	}
	// A successful render proves the response came from the mock: no live
	// endpoint would return this fixture's data for project test-org/1.
	if !strings.Contains(buf.String(), "Tracked") {
		t.Errorf("expected the mock fixture to drive output, got:\n%s", buf.String())
	}
}

// ---------------------------------------------------------------------------
// Harness self-tests
// ---------------------------------------------------------------------------

func TestGraphQLOperationName(t *testing.T) {
	tests := []struct {
		query string
		want  string
	}{
		{"query GetUserProject($number:Int!$owner:String!){user{id}}", "GetUserProject"},
		{"mutation CreateIssue($input:CreateIssueInput!){createIssue{id}}", "CreateIssue"},
		{"  query   GetIssue($n:Int!){x}", "GetIssue"},
		{"query { n0: node(id: \"abc\") { id } }", ""}, // anonymous batch query
		{"{ viewer { login } }", ""},                   // shorthand
		{"", ""},
	}

	for _, tt := range tests {
		if got := graphQLOperationName(tt.query); got != tt.want {
			t.Errorf("graphQLOperationName(%q) = %q, want %q", tt.query, got, tt.want)
		}
	}
}

// TestMockHandler_ExactOperationMatch pins the reason matching is exact rather
// than substring: GetIssue is a prefix of GetIssueComments, so a substring
// matcher could serve the wrong fixture depending on map iteration order.
func TestMockHandler_ExactOperationMatch(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetIssue", gqlData(map[string]interface{}{"marker": "issue"}))
	handler.respondTo("GetIssueComments", gqlData(map[string]interface{}{"marker": "comments"}))

	got := decodeHandlerResponse(t, handler, "query GetIssueComments($n:Int!){x}")
	if got["marker"] != "comments" {
		t.Errorf("expected GetIssueComments fixture, got %v", got)
	}

	got = decodeHandlerResponse(t, handler, "query GetIssue($n:Int!){x}")
	if got["marker"] != "issue" {
		t.Errorf("expected GetIssue fixture, got %v", got)
	}
}

func TestMockHandler_SubstringMatcherForAnonymousQuery(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetIssue", gqlData(map[string]interface{}{"marker": "named"}))
	handler.respondToQueryContaining("n0: node(id:", gqlData(map[string]interface{}{"marker": "batch"}))

	got := decodeHandlerResponse(t, handler, `query { n0: node(id: "abc") { id } }`)
	if got["marker"] != "batch" {
		t.Errorf("expected anonymous batch fixture, got %v", got)
	}
}

func TestMockHandler_FallsBackToDefaultResponse(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.defaultResponse = gqlData(map[string]interface{}{"marker": "default"})

	got := decodeHandlerResponse(t, handler, "query SomethingUnregistered($n:Int!){x}")
	if got["marker"] != "default" {
		t.Errorf("expected default fixture, got %v", got)
	}
}

// decodeHandlerResponse posts a query at the handler and returns the decoded
// "data" object.
func decodeHandlerResponse(t *testing.T, h *mockGraphQLHandler, query string) map[string]interface{} {
	t.Helper()

	body, err := json.Marshal(graphQLRequest{Query: query})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/graphql", bytes.NewReader(body))
	h.ServeHTTP(rec, req)

	var envelope map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to decode handler response: %v (body %s)", err, rec.Body.String())
	}
	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("response has no data object: %s", rec.Body.String())
	}
	return data
}

// ---------------------------------------------------------------------------
// list scenarios
// ---------------------------------------------------------------------------

// TestScenario_List_RendersIssuesFromProject drives runList through the real
// GraphQL stack and asserts the rendered table contains the issues the server
// returned. The wrapper-test equivalent (TestRunList_LoadsConfig) passes
// against an empty mock and asserts nothing about output.
func TestScenario_List_RendersIssuesFromProject(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Test Project"))
	handler.respondTo("SearchIssues", searchIssuesFixture(
		issueNode(101, "First tracked issue", "OPEN", []string{"bug"}, []string{"alice"}),
		issueNode(102, "Second tracked issue", "OPEN", []string{"enhancement"}, nil),
	))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runList(cmd, &listOptions{}); err != nil {
		t.Fatalf("runList() error = %v (output %q)", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "First tracked issue") {
		t.Errorf("expected output to contain first issue title, got:\n%s", out)
	}
	if !strings.Contains(out, "Second tracked issue") {
		t.Errorf("expected output to contain second issue title, got:\n%s", out)
	}
	if strings.Contains(out, "No issues found") {
		t.Errorf("expected rendered issues, got the empty-state message:\n%s", out)
	}

	// The command must have gone through the project lookup before searching.
	ops := handler.operationNames()
	if len(ops) < 2 || ops[0] != "GetUserProject" {
		t.Errorf("expected GetUserProject first, got %v", ops)
	}
}

// TestScenario_List_EmptyProjectRendersEmptyState is the counterpart: with the
// same wiring but no issues, the command reports the empty state rather than
// erroring.
func TestScenario_List_EmptyProjectRendersEmptyState(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Test Project"))
	handler.respondTo("SearchIssues", searchIssuesFixture())

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runList(cmd, &listOptions{}); err != nil {
		t.Fatalf("runList() error = %v", err)
	}

	if !strings.Contains(buf.String(), "No issues found") {
		t.Errorf("expected empty-state message, got:\n%s", buf.String())
	}
}

// TestScenario_List_SearchQueryCarriesRepoAndState asserts the query string the
// command sends upstream — the part interface-level mocks skip entirely.
func TestScenario_List_SearchQueryCarriesRepoAndState(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Test Project"))
	handler.respondTo("SearchIssues", searchIssuesFixture(
		issueNode(101, "Tracked", "OPEN", nil, nil),
	))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runList(cmd, &listOptions{}); err != nil {
		t.Fatalf("runList() error = %v", err)
	}

	reqs := handler.requestsFor("SearchIssues")
	if len(reqs) == 0 {
		t.Fatal("expected a SearchIssues request")
	}
	q, _ := reqs[0].Variables["query"].(string)
	if !strings.Contains(q, "repo:test-org/test-repo") {
		t.Errorf("expected search query scoped to the configured repo, got %q", q)
	}
	if !strings.Contains(q, "is:issue") {
		t.Errorf("expected search query restricted to issues, got %q", q)
	}
}

// ---------------------------------------------------------------------------
// view scenarios
// ---------------------------------------------------------------------------

// issueWithProjectFieldsFixture is the GetIssueWithProjectFields response.
// status/priority arrive as project single-select field values.
func issueWithProjectFieldsFixture(number int, title, body, state, status string) map[string]interface{} {
	fieldValues := []interface{}{
		map[string]interface{}{
			"__typename": "ProjectV2ItemFieldSingleSelectValue",
			"name":       status,
			"field":      map[string]interface{}{"name": "Status"},
		},
	}

	return gqlData(map[string]interface{}{
		"repository": map[string]interface{}{
			"issue": map[string]interface{}{
				"id":        "issue-node-id",
				"number":    number,
				"title":     title,
				"body":      body,
				"state":     state,
				"url":       "https://github.com/test-org/test-repo/issues/" + strconv.Itoa(number),
				"author":    map[string]interface{}{"login": "author-login"},
				"assignees": map[string]interface{}{"nodes": []interface{}{map[string]interface{}{"login": "alice"}}},
				"labels": map[string]interface{}{"nodes": []interface{}{
					map[string]interface{}{"name": "bug", "color": "d73a4a"},
				}},
				"milestone": nil,
				"projectItems": map[string]interface{}{"nodes": []interface{}{
					map[string]interface{}{"fieldValues": map[string]interface{}{"nodes": fieldValues}},
				}},
			},
		},
	})
}

// issueMultiProjectFixture is a GetIssueWithProjectFields response where the
// issue belongs to two project boards, each with its own Status. Only the
// configured board's value must survive filtering (#856).
func issueMultiProjectFixture(number int, title, configuredProjectID, configuredStatus, foreignProjectID, foreignStatus string) map[string]interface{} {
	return gqlData(map[string]interface{}{
		"repository": map[string]interface{}{
			"issue": map[string]interface{}{
				"id":        "issue-node-id",
				"number":    number,
				"title":     title,
				"body":      "multi-board body",
				"state":     "OPEN",
				"url":       "https://github.com/test-org/test-repo/issues/" + strconv.Itoa(number),
				"author":    map[string]interface{}{"login": "author-login"},
				"assignees": map[string]interface{}{"nodes": []interface{}{}},
				"labels":    map[string]interface{}{"nodes": []interface{}{}},
				"milestone": nil,
				"projectItems": map[string]interface{}{"nodes": []interface{}{
					map[string]interface{}{
						"project": map[string]interface{}{"id": configuredProjectID},
						"fieldValues": map[string]interface{}{"nodes": []interface{}{
							map[string]interface{}{
								"__typename": "ProjectV2ItemFieldSingleSelectValue",
								"name":       configuredStatus,
								"field":      map[string]interface{}{"name": "Status"},
							},
						}},
					},
					map[string]interface{}{
						"project": map[string]interface{}{"id": foreignProjectID},
						"fieldValues": map[string]interface{}{"nodes": []interface{}{
							map[string]interface{}{
								"__typename": "ProjectV2ItemFieldSingleSelectValue",
								"name":       foreignStatus,
								"field":      map[string]interface{}{"name": "Status"},
							},
						}},
					},
				}},
			},
		},
	})
}

// noParentFixture / noSubIssuesFixture are the "issue stands alone" responses
// that view issues after the main lookup.
func noParentFixture() map[string]interface{} {
	return gqlData(map[string]interface{}{
		"repository": map[string]interface{}{
			"issue": map[string]interface{}{"parent": nil},
		},
	})
}

func subIssuesFixture(nodes ...map[string]interface{}) map[string]interface{} {
	asAny := make([]interface{}, 0, len(nodes))
	for _, n := range nodes {
		asAny = append(asAny, n)
	}
	return gqlData(map[string]interface{}{
		"repository": map[string]interface{}{
			"issue": map[string]interface{}{
				"subIssues": map[string]interface{}{
					"nodes":    asAny,
					"pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": ""},
				},
			},
		},
	})
}

func subIssueNode(number int, title, state string) map[string]interface{} {
	return map[string]interface{}{
		"id":     "sub-" + strconv.Itoa(number),
		"number": number,
		"title":  title,
		"state":  state,
		"url":    "https://github.com/test-org/test-repo/issues/" + strconv.Itoa(number),
		"repository": map[string]interface{}{
			"name":  "test-repo",
			"owner": map[string]interface{}{"login": "test-org"},
		},
	}
}

// TestScenario_View_RendersIssueDetail drives runView through the real stack.
// The wrapper equivalent (TestRunView_LoadsConfig) renders " #0 / State: /
// URL:" against an empty mock and passes.
func TestScenario_View_RendersIssueDetail(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetIssueWithProjectFields",
		issueWithProjectFieldsFixture(42, "Scenario issue title", "Scenario issue body", "OPEN", "In Progress"))
	handler.respondTo("GetParentIssue", noParentFixture())
	handler.respondTo("GetSubIssues", subIssuesFixture())

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newViewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runView(cmd, []string{"42"}, &viewOptions{}); err != nil {
		t.Fatalf("runView() error = %v (output %q)", err, buf.String())
	}

	out := buf.String()
	for _, want := range []string{"Scenario issue title", "#42", "Scenario issue body", "OPEN"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected view output to contain %q, got:\n%s", want, out)
		}
	}
	// Project field values must be resolved from projectItems, not dropped.
	if !strings.Contains(out, "In Progress") {
		t.Errorf("expected view output to show the Status field value, got:\n%s", out)
	}
}

// TestScenario_View_FiltersForeignProjectFields covers #856 end-to-end: when an
// issue is on two boards, `gh pmu view` must show only the configured project's
// Status. runView resolves the configured project id (via GetUserProject) and
// threads it into GetIssueWithProjectFields, which drops the foreign board's
// item.
func TestScenario_View_FiltersForeignProjectFields(t *testing.T) {
	handler := newMockGraphQLHandler()
	// GetProject resolves to the configured board via the user-project path.
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Test Project"))
	handler.respondTo("GetIssueWithProjectFields",
		issueMultiProjectFixture(42, "Multi-board issue", "proj-1", "In Progress", "proj-foreign", "Done"))
	handler.respondTo("GetParentIssue", noParentFixture())
	handler.respondTo("GetSubIssues", subIssuesFixture())

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newViewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runView(cmd, []string{"42"}, &viewOptions{}); err != nil {
		t.Fatalf("runView() error = %v (output %q)", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "In Progress") {
		t.Errorf("expected the configured project's Status to render, got:\n%s", out)
	}
	if strings.Contains(out, "Done") {
		t.Errorf("foreign project's Status leaked into view output:\n%s", out)
	}
}

// TestScenario_View_JSONOutputDecodes asserts the --json path emits decodable
// JSON carrying the fetched issue.
func TestScenario_View_JSONOutputDecodes(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetIssueWithProjectFields",
		issueWithProjectFieldsFixture(42, "JSON scenario", "body text", "OPEN", "Backlog"))
	handler.respondTo("GetParentIssue", noParentFixture())
	handler.respondTo("GetSubIssues", subIssuesFixture())

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newViewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runView(cmd, []string{"42"}, &viewOptions{jsonFields: "number,title,state"}); err != nil {
		t.Fatalf("runView() error = %v (output %q)", err, buf.String())
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("expected decodable JSON, got error %v for output:\n%s", err, buf.String())
	}
	if int(decoded["number"].(float64)) != 42 {
		t.Errorf("expected number 42, got %v", decoded["number"])
	}
	if decoded["title"] != "JSON scenario" {
		t.Errorf("expected title 'JSON scenario', got %v", decoded["title"])
	}
}

// TestScenario_View_RendersSubIssueProgress covers the parent path: sub-issues
// present, progress derived from their state.
func TestScenario_View_RendersSubIssueProgress(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetIssueWithProjectFields",
		issueWithProjectFieldsFixture(10, "Parent epic", "epic body", "OPEN", "In Progress"))
	handler.respondTo("GetParentIssue", noParentFixture())
	handler.respondTo("GetSubIssues", subIssuesFixture(
		subIssueNode(11, "Done child", "CLOSED"),
		subIssueNode(12, "Open child", "OPEN"),
	))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newViewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runView(cmd, []string{"10"}, &viewOptions{}); err != nil {
		t.Fatalf("runView() error = %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Done child") || !strings.Contains(out, "Open child") {
		t.Errorf("expected both sub-issues listed, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// sub lifecycle scenarios
// ---------------------------------------------------------------------------

func issueFixture(number int, title, state string) map[string]interface{} {
	return gqlData(map[string]interface{}{
		"repository": map[string]interface{}{
			"issue": map[string]interface{}{
				"id":        "issue-" + strconv.Itoa(number),
				"number":    number,
				"title":     title,
				"body":      "",
				"state":     state,
				"url":       "https://github.com/test-org/test-repo/issues/" + strconv.Itoa(number),
				"author":    map[string]interface{}{"login": "author-login"},
				"assignees": map[string]interface{}{"nodes": []interface{}{}},
				"labels":    map[string]interface{}{"nodes": []interface{}{}},
				"milestone": nil,
			},
		},
	})
}

// TestScenario_SubList_RendersChildren drives runSubList end to end. Note this
// relies on exact operation matching: the flow issues GetIssue *and*
// GetSubIssues, and GetIssue is a prefix of other operation names.
func TestScenario_SubList_RendersChildren(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetIssue", issueFixture(10, "Parent epic", "OPEN"))
	handler.respondTo("GetSubIssues", subIssuesFixture(
		subIssueNode(11, "First child", "CLOSED"),
		subIssueNode(12, "Second child", "OPEN"),
	))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &subListOptions{state: "all", relation: "children"}
	if err := runSubList(cmd, []string{"10"}, opts); err != nil {
		t.Fatalf("runSubList() error = %v (output %q)", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Parent epic") {
		t.Errorf("expected parent title in output, got:\n%s", out)
	}
	if !strings.Contains(out, "First child") || !strings.Contains(out, "Second child") {
		t.Errorf("expected both children listed, got:\n%s", out)
	}
	if strings.Contains(out, "No sub-issues found") {
		t.Errorf("expected children, got the empty-state message:\n%s", out)
	}

	// Exact matching must have routed both operations to distinct fixtures.
	ops := handler.operationNames()
	if len(ops) < 2 || ops[0] != "GetIssue" || ops[1] != "GetSubIssues" {
		t.Errorf("expected GetIssue then GetSubIssues, got %v", ops)
	}
}

// ---------------------------------------------------------------------------
// move scenarios (mutation path)
// ---------------------------------------------------------------------------

// TestScenario_Move_ResolvesAliasAndSubmitsMutation is the highest-value
// scenario here: it drives the whole write chain — config alias "in_progress"
// -> field value "In Progress" -> single-select option id "opt-inprog" -> the
// BatchUpdate mutation payload. Interface-level mocks stop at the client
// boundary and never verify the option id actually submitted.
func TestScenario_Move_ResolvesAliasAndSubmitsMutation(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Test Project"))
	handler.respondToQueryContaining("i0_0: issue(number:",
		batchIssueLookupFixture(42, "Movable issue", "OPEN", "proj-1", "item-1", "Backlog"))
	handler.respondTo("GetProjectFields", projectFieldsFixture())

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newMoveCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runMove(cmd, []string{"42"}, &moveOptions{status: "in_progress"}); err != nil {
		t.Fatalf("runMove() error = %v (output %q)", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Updated issue #42") {
		t.Errorf("expected confirmation for issue #42, got:\n%s", out)
	}
	if !strings.Contains(out, "Status -> In Progress") {
		t.Errorf("expected the resolved status in output, got:\n%s", out)
	}

	// The mutation must carry the resolved option id, not the raw alias.
	reqs := handler.requestsFor("BatchUpdate")
	if len(reqs) != 1 {
		t.Fatalf("expected exactly one BatchUpdate mutation, got %d (ops: %v)",
			len(reqs), handler.operationNames())
	}
	input, ok := reqs[0].Variables["input0"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected input0 object in mutation variables, got %v", reqs[0].Variables)
	}
	if input["fieldId"] != "field-status" {
		t.Errorf("expected fieldId 'field-status', got %v", input["fieldId"])
	}
	if input["itemId"] != "item-1" {
		t.Errorf("expected itemId 'item-1', got %v", input["itemId"])
	}
	if input["projectId"] != "proj-1" {
		t.Errorf("expected projectId 'proj-1', got %v", input["projectId"])
	}
	value, ok := input["value"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected value object, got %v", input["value"])
	}
	if value["singleSelectOptionId"] != "opt-inprog" {
		t.Errorf("expected the 'in_progress' alias to resolve to option id 'opt-inprog', got %v",
			value["singleSelectOptionId"])
	}
}

// TestScenario_Move_UnknownIssueReportsError covers the failure path: the batch
// lookup finds nothing, so no mutation should be attempted.
func TestScenario_Move_UnknownIssueReportsError(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Test Project"))
	handler.respondToQueryContaining("i0_0: issue(number:", gqlData(map[string]interface{}{
		"r0": map[string]interface{}{"i0_0": nil},
	}))
	handler.respondTo("GetProjectFields", projectFieldsFixture())

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newMoveCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := runMove(cmd, []string{"999"}, &moveOptions{status: "in_progress"})
	if err == nil {
		t.Fatalf("expected an error for an unresolvable issue, got nil (output %q)", buf.String())
	}

	if got := handler.requestsFor("BatchUpdate"); len(got) != 0 {
		t.Errorf("expected no mutation when the issue cannot be resolved, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// branch lifecycle scenarios (#887)
// ---------------------------------------------------------------------------
//
// branch_test.go already drives runBranchStartWithDeps / AddWithDeps /
// CloseWithDeps across ~85 tests, but it substitutes the whole client, so it
// asserts what the code *intends* to send. These scenarios drive the real
// api.Client and assert what reaches the wire.

// branchTestConfig adds the "branch" text field the branch commands require;
// defaultTestConfig declares only status and priority. Values mirror the real
// .gh-pmu.json: Branch is a TEXT field, so SetProjectItemField routes through
// setTextField rather than the single-select path Status uses.
const branchTestConfig = `{
  "project": {
    "owner": "test-org",
    "number": 1
  },
  "repositories": ["test-org/test-repo"],
  "fields": {
    "status": {
      "field": "Status",
      "values": {
        "backlog": "Backlog",
        "in_progress": "In Progress",
        "done": "Done",
        "parking_lot": "Parking Lot"
      }
    },
    "branch": {
      "field": "Branch"
    }
  }
}`

// scenarioBranchClient is a real *api.Client — so every GraphQL call builds a
// real document and travels through redirectTransport to the mock server —
// with only the local side-effect methods of branchClient overridden.
//
// branchClient mixes API calls with git and filesystem work
// (GitCheckoutNewBranch, GitTag, GitAdd, WriteFile, MkdirAll). Those are the
// one part of the interface that must not run for real in a test: they would
// touch the working tree rather than the httptest server. Every method that
// talks to GitHub is left to the embedded client, which is the entire point of
// driving a scenario rather than a mock.
type scenarioBranchClient struct {
	*api.Client
	gitBranches []string
	gitTags     []string
}

func (c *scenarioBranchClient) GitCheckoutNewBranch(branch string) error {
	c.gitBranches = append(c.gitBranches, branch)
	return nil
}
func (c *scenarioBranchClient) GitTag(tag, message string) error {
	c.gitTags = append(c.gitTags, tag)
	return nil
}
func (c *scenarioBranchClient) GitAdd(paths ...string) error         { return nil }
func (c *scenarioBranchClient) WriteFile(path, content string) error { return nil }
func (c *scenarioBranchClient) MkdirAll(path string) error           { return nil }

// newScenarioBranchClient builds the client above against the active test
// transport. setupTestEnvironmentWithConfig must have run first.
func newScenarioBranchClient(t *testing.T) *scenarioBranchClient {
	t.Helper()
	real, err := api.NewClient()
	if err != nil {
		t.Fatalf("api.NewClient() error = %v", err)
	}
	return &scenarioBranchClient{Client: real}
}

// loadScenarioConfig loads the config written by setupTestEnvironmentWithConfig
// from the temp cwd, the same way the cobra RunE does.
func loadScenarioConfig(t *testing.T) *config.Config {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}
	cfg, err := config.LoadFromDirectory(cwd)
	if err != nil {
		t.Fatalf("config.LoadFromDirectory() error = %v", err)
	}
	return cfg
}

// branchTrackerNode builds one node of a GetIssuesByLabel response.
func branchTrackerNode(id string, number int, title, state string) map[string]interface{} {
	return map[string]interface{}{
		"id":     id,
		"number": number,
		"title":  title,
		"state":  state,
		"url":    "https://github.com/test-org/test-repo/issues/" + strconv.Itoa(number),
		"labels": map[string]interface{}{"nodes": []interface{}{
			map[string]interface{}{"name": "branch"},
		}},
	}
}

// issuesByLabelFixture is the GetIssuesByLabel response.
func issuesByLabelFixture(nodes ...map[string]interface{}) map[string]interface{} {
	asAny := make([]interface{}, 0, len(nodes))
	for _, n := range nodes {
		asAny = append(asAny, n)
	}
	return gqlData(map[string]interface{}{
		"repository": map[string]interface{}{
			"issues": map[string]interface{}{
				"nodes":    asAny,
				"pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": ""},
			},
		},
	})
}

// branchProjectFieldsFixture is GetProjectFields carrying both the Status
// single-select and the Branch TEXT field, which the branch flows need in
// order to route to setTextField.
func branchProjectFieldsFixture() map[string]interface{} {
	return gqlData(map[string]interface{}{
		"node": map[string]interface{}{
			"fields": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"__typename": "ProjectV2SingleSelectField",
						"id":         "field-status",
						"name":       "Status",
						"dataType":   "SINGLE_SELECT",
						"options": []interface{}{
							map[string]interface{}{"id": "opt-backlog", "name": "Backlog"},
							map[string]interface{}{"id": "opt-inprog", "name": "In Progress"},
							map[string]interface{}{"id": "opt-done", "name": "Done"},
							map[string]interface{}{"id": "opt-parking", "name": "Parking Lot"},
						},
					},
					map[string]interface{}{
						"__typename": "ProjectV2Field",
						"id":         "field-branch",
						"name":       "Branch",
						"dataType":   "TEXT",
					},
				},
				"pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": ""},
			},
		},
	})
}

// projectItemLookupFixture is the response to the GetProjectItemID flavour of
// GetProjectItems.
//
// Note the collision: GetProjectItemID and the list/board path both send an
// operation *named* GetProjectItems, but they select different fields —
// GetProjectItemID asks only for {id, content{... on Issue{id}}}. A fixture
// carrying __typename and issue detail, as the board-shaped response does,
// fails to decode against this query. Same operation name, different document,
// so one fixture cannot serve both; a scenario covering the board path needs
// its own.
func projectItemLookupFixture(itemID, issueID string) map[string]interface{} {
	return gqlData(map[string]interface{}{
		"node": map[string]interface{}{
			"items": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"id": itemID,
						// `... on Issue` flattens into content.
						"content": map[string]interface{}{"id": issueID},
					},
				},
				"pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": ""},
			},
		},
	})
}

// fieldUpdatesFor returns the UpdateProjectV2ItemFieldValue inputs whose
// fieldId matches, so assertions can pick the Status update out of a flow that
// also writes Branch (and vice versa).
func fieldUpdatesFor(t *testing.T, handler *mockGraphQLHandler, fieldID string) []map[string]interface{} {
	t.Helper()
	var out []map[string]interface{}
	for _, r := range handler.requestsFor("UpdateProjectV2ItemFieldValue") {
		input, ok := r.Variables["input"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected input object in field update, got %v", r.Variables)
		}
		if input["fieldId"] == fieldID {
			out = append(out, input)
		}
	}
	return out
}

// TestScenario_BranchLifecycle_StartAddClose drives the whole lifecycle in one
// pass — branch start -> branch add -> branch close — against a single handler
// whose GetIssuesByLabel answer changes once the tracker is created, the way
// GitHub's would. It asserts the tracker issue and the Branch field values that
// actually reach the API at each stage.
func TestScenario_BranchLifecycle_StartAddClose(t *testing.T) {
	handler := newMockGraphQLHandler()

	// GetIssuesByLabel is stateful: no tracker before start creates one, the
	// tracker afterwards. branch start refuses to run when one already exists,
	// and add/close both need to find it.
	trackerExists := false
	handler.respondToFunc("GetIssuesByLabel", func(graphQLRequest) interface{} {
		if !trackerExists {
			return issuesByLabelFixture()
		}
		return issuesByLabelFixture(
			branchTrackerNode("tracker-node-1", 500, "Branch: release/v2.0.0", "OPEN"))
	})
	handler.respondToFunc("CreateIssue", func(graphQLRequest) interface{} {
		trackerExists = true
		return gqlData(map[string]interface{}{
			"createIssue": map[string]interface{}{
				"issue": map[string]interface{}{
					"id":     "tracker-node-1",
					"number": 500,
					"title":  "Branch: release/v2.0.0",
					"body":   "",
					"state":  "OPEN",
					"url":    "https://github.com/test-org/test-repo/issues/500",
				},
			},
		})
	})
	handler.respondTo("GetRepositoryID", gqlData(map[string]interface{}{
		"repository": map[string]interface{}{"id": "repo-node-1"},
	}))
	// The label lookup builds an anonymous aliased document
	// (`query { repository(...) { l0: label(name: "branch") { id } } }`), so it
	// has no operation name to match on — the one substring matcher here.
	// Without it, CreateIssue's label resolution would not find "branch" and
	// production would auto-create the label, adding CreateLabel/GetLabelID
	// round trips that have nothing to do with what this test asserts.
	handler.respondToQueryContaining("l0: label(name:", gqlData(map[string]interface{}{
		"repository": map[string]interface{}{
			"l0": map[string]interface{}{"id": "label-branch"},
		},
	}))
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Test Project"))
	handler.respondTo("AddProjectV2ItemById", gqlData(map[string]interface{}{
		"addProjectV2ItemById": map[string]interface{}{
			"item": map[string]interface{}{"id": "item-tracker-1"},
		},
	}))
	handler.respondTo("GetProjectFields", branchProjectFieldsFixture())
	// SetProjectItemField selects only clientMutationId — returning a
	// projectV2Item object here fails to decode.
	handler.respondTo("UpdateProjectV2ItemFieldValue", gqlData(map[string]interface{}{
		"updateProjectV2ItemFieldValue": map[string]interface{}{"clientMutationId": nil},
	}))
	// branch add looks the issue up by number, then finds its project item.
	handler.respondToFunc("GetIssue", issueByNumberFixture(map[int]string{
		42: "Issue to track on the branch",
	}))
	handler.respondTo("GetProjectItems", projectItemLookupFixture("item-42", "issue-42"))
	// branch close reads the tracker's sub-issues; all closed means nothing to
	// move to backlog, so the close path runs clean.
	handler.respondTo("GetSubIssues", subIssuesFixture(
		subIssueNode(42, "Issue to track on the branch", "CLOSED"),
	))
	handler.respondTo("CloseIssue", gqlData(map[string]interface{}{
		"closeIssue": map[string]interface{}{
			"issue": map[string]interface{}{"id": "tracker-node-1"},
		},
	}))

	_, cleanup := setupTestEnvironmentWithConfig(t, handler, branchTestConfig)
	defer cleanup()

	cfg := loadScenarioConfig(t)
	client := newScenarioBranchClient(t)

	// --- branch start -----------------------------------------------------
	startCmd := newBranchStartCommand()
	var startBuf bytes.Buffer
	startCmd.SetOut(&startBuf)
	startCmd.SetErr(&startBuf)

	err := runBranchStartWithDeps(startCmd, &branchStartOptions{branchName: "release/v2.0.0"}, cfg, client)
	if err != nil {
		t.Fatalf("runBranchStartWithDeps() error = %v (output %q)", err, startBuf.String())
	}

	// The tracker issue must reach the API with the branch-derived title and
	// the branch label attached.
	created := handler.requestsFor("CreateIssue")
	if len(created) != 1 {
		t.Fatalf("expected exactly one CreateIssue mutation, got %d (ops: %v)",
			len(created), handler.operationNames())
	}
	createInput, ok := created[0].Variables["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected input object, got %v", created[0].Variables)
	}
	if createInput["title"] != "Branch: release/v2.0.0" {
		t.Errorf("expected the tracker title to carry the branch name, got %v", createInput["title"])
	}
	if createInput["repositoryId"] != "repo-node-1" {
		t.Errorf("expected the resolved repository id, got %v", createInput["repositoryId"])
	}
	labelIDs, ok := createInput["labelIds"].([]interface{})
	if !ok || len(labelIDs) != 1 || labelIDs[0] != "label-branch" {
		t.Errorf("expected the tracker to carry the resolved 'branch' label id, got %v", createInput["labelIds"])
	}

	// The tracker must be added to the configured project...
	added := handler.requestsFor("AddProjectV2ItemById")
	if len(added) != 1 {
		t.Fatalf("expected the tracker to be added to the project once, got %d", len(added))
	}
	addInput, _ := added[0].Variables["input"].(map[string]interface{})
	if addInput["projectId"] != "proj-1" || addInput["contentId"] != "tracker-node-1" {
		t.Errorf("expected the tracker added to proj-1, got %v", addInput)
	}

	// ...and its Status set to In Progress via the resolved option id.
	statusUpdates := fieldUpdatesFor(t, handler, "field-status")
	if len(statusUpdates) != 1 {
		t.Fatalf("expected exactly one Status update during start, got %d", len(statusUpdates))
	}
	statusValue, ok := statusUpdates[0]["value"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected value object, got %v", statusUpdates[0]["value"])
	}
	if statusValue["singleSelectOptionId"] != "opt-inprog" {
		t.Errorf("expected Status to resolve to option id 'opt-inprog', got %v",
			statusValue["singleSelectOptionId"])
	}
	if statusUpdates[0]["itemId"] != "item-tracker-1" {
		t.Errorf("expected the Status update to target the tracker's item, got %v", statusUpdates[0]["itemId"])
	}

	// The git branch is created locally, not via the API.
	if len(client.gitBranches) != 1 || client.gitBranches[0] != "release/v2.0.0" {
		t.Errorf("expected the git branch to be created once, got %v", client.gitBranches)
	}

	// --- branch add -------------------------------------------------------
	addCmd := newBranchAddCommand()
	var addBuf bytes.Buffer
	addCmd.SetOut(&addBuf)
	addCmd.SetErr(&addBuf)

	if err := runBranchAddWithDeps(addCmd, &branchAddOptions{issueNumber: 42}, cfg, client); err != nil {
		t.Fatalf("runBranchAddWithDeps() error = %v (output %q)", err, addBuf.String())
	}

	// The Branch field is TEXT: the value must reach the API as the branch name
	// extracted from the tracker title, under `text` rather than an option id.
	branchUpdates := fieldUpdatesFor(t, handler, "field-branch")
	if len(branchUpdates) != 1 {
		t.Fatalf("expected exactly one Branch field update during add, got %d", len(branchUpdates))
	}
	if branchUpdates[0]["itemId"] != "item-42" {
		t.Errorf("expected the Branch update to target issue #42's project item, got %v",
			branchUpdates[0]["itemId"])
	}
	branchValue, ok := branchUpdates[0]["value"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected value object, got %v", branchUpdates[0]["value"])
	}
	if branchValue["text"] != "release/v2.0.0" {
		t.Errorf("expected the Branch field to carry the branch name as text, got %v", branchValue["text"])
	}
	if !strings.Contains(addBuf.String(), "Added #42 to release release/v2.0.0") {
		t.Errorf("expected the add confirmation, got:\n%s", addBuf.String())
	}

	// --- branch close -----------------------------------------------------
	closeCmd := newBranchCloseCommand()
	var closeBuf bytes.Buffer
	closeCmd.SetOut(&closeBuf)
	closeCmd.SetErr(&closeBuf)

	closeOpts := &branchCloseOptions{branchName: "release/v2.0.0", yes: true}
	if err := runBranchCloseWithDeps(closeCmd, closeOpts, cfg, client); err != nil {
		t.Fatalf("runBranchCloseWithDeps() error = %v (output %q)", err, closeBuf.String())
	}

	// The tracker issue — not some other issue — must be the one closed.
	closed := handler.requestsFor("CloseIssue")
	if len(closed) != 1 {
		t.Fatalf("expected exactly one CloseIssue mutation, got %d (ops: %v)",
			len(closed), handler.operationNames())
	}
	closeInput, ok := closed[0].Variables["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected input object, got %v", closed[0].Variables)
	}
	if closeInput["issueId"] != "tracker-node-1" {
		t.Errorf("expected the tracker's node id to be closed, got %v", closeInput["issueId"])
	}

	out := closeBuf.String()
	if !strings.Contains(out, "Tracker issue: #500") {
		t.Errorf("expected the tracker number in the close summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Branch closed: release/v2.0.0") {
		t.Errorf("expected the close confirmation, got:\n%s", out)
	}
	// No tag requested, so none should have been cut.
	if len(client.gitTags) != 0 {
		t.Errorf("expected no git tag without --tag, got %v", client.gitTags)
	}
}

// TestScenario_BranchStart_RefusesWhenBranchAlreadyActive covers the guard:
// with a tracker already open, start must not create a second one — and must
// not create the git branch either.
func TestScenario_BranchStart_RefusesWhenBranchAlreadyActive(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetIssuesByLabel", issuesByLabelFixture(
		branchTrackerNode("tracker-node-1", 500, "Branch: release/v1.0.0", "OPEN"),
	))

	_, cleanup := setupTestEnvironmentWithConfig(t, handler, branchTestConfig)
	defer cleanup()

	cfg := loadScenarioConfig(t)
	client := newScenarioBranchClient(t)

	cmd := newBranchStartCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := runBranchStartWithDeps(cmd, &branchStartOptions{branchName: "release/v2.0.0"}, cfg, client)
	if err == nil {
		t.Fatalf("expected an error when a branch is already active, got nil (output %q)", buf.String())
	}
	if !strings.Contains(err.Error(), "active branch exists") {
		t.Errorf("expected an active-branch error, got: %v", err)
	}

	if got := handler.requestsFor("CreateIssue"); len(got) != 0 {
		t.Errorf("expected no tracker issue to be created, got %d", len(got))
	}
	if len(client.gitBranches) != 0 {
		t.Errorf("expected no git branch to be created, got %v", client.gitBranches)
	}
}

// TestScenario_BranchClose_TagsReleaseWhenRequested pins the --tag path: the
// tag is cut, and the tracker is still closed.
//
// It deliberately does NOT claim the tag name is derived from the tracker
// title rather than the flag. extractBranchVersion strips the "Branch: "
// prefix and any " (codename)" suffix, so the extracted version is equal to
// opts.branchName by construction for every title the close matcher accepts —
// no fixture can make the two differ, and a test asserting the distinction
// would be asserting its own arithmetic. The lifecycle test's no-tag-without
// --tag assertion is what gives this flag its teeth.
func TestScenario_BranchClose_TagsReleaseWhenRequested(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetIssuesByLabel", issuesByLabelFixture(
		branchTrackerNode("tracker-node-1", 500, "Branch: release/v2.0.0", "OPEN"),
	))
	handler.respondTo("GetSubIssues", subIssuesFixture(
		subIssueNode(42, "Done work", "CLOSED"),
	))
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Test Project"))
	handler.respondTo("CloseIssue", gqlData(map[string]interface{}{
		"closeIssue": map[string]interface{}{
			"issue": map[string]interface{}{"id": "tracker-node-1"},
		},
	}))

	_, cleanup := setupTestEnvironmentWithConfig(t, handler, branchTestConfig)
	defer cleanup()

	cfg := loadScenarioConfig(t)
	client := newScenarioBranchClient(t)

	cmd := newBranchCloseCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &branchCloseOptions{branchName: "release/v2.0.0", yes: true, tag: true}
	if err := runBranchCloseWithDeps(cmd, opts, cfg, client); err != nil {
		t.Fatalf("runBranchCloseWithDeps() error = %v (output %q)", err, buf.String())
	}

	if len(client.gitTags) != 1 || client.gitTags[0] != "release/v2.0.0" {
		t.Errorf("expected the tag to be cut from the tracker's branch name, got %v", client.gitTags)
	}
	if got := handler.requestsFor("CloseIssue"); len(got) != 1 {
		t.Errorf("expected the tracker to still be closed, got %d CloseIssue calls", len(got))
	}
}

// ---------------------------------------------------------------------------
// sub write scenarios (#887)
// ---------------------------------------------------------------------------
//
// runSubAdd / runSubCreate / runSubRemove take no client dependency and have no
// WithDeps seam, so sub_test.go can only reach their command structure (flags,
// arg validation). Outside the retired //go:build integration tests these paths
// had no automated verification at all — these scenarios are their only
// coverage.

// issueByNumberFixture answers GetIssue per request, keyed on the `number`
// variable, so a flow that looks up several issues gets a distinct node id for
// each. Numbers arrive as float64 after the JSON round-trip through the mock
// server.
//
// Distinct ids are what gives the mutation assertions teeth: with one shared
// fixture, issueId and subIssueId would be equal and a parent/child swap in
// production would go undetected.
func issueByNumberFixture(titlesByNumber map[int]string) func(graphQLRequest) interface{} {
	return func(req graphQLRequest) interface{} {
		raw, ok := req.Variables["number"].(float64)
		if !ok {
			return issueNotFoundFixture(0)
		}
		number := int(raw)
		title, known := titlesByNumber[number]
		if !known {
			return issueNotFoundFixture(number)
		}
		return issueFixture(number, title, "OPEN")
	}
}

// issueNotFoundFixture models GitHub's response for a lookup by a number that
// does not exist: a NOT_FOUND error alongside a null issue node.
//
// The errors envelope is what makes GetIssue fail. A bare {"issue": null} with
// no errors decodes into a zero-value Issue and the caller proceeds with an
// empty node id — runSubAdd will happily send AddSubIssue with subIssueId "".
// The retired live-API test TestRunSubAdd_Integration_ChildNotFound asserted
// "failed to get child issue" against the real API, which is the evidence that
// GitHub errors here rather than returning a bare null.
func issueNotFoundFixture(number int) map[string]interface{} {
	return map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{"issue": nil},
		},
		"errors": []interface{}{
			map[string]interface{}{
				"type": "NOT_FOUND",
				"message": fmt.Sprintf(
					"Could not resolve to an Issue with the number of %d.", number),
			},
		},
	}
}

// TestScenario_SubAdd_LinksChildToParentViaMutation drives runSubAdd end to end
// and asserts the AddSubIssue payload carries the parent's node id as issueId
// and the child's as subIssueId — the direction of the link, which is the whole
// behavior of the command.
func TestScenario_SubAdd_LinksChildToParentViaMutation(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondToFunc("GetIssue", issueByNumberFixture(map[int]string{
		10: "Parent epic",
		15: "Child task",
	}))
	handler.respondTo("AddSubIssue", gqlData(map[string]interface{}{
		"addSubIssue": map[string]interface{}{
			"issue":    map[string]interface{}{"id": "issue-10"},
			"subIssue": map[string]interface{}{"id": "issue-15"},
		},
	}))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubAddCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runSubAdd(cmd, []string{"10", "15"}, &subAddOptions{}); err != nil {
		t.Fatalf("runSubAdd() error = %v (output %q)", err, buf.String())
	}

	reqs := handler.requestsFor("AddSubIssue")
	if len(reqs) != 1 {
		t.Fatalf("expected exactly one AddSubIssue mutation, got %d (ops: %v)",
			len(reqs), handler.operationNames())
	}
	input, ok := reqs[0].Variables["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected input object in mutation variables, got %v", reqs[0].Variables)
	}
	if input["issueId"] != "issue-10" {
		t.Errorf("expected the parent's node id as issueId, got %v", input["issueId"])
	}
	if input["subIssueId"] != "issue-15" {
		t.Errorf("expected the child's node id as subIssueId, got %v", input["subIssueId"])
	}

	out := buf.String()
	if !strings.Contains(out, "Linked issue #15 as sub-issue of #10") {
		t.Errorf("expected the link confirmation, got:\n%s", out)
	}
	if !strings.Contains(out, "Parent epic") || !strings.Contains(out, "Child task") {
		t.Errorf("expected both looked-up titles in output, got:\n%s", out)
	}
}

// TestScenario_SubAdd_ChildLookupFailureSendsNoMutation covers the guard path:
// when an issue cannot be resolved, no link is attempted.
func TestScenario_SubAdd_ChildLookupFailureSendsNoMutation(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondToFunc("GetIssue", issueByNumberFixture(map[int]string{
		10: "Parent epic",
		// 999 deliberately absent — the child lookup returns a null issue.
	}))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubAddCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := runSubAdd(cmd, []string{"10", "999"}, &subAddOptions{})
	if err == nil {
		t.Fatalf("expected an error for an unresolvable child, got nil (output %q)", buf.String())
	}

	if got := handler.requestsFor("AddSubIssue"); len(got) != 0 {
		t.Errorf("expected no link mutation when the child cannot be resolved, got %d", len(got))
	}
}

// issueBareNullFixture answers GetIssue with a null issue node and *no* errors
// envelope for any number absent from titlesByNumber.
//
// This is the shape issueNotFoundFixture deliberately does not model. GitHub
// sends the NOT_FOUND envelope today, and that envelope is the only reason
// GetIssue fails — shurcooL decodes a bare null into a zero-value Issue, so the
// caller proceeds with an empty node id. These scenarios pin the client-side
// guard rather than the server's rejection (#889).
func issueBareNullFixture(titlesByNumber map[int]string) func(graphQLRequest) interface{} {
	nullIssue := func() interface{} {
		return gqlData(map[string]interface{}{
			"repository": map[string]interface{}{"issue": nil},
		})
	}
	return func(req graphQLRequest) interface{} {
		raw, ok := req.Variables["number"].(float64)
		if !ok {
			return nullIssue()
		}
		if title, known := titlesByNumber[int(raw)]; known {
			return issueFixture(int(raw), title, "OPEN")
		}
		return nullIssue()
	}
}

// TestScenario_SubAdd_BareNullChildSendsNoMutation is the regression this issue
// exists for: a null child with no errors envelope must not produce an
// AddSubIssue carrying an empty subIssueId.
func TestScenario_SubAdd_BareNullChildSendsNoMutation(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondToFunc("GetIssue", issueBareNullFixture(map[int]string{
		10: "Parent epic",
		// 999 resolves to a bare null — no errors envelope.
	}))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubAddCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := runSubAdd(cmd, []string{"10", "999"}, &subAddOptions{})
	if err == nil {
		t.Fatalf("expected an error for a bare-null child, got nil (output %q)", buf.String())
	}
	if !strings.Contains(err.Error(), "999") {
		t.Errorf("expected the error to name the unresolved issue number, got %v", err)
	}
	if got := handler.requestsFor("AddSubIssue"); len(got) != 0 {
		t.Errorf("expected no link mutation for a bare-null child, got %d", len(got))
	}
}

// A bare-null parent must be caught before the child issue is created —
// otherwise the command leaves a newly created orphan issue behind.
func TestScenario_SubCreate_BareNullParentCreatesNothing(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondToFunc("GetIssue", issueBareNullFixture(nil))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubCreateCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := runSubCreate(cmd, &subCreateOptions{parent: "10", title: "Orphan task"})
	if err == nil {
		t.Fatalf("expected an error for a bare-null parent, got nil (output %q)", buf.String())
	}
	if !strings.Contains(err.Error(), "10") {
		t.Errorf("expected the error to name the unresolved issue number, got %v", err)
	}
	if got := handler.requestsFor("CreateIssue"); len(got) != 0 {
		t.Errorf("expected no issue to be created for a bare-null parent, got %d", len(got))
	}
	if got := handler.requestsFor("AddSubIssue"); len(got) != 0 {
		t.Errorf("expected no link mutation for a bare-null parent, got %d", len(got))
	}
}

func TestScenario_SubRemove_BareNullChildSendsNoMutation(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondToFunc("GetIssue", issueBareNullFixture(map[int]string{
		10: "Parent epic",
	}))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubRemoveCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := runSubRemove(cmd, []string{"10", "999"}, &subRemoveOptions{})
	if err == nil {
		t.Fatalf("expected an error for a bare-null child, got nil (output %q)", buf.String())
	}
	if got := handler.requestsFor("RemoveSubIssue"); len(got) != 0 {
		t.Errorf("expected no unlink mutation for a bare-null child, got %d", len(got))
	}
}

// TestScenario_SubCreate_CreatesIssueAndLinksToParent drives the full create
// chain: look up parent -> resolve repo id -> CreateIssue -> AddSubIssue with
// the new issue's node id.
func TestScenario_SubCreate_CreatesIssueAndLinksToParent(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondToFunc("GetIssue", issueByNumberFixture(map[int]string{
		10: "Parent epic",
	}))
	handler.respondTo("GetRepositoryID", gqlData(map[string]interface{}{
		"repository": map[string]interface{}{"id": "repo-node-1"},
	}))
	handler.respondTo("CreateIssue", gqlData(map[string]interface{}{
		"createIssue": map[string]interface{}{
			"issue": map[string]interface{}{
				"id":     "issue-node-new",
				"number": 21,
				"title":  "Implement feature X",
				"body":   "Task body",
				"state":  "OPEN",
				"url":    "https://github.com/test-org/test-repo/issues/21",
			},
		},
	}))
	handler.respondTo("AddSubIssue", gqlData(map[string]interface{}{
		"addSubIssue": map[string]interface{}{
			"issue":    map[string]interface{}{"id": "issue-10"},
			"subIssue": map[string]interface{}{"id": "issue-node-new"},
		},
	}))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubCreateCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &subCreateOptions{
		parent: "10",
		title:  "Implement feature X",
		body:   "Task body",
	}
	if err := runSubCreate(cmd, opts); err != nil {
		t.Fatalf("runSubCreate() error = %v (output %q)", err, buf.String())
	}

	// The new issue must carry the requested title and body to the mutation.
	created := handler.requestsFor("CreateIssue")
	if len(created) != 1 {
		t.Fatalf("expected exactly one CreateIssue mutation, got %d (ops: %v)",
			len(created), handler.operationNames())
	}
	createInput, ok := created[0].Variables["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected input object, got %v", created[0].Variables)
	}
	if createInput["title"] != "Implement feature X" {
		t.Errorf("expected the title to reach the mutation, got %v", createInput["title"])
	}
	if createInput["repositoryId"] != "repo-node-1" {
		t.Errorf("expected the resolved repository id, got %v", createInput["repositoryId"])
	}

	// And the newly created issue — not the parent — must be linked as the child.
	linked := handler.requestsFor("AddSubIssue")
	if len(linked) != 1 {
		t.Fatalf("expected exactly one AddSubIssue mutation, got %d (ops: %v)",
			len(linked), handler.operationNames())
	}
	linkInput, ok := linked[0].Variables["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected input object, got %v", linked[0].Variables)
	}
	if linkInput["issueId"] != "issue-10" {
		t.Errorf("expected the parent's node id as issueId, got %v", linkInput["issueId"])
	}
	if linkInput["subIssueId"] != "issue-node-new" {
		t.Errorf("expected the created issue's node id as subIssueId, got %v", linkInput["subIssueId"])
	}

	out := buf.String()
	if !strings.Contains(out, "Created sub-issue #21 under parent #10") {
		t.Errorf("expected the creation confirmation, got:\n%s", out)
	}
}

// TestScenario_SubCreate_LinkFailureStillReportsCreatedIssue pins the partial
// failure contract: the issue exists on GitHub even though the link failed, so
// the command must report its number rather than swallow it.
func TestScenario_SubCreate_LinkFailureStillReportsCreatedIssue(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondToFunc("GetIssue", issueByNumberFixture(map[int]string{
		10: "Parent epic",
	}))
	handler.respondTo("GetRepositoryID", gqlData(map[string]interface{}{
		"repository": map[string]interface{}{"id": "repo-node-1"},
	}))
	handler.respondTo("CreateIssue", gqlData(map[string]interface{}{
		"createIssue": map[string]interface{}{
			"issue": map[string]interface{}{
				"id":     "issue-node-new",
				"number": 22,
				"title":  "Orphan task",
				"url":    "https://github.com/test-org/test-repo/issues/22",
			},
		},
	}))
	// The link mutation fails: GraphQL errors envelope, no data.
	handler.respondTo("AddSubIssue", map[string]interface{}{
		"errors": []interface{}{
			map[string]interface{}{"message": "Sub-issues are not enabled for this repository"},
		},
	})

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubCreateCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := runSubCreate(cmd, &subCreateOptions{parent: "10", title: "Orphan task"})
	if err == nil {
		t.Fatal("expected an error when the sub-issue link fails")
	}

	// The created issue must not be silently lost.
	if !strings.Contains(buf.String(), "Created issue #22") {
		t.Errorf("expected the created issue to be reported despite the link failure, got:\n%s", buf.String())
	}
	if !strings.Contains(err.Error(), "created but failed to link") {
		t.Errorf("expected a partial-failure error, got: %v", err)
	}
}

// TestScenario_SubRemove_UnlinksChildViaMutation drives runSubRemove and asserts
// the RemoveSubIssue payload carries parent and child node ids in that order.
func TestScenario_SubRemove_UnlinksChildViaMutation(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondToFunc("GetIssue", issueByNumberFixture(map[int]string{
		10: "Parent epic",
		15: "Child task",
	}))
	handler.respondTo("RemoveSubIssue", gqlData(map[string]interface{}{
		"removeSubIssue": map[string]interface{}{
			"issue":    map[string]interface{}{"id": "issue-10"},
			"subIssue": map[string]interface{}{"id": "issue-15"},
		},
	}))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubRemoveCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runSubRemove(cmd, []string{"10", "15"}, &subRemoveOptions{}); err != nil {
		t.Fatalf("runSubRemove() error = %v (output %q)", err, buf.String())
	}

	reqs := handler.requestsFor("RemoveSubIssue")
	if len(reqs) != 1 {
		t.Fatalf("expected exactly one RemoveSubIssue mutation, got %d (ops: %v)",
			len(reqs), handler.operationNames())
	}
	input, ok := reqs[0].Variables["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected input object in mutation variables, got %v", reqs[0].Variables)
	}
	if input["issueId"] != "issue-10" {
		t.Errorf("expected the parent's node id as issueId, got %v", input["issueId"])
	}
	if input["subIssueId"] != "issue-15" {
		t.Errorf("expected the child's node id as subIssueId, got %v", input["subIssueId"])
	}

	if !strings.Contains(buf.String(), "#15 is no longer a sub-issue of #10") {
		t.Errorf("expected the unlink confirmation, got:\n%s", buf.String())
	}
}

// TestScenario_SubRemove_BatchUnlinksEachChild covers the batch path: one
// mutation per child, each carrying that child's own node id. The fixture is
// asymmetric (two distinct children) so a loop that reused one id would fail.
func TestScenario_SubRemove_BatchUnlinksEachChild(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondToFunc("GetIssue", issueByNumberFixture(map[int]string{
		10: "Parent epic",
		15: "First child",
		16: "Second child",
	}))
	handler.respondTo("RemoveSubIssue", gqlData(map[string]interface{}{
		"removeSubIssue": map[string]interface{}{
			"issue":    map[string]interface{}{"id": "issue-10"},
			"subIssue": map[string]interface{}{"id": "issue-15"},
		},
	}))

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubRemoveCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runSubRemove(cmd, []string{"10", "15", "16"}, &subRemoveOptions{}); err != nil {
		t.Fatalf("runSubRemove() error = %v (output %q)", err, buf.String())
	}

	reqs := handler.requestsFor("RemoveSubIssue")
	if len(reqs) != 2 {
		t.Fatalf("expected one RemoveSubIssue mutation per child, got %d (ops: %v)",
			len(reqs), handler.operationNames())
	}

	var subIssueIDs []interface{}
	for _, r := range reqs {
		input, ok := r.Variables["input"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected input object, got %v", r.Variables)
		}
		if input["issueId"] != "issue-10" {
			t.Errorf("expected every mutation to name the parent, got %v", input["issueId"])
		}
		subIssueIDs = append(subIssueIDs, input["subIssueId"])
	}
	if subIssueIDs[0] != "issue-15" || subIssueIDs[1] != "issue-16" {
		t.Errorf("expected each child's own node id, got %v", subIssueIDs)
	}

	if !strings.Contains(buf.String(), "2 succeeded, 0 failed") {
		t.Errorf("expected the batch summary, got:\n%s", buf.String())
	}
}

func TestScenario_SubList_NoChildrenRendersEmptyState(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetIssue", issueFixture(10, "Childless issue", "OPEN"))
	handler.respondTo("GetSubIssues", subIssuesFixture())

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &subListOptions{state: "all", relation: "children"}
	if err := runSubList(cmd, []string{"10"}, opts); err != nil {
		t.Fatalf("runSubList() error = %v", err)
	}

	if !strings.Contains(buf.String(), "No sub-issues found") {
		t.Errorf("expected empty-state message, got:\n%s", buf.String())
	}
}
