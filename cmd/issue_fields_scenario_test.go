package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// Command-level scenarios for organization issue fields (#932): the project is
// org-owned, Priority is an org issue field the project shows as an
// option-less single-select, and Status is an ordinary project field. Every
// request goes through the mock GraphQL server.

func orgProjectFieldsFixture() map[string]interface{} {
	return gqlData(map[string]interface{}{
		"node": map[string]interface{}{
			"owner": map[string]interface{}{"__typename": "Organization", "login": "test-org"},
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
						},
					},
					map[string]interface{}{
						"__typename": "ProjectV2SingleSelectField",
						"id":         "field-priority",
						"name":       "Priority",
						"dataType":   "SINGLE_SELECT",
						"options":    []interface{}{},
					},
				},
				"pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": ""},
			},
		},
	})
}

func orgIssueFieldsFixture() map[string]interface{} {
	options := []interface{}{}
	for _, name := range []string{"P0", "P1", "P2"} {
		options = append(options, map[string]interface{}{"id": "IFSSO_" + name, "name": name})
	}
	return gqlData(map[string]interface{}{
		"organization": map[string]interface{}{
			"issueFields": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"__typename": "IssueFieldSingleSelect",
						"id":         "IFSS_pri",
						"name":       "Priority",
						"dataType":   "SINGLE_SELECT",
						"options":    options,
					},
				},
				"pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": ""},
			},
		},
	})
}

// withOrgIssueFields registers discovery, item-content lookup and both field
// mutations for an org board whose Priority is an issue field.
func withOrgIssueFields(handler *mockGraphQLHandler, issueNodeID string) {
	handler.respondTo("GetProjectFields", orgProjectFieldsFixture())
	handler.respondTo("GetOrganizationIssueFields", orgIssueFieldsFixture())
	handler.respondTo("GetProjectIssueFieldPairs", gqlData(map[string]interface{}{
		"node": map[string]interface{}{"items": map[string]interface{}{"nodes": []interface{}{}}},
	}))
	handler.respondTo("GetProjectItemContent", gqlData(map[string]interface{}{
		"node": map[string]interface{}{"content": map[string]interface{}{"__typename": "Issue", "id": issueNodeID}},
	}))
	handler.respondTo("SetIssueFieldValue", gqlData(map[string]interface{}{
		"setIssueFieldValue": map[string]interface{}{"clientMutationId": nil},
	}))
	handler.respondTo("UpdateProjectV2ItemFieldValue", gqlData(map[string]interface{}{
		"updateProjectV2ItemFieldValue": map[string]interface{}{"clientMutationId": nil},
	}))
}

// assertPrioritySetViaIssueField checks exactly one setIssueFieldValue wrote
// Priority = P2 onto issueNodeID.
func assertPrioritySetViaIssueField(t *testing.T, handler *mockGraphQLHandler, issueNodeID string) {
	t.Helper()
	writes := handler.requestsFor("SetIssueFieldValue")
	if len(writes) != 1 {
		t.Fatalf("expected one setIssueFieldValue mutation, got %d (ops: %v)", len(writes), handler.operationNames())
	}
	input, _ := writes[0].Variables["input"].(map[string]interface{})
	if input["issueId"] != issueNodeID {
		t.Errorf("issueId = %v, want %s", input["issueId"], issueNodeID)
	}
	entries, _ := input["issueFields"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("issueFields = %v, want one entry", input["issueFields"])
	}
	entry, _ := entries[0].(map[string]interface{})
	if entry["fieldId"] != "IFSS_pri" || entry["singleSelectOptionId"] != "IFSSO_P2" {
		t.Errorf("Priority entry = %v, want IFSS_pri / IFSSO_P2", entry)
	}
}

func TestScenario_Create_PrioritySetsOrgIssueField(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetRepositoryID", gqlData(map[string]interface{}{
		"repository": map[string]interface{}{"id": "repo-node-1"},
	}))
	handler.respondTo("CreateIssue", gqlData(map[string]interface{}{
		"createIssue": map[string]interface{}{
			"issue": map[string]interface{}{
				"id": "issue-node-1", "number": 77, "title": "Org issue",
				"url": "https://github.com/test-org/test-repo/issues/77",
			},
		},
	}))
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Org Project"))
	handler.respondTo("AddProjectV2ItemById", gqlData(map[string]interface{}{
		"addProjectV2ItemById": map[string]interface{}{"item": map[string]interface{}{"id": "item-new-1"}},
	}))
	withOrgIssueFields(handler, "issue-node-1")

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newCreateCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &createOptions{title: "Org issue", body: "b", status: "in_progress", priority: "p2"}
	if err := runCreate(cmd, opts); err != nil {
		t.Fatalf("runCreate() error = %v (output %q)", err, buf.String())
	}

	assertPrioritySetViaIssueField(t, handler, "issue-node-1")

	// Status stays on the project-field mutation, and Priority never does.
	updates := handler.requestsFor("UpdateProjectV2ItemFieldValue")
	if len(updates) != 1 {
		t.Fatalf("expected one updateProjectV2ItemFieldValue (Status), got %d", len(updates))
	}
	upInput, _ := updates[0].Variables["input"].(map[string]interface{})
	if upInput["fieldId"] != "field-status" {
		t.Errorf("project mutation fieldId = %v, want field-status", upInput["fieldId"])
	}
}

func TestScenario_Move_PrioritySetsOrgIssueField(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Org Project"))
	handler.respondToQueryContaining("i0_0: issue(number:",
		batchIssueLookupFixture(42, "Movable issue", "OPEN", "proj-1", "item-1", "Backlog"))
	handler.respondTo("BatchUpdate", gqlData(map[string]interface{}{
		"u0": map[string]interface{}{"projectV2Item": map[string]interface{}{"id": "item-1"}},
	}))
	withOrgIssueFields(handler, "issue-42")

	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newMoveCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runMove(cmd, []string{"42"}, &moveOptions{status: "in_progress", priority: "p2"}); err != nil {
		t.Fatalf("runMove() error = %v (output %q)", err, buf.String())
	}

	assertPrioritySetViaIssueField(t, handler, "issue-42")

	batches := handler.requestsFor("BatchUpdate")
	if len(batches) != 1 {
		t.Fatalf("expected one BatchUpdate for Status, got %d (ops: %v)", len(batches), handler.operationNames())
	}
	input0, _ := batches[0].Variables["input0"].(map[string]interface{})
	if input0["fieldId"] != "field-status" {
		t.Errorf("batch input0 fieldId = %v, want field-status", input0["fieldId"])
	}
	if _, extra := batches[0].Variables["input1"]; extra {
		t.Errorf("Priority leaked into the project batch: %v", batches[0].Variables)
	}
}

// configWithUnknownPriority maps p9 to an option the org issue field lacks, so
// the alias resolves but the value cannot.
const configWithUnknownPriority = `{
  "project": {"owner": "test-org", "number": 1},
  "repositories": ["test-org/test-repo"],
  "fields": {
    "status": {"field": "Status", "values": {"backlog": "Backlog", "in_progress": "In Progress"}},
    "priority": {"field": "Priority", "values": {"p2": "P2", "p9": "P9"}}
  }
}`

func TestScenario_Move_UnresolvableIssueFieldValueFails(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Org Project"))
	handler.respondToQueryContaining("i0_0: issue(number:",
		batchIssueLookupFixture(42, "Movable issue", "OPEN", "proj-1", "item-1", "Backlog"))
	withOrgIssueFields(handler, "issue-42")

	_, cleanup := setupTestEnvironmentWithConfig(t, handler, configWithUnknownPriority)
	defer cleanup()

	cmd := newMoveCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// move reports per-field failures on os.Stderr.
	oldErr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err := runMove(cmd, []string{"42"}, &moveOptions{priority: "p9"})

	_ = w.Close()
	os.Stderr = oldErr
	var errBuf bytes.Buffer
	_, _ = io.Copy(&errBuf, r)

	if err == nil {
		t.Fatalf("expected a non-nil error (non-zero exit) for an unresolvable Priority, output:\n%s", buf.String())
	}
	if !strings.Contains(errBuf.String(), `option "P9" not found`) {
		t.Errorf("expected stderr to name the unresolvable option, got %q", errBuf.String())
	}
	if n := len(handler.requestsFor("SetIssueFieldValue")); n != 0 {
		t.Errorf("unresolvable value still sent %d setIssueFieldValue mutation(s)", n)
	}
}

func TestScenario_Create_UnresolvableIssueFieldValueFails(t *testing.T) {
	handler := newMockGraphQLHandler()
	handler.respondTo("GetRepositoryID", gqlData(map[string]interface{}{
		"repository": map[string]interface{}{"id": "repo-node-1"},
	}))
	handler.respondTo("CreateIssue", gqlData(map[string]interface{}{
		"createIssue": map[string]interface{}{
			"issue": map[string]interface{}{
				"id": "issue-node-1", "number": 77, "title": "Org issue",
				"url": "https://github.com/test-org/test-repo/issues/77",
			},
		},
	}))
	handler.respondTo("GetUserProject", userProjectFixture("proj-1", "Org Project"))
	handler.respondTo("AddProjectV2ItemById", gqlData(map[string]interface{}{
		"addProjectV2ItemById": map[string]interface{}{"item": map[string]interface{}{"id": "item-new-1"}},
	}))
	withOrgIssueFields(handler, "issue-node-1")

	_, cleanup := setupTestEnvironmentWithConfig(t, handler, configWithUnknownPriority)
	defer cleanup()

	cmd := newCreateCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := runCreate(cmd, &createOptions{title: "Org issue", body: "b", priority: "p9"})
	if err == nil {
		t.Fatalf("expected a non-nil error (non-zero exit) for an unresolvable Priority, output:\n%s", buf.String())
	}
	if n := len(handler.requestsFor("SetIssueFieldValue")); n != 0 {
		t.Errorf("unresolvable value still sent %d setIssueFieldValue mutation(s)", n)
	}
}
