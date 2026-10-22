package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Read-path tests for organization issue fields (#932). The project API
// returns an issue field's value as a ProjectV2ItemIssueFieldValue; each read
// path view, list and board use must surface it under the project field's
// name, like any other field value.

const (
	issuePriorityValueNode = `{"__typename":"ProjectV2ItemIssueFieldValue","field":{"name":"Priority"},"issueFieldValue":{"name":"P1"}}`
	issueStartDateNode     = `{"__typename":"ProjectV2ItemIssueFieldValue","field":{"name":"Start date"},"issueFieldValue":{"value":"2026-10-22"}}`
	projectStatusValueNode = `{"__typename":"ProjectV2ItemFieldSingleSelectValue","name":"In progress","field":{"name":"Status"}}`
)

func fieldValueMap(values []FieldValue) map[string]string {
	out := map[string]string{}
	for _, v := range values {
		out[v.Field] = v.Value
	}
	return out
}

func assertIssueFieldValues(t *testing.T, values []FieldValue) {
	t.Helper()
	got := fieldValueMap(values)
	if got["Priority"] != "P1" {
		t.Errorf("Priority = %q, want P1 from the org issue field (values: %v)", got["Priority"], got)
	}
	if got["Start date"] != "2026-10-22" {
		t.Errorf("Start date = %q, want 2026-10-22 from the org issue field (values: %v)", got["Start date"], got)
	}
	if got["Status"] != "In progress" {
		t.Errorf("Status = %q, want the project field value kept (values: %v)", got["Status"], got)
	}
}

func fieldValueNodes() string {
	return strings.Join([]string{projectStatusValueNode, issuePriorityValueNode, issueStartDateNode}, ",")
}

func TestAppendRawFieldValues_IssueFieldValues(t *testing.T) {
	var nodes []rawFieldValueNode
	if err := json.Unmarshal([]byte("["+fieldValueNodes()+"]"), &nodes); err != nil {
		t.Fatalf("decode nodes: %v", err)
	}
	assertIssueFieldValues(t, appendRawFieldValues(nil, nodes))
}

// view: single issue.
func TestGetIssueWithProjectFields_IssueFieldValues(t *testing.T) {
	c, _ := newScriptedClient(t, func(req scriptedRequest) string {
		if req.Op != "GetIssueWithProjectFields" {
			return ""
		}
		return fmt.Sprintf(`{"data":{"repository":{"issue":{"id":"I_1","number":7,"title":"t","state":"OPEN","projectItems":{"nodes":[{"project":{"id":"PVT_org"},"fieldValues":{"nodes":[%s]}}],"pageInfo":{"hasNextPage":false}}}}}}`, fieldValueNodes())
	})

	_, values, err := c.GetIssueWithProjectFields("PVT_org", "acme", "app", 7)
	if err != nil {
		t.Fatalf("GetIssueWithProjectFields: %v", err)
	}
	assertIssueFieldValues(t, values)
}

// view: several issues at once.
func TestParseIssuesBatchResponse_IssueFieldValues(t *testing.T) {
	data := fmt.Sprintf(`{"data":{"repository":{"i0":{"id":"I_1","number":7,"title":"t","state":"OPEN","projectItems":{"nodes":[{"project":{"id":"PVT_org"},"fieldValues":{"nodes":[%s]}}]}}}}}`, fieldValueNodes())

	_, values, _, err := parseIssuesBatchResponse([]byte(data), []int{7}, "PVT_org", "acme", "app")
	if err != nil {
		t.Fatalf("parseIssuesBatchResponse: %v", err)
	}
	assertIssueFieldValues(t, values[7])
}

// list: project items.
func TestGetProjectItems_IssueFieldValues(t *testing.T) {
	c, _ := newScriptedClient(t, func(req scriptedRequest) string {
		if req.Op != "GetProjectItems" {
			return ""
		}
		return fmt.Sprintf(`{"data":{"node":{"items":{"nodes":[{"id":"PVTI_1","content":{"__typename":"Issue","id":"I_1","number":7,"title":"t","state":"OPEN","repository":{"nameWithOwner":"acme/app"}},"fieldValues":{"nodes":[%s]}}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`, fieldValueNodes())
	})

	items, err := c.GetProjectItems("PVT_org", nil)
	if err != nil {
		t.Fatalf("GetProjectItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	assertIssueFieldValues(t, items[0].FieldValues)
}

// list and board: field values for a set of issues.
func TestGetProjectFieldsForIssues_IssueFieldValues(t *testing.T) {
	const issueID = "I_kwDOAAAAAA1"
	c, _ := newScriptedClient(t, func(req scriptedRequest) string {
		if !strings.Contains(req.Query, "projectItems") {
			return ""
		}
		return fmt.Sprintf(`{"data":{"n0":{"id":%q,"projectItems":{"nodes":[{"project":{"id":"PVT_org"},"fieldValues":{"nodes":[%s]}}]}}}}`, issueID, fieldValueNodes())
	})

	byIssue, err := c.GetProjectFieldsForIssues("PVT_org", []string{issueID})
	if err != nil {
		t.Fatalf("GetProjectFieldsForIssues: %v", err)
	}
	assertIssueFieldValues(t, byIssue[issueID])
}

// board.
func TestGetProjectItemsForBoard_IssueFieldPriority(t *testing.T) {
	c, _ := newScriptedClient(t, func(req scriptedRequest) string {
		if req.Op != "GetProjectItemsForBoard" {
			return ""
		}
		return fmt.Sprintf(`{"data":{"node":{"items":{"nodes":[{"content":{"__typename":"Issue","number":7,"title":"t","state":"OPEN","repository":{"nameWithOwner":"acme/app"}},"fieldValues":{"nodes":[%s]}}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`, fieldValueNodes())
	})

	items, err := c.GetProjectItemsForBoard("PVT_org", nil)
	if err != nil {
		t.Fatalf("GetProjectItemsForBoard: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].Priority != "P1" {
		t.Errorf("board Priority = %q, want P1 from the org issue field", items[0].Priority)
	}
	if items[0].Status != "In progress" {
		t.Errorf("board Status = %q, want In progress", items[0].Status)
	}
}
