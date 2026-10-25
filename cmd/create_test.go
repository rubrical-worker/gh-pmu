package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
)

// mockCreateClient implements createClient for testing
type mockCreateClient struct {
	createdIssue  *api.Issue
	project       *api.Project
	projectFields []api.ProjectField
	itemID        string
	issuesByLabel []api.Issue

	// Capture for verification
	lastBody      string
	lastLabels    []string
	lastAssignees []string
	lastMilestone string
	setFieldCalls []createSetFieldCall
	createCalls   int

	getProjectCalls   int
	lastProjectOwner  string
	lastProjectNumber int

	// Error injection
	createIssueErr          error
	getProjectErr           error
	getProjectFieldsErr     error
	addIssueToProjectErr    error
	setProjectItemFieldErr  error
	getOpenIssuesByLabelErr error
}

func newMockCreateClient() *mockCreateClient {
	return &mockCreateClient{
		createdIssue: &api.Issue{
			ID:     "issue-1",
			Number: 42,
			Title:  "New Issue",
			URL:    "https://github.com/owner/repo/issues/42",
		},
		project: &api.Project{
			ID:    "proj-1",
			Title: "Test Project",
		},
		projectFields: []api.ProjectField{
			{ID: "STATUS_FIELD", Name: "Status", DataType: "SINGLE_SELECT"},
			{ID: "BRANCH_FIELD", Name: "Branch", DataType: "TEXT"},
		},
		itemID: "item-123",
	}
}

func (m *mockCreateClient) CreateIssueWithOptions(owner, repo, title, body string, labels, assignees []string, milestone string) (*api.Issue, error) {
	m.createCalls++
	if m.createIssueErr != nil {
		return nil, m.createIssueErr
	}
	m.lastBody = body
	m.lastLabels = labels
	m.lastAssignees = assignees
	m.lastMilestone = milestone
	issue := m.createdIssue
	issue.Title = title
	return issue, nil
}

func (m *mockCreateClient) GetProject(owner string, number int) (*api.Project, error) {
	m.getProjectCalls++
	m.lastProjectOwner, m.lastProjectNumber = owner, number
	if m.getProjectErr != nil {
		return nil, m.getProjectErr
	}
	return m.project, nil
}

func (m *mockCreateClient) GetProjectFields(projectID string) ([]api.ProjectField, error) {
	if m.getProjectFieldsErr != nil {
		return nil, m.getProjectFieldsErr
	}
	return m.projectFields, nil
}

func (m *mockCreateClient) AddIssueToProject(projectID, issueID string) (string, error) {
	if m.addIssueToProjectErr != nil {
		return "", m.addIssueToProjectErr
	}
	return m.itemID, nil
}

func (m *mockCreateClient) SetProjectItemField(projectID, itemID, fieldName, value string) error {
	m.setFieldCalls = append(m.setFieldCalls, createSetFieldCall{fieldName: fieldName, value: value})
	return m.setProjectItemFieldErr
}

type createSetFieldCall struct {
	fieldName string
	value     string
}

func (m *mockCreateClient) GetOpenIssuesByLabel(owner, repo, label string) ([]api.Issue, error) {
	if m.getOpenIssuesByLabelErr != nil {
		return nil, m.getOpenIssuesByLabelErr
	}
	return m.issuesByLabel, nil
}

// ============================================================================
// #924: --project override
// ============================================================================

func TestParseProjectRef(t *testing.T) {
	tests := []struct {
		in        string
		wantOwner string
		wantNum   int
		wantErr   bool
	}{
		{"other-org/7", "other-org", 7, false},
		{"12", "local-owner", 12, false},
		{" other-org/7 ", "other-org", 7, false},
		{"", "", 0, true},
		{"other-org/x", "", 0, true},
		{"other-org/0", "", 0, true},
		{"a/b/3", "", 0, true},
		{"/7", "", 0, true},
	}
	for _, tt := range tests {
		owner, num, err := parseProjectRef(tt.in, "local-owner")
		if (err != nil) != tt.wantErr {
			t.Errorf("parseProjectRef(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && (owner != tt.wantOwner || num != tt.wantNum) {
			t.Errorf("parseProjectRef(%q) = %q, %d; want %q, %d", tt.in, owner, num, tt.wantOwner, tt.wantNum)
		}
	}
}

func projectOverrideCfg() *config.Config {
	return &config.Config{
		Project:      config.Project{Owner: "test-owner", Number: 1},
		Repositories: []string{"owner/repo", "owner/second"},
	}
}

func TestRunCreateWithDeps_ProjectOverrideAddsToNamedProject(t *testing.T) {
	client := newMockCreateClient()
	cmd := NewRootCommand()
	cmd.SetOut(new(bytes.Buffer))

	err := runCreateWithDeps(cmd, &createOptions{title: "T", repo: "other-org/target", project: "other-org/7"}, projectOverrideCfg(), client, "other-org", "target")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if client.lastProjectOwner != "other-org" || client.lastProjectNumber != 7 {
		t.Errorf("project = %s #%d, want other-org #7", client.lastProjectOwner, client.lastProjectNumber)
	}
	if client.createCalls != 1 {
		t.Errorf("createCalls = %d, want 1", client.createCalls)
	}
}

func TestRunCreateWithDeps_UnconfiguredRepoWithoutProjectRefuses(t *testing.T) {
	client := newMockCreateClient()
	cmd := NewRootCommand()
	cmd.SetOut(new(bytes.Buffer))

	err := runCreateWithDeps(cmd, &createOptions{title: "T", repo: "other-org/target"}, projectOverrideCfg(), client, "other-org", "target")

	if err == nil || !strings.Contains(err.Error(), "--project") {
		t.Fatalf("Expected refusal naming --project, got: %v", err)
	}
	if client.createCalls != 0 || client.getProjectCalls != 0 {
		t.Errorf("refusal must create nothing and touch no project: createCalls=%d getProjectCalls=%d", client.createCalls, client.getProjectCalls)
	}
}

func TestRunCreateWithDeps_UnconfiguredRepoFromFileWithoutProjectRefuses(t *testing.T) {
	client := newMockCreateClient()
	cmd := NewRootCommand()
	cmd.SetOut(new(bytes.Buffer))
	opts := &createOptions{repo: "other-org/target", fromFile: "issue.yml", parsedFile: &issueFromFile{Title: "T"}}

	err := runCreateWithDeps(cmd, opts, projectOverrideCfg(), client, "other-org", "target")

	if err == nil || !strings.Contains(err.Error(), "--project") || client.createCalls != 0 {
		t.Fatalf("Expected --from-file to refuse the same way, got err=%v createCalls=%d", err, client.createCalls)
	}
}

func TestRunCreateWithDeps_ConfiguredRepoKeepsLocalProject(t *testing.T) {
	client := newMockCreateClient()
	cmd := NewRootCommand()
	cmd.SetOut(new(bytes.Buffer))

	err := runCreateWithDeps(cmd, &createOptions{title: "T", repo: "Owner/Second"}, projectOverrideCfg(), client, "Owner", "Second")

	if err != nil {
		t.Fatalf("Configured repository (any listed, case-insensitive) must not refuse: %v", err)
	}
	if client.lastProjectOwner != "test-owner" || client.lastProjectNumber != 1 {
		t.Errorf("project = %s #%d, want local test-owner #1", client.lastProjectOwner, client.lastProjectNumber)
	}
}

func TestRunCreateWithDeps_InvalidProjectRefIsRejectedBeforeCreate(t *testing.T) {
	client := newMockCreateClient()
	cmd := NewRootCommand()
	cmd.SetOut(new(bytes.Buffer))

	err := runCreateWithDeps(cmd, &createOptions{title: "T", repo: "other-org/target", project: "other-org/nope"}, projectOverrideCfg(), client, "other-org", "target")

	if err == nil || client.createCalls != 0 {
		t.Fatalf("Expected invalid --project rejected before create, got err=%v createCalls=%d", err, client.createCalls)
	}
}

func TestRunCreateWithDeps_DryRunShowsOverrideProject(t *testing.T) {
	client := newMockCreateClient()
	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := runCreateWithDeps(cmd, &createOptions{title: "T", repo: "other-org/target", project: "other-org/7", dryRun: true}, projectOverrideCfg(), client, "other-org", "target")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "(other-org #7)") {
		t.Errorf("Expected dry run to name the override project, got: %s", buf.String())
	}
	if client.createCalls != 0 {
		t.Errorf("dry run created an issue")
	}
}

func overrideTargets(fields []api.ProjectField) *createTargets {
	return &createTargets{
		project:      &api.Project{ID: "proj-7", Title: "Other"},
		projectOwner: "other-org", projectNumber: 7, overridden: true,
		fields: fields,
	}
}

func TestSetCreateFields_OverrideResolvesAgainstSelectedProject(t *testing.T) {
	client := newMockCreateClient()
	fields := []api.ProjectField{
		{Name: "Status", DataType: "SINGLE_SELECT", Options: []api.FieldOption{{Name: "Backlog"}, {Name: "Done"}}},
		{Name: "Priority", DataType: "SINGLE_SELECT", Options: []api.FieldOption{{Name: "P0"}, {Name: "P1"}}},
	}

	failed, unset, err := setCreateFields(client, &config.Config{}, overrideTargets(fields), "item-1", "Backlog", "P9")

	if err != nil || len(failed) != 0 {
		t.Fatalf("an absent alias must not fail the creation: failed=%v err=%v", failed, err)
	}
	if len(client.setFieldCalls) != 1 || client.setFieldCalls[0].fieldName != "Status" || client.setFieldCalls[0].value != "Backlog" {
		t.Errorf("setFieldCalls = %+v, want only Status=Backlog", client.setFieldCalls)
	}
	if len(unset) != 1 || !strings.Contains(unset[0], "Priority") || !strings.Contains(unset[0], "P9") || !strings.Contains(unset[0], "other-org #7") {
		t.Errorf("unset = %v, want Priority P9 reported by name against other-org #7", unset)
	}
}

func TestSetCreateFields_OverrideMissingFieldReportedUnset(t *testing.T) {
	client := newMockCreateClient()
	fields := []api.ProjectField{{Name: "Status", DataType: "SINGLE_SELECT", Options: []api.FieldOption{{Name: "Backlog"}}}}

	failed, unset, err := setCreateFields(client, &config.Config{}, overrideTargets(fields), "item-1", "", "P1")

	if err != nil || len(failed) != 0 || len(client.setFieldCalls) != 0 {
		t.Fatalf("missing field must be skipped, not attempted: failed=%v err=%v calls=%+v", failed, err, client.setFieldCalls)
	}
	if len(unset) != 1 || !strings.Contains(unset[0], "Priority") {
		t.Errorf("unset = %v, want the missing Priority field named", unset)
	}
}

func TestSetCreateFields_ConfiguredProjectBehaviorUnchanged(t *testing.T) {
	// Without --project nothing is pre-checked: the write decides, as before.
	client := newMockCreateClient()
	targets := &createTargets{project: &api.Project{ID: "proj-1"}, projectOwner: "o", projectNumber: 1,
		fields: []api.ProjectField{{Name: "Priority", DataType: "SINGLE_SELECT", Options: []api.FieldOption{{Name: "P0"}}}}}

	_, unset, _ := setCreateFields(client, &config.Config{}, targets, "item-1", "", "P9")

	if len(unset) != 0 || len(client.setFieldCalls) != 1 {
		t.Errorf("configured project: unset=%v calls=%+v, want the write attempted and nothing pre-skipped", unset, client.setFieldCalls)
	}
}

func TestCreateCommand_Exists(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command should exist: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("create")) {
		t.Error("Expected help output to mention 'create'")
	}
}

func TestCreateCommand_HasTitleFlag(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	flag := createCmd.Flags().Lookup("title")
	if flag == nil {
		t.Fatal("Expected --title flag to exist")
	}
}

func TestCreateCommand_HasBodyFlag(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	flag := createCmd.Flags().Lookup("body")
	if flag == nil {
		t.Fatal("Expected --body flag to exist")
	}
}

func TestCreateCommand_HasStatusFlag(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	flag := createCmd.Flags().Lookup("status")
	if flag == nil {
		t.Fatal("Expected --status flag to exist")
	}
}

func TestCreateCommand_HasPriorityFlag(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	flag := createCmd.Flags().Lookup("priority")
	if flag == nil {
		t.Fatal("Expected --priority flag to exist")
	}
}

func TestCreateCommand_HasLabelFlag(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	flag := createCmd.Flags().Lookup("label")
	if flag == nil {
		t.Fatal("Expected --label flag to exist")
	}
}

func TestCreateCommand_HasAssigneeFlag(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	flag := createCmd.Flags().Lookup("assignee")
	if flag == nil {
		t.Fatal("Expected --assignee flag to exist")
	}
	if flag.Shorthand != "a" {
		t.Errorf("Expected shorthand 'a', got '%s'", flag.Shorthand)
	}
	if flag.Value.Type() != "stringArray" {
		t.Errorf("Expected --assignee to be stringArray, got %s", flag.Value.Type())
	}
}

func TestCreateCommand_HasMilestoneFlag(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	flag := createCmd.Flags().Lookup("milestone")
	if flag == nil {
		t.Fatal("Expected --milestone flag to exist")
	}
	if flag.Shorthand != "m" {
		t.Errorf("Expected shorthand 'm', got '%s'", flag.Shorthand)
	}
}

func TestCreateCommand_HasRepoFlag(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	flag := createCmd.Flags().Lookup("repo")
	if flag == nil {
		t.Fatal("Expected --repo flag to exist")
	}
	if flag.Shorthand != "R" {
		t.Errorf("Expected shorthand 'R', got '%s'", flag.Shorthand)
	}
}

func TestCreateCommand_HasFromFileFlag(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	flag := createCmd.Flags().Lookup("from-file")
	if flag == nil {
		t.Fatal("Expected --from-file flag to exist")
	}
	if flag.Shorthand != "f" {
		t.Errorf("Expected shorthand 'f', got '%s'", flag.Shorthand)
	}
}

// =============================================================================
// REQ-006: Integration with Create Command
// =============================================================================

func TestCreateCommand_RequiresTitleInNonInteractiveMode(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create", "--body", "test body"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Error("Expected error when title not provided with --body")
	}
}

// ============================================================================
// Label Merging Logic Tests
//
// These exercise the real merge in runCreateWithDeps (create.go:256-257) and
// assert on the labels the mock client actually received, so a regression in
// the merge — dropped config defaults, dropped CLI labels, or wrong order —
// fails the test.
// ============================================================================

// runCreateForLabels invokes the real create path with the given config default
// labels and CLI labels, returning the labels that reached the API client.
func runCreateForLabels(t *testing.T, configLabels, cliLabels []string) []string {
	t.Helper()

	client := newMockCreateClient()
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-owner", Number: 1},
		Repositories: []string{"owner/repo"},
		Defaults:     config.Defaults{Labels: configLabels},
	}
	opts := &createOptions{title: "Test Issue", labels: cliLabels}

	cmd := NewRootCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	if err := runCreateWithDeps(cmd, opts, cfg, client, "owner", "repo"); err != nil {
		t.Fatalf("runCreateWithDeps failed: %v", err)
	}
	return client.lastLabels
}

func TestLabelMerging_EmptyDefaults(t *testing.T) {
	labels := runCreateForLabels(t, []string{}, []string{"bug", "urgent"})

	if len(labels) != 2 {
		t.Fatalf("Expected 2 labels, got %d: %v", len(labels), labels)
	}
	if labels[0] != "bug" || labels[1] != "urgent" {
		t.Errorf("Expected [bug urgent], got %v", labels)
	}
}

func TestLabelMerging_WithDefaults(t *testing.T) {
	labels := runCreateForLabels(t, []string{"enhancement"}, []string{"bug", "urgent"})

	if len(labels) != 3 {
		t.Fatalf("Expected 3 labels, got %d: %v", len(labels), labels)
	}
	// Config defaults must precede CLI labels.
	if labels[0] != "enhancement" || labels[1] != "bug" || labels[2] != "urgent" {
		t.Errorf("Expected [enhancement bug urgent], got %v", labels)
	}
}

func TestLabelMerging_NoCLILabels(t *testing.T) {
	labels := runCreateForLabels(t, []string{"enhancement", "auto-created"}, nil)

	if len(labels) != 2 {
		t.Fatalf("Expected 2 labels, got %d: %v", len(labels), labels)
	}
	if labels[0] != "enhancement" || labels[1] != "auto-created" {
		t.Errorf("Expected [enhancement auto-created], got %v", labels)
	}
}

func TestLabelMerging_BothEmpty(t *testing.T) {
	labels := runCreateForLabels(t, []string{}, nil)

	if len(labels) != 0 {
		t.Errorf("Expected 0 labels, got %d: %v", len(labels), labels)
	}
}

func TestLabelMerging_PassesAssigneesAndMilestone(t *testing.T) {
	client := newMockCreateClient()
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-owner", Number: 1},
		Repositories: []string{"owner/repo"},
		Defaults:     config.Defaults{Labels: []string{"enhancement"}},
	}
	opts := &createOptions{
		title:     "Test Issue",
		labels:    []string{"bug"},
		assignees: []string{"octocat"},
		milestone: "v1.0.0",
	}

	cmd := NewRootCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	if err := runCreateWithDeps(cmd, opts, cfg, client, "owner", "repo"); err != nil {
		t.Fatalf("runCreateWithDeps failed: %v", err)
	}

	if len(client.lastAssignees) != 1 || client.lastAssignees[0] != "octocat" {
		t.Errorf("Expected assignees [octocat], got %v", client.lastAssignees)
	}
	if client.lastMilestone != "v1.0.0" {
		t.Errorf("Expected milestone 'v1.0.0', got %q", client.lastMilestone)
	}
}

func TestCreateFromFile_MergesAssigneesForResolution(t *testing.T) {
	// #895 AC11: assignees named in a --from-file payload must reach the same
	// resolution path as flag-supplied ones. They are concatenated before the
	// create call, so both sets land in one slice that CreateIssueWithOptions
	// resolves — file entries first, flags after.
	dir := t.TempDir()
	path := filepath.Join(dir, "issue.yml")
	body := "title: From File\nbody: filled in\nassignees:\n  - hubot\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	client := newMockCreateClient()
	cfg := &config.Config{
		Project:      config.Project{Owner: "test-owner", Number: 1},
		Repositories: []string{"owner/repo"},
	}
	opts := &createOptions{
		fromFile:  path,
		assignees: []string{"@me"},
	}

	cmd := NewRootCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	if err := runCreateFromFileWithDeps(cmd, opts, cfg, client, "owner", "repo"); err != nil {
		t.Fatalf("runCreateFromFileWithDeps failed: %v", err)
	}

	want := []string{"hubot", "@me"}
	if !reflect.DeepEqual(client.lastAssignees, want) {
		t.Errorf("expected %v to reach the resolver, got %v", want, client.lastAssignees)
	}
}

// ============================================================================
// Error Message Tests
// ============================================================================

func TestCreateCommand_TitleRequiredErrorMessage(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error when no title provided")
	}

	// The error should mention title is required
	errStr := err.Error()
	if !strings.Contains(errStr, "title") && !strings.Contains(errStr, "configuration") {
		t.Errorf("Expected error about title or config, got: %v", err)
	}
}

func TestCreateCommand_FlagShortcuts(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	tests := []struct {
		longFlag  string
		shortFlag string
	}{
		{"title", "t"},
		{"body", "b"},
		{"status", "s"},
		{"priority", "p"},
		{"label", "l"},
	}

	for _, tt := range tests {
		t.Run(tt.longFlag, func(t *testing.T) {
			flag := createCmd.Flags().Lookup(tt.longFlag)
			if flag == nil {
				t.Fatalf("Flag --%s not found", tt.longFlag)
			}
			if flag.Shorthand != tt.shortFlag {
				t.Errorf("Expected shorthand -%s for --%s, got -%s", tt.shortFlag, tt.longFlag, flag.Shorthand)
			}
		})
	}
}

func TestCreateCommand_LabelFlagIsArray(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	flag := createCmd.Flags().Lookup("label")
	if flag == nil {
		t.Fatal("Expected --label flag to exist")
	}

	// Check that it's a stringArray type (can be specified multiple times)
	if flag.Value.Type() != "stringArray" {
		t.Errorf("Expected --label to be stringArray, got %s", flag.Value.Type())
	}
}

// ============================================================================
// runCreate Integration Tests (with temp config files)
// ============================================================================

// createTempConfig creates a temporary directory with a .gh-pmu.json config file
// and returns the directory path. Caller should defer os.RemoveAll(dir).
func createTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write temp config: %v", err)
	}
	return dir
}

func TestRunCreate_NoConfigFile_ReturnsError(t *testing.T) {
	// ARRANGE: Empty temp directory (no config)
	dir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create", "--title", "Test Issue"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT
	if err == nil {
		t.Fatal("Expected error when no config file exists")
	}
	if !strings.Contains(err.Error(), "configuration") {
		t.Errorf("Expected error about configuration, got: %v", err)
	}
}

func TestRunCreate_InvalidConfig_ReturnsError(t *testing.T) {
	// ARRANGE: Config missing required fields
	config := `{"project":{"owner":"","number":0},"repositories":[]}`
	dir := createTempConfig(t, config)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create", "--title", "Test Issue"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT
	if err == nil {
		t.Fatal("Expected error for invalid config")
	}
	if !strings.Contains(err.Error(), "invalid configuration") {
		t.Errorf("Expected 'invalid configuration' error, got: %v", err)
	}
}

func TestRunCreate_NoRepositories_ReturnsError(t *testing.T) {
	// ARRANGE: Config with no repositories
	config := `{"project":{"owner":"test-owner","number":1},"repositories":[]}`
	dir := createTempConfig(t, config)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create", "--title", "Test Issue"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT
	if err == nil {
		t.Fatal("Expected error for missing repositories")
	}
	// Either "invalid configuration" (validation) or "no repository" error
	errStr := err.Error()
	if !strings.Contains(errStr, "repository") && !strings.Contains(errStr, "configuration") {
		t.Errorf("Expected error about repositories, got: %v", err)
	}
}

func TestRunCreate_InvalidRepositoryFormat_ReturnsError(t *testing.T) {
	// ARRANGE: Config with invalid repository format (missing slash)
	config := `{"project":{"owner":"test-owner","number":1},"repositories":["invalid-repo-no-slash"]}`
	dir := createTempConfig(t, config)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create", "--title", "Test Issue"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT
	if err == nil {
		t.Fatal("Expected error for invalid repository format")
	}
	if !strings.Contains(err.Error(), "invalid repository format") {
		t.Errorf("Expected 'invalid repository format' error, got: %v", err)
	}
}

func TestRunCreate_NoTitle_ReturnsError(t *testing.T) {
	// ARRANGE: Valid config but no title provided
	config := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"]}`
	dir := createTempConfig(t, config)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create"}) // No --title

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT
	if err == nil {
		t.Fatal("Expected error when no title provided")
	}
	if !strings.Contains(err.Error(), "--title is required") {
		t.Errorf("Expected '--title is required' error, got: %v", err)
	}
}

func TestRunCreate_ValidConfigWithTitle_AttemptsAPICall(t *testing.T) {
	// ARRANGE: Valid config with title provided
	// This test verifies that we get past config validation and into API calls
	config := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"]}`
	dir := createTempConfig(t, config)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create", "--title", "Test Issue", "--body", "Test body"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT: We expect an API error (since we're not authenticated in tests)
	// The key thing is we got PAST the config validation phase
	if err == nil {
		t.Skip("Skipping: API call succeeded (authenticated environment)")
	}

	// Should be an API-related error, not a config error
	errStr := err.Error()
	if strings.Contains(errStr, "configuration") || strings.Contains(errStr, "--title is required") {
		t.Errorf("Expected API error after passing config validation, got: %v", err)
	}
}

func TestRunCreate_WithAllFlags_ParsesFlagsCorrectly(t *testing.T) {
	// ARRANGE: Valid config with all flags
	config := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"],"fields":{"status":{"field":"Status","values":{"todo":"Todo","in_progress":"In Progress"}},"priority":{"field":"Priority","values":{"p1":"P1","p2":"P2"}}},"defaults":{"labels":["auto-label"]}}`
	dir := createTempConfig(t, config)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{
		"create",
		"--title", "Test Issue",
		"--body", "Test body",
		"--status", "in_progress",
		"--priority", "p1",
		"--label", "bug",
		"--label", "urgent",
	})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT: We should get past flag parsing to API calls
	if err == nil {
		t.Skip("Skipping: API call succeeded (authenticated environment)")
	}

	// Verify we didn't get a flag parsing error
	errStr := err.Error()
	if strings.Contains(errStr, "unknown flag") || strings.Contains(errStr, "flag needs") {
		t.Errorf("Expected to pass flag parsing, got: %v", err)
	}
}

func TestRunCreate_ConfigWithDefaults_MergesLabels(t *testing.T) {
	// ARRANGE: Config with default labels
	config := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"],"defaults":{"labels":["enhancement","auto-created"],"status":"todo","priority":"p2"}}`
	dir := createTempConfig(t, config)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create", "--title", "Test Issue"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT: Should reach API call phase (past config loading and defaults)
	if err == nil {
		t.Skip("Skipping: API call succeeded (authenticated environment)")
	}

	// Verify we got past config validation
	errStr := err.Error()
	if strings.Contains(errStr, "configuration") {
		t.Errorf("Expected to pass config validation with defaults, got: %v", err)
	}
}

// ============================================================================
// Issue #325: Additional gh issue create options
// ============================================================================

func TestCreateCommand_HasBodyFileFlag(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	flag := createCmd.Flags().Lookup("body-file")
	if flag == nil {
		t.Fatal("Expected --body-file flag to exist")
	}
	if flag.Shorthand != "F" {
		t.Errorf("Expected shorthand 'F', got '%s'", flag.Shorthand)
	}
}

func TestCreateCommand_EditorFlagRemoved(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	flag := createCmd.Flags().Lookup("editor")
	if flag != nil {
		t.Error("--editor flag should have been removed")
	}
}

func TestCreateCommand_HasTemplateFlag(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	flag := createCmd.Flags().Lookup("template")
	if flag == nil {
		t.Fatal("Expected --template flag to exist")
	}
	if flag.Shorthand != "T" {
		t.Errorf("Expected shorthand 'T', got '%s'", flag.Shorthand)
	}
}

// ============================================================================
// readBodyFile Tests
// ============================================================================

func TestReadBodyFile_ReadsFileContent(t *testing.T) {
	// Create a temp file with content
	tmpfile, err := os.CreateTemp("", "body-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	content := "This is the issue body.\n\nWith multiple lines."
	if _, err := tmpfile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpfile.Close()

	// Read it back
	result, err := readBodyFile(tmpfile.Name())
	if err != nil {
		t.Fatalf("readBodyFile failed: %v", err)
	}

	if result != content {
		t.Errorf("Expected %q, got %q", content, result)
	}
}

func TestReadBodyFile_FileNotFound(t *testing.T) {
	_, err := readBodyFile("/nonexistent/path/to/file.md")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

// ============================================================================
// extractTemplateBody Tests
// ============================================================================

func TestExtractTemplateBody_NoFrontmatter(t *testing.T) {
	content := "This is the template body.\n\nWith multiple lines."
	result := extractTemplateBody(content)
	if result != content {
		t.Errorf("Expected %q, got %q", content, result)
	}
}

func TestExtractTemplateBody_WithYAMLFrontmatter(t *testing.T) {
	content := `---
name: Bug Report
about: Create a report to help us improve
---

## Description
Describe the bug here.

## Steps to Reproduce
1. Step 1
2. Step 2`

	expected := `## Description
Describe the bug here.

## Steps to Reproduce
1. Step 1
2. Step 2`

	result := extractTemplateBody(content)
	if result != expected {
		t.Errorf("Expected:\n%q\n\nGot:\n%q", expected, result)
	}
}

func TestExtractTemplateBody_EmptyContent(t *testing.T) {
	result := extractTemplateBody("")
	if result != "" {
		t.Errorf("Expected empty string, got %q", result)
	}
}

func TestExtractTemplateBody_OnlyFrontmatter(t *testing.T) {
	content := `---
name: Bug Report
about: Create a report
---`

	result := extractTemplateBody(content)
	if result != "" {
		t.Errorf("Expected empty string, got %q", result)
	}
}

// ============================================================================
// Mutual Exclusivity Tests
// ============================================================================

func TestRunCreate_BodyAndBodyFileMutuallyExclusive(t *testing.T) {
	// ARRANGE: Valid config
	config := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"]}`
	dir := createTempConfig(t, config)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create a temp body file
	tmpfile, err := os.CreateTemp(dir, "body-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	if _, err := tmpfile.WriteString("Body from file"); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpfile.Close()
	defer os.Remove(tmpfile.Name())

	cmd := NewRootCommand()
	cmd.SetArgs([]string{
		"create",
		"--title", "Test Issue",
		"--body", "Body from flag",
		"--body-file", tmpfile.Name(),
	})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err = cmd.Execute()

	// ASSERT
	if err == nil {
		t.Fatal("Expected error when using --body and --body-file together")
	}
	if !strings.Contains(err.Error(), "cannot use --body and --body-file together") {
		t.Errorf("Expected mutual exclusivity error, got: %v", err)
	}
}

func TestRunCreate_TemplateWithBodyFlagError(t *testing.T) {
	// ARRANGE: Valid config
	config := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"]}`
	dir := createTempConfig(t, config)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{
		"create",
		"--title", "Test Issue",
		"--body", "Body from flag",
		"--template", "bug",
	})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT
	if err == nil {
		t.Fatal("Expected error when using --template with --body")
	}
	if !strings.Contains(err.Error(), "cannot use --template with --body") {
		t.Errorf("Expected mutual exclusivity error, got: %v", err)
	}
}

func TestRunCreate_BodyFileReadsContent(t *testing.T) {
	// ARRANGE: Valid config with body file
	config := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"]}`
	dir := createTempConfig(t, config)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create a temp body file
	tmpfile, err := os.CreateTemp(dir, "body-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	if _, err := tmpfile.WriteString("Body content from file"); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpfile.Close()
	defer os.Remove(tmpfile.Name())

	cmd := NewRootCommand()
	cmd.SetArgs([]string{
		"create",
		"--title", "Test Issue",
		"--body-file", tmpfile.Name(),
	})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err = cmd.Execute()

	// ASSERT: Should reach API call phase (past body file reading)
	if err == nil {
		t.Skip("Skipping: API call succeeded (authenticated environment)")
	}

	// Verify we didn't get a body file reading error
	errStr := err.Error()
	if strings.Contains(errStr, "failed to read body file") {
		t.Errorf("Expected to successfully read body file, got: %v", err)
	}
}

// ============================================================================
// REQ-370: Create Validation Tests
// ============================================================================

func TestValidateCreateOptions_ReadyWithoutRelease(t *testing.T) {
	err := validateCreateOptions("Ready", "Issue body content", "")
	if err == nil {
		t.Fatal("Expected error for Ready status without release")
	}

	if !strings.Contains(err.Error(), "--branch") {
		t.Errorf("Expected error to mention --branch flag, got: %v", err)
	}
}

func TestValidateCreateOptions_InProgressWithoutRelease(t *testing.T) {
	err := validateCreateOptions("In Progress", "Issue body content", "")
	if err == nil {
		t.Fatal("Expected error for In Progress status without release")
	}
}

func TestValidateCreateOptions_ReadyWithRelease(t *testing.T) {
	err := validateCreateOptions("Ready", "Issue body content", "v1.0.0")
	if err != nil {
		t.Errorf("Expected no error for Ready with release, got: %v", err)
	}
}

func TestValidateCreateOptions_InReviewWithoutBody(t *testing.T) {
	err := validateCreateOptions("In Review", "", "")
	if err == nil {
		t.Fatal("Expected error for In Review without body")
	}

	if !strings.Contains(err.Error(), "without body") {
		t.Errorf("Expected error about body content, got: %v", err)
	}
}

func TestValidateCreateOptions_DoneWithoutBody(t *testing.T) {
	err := validateCreateOptions("Done", "", "")
	if err == nil {
		t.Fatal("Expected error for Done without body")
	}
}

func TestValidateCreateOptions_DoneWithBodyWithoutBranch(t *testing.T) {
	err := validateCreateOptions("Done", "Issue completed", "")
	if err == nil {
		t.Fatal("Expected error for Done without branch")
	}

	if !strings.Contains(err.Error(), "--branch") {
		t.Errorf("Expected error about branch, got: %v", err)
	}
}

func TestValidateCreateOptions_DoneWithBodyWithBranch(t *testing.T) {
	err := validateCreateOptions("Done", "Issue completed", "v1.0.0")
	if err != nil {
		t.Errorf("Expected no error for Done with body and branch, got: %v", err)
	}
}

func TestValidateCreateOptions_DoneWithUncheckedBoxes(t *testing.T) {
	body := `Task list:
- [x] First task
- [ ] Unchecked task`

	err := validateCreateOptions("Done", body, "")
	if err == nil {
		t.Fatal("Expected error for Done with unchecked boxes")
	}

	if !strings.Contains(err.Error(), "unchecked") {
		t.Errorf("Expected error about unchecked boxes, got: %v", err)
	}
}

func TestValidateCreateOptions_DoneWithAllChecked(t *testing.T) {
	body := `Task list:
- [x] First task
- [x] Second task`

	err := validateCreateOptions("Done", body, "v1.0.0")
	if err != nil {
		t.Errorf("Expected no error for Done with all checked and branch, got: %v", err)
	}
}

func TestValidateCreateOptions_InReviewWithoutBranch(t *testing.T) {
	err := validateCreateOptions("In Review", "Issue body", "")
	if err == nil {
		t.Fatal("Expected error for In Review without branch")
	}

	if !strings.Contains(err.Error(), "--branch") {
		t.Errorf("Expected error about branch, got: %v", err)
	}
}

func TestValidateCreateOptions_InReviewWithBranch(t *testing.T) {
	err := validateCreateOptions("In Review", "Issue body", "v1.0.0")
	if err != nil {
		t.Errorf("Expected no error for In Review with body and branch, got: %v", err)
	}
}

func TestValidateCreateOptions_BacklogNoRequirements(t *testing.T) {
	// Backlog has no validation requirements
	err := validateCreateOptions("Backlog", "", "")
	if err != nil {
		t.Errorf("Expected no error for Backlog, got: %v", err)
	}
}

func TestValidateCreateOptions_CaseInsensitive(t *testing.T) {
	// Test various case combinations
	err := validateCreateOptions("READY", "body", "")
	if err == nil {
		t.Error("Expected error for READY without release")
	}

	err = validateCreateOptions("ready", "body", "v1.0")
	if err != nil {
		t.Errorf("Expected no error for ready with release, got: %v", err)
	}

	err = validateCreateOptions("in_progress", "body", "")
	if err == nil {
		t.Error("Expected error for in_progress without release")
	}
}

// ============================================================================
// findActiveBranchForCreate Tests
// ============================================================================

func TestFindActiveReleaseForCreate_FindsActiveRelease(t *testing.T) {
	issues := []api.Issue{
		{Number: 1, Title: "Bug: Something"},
		{Number: 2, Title: "Release: v1.0.0"},
		{Number: 3, Title: "Feature: New thing"},
	}

	result := findActiveBranchForCreate(issues)
	if result == nil {
		t.Fatal("Expected to find active release")
	}
	if result.Number != 2 {
		t.Errorf("Expected issue #2, got #%d", result.Number)
	}
}

func TestFindActiveReleaseForCreate_NoActiveRelease(t *testing.T) {
	issues := []api.Issue{
		{Number: 1, Title: "Bug: Something"},
		{Number: 2, Title: "Feature: New thing"},
	}

	result := findActiveBranchForCreate(issues)
	if result != nil {
		t.Errorf("Expected nil for no active release, got #%d", result.Number)
	}
}

func TestFindActiveReleaseForCreate_EmptyIssues(t *testing.T) {
	var issues []api.Issue

	result := findActiveBranchForCreate(issues)
	if result != nil {
		t.Error("Expected nil for empty issues slice")
	}
}

func TestFindActiveReleaseForCreate_ReturnsFirstMatch(t *testing.T) {
	issues := []api.Issue{
		{Number: 1, Title: "Release: v1.0.0"},
		{Number: 2, Title: "Release: v2.0.0"},
	}

	result := findActiveBranchForCreate(issues)
	if result == nil {
		t.Fatal("Expected to find active release")
	}
	if result.Number != 1 {
		t.Errorf("Expected first match (issue #1), got #%d", result.Number)
	}
}

func TestFindActiveReleaseForCreate_ExactPrefixMatch(t *testing.T) {
	issues := []api.Issue{
		{Number: 1, Title: "ReleaseNotes: v1.0.0"}, // Not a release
		{Number: 2, Title: "Release: v2.0.0"},      // Correct prefix
	}

	result := findActiveBranchForCreate(issues)
	if result == nil {
		t.Fatal("Expected to find active release")
	}
	if result.Number != 2 {
		t.Errorf("Expected issue #2 with correct prefix, got #%d", result.Number)
	}
}

// ============================================================================
// stripBranchPrefix Tests
// ============================================================================

func TestStripBranchPrefix_BranchPrefix(t *testing.T) {
	result := stripBranchPrefix("Branch: v1.0.0")
	if result != "v1.0.0" {
		t.Errorf("Expected 'v1.0.0', got '%s'", result)
	}
}

func TestStripBranchPrefix_ReleasePrefix(t *testing.T) {
	result := stripBranchPrefix("Release: v1.0.0")
	if result != "v1.0.0" {
		t.Errorf("Expected 'v1.0.0', got '%s'", result)
	}
}

func TestStripBranchPrefix_DoublePrefix_OnlyStripsFirst(t *testing.T) {
	// Regression: "Branch: Release: something" must strip only "Branch: "
	result := stripBranchPrefix("Branch: Release: something")
	if result != "Release: something" {
		t.Errorf("Expected 'Release: something', got '%s'", result)
	}
}

func TestStripBranchPrefix_NoPrefix(t *testing.T) {
	result := stripBranchPrefix("Some other title")
	if result != "Some other title" {
		t.Errorf("Expected 'Some other title', got '%s'", result)
	}
}

// ============================================================================
// decodeBase64Content Tests
// ============================================================================

func TestDecodeBase64Content_ValidBase64(t *testing.T) {
	// "Hello, World!" encoded in base64
	encoded := "SGVsbG8sIFdvcmxkIQ=="
	result, err := decodeBase64Content(encoded)
	if err != nil {
		t.Fatalf("decodeBase64Content failed: %v", err)
	}
	if result != "Hello, World!" {
		t.Errorf("Expected 'Hello, World!', got %q", result)
	}
}

func TestDecodeBase64Content_WithNewlines(t *testing.T) {
	// GitHub API includes newlines in base64 content
	encoded := "SGVs\nbG8s\nIFdv\ncmxk\nIQ=="
	result, err := decodeBase64Content(encoded)
	if err != nil {
		t.Fatalf("decodeBase64Content failed: %v", err)
	}
	if result != "Hello, World!" {
		t.Errorf("Expected 'Hello, World!', got %q", result)
	}
}

func TestDecodeBase64Content_EmptyString(t *testing.T) {
	result, err := decodeBase64Content("")
	if err != nil {
		t.Fatalf("decodeBase64Content failed: %v", err)
	}
	if result != "" {
		t.Errorf("Expected empty string, got %q", result)
	}
}

func TestDecodeBase64Content_InvalidBase64(t *testing.T) {
	// Invalid base64 characters
	encoded := "This is not valid base64!!!"
	_, err := decodeBase64Content(encoded)
	if err == nil {
		t.Error("Expected error for invalid base64")
	}
}

func TestDecodeBase64Content_MultilineContent(t *testing.T) {
	// Multi-line markdown content
	content := "## Title\n\nParagraph text.\n\n- Item 1\n- Item 2"
	encoded := "IyMgVGl0bGUKClBhcmFncmFwaCB0ZXh0LgoKLSBJdGVtIDEKLSBJdGVtIDI="
	result, err := decodeBase64Content(encoded)
	if err != nil {
		t.Fatalf("decodeBase64Content failed: %v", err)
	}
	if result != content {
		t.Errorf("Expected:\n%q\n\nGot:\n%q", content, result)
	}
}

// ============================================================================
// runCreateFromFile Tests
// ============================================================================

func TestRunCreateFromFile_YAMLParsing(t *testing.T) {
	// ARRANGE: Create config
	configContent := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"]}`
	dir := createTempConfig(t, configContent)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create YAML issue file
	yamlContent := `
title: "Test Issue from YAML"
body: "This is the body from YAML file"
labels:
  - bug
  - urgent
status: "backlog"
priority: "p1"
`
	yamlFile := filepath.Join(dir, "issue.yaml")
	if err := os.WriteFile(yamlFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write YAML file: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create", "--from-file", yamlFile})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT: Should reach API call phase (past file parsing)
	if err == nil {
		t.Skip("Skipping: API call succeeded (authenticated environment)")
	}

	// Verify we didn't get a YAML parsing error
	errStr := err.Error()
	if strings.Contains(errStr, "failed to parse YAML") {
		t.Errorf("Expected to successfully parse YAML, got: %v", err)
	}
}

func TestRunCreateFromFile_JSONParsing(t *testing.T) {
	// ARRANGE: Create config
	configContent := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"]}`
	dir := createTempConfig(t, configContent)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create JSON issue file
	jsonContent := `{
  "title": "Test Issue from JSON",
  "body": "This is the body from JSON file",
  "labels": ["bug", "urgent"],
  "status": "backlog",
  "priority": "p1"
}`
	jsonFile := filepath.Join(dir, "issue.json")
	if err := os.WriteFile(jsonFile, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("Failed to write JSON file: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create", "--from-file", jsonFile})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT: Should reach API call phase (past file parsing)
	if err == nil {
		t.Skip("Skipping: API call succeeded (authenticated environment)")
	}

	// Verify we didn't get a JSON parsing error
	errStr := err.Error()
	if strings.Contains(errStr, "failed to parse JSON") {
		t.Errorf("Expected to successfully parse JSON, got: %v", err)
	}
}

func TestRunCreateFromFile_MissingTitleError(t *testing.T) {
	// ARRANGE: Create config
	configContent := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"]}`
	dir := createTempConfig(t, configContent)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create YAML issue file WITHOUT title
	yamlContent := `
body: "This is the body but no title"
labels:
  - bug
`
	yamlFile := filepath.Join(dir, "issue-no-title.yaml")
	if err := os.WriteFile(yamlFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write YAML file: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create", "--from-file", yamlFile})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT
	if err == nil {
		t.Fatal("Expected error for missing title in file")
	}
	if !strings.Contains(err.Error(), "title is required in file") {
		t.Errorf("Expected 'title is required in file' error, got: %v", err)
	}
}

func TestRunCreateFromFile_CLIFlagOverrides(t *testing.T) {
	// ARRANGE: Create config
	configContent := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"]}`
	dir := createTempConfig(t, configContent)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create YAML issue file with values
	yamlContent := `
title: "Title from file"
body: "Body from file"
status: "backlog"
priority: "p2"
`
	yamlFile := filepath.Join(dir, "issue.yaml")
	if err := os.WriteFile(yamlFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write YAML file: %v", err)
	}

	// Use CLI flags to override
	cmd := NewRootCommand()
	cmd.SetArgs([]string{
		"create",
		"--from-file", yamlFile,
		"--body", "Body from CLI override",
		"--status", "in_progress",
		"--priority", "p1",
	})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT: Should reach API call phase (past file parsing and merging)
	if err == nil {
		t.Skip("Skipping: API call succeeded (authenticated environment)")
	}

	// Verify we didn't get a parsing or merge error
	errStr := err.Error()
	if strings.Contains(errStr, "failed to parse") || strings.Contains(errStr, "title is required") {
		t.Errorf("Expected to pass parsing and merge phase, got: %v", err)
	}
}

func TestRunCreateFromFile_FileNotFound(t *testing.T) {
	// ARRANGE: Create config
	configContent := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"]}`
	dir := createTempConfig(t, configContent)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create", "--from-file", "nonexistent.yaml"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT
	if err == nil {
		t.Fatal("Expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "failed to read file") {
		t.Errorf("Expected 'failed to read file' error, got: %v", err)
	}
}

func TestRunCreateFromFile_InvalidYAML(t *testing.T) {
	// ARRANGE: Create config
	configContent := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"]}`
	dir := createTempConfig(t, configContent)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create invalid YAML file
	invalidYAML := `
title: "Test Issue"
body: this is invalid yaml because
  of: bad indentation
    that: doesn't work
`
	yamlFile := filepath.Join(dir, "invalid.yaml")
	if err := os.WriteFile(yamlFile, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("Failed to write YAML file: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create", "--from-file", yamlFile})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT
	if err == nil {
		t.Fatal("Expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "failed to parse YAML") {
		t.Errorf("Expected 'failed to parse YAML' error, got: %v", err)
	}
}

func TestRunCreateFromFile_InvalidJSON(t *testing.T) {
	// ARRANGE: Create config
	configContent := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"]}`
	dir := createTempConfig(t, configContent)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create invalid JSON file
	invalidJSON := `{
  "title": "Test Issue",
  "body": "Missing closing brace"
`
	jsonFile := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(jsonFile, []byte(invalidJSON), 0644); err != nil {
		t.Fatalf("Failed to write JSON file: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"create", "--from-file", jsonFile})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT
	if err == nil {
		t.Fatal("Expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse JSON") {
		t.Errorf("Expected 'failed to parse JSON' error, got: %v", err)
	}
}

func TestRunCreateFromFile_LabelsFromFileAndCLI(t *testing.T) {
	// ARRANGE: Create config with default labels
	configContent := `{"project":{"owner":"test-owner","number":1},"repositories":["owner/repo"],"defaults":{"labels":["default-label"]}}`
	dir := createTempConfig(t, configContent)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create YAML with labels
	yamlContent := `
title: "Test Issue"
labels:
  - "file-label"
`
	yamlFile := filepath.Join(dir, "issue.yaml")
	if err := os.WriteFile(yamlFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write YAML file: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{
		"create",
		"--from-file", yamlFile,
		"--label", "cli-label",
	})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// ACT
	err := cmd.Execute()

	// ASSERT: Should reach API call phase (labels should be merged)
	if err == nil {
		t.Skip("Skipping: API call succeeded (authenticated environment)")
	}

	// Verify we didn't get a parsing error - labels should be merged correctly
	errStr := err.Error()
	if strings.Contains(errStr, "failed to parse") || strings.Contains(errStr, "title is required") {
		t.Errorf("Expected to pass label merging phase, got: %v", err)
	}
}

// ============================================================================
// runCreateWithDeps Tests
// ============================================================================

func TestRunCreateWithDeps_Success(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newCreateCommand()

	opts := &createOptions{title: "Test Issue", body: "Test body"}
	err := runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Output goes to os.Stdout via fmt.Printf - verified by running successfully
}

func TestRunCreateWithDeps_NoTitle(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newCreateCommand()
	opts := &createOptions{body: "Test body"} // No title
	err := runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")

	if err == nil {
		t.Fatal("expected error for missing title")
	}
	if !strings.Contains(err.Error(), "--title is required") {
		t.Errorf("expected '--title is required' error, got: %v", err)
	}
}

func TestRunCreateWithDeps_CreateIssueError(t *testing.T) {
	mock := newMockCreateClient()
	mock.createIssueErr = errors.New("API error")
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newCreateCommand()
	opts := &createOptions{title: "Test Issue"}
	err := runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to create issue") {
		t.Errorf("expected 'failed to create issue' error, got: %v", err)
	}
}

func TestRunCreateWithDeps_GetProjectError(t *testing.T) {
	mock := newMockCreateClient()
	mock.getProjectErr = errors.New("project not found")
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newCreateCommand()
	opts := &createOptions{title: "Test Issue"}
	err := runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to get project") {
		t.Errorf("expected 'failed to get project' error, got: %v", err)
	}
}

// #929: every lookup create needs runs before the issue is created, on both the
// flag path and the --from-file path, so a failed lookup writes nothing.
func TestRunCreate_LookupFailureCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	issueFile := filepath.Join(dir, "issue.yml")
	if err := os.WriteFile(issueFile, []byte("title: From File\nbody: body\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	paths := map[string]func() *createOptions{
		"flag path":      func() *createOptions { return &createOptions{title: "Test Issue", release: "current"} },
		"from-file path": func() *createOptions { return &createOptions{fromFile: issueFile, release: "current"} },
	}
	failures := map[string]struct {
		inject  func(m *mockCreateClient)
		wantErr string
	}{
		"get project":        {func(m *mockCreateClient) { m.getProjectErr = errors.New("boom") }, "failed to get project"},
		"get project fields": {func(m *mockCreateClient) { m.getProjectFieldsErr = errors.New("boom") }, "failed to get project fields"},
		"branch lookup":      {func(m *mockCreateClient) { m.getOpenIssuesByLabelErr = errors.New("boom") }, "failed to get branch issues"},
		"no active branch":   {func(m *mockCreateClient) { m.issuesByLabel = nil }, "no active branch found"},
	}

	for pathName, newOpts := range paths {
		for failName, f := range failures {
			t.Run(pathName+"/"+failName, func(t *testing.T) {
				mock := newMockCreateClient()
				f.inject(mock)
				cfg := &config.Config{Project: config.Project{Owner: "test-org", Number: 1}}

				err := runCreateWithDeps(newCreateCommand(), newOpts(), cfg, mock, "owner", "repo")
				if err == nil || !strings.Contains(err.Error(), f.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", f.wantErr, err)
				}
				if mock.createCalls != 0 {
					t.Errorf("expected no issue to be created when the %s lookup fails; CreateIssueWithOptions called %d time(s)", failName, mock.createCalls)
				}
			})
		}
	}
}

// #929: once the issue exists, a later failure is a partial success — the error
// names the new issue, its URL and the failed step, and maps to exit 6 — so a
// caller does not retry and file a duplicate. Covers both creation paths.
func TestRunCreate_PostCreateFailureIsPartialSuccess(t *testing.T) {
	dir := t.TempDir()
	issueFile := filepath.Join(dir, "issue.yml")
	if err := os.WriteFile(issueFile, []byte("title: From File\nbody: body\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	paths := map[string]func() *createOptions{
		"flag path":      func() *createOptions { return &createOptions{title: "Test Issue", status: "todo", priority: "p1"} },
		"from-file path": func() *createOptions { return &createOptions{fromFile: issueFile, status: "todo", priority: "p1"} },
	}
	failures := map[string]struct {
		inject    func(m *mockCreateClient)
		wantSteps []string
	}{
		"add to project": {func(m *mockCreateClient) { m.addIssueToProjectErr = errors.New("add failed") }, []string{"add to project"}},
		"field updates":  {func(m *mockCreateClient) { m.setProjectItemFieldErr = errors.New("field failed") }, []string{"set status", "set priority"}},
	}

	for pathName, newOpts := range paths {
		for failName, f := range failures {
			t.Run(pathName+"/"+failName, func(t *testing.T) {
				mock := newMockCreateClient()
				f.inject(mock)
				cfg := &config.Config{Project: config.Project{Owner: "test-org", Number: 1}}

				err := runCreateWithDeps(newCreateCommand(), newOpts(), cfg, mock, "owner", "repo")
				if err == nil {
					t.Fatal("expected a partial-success error")
				}
				var partial *api.PartialSuccessError
				if !errors.As(err, &partial) {
					t.Fatalf("expected *api.PartialSuccessError (exit %d), got %T: %v", api.ExitCodePartialSuccess, err, err)
				}
				msg := err.Error()
				for _, want := range append([]string{"issue #42", "https://github.com/owner/repo/issues/42"}, f.wantSteps...) {
					if !strings.Contains(msg, want) {
						t.Errorf("expected %q in error, got: %s", want, msg)
					}
				}
			})
		}
	}
}

// #929: --dry-run resolves everything create would use and prints it, on both
// creation paths, without creating the issue or writing any field.
func TestRunCreate_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	issueFile := filepath.Join(dir, "issue.yml")
	if err := os.WriteFile(issueFile, []byte("title: From File\nbody: body\nlabels: [bug]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bodyFile := filepath.Join(dir, "body.md")
	if err := os.WriteFile(bodyFile, []byte("Body from file"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		opts *createOptions
		want []string
	}{
		"flag path": {
			opts: &createOptions{title: "Dry Issue", bodyFile: bodyFile, status: "todo", priority: "p1", release: "current", labels: []string{"enhancement"}, dryRun: true},
			want: []string{"Dry run", "owner/repo", "Test Project", "Dry Issue", "--body-file " + bodyFile, "Todo", "P1", "v2.0.0", "enhancement"},
		},
		"from-file path": {
			opts: &createOptions{fromFile: issueFile, dryRun: true},
			want: []string{"Dry run", "owner/repo", "Test Project", "From File", "--from-file " + issueFile, "bug"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mock := newMockCreateClient()
			mock.issuesByLabel = []api.Issue{{Number: 9, Title: "Branch: release/v2.0.0", Labels: []api.Label{{Name: "branch"}, {Name: "active"}}}}
			cfg := &config.Config{
				Project: config.Project{Owner: "test-org", Number: 1},
				Fields: map[string]config.Field{
					"status":   {Field: "Status", Values: map[string]string{"todo": "Todo"}},
					"priority": {Field: "Priority", Values: map[string]string{"p1": "P1"}},
				},
			}
			cmd := newCreateCommand()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)

			if err := runCreateWithDeps(cmd, tt.opts, cfg, mock, "owner", "repo"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mock.createCalls != 0 || len(mock.setFieldCalls) != 0 {
				t.Errorf("dry run wrote: %d create call(s), %d field write(s)", mock.createCalls, len(mock.setFieldCalls))
			}
			for _, w := range tt.want {
				if !strings.Contains(buf.String(), w) {
					t.Errorf("expected %q in dry-run output, got:\n%s", w, buf.String())
				}
			}
		})
	}
}

func TestCreateCommand_HasDryRunFlag(t *testing.T) {
	if newCreateCommand().Flags().Lookup("dry-run") == nil {
		t.Fatal("Expected create to define --dry-run")
	}
}

// #929: total failure — nothing was created — stays a plain error (exit 1).
func TestRunCreateWithDeps_CreateFailureIsNotPartialSuccess(t *testing.T) {
	mock := newMockCreateClient()
	mock.createIssueErr = errors.New("API error")
	cfg := &config.Config{Project: config.Project{Owner: "test-org", Number: 1}}

	err := runCreateWithDeps(newCreateCommand(), &createOptions{title: "Test Issue"}, cfg, mock, "owner", "repo")
	if err == nil || api.IsPartialSuccess(err) {
		t.Fatalf("expected a plain error when creation itself fails, got: %v", err)
	}
}

func TestRunCreateWithDeps_AddIssueToProjectError(t *testing.T) {
	mock := newMockCreateClient()
	mock.addIssueToProjectErr = errors.New("add failed")
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newCreateCommand()
	opts := &createOptions{title: "Test Issue"}
	err := runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to add issue to project") {
		t.Errorf("expected 'failed to add issue to project' error, got: %v", err)
	}
}

func TestRunCreateWithDeps_WithStatus(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		Fields: map[string]config.Field{
			"status": {
				Field:  "Status",
				Values: map[string]string{"todo": "Todo"},
			},
		},
	}

	cmd := newCreateCommand()
	opts := &createOptions{title: "Test Issue", status: "todo"}
	err := runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCreateWithDeps_WithPriority(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		Fields: map[string]config.Field{
			"priority": {
				Field:  "Priority",
				Values: map[string]string{"p1": "P1"},
			},
		},
	}

	cmd := newCreateCommand()
	opts := &createOptions{title: "Test Issue", priority: "p1"}
	err := runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// #863 AC1: create resolves the status/priority field NAMES from cfg.Fields
// rather than hardcoding "Status"/"Priority".
func TestRunCreateWithDeps_UsesConfiguredFieldNames(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		Fields: map[string]config.Field{
			"status":   {Field: "Workflow", Values: map[string]string{"todo": "Todo"}},
			"priority": {Field: "Urgency", Values: map[string]string{"p1": "P1"}},
		},
	}

	cmd := newCreateCommand()
	opts := &createOptions{title: "Test Issue", status: "todo", priority: "p1"}
	if err := runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sawWorkflow, sawUrgency bool
	for _, c := range mock.setFieldCalls {
		if c.fieldName == "Status" || c.fieldName == "Priority" {
			t.Errorf("expected configured field names, got hardcoded %q", c.fieldName)
		}
		if c.fieldName == "Workflow" {
			sawWorkflow = true
		}
		if c.fieldName == "Urgency" {
			sawUrgency = true
		}
	}
	if !sawWorkflow {
		t.Errorf("expected status written to 'Workflow' field; calls=%+v", mock.setFieldCalls)
	}
	if !sawUrgency {
		t.Errorf("expected priority written to 'Urgency' field; calls=%+v", mock.setFieldCalls)
	}
}

// #863 AC1: absent field mapping falls back to the capitalized default names.
func TestRunCreateWithDeps_FieldNameFallbacks(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		Fields:  map[string]config.Field{}, // no status/priority mapping
	}

	cmd := newCreateCommand()
	opts := &createOptions{title: "Test Issue", status: "todo", priority: "p1"}
	if err := runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sawStatus, sawPriority bool
	for _, c := range mock.setFieldCalls {
		if c.fieldName == "Status" {
			sawStatus = true
		}
		if c.fieldName == "Priority" {
			sawPriority = true
		}
	}
	if !sawStatus || !sawPriority {
		t.Errorf("expected fallback 'Status'/'Priority' field names; calls=%+v", mock.setFieldCalls)
	}
}

func TestRunCreateWithDeps_WithRelease(t *testing.T) {
	mock := newMockCreateClient()
	mock.issuesByLabel = []api.Issue{
		{Number: 100, Title: "Release: v1.0.0"},
	}
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newCreateCommand()
	opts := &createOptions{title: "Test Issue", release: "current"}
	err := runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCreateWithDeps_NoActiveRelease(t *testing.T) {
	mock := newMockCreateClient()
	mock.issuesByLabel = []api.Issue{} // No releases
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newCreateCommand()
	opts := &createOptions{title: "Test Issue", release: "current"}
	err := runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")

	if err == nil {
		t.Fatal("expected error for no active branch")
	}
	if !strings.Contains(err.Error(), "no active branch found") {
		t.Errorf("expected 'no active branch found' error, got: %v", err)
	}
}

func TestRunCreateWithDeps_BodyAndBodyFileMutuallyExclusiveInDeps(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	// Create a temp file for body-file
	tmpfile, err := os.CreateTemp("", "body-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	if _, err := tmpfile.WriteString("Body from file"); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpfile.Close()
	defer os.Remove(tmpfile.Name())

	cmd := newCreateCommand()
	opts := &createOptions{
		title:    "Test Issue",
		body:     "Body from flag",
		bodyFile: tmpfile.Name(),
	}
	err = runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")

	if err == nil {
		t.Fatal("expected error for mutually exclusive flags")
	}
	if !strings.Contains(err.Error(), "cannot use --body and --body-file together") {
		t.Errorf("expected mutual exclusivity error, got: %v", err)
	}
}

func TestRunCreateWithDeps_DefaultsApplied(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
		Defaults: config.Defaults{
			Status:   "backlog",
			Priority: "p2",
			Labels:   []string{"auto-label"},
		},
		Fields: map[string]config.Field{
			"status": {
				Field:  "Status",
				Values: map[string]string{"backlog": "Backlog"},
			},
			"priority": {
				Field:  "Priority",
				Values: map[string]string{"p2": "P2"},
			},
		},
	}

	cmd := newCreateCommand()
	opts := &createOptions{title: "Test Issue"}
	err := runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ============================================================================
// runCreateFromFileWithDeps Tests
// ============================================================================

func TestRunCreateFromFileWithDeps_Success(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	// Create YAML file
	tmpfile, err := os.CreateTemp("", "issue-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	yamlContent := `title: "Test Issue from YAML"
body: "Test body"
`
	if _, err := tmpfile.WriteString(yamlContent); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpfile.Close()
	defer os.Remove(tmpfile.Name())

	cmd := newCreateCommand()

	opts := &createOptions{fromFile: tmpfile.Name()}
	err = runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Output goes to os.Stdout via fmt.Printf - verified by running successfully
}

func TestRunCreateFromFileWithDeps_JSONFile(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	// Create JSON file
	tmpfile, err := os.CreateTemp("", "issue-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	jsonContent := `{"title": "Test Issue from JSON", "body": "Test body"}`
	if _, err := tmpfile.WriteString(jsonContent); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpfile.Close()
	defer os.Remove(tmpfile.Name())

	cmd := newCreateCommand()
	opts := &createOptions{fromFile: tmpfile.Name()}
	err = runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCreateFromFileWithDeps_FileNotFound(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newCreateCommand()
	opts := &createOptions{fromFile: "/nonexistent/file.yaml"}
	err := runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")

	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "failed to read file") {
		t.Errorf("expected 'failed to read file' error, got: %v", err)
	}
}

func TestRunCreateFromFileWithDeps_MissingTitle(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	// Create YAML file without title
	tmpfile, err := os.CreateTemp("", "issue-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	yamlContent := `body: "Test body without title"`
	if _, err := tmpfile.WriteString(yamlContent); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpfile.Close()
	defer os.Remove(tmpfile.Name())

	cmd := newCreateCommand()
	opts := &createOptions{fromFile: tmpfile.Name()}
	err = runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")

	if err == nil {
		t.Fatal("expected error for missing title")
	}
	if !strings.Contains(err.Error(), "title is required in file") {
		t.Errorf("expected 'title is required in file' error, got: %v", err)
	}
}

// ============================================================================
// #864: --from-file honors flags, validates, and parses once
// ============================================================================

// writeTempIssueFile writes content to a temp file with the given extension and
// returns its path (cleaned up by t.Cleanup).
func writeTempIssueFile(t *testing.T, ext, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "issue-*"+ext)
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	f.Close()
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	return f.Name()
}

// AC3: unsupported body-source flags are rejected (not silently ignored).
func TestValidateCreateFromFileFlags_RejectsUnsupportedFlags(t *testing.T) {
	file := writeTempIssueFile(t, ".yaml", "title: From File\n")
	cases := []struct {
		name string
		opts *createOptions
		want string
	}{
		{"template", &createOptions{fromFile: file, template: "bug"}, "--template"},
		{"body-file", &createOptions{fromFile: file, bodyFile: "b.md"}, "--body-file"},
		{"body-stdin", &createOptions{fromFile: file, bodyStdin: true}, "--body-stdin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCreateFromFileFlags(tc.opts)
			if err == nil {
				t.Fatalf("expected rejection error for %s, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "--from-file") {
				t.Errorf("expected error naming %s and --from-file, got: %v", tc.want, err)
			}
		})
	}
}

// AC3: --title is honored — it can supply a title missing from the file.
func TestValidateCreateFromFileFlags_TitleFlagSuppliesMissingTitle(t *testing.T) {
	file := writeTempIssueFile(t, ".yaml", "body: no title here\n")
	opts := &createOptions{fromFile: file, title: "CLI Title"}
	if err := validateCreateFromFileFlags(opts); err != nil {
		t.Fatalf("expected --title to satisfy title requirement, got: %v", err)
	}
	if opts.parsedFile == nil {
		t.Fatal("expected parsedFile to be stashed for single-parse reuse")
	}
}

// AC3/AC5: --title overrides the file title.
func TestRunCreateFromFileWithDeps_TitleFlagOverridesFile(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{Project: config.Project{Owner: "o", Number: 1}}
	file := writeTempIssueFile(t, ".yaml", "title: File Title\nbody: b\n")

	opts := &createOptions{fromFile: file, title: "Overridden Title"}
	if err := runCreateFromFileWithDeps(newCreateCommand(), opts, cfg, mock, "o", "r"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.createdIssue.Title != "Overridden Title" {
		t.Errorf("expected CLI --title to win, got %q", mock.createdIssue.Title)
	}
}

// AC2: --branch is honored in from-file mode (was silently ignored).
func TestRunCreateFromFileWithDeps_HonorsBranchFlag(t *testing.T) {
	mock := newMockCreateClient()
	mock.issuesByLabel = []api.Issue{{Number: 100, Title: "Branch: v1.0.0"}}
	cfg := &config.Config{Project: config.Project{Owner: "o", Number: 1}}
	file := writeTempIssueFile(t, ".yaml", "title: T\nbody: b\n")

	opts := &createOptions{fromFile: file, release: "current"}
	if err := runCreateFromFileWithDeps(newCreateCommand(), opts, cfg, mock, "o", "r"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var branchSet bool
	for _, c := range mock.setFieldCalls {
		if c.fieldName == "Branch" && c.value == "v1.0.0" {
			branchSet = true
		}
	}
	if !branchSet {
		t.Errorf("expected Branch field set to 'v1.0.0'; calls=%+v", mock.setFieldCalls)
	}
}

// AC1: IDPF validation runs on merged values — creating as 'done' with unchecked
// boxes is rejected in from-file mode, just like the normal path.
func TestRunCreateFromFileWithDeps_RunsIDPFValidation(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project:   config.Project{Owner: "o", Number: 1},
		Framework: "IDPF",
		Fields: map[string]config.Field{
			"status": {Field: "Status", Values: map[string]string{"done": "Done"}},
		},
	}
	file := writeTempIssueFile(t, ".yaml", "title: T\nstatus: done\nbody: \"- [ ] unchecked\"\n")

	opts := &createOptions{fromFile: file}
	err := runCreateFromFileWithDeps(newCreateCommand(), opts, cfg, mock, "o", "r")
	if err == nil {
		t.Fatal("expected IDPF validation to reject done+unchecked, got nil")
	}
	if !strings.Contains(err.Error(), "unchecked") {
		t.Errorf("expected unchecked-checkbox validation error, got: %v", err)
	}
}

// AC4: file is parsed once — a stashed parsedFile is reused, so execution does not
// re-read the (here nonexistent) path.
func TestRunCreateFromFileWithDeps_ReusesPreParsedFile(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{Project: config.Project{Owner: "o", Number: 1}}

	opts := &createOptions{
		fromFile:   "/nonexistent/should-not-be-read.yaml",
		parsedFile: &issueFromFile{Title: "Pre-parsed", Body: "b"},
	}
	if err := runCreateFromFileWithDeps(newCreateCommand(), opts, cfg, mock, "o", "r"); err != nil {
		t.Fatalf("expected reuse of parsedFile (no re-read), got: %v", err)
	}
	if mock.createdIssue.Title != "Pre-parsed" {
		t.Errorf("expected pre-parsed title, got %q", mock.createdIssue.Title)
	}
}

// AC5: CLI --status/--body/--priority take precedence over file values.
func TestRunCreateFromFileWithDeps_FlagsOverrideFileValues(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "o", Number: 1},
		Fields: map[string]config.Field{
			"status":   {Field: "Status", Values: map[string]string{"in_progress": "In progress"}},
			"priority": {Field: "Priority", Values: map[string]string{"p1": "P1"}},
		},
	}
	file := writeTempIssueFile(t, ".yaml", "title: T\nbody: file body\nstatus: backlog\npriority: p3\n")

	opts := &createOptions{fromFile: file, body: "cli body", status: "in_progress", priority: "p1"}
	if err := runCreateFromFileWithDeps(newCreateCommand(), opts, cfg, mock, "o", "r"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.lastBody != "cli body" {
		t.Errorf("expected CLI body to win, got %q", mock.lastBody)
	}
	var sawInProgress, sawP1 bool
	for _, c := range mock.setFieldCalls {
		if c.fieldName == "Status" && c.value == "In progress" {
			sawInProgress = true
		}
		if c.fieldName == "Priority" && c.value == "P1" {
			sawP1 = true
		}
	}
	if !sawInProgress {
		t.Errorf("expected CLI --status to win (In progress); calls=%+v", mock.setFieldCalls)
	}
	if !sawP1 {
		t.Errorf("expected CLI --priority to win (P1); calls=%+v", mock.setFieldCalls)
	}
}

// ============================================================================
// Issue #491: --body-stdin Tests
// ============================================================================

func TestCreateCommand_HasBodyStdinFlag(t *testing.T) {
	cmd := NewRootCommand()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("create command not found: %v", err)
	}

	flag := createCmd.Flags().Lookup("body-stdin")
	if flag == nil {
		t.Fatal("Expected --body-stdin flag to exist")
	}
	if flag.Value.Type() != "bool" {
		t.Errorf("Expected --body-stdin to be bool, got %s", flag.Value.Type())
	}
}

func TestRunCreateWithDeps_BodyStdinAndBodyMutuallyExclusive(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	cmd := newCreateCommand()
	opts := &createOptions{
		title:     "Test Issue",
		body:      "Body from flag",
		bodyStdin: true,
	}
	err := runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")

	if err == nil {
		t.Fatal("expected error for mutually exclusive flags")
	}
	if !strings.Contains(err.Error(), "cannot use --body and --body-stdin together") {
		t.Errorf("expected mutual exclusivity error, got: %v", err)
	}
}

func TestRunCreateWithDeps_BodyStdinAndBodyFileMutuallyExclusive(t *testing.T) {
	mock := newMockCreateClient()
	cfg := &config.Config{
		Project: config.Project{Owner: "test-org", Number: 1},
	}

	// Create a temp file for body-file
	tmpfile, err := os.CreateTemp("", "body-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	if _, err := tmpfile.WriteString("Body from file"); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpfile.Close()
	defer os.Remove(tmpfile.Name())

	cmd := newCreateCommand()
	opts := &createOptions{
		title:     "Test Issue",
		bodyFile:  tmpfile.Name(),
		bodyStdin: true,
	}
	err = runCreateWithDeps(cmd, opts, cfg, mock, "owner", "repo")

	if err == nil {
		t.Fatal("expected error for mutually exclusive flags")
	}
	if !strings.Contains(err.Error(), "cannot use --body-file and --body-stdin together") {
		t.Errorf("expected mutual exclusivity error, got: %v", err)
	}
}

func TestLoadIssueTemplate_LocalFile(t *testing.T) {
	// Create a temp dir with a local template
	dir := t.TempDir()
	templateDir := filepath.Join(dir, ".github", "ISSUE_TEMPLATE")
	if err := os.MkdirAll(templateDir, 0755); err != nil {
		t.Fatal(err)
	}
	templateContent := "---\nname: Bug\n---\nPlease describe the bug."
	if err := os.WriteFile(filepath.Join(templateDir, "bug.md"), []byte(templateContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Change to temp dir so local paths resolve
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	content, err := loadIssueTemplate("owner", "repo", "bug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "Please describe the bug." {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestLoadIssueTemplate_NotFoundReturnsError(t *testing.T) {
	// In a dir without templates, the function should try REST and fail
	dir := t.TempDir()
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	_, err := loadIssueTemplate("nonexistent-owner", "nonexistent-repo", "nonexistent-template")
	if err == nil {
		t.Fatal("expected error for nonexistent template")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

// ============================================================================
// readBodyFile size limit tests
// ============================================================================

func TestReadBodyFile_AtLimit(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "at-limit.txt")
	// Create a file exactly at maxBodyFileSize (1MB)
	data := bytes.Repeat([]byte("a"), int(maxBodyFileSize))
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	content, err := readBodyFile(path)
	if err != nil {
		t.Errorf("readBodyFile at limit should succeed, got error: %v", err)
	}
	if len(content) != int(maxBodyFileSize) {
		t.Errorf("expected %d bytes, got %d", maxBodyFileSize, len(content))
	}
}

func TestReadBodyFile_OverLimit(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "over-limit.txt")
	// Create a file exceeding maxBodyFileSize
	data := bytes.Repeat([]byte("a"), int(maxBodyFileSize)+1)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := readBodyFile(path)
	if err == nil {
		t.Fatal("readBodyFile over limit should return error")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Errorf("expected 'exceeds maximum size' error, got: %v", err)
	}
}

func TestReadBodyFile_EmptyInput(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "empty.txt")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	content, err := readBodyFile(path)
	if err != nil {
		t.Errorf("readBodyFile empty should succeed, got error: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}
