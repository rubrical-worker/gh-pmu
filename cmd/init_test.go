package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/rubrical-works/gh-pmu/internal/defaults"
)

func TestInitCommand_Exists(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("init command should exist: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("init")) {
		t.Error("Expected help output to mention 'init'")
	}
}

func TestWriteConfig_CreatesValidJSON(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()

	cfg := &InitConfig{
		ProjectOwner:  "test-owner",
		ProjectNumber: 5,
		Repositories:  []string{"test-owner/test-repo"},
	}

	err := writeConfig(tmpDir, cfg)
	if err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	// Verify file was created
	configPath := tmpDir + "/.gh-pmu.json"
	content, err := readFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	// Check content contains expected values (JSON format)
	if !bytes.Contains(content, []byte(`"owner": "test-owner"`)) {
		t.Error("Config should contain owner")
	}
	if !bytes.Contains(content, []byte(`"number": 5`)) {
		t.Error("Config should contain project number")
	}
	if !bytes.Contains(content, []byte("test-owner/test-repo")) {
		t.Error("Config should contain repository")
	}
}

func TestWriteConfig_WithDefaults(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	err := writeConfig(tmpDir, cfg)
	if err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	content, _ := readFile(tmpDir + "/.gh-pmu.json")

	// Should have default status field mapping
	if !bytes.Contains(content, []byte(`"status"`)) {
		t.Error("Config should have default status field")
	}
}

func TestWriteConfig_IncludesTriageAndLabels(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &InitConfig{
		ProjectName:   "Test Project",
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	err := writeConfig(tmpDir, cfg)
	if err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	content, _ := readFile(tmpDir + "/.gh-pmu.json")

	// Should have project name
	if !bytes.Contains(content, []byte(`"name": "Test Project"`)) {
		t.Error("Config should have project name")
	}

	// Should have triage section
	if !bytes.Contains(content, []byte(`"triage"`)) {
		t.Error("Config should have triage section")
	}

	// Should have estimate triage rule
	if !bytes.Contains(content, []byte(`"estimate"`)) {
		t.Error("Config should have estimate triage rule")
	}
}

// Helper to read file for tests
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func TestValidateProject_Success(t *testing.T) {
	// Mock client that returns a valid project
	mockClient := &MockAPIClient{
		project: &MockProject{
			ID:    "PVT_test123",
			Title: "Test Project",
		},
	}

	err := validateProject(mockClient, "owner", 1)
	if err != nil {
		t.Errorf("validateProject should succeed for valid project: %v", err)
	}
}

func TestValidateProject_NotFound(t *testing.T) {
	// Mock client that returns not found error
	mockClient := &MockAPIClient{
		err: ErrProjectNotFound,
	}

	err := validateProject(mockClient, "owner", 999)
	if err == nil {
		t.Error("validateProject should fail for non-existent project")
	}
}

// MockProject represents a mock project for testing
type MockProject struct {
	ID    string
	Title string
}

// MockAPIClient is a mock implementation for testing
type MockAPIClient struct {
	project *MockProject
	err     error
}

// GetProject implements ProjectValidator interface
func (m *MockAPIClient) GetProject(owner string, number int) (interface{}, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.project, nil
}

// ErrProjectNotFound is returned when project doesn't exist
var ErrProjectNotFound = fmt.Errorf("project not found")

func TestWriteConfigWithMetadata_IncludesFields(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	metadata := &ProjectMetadata{
		ProjectID: "PVT_test123",
		Fields: []FieldMetadata{
			{
				ID:       "PVTF_status",
				Name:     "Status",
				DataType: "SINGLE_SELECT",
				Options: []OptionMetadata{
					{ID: "opt1", Name: "Backlog"},
					{ID: "opt2", Name: "Done"},
				},
			},
			{
				ID:       "PVTF_priority",
				Name:     "Priority",
				DataType: "SINGLE_SELECT",
				Options: []OptionMetadata{
					{ID: "opt3", Name: "High"},
					{ID: "opt4", Name: "Low"},
				},
			},
		},
	}

	err := writeConfigWithMetadata(tmpDir, cfg, metadata)
	if err != nil {
		t.Fatalf("writeConfigWithMetadata failed: %v", err)
	}

	content, _ := readFile(tmpDir + "/.gh-pmu.json")

	// Should contain metadata section with project ID
	if !bytes.Contains(content, []byte(`"metadata"`)) {
		t.Error("Config should have metadata section")
	}
	if !bytes.Contains(content, []byte("PVT_test123")) {
		t.Error("Config should contain project ID")
	}
	// Should contain field IDs
	if !bytes.Contains(content, []byte("PVTF_status")) {
		t.Error("Config should contain field IDs")
	}
}

func TestSplitRepository(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedOwner string
		expectedName  string
	}{
		{
			name:          "valid owner/repo format",
			input:         "rubrical-works/gh-pmu",
			expectedOwner: "rubrical-works",
			expectedName:  "gh-pmu",
		},
		{
			name:          "simple owner/repo",
			input:         "owner/repo",
			expectedOwner: "owner",
			expectedName:  "repo",
		},
		{
			name:          "no slash - invalid input",
			input:         "noslash",
			expectedOwner: "",
			expectedName:  "",
		},
		{
			name:          "empty string",
			input:         "",
			expectedOwner: "",
			expectedName:  "",
		},
		{
			name:          "multiple slashes - takes first split",
			input:         "owner/repo/extra",
			expectedOwner: "owner",
			expectedName:  "repo/extra",
		},
		{
			name:          "only slash",
			input:         "/",
			expectedOwner: "",
			expectedName:  "",
		},
		{
			name:          "owner with trailing slash",
			input:         "owner/",
			expectedOwner: "owner",
			expectedName:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, name := splitRepository(tt.input)
			if owner != tt.expectedOwner {
				t.Errorf("splitRepository(%q) owner = %q, want %q", tt.input, owner, tt.expectedOwner)
			}
			if name != tt.expectedName {
				t.Errorf("splitRepository(%q) name = %q, want %q", tt.input, name, tt.expectedName)
			}
		})
	}
}

func TestWriteConfigWithMetadata_EmptyMetadata(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &InitConfig{
		ProjectName:   "Test",
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	// Empty metadata with no fields
	metadata := &ProjectMetadata{
		ProjectID: "PVT_empty",
		Fields:    []FieldMetadata{},
	}

	err := writeConfigWithMetadata(tmpDir, cfg, metadata)
	if err != nil {
		t.Fatalf("writeConfigWithMetadata failed with empty fields: %v", err)
	}

	content, err := readFile(tmpDir + "/.gh-pmu.json")
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	// Should still have metadata section
	if !bytes.Contains(content, []byte(`"metadata"`)) {
		t.Error("Config should have metadata section even with empty fields")
	}
	if !bytes.Contains(content, []byte("PVT_empty")) {
		t.Error("Config should contain project ID")
	}
}

func TestWriteConfigWithMetadata_FieldOptions(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	metadata := &ProjectMetadata{
		ProjectID: "PVT_test",
		Fields: []FieldMetadata{
			{
				ID:       "PVTF_size",
				Name:     "Size",
				DataType: "SINGLE_SELECT",
				Options: []OptionMetadata{
					{ID: "size_xs", Name: "XS"},
					{ID: "size_s", Name: "S"},
					{ID: "size_m", Name: "M"},
					{ID: "size_l", Name: "L"},
					{ID: "size_xl", Name: "XL"},
				},
			},
		},
	}

	err := writeConfigWithMetadata(tmpDir, cfg, metadata)
	if err != nil {
		t.Fatalf("writeConfigWithMetadata failed: %v", err)
	}

	content, _ := readFile(tmpDir + "/.gh-pmu.json")

	// Check all options are written
	options := []string{"XS", "S", "M", "L", "XL"}
	for _, opt := range options {
		if !bytes.Contains(content, []byte(opt)) {
			t.Errorf("Config should contain option %q", opt)
		}
	}
}

func TestWriteConfig_FilePermissions(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	err := writeConfig(tmpDir, cfg)
	if err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	// Check file exists and is readable
	info, err := os.Stat(tmpDir + "/.gh-pmu.json")
	if err != nil {
		t.Fatalf("Failed to stat config file: %v", err)
	}

	// File should not be a directory
	if info.IsDir() {
		t.Error("Config file should not be a directory")
	}

	// File should have some content
	if info.Size() == 0 {
		t.Error("Config file should not be empty")
	}
}

// TestWriteConfig_DoesNotCreateYAML verifies init only writes .gh-pmu.json
func TestWriteConfig_DoesNotCreateYAML(t *testing.T) {
	tmpDir := t.TempDir()
	protectRepoRoot.Store(false)
	defer protectRepoRoot.Store(true)

	cfg := &InitConfig{
		ProjectOwner:  "test-owner",
		ProjectNumber: 1,
		Repositories:  []string{"test-owner/test-repo"},
	}

	err := writeConfig(tmpDir, cfg)
	if err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	// .gh-pmu.json should exist
	if _, err := os.Stat(filepath.Join(tmpDir, ".gh-pmu.json")); err != nil {
		t.Errorf("Expected .gh-pmu.json to exist: %v", err)
	}

	// .gh-pmu.yml should NOT exist
	if _, err := os.Stat(filepath.Join(tmpDir, ".gh-pmu.yml")); err == nil {
		t.Error("Expected .gh-pmu.yml to NOT be created, but it exists")
	}
}

// ============================================================================
// writeConfig Error Path Tests (IT-3.4)
// ============================================================================

func TestWriteConfig_InvalidDirectory(t *testing.T) {
	// Try to write to a non-existent directory
	nonExistentDir := "/nonexistent/path/that/does/not/exist"

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	err := writeConfig(nonExistentDir, cfg)
	if err == nil {
		t.Error("Expected error when writing to non-existent directory")
	}

	// Check error message mentions file write failure
	if !strings.Contains(err.Error(), "failed to write config file") {
		t.Errorf("Expected 'failed to write config file' error, got: %v", err)
	}
}

func TestWriteConfig_ReadOnlyDirectory(t *testing.T) {
	// Skip on Windows as permission handling differs
	if os.Getenv("OS") == "Windows_NT" || strings.Contains(os.Getenv("OS"), "Windows") {
		t.Skip("Skipping permission test on Windows")
	}

	tmpDir := t.TempDir()

	// Make directory read-only
	if err := os.Chmod(tmpDir, 0444); err != nil {
		t.Fatalf("Failed to make directory read-only: %v", err)
	}
	// Restore permissions for cleanup
	defer func() { _ = os.Chmod(tmpDir, 0755) }()

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	err := writeConfig(tmpDir, cfg)
	if err == nil {
		t.Error("Expected error when writing to read-only directory")
	}
}

func TestWriteConfigWithMetadata_InvalidDirectory(t *testing.T) {
	// Try to write to a non-existent directory
	nonExistentDir := "/nonexistent/path/that/does/not/exist"

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	metadata := &ProjectMetadata{
		ProjectID: "test-id",
		Fields:    []FieldMetadata{},
	}

	err := writeConfigWithMetadata(nonExistentDir, cfg, metadata)
	if err == nil {
		t.Error("Expected error when writing to non-existent directory")
	}

	// Check error message mentions file write failure
	if !strings.Contains(err.Error(), "failed to write config file") {
		t.Errorf("Expected 'failed to write config file' error, got: %v", err)
	}
}

func TestWriteConfig_EmptyConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Empty config should still work (though with empty/default values)
	cfg := &InitConfig{}

	err := writeConfig(tmpDir, cfg)
	if err != nil {
		t.Fatalf("writeConfig with empty config failed: %v", err)
	}

	// Verify file was created
	configPath := filepath.Join(tmpDir, ".gh-pmu.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Config file was not created")
	}
}

func TestWriteConfig_OverwriteExisting(t *testing.T) {
	tmpDir := t.TempDir()

	// Write initial config
	cfg1 := &InitConfig{
		ProjectOwner:  "owner1",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo1"},
	}
	if err := writeConfig(tmpDir, cfg1); err != nil {
		t.Fatalf("Initial writeConfig failed: %v", err)
	}

	// Write second config (should overwrite)
	cfg2 := &InitConfig{
		ProjectOwner:  "owner2",
		ProjectNumber: 2,
		Repositories:  []string{"owner/repo2"},
	}
	if err := writeConfig(tmpDir, cfg2); err != nil {
		t.Fatalf("Second writeConfig failed: %v", err)
	}

	// Read file and verify it has new content
	configPath := filepath.Join(tmpDir, ".gh-pmu.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "owner2") {
		t.Error("Expected config to contain 'owner2' (new value)")
	}
	if strings.Contains(content, "owner1") {
		t.Error("Expected old 'owner1' to be overwritten")
	}
}

func TestWriteConfig_IncludesVersion(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	err := writeConfig(tmpDir, cfg)
	if err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	content, _ := readFile(tmpDir + "/.gh-pmu.json")

	// Version field should be present and match the source constant
	expected := getVersion()
	if !bytes.Contains(content, []byte(`"version": "`+expected+`"`)) {
		t.Errorf("Config should contain version: %s, got:\n%s", expected, string(content))
	}
}

func TestWriteConfigWithMetadata_IncludesVersion(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}
	meta := &ProjectMetadata{
		ProjectID: "test-project-id",
		Fields:    []FieldMetadata{},
	}

	err := writeConfigWithMetadata(tmpDir, cfg, meta)
	if err != nil {
		t.Fatalf("writeConfigWithMetadata failed: %v", err)
	}

	content, _ := readFile(tmpDir + "/.gh-pmu.json")

	expected := getVersion()
	if !bytes.Contains(content, []byte(`"version": "`+expected+`"`)) {
		t.Errorf("Config should contain version: %s, got:\n%s", expected, string(content))
	}
}

func TestWriteConfigWithMetadata_NilMetadataPanics(t *testing.T) {
	// Document that nil metadata causes a panic
	// This test verifies the current behavior - the function does not handle nil metadata
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when metadata is nil, but function didn't panic")
		}
	}()

	tmpDir := t.TempDir()

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	// This should panic because metadata is nil
	// Note: In production, metadata is always provided by the caller
	_ = writeConfigWithMetadata(tmpDir, cfg, nil)
}

// ============================================================================
// loadExistingFramework Tests
// ============================================================================

func TestLoadExistingFramework_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a config file
	configContent := `{"framework":"IDPF","project":{"owner":"test","number":1}}`
	configPath := filepath.Join(tmpDir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Load and verify
	result, err := loadExistingFramework(tmpDir)
	if err != nil {
		t.Fatalf("loadExistingFramework failed: %v", err)
	}

	if result != "IDPF" {
		t.Errorf("Expected framework 'IDPF', got %q", result)
	}
}

func TestLoadExistingFramework_NoFramework(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `{"project":{"owner":"test","number":1}}`
	configPath := filepath.Join(tmpDir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	result, err := loadExistingFramework(tmpDir)
	if err != nil {
		t.Fatalf("loadExistingFramework failed: %v", err)
	}

	if result != "" {
		t.Errorf("Expected empty framework, got %q", result)
	}
}

func TestLoadExistingFramework_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	// Don't create any config file

	_, err := loadExistingFramework(tmpDir)
	if err == nil {
		t.Error("Expected error for missing config file")
	}
}

func TestLoadExistingFramework_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `{not valid json`
	configPath := filepath.Join(tmpDir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	_, err := loadExistingFramework(tmpDir)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// ============================================================================
// isRepoRoot Tests
// ============================================================================

func TestIsRepoRoot_WithGoMod(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a go.mod file
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test"), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	result := isRepoRoot(tmpDir)
	if !result {
		t.Error("Expected isRepoRoot to return true when go.mod exists")
	}
}

func TestIsRepoRoot_WithoutGoMod(t *testing.T) {
	tmpDir := t.TempDir()

	result := isRepoRoot(tmpDir)
	if result {
		t.Error("Expected isRepoRoot to return false when go.mod doesn't exist")
	}
}

func TestIsRepoRoot_InvalidPath(t *testing.T) {
	result := isRepoRoot("/nonexistent/path/12345")
	if result {
		t.Error("Expected isRepoRoot to return false for invalid path")
	}
}

// ============================================================================
// SetRepoRootProtection Tests
// ============================================================================

func TestSetRepoRootProtection_EnablesProtection(t *testing.T) {
	setRepoRootProtectionForTest(t, true)

	tmpDir := t.TempDir()
	// Create go.mod to simulate repo root
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test"), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	err := writeConfig(tmpDir, cfg)
	if err != ErrRepoRootProtected {
		t.Errorf("Expected ErrRepoRootProtected, got: %v", err)
	}
}

func TestSetRepoRootProtection_DisabledAllowsWrite(t *testing.T) {
	setRepoRootProtectionForTest(t, false)

	tmpDir := t.TempDir()
	// Create go.mod to simulate repo root
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test"), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	err := writeConfig(tmpDir, cfg)
	if err != nil {
		t.Errorf("Expected write to succeed with protection disabled, got: %v", err)
	}
}

func TestWriteConfigWithMetadata_RepoRootProtection(t *testing.T) {
	setRepoRootProtectionForTest(t, true)

	tmpDir := t.TempDir()
	// Create go.mod to simulate repo root
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test"), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	metadata := &ProjectMetadata{
		ProjectID: "PVT_test",
		Fields:    []FieldMetadata{},
	}

	err := writeConfigWithMetadata(tmpDir, cfg, metadata)
	if err != ErrRepoRootProtected {
		t.Errorf("Expected ErrRepoRootProtected, got: %v", err)
	}
}

// ============================================================================
// optionNameToAlias Tests (Issue #442)
// ============================================================================

func TestOptionNameToAlias(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple lowercase",
			input:    "Backlog",
			expected: "backlog",
		},
		{
			name:     "space to underscore",
			input:    "In progress",
			expected: "in_progress",
		},
		{
			name:     "multiple spaces",
			input:    "In Review",
			expected: "in_review",
		},
		{
			name:     "emoji prefix with space",
			input:    "🅿️ Parking Lot",
			expected: "parking_lot",
		},
		{
			name:     "emoji prefix no space",
			input:    "🚀Ready",
			expected: "ready",
		},
		{
			name:     "multiple emojis",
			input:    "✅ ✓ Done",
			expected: "done",
		},
		{
			name:     "emoji only",
			input:    "🔥",
			expected: "",
		},
		{
			name:     "already lowercase underscore",
			input:    "in_progress",
			expected: "in_progress",
		},
		{
			name:     "uppercase with underscore",
			input:    "IN_PROGRESS",
			expected: "in_progress",
		},
		{
			name:     "leading and trailing spaces",
			input:    "  Backlog  ",
			expected: "backlog",
		},
		{
			name:     "P0 priority",
			input:    "P0",
			expected: "p0",
		},
		{
			name:     "complex emoji",
			input:    "🏃‍♂️ Running",
			expected: "running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := optionNameToAlias(tt.input)
			if result != tt.expected {
				t.Errorf("optionNameToAlias(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// buildFieldMappingsFromMetadata Tests (Issue #442)
// ============================================================================

func TestBuildFieldMappingsFromMetadata_StatusField(t *testing.T) {
	metadata := &ProjectMetadata{
		ProjectID: "PVT_test",
		Fields: []FieldMetadata{
			{
				ID:       "PVTF_status",
				Name:     "Status",
				DataType: "SINGLE_SELECT",
				Options: []OptionMetadata{
					{ID: "opt1", Name: "Backlog"},
					{ID: "opt2", Name: "In progress"},
					{ID: "opt3", Name: "🅿️ Parking Lot"},
					{ID: "opt4", Name: "Done"},
				},
			},
		},
	}

	mappings := buildFieldMappingsFromMetadata(metadata)

	// Check status field exists
	status, ok := mappings["status"]
	if !ok {
		t.Fatal("Expected 'status' field mapping")
	}

	if status.Field != "Status" {
		t.Errorf("Expected field name 'Status', got %q", status.Field)
	}

	// Check all values are mapped
	expectedMappings := map[string]string{
		"backlog":     "Backlog",
		"in_progress": "In progress",
		"parking_lot": "🅿️ Parking Lot",
		"done":        "Done",
	}

	for alias, expected := range expectedMappings {
		if actual, ok := status.Values[alias]; !ok {
			t.Errorf("Missing alias %q in status values", alias)
		} else if actual != expected {
			t.Errorf("status.Values[%q] = %q, want %q", alias, actual, expected)
		}
	}
}

func TestBuildFieldMappingsFromMetadata_PriorityField(t *testing.T) {
	metadata := &ProjectMetadata{
		ProjectID: "PVT_test",
		Fields: []FieldMetadata{
			{
				ID:       "PVTF_priority",
				Name:     "Priority",
				DataType: "SINGLE_SELECT",
				Options: []OptionMetadata{
					{ID: "opt1", Name: "P0"},
					{ID: "opt2", Name: "P1"},
					{ID: "opt3", Name: "P2"},
					{ID: "opt4", Name: "P3"},
				},
			},
		},
	}

	mappings := buildFieldMappingsFromMetadata(metadata)

	priority, ok := mappings["priority"]
	if !ok {
		t.Fatal("Expected 'priority' field mapping")
	}

	// Check P3 is included (not hardcoded)
	if _, ok := priority.Values["p3"]; !ok {
		t.Error("Expected 'p3' to be included in priority values")
	}
}

func TestBuildFieldMappingsFromMetadata_FallbackDefaults(t *testing.T) {
	// Empty metadata - should use fallback defaults
	metadata := &ProjectMetadata{
		ProjectID: "PVT_test",
		Fields:    []FieldMetadata{},
	}

	mappings := buildFieldMappingsFromMetadata(metadata)

	// Should have default status
	status, ok := mappings["status"]
	if !ok {
		t.Fatal("Expected default 'status' field mapping")
	}
	if _, ok := status.Values["backlog"]; !ok {
		t.Error("Expected default 'backlog' in status values")
	}

	// Should have default priority
	priority, ok := mappings["priority"]
	if !ok {
		t.Fatal("Expected default 'priority' field mapping")
	}
	if _, ok := priority.Values["p0"]; !ok {
		t.Error("Expected default 'p0' in priority values")
	}
}

func TestBuildFieldMappingsFromMetadata_NoOptions(t *testing.T) {
	// Fields exist but have no options - should fall back to defaults
	metadata := &ProjectMetadata{
		ProjectID: "PVT_test",
		Fields: []FieldMetadata{
			{
				ID:       "PVTF_status",
				Name:     "Status",
				DataType: "SINGLE_SELECT",
				Options:  []OptionMetadata{}, // Empty options
			},
		},
	}

	mappings := buildFieldMappingsFromMetadata(metadata)

	// Should fall back to default status values
	status := mappings["status"]
	if _, ok := status.Values["backlog"]; !ok {
		t.Error("Expected fallback to default 'backlog' when no options")
	}
}

func TestBuildFieldMappingsFromMetadata_CaseInsensitiveFieldName(t *testing.T) {
	metadata := &ProjectMetadata{
		ProjectID: "PVT_test",
		Fields: []FieldMetadata{
			{
				ID:       "PVTF_status",
				Name:     "STATUS", // Uppercase
				DataType: "SINGLE_SELECT",
				Options: []OptionMetadata{
					{ID: "opt1", Name: "Active"},
				},
			},
		},
	}

	mappings := buildFieldMappingsFromMetadata(metadata)

	status, ok := mappings["status"]
	if !ok {
		t.Fatal("Expected 'status' field mapping for uppercase 'STATUS'")
	}
	if status.Field != "STATUS" {
		t.Errorf("Expected field name to preserve case 'STATUS', got %q", status.Field)
	}
}

func TestWriteConfigWithMetadata_IncludesParkingLot(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	metadata := &ProjectMetadata{
		ProjectID: "PVT_test",
		Fields: []FieldMetadata{
			{
				ID:       "PVTF_status",
				Name:     "Status",
				DataType: "SINGLE_SELECT",
				Options: []OptionMetadata{
					{ID: "opt1", Name: "Backlog"},
					{ID: "opt2", Name: "🅿️ Parking Lot"},
					{ID: "opt3", Name: "Done"},
				},
			},
		},
	}

	err := writeConfigWithMetadata(tmpDir, cfg, metadata)
	if err != nil {
		t.Fatalf("writeConfigWithMetadata failed: %v", err)
	}

	content, _ := readFile(tmpDir + "/.gh-pmu.json")

	// Should contain parking_lot alias (JSON key format)
	if !bytes.Contains(content, []byte(`"parking_lot"`)) {
		t.Error("Config should contain '\"parking_lot\"' alias")
	}

	// Should contain the original name with emoji
	if !bytes.Contains(content, []byte("Parking Lot")) {
		t.Error("Config should contain 'Parking Lot' value")
	}
}

func TestFindFieldByName(t *testing.T) {
	fields := []api.ProjectField{
		{ID: "1", Name: "Status", DataType: "SINGLE_SELECT"},
		{ID: "2", Name: "Priority", DataType: "SINGLE_SELECT"},
		{ID: "3", Name: "Branch", DataType: "TEXT"},
	}

	tests := []struct {
		name      string
		fieldName string
		wantID    string
		wantNil   bool
	}{
		{
			name:      "find Status field",
			fieldName: "Status",
			wantID:    "1",
			wantNil:   false,
		},
		{
			name:      "find Priority field",
			fieldName: "Priority",
			wantID:    "2",
			wantNil:   false,
		},
		{
			name:      "find Branch field",
			fieldName: "Branch",
			wantID:    "3",
			wantNil:   false,
		},
		{
			name:      "field not found",
			fieldName: "NonExistent",
			wantNil:   true,
		},
		{
			name:      "case sensitive - lowercase",
			fieldName: "status",
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findFieldByName(fields, tt.fieldName)
			if tt.wantNil {
				if result != nil {
					t.Errorf("findFieldByName() = %v, want nil", result)
				}
			} else {
				if result == nil {
					t.Errorf("findFieldByName() = nil, want field with ID %s", tt.wantID)
				} else if result.ID != tt.wantID {
					t.Errorf("findFieldByName().ID = %s, want %s", result.ID, tt.wantID)
				}
			}
		})
	}
}

func TestFindFieldByName_EmptySlice(t *testing.T) {
	result := findFieldByName([]api.ProjectField{}, "Status")
	if result != nil {
		t.Errorf("findFieldByName() on empty slice = %v, want nil", result)
	}
}

// ============================================================================
// Non-Interactive Mode Tests (Issue #609)
// ============================================================================

func TestInitCommand_NonInteractiveIsDeprecated(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("init --help should work: %v", err)
	}

	// Deprecated flags are hidden from help output
	output := buf.String()
	if strings.Contains(output, "--non-interactive") {
		t.Error("Expected --non-interactive to be hidden from help (deprecated)")
	}
}

func TestInitCommand_HasSourceProjectFlag(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "--source-project") {
		t.Error("Expected help output to mention '--source-project'")
	}
}

func TestInitCommand_HasRepoFlag(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "--repo") {
		t.Error("Expected help output to mention '--repo'")
	}
}

func TestInitCommand_HasOwnerFlag(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "--owner") {
		t.Error("Expected help output to mention '--owner'")
	}
}

func TestInitCommand_HasFrameworkFlag(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "--framework") {
		t.Error("Expected help output to mention '--framework'")
	}
}

func TestInitCommand_HasYesFlag(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "--yes") {
		t.Error("Expected help output to mention '--yes'")
	}
	if !strings.Contains(output, "-y") {
		t.Error("Expected help output to mention '-y' shorthand")
	}
}

func TestInitOptions_DefaultFramework(t *testing.T) {
	// Verify the default framework is IDPF
	cmd := newInitCommand()

	// Get the framework flag
	frameworkFlag := cmd.Flags().Lookup("framework")
	if frameworkFlag == nil {
		t.Fatal("Expected --framework flag to exist")
	}

	if frameworkFlag.DefValue != "IDPF" {
		t.Errorf("Expected default framework to be 'IDPF', got %q", frameworkFlag.DefValue)
	}
}

func TestInitNonInteractive_MissingSourceProjectFlag(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--non-interactive", "--repo", "owner/repo"})

	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when --source-project is missing in non-interactive mode")
	}

	// Check error mentions --source-project
	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "--source-project") {
		t.Errorf("Expected error to mention --source-project, got: %s", errOutput)
	}
}

func TestInitNonInteractive_MissingRepoFlag(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--non-interactive", "--source-project", "5"})

	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when --repo is missing in non-interactive mode")
	}

	// Check error mentions --repo
	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "--repo") {
		t.Errorf("Expected error to mention --repo, got: %s", errOutput)
	}
}

func TestInitNonInteractive_MissingBothFlags(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--non-interactive"})

	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when both --source-project and --repo are missing")
	}

	// Check error mentions both flags
	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "--source-project") || !strings.Contains(errOutput, "--repo") {
		t.Errorf("Expected error to mention both --source-project and --repo, got: %s", errOutput)
	}
}

func TestInitNonInteractive_InvalidRepoFormat(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--non-interactive", "--source-project", "5", "--repo", "invalidrepo"})

	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error for invalid repo format")
	}

	// Check error mentions format issue
	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "owner/repo") && !strings.Contains(errOutput, "invalid repo format") {
		t.Errorf("Expected error to mention format issue, got: %s", errOutput)
	}
}

func TestInitNonInteractive_OwnerInferredFromRepo(t *testing.T) {
	// Test that owner is correctly inferred from repo
	// This tests the splitRepository function which is used internally
	tests := []struct {
		repo          string
		expectedOwner string
	}{
		{"myorg/myrepo", "myorg"},
		{"company/repo-name", "company"},
		{"user123/project", "user123"},
	}

	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			owner, _ := splitRepository(tt.repo)
			if owner != tt.expectedOwner {
				t.Errorf("Expected owner %q from repo %q, got %q", tt.expectedOwner, tt.repo, owner)
			}
		})
	}
}

func TestInitNonInteractive_ExistingConfigWithoutYes(t *testing.T) {
	// Create temp directory with existing config
	tmpDir := t.TempDir()

	// Write existing config
	configPath := filepath.Join(tmpDir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, []byte(`{"project":{"owner":"test"}}`), 0644); err != nil {
		t.Fatalf("Failed to write existing config: %v", err)
	}

	// Change to temp directory for the test
	oldWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--non-interactive", "--source-project", "5", "--repo", "owner/repo"})

	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when config exists without --yes flag")
	}

	// Check error mentions --yes or already exists
	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "--yes") && !strings.Contains(errOutput, "already exists") {
		t.Errorf("Expected error to mention --yes or already exists, got: %s", errOutput)
	}
}

// #865 Gap 3 (AC3/AC4): running init from a subdirectory of an initialized project
// detects the parent config (via config.FindConfigFile walking up) and refuses to
// silently create a shadowing nested config.
func TestInitNonInteractive_ParentConfigDetectedFromSubdirectory(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, ".gh-pmu.json"), []byte(`{"project":{"owner":"test","number":1}}`), 0644); err != nil {
		t.Fatalf("Failed to write parent config: %v", err)
	}

	subdir := filepath.Join(tmpDir, "packages", "app")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--non-interactive", "--source-project", "5", "--repo", "owner/repo"})
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error when a parent config exists without --yes")
	}
	if !strings.Contains(errBuf.String(), "already exists") {
		t.Errorf("Expected 'already exists' guard from the parent config, got: %s", errBuf.String())
	}
}

func TestInitNonInteractive_ConfigUsesNewProjectNumber(t *testing.T) {
	// Verify that when non-interactive mode creates a project by copying,
	// the config is written with the NEW project number, not the source number.
	tmpDir := t.TempDir()

	// The source project is #30, but the new project should get a different number.
	// We test this by writing config with a known new project number
	// and verifying it doesn't use the source number.
	sourceNumber := 30
	newNumber := 99
	cfg := &InitConfig{
		ProjectName:   "testrepo",
		ProjectOwner:  "testowner",
		ProjectNumber: newNumber, // Must be the NEW project number
		Repositories:  []string{"testowner/testrepo"},
		Framework:     "IDPF",
	}

	metadata := &ProjectMetadata{
		ProjectID: "PVT_new99",
	}

	err := writeConfigWithMetadata(tmpDir, cfg, metadata)
	if err != nil {
		t.Fatalf("writeConfigWithMetadata failed: %v", err)
	}

	// Read the config and verify it uses the new project number
	configData, err := os.ReadFile(filepath.Join(tmpDir, ".gh-pmu.json"))
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	var configFile ConfigFileWithMetadata
	if err := json.Unmarshal(configData, &configFile); err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	if configFile.Project.Number == sourceNumber {
		t.Errorf("Config should use new project number (%d), not source number (%d)", newNumber, sourceNumber)
	}
	if configFile.Project.Number != newNumber {
		t.Errorf("Expected project number %d, got %d", newNumber, configFile.Project.Number)
	}
}

func TestInitNonInteractive_HelpDescribesSourceProjectCopy(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	_ = cmd.Execute()

	output := buf.String()

	// Verify help mentions source project copying semantics
	if !strings.Contains(output, "source") {
		t.Error("Expected help to mention 'source' project")
	}
	if !strings.Contains(output, "copy") {
		t.Error("Expected help to mention 'copy' behavior")
	}
}

func TestInitNonInteractive_ErrorMentionsSourceProject(t *testing.T) {
	// When --source-project is missing, error should mention --source-project
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--non-interactive", "--repo", "owner/repo"})

	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error when --source-project is missing")
	}

	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "--source-project") {
		t.Errorf("Error should mention --source-project, got: %s", errOutput)
	}
	// Should NOT mention the old --project flag
	if strings.Contains(errOutput, " --project") && !strings.Contains(errOutput, "--source-project") {
		t.Errorf("Error should not mention old --project flag, got: %s", errOutput)
	}
}

func TestDeriveProjectTitle_NoSuffix(t *testing.T) {
	tests := []struct {
		name     string
		repoName string
		expected string
	}{
		{
			name:     "simple repo name",
			repoName: "my-app",
			expected: "my-app",
		},
		{
			name:     "hyphenated repo name",
			repoName: "gh-pmu",
			expected: "gh-pmu",
		},
		{
			name:     "single word repo name",
			repoName: "project",
			expected: "project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveProjectTitle(tt.repoName)
			if got != tt.expected {
				t.Errorf("deriveProjectTitle(%q) = %q, want %q", tt.repoName, got, tt.expected)
			}
			if strings.Contains(got, "Board") {
				t.Errorf("deriveProjectTitle(%q) should not contain 'Board', got %q", tt.repoName, got)
			}
		})
	}
}

func TestWriteConfig_CreatesJSONCompanion(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &InitConfig{
		ProjectOwner:  "test-owner",
		ProjectNumber: 5,
		Repositories:  []string{"test-owner/test-repo"},
	}

	err := writeConfig(tmpDir, cfg)
	if err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	// Verify JSON companion exists
	jsonPath := filepath.Join(tmpDir, config.ConfigFileName)
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Expected JSON companion to exist: %v", err)
	}

	// Verify it's valid JSON with expected content
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON companion is not valid JSON: %v", err)
	}

	project, ok := parsed["project"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected JSON to contain project object")
	}
	if project["owner"] != "test-owner" {
		t.Errorf("Expected owner 'test-owner', got %v", project["owner"])
	}
}

// --- Acceptance Preservation Tests ---

// seedAcceptance writes a .gh-pmu.json with acceptance data and the given version.
func seedAcceptance(t *testing.T, dir, version string) {
	t.Helper()
	acc := map[string]interface{}{
		"version": version,
		"project": map[string]interface{}{
			"owner":  "owner",
			"number": 1,
		},
		"repositories": []string{"owner/repo"},
		"acceptance": map[string]interface{}{
			"accepted": true,
			"user":     "testuser",
			"date":     "2026-01-15",
			"version":  version,
		},
	}
	data, err := json.MarshalIndent(acc, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal seed config: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, config.ConfigFileName), data, 0644); err != nil {
		t.Fatalf("Failed to write seed config: %v", err)
	}
}

func TestWriteConfigWithMetadata_PreservesAcceptance_PatchVersion(t *testing.T) {
	tmpDir := t.TempDir()

	// Derive seed version from current version with same major.minor but patch 0
	// so that writeConfigWithMetadata sees a patch-only difference and preserves acceptance.
	cur := getVersion()
	parts := strings.SplitN(cur, ".", 3)
	seedVer := "1.0.0" // fallback for dev builds
	if len(parts) >= 2 {
		seedVer = parts[0] + "." + parts[1] + ".0"
	}
	seedAcceptance(t, tmpDir, seedVer)

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}
	meta := &ProjectMetadata{
		ProjectID: "test-project-id",
		Fields:    []FieldMetadata{},
	}

	err := writeConfigWithMetadata(tmpDir, cfg, meta)
	if err != nil {
		t.Fatalf("writeConfigWithMetadata failed: %v", err)
	}

	// Read the JSON output and check acceptance
	jsonPath := filepath.Join(tmpDir, config.ConfigFileName)
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Failed to read JSON config: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON is not valid: %v", err)
	}

	acc, ok := parsed["acceptance"]
	if !ok {
		t.Fatal("Expected acceptance section to be preserved on patch version change, but it was missing")
	}

	accMap, ok := acc.(map[string]interface{})
	if !ok {
		t.Fatal("acceptance is not an object")
	}

	if accMap["accepted"] != true {
		t.Errorf("Expected accepted=true, got %v", accMap["accepted"])
	}
	if accMap["user"] != "testuser" {
		t.Errorf("Expected user=testuser, got %v", accMap["user"])
	}
}

func TestWriteConfigWithMetadata_ClearsAcceptance_MajorVersion(t *testing.T) {
	tmpDir := t.TempDir()
	// Seed with version 0.9.0 — any current version 1.x.x is a major bump
	seedAcceptance(t, tmpDir, "0.9.0")

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}
	meta := &ProjectMetadata{
		ProjectID: "test-project-id",
		Fields:    []FieldMetadata{},
	}

	err := writeConfigWithMetadata(tmpDir, cfg, meta)
	if err != nil {
		t.Fatalf("writeConfigWithMetadata failed: %v", err)
	}

	jsonPath := filepath.Join(tmpDir, config.ConfigFileName)
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Failed to read JSON config: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON is not valid: %v", err)
	}

	if _, ok := parsed["acceptance"]; ok {
		t.Error("Expected acceptance to be cleared on major version change, but it was present")
	}
}

func TestWriteConfigWithMetadata_ClearsAcceptance_MinorVersion(t *testing.T) {
	tmpDir := t.TempDir()

	// Seed with a version that differs in minor from current.
	currentVersion := getVersion()
	// Parse manually to construct a different-minor version
	parts := strings.SplitN(currentVersion, ".", 3)
	if len(parts) >= 2 {
		// Same major, different minor (add 1)
		major := parts[0]
		minor := 999 // guaranteed different
		differentMinor := fmt.Sprintf("%s.%d.0", major, minor)
		seedAcceptance(t, tmpDir, differentMinor)
	} else {
		// Dev build or unparseable — seed with "1.0.0" to guarantee diff
		seedAcceptance(t, tmpDir, "1.0.0")
	}

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}
	meta := &ProjectMetadata{
		ProjectID: "test-project-id",
		Fields:    []FieldMetadata{},
	}

	err := writeConfigWithMetadata(tmpDir, cfg, meta)
	if err != nil {
		t.Fatalf("writeConfigWithMetadata failed: %v", err)
	}

	jsonPath := filepath.Join(tmpDir, config.ConfigFileName)
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Failed to read JSON config: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON is not valid: %v", err)
	}

	if _, ok := parsed["acceptance"]; ok {
		t.Error("Expected acceptance to be cleared on minor version change, but it was present")
	}
}

func TestWriteConfigWithMetadata_PreservesAcceptance_SameVersion(t *testing.T) {
	tmpDir := t.TempDir()

	// Seed with exactly the current version
	currentVersion := getVersion()
	seedAcceptance(t, tmpDir, currentVersion)

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}
	meta := &ProjectMetadata{
		ProjectID: "test-project-id",
		Fields:    []FieldMetadata{},
	}

	err := writeConfigWithMetadata(tmpDir, cfg, meta)
	if err != nil {
		t.Fatalf("writeConfigWithMetadata failed: %v", err)
	}

	jsonPath := filepath.Join(tmpDir, config.ConfigFileName)
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Failed to read JSON config: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON is not valid: %v", err)
	}

	acc, ok := parsed["acceptance"]
	if !ok {
		t.Fatal("Expected acceptance section to be preserved on same version re-init, but it was missing")
	}

	accMap, ok := acc.(map[string]interface{})
	if !ok {
		t.Fatal("acceptance is not an object")
	}

	if accMap["accepted"] != true {
		t.Errorf("Expected accepted=true, got %v", accMap["accepted"])
	}
	if accMap["version"] != currentVersion {
		t.Errorf("Expected version=%s, got %v", currentVersion, accMap["version"])
	}
}

// ============================================================================
// --project flag tests (connect to existing project)
// ============================================================================

func TestInitCommand_HasProjectFlag(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("init --help should not error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "--project") {
		t.Error("Expected --help output to mention --project flag")
	}
}

func TestInit_MutualExclusion_ProjectAndSourceProject(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--project", "42", "--source-project", "5", "--repo", "owner/repo"})

	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when both --project and --source-project are provided")
	}

	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "none of the others can be") && !strings.Contains(errOutput, "mutually exclusive") {
		t.Errorf("Expected mutual exclusion error, got: %s", errOutput)
	}
}

func TestInit_ProjectRequiresRepo(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--project", "42"})

	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when --repo is missing with --project")
	}

	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "--repo") {
		t.Errorf("Expected error to mention --repo, got: %s", errOutput)
	}
}

func TestInit_NeitherProjectNorSourceProject(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--repo", "owner/repo"})

	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when neither --project nor --source-project is provided")
	}

	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "--source-project") || !strings.Contains(errOutput, "--project") {
		t.Errorf("Expected error to mention --source-project or --project, got: %s", errOutput)
	}
}

// ============================================================================
// --non-interactive deprecation tests
// ============================================================================

func TestInit_NonInteractiveEmitsDeprecationWarning(t *testing.T) {
	cmd := NewRootCommand()
	// Use --non-interactive with minimal flags to trigger deprecation during parsing
	// Cobra prints deprecation to stderr during flag parsing before command execution
	cmd.SetArgs([]string{"init", "--non-interactive", "--source-project", "5", "--repo", "owner/repo"})

	combinedBuf := new(bytes.Buffer)
	cmd.SetOut(combinedBuf)
	cmd.SetErr(combinedBuf)

	// Will fail at API call, but deprecation warning should appear before the error
	_ = cmd.Execute()

	output := combinedBuf.String()
	if !strings.Contains(output, "deprecated") {
		t.Errorf("Expected deprecation warning when --non-interactive is used, got: %s", output)
	}
}

func TestInit_NonInteractiveFlagStillAccepted(t *testing.T) {
	cmd := NewRootCommand()
	// Should not return "unknown flag" error
	cmd.SetArgs([]string{"init", "--non-interactive", "--source-project", "5", "--repo", "owner/repo"})

	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)

	err := cmd.Execute()

	// It will fail at the API call, but should NOT fail with "unknown flag"
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Error("--non-interactive should still be accepted (deprecated, not removed)")
	}
}

// ============================================================================
// Branch field alias emission + preservation (Issue #841)
// ============================================================================

// TestBuildFieldMappingsFromMetadata_BranchField verifies that a Branch (TEXT)
// field present in project metadata is emitted as fields.branch in the config.
func TestBuildFieldMappingsFromMetadata_BranchField(t *testing.T) {
	metadata := &ProjectMetadata{
		ProjectID: "PVT_test",
		Fields: []FieldMetadata{
			{ID: "PVTF_branch", Name: "Branch", DataType: "TEXT"},
		},
	}

	mappings := buildFieldMappingsFromMetadata(metadata)

	branch, ok := mappings["branch"]
	if !ok {
		t.Fatal("Expected 'branch' field mapping when Branch field exists in metadata")
	}
	if branch.Field != "Branch" {
		t.Errorf("Expected branch.Field = %q, got %q", "Branch", branch.Field)
	}
}

// TestBuildFieldMappingsFromMetadata_NoBranchField verifies that an absent Branch
// field does not produce a fields.branch entry (so `cfg.Fields[\"branch\"]` absence
// remains meaningful for upstream callers).
func TestBuildFieldMappingsFromMetadata_NoBranchField(t *testing.T) {
	metadata := &ProjectMetadata{
		ProjectID: "PVT_test",
		Fields: []FieldMetadata{
			{ID: "PVTF_status", Name: "Status", DataType: "SINGLE_SELECT", Options: []OptionMetadata{{ID: "o1", Name: "Backlog"}}},
		},
	}

	mappings := buildFieldMappingsFromMetadata(metadata)

	if _, ok := mappings["branch"]; ok {
		t.Error("Did not expect 'branch' mapping when Branch field is absent from metadata")
	}
}

// TestWriteConfigWithMetadata_EmitsBranchAlias verifies AC 2: a freshly written
// config file includes fields.branch: { field: "Branch" } when the project has a Branch field.
func TestWriteConfigWithMetadata_EmitsBranchAlias(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &InitConfig{
		ProjectOwner:  "owner",
		ProjectNumber: 1,
		Repositories:  []string{"owner/repo"},
	}

	metadata := &ProjectMetadata{
		ProjectID: "PVT_test",
		Fields: []FieldMetadata{
			{ID: "PVTF_branch", Name: "Branch", DataType: "TEXT"},
		},
	}

	if err := writeConfigWithMetadata(tmpDir, cfg, metadata); err != nil {
		t.Fatalf("writeConfigWithMetadata failed: %v", err)
	}

	content, _ := readFile(tmpDir + "/.gh-pmu.json")
	if !bytes.Contains(content, []byte(`"branch"`)) {
		t.Error("Config should contain \"branch\" field alias key")
	}
	if !bytes.Contains(content, []byte(`"field": "Branch"`)) {
		t.Error("Config should map branch alias to field name \"Branch\"")
	}
}

// TestWriteConfigWithMetadata_PreservesExistingBranchAlias verifies AC 3: re-init
// against an existing .gh-pmu.json preserves a user-renamed fields.branch.field
// instead of clobbering it with the project's current field name.
func TestWriteConfigWithMetadata_PreservesExistingBranchAlias(t *testing.T) {
	tmpDir := t.TempDir()

	// Seed an existing config with a user-renamed branch alias
	existing := &ConfigFileWithMetadata{
		Version:      getVersion(),
		Project:      ProjectConfig{Name: "X", Owner: "owner", Number: 1},
		Repositories: []string{"owner/repo"},
		Defaults:     DefaultsConfig{Priority: "p2", Status: "backlog"},
		Fields: map[string]FieldMapping{
			"branch": {Field: "Git Branch"}, // user renamed it
		},
		Metadata: MetadataSection{Project: MetadataProject{ID: "PVT_existing"}},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(tmpDir, config.ConfigFileName), data, 0600); err != nil {
		t.Fatalf("seed config write failed: %v", err)
	}

	// Re-init: fresh project metadata has "Branch" as the canonical name
	cfg := &InitConfig{ProjectOwner: "owner", ProjectNumber: 1, Repositories: []string{"owner/repo"}}
	metadata := &ProjectMetadata{
		ProjectID: "PVT_test",
		Fields: []FieldMetadata{
			{ID: "PVTF_branch", Name: "Branch", DataType: "TEXT"},
		},
	}

	if err := writeConfigWithMetadata(tmpDir, cfg, metadata); err != nil {
		t.Fatalf("writeConfigWithMetadata failed: %v", err)
	}

	content, _ := readFile(filepath.Join(tmpDir, config.ConfigFileName))
	var got ConfigFileWithMetadata
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("unmarshal written config: %v", err)
	}
	branch, ok := got.Fields["branch"]
	if !ok {
		t.Fatal("Expected 'branch' field mapping to be preserved on re-init")
	}
	if branch.Field != "Git Branch" {
		t.Errorf("Expected preserved user alias %q, got %q (re-init clobbered the rename)", "Git Branch", branch.Field)
	}
}

// ============================================================================
// ensureOptionalProjectFields tests (Issue #844)
//
// Verifies that the create-if-missing helper invoked by both --source-project
// and --project init paths creates absent fields (e.g. Branch) on the project
// and skips fields that already exist.
// ============================================================================

// mockOptionalFieldClient implements the optionalFieldClient interface for
// testing ensureOptionalProjectFields without hitting the API.
type mockOptionalFieldClient struct {
	existingByName  map[string]bool
	fieldExistsErr  map[string]error
	createFieldErr  map[string]error
	createCalls     []mockCreateFieldCall
	fieldExistsCall []string
}

type mockCreateFieldCall struct {
	ProjectID string
	Name      string
	DataType  string
	Options   []string
}

func (m *mockOptionalFieldClient) FieldExists(projectID, name string) (bool, error) {
	m.fieldExistsCall = append(m.fieldExistsCall, name)
	if err, ok := m.fieldExistsErr[name]; ok {
		return false, err
	}
	return m.existingByName[name], nil
}

func (m *mockOptionalFieldClient) CreateProjectField(projectID, name, dataType string, options []string) (*api.ProjectField, error) {
	if err, ok := m.createFieldErr[name]; ok {
		return nil, err
	}
	m.createCalls = append(m.createCalls, mockCreateFieldCall{
		ProjectID: projectID,
		Name:      name,
		DataType:  dataType,
		Options:   options,
	})
	return &api.ProjectField{
		ID:       "PVTF_" + name,
		Name:     name,
		DataType: dataType,
	}, nil
}

func TestEnsureOptionalProjectFields_BranchMissing_CreatesBranch(t *testing.T) {
	mock := &mockOptionalFieldClient{
		existingByName: map[string]bool{
			"Priority": true,
			"Branch":   false,
		},
	}
	defs := []defaults.FieldDef{
		{Name: "Priority", Type: "SINGLE_SELECT", Options: []string{"P0", "P1", "P2"}},
		{Name: "Branch", Type: "TEXT"},
	}

	errBuf := new(bytes.Buffer)
	ensureOptionalProjectFields(mock, "PVT_test", defs, errBuf)

	if len(mock.createCalls) != 1 {
		t.Fatalf("expected exactly 1 CreateProjectField call (Branch), got %d: %+v", len(mock.createCalls), mock.createCalls)
	}
	if mock.createCalls[0].Name != "Branch" {
		t.Errorf("expected Branch to be created, got %q", mock.createCalls[0].Name)
	}
	if mock.createCalls[0].DataType != "TEXT" {
		t.Errorf("expected Branch DataType TEXT, got %q", mock.createCalls[0].DataType)
	}
}

func TestEnsureOptionalProjectFields_BranchExists_NoCreate(t *testing.T) {
	mock := &mockOptionalFieldClient{
		existingByName: map[string]bool{
			"Priority": true,
			"Branch":   true,
		},
	}
	defs := []defaults.FieldDef{
		{Name: "Priority", Type: "SINGLE_SELECT", Options: []string{"P0", "P1", "P2"}},
		{Name: "Branch", Type: "TEXT"},
	}

	errBuf := new(bytes.Buffer)
	ensureOptionalProjectFields(mock, "PVT_test", defs, errBuf)

	if len(mock.createCalls) != 0 {
		t.Errorf("expected no CreateProjectField calls when all fields exist, got %d: %+v", len(mock.createCalls), mock.createCalls)
	}
}

func TestEnsureOptionalProjectFields_FieldExistsError_LogsWarningAndContinues(t *testing.T) {
	mock := &mockOptionalFieldClient{
		existingByName: map[string]bool{"Branch": false},
		fieldExistsErr: map[string]error{"Priority": fmt.Errorf("graphql: rate limit")},
	}
	defs := []defaults.FieldDef{
		{Name: "Priority", Type: "SINGLE_SELECT", Options: []string{"P0"}},
		{Name: "Branch", Type: "TEXT"},
	}

	errBuf := new(bytes.Buffer)
	ensureOptionalProjectFields(mock, "PVT_test", defs, errBuf)

	if !strings.Contains(errBuf.String(), "Priority") {
		t.Errorf("expected warning to mention failing field name Priority, got: %s", errBuf.String())
	}
	createdNames := make([]string, 0, len(mock.createCalls))
	for _, c := range mock.createCalls {
		createdNames = append(createdNames, c.Name)
	}
	found := false
	for _, n := range createdNames {
		if n == "Branch" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Branch to still be created after Priority FieldExists error, got created: %v", createdNames)
	}
}

func TestEnsureOptionalProjectFields_CreateFieldError_LogsWarningAndContinues(t *testing.T) {
	mock := &mockOptionalFieldClient{
		existingByName: map[string]bool{"Branch": false, "Priority": false},
		createFieldErr: map[string]error{"Priority": fmt.Errorf("permission denied")},
	}
	defs := []defaults.FieldDef{
		{Name: "Priority", Type: "SINGLE_SELECT", Options: []string{"P0"}},
		{Name: "Branch", Type: "TEXT"},
	}

	errBuf := new(bytes.Buffer)
	ensureOptionalProjectFields(mock, "PVT_test", defs, errBuf)

	if !strings.Contains(errBuf.String(), "Priority") {
		t.Errorf("expected warning to mention failing field name Priority, got: %s", errBuf.String())
	}
	branchCreated := false
	for _, c := range mock.createCalls {
		if c.Name == "Branch" {
			branchCreated = true
		}
	}
	if !branchCreated {
		t.Errorf("expected Branch to still be created after Priority CreateProjectField error, got: %+v", mock.createCalls)
	}
}

// ============================================================================
// AC4 — --source-project guard tests (#847)
// ============================================================================

func TestSourceProjectGuard_AllowsWhenNoExistingConfig(t *testing.T) {
	if err := sourceProjectGuard(0, 30, false); err != nil {
		t.Errorf("Expected nil (no existing project), got: %v", err)
	}
}

func TestSourceProjectGuard_AllowsWhenForcePresent(t *testing.T) {
	if err := sourceProjectGuard(46, 30, true); err != nil {
		t.Errorf("Expected nil with --force, got: %v", err)
	}
}

func TestSourceProjectGuard_RefusesWhenExistingProjectAndNoForce(t *testing.T) {
	err := sourceProjectGuard(46, 30, false)
	if err == nil {
		t.Fatal("Expected error when existing project + --source-project + no --force")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--project") {
		t.Errorf("Expected error to recommend --project, got: %v", err)
	}
	if !strings.Contains(msg, "--force") {
		t.Errorf("Expected error to mention --force override, got: %v", err)
	}
}

func TestSourceProjectGuard_AllowsWhenSourceProjectZero(t *testing.T) {
	// Caller didn't pass --source-project; nothing to guard.
	if err := sourceProjectGuard(46, 0, false); err != nil {
		t.Errorf("Expected nil when sourceProject == 0, got: %v", err)
	}
}

func TestInitNonInteractive_SourceProjectGuard_RefusesWithoutForce(t *testing.T) {
	tmpDir := t.TempDir()

	// Existing config declares project #46
	configPath := filepath.Join(tmpDir, ".gh-pmu.json")
	body := `{"version":"1.4.6","project":{"owner":"test","number":46},"repositories":["test/repo"],"framework":"IDPF","defaults":{"priority":"p2","status":"backlog"},"fields":{}}`
	if err := os.WriteFile(configPath, []byte(body), 0644); err != nil {
		t.Fatalf("Failed to write existing config: %v", err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"init", "--source-project", "30", "--repo", "owner/repo", "-y"})

	out := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(errBuf)

	if err := cmd.Execute(); err == nil {
		t.Fatal("Expected refusal when existing project + --source-project + no --force")
	}
	msg := errBuf.String()
	if !strings.Contains(msg, "already declares project #46") {
		t.Errorf("Expected guard message to name existing project #46, got: %s", msg)
	}
	if !strings.Contains(msg, "--force") {
		t.Errorf("Expected guard message to suggest --force, got: %s", msg)
	}
}

// ============================================================================
// AC3 — Structured failure trailer tests (#847)
// ============================================================================

func TestPrintInitFailureTrailer_AllFields(t *testing.T) {
	var buf bytes.Buffer
	printInitFailureTrailer(&buf, 176, FailedStepGetFields, false)
	out := buf.String()
	if !strings.Contains(out, "destinationProjectNumber=176") {
		t.Errorf("Expected destinationProjectNumber=176 in output, got: %s", out)
	}
	if !strings.Contains(out, "failedStep=get-fields") {
		t.Errorf("Expected failedStep=get-fields, got: %s", out)
	}
	if !strings.Contains(out, "configRewritten=false") {
		t.Errorf("Expected configRewritten=false, got: %s", out)
	}
}

func TestPrintInitFailureTrailer_NoProjectCreated(t *testing.T) {
	// projectNumber=0 means project was never created (failure before/at create-project).
	// Render explicitly as 'none' rather than "0" to avoid ambiguity.
	var buf bytes.Buffer
	printInitFailureTrailer(&buf, 0, FailedStepCreateProject, false)
	out := buf.String()
	if !strings.Contains(out, "destinationProjectNumber=none") {
		t.Errorf("Expected destinationProjectNumber=none for projectNumber=0, got: %s", out)
	}
	if !strings.Contains(out, "failedStep=create-project") {
		t.Errorf("Expected failedStep=create-project, got: %s", out)
	}
}

func TestPrintInitFailureTrailer_ConfigWritten(t *testing.T) {
	var buf bytes.Buffer
	printInitFailureTrailer(&buf, 176, FailedStepWriteConfig, true)
	out := buf.String()
	if !strings.Contains(out, "configRewritten=true") {
		t.Errorf("Expected configRewritten=true, got: %s", out)
	}
}

func TestFailedStep_EnumStability(t *testing.T) {
	// AC5 decision (#847): label-setup is warn+continue, NOT a hard-fail step.
	// Enum must NOT include label-setup; if it ever does, the trailer contract
	// becomes inconsistent with the documented label-loop posture.
	allowed := map[string]bool{
		"create-project":           true,
		"get-fields":               true,
		"validate-required-fields": true,
		"refetch-fields":           true,
		"write-config":             true,
		"source-copy":              true,
	}
	for _, step := range AllFailedSteps {
		if !allowed[step] {
			t.Errorf("Unexpected failedStep value %q — keep enum aligned with #847 AC5 (label-setup excluded)", step)
		}
	}
	if _, ok := allowed["label-setup"]; ok {
		t.Fatal("Test fixture wrong: label-setup must not be allowed")
	}
}

// ============================================================================
// AC2 — Atomicity tests (#847)
// ============================================================================

// fakeInitClient is a test double for initPostCreateClient. Each method's
// behavior is configurable via callbacks so individual tests can simulate
// failures at specific steps.
type fakeInitClient struct {
	getFieldsErr     error
	getFieldsResult  []api.ProjectField
	refetchFieldsErr error
	refetchResult    []api.ProjectField
	getFieldsCalls   int

	labelExistsErr error
	createLabelErr error
	fieldExistsErr error
	createFieldErr error

	deleteProjectErr   error
	deleteProjectCalls []string

	updateOptionsErr   error
	updateOptionsCalls [][]api.FieldOptionUpdate
}

func (f *fakeInitClient) UpdateProjectFieldOptions(fieldID string, options []api.FieldOptionUpdate) ([]api.FieldOption, error) {
	f.updateOptionsCalls = append(f.updateOptionsCalls, options)
	return nil, f.updateOptionsErr
}

func (f *fakeInitClient) GetProjectFields(projectID string) ([]api.ProjectField, error) {
	f.getFieldsCalls++
	if f.getFieldsCalls == 1 {
		return f.getFieldsResult, f.getFieldsErr
	}
	if f.refetchFieldsErr != nil {
		return nil, f.refetchFieldsErr
	}
	if f.refetchResult != nil {
		return f.refetchResult, nil
	}
	return f.getFieldsResult, nil
}
func (f *fakeInitClient) LabelExists(o, r, n string) (bool, error) { return true, f.labelExistsErr }
func (f *fakeInitClient) CreateLabel(o, r, n, c, d string) error   { return f.createLabelErr }
func (f *fakeInitClient) FieldExists(p, n string) (bool, error)    { return true, f.fieldExistsErr }
func (f *fakeInitClient) CreateProjectField(p, n, dt string, opts []string) (*api.ProjectField, error) {
	return nil, f.createFieldErr
}
func (f *fakeInitClient) DeleteProject(projectID string) error {
	f.deleteProjectCalls = append(f.deleteProjectCalls, projectID)
	return f.deleteProjectErr
}

func newAtomicityInputs(t *testing.T, errOut *bytes.Buffer) *initPostCreateInputs {
	t.Helper()
	tmpDir := t.TempDir()
	return &initPostCreateInputs{
		Cwd:       tmpDir,
		RepoOwner: "owner",
		RepoName:  "repo",
		Framework: "none", // skip required-field validation for clean defer test
		NewProject: &api.Project{
			ID:     "PVT_new123",
			Number: 176,
			Title:  "test-repo",
		},
		Cfg: &InitConfig{
			ProjectName:   "test-repo",
			ProjectOwner:  "owner",
			ProjectNumber: 176,
			Repositories:  []string{"owner/repo"},
			Framework:     "none",
		},
		Defs:   &defaults.Defaults{},
		ErrOut: errOut,
	}
}

func TestRunInitPostCreate_GetFieldsFailure_RollsBackProject(t *testing.T) {
	errBuf := &bytes.Buffer{}
	client := &fakeInitClient{getFieldsErr: fmt.Errorf("non-200 OK status code: 504")}
	in := newAtomicityInputs(t, errBuf)

	err := runInitPostCreate(client, in)
	if err == nil {
		t.Fatal("Expected error from runInitPostCreate")
	}

	// Rollback: DeleteProject called with the just-created project's ID.
	if len(client.deleteProjectCalls) != 1 || client.deleteProjectCalls[0] != "PVT_new123" {
		t.Errorf("Expected DeleteProject('PVT_new123'), got: %v", client.deleteProjectCalls)
	}

	// Trailer: failedStep + projectNumber + configRewritten=false (config never reached).
	out := errBuf.String()
	if !strings.Contains(out, "failedStep=get-fields") {
		t.Errorf("Expected trailer failedStep=get-fields, got: %s", out)
	}
	if !strings.Contains(out, "destinationProjectNumber=176") {
		t.Errorf("Expected destinationProjectNumber=176, got: %s", out)
	}
	if !strings.Contains(out, "configRewritten=false") {
		t.Errorf("Expected configRewritten=false (config write never attempted), got: %s", out)
	}

	// Defer: .gh-pmu.json must NOT exist in cwd.
	if _, err := os.Stat(filepath.Join(in.Cwd, ".gh-pmu.json")); err == nil {
		t.Error("Expected .gh-pmu.json NOT to exist after pre-config-write failure (defer variant)")
	}
}

func TestRunInitPostCreate_RollbackErrorDoesNotMaskOriginal(t *testing.T) {
	errBuf := &bytes.Buffer{}
	client := &fakeInitClient{
		getFieldsErr:     fmt.Errorf("non-200 OK status code: 504"),
		deleteProjectErr: fmt.Errorf("rollback also failed"),
	}
	in := newAtomicityInputs(t, errBuf)

	err := runInitPostCreate(client, in)
	if err == nil {
		t.Fatal("Expected original error preserved")
	}
	if !strings.Contains(err.Error(), "could not fetch project fields") {
		t.Errorf("Expected original 'could not fetch project fields' error, got: %v", err)
	}
	out := errBuf.String()
	if !strings.Contains(out, "warning: rollback DeleteProject(PVT_new123) failed") {
		t.Errorf("Expected rollback-failure warning, got: %s", out)
	}
}

func TestRunInitPostCreate_LabelLoopFailure_DoesNotRollBack(t *testing.T) {
	// AC5: label-loop is warn+continue. Must NOT trigger rollback.
	errBuf := &bytes.Buffer{}
	client := &fakeInitClient{
		labelExistsErr: fmt.Errorf("transient label-check failure"),
	}
	in := newAtomicityInputs(t, errBuf)
	in.Framework = "IDPF"
	in.Defs = &defaults.Defaults{
		Labels: []defaults.LabelDef{{Name: "active", Color: "0e8a16"}},
	}

	err := runInitPostCreate(client, in)
	if err != nil {
		t.Fatalf("Expected nil (label failure is warn+continue), got: %v", err)
	}
	if len(client.deleteProjectCalls) != 0 {
		t.Errorf("Expected zero DeleteProject calls (warn+continue), got: %v", client.deleteProjectCalls)
	}
	out := errBuf.String()
	if !strings.Contains(out, "Warning: failed to check label") {
		t.Errorf("Expected warning to be emitted for label failure, got: %s", out)
	}
}

// TestRunInitPostCreate_ReconcilesStatus (#917): a copied project missing
// required Status values gets them added instead of being rolled back, and the
// written config carries an alias for every Status value.
func TestRunInitPostCreate_ReconcilesStatus(t *testing.T) {
	errBuf := &bytes.Buffer{}
	defs, err := defaults.Load()
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	var full []api.FieldOption
	for i, def := range canonicalStatusDefs(t) {
		full = append(full, api.FieldOption{ID: fmt.Sprintf("o%d", i), Name: def.Name, Color: def.Color})
	}
	client := &fakeInitClient{
		getFieldsResult: []api.ProjectField{{ID: "F", Name: "Status", DataType: "SINGLE_SELECT", Options: project11Status()}},
		refetchResult:   []api.ProjectField{{ID: "F", Name: "Status", DataType: "SINGLE_SELECT", Options: full}},
	}
	in := newAtomicityInputs(t, errBuf)
	in.Framework = "IDPF"
	in.Defs = &defaults.Defaults{Fields: defs.Fields}

	if err := runInitPostCreate(client, in); err != nil {
		t.Fatalf("missing Status values must be added, not fail init: %v\n%s", err, errBuf.String())
	}
	if len(client.deleteProjectCalls) != 0 {
		t.Errorf("expected no rollback, got %v", client.deleteProjectCalls)
	}
	if len(client.updateOptionsCalls) != 1 {
		t.Fatalf("expected one Status options update, got %d", len(client.updateOptionsCalls))
	}

	cfg, err := config.Load(filepath.Join(in.Cwd, ".gh-pmu.json"))
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	for _, alias := range []string{"backlog", "up_next", "ready", "in_progress", "in_review", "qa_required", "done", "parking_lot", "notes"} {
		if _, ok := cfg.Fields["status"].Values[alias]; !ok {
			t.Errorf("written config missing status alias %q: %v", alias, cfg.Fields["status"].Values)
		}
	}
}

func TestRunInitPostCreate_StatusUpdateFailure_RollsBack(t *testing.T) {
	errBuf := &bytes.Buffer{}
	defs, err := defaults.Load()
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	client := &fakeInitClient{
		getFieldsResult:  []api.ProjectField{{ID: "F", Name: "Status", DataType: "SINGLE_SELECT", Options: project11Status()}},
		updateOptionsErr: fmt.Errorf("forbidden"),
	}
	in := newAtomicityInputs(t, errBuf)
	in.Framework = "IDPF"
	in.Defs = &defaults.Defaults{Fields: defs.Fields}

	if err := runInitPostCreate(client, in); err == nil {
		t.Fatal("expected the Status update failure to fail init")
	}
	if len(client.deleteProjectCalls) != 1 {
		t.Errorf("expected rollback of the new project, got %v", client.deleteProjectCalls)
	}
	if !strings.Contains(errBuf.String(), "failedStep=validate-required-fields") {
		t.Errorf("expected the required-fields failure step in the trailer, got: %s", errBuf.String())
	}
}

func TestRunInitPostCreate_Success_NoRollback(t *testing.T) {
	errBuf := &bytes.Buffer{}
	client := &fakeInitClient{}
	in := newAtomicityInputs(t, errBuf)

	if err := runInitPostCreate(client, in); err != nil {
		t.Fatalf("Expected success, got: %v", err)
	}
	if len(client.deleteProjectCalls) != 0 {
		t.Errorf("Expected zero DeleteProject calls on success, got: %v", client.deleteProjectCalls)
	}
	if _, err := os.Stat(filepath.Join(in.Cwd, ".gh-pmu.json")); err != nil {
		t.Errorf("Expected .gh-pmu.json to exist after successful init: %v", err)
	}
}
