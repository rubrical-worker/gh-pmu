package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_ValidConfig_ReturnsProjectDetails(t *testing.T) {
	// ARRANGE: Path to valid test config
	configPath := filepath.Join("..", "..", "testdata", "config", "valid.gh-pmu.yml")

	// ACT: Load the configuration
	cfg, err := Load(configPath)

	// ASSERT: No error and correct values
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cfg.Project.Owner != "rubrical-works" {
		t.Errorf("Expected owner 'rubrical-works', got '%s'", cfg.Project.Owner)
	}

	if cfg.Project.Number != 13 {
		t.Errorf("Expected project number 13, got %d", cfg.Project.Number)
	}
}

func TestLoad_MinimalConfig_ReturnsRequiredFields(t *testing.T) {
	// ARRANGE: Path to minimal test config
	configPath := filepath.Join("..", "..", "testdata", "config", "minimal.gh-pmu.yml")

	// ACT: Load the configuration
	cfg, err := Load(configPath)

	// ASSERT: No error and required fields present
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cfg.Project.Owner != "rubrical-works" {
		t.Errorf("Expected owner 'rubrical-works', got '%s'", cfg.Project.Owner)
	}

	if cfg.Project.Number != 13 {
		t.Errorf("Expected project number 13, got %d", cfg.Project.Number)
	}

	if len(cfg.Repositories) != 1 {
		t.Errorf("Expected 1 repository, got %d", len(cfg.Repositories))
	}
}

// TestLoad_PopulatedReleaseBlock_IsIgnored guards the removal of the release
// config section (#902). A config written by an older gh-pmu still carries a
// populated release block; loading one must stay silent rather than erroring on
// a key the struct no longer declares. This is a regression guard against a
// future strict decoder, not a test of the removal itself — Load uses plain
// json.Unmarshal, which already ignores unknown keys.
func TestLoad_PopulatedReleaseBlock_IsIgnored(t *testing.T) {
	// ARRANGE: a config in the pre-#902 shape, with release fully populated
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ConfigFileName)
	legacy := `{
  "project": {"name": "gh-pmu", "number": 11, "owner": "rubrical-worker"},
  "repositories": ["rubrical-worker/gh-pmu"],
  "release": {
    "tracks": {"stable": {"prefix": "v", "default": true}},
    "artifacts": {"directory": "Releases", "release_notes": true},
    "coverage": {"enabled": true, "threshold": 80}
  }
}`
	if err := os.WriteFile(configPath, []byte(legacy), 0600); err != nil {
		t.Fatalf("Failed to write legacy config: %v", err)
	}

	// ACT
	cfg, err := Load(configPath)

	// ASSERT: the unknown release key is ignored, not an error
	if err != nil {
		t.Fatalf("Expected a populated release block to load cleanly, got: %v", err)
	}
	if cfg.Project.Owner != "rubrical-worker" {
		t.Errorf("Expected owner rubrical-worker, got %q", cfg.Project.Owner)
	}
	if cfg.Project.Number != 11 {
		t.Errorf("Expected project number 11, got %d", cfg.Project.Number)
	}
}

func TestLoad_MissingFile_ReturnsError(t *testing.T) {
	// ARRANGE: Path to non-existent file
	configPath := filepath.Join("..", "..", "testdata", "config", "does-not-exist.yml")

	// ACT: Load the configuration
	_, err := Load(configPath)

	// ASSERT: Error is returned
	if err == nil {
		t.Fatal("Expected error for missing file, got nil")
	}
}

func TestLoad_InvalidYAML_ReturnsError(t *testing.T) {
	// ARRANGE: Path to invalid YAML
	configPath := filepath.Join("..", "..", "testdata", "config", "invalid-yaml-syntax.gh-pmu.yml")

	// ACT: Load the configuration
	_, err := Load(configPath)

	// ASSERT: Error is returned
	if err == nil {
		t.Fatal("Expected error for invalid YAML, got nil")
	}
}

func TestValidate_MissingOwner_ReturnsError(t *testing.T) {
	// ARRANGE: Config with missing owner
	cfg := &Config{
		Project: Project{
			Number: 13,
			// Owner is missing
		},
		Repositories: []string{"rubrical-works/gh-pm-test"},
	}

	// ACT: Validate the config
	err := cfg.Validate()

	// ASSERT: Error mentions owner
	if err == nil {
		t.Fatal("Expected validation error for missing owner, got nil")
	}
}

func TestValidate_MissingNumber_ReturnsError(t *testing.T) {
	// ARRANGE: Config with missing project number
	cfg := &Config{
		Project: Project{
			Owner: "rubrical-works",
			// Number is missing (zero value)
		},
		Repositories: []string{"rubrical-works/gh-pm-test"},
	}

	// ACT: Validate the config
	err := cfg.Validate()

	// ASSERT: Error mentions number
	if err == nil {
		t.Fatal("Expected validation error for missing project number, got nil")
	}
}

func TestValidate_MissingRepositories_ReturnsError(t *testing.T) {
	// ARRANGE: Config with no repositories
	cfg := &Config{
		Project: Project{
			Owner:  "rubrical-works",
			Number: 13,
		},
		Repositories: []string{}, // Empty
	}

	// ACT: Validate the config
	err := cfg.Validate()

	// ASSERT: Error mentions repositories
	if err == nil {
		t.Fatal("Expected validation error for missing repositories, got nil")
	}
}

func TestValidate_ValidConfig_ReturnsNil(t *testing.T) {
	// ARRANGE: Valid config
	cfg := &Config{
		Project: Project{
			Owner:  "rubrical-works",
			Number: 13,
		},
		Repositories: []string{"rubrical-works/gh-pm-test"},
	}

	// ACT: Validate the config
	err := cfg.Validate()

	// ASSERT: No error
	if err != nil {
		t.Fatalf("Expected no error for valid config, got: %v", err)
	}
}

func TestResolveFieldValue_WithAlias_ReturnsActualValue(t *testing.T) {
	// ARRANGE: Config with field aliases
	cfg := &Config{
		Fields: map[string]Field{
			"priority": {
				Field: "Priority",
				Values: map[string]string{
					"p0": "P0",
					"p1": "P1",
					"p2": "P2",
				},
			},
		},
	}

	// ACT: Resolve alias
	value := cfg.ResolveFieldValue("priority", "p1")

	// ASSERT: Returns actual value
	if value != "P1" {
		t.Errorf("Expected 'P1', got '%s'", value)
	}
}

func TestResolveFieldValue_NoAlias_ReturnsOriginal(t *testing.T) {
	// ARRANGE: Config with field aliases
	cfg := &Config{
		Fields: map[string]Field{
			"priority": {
				Field: "Priority",
				Values: map[string]string{
					"p0": "P0",
					"p1": "P1",
				},
			},
		},
	}

	// ACT: Try to resolve value that has no alias
	value := cfg.ResolveFieldValue("priority", "Unknown")

	// ASSERT: Returns original value unchanged
	if value != "Unknown" {
		t.Errorf("Expected 'Unknown', got '%s'", value)
	}
}

func TestResolveFieldValue_CaseInsensitiveAlias(t *testing.T) {
	// #869 finding 1: ResolveFieldValue must match aliases case-insensitively to
	// stay consistent with ValidateFieldValue. Otherwise a case-variant alias
	// passes validation but resolves to the literal input instead of the
	// configured GitHub field value.
	cfg := &Config{
		Fields: map[string]Field{
			"status": {
				Field: "Status",
				Values: map[string]string{
					"in_progress": "In progress",
				},
			},
		},
	}

	cases := []string{"In_Progress", "IN_PROGRESS", "in_progress"}
	for _, input := range cases {
		if got := cfg.ResolveFieldValue("status", input); got != "In progress" {
			t.Errorf("ResolveFieldValue(status, %q) = %q, want %q", input, got, "In progress")
		}
	}
}

func TestResolveFieldValue_UnknownField_ReturnsOriginal(t *testing.T) {
	// ARRANGE: Config with no fields configured
	cfg := &Config{
		Fields: map[string]Field{},
	}

	// ACT: Try to resolve unknown field
	value := cfg.ResolveFieldValue("unknown", "some-value")

	// ASSERT: Returns original value unchanged
	if value != "some-value" {
		t.Errorf("Expected 'some-value', got '%s'", value)
	}
}

func TestValidateFieldValue_ValidAlias_ReturnsNil(t *testing.T) {
	// ARRANGE: Config with field aliases
	cfg := &Config{
		Fields: map[string]Field{
			"status": {
				Field: "Status",
				Values: map[string]string{
					"backlog":     "Backlog",
					"in_progress": "In progress",
					"done":        "Done",
				},
			},
		},
	}

	// ACT: Validate valid alias
	err := cfg.ValidateFieldValue("status", "backlog")

	// ASSERT: Returns nil (valid)
	if err != nil {
		t.Errorf("Expected nil error for valid alias, got: %v", err)
	}
}

func TestValidateFieldValue_InvalidValue_ReturnsError(t *testing.T) {
	// ARRANGE: Config with field aliases
	cfg := &Config{
		Fields: map[string]Field{
			"status": {
				Field: "Status",
				Values: map[string]string{
					"backlog":     "Backlog",
					"in_progress": "In progress",
					"done":        "Done",
				},
			},
		},
	}

	// ACT: Validate invalid value
	err := cfg.ValidateFieldValue("status", "nonexistent")

	// ASSERT: Returns error with available values
	if err == nil {
		t.Fatal("Expected error for invalid value, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, `invalid status value "nonexistent"`) {
		t.Errorf("Expected error to contain invalid value message, got: %s", errStr)
	}
	if !strings.Contains(errStr, "Available values:") {
		t.Errorf("Expected error to list available values, got: %s", errStr)
	}
}

func TestValidateFieldValue_FieldNotConfigured_ReturnsNil(t *testing.T) {
	// ARRANGE: Config without the field
	cfg := &Config{
		Fields: map[string]Field{},
	}

	// ACT: Validate value for unconfigured field
	err := cfg.ValidateFieldValue("status", "anything")

	// ASSERT: Returns nil (pass-through behavior)
	if err != nil {
		t.Errorf("Expected nil error for unconfigured field, got: %v", err)
	}
}

func TestValidateFieldValue_CaseInsensitive(t *testing.T) {
	// ARRANGE: Config with lowercase aliases
	cfg := &Config{
		Fields: map[string]Field{
			"status": {
				Field: "Status",
				Values: map[string]string{
					"backlog": "Backlog",
				},
			},
		},
	}

	// ACT: Validate with uppercase input
	err := cfg.ValidateFieldValue("status", "BACKLOG")

	// ASSERT: Returns nil (case-insensitive match)
	if err != nil {
		t.Errorf("Expected nil error for case-insensitive match, got: %v", err)
	}
}

func TestValidateFieldValue_FieldWithNoValues_ReturnsNil(t *testing.T) {
	// ARRANGE: Config with field but no values defined
	cfg := &Config{
		Fields: map[string]Field{
			"status": {
				Field:  "Status",
				Values: map[string]string{}, // Empty values
			},
		},
	}

	// ACT: Validate any value
	err := cfg.ValidateFieldValue("status", "anything")

	// ASSERT: Returns nil (no values to validate against)
	if err != nil {
		t.Errorf("Expected nil error when no values defined, got: %v", err)
	}
}

func TestGetFieldName_WithMapping_ReturnsActualName(t *testing.T) {
	// ARRANGE: Config with field mapping
	cfg := &Config{
		Fields: map[string]Field{
			"priority": {
				Field: "Priority",
			},
			"status": {
				Field: "Status",
			},
		},
	}

	// ACT: Get actual field name
	name := cfg.GetFieldName("priority")

	// ASSERT: Returns mapped name
	if name != "Priority" {
		t.Errorf("Expected 'Priority', got '%s'", name)
	}
}

func TestGetFieldName_NoMapping_ReturnsOriginal(t *testing.T) {
	// ARRANGE: Config with no field mapping
	cfg := &Config{
		Fields: map[string]Field{},
	}

	// ACT: Get field name for unmapped field
	name := cfg.GetFieldName("SomeField")

	// ASSERT: Returns original name
	if name != "SomeField" {
		t.Errorf("Expected 'SomeField', got '%s'", name)
	}
}

func TestGetFieldNameOr_WithMapping_ReturnsActualName(t *testing.T) {
	cfg := &Config{
		Fields: map[string]Field{
			"status": {Field: "Workflow"},
		},
	}
	if got := cfg.GetFieldNameOr("status", "Status"); got != "Workflow" {
		t.Errorf("Expected 'Workflow', got '%s'", got)
	}
}

func TestGetFieldNameOr_NoMapping_ReturnsFallback(t *testing.T) {
	cfg := &Config{Fields: map[string]Field{}}
	if got := cfg.GetFieldNameOr("status", "Status"); got != "Status" {
		t.Errorf("Expected fallback 'Status', got '%s'", got)
	}
}

func TestGetFieldNameOr_EmptyFieldValue_ReturnsFallback(t *testing.T) {
	cfg := &Config{
		Fields: map[string]Field{
			"priority": {Field: ""},
		},
	}
	if got := cfg.GetFieldNameOr("priority", "Priority"); got != "Priority" {
		t.Errorf("Expected fallback 'Priority', got '%s'", got)
	}
}

func TestLoadFromDirectory_FindsConfigFile(t *testing.T) {
	// ARRANGE: Create a temporary .gh-pmu.json config file
	testDir := t.TempDir()
	configContent := `{"project":{"owner":"rubrical-works","number":13},"repositories":["rubrical-works/gh-pmu"]}`
	dstPath := filepath.Join(testDir, ConfigFileName)
	if err := os.WriteFile(dstPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// ACT: Load from directory
	cfg, err := LoadFromDirectory(testDir)

	// ASSERT: Config loaded successfully
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if cfg.Project.Owner != "rubrical-works" {
		t.Errorf("Expected owner 'rubrical-works', got '%s'", cfg.Project.Owner)
	}
}

func TestLoadFromDirectory_NoConfigFile_ReturnsError(t *testing.T) {
	// ARRANGE: Empty directory
	testDir := t.TempDir()

	// ACT: Try to load from directory with no config
	_, err := LoadFromDirectory(testDir)

	// ASSERT: Error is returned
	if err == nil {
		t.Fatal("Expected error for missing config file, got nil")
	}
}

func TestApplyEnvOverrides_OverridesOwner(t *testing.T) {
	// ARRANGE: Config and env var
	cfg := &Config{
		Project: Project{
			Owner:  "original-owner",
			Number: 13,
		},
	}
	t.Setenv("GH_PM_PROJECT_OWNER", "env-owner")

	// ACT: Apply overrides
	cfg.ApplyEnvOverrides()

	// ASSERT: Owner is overridden
	if cfg.Project.Owner != "env-owner" {
		t.Errorf("Expected owner 'env-owner', got '%s'", cfg.Project.Owner)
	}
}

func TestApplyEnvOverrides_OverridesNumber(t *testing.T) {
	// ARRANGE: Config and env var
	cfg := &Config{
		Project: Project{
			Owner:  "rubrical-works",
			Number: 13,
		},
	}
	t.Setenv("GH_PM_PROJECT_NUMBER", "99")

	// ACT: Apply overrides
	cfg.ApplyEnvOverrides()

	// ASSERT: Number is overridden
	if cfg.Project.Number != 99 {
		t.Errorf("Expected project number 99, got %d", cfg.Project.Number)
	}
}

func TestApplyEnvOverrides_InvalidNumber_Ignored(t *testing.T) {
	// ARRANGE: Config and invalid env var
	cfg := &Config{
		Project: Project{
			Owner:  "rubrical-works",
			Number: 13,
		},
	}
	t.Setenv("GH_PM_PROJECT_NUMBER", "not-a-number")

	// ACT: Apply overrides
	cfg.ApplyEnvOverrides()

	// ASSERT: Number unchanged
	if cfg.Project.Number != 13 {
		t.Errorf("Expected project number 13 (unchanged), got %d", cfg.Project.Number)
	}
}

func TestApplyEnvOverrides_NoEnvVars_Unchanged(t *testing.T) {
	// ARRANGE: Config with no env vars set
	cfg := &Config{
		Project: Project{
			Owner:  "original-owner",
			Number: 13,
		},
	}
	// Ensure env vars are not set
	os.Unsetenv("GH_PM_PROJECT_OWNER")
	os.Unsetenv("GH_PM_PROJECT_NUMBER")

	// ACT: Apply overrides
	cfg.ApplyEnvOverrides()

	// ASSERT: Values unchanged
	if cfg.Project.Owner != "original-owner" {
		t.Errorf("Expected owner 'original-owner', got '%s'", cfg.Project.Owner)
	}
	if cfg.Project.Number != 13 {
		t.Errorf("Expected project number 13, got %d", cfg.Project.Number)
	}
}

func TestFindConfigFile_InCurrentDir_ReturnsPath(t *testing.T) {
	// ARRANGE: Create temp dir with config file
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(`{"project":{"owner":"test","number":1}}`), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// ACT: Find config starting from same dir
	found, err := FindConfigFile(testDir)

	// ASSERT: Found in current dir
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if found != configPath {
		t.Errorf("Expected %s, got %s", configPath, found)
	}
}

func TestFindConfigFile_InParentDir_ReturnsPath(t *testing.T) {
	// ARRANGE: Create nested dirs, config in parent
	parentDir := t.TempDir()
	childDir := filepath.Join(parentDir, "subdir")
	if err := os.MkdirAll(childDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}
	configPath := filepath.Join(parentDir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(`{"project":{"owner":"test","number":1}}`), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// ACT: Find config starting from child dir
	found, err := FindConfigFile(childDir)

	// ASSERT: Found in parent dir
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if found != configPath {
		t.Errorf("Expected %s, got %s", configPath, found)
	}
}

func TestFindConfigFile_InGrandparentDir_ReturnsPath(t *testing.T) {
	// ARRANGE: Create deeply nested dirs, config in grandparent
	grandparentDir := t.TempDir()
	parentDir := filepath.Join(grandparentDir, "parent")
	childDir := filepath.Join(parentDir, "child")
	if err := os.MkdirAll(childDir, 0755); err != nil {
		t.Fatalf("Failed to create nested dirs: %v", err)
	}
	configPath := filepath.Join(grandparentDir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(`{"project":{"owner":"test","number":1}}`), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// ACT: Find config starting from grandchild dir
	found, err := FindConfigFile(childDir)

	// ASSERT: Found in grandparent dir
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if found != configPath {
		t.Errorf("Expected %s, got %s", configPath, found)
	}
}

func TestFindConfigFile_NotFound_ReturnsError(t *testing.T) {
	// ARRANGE: Empty temp dir (no config anywhere in tree)
	testDir := t.TempDir()
	childDir := filepath.Join(testDir, "subdir")
	if err := os.MkdirAll(childDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	// ACT: Try to find config
	_, err := FindConfigFile(childDir)

	// ASSERT: Error returned
	if err == nil {
		t.Fatal("Expected error when no config file exists, got nil")
	}
}

func TestLoadFromDirectory_FromSubdir_FindsParentConfig(t *testing.T) {
	// ARRANGE: Create nested structure with config in parent
	parentDir := t.TempDir()
	childDir := filepath.Join(parentDir, "subdir", "nested")
	if err := os.MkdirAll(childDir, 0755); err != nil {
		t.Fatalf("Failed to create nested dirs: %v", err)
	}

	configContent := `{"project":{"owner":"rubrical-works","number":13},"repositories":["rubrical-works/gh-pmu"]}`
	configPath := filepath.Join(parentDir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// ACT: Load from nested child dir
	cfg, err := LoadFromDirectory(childDir)

	// ASSERT: Config loaded from parent
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if cfg.Project.Owner != "rubrical-works" {
		t.Errorf("Expected owner 'rubrical-works', got '%s'", cfg.Project.Owner)
	}
	if cfg.Project.Number != 13 {
		t.Errorf("Expected number 13, got %d", cfg.Project.Number)
	}
}

// ============================================================================
// Save Tests
// ============================================================================

func TestConfig_Save_Success(t *testing.T) {
	// ARRANGE: Create temp dir and config
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)

	cfg := &Config{
		Project: Project{
			Owner:  "test-owner",
			Number: 42,
		},
		Repositories: []string{"test-owner/test-repo"},
	}

	// ACT: Save config
	err := cfg.Save(filepath.Dir(configPath))

	// ASSERT: File saved correctly
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify file was created and can be loaded
	loadedCfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load saved config: %v", err)
	}
	if loadedCfg.Project.Owner != "test-owner" {
		t.Errorf("Expected owner 'test-owner', got '%s'", loadedCfg.Project.Owner)
	}
	if loadedCfg.Project.Number != 42 {
		t.Errorf("Expected number 42, got %d", loadedCfg.Project.Number)
	}
}

func TestConfig_Save_WithMetadata(t *testing.T) {
	// ARRANGE: Config with metadata
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)

	cfg := &Config{
		Project: Project{
			Owner:  "test-owner",
			Number: 1,
		},
		Repositories: []string{"test-owner/test-repo"},
		Metadata: &Metadata{
			Project: ProjectMetadata{ID: "PVT_test123"},
			Fields: []FieldMetadata{
				{Name: "Status", ID: "F1", DataType: "SINGLE_SELECT"},
				{Name: "PRD", ID: "F2", DataType: "TEXT"},
			},
		},
	}

	// ACT: Save config
	err := cfg.Save(filepath.Dir(configPath))

	// ASSERT: Metadata preserved
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	loadedCfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load saved config: %v", err)
	}
	if loadedCfg.Metadata == nil {
		t.Fatal("Expected metadata to be preserved")
	}
	if loadedCfg.Metadata.Project.ID != "PVT_test123" {
		t.Errorf("Expected project ID 'PVT_test123', got '%s'", loadedCfg.Metadata.Project.ID)
	}
	if len(loadedCfg.Metadata.Fields) != 2 {
		t.Errorf("Expected 2 fields in metadata, got %d", len(loadedCfg.Metadata.Fields))
	}
}

func TestConfig_Save_WithVersion_RoundTrip(t *testing.T) {
	// ARRANGE: Config with version field
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)

	cfg := &Config{
		Version: "0.16.0",
		Project: Project{
			Owner:  "test-owner",
			Number: 1,
		},
		Repositories: []string{"test-owner/test-repo"},
	}

	// ACT: Save and reload
	err := cfg.Save(filepath.Dir(configPath))
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	loadedCfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load saved config: %v", err)
	}

	// ASSERT: Version preserved through round-trip
	if loadedCfg.Version != "0.16.0" {
		t.Errorf("Expected version '0.16.0', got '%s'", loadedCfg.Version)
	}
}

func TestConfig_Save_WithoutView_OmitsKey(t *testing.T) {
	// ARRANGE: config that never had a view resolved
	testDir := t.TempDir()

	cfg := &Config{
		Project:      Project{Owner: "test-owner", Number: 1},
		Repositories: []string{"test-owner/test-repo"},
	}

	// ACT
	if err := cfg.Save(testDir); err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// ASSERT: omitempty keeps the key out entirely — existing configs stay byte-identical
	raw, err := os.ReadFile(filepath.Join(testDir, ConfigFileName))
	if err != nil {
		t.Fatalf("Failed to read saved config: %v", err)
	}
	if strings.Contains(string(raw), "\"view\"") {
		t.Errorf("Expected no 'view' key when View is unset, got: %s", raw)
	}
}

func TestConfig_Save_WithView_RoundTrip(t *testing.T) {
	// ARRANGE: view resolved to a number that is deliberately not 1
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)

	cfg := &Config{
		Project:      Project{Owner: "test-owner", Number: 1, View: 2},
		Repositories: []string{"test-owner/test-repo"},
	}

	// ACT
	if err := cfg.Save(testDir); err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	loadedCfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load saved config: %v", err)
	}

	// ASSERT
	if loadedCfg.Project.View != 2 {
		t.Errorf("Expected project.view 2 through round-trip, got %d", loadedCfg.Project.View)
	}
}

func TestConfig_Load_WithoutView_LeavesViewUnset(t *testing.T) {
	// ARRANGE: an existing config with no view key
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)
	configContent := `{"project":{"owner":"test-owner","number":1},"repositories":["test-owner/test-repo"]}`
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// ACT
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// ASSERT: absent means unresolved, which is the zero value
	if cfg.Project.View != 0 {
		t.Errorf("Expected View 0 when key absent, got %d", cfg.Project.View)
	}
}

func TestConfig_HasResolvedView(t *testing.T) {
	// A view number is a GitHub creation ordinal and is always >= 1, so anything
	// at or below zero is "not resolved" rather than a usable value. Guarding here
	// keeps a bogus {projectUrl}/views/0 URL from ever being built (#901).
	tests := []struct {
		name string
		view int
		want bool
	}{
		{name: "unset", view: 0, want: false},
		{name: "negative", view: -1, want: false},
		{name: "first view", view: 1, want: true},
		{name: "org board starting at 2", view: 2, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Project: Project{Owner: "o", Number: 1, View: tt.view}}
			if got := cfg.HasResolvedView(); got != tt.want {
				t.Errorf("HasResolvedView() with view %d = %v, want %v", tt.view, got, tt.want)
			}
		})
	}
}

func TestConfig_Load_NonIntegerView_ReturnsError(t *testing.T) {
	// ARRANGE: a hand-edited config where view is a string
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)
	configContent := `{"project":{"owner":"o","number":1,"view":"two"},"repositories":["o/r"]}`
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// ACT
	_, err := Load(configPath)

	// ASSERT: a malformed view is reported, not silently coerced to 0
	if err == nil {
		t.Fatal("Expected an error for a non-integer view, got nil")
	}
}

func TestConfig_Load_WithoutVersion_BackwardCompatible(t *testing.T) {
	// ARRANGE: Config JSON without version field (existing configs)
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)
	configContent := `{"project":{"owner":"test-owner","number":1},"repositories":["test-owner/test-repo"]}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// ACT: Load config without version field
	cfg, err := Load(configPath)

	// ASSERT: Loads without error, version is empty string
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if cfg.Version != "" {
		t.Errorf("Expected empty version for config without version field, got '%s'", cfg.Version)
	}
}

func TestConfig_Load_WithVersion_ReadsCorrectly(t *testing.T) {
	// ARRANGE: Config JSON with version field
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)
	configContent := `{"version":"1.0.0","project":{"owner":"test-owner","number":1},"repositories":["test-owner/test-repo"]}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// ACT: Load config with version field
	cfg, err := Load(configPath)

	// ASSERT: Version read correctly
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if cfg.Version != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got '%s'", cfg.Version)
	}
}

func TestConfig_Save_InvalidPath(t *testing.T) {
	// ARRANGE: Config with invalid target directory
	cfg := &Config{
		Project: Project{Owner: "test", Number: 1},
	}

	// ACT: Try to save into a directory that does not exist
	err := cfg.Save("/nonexistent/directory")

	// ASSERT: Error returned
	if err == nil {
		t.Fatal("Expected error for invalid path, got nil")
	}
}

// ============================================================================
// AddFieldMetadata Tests
// ============================================================================

func TestConfig_AddFieldMetadata_NewField(t *testing.T) {
	// ARRANGE: Config without metadata
	cfg := &Config{
		Project: Project{Owner: "test", Number: 1},
	}

	field := FieldMetadata{
		Name:     "PRD",
		ID:       "PVTF_test",
		DataType: "TEXT",
	}

	// ACT: Add field metadata
	cfg.AddFieldMetadata(field)

	// ASSERT: Metadata created and field added
	if cfg.Metadata == nil {
		t.Fatal("Expected metadata to be created")
	}
	if len(cfg.Metadata.Fields) != 1 {
		t.Fatalf("Expected 1 field, got %d", len(cfg.Metadata.Fields))
	}
	if cfg.Metadata.Fields[0].Name != "PRD" {
		t.Errorf("Expected field name 'PRD', got '%s'", cfg.Metadata.Fields[0].Name)
	}
}

func TestConfig_AddFieldMetadata_UpdateExisting(t *testing.T) {
	// ARRANGE: Config with existing field
	cfg := &Config{
		Project: Project{Owner: "test", Number: 1},
		Metadata: &Metadata{
			Fields: []FieldMetadata{
				{Name: "PRD", ID: "old-id", DataType: "TEXT"},
			},
		},
	}

	updatedField := FieldMetadata{
		Name:     "PRD",
		ID:       "new-id",
		DataType: "TEXT",
	}

	// ACT: Add same field with different ID
	cfg.AddFieldMetadata(updatedField)

	// ASSERT: Field updated, not duplicated
	if len(cfg.Metadata.Fields) != 1 {
		t.Fatalf("Expected 1 field (no duplicates), got %d", len(cfg.Metadata.Fields))
	}
	if cfg.Metadata.Fields[0].ID != "new-id" {
		t.Errorf("Expected field ID 'new-id', got '%s'", cfg.Metadata.Fields[0].ID)
	}
}

func TestConfig_AddFieldMetadata_MultipleFields(t *testing.T) {
	// ARRANGE: Empty config
	cfg := &Config{
		Project: Project{Owner: "test", Number: 1},
	}

	// ACT: Add multiple fields
	cfg.AddFieldMetadata(FieldMetadata{Name: "Field1", ID: "F1", DataType: "TEXT"})
	cfg.AddFieldMetadata(FieldMetadata{Name: "Field2", ID: "F2", DataType: "NUMBER"})
	cfg.AddFieldMetadata(FieldMetadata{Name: "Field3", ID: "F3", DataType: "SINGLE_SELECT"})

	// ASSERT: All fields added
	if len(cfg.Metadata.Fields) != 3 {
		t.Fatalf("Expected 3 fields, got %d", len(cfg.Metadata.Fields))
	}
}

func TestConfig_AddFieldMetadata_WithOptions(t *testing.T) {
	// ARRANGE: Empty config
	cfg := &Config{
		Project: Project{Owner: "test", Number: 1},
	}

	field := FieldMetadata{
		Name:     "Environment",
		ID:       "PVTSSF_test",
		DataType: "SINGLE_SELECT",
		Options: []OptionMetadata{
			{Name: "Development", ID: "opt1"},
			{Name: "Production", ID: "opt2"},
		},
	}

	// ACT: Add field with options
	cfg.AddFieldMetadata(field)

	// ASSERT: Options preserved
	if len(cfg.Metadata.Fields[0].Options) != 2 {
		t.Fatalf("Expected 2 options, got %d", len(cfg.Metadata.Fields[0].Options))
	}
	if cfg.Metadata.Fields[0].Options[0].Name != "Development" {
		t.Errorf("Expected first option 'Development', got '%s'", cfg.Metadata.Fields[0].Options[0].Name)
	}
}

// ============================================================================
// IsIDPF Tests
// ============================================================================

func TestConfig_IsIDPF_WithIDPF(t *testing.T) {
	cfg := &Config{Framework: "IDPF"}
	if !cfg.IsIDPF() {
		t.Error("Expected IsIDPF() to return true for 'IDPF'")
	}
}

func TestConfig_IsIDPF_WithLowercase(t *testing.T) {
	cfg := &Config{Framework: "idpf"}
	if !cfg.IsIDPF() {
		t.Error("Expected IsIDPF() to return true for 'idpf'")
	}
}

func TestConfig_IsIDPF_WithNone(t *testing.T) {
	cfg := &Config{Framework: "none"}
	if cfg.IsIDPF() {
		t.Error("Expected IsIDPF() to return false for 'none'")
	}
}

func TestConfig_IsIDPF_WithEmpty(t *testing.T) {
	cfg := &Config{Framework: ""}
	if cfg.IsIDPF() {
		t.Error("Expected IsIDPF() to return false for empty string")
	}
}

func TestConfig_IsIDPF_WithIDPFAgile(t *testing.T) {
	cfg := &Config{Framework: "IDPF-Agile"}
	if !cfg.IsIDPF() {
		t.Error("Expected IsIDPF() to return true for 'IDPF-Agile'")
	}
}

func TestConfig_IsIDPF_WithIDPFAgileLowercase(t *testing.T) {
	cfg := &Config{Framework: "idpf-agile"}
	if !cfg.IsIDPF() {
		t.Error("Expected IsIDPF() to return true for 'idpf-agile'")
	}
}

func TestConfig_IsIDPF_WithMixedCase(t *testing.T) {
	cfg := &Config{Framework: "Idpf"}
	if !cfg.IsIDPF() {
		t.Error("Expected IsIDPF() to return true for 'Idpf'")
	}
}

// ============================================================================
// LoadFromDirectoryAndNormalize Tests
// ============================================================================

func TestLoadFromDirectoryAndNormalize_NormalizesEmptyFramework(t *testing.T) {
	// ARRANGE: Create temp dir with config without framework
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)
	configContent := `{"project":{"owner":"test-owner","number":1},"repositories":["test-owner/test-repo"]}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// ACT: Load and normalize
	cfg, err := LoadFromDirectoryAndNormalize(testDir)

	// ASSERT: Framework is set to IDPF
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if cfg.Framework != "IDPF" {
		t.Errorf("Expected framework 'IDPF', got '%s'", cfg.Framework)
	}

	// ASSERT: File was updated
	loadedCfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to reload config: %v", err)
	}
	if loadedCfg.Framework != "IDPF" {
		t.Errorf("Expected saved framework 'IDPF', got '%s'", loadedCfg.Framework)
	}
}

func TestLoadFromDirectoryAndNormalize_PreservesExistingFramework(t *testing.T) {
	// ARRANGE: Create temp dir with config with framework: none
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)
	configContent := `{"project":{"owner":"test-owner","number":1},"repositories":["test-owner/test-repo"],"framework":"none"}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// ACT: Load and normalize
	cfg, err := LoadFromDirectoryAndNormalize(testDir)

	// ASSERT: Framework is preserved as 'none'
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if cfg.Framework != "none" {
		t.Errorf("Expected framework 'none', got '%s'", cfg.Framework)
	}
}

// ============================================================================
// Temp File Handling Tests
// ============================================================================

// resolvedPath returns p with symlinks resolved. os.Getwd reports the resolved
// path, so on macOS a t.TempDir() under /var comes back as /private/var; both
// sides of a path comparison are resolved so the check holds on every OS (#930).
func resolvedPath(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("Failed to resolve %s: %v", p, err)
	}
	return resolved
}

func TestGetTempDir_CreatesTmpDirectory(t *testing.T) {
	// ARRANGE: Create temp dir with config file and change to it
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(`{"project":{"owner":"test","number":1}}`), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// Save current dir and change to test dir
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current dir: %v", err)
	}
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test dir: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	// ACT: Get temp dir
	tempDir, err := GetTempDir()

	// ASSERT: Temp dir created
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expectedPath := filepath.Join(testDir, TempDirName)
	if resolvedPath(t, tempDir) != resolvedPath(t, expectedPath) {
		t.Errorf("Expected temp dir '%s', got '%s'", expectedPath, tempDir)
	}

	// Verify directory exists
	info, err := os.Stat(tempDir)
	if err != nil {
		t.Fatalf("Temp directory should exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("Expected temp path to be a directory")
	}
}

func TestGetTempDir_AddsToGitignore(t *testing.T) {
	// ARRANGE: Create temp dir with config file
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(`{"project":{"owner":"test","number":1}}`), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// Save current dir and change to test dir
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current dir: %v", err)
	}
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test dir: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	// ACT: Get temp dir (should create .gitignore entry)
	_, err = GetTempDir()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// ASSERT: .gitignore contains tmp/
	gitignorePath := filepath.Join(testDir, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("Expected .gitignore to be created: %v", err)
	}

	content := string(data)
	if content != "tmp/\n" {
		t.Errorf("Expected .gitignore to contain 'tmp/', got '%s'", content)
	}
}

func TestGetTempDir_DoesNotDuplicateGitignoreEntry(t *testing.T) {
	// ARRANGE: Create temp dir with config file and existing .gitignore
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(`{"project":{"owner":"test","number":1}}`), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	gitignorePath := filepath.Join(testDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("tmp/\n"), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	// Save current dir and change to test dir
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current dir: %v", err)
	}
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test dir: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	// ACT: Get temp dir twice
	_, _ = GetTempDir()
	_, _ = GetTempDir()

	// ASSERT: .gitignore still only has one entry
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	content := string(data)
	if content != "tmp/\n" {
		t.Errorf("Expected .gitignore to contain only one 'tmp/' entry, got '%s'", content)
	}
}

func TestGetTempDir_AppendsToExistingGitignore(t *testing.T) {
	// ARRANGE: Create temp dir with config file and existing .gitignore with other entries
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(`{"project":{"owner":"test","number":1}}`), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	gitignorePath := filepath.Join(testDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("node_modules/\n.env\n"), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	// Save current dir and change to test dir
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current dir: %v", err)
	}
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test dir: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	// ACT: Get temp dir
	_, err = GetTempDir()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// ASSERT: .gitignore has original entries plus tmp/
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	content := string(data)
	expected := "node_modules/\n.env\ntmp/\n"
	if content != expected {
		t.Errorf("Expected .gitignore content '%s', got '%s'", expected, content)
	}
}

func TestCreateTempFile_CreatesFileInTmpDir(t *testing.T) {
	// ARRANGE: Create temp dir with config file
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(`{"project":{"owner":"test","number":1}}`), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// Save current dir and change to test dir
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current dir: %v", err)
	}
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test dir: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	// ACT: Create temp file
	file, err := CreateTempFile("test-*.txt")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	defer func() {
		file.Close()
		os.Remove(file.Name())
	}()

	// ASSERT: File is in tmp directory
	expectedDir := filepath.Join(testDir, TempDirName)
	if resolvedPath(t, filepath.Dir(file.Name())) != resolvedPath(t, expectedDir) {
		t.Errorf("Expected file in '%s', got '%s'", expectedDir, filepath.Dir(file.Name()))
	}

	// Verify file exists
	if _, err := os.Stat(file.Name()); err != nil {
		t.Errorf("Temp file should exist: %v", err)
	}
}

func TestCreateTempFile_UsesPattern(t *testing.T) {
	// ARRANGE: Create temp dir with config file
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(`{"project":{"owner":"test","number":1}}`), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// Save current dir and change to test dir
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current dir: %v", err)
	}
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test dir: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	// ACT: Create temp file with pattern
	file, err := CreateTempFile("gh-pmu-issue-*.md")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	defer func() {
		file.Close()
		os.Remove(file.Name())
	}()

	// ASSERT: File name matches pattern
	filename := filepath.Base(file.Name())
	if len(filename) < len("gh-pmu-issue-.md") {
		t.Errorf("Filename should be longer than pattern base, got '%s'", filename)
	}
	if filename[:13] != "gh-pmu-issue-" {
		t.Errorf("Filename should start with 'gh-pmu-issue-', got '%s'", filename)
	}
	if filename[len(filename)-3:] != ".md" {
		t.Errorf("Filename should end with '.md', got '%s'", filename)
	}
}

func TestGetTempDir_NoConfigFile_ReturnsError(t *testing.T) {
	// ARRANGE: Empty temp dir with no config file
	testDir := t.TempDir()

	// Save current dir and change to test dir
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current dir: %v", err)
	}
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test dir: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	// ACT: Try to get temp dir
	_, err = GetTempDir()

	// ASSERT: Error returned
	if err == nil {
		t.Fatal("Expected error when no config file exists, got nil")
	}
}

func TestCreateTempFile_NoConfigFile_ReturnsError(t *testing.T) {
	// ARRANGE: Empty temp dir with no config file
	testDir := t.TempDir()

	// Save current dir and change to test dir
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current dir: %v", err)
	}
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test dir: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	// ACT: Try to create temp file
	_, err = CreateTempFile("test-*.txt")

	// ASSERT: Error returned
	if err == nil {
		t.Fatal("Expected error when no config file exists, got nil")
	}
}

func TestEnsureGitignore_HandlesNoTrailingNewline(t *testing.T) {
	// ARRANGE: Create temp dir with .gitignore without trailing newline
	testDir := t.TempDir()
	gitignorePath := filepath.Join(testDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("node_modules/"), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	// ACT: Call ensureGitignore
	err := ensureGitignore(testDir)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// ASSERT: .gitignore has newline before tmp/
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	content := string(data)
	expected := "node_modules/\ntmp/\n"
	if content != expected {
		t.Errorf("Expected .gitignore content '%s', got '%s'", expected, content)
	}
}

func TestEnsureGitignore_RecognizesTmpWithoutSlash(t *testing.T) {
	// ARRANGE: Create temp dir with .gitignore containing "tmp" without slash
	testDir := t.TempDir()
	gitignorePath := filepath.Join(testDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("tmp\n"), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	// ACT: Call ensureGitignore
	err := ensureGitignore(testDir)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// ASSERT: .gitignore is not modified (tmp already present)
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	content := string(data)
	if content != "tmp\n" {
		t.Errorf("Expected .gitignore to remain unchanged, got '%s'", content)
	}
}

// ============================================================================
// Config File Protection Test
// ============================================================================

// TestRealConfigFileNotCorrupted verifies that the real .gh-pmu.json file
// at the project root has not been corrupted by tests writing test data to it.
// This test acts as a canary to detect when test isolation fails.
func TestRealConfigFileNotCorrupted(t *testing.T) {
	// Find the real config file at project root
	cwd, err := os.Getwd()
	if err != nil {
		t.Skipf("Could not get current directory: %v", err)
	}

	// Walk up to find project root (where .gh-pmu.json should be)
	configPath, err := FindConfigFile(cwd)
	if err != nil {
		t.Skipf("No .gh-pmu.json found in path: %v", err)
	}

	// Read the config file
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Skipf("Could not read config file: %v", err)
	}

	// Verify it contains the real project owner, not test data
	if strings.Contains(string(content), "testowner") {
		t.Error("Real .gh-pmu.json contains 'testowner' - tests have corrupted the config file! " +
			"Tests that call cfg.Save() must use setupBranchTestDir for isolation.")
	}

	// Verify it contains expected owner (org renamed rubrical-works -> rubrical-worker)
	if !strings.Contains(string(content), "rubrical-worker") {
		t.Error("Real config does not contain 'rubrical-worker' - the config may be corrupted")
	}
}

func TestSave_WritesJSONOnly(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, ConfigFileName)

	cfg := &Config{
		Project: Project{
			Owner:  "test-owner",
			Number: 1,
		},
		Repositories: []string{"test-owner/test-repo"},
	}

	// ACT: Save config
	err := cfg.Save(filepath.Dir(jsonPath))
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// ASSERT: JSON file exists
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Error("Expected .gh-pmu.json to exist")
	}

	// ASSERT: YAML companion NOT created
	yamlPath := filepath.Join(tmpDir, ".gh-pmu.yml")
	if _, err := os.Stat(yamlPath); !os.IsNotExist(err) {
		t.Error("Expected .gh-pmu.yml to NOT be created by Save()")
	}
}

func TestSave_OmitsReleaseKey(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &Config{
		Project: Project{
			Owner:  "test-owner",
			Number: 1,
		},
		Repositories: []string{"test-owner/test-repo"},
	}

	// ACT: Save a config that sets no release configuration
	if err := cfg.Save(tmpDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	jsonData, err := os.ReadFile(filepath.Join(tmpDir, ConfigFileName))
	if err != nil {
		t.Fatalf("Failed to read JSON file: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(jsonData, &raw); err != nil {
		t.Fatalf("Saved config is not valid JSON: %v", err)
	}

	// ASSERT: no release key at all. A non-pointer struct tagged omitempty is
	// not omitted by encoding/json, so a dead release block would surface here
	// as "release": {} in every config the tool writes.
	if _, ok := raw["release"]; ok {
		t.Errorf("Saved config must not contain a release key, got: %s", string(jsonData))
	}
}

func TestSave_JSONContainsExpectedData(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, ConfigFileName)

	cfg := &Config{
		Project: Project{
			Owner:  "test-owner",
			Number: 42,
		},
		Repositories: []string{"test-owner/test-repo"},
		Framework:    "IDPF-Agile",
	}

	// ACT: Save config
	if err := cfg.Save(filepath.Dir(jsonPath)); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// ASSERT: JSON contains expected fields
	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Failed to read JSON file: %v", err)
	}

	jsonStr := string(jsonData)
	if !strings.Contains(jsonStr, "test-owner") {
		t.Error("JSON file should contain project owner")
	}
	if !strings.Contains(jsonStr, "42") {
		t.Error("JSON file should contain project number")
	}
	if !strings.Contains(jsonStr, "IDPF-Agile") {
		t.Error("JSON file should contain framework")
	}
}

func TestFindConfigFile_JSONTakesPrecedence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create both files
	yamlPath := filepath.Join(tmpDir, ".gh-pmu.yml")
	jsonPath := filepath.Join(tmpDir, ".gh-pmu.json")
	if err := os.WriteFile(yamlPath, []byte("project:\n  owner: yaml-owner\n  number: 1\n"), 0644); err != nil {
		t.Fatalf("Failed to write YAML: %v", err)
	}
	if err := os.WriteFile(jsonPath, []byte(`{"project":{"owner":"json-owner","number":1}}`), 0644); err != nil {
		t.Fatalf("Failed to write JSON: %v", err)
	}

	// ACT: FindConfigFile should find JSON (primary)
	found, err := FindConfigFile(tmpDir)
	if err != nil {
		t.Fatalf("FindConfigFile failed: %v", err)
	}

	if !strings.HasSuffix(found, ".gh-pmu.json") {
		t.Errorf("Expected JSON to take precedence, got: %s", found)
	}
}

// ============================================================================
// ensureGitignore Tests
// ============================================================================

func TestEnsureGitignore_CreatesNewFile(t *testing.T) {
	tmpDir := t.TempDir()

	err := ensureGitignore(tmpDir)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	if !strings.Contains(string(content), TempDirName+"/") {
		t.Errorf("Expected .gitignore to contain '%s/', got: %s", TempDirName, string(content))
	}
}

func TestEnsureGitignore_EntryAlreadyPresent_NoOp(t *testing.T) {
	tmpDir := t.TempDir()
	gitignorePath := filepath.Join(tmpDir, ".gitignore")

	// Pre-create .gitignore with the entry
	original := "node_modules/\n" + TempDirName + "/\n"
	if err := os.WriteFile(gitignorePath, []byte(original), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	err := ensureGitignore(tmpDir)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	// Should be unchanged
	if string(content) != original {
		t.Errorf("Expected .gitignore unchanged, got: %s", string(content))
	}
}

func TestEnsureGitignore_AppendsToExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	gitignorePath := filepath.Join(tmpDir, ".gitignore")

	// Pre-create .gitignore without the entry
	if err := os.WriteFile(gitignorePath, []byte("node_modules/\n"), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	err := ensureGitignore(tmpDir)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	if !strings.Contains(string(content), TempDirName+"/") {
		t.Errorf("Expected .gitignore to contain '%s/', got: %s", TempDirName, string(content))
	}
	// Should still have the original content
	if !strings.Contains(string(content), "node_modules/") {
		t.Errorf("Expected .gitignore to still contain 'node_modules/', got: %s", string(content))
	}
}

func TestRefreshVersion_StaleVersion_RewritesToCurrent(t *testing.T) {
	// ARRANGE: config stamped by an older binary
	testDir := t.TempDir()
	jsonPath := filepath.Join(testDir, ConfigFileName)
	jsonContent := `{"version":"1.1.0","project":{"owner":"test-owner","number":1},"repositories":["test-owner/test-repo"]}`
	if err := os.WriteFile(jsonPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("Failed to write JSON config: %v", err)
	}

	// ACT
	wrote, err := RefreshVersion(testDir, "1.5.3")

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !wrote {
		t.Error("Expected RefreshVersion to report a write for a stale version")
	}

	cfg, err := Load(jsonPath)
	if err != nil {
		t.Fatalf("Failed to reload config: %v", err)
	}
	if cfg.Version != "1.5.3" {
		t.Errorf("Expected version to be rewritten to '1.5.3', got '%s'", cfg.Version)
	}
}

func TestRefreshVersion_CurrentVersion_NoWriteBytesUnchanged(t *testing.T) {
	// ARRANGE: config already stamped at the running version
	testDir := t.TempDir()
	jsonPath := filepath.Join(testDir, ConfigFileName)
	jsonContent := `{"version":"1.5.3","project":{"owner":"test-owner","number":1},"repositories":["test-owner/test-repo"]}`
	if err := os.WriteFile(jsonPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("Failed to write JSON config: %v", err)
	}
	before, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	// ACT
	wrote, err := RefreshVersion(testDir, "1.5.3")

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if wrote {
		t.Error("Expected RefreshVersion to report no write when the version already matches")
	}

	// Byte comparison, not mtime: filesystem timestamp granularity makes mtime flaky.
	after, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Failed to re-read config: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("Expected config bytes to be unchanged.\nBefore: %s\nAfter:  %s", before, after)
	}
}

func TestRefreshVersion_PreservesOtherFields(t *testing.T) {
	// ARRANGE: a stale config carrying fields the refresh must not disturb
	testDir := t.TempDir()
	jsonPath := filepath.Join(testDir, ConfigFileName)
	jsonContent := `{"version":"1.1.0","project":{"owner":"test-owner","number":7},"repositories":["test-owner/test-repo"],"framework":"IDPF-Agile","acceptance":{"accepted":true,"user":"tester","date":"2026-01-01","version":"1.1.0"}}`
	if err := os.WriteFile(jsonPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("Failed to write JSON config: %v", err)
	}

	// ACT
	if _, err := RefreshVersion(testDir, "1.5.3"); err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// ASSERT: only the top-level version moved
	cfg, err := Load(jsonPath)
	if err != nil {
		t.Fatalf("Failed to reload config: %v", err)
	}
	if cfg.Project.Number != 7 || cfg.Project.Owner != "test-owner" {
		t.Errorf("Expected project fields preserved, got owner=%q number=%d", cfg.Project.Owner, cfg.Project.Number)
	}
	if cfg.Framework != "IDPF-Agile" {
		t.Errorf("Expected framework preserved, got %q", cfg.Framework)
	}
	if cfg.Acceptance == nil || cfg.Acceptance.Version != "1.1.0" {
		t.Error("Expected acceptance.version to be left untouched by the top-level version refresh")
	}
}

func TestRefreshVersion_NoConfig_ReturnsErrorWithoutWriting(t *testing.T) {
	// ARRANGE: an uninitialized directory
	testDir := t.TempDir()

	// ACT
	wrote, err := RefreshVersion(testDir, "1.5.3")

	// ASSERT
	if err == nil {
		t.Error("Expected an error when no config file exists")
	}
	if wrote {
		t.Error("Expected no write to be reported when no config file exists")
	}
	if _, statErr := os.Stat(filepath.Join(testDir, ConfigFileName)); !os.IsNotExist(statErr) {
		t.Error("Expected RefreshVersion not to create a config file")
	}
}

// assertTrailingNewline fails unless data ends with exactly one 0x0a.
//
// The assertion is on raw bytes on purpose. Every existing config test reaches
// the file through Load or json.Unmarshal, and a JSON round-trip cannot observe
// a trailing newline at all — the byte is outside the document. That blind spot
// is why the newline could go missing without a single test turning red.
func assertTrailingNewline(t *testing.T, data []byte, what string) {
	t.Helper()
	if len(data) == 0 {
		t.Fatalf("%s: file is empty, expected a trailing newline", what)
	}
	if data[len(data)-1] != '\n' {
		t.Errorf("%s: expected final byte 0x0a, got %#x (tail: %q)", what, data[len(data)-1], tailOf(data))
		// The "exactly one" check below reads the byte before the last one. With
		// no trailing newline at all that byte is the newline MarshalIndent puts
		// before the closing brace, which would report a spurious second failure.
		return
	}
	if len(data) >= 2 && data[len(data)-2] == '\n' {
		t.Errorf("%s: expected exactly one trailing newline, found more than one (tail: %q)", what, tailOf(data))
	}
}

// tailOf returns the last few bytes of data for use in failure messages.
func tailOf(data []byte) string {
	const n = 8
	if len(data) <= n {
		return string(data)
	}
	return string(data[len(data)-n:])
}

func TestSave_WritesExactlyOneTrailingNewline(t *testing.T) {
	// ARRANGE
	testDir := t.TempDir()
	cfg := &Config{
		Version:      "1.5.3",
		Project:      Project{Owner: "test-owner", Number: 1},
		Repositories: []string{"test-owner/test-repo"},
	}

	// ACT
	if err := cfg.Save(testDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// ASSERT
	data, err := os.ReadFile(filepath.Join(testDir, ConfigFileName))
	if err != nil {
		t.Fatalf("Failed to read saved config: %v", err)
	}
	assertTrailingNewline(t, data, "Config.Save")
}

func TestRefreshVersion_WritesExactlyOneTrailingNewline(t *testing.T) {
	// ARRANGE: a stale config written without a trailing newline, so a passing
	// result proves RefreshVersion emitted one rather than preserving one.
	testDir := t.TempDir()
	jsonPath := filepath.Join(testDir, ConfigFileName)
	jsonContent := `{"version":"1.1.0","project":{"owner":"test-owner","number":1},"repositories":["test-owner/test-repo"]}`
	if err := os.WriteFile(jsonPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("Failed to write JSON config: %v", err)
	}

	// ACT
	wrote, err := RefreshVersion(testDir, "1.5.3")

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !wrote {
		t.Fatal("Expected RefreshVersion to report a write for a stale version")
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Failed to read refreshed config: %v", err)
	}
	assertTrailingNewline(t, data, "RefreshVersion")
}

func TestRefreshVersion_ChangesOnlyTheVersionValueBytes(t *testing.T) {
	// ARRANGE: seed through Save so the file is already in canonical form.
	// Seeding with hand-written JSON would make the first refresh reformat the
	// whole document, and the test would pass or fail on formatting rather than
	// on what the refresh changed.
	testDir := t.TempDir()
	jsonPath := filepath.Join(testDir, ConfigFileName)
	cfg := &Config{
		Version:      "1.1.0",
		Project:      Project{Name: "gh-pmu", Owner: "test-owner", Number: 11, View: 2},
		Repositories: []string{"test-owner/test-repo", "test-owner/other-repo"},
		Framework:    "IDPF-Agile",
		Defaults:     Defaults{Priority: "P2", Status: "Backlog", Labels: []string{"needs-triage"}},
		Fields:       map[string]Field{"status": {Field: "Status", Values: map[string]string{"wip": "In Progress"}}},
	}
	if err := cfg.Save(testDir); err != nil {
		t.Fatalf("Failed to seed config: %v", err)
	}
	before, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Failed to read seeded config: %v", err)
	}

	// ACT
	wrote, err := RefreshVersion(testDir, "1.5.3")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !wrote {
		t.Fatal("Expected RefreshVersion to report a write for a stale version")
	}

	// ASSERT: the only permitted difference is the version value itself.
	// Comparing raw bytes catches indentation drift, key reordering and a lost
	// trailing newline in one assertion — none of which a JSON round-trip or a
	// field-by-field struct comparison can see.
	after, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Failed to read refreshed config: %v", err)
	}
	want := bytes.Replace(before, []byte(`"version": "1.1.0"`), []byte(`"version": "1.5.3"`), 1)
	if bytes.Equal(want, before) {
		t.Fatal("Test setup error: expected version string not found in the seeded config")
	}
	if !bytes.Equal(want, after) {
		t.Errorf("RefreshVersion changed more than the version value.\n--- want ---\n%s\n--- got ---\n%s", want, after)
	}

	// Keys the Config struct does not model are deliberately outside this
	// comparison: Save drops them, which is tracked separately as #910.
}

// configWriteSite is one os.WriteFile call that targets ConfigFileName.
type configWriteSite struct {
	file string
	line int
}

// findConfigWriteSites walks the module for non-test Go files containing an
// os.WriteFile call whose destination is built from ConfigFileName.
//
// The destination is identified by looking a few lines above the call for a
// ConfigFileName reference, which is how all known writers are shaped:
//
//	jsonPath := filepath.Join(dir, ConfigFileName)
//	if err := os.WriteFile(jsonPath, jsonData, 0600); err != nil {
func findConfigWriteSites(t *testing.T, root string) []configWriteSite {
	t.Helper()
	const lookback = 5
	var sites []configWriteSite

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Vendored and fixture trees are not ours to police.
			switch info.Name() {
			case ".git", "vendor", "testdata", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if !strings.Contains(line, "os.WriteFile(") {
				continue
			}
			start := i - lookback
			if start < 0 {
				start = 0
			}
			if !strings.Contains(strings.Join(lines[start:i+1], "\n"), "ConfigFileName") {
				continue
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			sites = append(sites, configWriteSite{file: filepath.ToSlash(rel), line: i + 1})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk %s: %v", root, err)
	}
	return sites
}

func TestNoWriterBypassesTheNewlineEmittingPath(t *testing.T) {
	// The byte assertions above cannot catch this. A fourth writer that
	// marshals and writes .gh-pmu.json inline would leave every one of them
	// green and still strip the trailing newline — which is exactly the defect
	// review caught on this branch, in a different codebase (px-manager#1042).
	//
	// Counting per file rather than pinning line numbers is deliberate: an
	// edit anywhere above a call would shift a pinned line and fail the test
	// for a reason unrelated to what it guards. A new writer changes the count,
	// which is the condition worth failing on.
	want := map[string]int{
		"internal/config/config.go": 1, // Config.Save
		"cmd/init.go":               2, // writeConfig, writeConfigWithMetadata
	}

	sites := findConfigWriteSites(t, filepath.Join("..", ".."))

	got := make(map[string]int, len(want))
	for _, s := range sites {
		got[s.file]++
	}

	for file, n := range got {
		if want[file] == 0 {
			t.Errorf("New writer of %s found at %s — it must route through Config.Save, "+
				"or append '\\n' itself as the existing writers do", ConfigFileName, describeSites(sites, file))
			continue
		}
		if n != want[file] {
			t.Errorf("%s: expected %d writer(s) of %s, found %d at %s",
				file, want[file], ConfigFileName, n, describeSites(sites, file))
		}
	}
	for file, n := range want {
		if got[file] == 0 {
			t.Errorf("%s: expected %d writer(s) of %s, found none — if a writer moved, "+
				"update this guard so it keeps covering it", file, n, ConfigFileName)
		}
	}
}

// describeSites renders the file:line list for one file, for failure messages.
func describeSites(sites []configWriteSite, file string) string {
	var out []string
	for _, s := range sites {
		if s.file == file {
			out = append(out, fmt.Sprintf("%s:%d", s.file, s.line))
		}
	}
	return strings.Join(out, ", ")
}
func TestSave_DoesNotWriteYAMLCompanion(t *testing.T) {
	// ARRANGE: Save a config and verify no YAML companion is created
	testDir := t.TempDir()
	jsonPath := filepath.Join(testDir, ConfigFileName)

	cfg := &Config{
		Version:      "1.4.0",
		Project:      Project{Owner: "test-owner", Number: 1},
		Repositories: []string{"test-owner/test-repo"},
	}

	// ACT: Save config
	err := cfg.Save(filepath.Dir(jsonPath))
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// ASSERT: JSON file exists
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("Expected JSON config to exist: %v", err)
	}

	// ASSERT: YAML file NOT created
	yamlPath := filepath.Join(testDir, ".gh-pmu.yml")
	if _, err := os.Stat(yamlPath); !os.IsNotExist(err) {
		t.Error("Expected .gh-pmu.yml to NOT be created by Save(), but it exists")
	}
}
