package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/rubrical-works/gh-pmu/internal/defaults"
	"github.com/rubrical-works/gh-pmu/internal/ui"
	"github.com/spf13/cobra"
)

// mockLabelClient implements labelClient interface for testing
type mockLabelClient struct {
	labels         map[string]*api.RepoLabel // name → label (nil = not found)
	allLabels      []api.RepoLabel
	createCalls    []createLabelCall
	updateCalls    []updateLabelCall
	deleteCalls    []string
	createErr      error
	updateErr      error
	deleteErr      error
	listErr        error
	getLabelErr    error
	labelExistsErr error
}

type createLabelCall struct {
	name, color, description string
}

type updateLabelCall struct {
	oldName, newName, color, description string
}

func newMockLabelClient() *mockLabelClient {
	return &mockLabelClient{
		labels: make(map[string]*api.RepoLabel),
	}
}

func (m *mockLabelClient) LabelExists(owner, repo, labelName string) (bool, error) {
	if m.labelExistsErr != nil {
		return false, m.labelExistsErr
	}
	_, ok := m.labels[labelName]
	return ok, nil
}

func (m *mockLabelClient) CreateLabel(owner, repo, name, color, description string) error {
	m.createCalls = append(m.createCalls, createLabelCall{name, color, description})
	if m.createErr != nil {
		return m.createErr
	}
	m.labels[name] = &api.RepoLabel{Name: name, Color: color, Description: description}
	return nil
}

func (m *mockLabelClient) UpdateLabel(owner, repo, labelName, newName, newColor, newDescription string) error {
	m.updateCalls = append(m.updateCalls, updateLabelCall{labelName, newName, newColor, newDescription})
	if m.updateErr != nil {
		return m.updateErr
	}
	return nil
}

func (m *mockLabelClient) DeleteLabel(owner, repo, labelName string) error {
	m.deleteCalls = append(m.deleteCalls, labelName)
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.labels, labelName)
	return nil
}

func (m *mockLabelClient) ListLabels(owner, repo string) ([]api.RepoLabel, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.allLabels, nil
}

func (m *mockLabelClient) GetLabel(owner, repo, labelName string) (*api.RepoLabel, error) {
	if m.getLabelErr != nil {
		return nil, m.getLabelErr
	}
	label, ok := m.labels[labelName]
	if !ok {
		return nil, nil
	}
	return label, nil
}

// setupLabelTestDir creates a temp directory with config and chdir
func setupLabelTestDir(t *testing.T) func() {
	t.Helper()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get cwd: %v", err)
	}

	tempDir := t.TempDir()
	cfg := &config.Config{
		Repositories: []string{"test-owner/test-repo"},
	}
	configPath := filepath.Join(tempDir, ".gh-pmu.json")
	if err := cfg.Save(filepath.Dir(configPath)); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	return func() {
		_ = os.Chdir(originalDir)
	}
}

func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	return cmd
}

// --- Sync Tests ---

func TestRunLabelSync_CreatesMissing(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	mock := newMockLabelClient()
	// No labels exist yet

	defs := []defaults.LabelDef{
		{Name: "bug", Color: "d73a4a", Description: "Something isn't working"},
		{Name: "enhancement", Color: "1d76db", Description: "New feature or request"},
	}

	var buf bytes.Buffer
	cmd := newTestCmd()
	cmd.SetOut(&buf)

	opts := &labelSyncOptions{}
	err := SyncLabels(&buf, ui.New(&buf), mock, "owner", "repo", defs, false, false)
	_ = opts // used for type reference only

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.createCalls) != 2 {
		t.Errorf("expected 2 create calls, got %d", len(mock.createCalls))
	}
	if mock.createCalls[0].name != "bug" {
		t.Errorf("expected first create to be 'bug', got %q", mock.createCalls[0].name)
	}
	if mock.createCalls[1].color != "1d76db" {
		t.Errorf("expected enhancement color '1d76db', got %q", mock.createCalls[1].color)
	}
}

// TestRunLabelSync_CreatesAutoFiledFromDefaults (#915): syncing the embedded
// defaults into a repository without the label creates auto-filed.
func TestRunLabelSync_CreatesAutoFiledFromDefaults(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	defs, err := defaults.Load()
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	mock := newMockLabelClient()

	var buf bytes.Buffer
	if err := SyncLabels(&buf, ui.New(&buf), mock, "owner", "repo", defs.Labels, false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var created *createLabelCall
	for i := range mock.createCalls {
		if mock.createCalls[i].name == "auto-filed" {
			created = &mock.createCalls[i]
		}
	}
	if created == nil {
		t.Fatalf("expected CreateLabel for auto-filed, got %+v", mock.createCalls)
	}
	if created.color != "116329" || created.description != "Issue filed by the hall-monitor" {
		t.Errorf("auto-filed created as %+v, want color 116329 and the hall-monitor description", *created)
	}
}

func TestRunLabelSync_SkipsExisting(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	mock := newMockLabelClient()
	mock.labels["bug"] = &api.RepoLabel{Name: "bug", Color: "d73a4a", Description: "Something isn't working"}

	defs := []defaults.LabelDef{
		{Name: "bug", Color: "d73a4a", Description: "Something isn't working"},
	}

	var buf bytes.Buffer
	err := SyncLabels(&buf, ui.New(&buf), mock, "owner", "repo", defs, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.createCalls) != 0 {
		t.Errorf("expected 0 create calls for existing label, got %d", len(mock.createCalls))
	}
}

func TestRunLabelSync_DryRun(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	mock := newMockLabelClient()

	defs := []defaults.LabelDef{
		{Name: "bug", Color: "d73a4a", Description: "Something isn't working"},
	}

	var buf bytes.Buffer
	err := SyncLabels(&buf, ui.New(&buf), mock, "owner", "repo", defs, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should NOT have created anything
	if len(mock.createCalls) != 0 {
		t.Errorf("dry-run should not create labels, got %d create calls", len(mock.createCalls))
	}
	output := buf.String()
	if !strings.Contains(output, "would create") {
		t.Errorf("dry-run output should contain 'would create', got: %s", output)
	}
}

func TestRunLabelSync_UpdateMode(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	mock := newMockLabelClient()
	mock.labels["bug"] = &api.RepoLabel{Name: "bug", Color: "old123", Description: "Old description"}

	defs := []defaults.LabelDef{
		{Name: "bug", Color: "d73a4a", Description: "Something isn't working"},
	}

	var buf bytes.Buffer
	err := SyncLabels(&buf, ui.New(&buf), mock, "owner", "repo", defs, false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.updateCalls) != 1 {
		t.Fatalf("expected 1 update call, got %d", len(mock.updateCalls))
	}
	if mock.updateCalls[0].color != "d73a4a" {
		t.Errorf("expected updated color 'd73a4a', got %q", mock.updateCalls[0].color)
	}
}

func TestRunLabelSync_UpdateDryRun(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	mock := newMockLabelClient()
	mock.labels["bug"] = &api.RepoLabel{Name: "bug", Color: "old123", Description: "Old"}

	defs := []defaults.LabelDef{
		{Name: "bug", Color: "d73a4a", Description: "Something isn't working"},
	}

	var buf bytes.Buffer
	err := SyncLabels(&buf, ui.New(&buf), mock, "owner", "repo", defs, true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.updateCalls) != 0 {
		t.Errorf("dry-run should not update, got %d update calls", len(mock.updateCalls))
	}
	output := buf.String()
	if !strings.Contains(output, "would update") {
		t.Errorf("dry-run output should contain 'would update', got: %s", output)
	}
}

// --- List Tests ---

func TestRunLabelList_ShowsLabels(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	mock := newMockLabelClient()
	mock.allLabels = []api.RepoLabel{
		{Name: "bug", Color: "d73a4a", Description: "Something isn't working"},
		{Name: "custom-label", Color: "ff0000", Description: "A custom label"},
	}

	cmd := newTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &labelListOptions{}
	err := runLabelListWithDeps(cmd, opts, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "bug") {
		t.Errorf("output should contain 'bug', got: %s", output)
	}
	if !strings.Contains(output, "standard") {
		t.Errorf("output should contain 'standard' indicator, got: %s", output)
	}
	if !strings.Contains(output, "custom") {
		t.Errorf("output should contain 'custom' indicator, got: %s", output)
	}
}

func TestRunLabelList_Empty(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	mock := newMockLabelClient()
	mock.allLabels = []api.RepoLabel{}

	cmd := newTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &labelListOptions{}
	err := runLabelListWithDeps(cmd, opts, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No labels found") {
		t.Errorf("empty list should say 'No labels found', got: %s", output)
	}
}

// --- Add Tests ---

func TestRunLabelAdd_CreatesLabel(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	mock := newMockLabelClient()

	cmd := newTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &labelAddOptions{color: "ff0000", description: "Test label"}
	err := runLabelAddWithDeps(cmd, []string{"my-label"}, opts, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.createCalls) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(mock.createCalls))
	}
	if mock.createCalls[0].name != "my-label" {
		t.Errorf("expected name 'my-label', got %q", mock.createCalls[0].name)
	}
	if mock.createCalls[0].color != "ff0000" {
		t.Errorf("expected color 'ff0000', got %q", mock.createCalls[0].color)
	}
}

func TestRunLabelAdd_StripsHashPrefix(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	mock := newMockLabelClient()

	cmd := newTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &labelAddOptions{color: "#ff0000", description: "Test"}
	err := runLabelAddWithDeps(cmd, []string{"test"}, opts, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.createCalls[0].color != "ff0000" {
		t.Errorf("expected # to be stripped, got color %q", mock.createCalls[0].color)
	}
}

func TestRunLabelAdd_CreateError(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	mock := newMockLabelClient()
	mock.createErr = fmt.Errorf("API error")

	cmd := newTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &labelAddOptions{color: "ff0000"}
	err := runLabelAddWithDeps(cmd, []string{"test"}, opts, mock)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "API error") {
		t.Errorf("expected API error in message, got: %s", err.Error())
	}
}

// --- Update Tests ---

func TestRunLabelUpdate_UpdatesLabel(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	mock := newMockLabelClient()
	mock.labels["my-label"] = &api.RepoLabel{Name: "my-label", Color: "ff0000", Description: "Old"}

	cmd := newTestCmd()
	cmd.Flags().String("color", "", "")
	cmd.Flags().String("description", "", "")
	_ = cmd.Flags().Set("color", "00ff00")
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &labelUpdateOptions{color: "00ff00"}
	err := runLabelUpdateWithDeps(cmd, []string{"my-label"}, opts, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.updateCalls) != 1 {
		t.Fatalf("expected 1 update call, got %d", len(mock.updateCalls))
	}
	if mock.updateCalls[0].color != "00ff00" {
		t.Errorf("expected color '00ff00', got %q", mock.updateCalls[0].color)
	}
	// Description should be preserved from existing
	if mock.updateCalls[0].description != "Old" {
		t.Errorf("expected preserved description 'Old', got %q", mock.updateCalls[0].description)
	}
}

func TestRunLabelUpdate_NotFound(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	mock := newMockLabelClient()

	cmd := newTestCmd()
	cmd.Flags().String("color", "", "")
	cmd.Flags().String("description", "", "")
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &labelUpdateOptions{color: "00ff00"}
	err := runLabelUpdateWithDeps(cmd, []string{"nonexistent"}, opts, mock)
	if err == nil {
		t.Fatal("expected error for nonexistent label")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %s", err.Error())
	}
}

// --- Delete Tests ---

func TestRunLabelDelete_DeletesLabel(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	mock := newMockLabelClient()
	mock.labels["my-label"] = &api.RepoLabel{Name: "my-label"}

	cmd := newTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &labelDeleteOptions{}
	err := runLabelDeleteWithDeps(cmd, []string{"my-label"}, opts, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.deleteCalls) != 1 {
		t.Fatalf("expected 1 delete call, got %d", len(mock.deleteCalls))
	}
	if mock.deleteCalls[0] != "my-label" {
		t.Errorf("expected delete of 'my-label', got %q", mock.deleteCalls[0])
	}
}

func TestRunLabelDelete_Error(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	mock := newMockLabelClient()
	mock.deleteErr = fmt.Errorf("not found")

	cmd := newTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	opts := &labelDeleteOptions{}
	err := runLabelDeleteWithDeps(cmd, []string{"missing"}, opts, mock)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- ResolveRepo Tests ---

func TestResolveRepo_FromFlag(t *testing.T) {
	owner, repo, err := resolveRepo("my-org/my-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "my-org" || repo != "my-repo" {
		t.Errorf("expected my-org/my-repo, got %s/%s", owner, repo)
	}
}

func TestResolveRepo_InvalidFlag(t *testing.T) {
	_, _, err := resolveRepo("invalid")
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestResolveRepo_FromConfig(t *testing.T) {
	cleanup := setupLabelTestDir(t)
	defer cleanup()

	owner, repo, err := resolveRepo("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "test-owner" || repo != "test-repo" {
		t.Errorf("expected test-owner/test-repo, got %s/%s", owner, repo)
	}
}
