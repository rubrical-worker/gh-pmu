package cmd

import (
	"os"
	"path/filepath"
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
