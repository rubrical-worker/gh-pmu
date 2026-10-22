package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
)

// Init caches the org issue field classification of each field (#932).

func TestFieldMetadataFromProjectField_RecordsKind(t *testing.T) {
	issueBacked := fieldMetadataFromProjectField(api.ProjectField{
		ID: "PVTSSF_pri", Name: "Priority", DataType: "SINGLE_SELECT",
		Options:    []api.FieldOption{{ID: "IFSSO_P2", Name: "P2"}},
		IssueField: &api.IssueFieldInfo{ID: "IFSS_pri", DataType: "SINGLE_SELECT"},
	})
	if issueBacked.Kind != api.FieldKindIssueField || issueBacked.IssueFieldID != "IFSS_pri" || issueBacked.IssueFieldDataType != "SINGLE_SELECT" {
		t.Errorf("issue-backed metadata = %+v", issueBacked)
	}
	if issueBacked.ID != "PVTSSF_pri" {
		t.Errorf("project field ID = %q, want it kept", issueBacked.ID)
	}
	if len(issueBacked.Options) != 1 || issueBacked.Options[0].ID != "IFSSO_P2" {
		t.Errorf("issue-backed options = %+v, want the issue field's option IDs", issueBacked.Options)
	}

	project := fieldMetadataFromProjectField(api.ProjectField{ID: "PVTSSF_status", Name: "Status", DataType: "SINGLE_SELECT"})
	if project.Kind != api.FieldKindProject || project.IssueFieldID != "" {
		t.Errorf("project metadata = %+v", project)
	}
}

// init's per-name fallback cannot classify, so recording PROJECT_FIELD would
// stamp an issue-backed Priority as a project field and defeat the cache guard
// (#933). Its fields are cached with no kind, which the guard refuses.
func TestFieldMetadataFromProjectField_UnclassifiedLeavesKindUnset(t *testing.T) {
	fm := fieldMetadataFromProjectField(api.ProjectField{
		ID: "PVTSSF_pri", Name: "Priority", DataType: "SINGLE_SELECT", Unclassified: true,
	})
	if fm.Kind != "" {
		t.Errorf("Kind = %q, want unset for a field the per-name fallback could not classify", fm.Kind)
	}
	if fm.ID != "PVTSSF_pri" || fm.Name != "Priority" {
		t.Errorf("identity not kept: %+v", fm)
	}
}

func TestWriteConfigWithMetadata_CachesIssueFieldKind(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &InitConfig{ProjectOwner: "acme", ProjectNumber: 2, Repositories: []string{"acme/app"}}
	metadata := &ProjectMetadata{
		ProjectID: "PVT_org",
		Fields: []FieldMetadata{
			fieldMetadataFromProjectField(api.ProjectField{
				ID: "PVTSSF_status", Name: "Status", DataType: "SINGLE_SELECT",
				Options: []api.FieldOption{{ID: "opt_backlog", Name: "Backlog"}},
			}),
			fieldMetadataFromProjectField(api.ProjectField{
				ID: "PVTSSF_pri", Name: "Priority", DataType: "SINGLE_SELECT",
				Options:    []api.FieldOption{{ID: "IFSSO_P2", Name: "P2"}},
				IssueField: &api.IssueFieldInfo{ID: "IFSS_pri", DataType: "SINGLE_SELECT"},
			}),
		},
	}

	if err := writeConfigWithMetadata(tmpDir, cfg, metadata); err != nil {
		t.Fatalf("writeConfigWithMetadata: %v", err)
	}

	loaded, err := config.Load(filepath.Join(tmpDir, ".gh-pmu.json"))
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	byName := map[string]config.FieldMetadata{}
	for _, f := range loaded.Metadata.Fields {
		byName[f.Name] = f
	}
	pri := byName["Priority"]
	if pri.Kind != api.FieldKindIssueField || pri.IssueFieldID != "IFSS_pri" || pri.IssueFieldDataType != "SINGLE_SELECT" {
		t.Errorf("cached Priority = %+v", pri)
	}
	if len(pri.Options) != 1 || pri.Options[0].ID != "IFSSO_P2" {
		t.Errorf("cached Priority options = %+v", pri.Options)
	}
	if byName["Status"].Kind != api.FieldKindProject {
		t.Errorf("cached Status kind = %q, want %q", byName["Status"].Kind, api.FieldKindProject)
	}
	if got := loaded.Fields["priority"].Values["p2"]; got != "P2" {
		t.Errorf("priority alias p2 = %q, want P2 from the issue field's options", got)
	}
}

func TestWriteConfigWithMetadata_ReInitKeepsIssueFieldOverride(t *testing.T) {
	tmpDir := t.TempDir()
	existing := `{
  "project": {"owner": "acme", "number": 2},
  "repositories": ["acme/app"],
  "fields": {"priority": {"field": "Priority", "values": {"p2": "P2"}, "issue_field": "none"}}
}`
	if err := os.WriteFile(filepath.Join(tmpDir, ".gh-pmu.json"), []byte(existing), 0600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	cfg := &InitConfig{ProjectOwner: "acme", ProjectNumber: 2, Repositories: []string{"acme/app"}}
	metadata := &ProjectMetadata{
		ProjectID: "PVT_org",
		Fields: []FieldMetadata{{
			ID: "PVTSSF_pri", Name: "Priority", DataType: "SINGLE_SELECT", Kind: api.FieldKindProject,
			Options: []OptionMetadata{{ID: "opt_p2", Name: "P2"}},
		}},
	}
	if err := writeConfigWithMetadata(tmpDir, cfg, metadata); err != nil {
		t.Fatalf("writeConfigWithMetadata: %v", err)
	}

	loaded, err := config.Load(filepath.Join(tmpDir, ".gh-pmu.json"))
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if got := loaded.Fields["priority"].IssueField; got != "none" {
		t.Errorf("re-init dropped the priority issue_field override: got %q", got)
	}
}

// An override on an alias init does not regenerate was dropped with its alias,
// silently reverting the user's classification decision (#936).
func TestWriteConfigWithMetadata_ReInitKeepsOverrideOnUngeneratedAlias(t *testing.T) {
	tmpDir := t.TempDir()
	existing := `{
  "project": {"owner": "acme", "number": 2},
  "repositories": ["acme/app"],
  "fields": {"severity": {"field": "Severity", "values": {"high": "High"}, "issue_field": "none"}}
}`
	if err := os.WriteFile(filepath.Join(tmpDir, ".gh-pmu.json"), []byte(existing), 0600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	cfg := &InitConfig{ProjectOwner: "acme", ProjectNumber: 2, Repositories: []string{"acme/app"}}
	metadata := &ProjectMetadata{
		ProjectID: "PVT_org",
		Fields: []FieldMetadata{{
			ID: "PVTSSF_status", Name: "Status", DataType: "SINGLE_SELECT", Kind: api.FieldKindProject,
			Options: []OptionMetadata{{ID: "opt_backlog", Name: "Backlog"}},
		}},
	}
	if err := writeConfigWithMetadata(tmpDir, cfg, metadata); err != nil {
		t.Fatalf("writeConfigWithMetadata: %v", err)
	}

	loaded, err := config.Load(filepath.Join(tmpDir, ".gh-pmu.json"))
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	severity, ok := loaded.Fields["severity"]
	if !ok {
		t.Fatalf("re-init dropped the severity alias carrying an issue_field override; fields: %v", loaded.Fields)
	}
	if severity.Field != "Severity" || severity.IssueField != "none" || severity.Values["high"] != "High" {
		t.Errorf("severity alias = %+v, want it kept as written", severity)
	}
}

// Re-init rebuilt the file from a fixed struct, so top-level keys it does not
// write vanished — configIntegrity: "strict" among them, silently turning the
// integrity check off (#936).
func TestWriteConfigWithMetadata_ReInitKeepsUnwrittenTopLevelKeys(t *testing.T) {
	tmpDir := t.TempDir()
	existing := `{
  "project": {"owner": "acme", "number": 2},
  "repositories": ["acme/app"],
  "configIntegrity": "strict",
  "customKey": {"nested": [1, 2]}
}`
	if err := os.WriteFile(filepath.Join(tmpDir, ".gh-pmu.json"), []byte(existing), 0600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	cfg := &InitConfig{ProjectOwner: "acme", ProjectNumber: 2, Repositories: []string{"acme/app"}}
	if err := writeConfigWithMetadata(tmpDir, cfg, &ProjectMetadata{ProjectID: "PVT_org"}); err != nil {
		t.Fatalf("writeConfigWithMetadata: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(tmpDir, ".gh-pmu.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("written config is not valid JSON: %v\n%s", err, raw)
	}
	if string(got["configIntegrity"]) != `"strict"` {
		t.Errorf("configIntegrity = %s, want \"strict\" kept across re-init", got["configIntegrity"])
	}
	var customKey bytes.Buffer
	if err := json.Compact(&customKey, got["customKey"]); err != nil || customKey.String() != `{"nested":[1,2]}` {
		t.Errorf("customKey = %s, want it kept across re-init", got["customKey"])
	}
	if !strings.Contains(string(raw), "\n  \"configIntegrity\": \"strict\"") {
		t.Errorf("kept keys should be indented like the rest of the file:\n%s", raw)
	}
}
