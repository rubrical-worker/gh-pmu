package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/spf13/cobra"
)

// setupFieldTestDir creates a temp directory with a .gh-pmu.json config file
// and changes to that directory. Returns cleanup function to restore original dir.
func setupFieldTestDir(t *testing.T, cfg *config.Config) func() {
	t.Helper()

	// Save original directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}

	// Create temp directory
	tempDir := t.TempDir()

	// Save config to temp directory
	configPath := filepath.Join(tempDir, ".gh-pmu.json")
	if err := cfg.Save(filepath.Dir(configPath)); err != nil {
		t.Fatalf("Failed to save test config: %v", err)
	}

	// Change to temp directory
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to chdir to temp dir: %v", err)
	}

	// Return cleanup function
	return func() {
		_ = os.Chdir(originalDir)
	}
}

// mockFieldClient implements fieldClient interface for testing
type mockFieldClient struct {
	project        *api.Project
	fields         []api.ProjectField
	createdField   *api.ProjectField
	getProjectErr  error
	getFieldsErr   error
	createFieldErr error
}

func (m *mockFieldClient) GetProject(owner string, number int) (*api.Project, error) {
	if m.getProjectErr != nil {
		return nil, m.getProjectErr
	}
	return m.project, nil
}

func (m *mockFieldClient) GetProjectFields(projectID string) ([]api.ProjectField, error) {
	if m.getFieldsErr != nil {
		return nil, m.getFieldsErr
	}
	return m.fields, nil
}

func (m *mockFieldClient) CreateProjectField(projectID, name, dataType string, singleSelectOptions []string) (*api.ProjectField, error) {
	if m.createFieldErr != nil {
		return nil, m.createFieldErr
	}
	if m.createdField != nil {
		return m.createdField, nil
	}
	// Return a default created field
	return &api.ProjectField{
		ID:       "PVTF_test123",
		Name:     name,
		DataType: dataType,
	}, nil
}

func newTestFieldCmd() (*cobra.Command, *bytes.Buffer) {
	cmd := newFieldCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	return cmd, buf
}

func TestRunFieldList_Success(t *testing.T) {
	cmd, buf := newTestFieldCmd()
	cmd.SetArgs([]string{"list"})

	mock := &mockFieldClient{
		project: &api.Project{
			ID:    "PVT_test",
			Title: "Test Project",
		},
		fields: []api.ProjectField{
			{ID: "F1", Name: "Status", DataType: "SINGLE_SELECT", Options: []api.FieldOption{
				{ID: "O1", Name: "Open"},
				{ID: "O2", Name: "Closed"},
			}},
			{ID: "F2", Name: "Priority", DataType: "SINGLE_SELECT", Options: []api.FieldOption{
				{ID: "O3", Name: "High"},
				{ID: "O4", Name: "Low"},
			}},
			{ID: "F3", Name: "PRD", DataType: "TEXT"},
		},
	}

	cfg := &config.Config{
		Project: config.Project{
			Owner:  "test-owner",
			Number: 1,
		},
		Repositories: []string{"test-owner/test-repo"},
	}

	err := runFieldListWithDeps(cmd, cfg, mock)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Test Project") {
		t.Errorf("expected project title in output, got %s", output)
	}
	if !strings.Contains(output, "Status (SINGLE_SELECT)") {
		t.Errorf("expected Status field in output, got %s", output)
	}
	if !strings.Contains(output, "PRD (TEXT)") {
		t.Errorf("expected PRD field in output, got %s", output)
	}
	if !strings.Contains(output, "Total: 3 fields") {
		t.Errorf("expected field count in output, got %s", output)
	}
}

func TestRunFieldCreate_TextFieldSuccess(t *testing.T) {
	cmd, buf := newTestFieldCmd()

	mock := &mockFieldClient{
		project: &api.Project{
			ID:    "PVT_test",
			Title: "Test Project",
		},
		fields: []api.ProjectField{}, // No existing fields
		createdField: &api.ProjectField{
			ID:       "PVTF_new",
			Name:     "PRD",
			DataType: "TEXT",
		},
	}

	cfg := &config.Config{
		Project: config.Project{
			Owner:  "test-owner",
			Number: 1,
		},
		Repositories: []string{"test-owner/test-repo"},
	}

	// Setup temp directory with config to avoid polluting repo root
	cleanup := setupFieldTestDir(t, cfg)
	defer cleanup()

	opts := &fieldCreateOptions{
		fieldType: "text",
		yes:       true, // Skip confirmation
	}

	err := runFieldCreateWithDeps(cmd, "PRD", opts, cfg, mock)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Created field \"PRD\"") {
		t.Errorf("expected success message in output, got %s", output)
	}
}

// field create caches the created field's own classification rather than a
// hard-coded PROJECT_FIELD, so the entry matches what the field is (#933).
func TestRunFieldCreate_RecordsCreatedFieldClassification(t *testing.T) {
	cases := []struct {
		name    string
		created api.ProjectField
		want    config.FieldMetadata
	}{
		{
			name:    "project field",
			created: api.ProjectField{ID: "PVTF_new", Name: "PRD", DataType: "TEXT"},
			want:    config.FieldMetadata{Name: "PRD", ID: "PVTF_new", DataType: "TEXT", Kind: api.FieldKindProject},
		},
		{
			name: "issue-backed field",
			created: api.ProjectField{ID: "PVTF_new", Name: "PRD", DataType: "TEXT",
				IssueField: &api.IssueFieldInfo{ID: "IFT_prd", DataType: "TEXT"}},
			want: config.FieldMetadata{Name: "PRD", ID: "PVTF_new", DataType: "TEXT", Kind: api.FieldKindIssueField,
				IssueFieldID: "IFT_prd", IssueFieldDataType: "TEXT"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, _ := newTestFieldCmd()
			created := tc.created
			mock := &mockFieldClient{project: &api.Project{ID: "PVT_test", Title: "Test Project"}, createdField: &created}
			cfg := &config.Config{Project: config.Project{Owner: "test-owner", Number: 1}, Repositories: []string{"test-owner/test-repo"}}
			cleanup := setupFieldTestDir(t, cfg)
			defer cleanup()

			if err := runFieldCreateWithDeps(cmd, "PRD", &fieldCreateOptions{fieldType: "text", yes: true}, cfg, mock); err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			saved, err := config.Load(".gh-pmu.json")
			if err != nil {
				t.Fatalf("reload config: %v", err)
			}
			if saved.Metadata == nil || len(saved.Metadata.Fields) != 1 {
				t.Fatalf("expected one metadata entry, got %+v", saved.Metadata)
			}
			got := saved.Metadata.Fields[0]
			if got.Kind != tc.want.Kind || got.IssueFieldID != tc.want.IssueFieldID || got.IssueFieldDataType != tc.want.IssueFieldDataType || got.ID != tc.want.ID {
				t.Errorf("metadata = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRunFieldCreate_FieldAlreadyExists(t *testing.T) {
	cmd, _ := newTestFieldCmd()

	mock := &mockFieldClient{
		project: &api.Project{
			ID:    "PVT_test",
			Title: "Test Project",
		},
		fields: []api.ProjectField{
			{ID: "F1", Name: "PRD", DataType: "TEXT"},
		},
	}

	cfg := &config.Config{
		Project: config.Project{
			Owner:  "test-owner",
			Number: 1,
		},
		Repositories: []string{"test-owner/test-repo"},
	}

	// Setup temp directory with config to avoid polluting repo root
	cleanup := setupFieldTestDir(t, cfg)
	defer cleanup()

	opts := &fieldCreateOptions{
		fieldType: "text",
		yes:       true,
	}

	err := runFieldCreateWithDeps(cmd, "PRD", opts, cfg, mock)
	if err == nil {
		t.Fatal("expected error for existing field, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got %v", err)
	}
}

func TestRunFieldCreate_SingleSelectWithOptions(t *testing.T) {
	cmd, buf := newTestFieldCmd()

	mock := &mockFieldClient{
		project: &api.Project{
			ID:    "PVT_test",
			Title: "Test Project",
		},
		fields: []api.ProjectField{},
		createdField: &api.ProjectField{
			ID:       "PVTSSF_new",
			Name:     "Environment",
			DataType: "SINGLE_SELECT",
			Options: []api.FieldOption{
				{ID: "O1", Name: "Development"},
				{ID: "O2", Name: "Production"},
			},
		},
	}

	cfg := &config.Config{
		Project: config.Project{
			Owner:  "test-owner",
			Number: 1,
		},
		Repositories: []string{"test-owner/test-repo"},
	}

	// Setup temp directory with config to avoid polluting repo root
	cleanup := setupFieldTestDir(t, cfg)
	defer cleanup()

	opts := &fieldCreateOptions{
		fieldType: "single_select",
		options:   []string{"Development", "Production"},
		yes:       true,
	}

	err := runFieldCreateWithDeps(cmd, "Environment", opts, cfg, mock)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Created field \"Environment\"") {
		t.Errorf("expected success message in output, got %s", output)
	}
}

func TestRunFieldCreate_SingleSelectRequiresOptions(t *testing.T) {
	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &fieldCreateOptions{
		fieldType: "single_select",
		options:   []string{},
		yes:       true,
	}

	err := runFieldCreate(cmd, []string{"Environment"}, opts)
	if err == nil {
		t.Fatal("Expected error for single_select without options")
	}
	if !strings.Contains(err.Error(), "at least one --option") {
		t.Errorf("Expected error about options, got: %v", err)
	}
}

func TestRunFieldCreate_InvalidFieldType(t *testing.T) {
	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	opts := &fieldCreateOptions{
		fieldType: "invalid_type",
		yes:       true,
	}

	err := runFieldCreate(cmd, []string{"TestField"}, opts)
	if err == nil {
		t.Fatal("Expected error for invalid field type")
	}
	if !strings.Contains(err.Error(), "invalid field type") {
		t.Errorf("Expected 'invalid field type' error, got: %v", err)
	}
}

func TestRunFieldCreate_ValidTypes(t *testing.T) {
	// Valid types should pass validation (may fail at config load, which is fine)
	validTypes := []string{"text", "number", "date"}

	for _, ft := range validTypes {
		t.Run(ft, func(t *testing.T) {
			cmd := &cobra.Command{}
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(buf)

			opts := &fieldCreateOptions{fieldType: ft, yes: true}
			err := runFieldCreate(cmd, []string{"TestField"}, opts)
			// Should NOT fail with "invalid field type" — may fail at config load
			if err != nil && strings.Contains(err.Error(), "invalid field type") {
				t.Errorf("Type %q should be valid, got: %v", ft, err)
			}
		})
	}
}

func TestRunFieldList_ShowsOrgIssueFieldKindAndOptions(t *testing.T) {
	cmd, buf := newTestFieldCmd()

	mock := &mockFieldClient{
		project: &api.Project{ID: "PVT_org", Title: "Org Project"},
		fields: []api.ProjectField{
			{ID: "F1", Name: "Status", DataType: "SINGLE_SELECT", Options: []api.FieldOption{{ID: "O1", Name: "Backlog"}}},
			{ID: "PVTSSF_pri", Name: "Priority", DataType: "SINGLE_SELECT",
				Options: []api.FieldOption{
					{ID: "IFSSO_P0", Name: "P0"}, {ID: "IFSSO_P1", Name: "P1"}, {ID: "IFSSO_P2", Name: "P2"},
				},
				IssueField: &api.IssueFieldInfo{ID: "IFSS_pri", DataType: "SINGLE_SELECT"}},
			{ID: "PVTF_start", Name: "Start date", DataType: "DATE",
				IssueField: &api.IssueFieldInfo{ID: "IFD_start", DataType: "DATE"}},
		},
	}
	cfg := &config.Config{
		Project:      config.Project{Owner: "acme", Number: 2},
		Repositories: []string{"acme/app"},
	}

	if err := runFieldListWithDeps(cmd, cfg, mock); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		"Status (SINGLE_SELECT)\n",
		"Priority (SINGLE_SELECT, org issue field)",
		"Options: P0, P1, P2",
		"Start date (DATE, org issue field)",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output, got:\n%s", want, output)
		}
	}
}
