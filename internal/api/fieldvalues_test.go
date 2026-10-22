package api

import (
	"bytes"
	"strings"
	"testing"
)

// Tests for the shared ProjectV2 field-value extraction helpers (#874).
//
// Before extraction, six call sites in queries.go duplicated the __typename
// switch with divergent guards: three typed sites guarded only on a non-empty
// value, one raw site guarded only on a non-empty field name, one guarded on
// both, and one guarded only on the value. The helpers normalize that to a
// single rule — both the field name and the value must be non-empty.

// A server whose schema predates organization issue fields (a GitHub Enterprise
// Server without the rollout) rejects the whole document when it selects
// ProjectV2ItemIssueFieldValue. Every read path must drop the fragment, keep
// the project field values, warn once, and stop sending the fragment for the
// rest of the client's life rather than paying a rejection per query (#935).
func TestIssueFieldFragment_SchemaWithoutIssueFields(t *testing.T) {
	const rejection = `{"errors":[{"message":"No such type ProjectV2ItemIssueFieldValue, so it can't be a fragment condition","locations":[{"line":1,"column":1}]}]}`
	values := `"fieldValues":{"nodes":[` + projectStatusValueNode + `]}`
	page := func(item string) string {
		return `{"data":{"node":{"items":{"nodes":[{` + item + `,` + values + `}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`
	}
	// Each typed query selects a different item shape; the decoder rejects keys
	// a query did not select.
	itemsPage := page(`"id":"PVTI_1","content":{"__typename":"Issue","id":"I_kwDOAAAAAA1","number":7,"title":"t","state":"OPEN","repository":{"nameWithOwner":"acme/app"}}`)
	minimalPage := page(`"content":{"__typename":"Issue","id":"I_kwDOAAAAAA1","number":7,"state":"OPEN","repository":{"nameWithOwner":"acme/app"}}`)
	boardPage := page(`"content":{"__typename":"Issue","number":7,"title":"t","state":"OPEN","repository":{"nameWithOwner":"acme/app"}}`)
	issueNode := `"id":"I_kwDOAAAAAA1","number":7,"title":"t","body":"","state":"OPEN","url":"u","author":{"login":"a"},"assignees":{"nodes":[]},"labels":{"nodes":[]},"milestone":{"title":""},"projectItems":{"nodes":[{"project":{"id":"PVT_org"},` + values + `}],"pageInfo":{"hasNextPage":false}}`
	refIssueNode := `"id":"I_kwDOAAAAAA1","number":7,"title":"t","body":"","state":"OPEN","url":"u","repository":{"nameWithOwner":"acme/app"},"assignees":{"nodes":[]},"labels":{"nodes":[]},"projectItems":{"nodes":[{"id":"PVTI_1","project":{"id":"PVT_org"},` + values + `}]}`

	var rejected int
	c, _ := newScriptedClient(t, func(req scriptedRequest) string {
		if strings.Contains(req.Query, "ProjectV2ItemIssueFieldValue") {
			rejected++
			return rejection
		}
		switch {
		case req.Op == "GetIssueWithProjectFields":
			return `{"data":{"repository":{"issue":{` + issueNode + `}}}}`
		case req.Op == "GetProjectItems":
			return itemsPage
		case req.Op == "GetProjectItemsMinimal":
			return minimalPage
		case req.Op == "GetProjectItemsForBoard":
			return boardPage
		case strings.Contains(req.Query, "n0: node"):
			return `{"data":{"n0":{"id":"I_kwDOAAAAAA1","projectItems":{"nodes":[{"project":{"id":"PVT_org"},` + values + `}]}}}}`
		case strings.Contains(req.Query, "r0: repository"):
			return `{"data":{"r0":{"i0_0":{` + refIssueNode + `}}}}`
		case strings.Contains(req.Query, "i0: issue"):
			return `{"data":{"repository":{"i0":{` + issueNode + `}}}}`
		}
		return ""
	})
	var warnings bytes.Buffer
	c.warnOut = &warnings

	status := func(path string, fvs []FieldValue) {
		t.Helper()
		if got := fieldValueMap(fvs)["Status"]; got != "In progress" {
			t.Errorf("%s: Status = %q, want the project field value kept (values: %v)", path, got, fvs)
		}
	}

	_, fvs, err := c.GetIssueWithProjectFields("PVT_org", "acme", "app", 7)
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	status("view", fvs)

	items, err := c.GetProjectItems("PVT_org", nil)
	if err != nil || len(items) != 1 {
		t.Fatalf("list: items=%d err=%v", len(items), err)
	}
	status("list", items[0].FieldValues)

	minimal, err := c.GetProjectItemsMinimal("PVT_org", nil)
	if err != nil || len(minimal) != 1 {
		t.Fatalf("list (minimal): items=%d err=%v", len(minimal), err)
	}
	status("list (minimal)", minimal[0].FieldValues)

	board, err := c.GetProjectItemsForBoard("PVT_org", nil)
	if err != nil || len(board) != 1 {
		t.Fatalf("board: items=%d err=%v", len(board), err)
	}
	if board[0].Status != "In progress" {
		t.Errorf("board: Status = %q, want In progress", board[0].Status)
	}

	byID, err := c.GetProjectFieldsForIssues("PVT_org", []string{"I_kwDOAAAAAA1"})
	if err != nil {
		t.Fatalf("field values for issues: %v", err)
	}
	status("field values for issues", byID["I_kwDOAAAAAA1"])

	_, batch, _, err := c.GetIssuesWithProjectFieldsBatch("PVT_org", "acme", "app", []int{7})
	if err != nil {
		t.Fatalf("view (batch): %v", err)
	}
	status("view (batch)", batch[7])

	byRef, err := c.GetProjectItemsByIssues("PVT_org", []IssueRef{{Owner: "acme", Repo: "app", Number: 7}})
	if err != nil || len(byRef) != 1 {
		t.Fatalf("items by issue: items=%d err=%v", len(byRef), err)
	}
	status("items by issue", byRef[0].FieldValues)

	if rejected != 1 {
		t.Errorf("server rejected %d documents, want 1: after the first rejection the fragment must be omitted up front", rejected)
	}
	if n := strings.Count(warnings.String(), "\n"); n != 1 {
		t.Errorf("want exactly one warning line, got %d: %q", n, warnings.String())
	}
	if !strings.Contains(warnings.String(), "issue field") {
		t.Errorf("warning should say issue field values are unavailable, got %q", warnings.String())
	}
}

func TestNewFieldValue_SingleSelect(t *testing.T) {
	fv, ok := newFieldValue("ProjectV2ItemFieldSingleSelectValue", "Status", "In progress")
	if !ok {
		t.Fatalf("Expected single-select node to be accepted")
	}
	if fv.Field != "Status" || fv.Value != "In progress" {
		t.Errorf("Expected Status/In progress; got %q/%q", fv.Field, fv.Value)
	}
}

func TestNewFieldValue_Text(t *testing.T) {
	fv, ok := newFieldValue("ProjectV2ItemFieldTextValue", "Branch", "pmu/next-version")
	if !ok {
		t.Fatalf("Expected text node to be accepted")
	}
	if fv.Field != "Branch" || fv.Value != "pmu/next-version" {
		t.Errorf("Expected Branch/pmu/next-version; got %q/%q", fv.Field, fv.Value)
	}
}

func TestNewFieldValue_UnsupportedTypeRejected(t *testing.T) {
	for _, typeName := range []string{
		"ProjectV2ItemFieldNumberValue",
		"ProjectV2ItemFieldDateValue",
		"ProjectV2ItemFieldIterationValue",
		"",
	} {
		if _, ok := newFieldValue(typeName, "Estimate", "5"); ok {
			t.Errorf("Expected %q to be rejected as unsupported", typeName)
		}
	}
}

// Pins the normalized empty-field-name guard. Unifying sites that previously
// disagreed about this check is a semantic decision no prior test constrained,
// so it is asserted directly (#874 AC4).
func TestNewFieldValue_EmptyFieldNameRejected(t *testing.T) {
	if _, ok := newFieldValue("ProjectV2ItemFieldSingleSelectValue", "", "In progress"); ok {
		t.Errorf("Expected empty field name to be rejected for single-select")
	}
	if _, ok := newFieldValue("ProjectV2ItemFieldTextValue", "", "pmu/next-version"); ok {
		t.Errorf("Expected empty field name to be rejected for text")
	}
}

func TestNewFieldValue_EmptyValueRejected(t *testing.T) {
	if _, ok := newFieldValue("ProjectV2ItemFieldSingleSelectValue", "Status", ""); ok {
		t.Errorf("Expected empty value to be rejected for single-select")
	}
	if _, ok := newFieldValue("ProjectV2ItemFieldTextValue", "Branch", ""); ok {
		t.Errorf("Expected empty value to be rejected for text")
	}
}

func TestAppendTypedFieldValues(t *testing.T) {
	nodes := make([]typedFieldValueNode, 4)

	nodes[0].TypeName = "ProjectV2ItemFieldSingleSelectValue"
	nodes[0].ProjectV2ItemFieldSingleSelectValue.Name = "In progress"
	nodes[0].ProjectV2ItemFieldSingleSelectValue.Field.ProjectV2SingleSelectField.Name = "Status"

	nodes[1].TypeName = "ProjectV2ItemFieldTextValue"
	nodes[1].ProjectV2ItemFieldTextValue.Text = "pmu/next-version"
	nodes[1].ProjectV2ItemFieldTextValue.Field.ProjectV2Field.Name = "Branch"

	// Unsupported type — dropped.
	nodes[2].TypeName = "ProjectV2ItemFieldNumberValue"

	// Non-empty value but empty field name — dropped by the normalized guard.
	nodes[3].TypeName = "ProjectV2ItemFieldSingleSelectValue"
	nodes[3].ProjectV2ItemFieldSingleSelectValue.Name = "Orphaned"

	existing := []FieldValue{{Field: "Priority", Value: "P2"}}
	got := appendTypedFieldValues(existing, nodes)

	want := []FieldValue{
		{Field: "Priority", Value: "P2"},
		{Field: "Status", Value: "In progress"},
		{Field: "Branch", Value: "pmu/next-version"},
	}
	assertFieldValues(t, got, want)
}

func TestAppendRawFieldValues(t *testing.T) {
	nodes := []rawFieldValueNode{
		{TypeName: "ProjectV2ItemFieldSingleSelectValue", Name: "Done"},
		{TypeName: "ProjectV2ItemFieldTextValue", Text: "main"},
		{TypeName: "ProjectV2ItemFieldNumberValue", Name: "5"},
		// Field name present but value empty — dropped by the normalized guard.
		{TypeName: "ProjectV2ItemFieldTextValue"},
	}
	nodes[0].Field.Name = "Status"
	nodes[1].Field.Name = "Branch"
	nodes[2].Field.Name = "Estimate"
	nodes[3].Field.Name = "Notes"

	got := appendRawFieldValues(nil, nodes)

	want := []FieldValue{
		{Field: "Status", Value: "Done"},
		{Field: "Branch", Value: "main"},
	}
	assertFieldValues(t, got, want)
}

func TestAppendFieldValues_NilInputLeavesDestinationUntouched(t *testing.T) {
	if got := appendTypedFieldValues[typedFieldValueNode](nil, nil); got != nil {
		t.Errorf("Expected nil destination to stay nil; got %v", got)
	}
	if got := appendRawFieldValues(nil, nil); got != nil {
		t.Errorf("Expected nil destination to stay nil; got %v", got)
	}
}

func assertFieldValues(t *testing.T, got, want []FieldValue) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Expected %d field values; got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Index %d: expected %+v; got %+v", i, want[i], got[i])
		}
	}
}
