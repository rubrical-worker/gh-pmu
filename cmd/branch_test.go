package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/spf13/cobra"
)

// setupBranchTestDir creates a temp directory with a .gh-pmu.json config file
// and changes to that directory. Returns cleanup function to restore original dir.
func setupBranchTestDir(t *testing.T, cfg *config.Config) func() {
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

// mockBranchClient implements branchClient for testing
type mockBranchClient struct {
	// Return values
	createdIssue           *api.Issue
	openIssues             []api.Issue
	openIssuesByLabels     []api.Issue // For GetOpenIssuesByLabels
	closedIssues           []api.Issue
	project                *api.Project
	addedItemID            string
	issueByNumber          *api.Issue
	projectItemID          string
	projectItemIDs         map[string]string // issueID -> itemID mapping for per-issue returns
	projectItemFieldValue  string
	projectItemFieldValues map[string]string // itemID -> fieldValue mapping for per-issue status
	projectItems           []api.ProjectItem
	minimalProjectItems    []api.MinimalProjectItem // For GetProjectItemsMinimal
	projectItemsByIssues   []api.ProjectItem        // For GetProjectItemsByIssues
	subIssues              []api.SubIssue           // For GetSubIssues

	// Captured calls for verification
	createIssueCalls             []createIssueCall
	addToProjectCalls            []addToProjectCall
	setFieldCalls                []setFieldCall
	updateIssueBodyCalls         []updateIssueBodyCall
	writeFileCalls               []writeFileCall
	gitAddCalls                  []gitAddCall
	closeIssueCalls              []closeIssueCall
	gitTagCalls                  []gitTagCall
	getProjectItemsCalls         []getProjectItemsCall
	getProjectItemsMinimalCalls  []getProjectItemsCall
	getProjectItemsByIssuesCalls []getProjectItemsByIssuesCall
	getOpenIssuesByLabelsCalls   []getOpenIssuesByLabelsCall
	getSubIssuesCalls            []getSubIssuesCall
	addLabelCalls                []branchLabelCall
	removeLabelCalls             []branchLabelCall
	addSubIssueCalls             []subIssueLinkCall
	removeSubIssueCalls          []subIssueLinkCall
	gitCheckoutCalls             []string

	// Error injection
	createIssueErr             error
	getOpenIssuesErr           error
	getOpenIssuesByLabelsErr   error
	getClosedIssuesErr         error
	addToProjectErr            error
	setFieldErr                error
	getProjectErr              error
	getIssueErr                error
	getProjectItemErr          error
	getProjectItemFieldErr     error
	reopenIssueErr             error
	getProjectItemsErr         error
	getProjectItemsMinimalErr  error
	getProjectItemsByIssuesErr error
	getSubIssuesErr            error
	addLabelErr                error
	removeLabelErr             error
	addSubIssueErr             error
	removeSubIssueErr          error
}

type subIssueLinkCall struct {
	parentIssueID string
	childIssueID  string
}

type branchLabelCall struct {
	owner     string
	repo      string
	issueID   string
	labelName string
}

// Helper types for call tracking
type createIssueCall struct {
	owner  string
	repo   string
	title  string
	body   string
	labels []string
}

type addToProjectCall struct {
	projectID string
	issueID   string
}

type setFieldCall struct {
	projectID string
	itemID    string
	fieldID   string
	value     string
}

type closeIssueCall struct {
	issueID string
}

type updateIssueBodyCall struct {
	issueID string
	body    string
}

type writeFileCall struct {
	path    string
	content string
}

type gitAddCall struct {
	paths []string
}

type gitTagCall struct {
	tag     string
	message string
}

type getProjectItemsCall struct {
	projectID string
	filter    *api.ProjectItemsFilter
}

type getProjectItemsByIssuesCall struct {
	projectID string
	refs      []api.IssueRef
}

type getOpenIssuesByLabelsCall struct {
	owner  string
	repo   string
	labels []string
}

type getSubIssuesCall struct {
	owner  string
	repo   string
	number int
}

func (m *mockBranchClient) CreateIssue(owner, repo, title, body string, labels []string) (*api.Issue, error) {
	m.createIssueCalls = append(m.createIssueCalls, createIssueCall{
		owner:  owner,
		repo:   repo,
		title:  title,
		body:   body,
		labels: labels,
	})
	if m.createIssueErr != nil {
		return nil, m.createIssueErr
	}
	return m.createdIssue, nil
}

func (m *mockBranchClient) GetOpenIssuesByLabel(owner, repo, label string) ([]api.Issue, error) {
	if m.getOpenIssuesErr != nil {
		return nil, m.getOpenIssuesErr
	}
	return m.openIssues, nil
}

func (m *mockBranchClient) GetOpenIssuesByLabels(owner, repo string, labels []string) ([]api.Issue, error) {
	m.getOpenIssuesByLabelsCalls = append(m.getOpenIssuesByLabelsCalls, getOpenIssuesByLabelsCall{
		owner:  owner,
		repo:   repo,
		labels: labels,
	})
	if m.getOpenIssuesByLabelsErr != nil {
		return nil, m.getOpenIssuesByLabelsErr
	}
	return m.openIssuesByLabels, nil
}

func (m *mockBranchClient) GetSubIssues(owner, repo string, number int) ([]api.SubIssue, error) {
	m.getSubIssuesCalls = append(m.getSubIssuesCalls, getSubIssuesCall{
		owner:  owner,
		repo:   repo,
		number: number,
	})
	if m.getSubIssuesErr != nil {
		return nil, m.getSubIssuesErr
	}
	return m.subIssues, nil
}

func (m *mockBranchClient) GetClosedIssuesByLabel(owner, repo, label string) ([]api.Issue, error) {
	if m.getClosedIssuesErr != nil {
		return nil, m.getClosedIssuesErr
	}
	return m.closedIssues, nil
}

func (m *mockBranchClient) AddIssueToProject(projectID, issueID string) (string, error) {
	m.addToProjectCalls = append(m.addToProjectCalls, addToProjectCall{
		projectID: projectID,
		issueID:   issueID,
	})
	if m.addToProjectErr != nil {
		return "", m.addToProjectErr
	}
	return m.addedItemID, nil
}

func (m *mockBranchClient) SetProjectItemField(projectID, itemID, fieldID, value string) error {
	m.setFieldCalls = append(m.setFieldCalls, setFieldCall{
		projectID: projectID,
		itemID:    itemID,
		fieldID:   fieldID,
		value:     value,
	})
	return m.setFieldErr
}

func (m *mockBranchClient) GetProject(owner string, number int) (*api.Project, error) {
	if m.getProjectErr != nil {
		return nil, m.getProjectErr
	}
	return m.project, nil
}

func (m *mockBranchClient) GetIssueByNumber(owner, repo string, number int) (*api.Issue, error) {
	if m.getIssueErr != nil {
		return nil, m.getIssueErr
	}
	return m.issueByNumber, nil
}

func (m *mockBranchClient) GetProjectItemID(projectID, issueID string) (string, error) {
	if m.getProjectItemErr != nil {
		return "", m.getProjectItemErr
	}
	// Check per-issue mapping first
	if m.projectItemIDs != nil {
		if itemID, ok := m.projectItemIDs[issueID]; ok {
			return itemID, nil
		}
		// If map is set but issueID not found, return error (not found)
		return "", fmt.Errorf("project item not found for issue %s", issueID)
	}
	return m.projectItemID, nil
}

func (m *mockBranchClient) GetProjectItemFieldValue(projectID, itemID, fieldID string) (string, bool, error) {
	if m.getProjectItemFieldErr != nil {
		return "", false, m.getProjectItemFieldErr
	}
	// Check per-item mapping first
	if m.projectItemFieldValues != nil {
		if value, ok := m.projectItemFieldValues[itemID]; ok {
			return value, true, nil
		}
	}
	return m.projectItemFieldValue, m.projectItemFieldValue != "", nil
}

func (m *mockBranchClient) GetProjectItems(projectID string, filter *api.ProjectItemsFilter) ([]api.ProjectItem, error) {
	m.getProjectItemsCalls = append(m.getProjectItemsCalls, getProjectItemsCall{
		projectID: projectID,
		filter:    filter,
	})
	if m.getProjectItemsErr != nil {
		return nil, m.getProjectItemsErr
	}
	return m.projectItems, nil
}

func (m *mockBranchClient) GetProjectItemsMinimal(projectID string, filter *api.ProjectItemsFilter) ([]api.MinimalProjectItem, error) {
	m.getProjectItemsMinimalCalls = append(m.getProjectItemsMinimalCalls, getProjectItemsCall{
		projectID: projectID,
		filter:    filter,
	})
	if m.getProjectItemsMinimalErr != nil {
		return nil, m.getProjectItemsMinimalErr
	}
	// If minimalProjectItems is set, return it
	if m.minimalProjectItems != nil {
		return m.minimalProjectItems, nil
	}
	// Otherwise, convert projectItems to minimal items for backward compatibility
	var minimal []api.MinimalProjectItem
	for _, item := range m.projectItems {
		if item.Issue != nil {
			repo := ""
			if item.Issue.Repository.Owner != "" && item.Issue.Repository.Name != "" {
				repo = item.Issue.Repository.Owner + "/" + item.Issue.Repository.Name
			}
			minimal = append(minimal, api.MinimalProjectItem{
				IssueID:     item.Issue.ID,
				IssueNumber: item.Issue.Number,
				IssueState:  item.Issue.State,
				Repository:  repo,
				FieldValues: item.FieldValues,
			})
		}
	}
	return minimal, nil
}

func (m *mockBranchClient) GetProjectItemsByIssues(projectID string, refs []api.IssueRef) ([]api.ProjectItem, error) {
	m.getProjectItemsByIssuesCalls = append(m.getProjectItemsByIssuesCalls, getProjectItemsByIssuesCall{
		projectID: projectID,
		refs:      refs,
	})
	if m.getProjectItemsByIssuesErr != nil {
		return nil, m.getProjectItemsByIssuesErr
	}
	// If projectItemsByIssues is set, return it
	if m.projectItemsByIssues != nil {
		return m.projectItemsByIssues, nil
	}
	// Otherwise, filter projectItems to match the requested refs
	var result []api.ProjectItem
	for _, item := range m.projectItems {
		if item.Issue == nil {
			continue
		}
		for _, ref := range refs {
			if item.Issue.Number == ref.Number &&
				item.Issue.Repository.Owner == ref.Owner &&
				item.Issue.Repository.Name == ref.Repo {
				result = append(result, item)
				break
			}
		}
	}
	return result, nil
}

func (m *mockBranchClient) UpdateIssueBody(issueID, body string) error {
	m.updateIssueBodyCalls = append(m.updateIssueBodyCalls, updateIssueBodyCall{
		issueID: issueID,
		body:    body,
	})
	return nil
}

func (m *mockBranchClient) WriteFile(path, content string) error {
	m.writeFileCalls = append(m.writeFileCalls, writeFileCall{
		path:    path,
		content: content,
	})
	return nil
}

func (m *mockBranchClient) MkdirAll(path string) error {
	return nil
}

func (m *mockBranchClient) GitAdd(paths ...string) error {
	m.gitAddCalls = append(m.gitAddCalls, gitAddCall{
		paths: paths,
	})
	return nil
}

func (m *mockBranchClient) CloseIssue(issueID string, stateReason string) error {
	m.closeIssueCalls = append(m.closeIssueCalls, closeIssueCall{
		issueID: issueID,
	})
	return nil
}

func (m *mockBranchClient) ReopenIssue(issueID string) error {
	if m.reopenIssueErr != nil {
		return m.reopenIssueErr
	}
	return nil
}

func (m *mockBranchClient) GitTag(tag, message string) error {
	m.gitTagCalls = append(m.gitTagCalls, gitTagCall{
		tag:     tag,
		message: message,
	})
	return nil
}

func (m *mockBranchClient) GitCheckoutNewBranch(branch string) error {
	m.gitCheckoutCalls = append(m.gitCheckoutCalls, branch)
	return nil
}

func (m *mockBranchClient) AddLabelToIssue(owner, repo, issueID, labelName string) error {
	m.addLabelCalls = append(m.addLabelCalls, branchLabelCall{
		owner:     owner,
		repo:      repo,
		issueID:   issueID,
		labelName: labelName,
	})
	return m.addLabelErr
}

func (m *mockBranchClient) RemoveLabelFromIssue(owner, repo, issueID, labelName string) error {
	m.removeLabelCalls = append(m.removeLabelCalls, branchLabelCall{
		owner:     owner,
		repo:      repo,
		issueID:   issueID,
		labelName: labelName,
	})
	return m.removeLabelErr
}

func (m *mockBranchClient) AddSubIssue(parentIssueID, childIssueID string) error {
	m.addSubIssueCalls = append(m.addSubIssueCalls, subIssueLinkCall{
		parentIssueID: parentIssueID,
		childIssueID:  childIssueID,
	})
	return m.addSubIssueErr
}

func (m *mockBranchClient) RemoveSubIssue(parentIssueID, childIssueID string) error {
	m.removeSubIssueCalls = append(m.removeSubIssueCalls, subIssueLinkCall{
		parentIssueID: parentIssueID,
		childIssueID:  childIssueID,
	})
	return m.removeSubIssueErr
}

// testBranchConfig returns a test configuration for release tests
func testBranchConfig() *config.Config {
	return &config.Config{
		Project: config.Project{
			Owner:  "testowner",
			Number: 1,
		},
		Repositories: []string{"testowner/testrepo"},
		Fields: map[string]config.Field{
			"status": {
				Field: "Status",
				Values: map[string]string{
					"in_progress": "In progress",
				},
			},
		},
	}
}

// setupMockForBranch creates a mock configured for release start tests
func setupMockForBranch() *mockBranchClient {
	return &mockBranchClient{
		openIssues: []api.Issue{}, // No active releases
		createdIssue: &api.Issue{
			ID:     "ISSUE_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			URL:    "https://github.com/testowner/testrepo/issues/100",
		},
		project: &api.Project{
			ID:     "PROJECT_1",
			Number: 1,
			Title:  "Test Project",
		},
		addedItemID: "ITEM_456",
	}
}

// Helper to create a test command with captured output
func newTestBranchCmd() (*cobra.Command, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "release"}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	return cmd, buf
}

// =============================================================================
// REQ-017: Start Release
// =============================================================================

// AC-017-1: Given `release start --branch release/v1.2.0`, Then tracker issue created: "Branch: release/v1.2.0"
func TestRunBranchStartWithDeps_CreatesTrackerIssue(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchStartOptions{
		branchName: "release/v1.2.0",
	}

	expectedTitle := "Branch: release/v1.2.0"

	// ACT
	err := runBranchStartWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify CreateIssue was called
	if len(mock.createIssueCalls) != 1 {
		t.Fatalf("Expected 1 CreateIssue call, got %d", len(mock.createIssueCalls))
	}

	call := mock.createIssueCalls[0]

	// Verify title matches expected pattern
	if call.title != expectedTitle {
		t.Errorf("Expected title '%s', got '%s'", expectedTitle, call.title)
	}
}

// #929: --dry-run checks for an active branch and resolves the project, then
// prints the plan without creating the git branch, the tracker or any field.
func TestRunBranchStartWithDeps_DryRunWritesNothing(t *testing.T) {
	mock := setupMockForBranch()
	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, buf := newTestBranchCmd()
	opts := &branchStartOptions{branchName: "release/v1.2.0", dryRun: true}

	if err := runBranchStartWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if len(mock.gitCheckoutCalls) != 0 || len(mock.createIssueCalls) != 0 ||
		len(mock.addToProjectCalls) != 0 || len(mock.setFieldCalls) != 0 {
		t.Errorf("dry run wrote: checkout=%v create=%d add=%d fields=%d",
			mock.gitCheckoutCalls, len(mock.createIssueCalls), len(mock.addToProjectCalls), len(mock.setFieldCalls))
	}
	for _, want := range []string{"Dry run", "release/v1.2.0", "Branch: release/v1.2.0", "branch"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("expected %q in dry-run output, got:\n%s", want, buf.String())
		}
	}
}

// #929: a dry run still refuses when a branch is already active — it checks
// what the real run checks.
func TestRunBranchStartWithDeps_DryRunStillRejectsActiveBranch(t *testing.T) {
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{{Number: 5, Title: "Branch: release/v1.1.0"}}
	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	err := runBranchStartWithDeps(cmd, &branchStartOptions{branchName: "release/v1.2.0", dryRun: true}, cfg, mock)
	if err == nil || !strings.Contains(err.Error(), "active branch exists") {
		t.Fatalf("expected 'active branch exists', got: %v", err)
	}
}

func TestBranchStartCommand_HasDryRunFlag(t *testing.T) {
	if newBranchStartCommand().Flags().Lookup("dry-run") == nil {
		t.Fatal("Expected branch start to define --dry-run")
	}
}

func TestRunBranchStartWithDeps_TrackerFailureIncludesRecoveryGuidance(t *testing.T) {
	// #871 finding 4: the git branch is created before the tracker/project setup.
	// A failure afterwards leaves partial state; the error must name the created
	// branch and give recovery guidance rather than a bare error.
	mock := setupMockForBranch()
	mock.createIssueErr = errors.New("tracker create failed")
	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchStartOptions{branchName: "release/v1.2.0"}

	err := runBranchStartWithDeps(cmd, opts, cfg, mock)
	if err == nil {
		t.Fatal("expected error when tracker creation fails")
	}
	msg := err.Error()
	if !strings.Contains(msg, "release/v1.2.0") {
		t.Errorf("expected error to name the already-created branch for recovery, got: %v", err)
	}
}

// AC-017-3: Given tracker issue created, Then has `branch` label
func TestRunBranchStartWithDeps_HasBranchLabel(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchStartOptions{
		branchName: "release/v1.2.0",
	}

	// ACT
	err := runBranchStartWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(mock.createIssueCalls) != 1 {
		t.Fatalf("Expected 1 CreateIssue call, got %d", len(mock.createIssueCalls))
	}

	call := mock.createIssueCalls[0]
	hasLabel := false
	for _, label := range call.labels {
		if label == "branch" {
			hasLabel = true
			break
		}
	}
	if !hasLabel {
		t.Errorf("Expected 'branch' label, got labels: %v", call.labels)
	}
}

// AC-017-4: Given active branch exists, When running `release start`, Then error: "Active release exists"
func TestRunBranchStartWithDeps_ActiveReleaseExists_ReturnsError(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	// Simulate an existing active release
	mock.openIssues = []api.Issue{
		{
			ID:     "EXISTING_RELEASE",
			Number: 50,
			Title:  "Branch: release/v1.1.0",
			State:  "OPEN",
		},
	}
	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchStartOptions{
		branchName: "release/v1.2.0",
	}

	// ACT
	err := runBranchStartWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatalf("Expected error for active branch exists, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(strings.ToLower(errMsg), "active branch") {
		t.Errorf("Expected error to mention 'active branch', got: %s", errMsg)
	}
}

// Test that release is added to project and status set to In Progress
func TestRunBranchStartWithDeps_AddsToProjectAndSetsStatus(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchStartOptions{
		branchName: "release/v1.2.0",
	}

	// ACT
	err := runBranchStartWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify issue was added to project
	if len(mock.addToProjectCalls) != 1 {
		t.Fatalf("Expected 1 AddIssueToProject call, got %d", len(mock.addToProjectCalls))
	}

	// Verify status field was set to In Progress
	statusSet := false
	for _, call := range mock.setFieldCalls {
		if call.value == "In progress" {
			statusSet = true
			break
		}
	}
	if !statusSet {
		t.Errorf("Expected status to be set to 'In progress', got calls: %+v", mock.setFieldCalls)
	}
}

// =============================================================================
// REQ-018: Version Validation
// =============================================================================

// AC-018-1: Given `release start --version 1.2.0`, Then accepted (valid semver)
func TestBranchValidateVersion_ValidSemver_Accepted(t *testing.T) {
	validVersions := []string{
		"1.2.0",
		"0.1.0",
		"10.20.30",
		"1.0.0",
	}

	for _, version := range validVersions {
		err := validateVersion(version)
		if err != nil {
			t.Errorf("Expected version '%s' to be valid, got error: %v", version, err)
		}
	}
}

// AC-018-2: Given `release start --version 1.2`, Then error: "Invalid version format. Use semver: X.Y.Z"
func TestBranchValidateVersion_InvalidFormat_ReturnsError(t *testing.T) {
	invalidVersions := []string{
		"1.2",
		"1",
		"1.2.3.4",
		"abc",
		"1.2.x",
		"",
	}

	for _, version := range invalidVersions {
		err := validateVersion(version)
		if err == nil {
			t.Errorf("Expected version '%s' to be invalid, got no error", version)
			continue
		}

		errMsg := err.Error()
		if !strings.Contains(errMsg, "Invalid version format") {
			t.Errorf("Expected error message to contain 'Invalid version format', got: %s", errMsg)
		}
	}
}

// AC-018-3: Given `release start --version v1.2.0`, Then accepted (v prefix allowed)
func TestBranchValidateVersion_VPrefixAllowed(t *testing.T) {
	versionsWithPrefix := []string{
		"v1.2.0",
		"v0.1.0",
		"v10.20.30",
	}

	for _, version := range versionsWithPrefix {
		err := validateVersion(version)
		if err != nil {
			t.Errorf("Expected version '%s' (with v prefix) to be valid, got error: %v", version, err)
		}
	}
}

// Test that branch names are used literally
func TestRunBranchStartWithDeps_BranchNameUsedLiterally(t *testing.T) {
	testCases := []struct {
		branch        string
		expectedTitle string
	}{
		{"release/v1.2.0", "Branch: release/v1.2.0"},
		{"patch/v1.1.1", "Branch: patch/v1.1.1"},
		{"hotfix-auth-bypass", "Branch: hotfix-auth-bypass"},
	}

	for _, tc := range testCases {
		t.Run(tc.branch, func(t *testing.T) {
			mock := setupMockForBranch()
			cfg := testBranchConfig()
			cleanup := setupBranchTestDir(t, cfg)
			defer cleanup()

			cmd, _ := newTestBranchCmd()
			opts := &branchStartOptions{
				branchName: tc.branch,
			}

			err := runBranchStartWithDeps(cmd, opts, cfg, mock)
			if err != nil {
				t.Fatalf("Expected no error, got: %v", err)
			}

			if len(mock.createIssueCalls) != 1 {
				t.Fatalf("Expected 1 CreateIssue call, got %d", len(mock.createIssueCalls))
			}

			if mock.createIssueCalls[0].title != tc.expectedTitle {
				t.Errorf("Expected title '%s', got '%s'", tc.expectedTitle, mock.createIssueCalls[0].title)
			}
		})
	}
}

// =============================================================================
// REQ-019: Add Issue to Release
// =============================================================================

// AC-019-1: Given active release v1.2.0, When running `release add 42`,
// Then Release field on #42 set to "v1.2.0"
func TestRunBranchAddWithDeps_SetsReleaseField(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	// Active release exists
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	// The issue to add
	mock.issueByNumber = &api.Issue{
		ID:     "ISSUE_42",
		Number: 42,
		Title:  "Fix login bug",
	}
	// Project item for issue 42
	mock.projectItemID = "ITEM_42"

	cfg := testBranchConfig()
	// Add release field to config
	cfg.Fields["branch"] = config.Field{
		Field: "Release",
	}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchAddOptions{
		issueNumber: 42,
	}

	// ACT
	err := runBranchAddWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify SetProjectItemField was called with correct values
	if len(mock.setFieldCalls) != 1 {
		t.Fatalf("Expected 1 SetProjectItemField call, got %d", len(mock.setFieldCalls))
	}

	call := mock.setFieldCalls[0]
	if call.value != "v1.2.0" {
		t.Errorf("Expected field value 'v1.2.0', got '%s'", call.value)
	}
	if call.fieldID != "Release" {
		t.Errorf("Expected fieldID 'Release', got '%s'", call.fieldID)
	}
}

// AC-019-2: Given issue added, Then output: "Added #42 to release v1.2.0"
func TestRunBranchAddWithDeps_OutputsConfirmation(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	mock.issueByNumber = &api.Issue{
		ID:     "ISSUE_42",
		Number: 42,
		Title:  "Fix login bug",
	}
	mock.projectItemID = "ITEM_42"

	cfg := testBranchConfig()
	cfg.Fields["branch"] = config.Field{
		Field: "Release",
	}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, buf := newTestBranchCmd()
	opts := &branchAddOptions{
		issueNumber: 42,
	}

	// ACT
	err := runBranchAddWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	expectedOutput := "Added #42 to release v1.2.0"
	if !strings.Contains(output, expectedOutput) {
		t.Errorf("Expected output to contain '%s', got '%s'", expectedOutput, output)
	}
}

// Test error when no active branch exists
func TestRunBranchAddWithDeps_NoActiveRelease_ReturnsError(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{} // No active release

	cfg := testBranchConfig()
	cfg.Fields["branch"] = config.Field{
		Field: "Release",
	}

	cmd, _ := newTestBranchCmd()
	opts := &branchAddOptions{
		issueNumber: 42,
	}

	// ACT
	err := runBranchAddWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatalf("Expected error for no active release, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "no active branch found") {
		t.Errorf("Expected error to mention 'no active branch found', got: %s", errMsg)
	}
}

// =============================================================================
// REQ-039: Remove Issue from Release
// =============================================================================

// AC-039-1: Given issue #42 assigned to release, When running `release remove 42`,
// Then Release Text field cleared (set to empty)
func TestRunBranchRemoveWithDeps_ClearsReleaseField(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	mock.issueByNumber = &api.Issue{
		ID:     "ISSUE_42",
		Number: 42,
		Title:  "Fix login bug",
	}
	mock.projectItemID = "ITEM_42"
	mock.projectItemFieldValue = "v1.2.0" // Currently assigned

	cfg := testBranchConfig()
	cfg.Fields["branch"] = config.Field{
		Field: "Release",
	}

	cmd, _ := newTestBranchCmd()
	opts := &branchRemoveOptions{
		issueNumber: 42,
	}

	// ACT
	err := runBranchRemoveWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(mock.setFieldCalls) != 1 {
		t.Fatalf("Expected 1 SetProjectItemField call, got %d", len(mock.setFieldCalls))
	}

	call := mock.setFieldCalls[0]
	if call.value != "" {
		t.Errorf("Expected field value to be empty (cleared), got '%s'", call.value)
	}
}

// AC-039-2: Given field cleared, Then output confirms "Removed #42 from release vX.Y.Z"
func TestRunBranchRemoveWithDeps_OutputsConfirmation(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	mock.issueByNumber = &api.Issue{
		ID:     "ISSUE_42",
		Number: 42,
		Title:  "Fix login bug",
	}
	mock.projectItemID = "ITEM_42"
	mock.projectItemFieldValue = "v1.2.0"

	cfg := testBranchConfig()
	cfg.Fields["branch"] = config.Field{
		Field: "Release",
	}

	cmd, buf := newTestBranchCmd()
	opts := &branchRemoveOptions{
		issueNumber: 42,
	}

	// ACT
	err := runBranchRemoveWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	expectedOutput := "Removed #42 from release v1.2.0"
	if !strings.Contains(output, expectedOutput) {
		t.Errorf("Expected output to contain '%s', got '%s'", expectedOutput, output)
	}
}

// AC-039-3: Given issue not in any release, When running `release remove 42`,
// Then warning: "Issue #42 is not assigned to a release"
func TestRunBranchRemoveWithDeps_WarnsIfNotAssigned(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	mock.issueByNumber = &api.Issue{
		ID:     "ISSUE_42",
		Number: 42,
		Title:  "Fix login bug",
	}
	mock.projectItemID = "ITEM_42"
	mock.projectItemFieldValue = "" // Not assigned

	cfg := testBranchConfig()
	cfg.Fields["branch"] = config.Field{
		Field: "Release",
	}

	cmd, buf := newTestBranchCmd()
	opts := &branchRemoveOptions{
		issueNumber: 42,
	}

	// ACT
	err := runBranchRemoveWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error (warning only), got: %v", err)
	}

	output := buf.String()
	expectedWarning := "Issue #42 is not assigned to a release"
	if !strings.Contains(output, expectedWarning) {
		t.Errorf("Expected output to contain warning '%s', got '%s'", expectedWarning, output)
	}

	if len(mock.setFieldCalls) != 0 {
		t.Errorf("Expected 0 SetProjectItemField calls (nothing to clear), got %d", len(mock.setFieldCalls))
	}
}

// =============================================================================
// REQ-036: View Current Release
// =============================================================================

// AC-036-1: Given active release via active label, Then displays details (1 API call)
func TestRunBranchCurrentWithDeps_DisplaysActiveDetails(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssuesByLabels = []api.Issue{
		{
			ID:            "TRACKER_123",
			Number:        100,
			Title:         "Branch: v1.2.0 (Phoenix)",
			State:         "OPEN",
			SubIssueCount: 3,
		},
	}

	cfg := testBranchConfig()
	cmd, buf := newTestBranchCmd()
	opts := &branchCurrentOptions{}

	// ACT
	err := runBranchCurrentWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "v1.2.0") {
		t.Errorf("Expected output to contain version 'v1.2.0', got '%s'", output)
	}
	if !strings.Contains(output, "#100") {
		t.Errorf("Expected output to contain tracker issue '#100', got '%s'", output)
	}
	if !strings.Contains(output, "Issues: 3") {
		t.Errorf("Expected output to contain 'Issues: 3', got '%s'", output)
	}

	// Verify only 1 API call was made (GetOpenIssuesByLabels, no GetProject/GetProjectItemsMinimal)
	if len(mock.getOpenIssuesByLabelsCalls) != 1 {
		t.Errorf("Expected 1 GetOpenIssuesByLabels call, got %d", len(mock.getOpenIssuesByLabelsCalls))
	}
	if len(mock.getProjectItemsMinimalCalls) != 0 {
		t.Errorf("Expected 0 GetProjectItemsMinimal calls, got %d", len(mock.getProjectItemsMinimalCalls))
	}
}

// AC-036-2: Given no active release, Then message: "No active release"
func TestRunBranchCurrentWithDeps_NoActiveRelease(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssuesByLabels = nil   // No active+branch match
	mock.openIssues = []api.Issue{} // No branch-only match either

	cfg := testBranchConfig()
	cmd, buf := newTestBranchCmd()
	opts := &branchCurrentOptions{}

	// ACT
	err := runBranchCurrentWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	expectedMessage := "No active release"
	if !strings.Contains(output, expectedMessage) {
		t.Errorf("Expected output to contain '%s', got '%s'", expectedMessage, output)
	}
}

// AC: findActiveBranch prefers active+branch label query, falls back to title scan
func TestRunBranchCurrentWithDeps_ActiveLabelLookup(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssuesByLabels = []api.Issue{
		{
			ID:            "TRACKER_ACTIVE",
			Number:        200,
			Title:         "Branch: v2.0.0",
			State:         "OPEN",
			SubIssueCount: 5,
		},
	}

	cfg := testBranchConfig()
	cmd, buf := newTestBranchCmd()
	opts := &branchCurrentOptions{}

	// ACT
	err := runBranchCurrentWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "v2.0.0") {
		t.Errorf("Expected output to contain 'v2.0.0', got '%s'", output)
	}

	// Should use active+branch labels
	if len(mock.getOpenIssuesByLabelsCalls) != 1 {
		t.Fatalf("Expected 1 GetOpenIssuesByLabels call, got %d", len(mock.getOpenIssuesByLabelsCalls))
	}
	call := mock.getOpenIssuesByLabelsCalls[0]
	if len(call.labels) != 2 || call.labels[0] != "active" || call.labels[1] != "branch" {
		t.Errorf("Expected labels [active, branch], got %v", call.labels)
	}
}

// AC: Falls back to title scan when no active label found
func TestRunBranchCurrentWithDeps_FallbackToTitleScan(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssuesByLabels = nil // No active+branch match
	mock.openIssues = []api.Issue{
		{
			ID:            "TRACKER_123",
			Number:        100,
			Title:         "Branch: v1.2.0",
			State:         "OPEN",
			SubIssueCount: 2,
		},
		{
			ID:     "OTHER_ISSUE",
			Number: 101,
			Title:  "Some other issue",
			State:  "OPEN",
		},
	}

	cfg := testBranchConfig()
	cmd, buf := newTestBranchCmd()
	opts := &branchCurrentOptions{}

	// ACT
	err := runBranchCurrentWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "v1.2.0") {
		t.Errorf("Expected output to contain 'v1.2.0', got '%s'", output)
	}
	if !strings.Contains(output, "#100") {
		t.Errorf("Expected output to contain '#100', got '%s'", output)
	}
}

// AC: --json outputs full JSON object
func TestRunBranchCurrentWithDeps_JSONFullOutput(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssuesByLabels = []api.Issue{
		{
			ID:            "TRACKER_123",
			Number:        100,
			Title:         "Branch: v1.2.0",
			State:         "OPEN",
			SubIssueCount: 2,
		},
	}
	mock.subIssues = []api.SubIssue{
		{Number: 41, Title: "Fix bug A", State: "CLOSED"},
		{Number: 42, Title: "Fix bug B", State: "OPEN"},
	}

	cfg := testBranchConfig()
	cmd, buf := newTestBranchCmd()
	opts := &branchCurrentOptions{jsonFlag: "*", jsonSet: true}

	// ACT
	err := runBranchCurrentWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	// Should contain JSON with all fields
	if !strings.Contains(output, `"name":"v1.2.0"`) {
		t.Errorf("Expected JSON to contain name, got '%s'", output)
	}
	if !strings.Contains(output, `"tracker":100`) {
		t.Errorf("Expected JSON to contain tracker, got '%s'", output)
	}
	if !strings.Contains(output, `"number":41`) {
		t.Errorf("Expected JSON to contain issue 41, got '%s'", output)
	}
	if !strings.Contains(output, `"state":"done"`) {
		t.Errorf("Expected JSON to contain state 'done' for closed issue, got '%s'", output)
	}
	if !strings.Contains(output, `"state":"open"`) {
		t.Errorf("Expected JSON to contain state 'open' for open issue, got '%s'", output)
	}
}

// AC: --json=<fields> selects specific fields
func TestRunBranchCurrentWithDeps_JSONFieldSelection(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssuesByLabels = []api.Issue{
		{
			ID:            "TRACKER_123",
			Number:        730,
			Title:         "Branch: pmu/next-version",
			State:         "OPEN",
			SubIssueCount: 4,
		},
	}

	cfg := testBranchConfig()
	cmd, buf := newTestBranchCmd()
	opts := &branchCurrentOptions{jsonFlag: "tracker", jsonSet: true}

	// ACT
	err := runBranchCurrentWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := strings.TrimSpace(buf.String())
	if output != `{"tracker":730}` {
		t.Errorf("Expected JSON '{\"tracker\":730}', got '%s'", output)
	}
}

// AC: --json=tracker,issues returns both tracker and issues
func TestRunBranchCurrentWithDeps_JSONMultipleFields(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssuesByLabels = []api.Issue{
		{
			ID:            "TRACKER_123",
			Number:        730,
			Title:         "Branch: pmu/next-version",
			State:         "OPEN",
			SubIssueCount: 1,
		},
	}
	mock.subIssues = []api.SubIssue{
		{Number: 698, Title: "Praxis rebrand", State: "CLOSED"},
	}

	cfg := testBranchConfig()
	cmd, buf := newTestBranchCmd()
	opts := &branchCurrentOptions{jsonFlag: "tracker,issues", jsonSet: true}

	// ACT
	err := runBranchCurrentWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := strings.TrimSpace(buf.String())
	if !strings.Contains(output, `"tracker":730`) {
		t.Errorf("Expected JSON to contain tracker, got '%s'", output)
	}
	if !strings.Contains(output, `"number":698`) {
		t.Errorf("Expected JSON to contain issue 698, got '%s'", output)
	}
	if !strings.Contains(output, `"state":"done"`) {
		t.Errorf("Expected closed issue state to be 'done', got '%s'", output)
	}
	// Should NOT contain name field (not requested)
	if strings.Contains(output, `"name"`) {
		t.Errorf("Expected JSON to NOT contain 'name' field, got '%s'", output)
	}
}

// AC: issues array contains sub-issues with number, title, state (open/done)
func TestRunBranchCurrentWithDeps_JSONIssuesStateMapping(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssuesByLabels = []api.Issue{
		{
			ID:            "TRACKER_123",
			Number:        100,
			Title:         "Branch: v1.0.0",
			State:         "OPEN",
			SubIssueCount: 2,
		},
	}
	mock.subIssues = []api.SubIssue{
		{Number: 10, Title: "Open issue", State: "OPEN"},
		{Number: 11, Title: "Closed issue", State: "CLOSED"},
	}

	cfg := testBranchConfig()
	cmd, buf := newTestBranchCmd()
	opts := &branchCurrentOptions{jsonFlag: "issues", jsonSet: true}

	// ACT
	err := runBranchCurrentWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := strings.TrimSpace(buf.String())
	// OPEN maps to "open", CLOSED maps to "done"
	if !strings.Contains(output, `"state":"open"`) {
		t.Errorf("Expected 'open' state, got '%s'", output)
	}
	if !strings.Contains(output, `"state":"done"`) {
		t.Errorf("Expected 'done' state, got '%s'", output)
	}
}

// AC: --refresh removal doesn't break existing callers (no --refresh flag exists)
func TestRunBranchCurrentWithDeps_NoRefreshFlag(t *testing.T) {
	cmd := NewRootCommand()
	currentCmd, _, err := cmd.Find([]string{"branch", "current"})
	if err != nil {
		t.Fatalf("branch current command not found: %v", err)
	}

	flag := currentCmd.Flags().Lookup("refresh")
	if flag != nil {
		t.Fatal("Expected --refresh flag to NOT exist (removed)")
	}
}

// AC: No behavioral change for default text output (same format without --json)
func TestRunBranchCurrentWithDeps_DefaultTextOutput(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssuesByLabels = []api.Issue{
		{
			ID:            "TRACKER_123",
			Number:        100,
			Title:         "Branch: v1.2.0",
			State:         "OPEN",
			SubIssueCount: 3,
		},
	}

	cfg := testBranchConfig()
	cmd, buf := newTestBranchCmd()
	opts := &branchCurrentOptions{}

	// ACT
	err := runBranchCurrentWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	// Verify exact format matches original
	if !strings.Contains(output, "Current Branch: v1.2.0") {
		t.Errorf("Expected 'Current Branch: v1.2.0', got '%s'", output)
	}
	if !strings.Contains(output, "Tracker: #100") {
		t.Errorf("Expected 'Tracker: #100', got '%s'", output)
	}
	if !strings.Contains(output, "Issues: 3") {
		t.Errorf("Expected 'Issues: 3', got '%s'", output)
	}
}

// AC: GetOpenIssuesByLabels error propagates
func TestRunBranchCurrentWithDeps_GetIssuesByLabelsError(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.getOpenIssuesByLabelsErr = errors.New("API error")

	cfg := testBranchConfig()
	cmd, _ := newTestBranchCmd()
	opts := &branchCurrentOptions{}

	// ACT
	err := runBranchCurrentWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get branch issues") {
		t.Errorf("Expected error to contain 'failed to get branch issues', got: %v", err)
	}
}

// Test that release close closes the tracker issue
func TestRunBranchCloseWithDeps_ClosesTrackerIssue(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify CloseIssue was called
	if len(mock.closeIssueCalls) != 1 {
		t.Fatalf("Expected 1 CloseIssue call, got %d", len(mock.closeIssueCalls))
	}

	if mock.closeIssueCalls[0].issueID != "TRACKER_123" {
		t.Errorf("Expected to close TRACKER_123, got %s", mock.closeIssueCalls[0].issueID)
	}
}

// Test error when no active release
func TestRunBranchCloseWithDeps_NoActiveRelease_ReturnsError(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{} // No active release

	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatalf("Expected error for no active release, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "branch not found") {
		t.Errorf("Expected error to mention 'branch not found', got: %s", errMsg)
	}
}

func TestRunBranchCloseWithDeps_GetProjectError(t *testing.T) {
	// ARRANGE: GetProject only called when there are incomplete issues needing field ops
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN"},
	}
	// Need an open sub-issue to trigger the deferred GetProject call
	mock.subIssues = []api.SubIssue{
		{ID: "ISSUE_1", Number: 41, Title: "Open issue", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	mock.getProjectErr = errors.New("failed to get project")

	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get project") {
		t.Errorf("Expected error to contain 'failed to get project', got: %v", err)
	}
}

func TestRunBranchCloseWithDeps_GetSubIssuesError_ReturnsError(t *testing.T) {
	// ARRANGE: GetSubIssues fails — branch close should fail
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN"},
	}
	mock.getSubIssuesErr = errors.New("failed to get sub-issues")

	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get") {
		t.Errorf("Expected error to contain 'failed to get', got: %v", err)
	}
}

// Test that branch close calls GetSubIssues with correct owner/repo/tracker
func TestRunBranchCloseWithDeps_UsesCorrectSubIssueParams(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN"},
	}

	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify GetSubIssues was called with correct params
	if len(mock.getSubIssuesCalls) != 1 {
		t.Fatalf("Expected 1 GetSubIssues call, got %d", len(mock.getSubIssuesCalls))
	}

	call := mock.getSubIssuesCalls[0]
	if call.owner != "testowner" {
		t.Errorf("Expected owner %q, got %q", "testowner", call.owner)
	}
	if call.repo != "testrepo" {
		t.Errorf("Expected repo %q, got %q", "testrepo", call.repo)
	}
	if call.number != 100 {
		t.Errorf("Expected tracker number 100, got %d", call.number)
	}
}

// =============================================================================
// REQ-021: Release Git Tag
// =============================================================================

// AC-021-1: Given `release close --tag`, Then `git tag -a v1.2.0 -m "Release v1.2.0"` executed
func TestRunBranchCloseWithDeps_WithTag_CreatesGitTag(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchCloseOptions{
		branchName: "v1.2.0",
		yes:        true,
		tag:        true,
	}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify GitTag was called
	if len(mock.gitTagCalls) != 1 {
		t.Fatalf("Expected 1 GitTag call, got %d", len(mock.gitTagCalls))
	}

	call := mock.gitTagCalls[0]
	if call.tag != "v1.2.0" {
		t.Errorf("Expected tag 'v1.2.0', got '%s'", call.tag)
	}
	if !strings.Contains(call.message, "Release v1.2.0") {
		t.Errorf("Expected message to contain 'Release v1.2.0', got '%s'", call.message)
	}
}

// AC-021-2: Given tag created, Then NOT pushed (user controls push timing)
// This is verified by NOT having a GitPush call in the implementation

// AC-021-3: Given `release close` (no --tag), Then no tag created
func TestRunBranchCloseWithDeps_NoTag_NoGitTagCreated(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchCloseOptions{
		branchName: "v1.2.0",
		yes:        true,
		tag:        false, // No --tag flag
	}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify GitTag was NOT called
	if len(mock.gitTagCalls) != 0 {
		t.Errorf("Expected 0 GitTag calls (no --tag flag), got %d", len(mock.gitTagCalls))
	}
}

// =============================================================================
// REQ-022: List Releases
// =============================================================================

// AC-022-1: Given `release list`, Then table: Version, Codename, Tracker#, Issues, Date, Status
func TestRunBranchListWithDeps_DisplaysReleaseTable(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_200",
			Number: 200,
			Title:  "Branch: v2.0.0 (Phoenix)",
			State:  "OPEN",
		},
	}
	mock.closedIssues = []api.Issue{
		{
			ID:     "TRACKER_100",
			Number: 100,
			Title:  "Branch: v1.0.0",
			State:  "CLOSED",
		},
	}

	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, buf := newTestBranchCmd()
	opts := &branchListOptions{}

	// ACT
	err := runBranchListWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()

	// Verify headers
	if !strings.Contains(output, "VERSION") {
		t.Errorf("Expected output to contain 'VERSION' header, got '%s'", output)
	}
	if !strings.Contains(output, "STATUS") {
		t.Errorf("Expected output to contain 'STATUS' header, got '%s'", output)
	}
	if !strings.Contains(output, "TRACKER") {
		t.Errorf("Expected output to contain 'TRACKER' header, got '%s'", output)
	}

	// Verify release data
	if !strings.Contains(output, "v2.0.0") {
		t.Errorf("Expected output to contain 'v2.0.0', got '%s'", output)
	}
	if !strings.Contains(output, "v1.0.0") {
		t.Errorf("Expected output to contain 'v1.0.0', got '%s'", output)
	}
	if !strings.Contains(output, "Phoenix") {
		t.Errorf("Expected output to contain codename 'Phoenix', got '%s'", output)
	}
}

// AC-022-2: Given multiple releases, Then sorted by version descending
func TestRunBranchListWithDeps_SortedByVersionDescending(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{}
	mock.closedIssues = []api.Issue{
		{
			ID:     "TRACKER_100",
			Number: 100,
			Title:  "Branch: v1.0.0",
			State:  "CLOSED",
		},
		{
			ID:     "TRACKER_300",
			Number: 300,
			Title:  "Branch: v3.0.0",
			State:  "CLOSED",
		},
		{
			ID:     "TRACKER_200",
			Number: 200,
			Title:  "Branch: v2.0.0",
			State:  "CLOSED",
		},
	}

	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, buf := newTestBranchCmd()
	opts := &branchListOptions{}

	// ACT
	err := runBranchListWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()

	// Find positions of versions in output - v3.0.0 should appear before v2.0.0, which should appear before v1.0.0
	pos3 := strings.Index(output, "v3.0.0")
	pos2 := strings.Index(output, "v2.0.0")
	pos1 := strings.Index(output, "v1.0.0")

	if pos3 > pos2 {
		t.Errorf("Expected v3.0.0 to appear before v2.0.0 (descending order)")
	}
	if pos2 > pos1 {
		t.Errorf("Expected v2.0.0 to appear before v1.0.0 (descending order)")
	}
}

// Test no releases shows message
func TestRunBranchListWithDeps_NoReleases(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{}
	mock.closedIssues = []api.Issue{}

	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, buf := newTestBranchCmd()
	opts := &branchListOptions{}

	// ACT
	err := runBranchListWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No branches found") {
		t.Errorf("Expected output to contain 'No branches found', got '%s'", output)
	}
}

// Test release list API error handling
func TestRunBranchListWithDeps_OpenIssuesError(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.getOpenIssuesErr = errors.New("API error")

	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchListOptions{}

	// ACT
	err := runBranchListWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get open branches") {
		t.Errorf("Expected error about open branches, got: %v", err)
	}
}

func TestRunBranchListWithDeps_ClosedIssuesError(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{}
	mock.getClosedIssuesErr = errors.New("API error")

	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchListOptions{}

	// ACT
	err := runBranchListWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get closed branches") {
		t.Errorf("Expected error about closed branches, got: %v", err)
	}
}

// ============================================================================
// runBranchReopenWithDeps Tests
// ============================================================================

func TestRunBranchReopenWithDeps_Success(t *testing.T) {
	mock := setupMockForBranch()
	mock.closedIssues = []api.Issue{
		{ID: "closed-1", Number: 100, Title: "Branch: v1.0.0"},
	}

	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, buf := newTestBranchCmd()

	err := runBranchReopenWithDeps(cmd, "v1.0.0", cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Reopened branch v1.0.0") {
		t.Errorf("expected 'Reopened branch v1.0.0' in output, got: %s", output)
	}
}

func TestRunBranchReopenWithDeps_WithCodename(t *testing.T) {
	mock := setupMockForBranch()
	mock.closedIssues = []api.Issue{
		{ID: "closed-1", Number: 100, Title: "Branch: v1.0.0 (Phoenix)"},
	}

	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, buf := newTestBranchCmd()

	err := runBranchReopenWithDeps(cmd, "v1.0.0", cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Reopened branch v1.0.0") {
		t.Errorf("expected 'Reopened branch v1.0.0' in output, got: %s", output)
	}
}

func TestRunBranchReopenWithDeps_ReleaseNotFound(t *testing.T) {
	mock := setupMockForBranch()
	mock.closedIssues = []api.Issue{
		{ID: "closed-1", Number: 100, Title: "Branch: v2.0.0"},
	}

	cfg := testBranchConfig()
	cmd, _ := newTestBranchCmd()

	err := runBranchReopenWithDeps(cmd, "v1.0.0", cfg, mock)
	if err == nil {
		t.Fatal("expected error for branch not found")
	}
	if !strings.Contains(err.Error(), "closed branch not found") {
		t.Errorf("expected 'closed branch not found' error, got: %v", err)
	}
}

func TestRunBranchReopenWithDeps_GetClosedIssuesError(t *testing.T) {
	mock := setupMockForBranch()
	mock.getClosedIssuesErr = errors.New("API error")

	cfg := testBranchConfig()
	cmd, _ := newTestBranchCmd()

	err := runBranchReopenWithDeps(cmd, "v1.0.0", cfg, mock)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to get closed branch issues") {
		t.Errorf("expected 'failed to get closed branch issues' error, got: %v", err)
	}
}

func TestRunBranchReopenWithDeps_ReopenError(t *testing.T) {
	mock := setupMockForBranch()
	mock.closedIssues = []api.Issue{
		{ID: "closed-1", Number: 100, Title: "Branch: v1.0.0"},
	}
	mock.reopenIssueErr = errors.New("reopen failed")

	cfg := testBranchConfig()
	cmd, _ := newTestBranchCmd()

	err := runBranchReopenWithDeps(cmd, "v1.0.0", cfg, mock)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to reopen tracker issue") {
		t.Errorf("expected 'failed to reopen tracker issue' error, got: %v", err)
	}
}

func TestRunBranchReopenWithDeps_NoRepositories(t *testing.T) {
	mock := setupMockForBranch()
	cfg := testBranchConfig()
	cfg.Repositories = []string{}

	cmd, _ := newTestBranchCmd()

	err := runBranchReopenWithDeps(cmd, "v1.0.0", cfg, mock)
	if err == nil {
		t.Fatal("expected error for no repositories")
	}
	if !strings.Contains(err.Error(), "no repositories") {
		t.Errorf("expected 'no repositories' error, got: %v", err)
	}
}

// =============================================================================
// generateBranchTrackerTemplate Tests
// =============================================================================

func TestGenerateBranchTrackerTemplate_ContainsBranchName(t *testing.T) {
	branch := "release/v1.2.0"
	result := generateBranchTrackerTemplate(branch)

	if !strings.Contains(result, "`"+branch+"`") {
		t.Errorf("Template should contain branch name in backticks, got: %s", result)
	}
}

func TestGenerateBranchTrackerTemplate_ContainsWarnings(t *testing.T) {
	result := generateBranchTrackerTemplate("release/v1.0.0")

	warnings := []string{
		"**Branch Tracker Issue**",
		"**Do not manually:**",
		"Close or reopen this issue",
		"Change the title",
		"Remove the `branch` label",
	}

	for _, warning := range warnings {
		if !strings.Contains(result, warning) {
			t.Errorf("Template should contain warning %q", warning)
		}
	}
}

func TestGenerateBranchTrackerTemplate_ContainsCommands(t *testing.T) {
	branch := "release/v1.0.0"
	result := generateBranchTrackerTemplate(branch)

	commands := []string{
		"`gh pmu branch add <issue>`",
		"`gh pmu branch remove <issue>`",
		"`gh pmu branch close " + branch + "`",
	}

	for _, cmd := range commands {
		if !strings.Contains(result, cmd) {
			t.Errorf("Template should contain command %q", cmd)
		}
	}
}

func TestGenerateBranchTrackerTemplate_ContainsIssuesSection(t *testing.T) {
	result := generateBranchTrackerTemplate("release/v1.0.0")

	if !strings.Contains(result, "## Issues in this branch") {
		t.Error("Template should contain 'Issues in this branch' section")
	}
	if !strings.Contains(result, "Branch field in the project") {
		t.Error("Template should explain issues are tracked via the Branch field")
	}
}

func TestGenerateBranchTrackerTemplate_DifferentBranchFormats(t *testing.T) {
	tests := []struct {
		branch string
	}{
		{"release/v1.0.0"},
		{"patch/v1.0.1"},
		{"hotfix-auth-bypass"},
		{"v2.0.0-beta"},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			result := generateBranchTrackerTemplate(tt.branch)
			if !strings.Contains(result, "`"+tt.branch+"`") {
				t.Errorf("Template should contain branch name %q in backticks", tt.branch)
			}
			if !strings.Contains(result, "gh pmu branch close "+tt.branch) {
				t.Errorf("Template should contain close command with branch name %q", tt.branch)
			}
		})
	}
}

func TestBranchCalculateNextVersions(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		wantPatch      string
		wantMinor      string
		wantMajor      string
		wantErr        bool
	}{
		{
			name:           "standard version",
			currentVersion: "v1.2.3",
			wantPatch:      "v1.2.4",
			wantMinor:      "v1.3.0",
			wantMajor:      "v2.0.0",
		},
		{
			name:           "without v prefix",
			currentVersion: "1.2.3",
			wantPatch:      "v1.2.4",
			wantMinor:      "v1.3.0",
			wantMajor:      "v2.0.0",
		},
		{
			name:           "zero version",
			currentVersion: "v0.0.0",
			wantPatch:      "v0.0.1",
			wantMinor:      "v0.1.0",
			wantMajor:      "v1.0.0",
		},
		{
			name:           "invalid format",
			currentVersion: "invalid",
			wantErr:        true,
		},
		{
			name:           "incomplete version",
			currentVersion: "v1.2",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			versions, err := calculateNextVersions(tt.currentVersion)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if versions.patch != tt.wantPatch {
				t.Errorf("patch: got %s, want %s", versions.patch, tt.wantPatch)
			}
			if versions.minor != tt.wantMinor {
				t.Errorf("minor: got %s, want %s", versions.minor, tt.wantMinor)
			}
			if versions.major != tt.wantMajor {
				t.Errorf("major: got %s, want %s", versions.major, tt.wantMajor)
			}
		})
	}
}

// =============================================================================
// Command Flag Existence Tests
// =============================================================================

func TestBranchStartCommand_HasNameFlag(t *testing.T) {
	cmd := NewRootCommand()
	startCmd, _, err := cmd.Find([]string{"branch", "start"})
	if err != nil {
		t.Fatalf("branch start command not found: %v", err)
	}

	flag := startCmd.Flags().Lookup("name")
	if flag == nil {
		t.Fatal("Expected --name flag to exist")
	}
}

func TestBranchCloseCommand_Flags(t *testing.T) {
	cmd := NewRootCommand()
	closeCmd, _, err := cmd.Find([]string{"branch", "close"})
	if err != nil {
		t.Fatalf("branch close command not found: %v", err)
	}

	tests := []struct {
		flag      string
		shorthand string
	}{
		{"yes", "y"},
		{"tag", ""},
	}

	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			flag := closeCmd.Flags().Lookup(tt.flag)
			if flag == nil {
				t.Fatalf("Expected --%s flag to exist", tt.flag)
			}
			if tt.shorthand != "" && flag.Shorthand != tt.shorthand {
				t.Errorf("Expected --%s shorthand to be '%s', got '%s'", tt.flag, tt.shorthand, flag.Shorthand)
			}
		})
	}
}

func TestBranchCurrentCommand_HasJsonFlag(t *testing.T) {
	cmd := NewRootCommand()
	currentCmd, _, err := cmd.Find([]string{"branch", "current"})
	if err != nil {
		t.Fatalf("branch current command not found: %v", err)
	}

	flag := currentCmd.Flags().Lookup("json")
	if flag == nil {
		t.Fatal("Expected --json flag to exist")
	}
}

func TestBranchCurrentCommand_JSONSpaceFormMatchesEqualsForm(t *testing.T) {
	spaced := newBranchCurrentCommand()
	if err := parseJSONFlagCommand(t, spaced, []string{"--json", "tracker"}); err != nil {
		t.Fatalf("--json tracker: unexpected error: %v", err)
	}
	equals := newBranchCurrentCommand()
	if err := parseJSONFlagCommand(t, equals, []string{"--json=tracker"}); err != nil {
		t.Fatalf("--json=tracker: unexpected error: %v", err)
	}
	a, _ := spaced.Flags().GetString("json")
	b, _ := equals.Flags().GetString("json")
	if a != b || a != "tracker" {
		t.Errorf("space form parsed %q, equals form parsed %q; want both \"tracker\"", a, b)
	}
}

func TestBranchCurrentCommand_JSONValueIsRequired(t *testing.T) {
	if err := newBranchCurrentCommand().ParseFlags([]string{"--json"}); err == nil {
		t.Error("Expected bare --json to be rejected; use --json-fields to list fields")
	}
	err := parseJSONFlagCommand(t, newBranchCurrentCommand(), []string{"--json="})
	if err == nil {
		t.Fatal("Expected --json= with no fields to be rejected")
	}
	if !strings.Contains(err.Error(), "--json-fields") {
		t.Errorf("Expected error to point at --json-fields, got: %v", err)
	}
}

func TestBranchCurrentCommand_JSONUnknownFieldRejected(t *testing.T) {
	err := parseJSONFlagCommand(t, newBranchCurrentCommand(), []string{"--json", "status"})
	if err == nil {
		t.Fatal("Expected an unknown --json field to be rejected rather than silently dropped")
	}
	if !strings.Contains(err.Error(), "--json=status") {
		t.Errorf("Expected error to name --json=status, got: %v", err)
	}
}

func TestBranchCurrentCommand_JSONFieldsListsFields(t *testing.T) {
	cmd := newBranchCurrentCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--json-fields"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("branch current --json-fields: unexpected error: %v", err)
	}
	for _, field := range []string{"issues", "name", "tracker"} {
		if !strings.Contains(buf.String(), field) {
			t.Errorf("Expected --json-fields output to include %q, got:\n%s", field, buf.String())
		}
	}
}

func TestBranchAddCommand_Structure(t *testing.T) {
	cmd := NewRootCommand()
	addCmd, _, err := cmd.Find([]string{"branch", "add"})
	if err != nil {
		t.Fatalf("branch add command not found: %v", err)
	}

	if addCmd.Use != "add <issue-number>" {
		t.Errorf("Expected Use 'add <issue-number>', got %s", addCmd.Use)
	}

	// Requires exactly 1 argument
	if err := addCmd.Args(addCmd, []string{}); err == nil {
		t.Error("Expected error when no arguments provided")
	}
	if err := addCmd.Args(addCmd, []string{"123"}); err != nil {
		t.Errorf("Unexpected error with one argument: %v", err)
	}
}

func TestBranchRemoveCommand_Structure(t *testing.T) {
	cmd := NewRootCommand()
	removeCmd, _, err := cmd.Find([]string{"branch", "remove"})
	if err != nil {
		t.Fatalf("branch remove command not found: %v", err)
	}

	if removeCmd.Use != "remove <issue-number>" {
		t.Errorf("Expected Use 'remove <issue-number>', got %s", removeCmd.Use)
	}

	// Requires exactly 1 argument
	if err := removeCmd.Args(removeCmd, []string{}); err == nil {
		t.Error("Expected error when no arguments provided")
	}
	if err := removeCmd.Args(removeCmd, []string{"123"}); err != nil {
		t.Errorf("Unexpected error with one argument: %v", err)
	}
}

func TestBranchListCommand_Structure(t *testing.T) {
	cmd := NewRootCommand()
	listCmd, _, err := cmd.Find([]string{"branch", "list"})
	if err != nil {
		t.Fatalf("branch list command not found: %v", err)
	}

	if listCmd.Use != "list" {
		t.Errorf("Expected Use 'list', got %s", listCmd.Use)
	}

	if listCmd.Short == "" {
		t.Error("Expected Short description to be set")
	}
}

func TestBranchReopenCommand_Structure(t *testing.T) {
	cmd := NewRootCommand()
	reopenCmd, _, err := cmd.Find([]string{"branch", "reopen"})
	if err != nil {
		t.Fatalf("branch reopen command not found: %v", err)
	}

	if reopenCmd.Use != "reopen <branch-name>" {
		t.Errorf("Expected Use 'reopen <branch-name>', got %s", reopenCmd.Use)
	}

	// Requires exactly 1 argument
	if err := reopenCmd.Args(reopenCmd, []string{}); err == nil {
		t.Error("Expected error when no arguments provided")
	}
	if err := reopenCmd.Args(reopenCmd, []string{"release/v1.0.0"}); err != nil {
		t.Errorf("Unexpected error with one argument: %v", err)
	}
}

func TestBranchCompareVersions_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected int
	}{
		{"equal", "v1.0.0", "v1.0.0", 0},
		{"a greater major", "v2.0.0", "v1.0.0", 1},
		{"b greater major", "v1.0.0", "v2.0.0", -1},
		{"a greater minor", "v1.2.0", "v1.1.0", 1},
		{"b greater minor", "v1.1.0", "v1.2.0", -1},
		{"a greater patch", "v1.0.2", "v1.0.1", 1},
		{"b greater patch", "v1.0.1", "v1.0.2", -1},
		{"without prefix", "1.0.0", "1.0.0", 0},
		{"mixed prefix", "v1.0.0", "1.0.0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareVersions(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// Parking Lot Exclusion Tests
// ============================================================================

func TestRunBranchCloseWithDeps_SkipsParkingLotIssues(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	// 3 incomplete sub-issues: 1 parking lot, 2 regular
	mock.subIssues = []api.SubIssue{
		{ID: "ISSUE_1", Number: 41, Title: "Parked feature idea", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
		{ID: "ISSUE_2", Number: 42, Title: "Incomplete work", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
		{ID: "ISSUE_3", Number: 43, Title: "Another incomplete", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	// Map issue IDs to item IDs
	mock.projectItemIDs = map[string]string{
		"ISSUE_1": "ITEM_1",
		"ISSUE_2": "ITEM_2",
		"ISSUE_3": "ITEM_3",
	}
	// ISSUE_1 is in Parking Lot status
	mock.projectItemFieldValues = map[string]string{
		"ITEM_1": "Parking Lot",
		"ITEM_2": "In Progress",
		"ITEM_3": "Ready",
	}

	cfg := testBranchConfig()
	cfg.Fields["status"] = config.Field{
		Field: "Status",
		Values: map[string]string{
			"backlog":     "Backlog",
			"parking_lot": "Parking Lot",
		},
	}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, output := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify output shows skipped parking lot issues
	outputStr := output.String()
	if !strings.Contains(outputStr, "Skipping 1 Parking Lot issue") {
		t.Errorf("Expected output to mention skipped parking lot issues, got: %s", outputStr)
	}

	// Verify only 2 issues were moved to backlog (not the parking lot one)
	statusSetCount := 0
	for _, call := range mock.setFieldCalls {
		if call.fieldID == "Status" && call.value == "Backlog" {
			statusSetCount++
		}
	}
	if statusSetCount != 2 {
		t.Errorf("Expected 2 issues moved to backlog, got %d. Calls: %+v", statusSetCount, mock.setFieldCalls)
	}

	// Verify output message reports correct count (2, not 3)
	if !strings.Contains(outputStr, "2 issue(s) moved to backlog") {
		t.Errorf("Expected output to say '2 issue(s) moved to backlog', got: %s", outputStr)
	}
}

// TestRunBranchCloseWithDeps_StatusReadFailureWarnsAndTreatsAsMovable covers the
// path #888 travelled through undetected. GetProjectItemFieldValue sent invalid
// GraphQL, so every status read failed; the error was discarded, status was "",
// nothing ever equalled parkingLotValue, and every issue was silently
// reclassified as movable. Nothing in the output said so.
//
// The classification stays fail-safe (an unreadable status means "movable", not
// "parked"), but it must be announced. A silent read failure here is the exact
// shape of the original defect: no error, no warning, wrong answer.
func TestRunBranchCloseWithDeps_StatusReadFailureWarnsAndTreatsAsMovable(t *testing.T) {
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN"},
	}
	mock.subIssues = []api.SubIssue{
		{ID: "ISSUE_1", Number: 41, Title: "Parked feature idea", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	mock.projectItemIDs = map[string]string{"ISSUE_1": "ITEM_1"}
	// The issue really is parked, but the status read fails — as it did against
	// the live API while the query was malformed.
	mock.projectItemFieldValues = map[string]string{"ITEM_1": "Parking Lot"}
	mock.getProjectItemFieldErr = errors.New("failed to get field value: Cannot query field \"name\" on type \"ProjectV2FieldConfiguration\"")

	cfg := testBranchConfig()
	cfg.Fields["status"] = config.Field{
		Field:  "Status",
		Values: map[string]string{"backlog": "Backlog", "parking_lot": "Parking Lot"},
	}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, output := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	if err := runBranchCloseWithDeps(cmd, opts, cfg, mock); err != nil {
		t.Fatalf("expected the close to proceed despite the status read failure, got: %v", err)
	}

	out := output.String()
	if !strings.Contains(out, "Warning") || !strings.Contains(out, "#41") {
		t.Errorf("expected a warning naming #41 whose status could not be read, got: %s", out)
	}
	if !strings.Contains(out, "could not read status") {
		t.Errorf("expected the warning to say the status was unreadable, got: %s", out)
	}

	// Fail-safe classification: unreadable status means the issue is moved, not
	// silently left behind as if it were parked.
	moved := 0
	for _, call := range mock.setFieldCalls {
		if call.fieldID == "Status" && call.value == "Backlog" {
			moved++
		}
	}
	if moved != 1 {
		t.Errorf("expected #41 to be moved to backlog when its status is unreadable, got %d moves: %+v", moved, mock.setFieldCalls)
	}
	if strings.Contains(out, "Skipping 1 Parking Lot issue") {
		t.Errorf("an unreadable status must not be reported as a confirmed Parking Lot skip, got: %s", out)
	}
}

func TestRunBranchCloseWithDeps_FieldSetFailureWarns(t *testing.T) {
	// #871 finding 3: SetProjectItemField errors were ignored (`_ =`) while moving
	// incomplete issues to backlog, yet the issue was reported as moved. A failure
	// must now emit a stderr warning naming the issue.
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN"},
	}
	mock.subIssues = []api.SubIssue{
		{ID: "ISSUE_2", Number: 42, Title: "Incomplete work", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	mock.projectItemIDs = map[string]string{"ISSUE_2": "ITEM_2"}
	mock.projectItemFieldValues = map[string]string{"ITEM_2": "In Progress"}
	mock.setFieldErr = errors.New("field update failed")

	cfg := testBranchConfig()
	cfg.Fields["status"] = config.Field{
		Field:  "Status",
		Values: map[string]string{"backlog": "Backlog", "parking_lot": "Parking Lot"},
	}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, output := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	_ = runBranchCloseWithDeps(cmd, opts, cfg, mock)

	out := output.String()
	if !strings.Contains(out, "Warning") || !strings.Contains(out, "#42") {
		t.Errorf("expected a stderr warning naming #42's failed field update, got: %s", out)
	}
}

func TestRunBranchCloseWithDeps_AllParkingLotNoMoves(t *testing.T) {
	// ARRANGE: All incomplete issues are in Parking Lot
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	// Sub-issues — all open (parking lot status determined via project field lookup)
	mock.subIssues = []api.SubIssue{
		{ID: "ISSUE_1", Number: 41, Title: "Parked idea 1", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
		{ID: "ISSUE_2", Number: 42, Title: "Parked idea 2", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	mock.projectItemIDs = map[string]string{
		"ISSUE_1": "ITEM_1",
		"ISSUE_2": "ITEM_2",
	}
	mock.projectItemFieldValues = map[string]string{
		"ITEM_1": "Parking Lot",
		"ITEM_2": "Parking Lot",
	}

	cfg := testBranchConfig()
	cfg.Fields["status"] = config.Field{
		Field: "Status",
		Values: map[string]string{
			"backlog":     "Backlog",
			"parking_lot": "Parking Lot",
		},
	}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, output := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify output shows 2 parking lot issues skipped
	outputStr := output.String()
	if !strings.Contains(outputStr, "Skipping 2 Parking Lot issue") {
		t.Errorf("Expected output to mention skipped parking lot issues, got: %s", outputStr)
	}

	// Verify no status changes to Backlog
	for _, call := range mock.setFieldCalls {
		if call.fieldID == "Status" && call.value == "Backlog" {
			t.Errorf("No issues should be moved to backlog, but found call: %+v", call)
		}
	}

	// Verify "Moving incomplete issues" message is NOT shown
	if strings.Contains(outputStr, "Moving incomplete issues") {
		t.Errorf("Should not show 'Moving incomplete issues' when all are parking lot")
	}
}

func TestRunBranchCloseWithDeps_NoParkingLotConfig(t *testing.T) {
	// ARRANGE: No parking_lot value configured, should use default "Parking Lot"
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	// Sub-issues — parking lot status determined via project field lookup
	mock.subIssues = []api.SubIssue{
		{ID: "ISSUE_1", Number: 41, Title: "Parked idea", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
		{ID: "ISSUE_2", Number: 42, Title: "Regular issue", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	mock.projectItemIDs = map[string]string{
		"ISSUE_1": "ITEM_1",
		"ISSUE_2": "ITEM_2",
	}
	mock.projectItemFieldValues = map[string]string{
		"ITEM_1": "Parking Lot", // Uses default value
		"ITEM_2": "In Progress",
	}

	cfg := testBranchConfig()
	// Status field configured but no parking_lot alias
	cfg.Fields["status"] = config.Field{
		Field: "Status",
		Values: map[string]string{
			"backlog": "Backlog",
		},
	}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, output := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should still skip parking lot with default value
	outputStr := output.String()
	if !strings.Contains(outputStr, "Skipping 1 Parking Lot issue") {
		t.Errorf("Expected parking lot issue to be skipped even without config, got: %s", outputStr)
	}
}

func TestRunBranchCloseWithDeps_ClearsBranchField(t *testing.T) {
	// ARRANGE: Incomplete issues that need to be moved to backlog
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	// Sub-issue — open, will be moved to backlog
	mock.subIssues = []api.SubIssue{
		{ID: "ISSUE_1", Number: 41, Title: "Incomplete work", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	mock.projectItemIDs = map[string]string{
		"ISSUE_1": "ITEM_1",
	}
	mock.projectItemFieldValues = map[string]string{
		"ITEM_1": "In Progress",
	}

	cfg := testBranchConfig()
	cfg.Fields["status"] = config.Field{
		Field: "Status",
		Values: map[string]string{
			"backlog": "Backlog",
		},
	}
	cfg.Fields["branch"] = config.Field{
		Field: "Release",
	}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify Release field was cleared (set to "")
	releaseCleared := false
	for _, call := range mock.setFieldCalls {
		if call.fieldID == "Release" && call.value == "" {
			releaseCleared = true
			break
		}
	}
	if !releaseCleared {
		t.Errorf("Expected Release field to be cleared, calls: %+v", mock.setFieldCalls)
	}

	// Verify Status was set to Backlog
	statusSet := false
	for _, call := range mock.setFieldCalls {
		if call.fieldID == "Status" && call.value == "Backlog" {
			statusSet = true
			break
		}
	}
	if !statusSet {
		t.Errorf("Expected Status field to be set to Backlog, calls: %+v", mock.setFieldCalls)
	}
}

func TestRunBranchCloseWithDeps_GetProjectItemIDError_ContinuesWithWarning(t *testing.T) {
	// ARRANGE: GetProjectItemID fails for one issue but succeeds for another
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	// Sub-issues — both open, ISSUE_1 will fail project item lookup
	mock.subIssues = []api.SubIssue{
		{ID: "ISSUE_1", Number: 41, Title: "Issue without project item", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
		{ID: "ISSUE_2", Number: 42, Title: "Normal issue", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	// Only ISSUE_2 has a project item ID - ISSUE_1 will fail lookup and show warning
	mock.projectItemIDs = map[string]string{
		"ISSUE_2": "ITEM_2",
	}
	mock.projectItemFieldValues = map[string]string{
		"ITEM_2": "In Progress",
	}

	cfg := testBranchConfig()
	cfg.Fields["status"] = config.Field{
		Field: "Status",
		Values: map[string]string{
			"backlog": "Backlog",
		},
	}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT: Should succeed overall
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify warning was shown for issue 41
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "Warning") || !strings.Contains(stderrStr, "#41") {
		t.Errorf("Expected warning about issue #41, got stderr: %s", stderrStr)
	}

	// Verify only 1 issue had status set (the one that succeeded)
	statusSetCount := 0
	for _, call := range mock.setFieldCalls {
		if call.fieldID == "Status" && call.value == "Backlog" {
			statusSetCount++
		}
	}
	if statusSetCount != 1 {
		t.Errorf("Expected 1 issue moved to backlog, got %d", statusSetCount)
	}
}

func TestRunBranchCloseWithDeps_AllIssuesDone_NoMoveToBacklog(t *testing.T) {
	// ARRANGE: All release issues are closed (done)
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	// Sub-issues — all closed (done)
	mock.subIssues = []api.SubIssue{
		{ID: "ISSUE_1", Number: 41, Title: "Completed work", State: "CLOSED", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
		{ID: "ISSUE_2", Number: 42, Title: "Also done", State: "CLOSED", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}

	cfg := testBranchConfig()
	cfg.Fields["status"] = config.Field{
		Field: "Status",
		Values: map[string]string{
			"backlog": "Backlog",
		},
	}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, output := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify output shows 2 done, 0 incomplete
	outputStr := output.String()
	if !strings.Contains(outputStr, "2 done") || !strings.Contains(outputStr, "0 incomplete") {
		t.Errorf("Expected '2 done, 0 incomplete' in output, got: %s", outputStr)
	}

	// Verify no backlog status updates (all issues already done)
	backlogCount := 0
	for _, call := range mock.setFieldCalls {
		if call.fieldID == "Status" && call.value == "Backlog" {
			backlogCount++
		}
	}
	if backlogCount != 0 {
		t.Errorf("Expected no backlog status updates (all done), got %d: %+v", backlogCount, mock.setFieldCalls)
	}
}

func TestRunBranchCloseWithDeps_DefaultBacklogValue(t *testing.T) {
	// ARRANGE: Status field has no backlog alias defined
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	// Sub-issue — open, will be moved to backlog
	mock.subIssues = []api.SubIssue{
		{ID: "ISSUE_1", Number: 41, Title: "Incomplete work", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	mock.projectItemIDs = map[string]string{
		"ISSUE_1": "ITEM_1",
	}
	mock.projectItemFieldValues = map[string]string{
		"ITEM_1": "In Progress",
	}

	cfg := testBranchConfig()
	// Status field with empty values map (no backlog alias)
	cfg.Fields["status"] = config.Field{
		Field:  "Status",
		Values: map[string]string{}, // No backlog alias
	}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify Status was set to default "Backlog" (not an alias)
	statusSet := false
	for _, call := range mock.setFieldCalls {
		if call.fieldID == "Status" && call.value == "Backlog" {
			statusSet = true
			break
		}
	}
	if !statusSet {
		t.Errorf("Expected Status field to be set to default 'Backlog', calls: %+v", mock.setFieldCalls)
	}
}

// ============================================================================
// Release Close Default to Current Tests (Issue #479)
// ============================================================================

func TestFindAllActiveBranches(t *testing.T) {
	tests := []struct {
		name     string
		issues   []api.Issue
		expected int
	}{
		{
			name:     "no issues",
			issues:   []api.Issue{},
			expected: 0,
		},
		{
			name: "no release issues",
			issues: []api.Issue{
				{Title: "Bug: something broken"},
				{Title: "Feature: new thing"},
			},
			expected: 0,
		},
		{
			name: "one release",
			issues: []api.Issue{
				{Title: "Branch: v1.0.0"},
			},
			expected: 1,
		},
		{
			name: "multiple releases",
			issues: []api.Issue{
				{Title: "Branch: v1.0.0"},
				{Title: "Branch: patch/v1.0.1"},
				{Title: "Bug: something"},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findAllActiveBranches(tt.issues)
			if len(result) != tt.expected {
				t.Errorf("findAllActiveBranches() returned %d releases, want %d", len(result), tt.expected)
			}
		})
	}
}

// =============================================================================
// #862: branch add/close membership symmetry + multi-active-branch disambiguation
// =============================================================================

// AC1: branch add links the issue as a sub-issue of the tracker so branch close
// (which enumerates via GetSubIssues) sees it.
func TestRunBranchAddWithDeps_LinksSubIssue(t *testing.T) {
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN"},
	}
	mock.issueByNumber = &api.Issue{ID: "ISSUE_42", Number: 42, Title: "Fix login bug", State: "OPEN"}
	mock.projectItemID = "ITEM_42"

	cfg := testBranchConfig()
	cfg.Fields["branch"] = config.Field{Field: "Branch"}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	if err := runBranchAddWithDeps(cmd, &branchAddOptions{issueNumber: 42}, cfg, mock); err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(mock.addSubIssueCalls) != 1 {
		t.Fatalf("Expected 1 AddSubIssue call, got %d", len(mock.addSubIssueCalls))
	}
	call := mock.addSubIssueCalls[0]
	if call.parentIssueID != "TRACKER_123" || call.childIssueID != "ISSUE_42" {
		t.Errorf("Expected AddSubIssue(TRACKER_123, ISSUE_42), got (%s, %s)", call.parentIssueID, call.childIssueID)
	}
}

// AC2: branch add errors with a disambiguation list when multiple branches are active.
func TestRunBranchAddWithDeps_MultipleActiveBranches_ReturnsDisambiguation(t *testing.T) {
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{ID: "T1", Number: 100, Title: "Branch: release/v1.0.0", State: "OPEN"},
		{ID: "T2", Number: 101, Title: "Branch: patch/v1.0.1", State: "OPEN"},
	}
	cfg := testBranchConfig()
	cfg.Fields["branch"] = config.Field{Field: "Branch"}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	err := runBranchAddWithDeps(cmd, &branchAddOptions{issueNumber: 42}, cfg, mock)
	if err == nil {
		t.Fatal("Expected error for multiple active branches, got nil")
	}
	expected := "multiple active branches. Specify one: release/v1.0.0, patch/v1.0.1"
	if err.Error() != expected {
		t.Errorf("Expected '%s', got: %s", expected, err.Error())
	}
	// Must not silently pick one and mutate.
	if len(mock.setFieldCalls) != 0 || len(mock.addSubIssueCalls) != 0 {
		t.Errorf("Expected no mutations on ambiguous add, got setField=%d addSubIssue=%d",
			len(mock.setFieldCalls), len(mock.addSubIssueCalls))
	}
}

// AC1: branch remove unlinks the sub-issue relationship established by branch add.
func TestRunBranchRemoveWithDeps_UnlinksSubIssue(t *testing.T) {
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN"},
	}
	mock.issueByNumber = &api.Issue{ID: "ISSUE_42", Number: 42, Title: "Fix login bug", State: "OPEN"}
	mock.projectItemID = "ITEM_42"
	mock.projectItemFieldValue = "v1.2.0" // currently assigned

	cfg := testBranchConfig()
	cfg.Fields["branch"] = config.Field{Field: "Branch"}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	if err := runBranchRemoveWithDeps(cmd, &branchRemoveOptions{issueNumber: 42}, cfg, mock); err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(mock.removeSubIssueCalls) != 1 {
		t.Fatalf("Expected 1 RemoveSubIssue call, got %d", len(mock.removeSubIssueCalls))
	}
	call := mock.removeSubIssueCalls[0]
	if call.parentIssueID != "TRACKER_123" || call.childIssueID != "ISSUE_42" {
		t.Errorf("Expected RemoveSubIssue(TRACKER_123, ISSUE_42), got (%s, %s)", call.parentIssueID, call.childIssueID)
	}
}

// AC2: branch remove errors with a disambiguation list when multiple branches are active.
func TestRunBranchRemoveWithDeps_MultipleActiveBranches_ReturnsDisambiguation(t *testing.T) {
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{ID: "T1", Number: 100, Title: "Branch: release/v1.0.0", State: "OPEN"},
		{ID: "T2", Number: 101, Title: "Branch: patch/v1.0.1", State: "OPEN"},
	}
	cfg := testBranchConfig()
	cfg.Fields["branch"] = config.Field{Field: "Branch"}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	err := runBranchRemoveWithDeps(cmd, &branchRemoveOptions{issueNumber: 42}, cfg, mock)
	if err == nil {
		t.Fatal("Expected error for multiple active branches, got nil")
	}
	expected := "multiple active branches. Specify one: release/v1.0.0, patch/v1.0.1"
	if err.Error() != expected {
		t.Errorf("Expected '%s', got: %s", expected, err.Error())
	}
}

// AC3: branch current errors with a disambiguation list when the fast-path label
// query returns multiple active branch trackers.
func TestRunBranchCurrentWithDeps_MultipleActiveBranches_ReturnsDisambiguation(t *testing.T) {
	mock := setupMockForBranch()
	mock.openIssuesByLabels = []api.Issue{
		{ID: "T1", Number: 100, Title: "Branch: release/v1.0.0", State: "OPEN"},
		{ID: "T2", Number: 101, Title: "Branch: patch/v1.0.1", State: "OPEN"},
	}
	cfg := testBranchConfig()
	cmd, _ := newTestBranchCmd()

	err := runBranchCurrentWithDeps(cmd, &branchCurrentOptions{}, cfg, mock)
	if err == nil {
		t.Fatal("Expected error for multiple active branches, got nil")
	}
	expected := "multiple active branches. Specify one: release/v1.0.0, patch/v1.0.1"
	if err.Error() != expected {
		t.Errorf("Expected '%s', got: %s", expected, err.Error())
	}
}

// AC3: branch current fast path ignores non-tracker issues that happen to carry
// the active+branch labels, falling back to the title scan.
func TestRunBranchCurrentWithDeps_FastPathFiltersNonTrackers(t *testing.T) {
	mock := setupMockForBranch()
	// Fast-path label query returns an issue that is NOT a branch tracker.
	mock.openIssuesByLabels = []api.Issue{
		{ID: "X", Number: 500, Title: "Some mislabeled issue", State: "OPEN"},
	}
	// Fallback title scan finds the real tracker.
	mock.openIssues = []api.Issue{
		{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN", SubIssueCount: 3},
	}
	cfg := testBranchConfig()
	cmd, buf := newTestBranchCmd()

	if err := runBranchCurrentWithDeps(cmd, &branchCurrentOptions{}, cfg, mock); err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !strings.Contains(buf.String(), "v1.2.0") {
		t.Errorf("Expected fallback to find 'v1.2.0', got: %s", buf.String())
	}
}

// AC4: round trip — an issue added via branch add is enumerated (and handled) by
// branch close, rather than orphaned with a stale Branch field.
func TestRunBranchAddThenClose_RoundTrip(t *testing.T) {
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN"},
	}
	mock.issueByNumber = &api.Issue{ID: "ISSUE_42", Number: 42, Title: "Fix login bug", State: "OPEN"}
	mock.projectItemID = "ITEM_42"

	cfg := testBranchConfig()
	cfg.Fields["branch"] = config.Field{Field: "Branch"}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	// ACT 1: add #42 to the branch.
	addCmd, _ := newTestBranchCmd()
	if err := runBranchAddWithDeps(addCmd, &branchAddOptions{issueNumber: 42}, cfg, mock); err != nil {
		t.Fatalf("branch add failed: %v", err)
	}
	if len(mock.addSubIssueCalls) != 1 {
		t.Fatalf("Expected branch add to link #42 as a sub-issue, got %d links", len(mock.addSubIssueCalls))
	}

	// ARRANGE 2: GitHub now returns #42 as an (incomplete) sub-issue of the tracker.
	mock.subIssues = []api.SubIssue{
		{ID: "ISSUE_42", Number: 42, Title: "Fix login bug", State: "OPEN",
			Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	mock.setFieldCalls = nil // observe only close's field writes

	// ACT 2: close the branch.
	closeCmd, _ := newTestBranchCmd()
	if err := runBranchCloseWithDeps(closeCmd, &branchCloseOptions{branchName: "v1.2.0", yes: true}, cfg, mock); err != nil {
		t.Fatalf("branch close failed: %v", err)
	}

	// ASSERT: close handled #42 — Branch cleared and status moved to backlog.
	var clearedBranch, movedBacklog bool
	for _, c := range mock.setFieldCalls {
		if c.fieldID == "Branch" && c.value == "" {
			clearedBranch = true
		}
		if c.fieldID == "Status" && c.value == "Backlog" {
			movedBacklog = true
		}
	}
	if !clearedBranch {
		t.Errorf("Expected close to clear Branch field for #42; setFieldCalls=%+v", mock.setFieldCalls)
	}
	if !movedBacklog {
		t.Errorf("Expected close to move #42 to Backlog; setFieldCalls=%+v", mock.setFieldCalls)
	}
}

func TestResolveCurrentBranch_NoActiveReleases(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{} // No active releases
	cfg := testBranchConfig()

	// ACT
	_, err := resolveCurrentBranch(cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatal("Expected error when no active releases, got nil")
	}
	if err.Error() != "no active branch found" {
		t.Errorf("Expected 'no active branch found' error, got: %s", err.Error())
	}
}

func TestResolveCurrentBranch_OneActiveRelease(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{Title: "Branch: patch/0.9.7"},
	}
	cfg := testBranchConfig()

	// ACT
	releaseName, err := resolveCurrentBranch(cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if releaseName != "patch/0.9.7" {
		t.Errorf("Expected 'patch/0.9.7', got '%s'", releaseName)
	}
}

func TestResolveCurrentBranch_MultipleActiveReleases(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{Title: "Branch: release/v1.0.0"},
		{Title: "Branch: patch/v1.0.1"},
	}
	cfg := testBranchConfig()

	// ACT
	_, err := resolveCurrentBranch(cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatal("Expected error when multiple active branches, got nil")
	}
	expectedMsg := "multiple active branches. Specify one: release/v1.0.0, patch/v1.0.1"
	if err.Error() != expectedMsg {
		t.Errorf("Expected '%s' error, got: %s", expectedMsg, err.Error())
	}
}

func TestBranchCloseCommand_AcceptsOptionalArgument(t *testing.T) {
	cmd := NewRootCommand()
	closeCmd, _, err := cmd.Find([]string{"branch", "close"})
	if err != nil {
		t.Fatalf("branch close command not found: %v", err)
	}

	// Should accept 0 arguments
	if err := closeCmd.Args(closeCmd, []string{}); err != nil {
		t.Errorf("Expected no error with 0 arguments, got: %v", err)
	}

	// Should accept 1 argument
	if err := closeCmd.Args(closeCmd, []string{"release/v1.0.0"}); err != nil {
		t.Errorf("Expected no error with 1 argument, got: %v", err)
	}

	// Should reject 2 arguments
	if err := closeCmd.Args(closeCmd, []string{"release/v1.0.0", "extra"}); err == nil {
		t.Error("Expected error with 2 arguments, got nil")
	}
}

func TestBranchCloseCommand_UseDescription(t *testing.T) {
	cmd := NewRootCommand()
	closeCmd, _, err := cmd.Find([]string{"branch", "close"})
	if err != nil {
		t.Fatalf("branch close command not found: %v", err)
	}

	// Should show optional argument in usage
	if closeCmd.Use != "close [branch-name]" {
		t.Errorf("Expected Use 'close [branch-name]', got '%s'", closeCmd.Use)
	}
}

func TestRunBranchCloseWithDeps_DryRun_ShowsPreview(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, buf := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", dryRun: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error in dry-run mode, got: %v", err)
	}

	// Should not close tracker issue in dry-run
	if len(mock.closeIssueCalls) != 0 {
		t.Errorf("Expected 0 CloseIssue calls in dry-run, got %d", len(mock.closeIssueCalls))
	}

	// Should show preview
	output := buf.String()
	if !strings.Contains(output, "[DRY RUN]") {
		t.Error("Expected output to contain '[DRY RUN]'")
	}
	if !strings.Contains(output, "Would close branch: v1.2.0") {
		t.Error("Expected output to contain 'Would close branch: v1.2.0'")
	}
	if !strings.Contains(output, "Would close tracker issue #100") {
		t.Error("Expected output to contain 'Would close tracker issue #100'")
	}
}

func TestBranchCloseCommand_HasDryRunFlag(t *testing.T) {
	cmd := NewRootCommand()
	closeCmd, _, err := cmd.Find([]string{"branch", "close"})
	if err != nil {
		t.Fatalf("branch close command not found: %v", err)
	}

	flag := closeCmd.Flags().Lookup("dry-run")
	if flag == nil {
		t.Fatal("Expected --dry-run flag to exist")
	}

	// Verify it's a boolean flag
	if flag.Value.Type() != "bool" {
		t.Errorf("Expected --dry-run to be bool, got %s", flag.Value.Type())
	}
}

func TestParseBranchTitle(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		wantVersion string
		wantTrack   string
	}{
		{
			name:        "simple version with Branch prefix",
			title:       "Branch: v1.2.0",
			wantVersion: "1.2.0",
			wantTrack:   "stable",
		},
		{
			name:        "simple version with Release prefix",
			title:       "Release: v1.2.0",
			wantVersion: "1.2.0",
			wantTrack:   "stable",
		},
		{
			name:        "version with codename",
			title:       "Release: v1.2.0 (Phoenix)",
			wantVersion: "1.2.0",
			wantTrack:   "stable",
		},
		{
			name:        "patch track",
			title:       "Branch: patch/1.1.1",
			wantVersion: "1.1.1",
			wantTrack:   "patch",
		},
		{
			name:        "beta track",
			title:       "Release: beta/2.0.0",
			wantVersion: "2.0.0",
			wantTrack:   "beta",
		},
		{
			name:        "patch track with v prefix",
			title:       "Branch: patch/v1.1.1",
			wantVersion: "1.1.1",
			wantTrack:   "patch",
		},
		{
			name:        "release track with codename",
			title:       "Branch: release/v2.0.0 (Aurora)",
			wantVersion: "2.0.0",
			wantTrack:   "release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVersion, gotTrack := parseBranchTitle(tt.title)
			if gotVersion != tt.wantVersion {
				t.Errorf("parseBranchTitle(%q) version = %q, want %q", tt.title, gotVersion, tt.wantVersion)
			}
			if gotTrack != tt.wantTrack {
				t.Errorf("parseBranchTitle(%q) track = %q, want %q", tt.title, gotTrack, tt.wantTrack)
			}
		})
	}
}

// =============================================================================
// Benchmark Tests for Branch Current Optimization
// =============================================================================

// generateBenchmarkProjectItems creates N project items for benchmarking.
// Half are assigned to the target release, half to other releases.
func generateBenchmarkProjectItems(n int, targetRelease string) []api.ProjectItem {
	items := make([]api.ProjectItem, n)
	for i := 0; i < n; i++ {
		release := targetRelease
		if i%2 == 0 {
			release = "other-release"
		}
		items[i] = api.ProjectItem{
			ID: fmt.Sprintf("ITEM_%d", i),
			Issue: &api.Issue{
				ID:     fmt.Sprintf("ISSUE_%d", i),
				Number: i + 1,
				Title:  fmt.Sprintf("Issue %d", i+1),
				State:  "OPEN",
			},
			FieldValues: []api.FieldValue{
				{Field: "Release", Value: release},
				{Field: "Status", Value: "In progress"},
			},
		}
	}
	return items
}

// BenchmarkBranchCurrent_ActiveLabel benchmarks the fast path (active+branch label lookup)
func BenchmarkBranchCurrent_ActiveLabel(b *testing.B) {
	mock := &mockBranchClient{
		openIssuesByLabels: []api.Issue{
			{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN", SubIssueCount: 50},
		},
		project: &api.Project{
			ID:     "PROJECT_1",
			Number: 1,
			Title:  "Test Project",
		},
	}

	cfg := testBranchConfig()
	cmd, _ := newTestBranchCmd()
	opts := &branchCurrentOptions{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mock.getOpenIssuesByLabelsCalls = nil
		_ = runBranchCurrentWithDeps(cmd, opts, cfg, mock)
	}
}

// BenchmarkBranchCurrent_Fallback benchmarks the fallback path (title scan)
func BenchmarkBranchCurrent_Fallback(b *testing.B) {
	mock := &mockBranchClient{
		openIssuesByLabels: nil, // No active label match
		openIssues: []api.Issue{
			{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN", SubIssueCount: 50},
		},
		project: &api.Project{
			ID:     "PROJECT_1",
			Number: 1,
			Title:  "Test Project",
		},
	}

	cfg := testBranchConfig()
	cmd, _ := newTestBranchCmd()
	opts := &branchCurrentOptions{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mock.getOpenIssuesByLabelsCalls = nil
		_ = runBranchCurrentWithDeps(cmd, opts, cfg, mock)
	}
}

// BenchmarkBranchClose_Optimized benchmarks the branch close command
// with repository-scoped filtering.
func BenchmarkBranchClose_Optimized(b *testing.B) {
	itemCount := 100
	mock := &mockBranchClient{
		openIssues: []api.Issue{
			{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN"},
		},
		project: &api.Project{
			ID:     "PROJECT_1",
			Number: 1,
			Title:  "Test Project",
		},
		projectItems: generateBenchmarkProjectItems(itemCount, "v1.2.0"),
	}

	cfg := testBranchConfig()

	// Create temp dir for config (required by branch close)
	tempDir := b.TempDir()
	configPath := tempDir + "/.gh-pmu.json"
	_ = cfg.Save(filepath.Dir(configPath))
	originalDir, _ := os.Getwd()
	_ = os.Chdir(tempDir)
	defer func() { _ = os.Chdir(originalDir) }()

	cmd, _ := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mock.getProjectItemsCalls = nil
		mock.closeIssueCalls = nil
		_ = runBranchCloseWithDeps(cmd, opts, cfg, mock)
	}

	b.ReportMetric(float64(itemCount), "items_processed")
}

// =============================================================================
// Assigned Label Tests
// =============================================================================

func TestBranchRemoveWithDeps_RemovesAssignedLabel(t *testing.T) {
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	mock.issueByNumber = &api.Issue{
		ID:         "ISSUE_42",
		Number:     42,
		Title:      "Test Issue",
		State:      "OPEN",
		Repository: api.Repository{Owner: "testowner", Name: "testrepo"},
	}
	mock.project = &api.Project{ID: "proj-1", Number: 1}
	mock.projectItemID = "ITEM_42"
	mock.projectItemFieldValue = "v1.2.0"

	cfg := testBranchConfig()
	cfg.Fields["branch"] = config.Field{Field: "Branch"}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchRemoveOptions{issueNumber: 42}

	err := runBranchRemoveWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify RemoveLabelFromIssue was called with 'assigned'
	if len(mock.removeLabelCalls) != 1 {
		t.Fatalf("Expected 1 RemoveLabelFromIssue call, got %d", len(mock.removeLabelCalls))
	}
	call := mock.removeLabelCalls[0]
	if call.labelName != "assigned" {
		t.Errorf("Expected label 'assigned', got %q", call.labelName)
	}
	if call.issueID != "ISSUE_42" {
		t.Errorf("Expected issueID 'ISSUE_42', got %q", call.issueID)
	}
}

func TestBranchRemoveWithDeps_SkipsLabelRemovalForClosedIssue(t *testing.T) {
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	mock.issueByNumber = &api.Issue{
		ID:         "ISSUE_42",
		Number:     42,
		Title:      "Closed Issue",
		State:      "CLOSED",
		Repository: api.Repository{Owner: "testowner", Name: "testrepo"},
	}
	mock.project = &api.Project{ID: "proj-1", Number: 1}
	mock.projectItemID = "ITEM_42"
	mock.projectItemFieldValue = "v1.2.0"

	cfg := testBranchConfig()
	cfg.Fields["branch"] = config.Field{Field: "Branch"}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchRemoveOptions{issueNumber: 42}

	err := runBranchRemoveWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify RemoveLabelFromIssue was NOT called for closed issue
	if len(mock.removeLabelCalls) != 0 {
		t.Errorf("Expected 0 RemoveLabelFromIssue calls for closed issue, got %d", len(mock.removeLabelCalls))
	}
}

func TestBranchCloseWithDeps_RemovesAssignedLabelFromOpenIssues(t *testing.T) {
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	// 2 open sub-issues + 1 closed sub-issue
	mock.subIssues = []api.SubIssue{
		{ID: "ISSUE_1", Number: 41, Title: "Open issue 1", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
		{ID: "ISSUE_2", Number: 42, Title: "Open issue 2", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
		{ID: "ISSUE_3", Number: 43, Title: "Done issue", State: "CLOSED", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	mock.projectItemIDs = map[string]string{
		"ISSUE_1": "ITEM_1",
		"ISSUE_2": "ITEM_2",
	}
	mock.projectItemFieldValues = map[string]string{
		"ITEM_1": "In Progress",
		"ITEM_2": "Ready",
	}

	cfg := testBranchConfig()
	cfg.Fields["status"] = config.Field{
		Field: "Status",
		Values: map[string]string{
			"backlog": "Backlog",
		},
	}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify RemoveLabelFromIssue was called for 2 open issues, not the closed one
	if len(mock.removeLabelCalls) != 2 {
		t.Fatalf("Expected 2 RemoveLabelFromIssue calls for open issues, got %d", len(mock.removeLabelCalls))
	}
	for _, call := range mock.removeLabelCalls {
		if call.labelName != "assigned" {
			t.Errorf("Expected label 'assigned', got %q", call.labelName)
		}
		if call.issueID == "ISSUE_3" {
			t.Error("Should NOT remove label from closed issue ISSUE_3")
		}
	}
}

func TestBranchCloseWithDeps_LabelErrorIsNonBlocking(t *testing.T) {
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{
			ID:     "TRACKER_123",
			Number: 100,
			Title:  "Branch: v1.2.0",
			State:  "OPEN",
		},
	}
	// Sub-issue — open, will trigger label removal
	mock.subIssues = []api.SubIssue{
		{ID: "ISSUE_1", Number: 41, Title: "Open issue", State: "OPEN", Repository: api.Repository{Owner: "testowner", Name: "testrepo"}},
	}
	mock.projectItemIDs = map[string]string{
		"ISSUE_1": "ITEM_1",
	}
	mock.projectItemFieldValues = map[string]string{
		"ITEM_1": "In Progress",
	}
	mock.removeLabelErr = errors.New("label API error")

	cfg := testBranchConfig()
	cfg.Fields["status"] = config.Field{
		Field: "Status",
		Values: map[string]string{
			"backlog": "Backlog",
		},
	}
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// Should succeed even when label removal fails
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)
	if err != nil {
		t.Fatalf("Label error should be non-blocking, got: %v", err)
	}
}

func TestRunBranchCloseWithDeps_UsesSubIssuesInsteadOfProjectScan(t *testing.T) {
	// ARRANGE: Set up tracker with sub-issues (not project items)
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN"},
	}
	mock.subIssues = []api.SubIssue{
		{
			ID:         "ISSUE_A",
			Number:     10,
			Title:      "Feature A",
			State:      "CLOSED",
			Repository: api.Repository{Owner: "testowner", Name: "testrepo"},
		},
		{
			ID:         "ISSUE_B",
			Number:     11,
			Title:      "Feature B",
			State:      "CLOSED",
			Repository: api.Repository{Owner: "testowner", Name: "testrepo"},
		},
	}

	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, buf := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify GetSubIssues was called with tracker number
	if len(mock.getSubIssuesCalls) != 1 {
		t.Fatalf("Expected 1 GetSubIssues call, got %d", len(mock.getSubIssuesCalls))
	}
	if mock.getSubIssuesCalls[0].number != 100 {
		t.Errorf("Expected GetSubIssues for tracker #100, got #%d", mock.getSubIssuesCalls[0].number)
	}

	// Verify GetProjectItemsMinimal was NOT called
	if len(mock.getProjectItemsMinimalCalls) != 0 {
		t.Errorf("Expected 0 GetProjectItemsMinimal calls, got %d", len(mock.getProjectItemsMinimalCalls))
	}

	// Verify output shows correct issue count
	output := buf.String()
	if !strings.Contains(output, "2 done") {
		t.Errorf("Expected output to mention '2 done', got: %s", output)
	}

	// Verify tracker was closed
	if len(mock.closeIssueCalls) != 1 {
		t.Fatalf("Expected 1 CloseIssue call, got %d", len(mock.closeIssueCalls))
	}
}

func TestRunBranchCloseWithDeps_SubIssues_IncompleteMovedToBacklog(t *testing.T) {
	// ARRANGE: Tracker with mix of done and open sub-issues
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN"},
	}
	mock.subIssues = []api.SubIssue{
		{
			ID:         "ISSUE_A",
			Number:     10,
			Title:      "Done Feature",
			State:      "CLOSED",
			Repository: api.Repository{Owner: "testowner", Name: "testrepo"},
		},
		{
			ID:         "ISSUE_B",
			Number:     11,
			Title:      "Incomplete Feature",
			State:      "OPEN",
			Repository: api.Repository{Owner: "testowner", Name: "testrepo"},
		},
	}
	mock.projectItemIDs = map[string]string{
		"ISSUE_B": "ITEM_B",
	}

	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, buf := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify output shows 1 done, 1 incomplete
	output := buf.String()
	if !strings.Contains(output, "1 done") {
		t.Errorf("Expected '1 done' in output, got: %s", output)
	}
	if !strings.Contains(output, "1 incomplete") {
		t.Errorf("Expected '1 incomplete' in output, got: %s", output)
	}

	// Verify GetProject was called (needed for field operations on incomplete issues)
	// but GetProjectItemsMinimal was NOT called
	if len(mock.getProjectItemsMinimalCalls) != 0 {
		t.Errorf("Expected 0 GetProjectItemsMinimal calls, got %d", len(mock.getProjectItemsMinimalCalls))
	}

	// Verify backlog move: SetProjectItemField should be called for status
	foundBacklogSet := false
	for _, call := range mock.setFieldCalls {
		if call.value == "Backlog" {
			foundBacklogSet = true
		}
	}
	if !foundBacklogSet {
		t.Error("Expected incomplete issue to be moved to backlog")
	}
}

func TestRunBranchCloseWithDeps_GetSubIssuesError(t *testing.T) {
	// ARRANGE
	mock := setupMockForBranch()
	mock.openIssues = []api.Issue{
		{ID: "TRACKER_123", Number: 100, Title: "Branch: v1.2.0", State: "OPEN"},
	}
	mock.getSubIssuesErr = errors.New("failed to get sub-issues")

	cfg := testBranchConfig()
	cleanup := setupBranchTestDir(t, cfg)
	defer cleanup()

	cmd, _ := newTestBranchCmd()
	opts := &branchCloseOptions{branchName: "v1.2.0", yes: true}

	// ACT
	err := runBranchCloseWithDeps(cmd, opts, cfg, mock)

	// ASSERT
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get sub-issues") {
		t.Errorf("Expected error about sub-issues, got: %v", err)
	}
}

// ============================================================================
// sortBranchesByVersionDesc Tests
// ============================================================================

func TestSortBranchesByVersionDesc_SortedOrder(t *testing.T) {
	releases := []branchInfo{
		{version: "1.0.0"},
		{version: "3.0.0"},
		{version: "2.0.0"},
		{version: "1.5.0"},
	}
	sortBranchesByVersionDesc(releases)
	expected := []string{"3.0.0", "2.0.0", "1.5.0", "1.0.0"}
	for i, r := range releases {
		if r.version != expected[i] {
			t.Errorf("index %d: got %s, want %s", i, r.version, expected[i])
		}
	}
}

func TestSortBranchesByVersionDesc_EqualVersions(t *testing.T) {
	releases := []branchInfo{
		{version: "1.0.0", codename: "alpha"},
		{version: "1.0.0", codename: "beta"},
	}
	sortBranchesByVersionDesc(releases)
	// Both are 1.0.0 — sort is stable-ish but both should remain
	if len(releases) != 2 {
		t.Errorf("expected 2 releases, got %d", len(releases))
	}
	if releases[0].version != "1.0.0" || releases[1].version != "1.0.0" {
		t.Errorf("expected both 1.0.0, got %s and %s", releases[0].version, releases[1].version)
	}
}

func TestSortBranchesByVersionDesc_EmptyInput(t *testing.T) {
	var releases []branchInfo
	sortBranchesByVersionDesc(releases) // should not panic
	if len(releases) != 0 {
		t.Errorf("expected empty, got %d", len(releases))
	}
}

func TestSortBranchesByVersionDesc_SingleElement(t *testing.T) {
	releases := []branchInfo{{version: "1.0.0"}}
	sortBranchesByVersionDesc(releases)
	if releases[0].version != "1.0.0" {
		t.Errorf("expected 1.0.0, got %s", releases[0].version)
	}
}
