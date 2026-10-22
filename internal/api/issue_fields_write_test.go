package api

import (
	"encoding/json"
	"strings"
	"testing"
)

// Write-path tests for organization issue fields (#932). Each asserts which
// mutation a field used: setIssueFieldValue for issue-backed fields,
// updateProjectV2ItemFieldValue for ordinary project fields.

const itemContentJSON = `{"data":{"node":{"content":{"__typename":"Issue","id":"I_1"}}}}`

const setIssueFieldValueJSON = `{"data":{"setIssueFieldValue":{"clientMutationId":""}}}`

const updateProjectFieldJSON = `{"data":{"updateProjectV2ItemFieldValue":{"clientMutationId":""}}}`

// withWrites extends a discovery handler with the item-content lookup and both
// mutations.
func withWrites(discovery func(req scriptedRequest) string) func(req scriptedRequest) string {
	return func(req scriptedRequest) string {
		switch req.Op {
		case "GetProjectItemContent":
			return itemContentJSON
		case "SetIssueFieldValue":
			return setIssueFieldValueJSON
		case "UpdateProjectV2ItemFieldValue":
			return updateProjectFieldJSON
		}
		if req.Op == "BatchUpdate" {
			// One alias per update.
			var aliases []string
			for i := 0; strings.Contains(req.Query, aliasName(i)+":"); i++ {
				aliases = append(aliases, `"`+aliasName(i)+`":{"projectV2Item":{"id":"PVTI_1"}}`)
			}
			return `{"data":{` + strings.Join(aliases, ",") + `}}`
		}
		return discovery(req)
	}
}

func aliasName(i int) string {
	return "u" + string(rune('0'+i))
}

// issueFieldInput decodes the single issueFields entry of a SetIssueFieldValue
// request.
func issueFieldInput(t *testing.T, req scriptedRequest) (issueID string, entry map[string]interface{}) {
	t.Helper()
	raw, _ := json.Marshal(req.Variables["input"])
	var input struct {
		IssueID     string                   `json:"issueId"`
		IssueFields []map[string]interface{} `json:"issueFields"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatalf("decode setIssueFieldValue input: %v", err)
	}
	if len(input.IssueFields) != 1 {
		t.Fatalf("issueFields entries = %d, want 1: %s", len(input.IssueFields), raw)
	}
	return input.IssueID, input.IssueFields[0]
}

func TestSetProjectItemField_MixedOrgBoard_RoutesPerField(t *testing.T) {
	c, transport := newScriptedClient(t, withWrites(rubricalOrgHandler))

	fields, err := c.GetProjectFields("PVT_org")
	if err != nil {
		t.Fatalf("GetProjectFields: %v", err)
	}

	if err := c.SetProjectItemFieldWithFields("PVT_org", "PVTI_1", "Priority", "P2", fields); err != nil {
		t.Fatalf("set Priority: %v", err)
	}
	if err := c.SetProjectItemFieldWithFields("PVT_org", "PVTI_1", "Status", "In progress", fields); err != nil {
		t.Fatalf("set Status: %v", err)
	}
	if err := c.SetProjectItemFieldWithFields("PVT_org", "PVTI_1", "Branch", "release/v1", fields); err != nil {
		t.Fatalf("set Branch: %v", err)
	}

	issueWrites := transport.requestsFor("SetIssueFieldValue")
	if len(issueWrites) != 1 {
		t.Fatalf("setIssueFieldValue calls = %d, want 1 (Priority only)", len(issueWrites))
	}
	issueID, entry := issueFieldInput(t, issueWrites[0])
	if issueID != "I_1" {
		t.Errorf("issueId = %q, want the item's Issue node ID I_1", issueID)
	}
	if entry["fieldId"] != "IFSS_pri" || entry["singleSelectOptionId"] != "IFSSO_P2" {
		t.Errorf("Priority entry = %v, want IFSS_pri / IFSSO_P2", entry)
	}

	projectWrites := transport.requestsFor("UpdateProjectV2ItemFieldValue")
	if len(projectWrites) != 2 {
		t.Fatalf("updateProjectV2ItemFieldValue calls = %d, want 2 (Status and Branch)", len(projectWrites))
	}
	for _, w := range projectWrites {
		raw, _ := json.Marshal(w.Variables["input"])
		if strings.Contains(string(raw), "IFSS_") {
			t.Errorf("project mutation carried an issue field ID: %s", raw)
		}
	}
}

func TestBatchUpdateProjectItemFields_MixedOrgBoard_SplitsByKind(t *testing.T) {
	c, transport := newScriptedClient(t, withWrites(rubricalOrgHandler))

	fields, err := c.GetProjectFields("PVT_org")
	if err != nil {
		t.Fatalf("GetProjectFields: %v", err)
	}

	results, err := c.BatchUpdateProjectItemFields("PVT_org", []FieldUpdate{
		{ItemID: "PVTI_1", FieldName: "Priority", Value: "P2"},
		{ItemID: "PVTI_1", FieldName: "Status", Value: "In progress"},
	}, fields)
	if err != nil {
		t.Fatalf("BatchUpdateProjectItemFields: %v", err)
	}
	for _, r := range results {
		if !r.Success {
			t.Errorf("%s update failed: %s", r.FieldName, r.Error)
		}
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}

	issueWrites := transport.requestsFor("SetIssueFieldValue")
	if len(issueWrites) != 1 {
		t.Fatalf("setIssueFieldValue calls = %d, want 1", len(issueWrites))
	}
	if _, entry := issueFieldInput(t, issueWrites[0]); entry["singleSelectOptionId"] != "IFSSO_P2" {
		t.Errorf("Priority entry = %v", entry)
	}

	batches := transport.requestsFor("BatchUpdate")
	if len(batches) != 1 {
		t.Fatalf("batch project mutations = %d, want 1", len(batches))
	}
	if !strings.Contains(batches[0].Query, "updateProjectV2ItemFieldValue") {
		t.Errorf("batch is not an updateProjectV2ItemFieldValue mutation: %s", batches[0].Query)
	}
	vars, _ := json.Marshal(batches[0].Variables)
	if strings.Contains(string(vars), "PVTSSF_pri") {
		t.Errorf("Priority leaked into the project batch mutation: %s", vars)
	}
	if !strings.Contains(string(vars), "PVTSSF_status") {
		t.Errorf("Status missing from the project batch mutation: %s", vars)
	}
}

func TestSetProjectItemField_OrgBoardOrdinaryPriority_UsesProjectMutation(t *testing.T) {
	c, transport := newScriptedClient(t, withWrites(func(req scriptedRequest) string {
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
	}))

	if err := c.SetProjectItemField("PVT_org", "PVTI_1", "Priority", "P2"); err != nil {
		t.Fatalf("SetProjectItemField: %v", err)
	}
	if n := len(transport.requestsFor("SetIssueFieldValue")); n != 0 {
		t.Errorf("setIssueFieldValue calls = %d, want 0 for an ordinary project field", n)
	}
	writes := transport.requestsFor("UpdateProjectV2ItemFieldValue")
	if len(writes) != 1 {
		t.Fatalf("updateProjectV2ItemFieldValue calls = %d, want 1", len(writes))
	}
	raw, _ := json.Marshal(writes[0].Variables["input"])
	if !strings.Contains(string(raw), "PVTSSF_pri_opt2") {
		t.Errorf("project mutation should use the project option ID: %s", raw)
	}
}

func TestSetProjectItemField_UserBoard_UsesProjectMutationOnly(t *testing.T) {
	c, transport := newScriptedClient(t, withWrites(func(req scriptedRequest) string {
		if req.Op == "GetProjectFields" {
			return projectFieldsJSON("User", "",
				singleSelectNode("PVTSSF_pri", "Priority", "P0", "P1", "P2"),
				singleSelectNode("PVTSSF_status", "Status", "Backlog", "In progress"))
		}
		return ""
	}))

	if err := c.SetProjectItemField("PVT_user", "PVTI_1", "Priority", "P2"); err != nil {
		t.Fatalf("set Priority: %v", err)
	}
	if err := c.SetProjectItemField("PVT_user", "PVTI_1", "Status", "In progress"); err != nil {
		t.Fatalf("set Status: %v", err)
	}
	if n := len(transport.requestsFor("UpdateProjectV2ItemFieldValue")); n != 2 {
		t.Errorf("updateProjectV2ItemFieldValue calls = %d, want 2", n)
	}
	for _, op := range transport.ops() {
		switch op {
		case "GetOrganizationIssueFields", "GetProjectIssueFieldPairs", "SetIssueFieldValue", "GetProjectItemContent":
			t.Errorf("user-owned board issued %s", op)
		}
	}
}

func TestSetProjectItemField_DateIssueField(t *testing.T) {
	t.Run("sets dateValue", func(t *testing.T) {
		c, transport := newScriptedClient(t, withWrites(rubricalOrgHandler))
		fields, err := c.GetProjectFields("PVT_org")
		if err != nil {
			t.Fatalf("GetProjectFields: %v", err)
		}
		if err := c.SetProjectItemFieldWithFields("PVT_org", "PVTI_1", "Start date", "2026-10-22", fields); err != nil {
			t.Fatalf("set Start date: %v", err)
		}
		writes := transport.requestsFor("SetIssueFieldValue")
		if len(writes) != 1 {
			t.Fatalf("setIssueFieldValue calls = %d, want 1", len(writes))
		}
		_, entry := issueFieldInput(t, writes[0])
		if entry["fieldId"] != "IFD_start" || entry["dateValue"] != "2026-10-22" {
			t.Errorf("Start date entry = %v, want IFD_start / 2026-10-22", entry)
		}
		if n := len(transport.requestsFor("UpdateProjectV2ItemFieldValue")); n != 0 {
			t.Errorf("date issue field also hit the project mutation %d time(s)", n)
		}
	})

	t.Run("empty value deletes", func(t *testing.T) {
		c, transport := newScriptedClient(t, withWrites(rubricalOrgHandler))
		fields, _ := c.GetProjectFields("PVT_org")
		if err := c.SetProjectItemFieldWithFields("PVT_org", "PVTI_1", "Start date", "", fields); err != nil {
			t.Fatalf("clear Start date: %v", err)
		}
		writes := transport.requestsFor("SetIssueFieldValue")
		if len(writes) != 1 {
			t.Fatalf("setIssueFieldValue calls = %d, want 1", len(writes))
		}
		if _, entry := issueFieldInput(t, writes[0]); entry["delete"] != true {
			t.Errorf("clear entry = %v, want delete:true", entry)
		}
	})

	t.Run("invalid date is rejected before any write", func(t *testing.T) {
		c, transport := newScriptedClient(t, withWrites(rubricalOrgHandler))
		fields, _ := c.GetProjectFields("PVT_org")
		if err := c.SetProjectItemFieldWithFields("PVT_org", "PVTI_1", "Start date", "22/10/2026", fields); err == nil {
			t.Fatal("expected an error for a malformed date")
		}
		if n := len(transport.requestsFor("SetIssueFieldValue")); n != 0 {
			t.Errorf("malformed date still sent %d write(s)", n)
		}
	})
}

func TestSetProjectItemField_IssueFieldUnresolvableValueFails(t *testing.T) {
	c, transport := newScriptedClient(t, withWrites(rubricalOrgHandler))
	fields, _ := c.GetProjectFields("PVT_org")

	err := c.SetProjectItemFieldWithFields("PVT_org", "PVTI_1", "Priority", "P9", fields)
	if err == nil || !strings.Contains(err.Error(), `option "P9" not found`) {
		t.Fatalf("expected option-not-found error, got %v", err)
	}
	if n := len(transport.requestsFor("SetIssueFieldValue")); n != 0 {
		t.Errorf("unresolvable value still sent %d write(s)", n)
	}

	results, err := c.BatchUpdateProjectItemFields("PVT_org", []FieldUpdate{
		{ItemID: "PVTI_1", FieldName: "Priority", Value: "P9"},
	}, fields)
	if err != nil {
		t.Fatalf("BatchUpdateProjectItemFields: %v", err)
	}
	if len(results) != 1 || results[0].Success || !strings.Contains(results[0].Error, `option "P9" not found`) {
		t.Errorf("batch result = %+v, want a failed result naming the option", results)
	}
}

func TestSetProjectItemField_UnsupportedIssueFieldKindFails(t *testing.T) {
	for _, kind := range []string{"MULTI_SELECT", "NUMBER", "TEXT"} {
		t.Run(kind, func(t *testing.T) {
			c, transport := newScriptedClient(t, withWrites(rubricalOrgHandler))
			fields := []ProjectField{{
				ID: "PVTF_x", Name: "Tags", DataType: "TEXT",
				IssueField: &IssueFieldInfo{ID: "IF_x", DataType: kind},
			}}
			err := c.SetProjectItemFieldWithFields("PVT_org", "PVTI_1", "Tags", "a", fields)
			if err == nil || !strings.Contains(err.Error(), kind) {
				t.Fatalf("expected an error naming %s, got %v", kind, err)
			}
			if n := len(transport.requestsFor("SetIssueFieldValue")) + len(transport.requestsFor("UpdateProjectV2ItemFieldValue")); n != 0 {
				t.Errorf("unsupported kind still sent %d write(s)", n)
			}
		})
	}
}

func TestSetProjectItemField_CachedIssueFieldNeverDrivesWrite(t *testing.T) {
	c, transport := newScriptedClient(t, withWrites(rubricalOrgHandler))
	fields := []ProjectField{{
		ID: "PVTSSF_pri", Name: "Priority", DataType: "SINGLE_SELECT", FromCache: true,
		Options:    []FieldOption{{ID: "IFSSO_P2", Name: "P2"}},
		IssueField: &IssueFieldInfo{ID: "IFSS_pri", DataType: "SINGLE_SELECT"},
	}}

	err := c.SetProjectItemFieldWithFields("PVT_org", "PVTI_1", "Priority", "P2", fields)
	if err == nil || !strings.Contains(err.Error(), "cached") {
		t.Fatalf("expected a fail-loud error for a cached issue field, got %v", err)
	}
	if n := len(transport.requestsFor("SetIssueFieldValue")); n != 0 {
		t.Errorf("cached metadata drove %d setIssueFieldValue write(s)", n)
	}

	results, err := c.BatchUpdateProjectItemFields("PVT_org", []FieldUpdate{
		{ItemID: "PVTI_1", FieldName: "Priority", Value: "P2"},
	}, fields)
	if err != nil {
		t.Fatalf("BatchUpdateProjectItemFields: %v", err)
	}
	if len(results) != 1 || results[0].Success {
		t.Errorf("batch result = %+v, want a failed result", results)
	}
	if n := len(transport.requestsFor("SetIssueFieldValue")); n != 0 {
		t.Errorf("cached metadata drove %d batch write(s)", n)
	}
}
