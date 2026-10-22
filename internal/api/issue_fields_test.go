package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// ============================================================================
// Organization issue fields (#932)
// ============================================================================
//
// These tests drive the real shurcooL decoding path: a scripted transport
// answers each request with GraphQL JSON keyed by operation name, so the
// responses exercise the same fragment handling production sees rather than a
// reflection-populated struct.

// scriptedRequest is one GraphQL request as it reached the wire.
type scriptedRequest struct {
	Op        string
	Query     string
	Variables map[string]interface{}
}

// scriptedTransport answers GraphQL requests through handler and records them.
type scriptedTransport struct {
	mu       sync.Mutex
	requests []scriptedRequest
	handler  func(req scriptedRequest) string
}

func (s *scriptedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var payload struct {
		Query     string                 `json:"query"`
		Variables map[string]interface{} `json:"variables"`
	}
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal(body, &payload)
	}
	req := scriptedRequest{Op: documentOperationName(payload.Query), Query: payload.Query, Variables: payload.Variables}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()

	body := `{"data":{}}`
	if s.handler != nil {
		if b := s.handler(req); b != "" {
			body = b
		}
	}
	return makeResp(200, body), nil
}

func (s *scriptedTransport) ops() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.requests))
	for _, r := range s.requests {
		out = append(out, r.Op)
	}
	return out
}

func (s *scriptedTransport) requestsFor(op string) []scriptedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []scriptedRequest
	for _, r := range s.requests {
		if r.Op == op {
			out = append(out, r)
		}
	}
	return out
}

func newScriptedClient(t *testing.T, handler func(req scriptedRequest) string) (*Client, *scriptedTransport) {
	t.Helper()
	transport := &scriptedTransport{handler: handler}
	SetTestTransport(transport)
	SetTestAuthToken("test-token")
	t.Cleanup(func() {
		SetTestTransport(nil)
		SetTestAuthToken("")
	})
	restoreOverrides := SetIssueFieldOverridesLoaderForTesting(func() (map[string]string, error) { return nil, nil })
	t.Cleanup(restoreOverrides)

	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	return c, transport
}

// --- response builders ------------------------------------------------------

func projectFieldsJSON(ownerType, ownerLogin string, fieldNodes ...string) string {
	owner := fmt.Sprintf(`{"__typename":%q}`, ownerType)
	if ownerType == "Organization" {
		owner = fmt.Sprintf(`{"__typename":"Organization","login":%q}`, ownerLogin)
	}
	return fmt.Sprintf(`{"data":{"node":{"owner":%s,"fields":{"nodes":[%s],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`,
		owner, strings.Join(fieldNodes, ","))
}

func singleSelectNode(id, name string, options ...string) string {
	var opts []string
	for i, o := range options {
		opts = append(opts, fmt.Sprintf(`{"id":"%s_opt%d","name":%q,"color":"GRAY","description":""}`, id, i, o))
	}
	return fmt.Sprintf(`{"__typename":"ProjectV2SingleSelectField","id":%q,"name":%q,"dataType":"SINGLE_SELECT","options":[%s]}`,
		id, name, strings.Join(opts, ","))
}

func plainFieldNode(id, name, dataType string) string {
	return fmt.Sprintf(`{"__typename":"ProjectV2Field","id":%q,"name":%q,"dataType":%q}`, id, name, dataType)
}

func orgIssueFieldsJSON(hasNext bool, endCursor string, nodes ...string) string {
	return fmt.Sprintf(`{"data":{"organization":{"issueFields":{"nodes":[%s],"pageInfo":{"hasNextPage":%t,"endCursor":%q}}}}}`,
		strings.Join(nodes, ","), hasNext, endCursor)
}

func issueSingleSelectNode(id, name string, options ...string) string {
	var opts []string
	for _, o := range options {
		opts = append(opts, fmt.Sprintf(`{"id":"IFSSO_%s","name":%q}`, o, o))
	}
	return fmt.Sprintf(`{"__typename":"IssueFieldSingleSelect","id":%q,"name":%q,"dataType":"SINGLE_SELECT","options":[%s]}`,
		id, name, strings.Join(opts, ","))
}

func issueFieldNode(typeName, id, name, dataType string) string {
	return fmt.Sprintf(`{"__typename":%q,"id":%q,"name":%q,"dataType":%q}`, typeName, id, name, dataType)
}

// pairsJSON builds a GetProjectIssueFieldPairs response; each pair is
// projectFieldID -> issueFieldID as seen on one item value.
func pairsJSON(pairs ...[2]string) string {
	var values []string
	for _, p := range pairs {
		values = append(values, fmt.Sprintf(
			`{"__typename":"ProjectV2ItemIssueFieldValue","field":{"id":%q},"issueFieldValue":{"field":{"id":%q}}}`,
			p[0], p[1]))
	}
	return fmt.Sprintf(`{"data":{"node":{"items":{"nodes":[{"fieldValues":{"nodes":[%s]}}]}}}}`,
		strings.Join(values, ","))
}

const emptyPairsJSON = `{"data":{"node":{"items":{"nodes":[]}}}}`

// rubricalOrgHandler models the rubrical-works board from the issue: Priority
// is an org issue field shown as an option-less project single-select; Status
// and Branch are ordinary project fields.
func rubricalOrgHandler(req scriptedRequest) string {
	switch req.Op {
	case "GetProjectFields":
		return projectFieldsJSON("Organization", "acme",
			singleSelectNode("PVTSSF_status", "Status", "Backlog", "In progress"),
			singleSelectNode("PVTSSF_pri", "Priority"),
			plainFieldNode("PVTF_branch", "Branch", "TEXT"),
			plainFieldNode("PVTF_start", "Start date", "DATE"),
		)
	case "GetOrganizationIssueFields":
		return orgIssueFieldsJSON(false, "",
			issueSingleSelectNode("IFSS_pri", "Priority", "P0", "P1", "P2"),
			issueSingleSelectNode("IFSS_effort", "Effort", "High", "Low"),
			issueFieldNode("IssueFieldDate", "IFD_start", "Start date", "DATE"),
		)
	case "GetProjectIssueFieldPairs":
		return emptyPairsJSON
	}
	return ""
}

func fieldByName(t *testing.T, fields []ProjectField, name string) ProjectField {
	t.Helper()
	for _, f := range fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("field %q not found in %d fields", name, len(fields))
	return ProjectField{}
}

func optionNames(opts []FieldOption) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		out = append(out, o.Name+"="+o.ID)
	}
	return out
}

// --- discovery ---------------------------------------------------------------

func TestGetProjectFields_OrgBoard_ClassifiesEachFieldIndividually(t *testing.T) {
	c, _ := newScriptedClient(t, rubricalOrgHandler)

	fields, err := c.GetProjectFields("PVT_org")
	if err != nil {
		t.Fatalf("GetProjectFields: %v", err)
	}

	pri := fieldByName(t, fields, "Priority")
	if pri.IssueField == nil {
		t.Fatalf("Priority should be classified as an org issue field")
	}
	if pri.IssueField.ID != "IFSS_pri" || pri.IssueField.DataType != "SINGLE_SELECT" {
		t.Errorf("Priority issue field = %+v, want IFSS_pri SINGLE_SELECT", *pri.IssueField)
	}
	if pri.ID != "PVTSSF_pri" {
		t.Errorf("Priority project field ID = %q, want the project field ID kept", pri.ID)
	}
	if got := strings.Join(optionNames(pri.Options), ","); got != "P0=IFSSO_P0,P1=IFSSO_P1,P2=IFSSO_P2" {
		t.Errorf("Priority options = %s, want the issue field's real options", got)
	}
	if pri.Kind() != FieldKindIssueField {
		t.Errorf("Priority kind = %q, want %q", pri.Kind(), FieldKindIssueField)
	}

	for _, name := range []string{"Status", "Branch"} {
		f := fieldByName(t, fields, name)
		if f.IssueField != nil {
			t.Errorf("%s should stay a project field, got issue field %+v", name, *f.IssueField)
		}
		if f.Kind() != FieldKindProject {
			t.Errorf("%s kind = %q, want %q", name, f.Kind(), FieldKindProject)
		}
	}
}

func TestGetProjectFields_OrgBoard_OrdinaryPriorityProjectFieldStaysProject(t *testing.T) {
	c, _ := newScriptedClient(t, func(req scriptedRequest) string {
		switch req.Op {
		case "GetProjectFields":
			return projectFieldsJSON("Organization", "acme",
				singleSelectNode("PVTSSF_pri", "Priority", "P0", "P1", "P2"))
		case "GetOrganizationIssueFields":
			return orgIssueFieldsJSON(false, "", issueSingleSelectNode("IFSS_pri", "Priority", "P0", "P1", "P2"))
		case "GetProjectIssueFieldPairs":
			return emptyPairsJSON
		}
		return ""
	})

	fields, err := c.GetProjectFields("PVT_org")
	if err != nil {
		t.Fatalf("GetProjectFields: %v", err)
	}
	pri := fieldByName(t, fields, "Priority")
	if pri.IssueField != nil {
		t.Fatalf("a Priority project field with its own options must stay on the project path; got %+v", *pri.IssueField)
	}
	if pri.Options[0].ID != "PVTSSF_pri_opt0" {
		t.Errorf("project options replaced: %v", optionNames(pri.Options))
	}
}

func TestGetProjectFields_UserBoard_NeverQueriesOrgIssueFields(t *testing.T) {
	c, transport := newScriptedClient(t, func(req scriptedRequest) string {
		if req.Op == "GetProjectFields" {
			return projectFieldsJSON("User", "",
				singleSelectNode("PVTSSF_pri", "Priority"),
				plainFieldNode("PVTF_start", "Start date", "DATE"))
		}
		return ""
	})

	fields, err := c.GetProjectFields("PVT_user")
	if err != nil {
		t.Fatalf("GetProjectFields: %v", err)
	}
	for _, f := range fields {
		if f.IssueField != nil {
			t.Errorf("user-owned board field %q classified as issue field", f.Name)
		}
	}
	for _, op := range transport.ops() {
		if op == "GetOrganizationIssueFields" || op == "GetProjectIssueFieldPairs" {
			t.Errorf("user-owned board issued %s", op)
		}
	}
}

func TestGetProjectFields_SameNameStartDate(t *testing.T) {
	t.Run("org board without a Start date issue field stays project", func(t *testing.T) {
		c, _ := newScriptedClient(t, func(req scriptedRequest) string {
			switch req.Op {
			case "GetProjectFields":
				return projectFieldsJSON("Organization", "acme", plainFieldNode("PVTF_start", "Start date", "DATE"))
			case "GetOrganizationIssueFields":
				return orgIssueFieldsJSON(false, "", issueSingleSelectNode("IFSS_pri", "Priority", "P0"))
			case "GetProjectIssueFieldPairs":
				return emptyPairsJSON
			}
			return ""
		})
		fields, err := c.GetProjectFields("PVT_org")
		if err != nil {
			t.Fatalf("GetProjectFields: %v", err)
		}
		if f := fieldByName(t, fields, "Start date"); f.IssueField != nil {
			t.Errorf("Start date classified as issue field with no matching org issue field")
		}
	})

	t.Run("name match with a different data type stays project", func(t *testing.T) {
		c, _ := newScriptedClient(t, func(req scriptedRequest) string {
			switch req.Op {
			case "GetProjectFields":
				return projectFieldsJSON("Organization", "acme", plainFieldNode("PVTF_start", "Start date", "TEXT"))
			case "GetOrganizationIssueFields":
				return orgIssueFieldsJSON(false, "", issueFieldNode("IssueFieldDate", "IFD_start", "Start date", "DATE"))
			case "GetProjectIssueFieldPairs":
				return emptyPairsJSON
			}
			return ""
		})
		fields, err := c.GetProjectFields("PVT_org")
		if err != nil {
			t.Fatalf("GetProjectFields: %v", err)
		}
		if f := fieldByName(t, fields, "Start date"); f.IssueField != nil {
			t.Errorf("TEXT Start date matched a DATE issue field by name alone")
		}
	})

	t.Run("org Start date issue field correlated by name and type", func(t *testing.T) {
		c, _ := newScriptedClient(t, rubricalOrgHandler)
		fields, err := c.GetProjectFields("PVT_org")
		if err != nil {
			t.Fatalf("GetProjectFields: %v", err)
		}
		f := fieldByName(t, fields, "Start date")
		if f.IssueField == nil || f.IssueField.ID != "IFD_start" {
			t.Fatalf("Start date should route to IFD_start, got %+v", f.IssueField)
		}
	})

	t.Run("item-value pairing is authoritative over the name", func(t *testing.T) {
		c, _ := newScriptedClient(t, func(req scriptedRequest) string {
			switch req.Op {
			case "GetProjectFields":
				return projectFieldsJSON("Organization", "acme",
					singleSelectNode("PVTSSF_urgency", "Urgency"))
			case "GetOrganizationIssueFields":
				return orgIssueFieldsJSON(false, "", issueSingleSelectNode("IFSS_pri", "Priority", "P0", "P1"))
			case "GetProjectIssueFieldPairs":
				return pairsJSON([2]string{"PVTSSF_urgency", "IFSS_pri"})
			}
			return ""
		})
		fields, err := c.GetProjectFields("PVT_org")
		if err != nil {
			t.Fatalf("GetProjectFields: %v", err)
		}
		f := fieldByName(t, fields, "Urgency")
		if f.IssueField == nil || f.IssueField.ID != "IFSS_pri" {
			t.Fatalf("Urgency should pair with IFSS_pri via item value, got %+v", f.IssueField)
		}
	})
}

func TestGetProjectFields_OverridePinsAndExcludes(t *testing.T) {
	c, _ := newScriptedClient(t, func(req scriptedRequest) string {
		switch req.Op {
		case "GetProjectFields":
			return projectFieldsJSON("Organization", "acme",
				singleSelectNode("PVTSSF_pri", "Priority"),
				singleSelectNode("PVTSSF_sev", "Severity"))
		case "GetOrganizationIssueFields":
			return orgIssueFieldsJSON(false, "",
				issueSingleSelectNode("IFSS_pri", "Priority", "P0"),
				issueSingleSelectNode("IFSS_effort", "Effort", "High"))
		case "GetProjectIssueFieldPairs":
			return emptyPairsJSON
		}
		return ""
	})
	restore := SetIssueFieldOverridesLoaderForTesting(func() (map[string]string, error) {
		return map[string]string{"Priority": IssueFieldOverrideNone, "Severity": "IFSS_effort"}, nil
	})
	defer restore()

	fields, err := c.GetProjectFields("PVT_org")
	if err != nil {
		t.Fatalf("GetProjectFields: %v", err)
	}
	if f := fieldByName(t, fields, "Priority"); f.IssueField != nil {
		t.Errorf("override %q should keep Priority on the project path", IssueFieldOverrideNone)
	}
	if f := fieldByName(t, fields, "Severity"); f.IssueField == nil || f.IssueField.ID != "IFSS_effort" {
		t.Errorf("override should pin Severity to IFSS_effort, got %+v", f.IssueField)
	}
}

func TestGetProjectFields_OverrideNamingUnknownIssueFieldFails(t *testing.T) {
	c, _ := newScriptedClient(t, rubricalOrgHandler)
	restore := SetIssueFieldOverridesLoaderForTesting(func() (map[string]string, error) {
		return map[string]string{"Priority": "IFSS_missing"}, nil
	})
	defer restore()

	if _, err := c.GetProjectFields("PVT_org"); err == nil || !strings.Contains(err.Error(), "IFSS_missing") {
		t.Fatalf("expected an error naming the unknown override target, got %v", err)
	}
}

func TestGetProjectFields_OrgIssueFieldsDenied_FailsLoud(t *testing.T) {
	c, _ := newScriptedClient(t, func(req scriptedRequest) string {
		switch req.Op {
		case "GetProjectFields":
			return projectFieldsJSON("Organization", "acme", singleSelectNode("PVTSSF_pri", "Priority"))
		case "GetOrganizationIssueFields":
			return `{"data":{"organization":null},"errors":[{"type":"FORBIDDEN","path":["organization","issueFields"],"message":"Resource not accessible by personal access token"}]}`
		}
		return ""
	})

	fields, err := c.GetProjectFields("PVT_org")
	if err == nil {
		t.Fatalf("expected discovery to fail when organization.issueFields is denied; got %d fields", len(fields))
	}
	msg := err.Error()
	if !strings.Contains(msg, "organization.issueFields") {
		t.Errorf("error should name organization.issueFields: %v", msg)
	}
	if !strings.Contains(msg, "Resource not accessible by personal access token") {
		t.Errorf("error should include GitHub's message: %v", msg)
	}
}

func TestGetProjectFields_OrgIssueFieldsPaginate(t *testing.T) {
	c, transport := newScriptedClient(t, func(req scriptedRequest) string {
		switch req.Op {
		case "GetProjectFields":
			return projectFieldsJSON("Organization", "acme",
				singleSelectNode("PVTSSF_pri", "Priority"),
				plainFieldNode("PVTF_target", "Target date", "DATE"))
		case "GetOrganizationIssueFields":
			if req.Variables["cursor"] == nil {
				return orgIssueFieldsJSON(true, "page2", issueSingleSelectNode("IFSS_pri", "Priority", "P0"))
			}
			return orgIssueFieldsJSON(false, "", issueFieldNode("IssueFieldDate", "IFD_target", "Target date", "DATE"))
		case "GetProjectIssueFieldPairs":
			return emptyPairsJSON
		}
		return ""
	})

	fields, err := c.GetProjectFields("PVT_org")
	if err != nil {
		t.Fatalf("GetProjectFields: %v", err)
	}
	if got := len(transport.requestsFor("GetOrganizationIssueFields")); got != 2 {
		t.Errorf("issueFields pages fetched = %d, want 2", got)
	}
	if f := fieldByName(t, fields, "Target date"); f.IssueField == nil || f.IssueField.ID != "IFD_target" {
		t.Errorf("Target date from page 2 not correlated: %+v", f.IssueField)
	}
}

func TestGetProjectFields_UnsupportedIssueFieldKindRecorded(t *testing.T) {
	c, _ := newScriptedClient(t, func(req scriptedRequest) string {
		switch req.Op {
		case "GetProjectFields":
			return projectFieldsJSON("Organization", "acme", plainFieldNode("PVTF_notes", "Notes", "TEXT"))
		case "GetOrganizationIssueFields":
			return orgIssueFieldsJSON(false, "", issueFieldNode("IssueFieldText", "IFT_notes", "Notes", "TEXT"))
		case "GetProjectIssueFieldPairs":
			return emptyPairsJSON
		}
		return ""
	})

	fields, err := c.GetProjectFields("PVT_org")
	if err != nil {
		t.Fatalf("GetProjectFields: %v", err)
	}
	f := fieldByName(t, fields, "Notes")
	if f.IssueField == nil || f.IssueField.DataType != "TEXT" {
		t.Fatalf("Notes should be recorded as a TEXT issue field, got %+v", f.IssueField)
	}
}
