package api

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Cached-metadata handling of organization issue fields (#932).

const resolverCrash = "GraphQL: Something went wrong while executing your query on 2026-05-14T18:07:30Z. Please include `E290:183153:9C0F68F:2723794C:6A060F61` when reporting this issue."

// withConfigFixture writes .gh-pmu.json into a temp dir and chdirs into it so
// the production cache loader reads it.
func withConfigFixture(t *testing.T, metadataFields string) {
	t.Helper()
	dir := t.TempDir()
	content := `{
  "project": {"owner": "acme", "number": 2},
  "repositories": ["acme/app"],
  "fields": {"priority": {"field": "Priority", "values": {"p2": "P2"}}},
  "metadata": {"project": {"id": "PVT_org"}, "fields": [` + metadataFields + `]}
}`
	if err := os.WriteFile(filepath.Join(dir, ".gh-pmu.json"), []byte(content), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func crashingFieldsClient() *Client {
	return NewClientWithGraphQL(&queryMockClient{
		queryFunc: func(name string, query interface{}, variables map[string]interface{}) error {
			return errors.New(resolverCrash)
		},
	})
}

func TestGetProjectFields_Fallback_PreFixIssueFieldCacheFailsNamingInit(t *testing.T) {
	resetFieldsCacheWarningForTesting()
	// Written by a gh pmu that predates #932: Priority is the org issue field's
	// option-less project projection, with no kind recorded.
	withConfigFixture(t, `
    {"name": "Status", "id": "PVTSSF_status", "data_type": "SINGLE_SELECT",
     "options": [{"name": "Backlog", "id": "opt_backlog"}]},
    {"name": "Priority", "id": "PVTSSF_pri", "data_type": "SINGLE_SELECT"}`)

	fields, err := crashingFieldsClient().GetProjectFields("PVT_org")
	if err == nil {
		t.Fatalf("expected the stale cache to be refused, got %d fields", len(fields))
	}
	msg := err.Error()
	if !strings.Contains(msg, "gh pmu init") {
		t.Errorf("error should name 'gh pmu init' as the remedy: %v", msg)
	}
	if !strings.Contains(msg, "Priority") {
		t.Errorf("error should name the stale field: %v", msg)
	}
}

// The guard keyed only on the single-select shape, so a pre-#932 entry for an
// issue-backed DATE field passed it and was routed to the project-field write
// (#933). Any entry with no recorded kind is unclassified, whatever its type.
func TestGetProjectFields_Fallback_PreFixDateFieldCacheFailsNamingInit(t *testing.T) {
	resetFieldsCacheWarningForTesting()
	withConfigFixture(t, `
    {"name": "Status", "id": "PVTSSF_status", "data_type": "SINGLE_SELECT", "kind": "PROJECT_FIELD",
     "options": [{"name": "Backlog", "id": "opt_backlog"}]},
    {"name": "Start date", "id": "PVTF_start", "data_type": "DATE"}`)

	fields, err := crashingFieldsClient().GetProjectFields("PVT_org")
	if err == nil {
		t.Fatalf("expected the unclassified DATE entry to be refused, got %d fields", len(fields))
	}
	msg := err.Error()
	if !strings.Contains(msg, "gh pmu init") {
		t.Errorf("error should name 'gh pmu init' as the remedy: %v", msg)
	}
	if !strings.Contains(msg, "Start date") {
		t.Errorf("error should name the unclassified field: %v", msg)
	}
	if strings.Contains(msg, "Status") {
		t.Errorf("Status has a recorded kind and should not be named: %v", msg)
	}
}

// An issue field entry without its issue field ID cannot be written through
// setIssueFieldValue; status --update used to write exactly that shape (#933).
func TestGetProjectFields_Fallback_IncompleteIssueFieldCacheFailsNamingInit(t *testing.T) {
	resetFieldsCacheWarningForTesting()
	withConfigFixture(t, `
    {"name": "Status", "id": "PVTSSF_status", "data_type": "SINGLE_SELECT", "kind": "ISSUE_FIELD",
     "options": [{"name": "Backlog", "id": "opt_backlog"}]}`)

	_, err := crashingFieldsClient().GetProjectFields("PVT_org")
	if err == nil || !strings.Contains(err.Error(), "gh pmu init") || !strings.Contains(err.Error(), "Status") {
		t.Fatalf("expected the incomplete issue field entry to be refused naming Status and 'gh pmu init', got: %v", err)
	}
}

func TestGetProjectFields_Fallback_LoadsCachedIssueFieldKind(t *testing.T) {
	resetFieldsCacheWarningForTesting()
	withConfigFixture(t, `
    {"name": "Status", "id": "PVTSSF_status", "data_type": "SINGLE_SELECT", "kind": "PROJECT_FIELD",
     "options": [{"name": "Backlog", "id": "opt_backlog"}]},
    {"name": "Priority", "id": "PVTSSF_pri", "data_type": "SINGLE_SELECT", "kind": "ISSUE_FIELD",
     "issue_field_id": "IFSS_pri", "issue_field_data_type": "SINGLE_SELECT",
     "options": [{"name": "P2", "id": "IFSSO_P2"}]}`)

	fields, err := crashingFieldsClient().GetProjectFields("PVT_org")
	if err != nil {
		t.Fatalf("GetProjectFields: %v", err)
	}
	pri := fieldByName(t, fields, "Priority")
	if pri.IssueField == nil || pri.IssueField.ID != "IFSS_pri" || pri.IssueField.DataType != "SINGLE_SELECT" {
		t.Fatalf("cached Priority issue field = %+v, want IFSS_pri SINGLE_SELECT", pri.IssueField)
	}
	if !pri.FromCache {
		t.Error("cached field must be marked FromCache")
	}
	if len(pri.Options) != 1 || pri.Options[0].ID != "IFSSO_P2" {
		t.Errorf("cached Priority options = %v", optionNames(pri.Options))
	}
	if status := fieldByName(t, fields, "Status"); status.IssueField != nil {
		t.Errorf("Status cached as PROJECT_FIELD came back as an issue field")
	}
}

func TestRefreshCachedFields_KeepsIssueBackedEntries(t *testing.T) {
	cached := []ProjectField{
		{ID: "PVTSSF_status", Name: "Status", DataType: "SINGLE_SELECT", FromCache: true},
		{ID: "PVTSSF_pri", Name: "Priority", DataType: "SINGLE_SELECT", FromCache: true,
			IssueField: &IssueFieldInfo{ID: "IFSS_pri", DataType: "SINGLE_SELECT"}},
	}
	var fetched []string
	fetcher := func(projectID, name string) (*ProjectField, error) {
		fetched = append(fetched, name)
		return &ProjectField{ID: "PVTSSF_" + name, Name: name, DataType: "SINGLE_SELECT"}, nil
	}

	out, _, _ := refreshCachedFields(fetcher, "PVT_org", cached)

	if strings.Join(fetched, ",") != "Status" {
		t.Errorf("per-name refresh fetched %v; the project-side lookup cannot refresh an issue field, so Priority must not be re-fetched", fetched)
	}
	pri := fieldByName(t, out, "Priority")
	if pri.IssueField == nil || !pri.FromCache {
		t.Errorf("issue-backed cached entry not retained as cached: %+v", pri)
	}
}
