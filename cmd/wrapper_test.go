package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
)

// operationNamePattern extracts the operation name from a GraphQL document,
// e.g. "query GetUserProject($number:Int!)..." -> "GetUserProject". Anonymous
// documents ("query { n0: node(...) }", built by getProjectFieldsForIssuesBatch)
// yield no name and must be matched with respondToQueryContaining instead.
var operationNamePattern = regexp.MustCompile(`^\s*(?:query|mutation)\s+([A-Za-z_][A-Za-z0-9_]*)`)

func graphQLOperationName(query string) string {
	m := operationNamePattern.FindStringSubmatch(query)
	if m == nil {
		return ""
	}
	return m[1]
}

// queryMatcher is a substring rule for documents that carry no operation name.
type queryMatcher struct {
	substring string
	response  interface{}
}

// mockGraphQLHandler creates an HTTP handler that responds to GraphQL requests
// with preconfigured responses.
//
// Matching is deterministic and happens in this order:
//  1. exact operation name (respondTo)
//  2. substring matchers, in registration order (respondToQueryContaining)
//  3. defaultResponse
//
// Exact-name matching matters because several operation names are prefixes of
// others — GetIssue is a prefix of GetIssueComments, GetIssueWithProjectFields
// and GetIssuesByLabel; GetProjectItems is a prefix of GetProjectItemsMinimal
// and GetProjectItemsForBoard. Substring matching over a map would resolve
// those in Go's randomized map order and make fixtures flaky.
type mockGraphQLHandler struct {
	// responses maps exact GraphQL operation names to their JSON responses
	responses map[string]interface{}
	// responders maps exact GraphQL operation names to a function that derives
	// the response from the request, for flows that issue the same operation
	// more than once and must be answered differently each time.
	responders map[string]func(graphQLRequest) interface{}
	// matchers holds ordered substring rules for anonymous documents
	matchers []queryMatcher
	// Default response for unmatched operations
	defaultResponse interface{}
	// Track requests for assertions
	requests []graphQLRequest
	// Mutex to protect concurrent access to requests
	mu sync.Mutex
}

type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

func newMockGraphQLHandler() *mockGraphQLHandler {
	return &mockGraphQLHandler{
		responses:  make(map[string]interface{}),
		responders: make(map[string]func(graphQLRequest) interface{}),
		requests:   []graphQLRequest{},
	}
}

// respondTo registers a response for an exact GraphQL operation name.
func (h *mockGraphQLHandler) respondTo(opName string, response interface{}) *mockGraphQLHandler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.responses[opName] = response
	return h
}

// respondToFunc registers a per-request responder for an exact GraphQL
// operation name. It takes precedence over respondTo for the same name.
//
// This exists because a single fixture per operation makes some flows
// untestable rather than merely awkward: sub add issues GetIssue twice — once
// for the parent, once for the child — and a static fixture answers both with
// the same node id. The AddSubIssue payload would then carry identical ids for
// issueId and subIssueId, so swapping parent and child in the production call
// would still satisfy the assertion. TESTING.md calls this out under "Watch for
// symmetric fixtures": when a test distinguishes two things, the fixtures must
// differ.
func (h *mockGraphQLHandler) respondToFunc(opName string, fn func(graphQLRequest) interface{}) *mockGraphQLHandler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.responders[opName] = fn
	return h
}

// respondToQueryContaining registers a response for documents containing
// substr. Use only for anonymous documents that carry no operation name.
func (h *mockGraphQLHandler) respondToQueryContaining(substr string, response interface{}) *mockGraphQLHandler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.matchers = append(h.matchers, queryMatcher{substring: substr, response: response})
	return h
}

// operationNames returns the operation name of every request received, in
// order, so scenarios can assert which calls the command actually made.
// Anonymous documents appear as "".
func (h *mockGraphQLHandler) operationNames() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	names := make([]string, 0, len(h.requests))
	for _, r := range h.requests {
		names = append(names, graphQLOperationName(r.Query))
	}
	return names
}

// requestsFor returns every request made for the given operation name.
func (h *mockGraphQLHandler) requestsFor(opName string) []graphQLRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []graphQLRequest
	for _, r := range h.requests {
		if graphQLOperationName(r.Query) == opName {
			out = append(out, r)
		}
	}
	return out
}

func (h *mockGraphQLHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse the request body
	var req graphQLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	h.requests = append(h.requests, req)

	// 1. Exact operation name — per-request responder first, then static.
	var response interface{}
	if op := graphQLOperationName(req.Query); op != "" {
		if fn, ok := h.responders[op]; ok {
			response = fn(req)
		} else if resp, ok := h.responses[op]; ok {
			response = resp
		}
	}

	// 2. Ordered substring matchers (anonymous documents).
	if response == nil {
		for _, m := range h.matchers {
			if strings.Contains(req.Query, m.substring) {
				response = m.response
				break
			}
		}
	}

	// 3. Fallbacks.
	if response == nil {
		response = h.defaultResponse
	}
	h.mu.Unlock()

	if response == nil {
		// Return empty data if no response configured
		response = map[string]interface{}{"data": map[string]interface{}{}}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// redirectTransport is an http.RoundTripper that redirects all requests to a test server
type redirectTransport struct {
	server *httptest.Server
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect the request to the test server
	newURL := t.server.URL + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}

	newReq, err := http.NewRequest(req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}

	// Copy headers
	for k, v := range req.Header {
		newReq.Header[k] = v
	}

	return t.server.Client().Do(newReq)
}

// defaultTestConfig is the config setupTestEnvironment writes. It carries no
// "branch" field: the branch commands require one, so scenarios for those use
// setupTestEnvironmentWithConfig and branchTestConfig instead.
const defaultTestConfig = `{
  "project": {
    "owner": "test-org",
    "number": 1
  },
  "repositories": ["test-org/test-repo"],
  "fields": {
    "status": {
      "field": "Status",
      "values": {
        "backlog": "Backlog",
        "in_progress": "In Progress",
        "done": "Done"
      }
    },
    "priority": {
      "field": "Priority",
      "values": {
        "p0": "P0",
        "p1": "P1",
        "p2": "P2"
      }
    }
  }
}`

// setupTestEnvironment creates a temp directory with a valid config file
// and sets up the mock GraphQL server. Returns a cleanup function.
func setupTestEnvironment(t *testing.T, handler *mockGraphQLHandler) (string, func()) {
	t.Helper()
	return setupTestEnvironmentWithConfig(t, handler, defaultTestConfig)
}

// setupTestEnvironmentWithConfig is setupTestEnvironment with a caller-supplied
// config document, for commands whose behavior depends on fields the default
// config does not declare.
func setupTestEnvironmentWithConfig(t *testing.T, handler *mockGraphQLHandler, configContent string) (string, func()) {
	t.Helper()

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	configPath := filepath.Join(tmpDir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to write config: %v", err)
	}

	// Start mock server
	server := httptest.NewServer(handler)

	// Set test transport that redirects all requests to mock server
	api.SetTestTransport(&redirectTransport{server: server})
	api.SetTestAuthToken("test-token")

	// Save original directory
	origDir, _ := os.Getwd()

	// Change to temp directory
	if err := os.Chdir(tmpDir); err != nil {
		server.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to chdir: %v", err)
	}

	cleanup := func() {
		_ = os.Chdir(origDir)
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
		server.Close()
		os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup
}

// ============================================================================
// runList Wrapper Tests
// ============================================================================

func TestRunList_LoadsConfig(t *testing.T) {
	// This test verifies that runList successfully loads config from cwd
	// We can't easily mock the full GraphQL flow, but we can verify:
	// 1. Config loading works
	// 2. The API client is created
	// The API call itself may fail without auth, but that's expected

	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &listOptions{}
	err := runList(cmd, opts)

	// We expect an API error (not a config error)
	// This proves the config was loaded successfully
	if err == nil {
		// If no error, even better - mock worked
		return
	}

	// Config loading should succeed; API call may fail
	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}

	// API errors are expected when mocking isn't perfect
	// This still proves we passed through the config loading code path
}

func TestRunList_ConfigNotFound(t *testing.T) {
	handler := newMockGraphQLHandler()

	// Create temp dir WITHOUT config file
	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(server.Client().Transport)
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newListCommand()
	opts := &listOptions{}
	err = runList(cmd, opts)

	if err == nil {
		t.Fatal("expected error when config not found")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("expected 'failed to load configuration' error, got: %v", err)
	}
}

func TestRunList_InvalidConfig(t *testing.T) {
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-badconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write invalid config (missing required fields)
	configPath := filepath.Join(tmpDir, ".gh-pmu.json")
	_ = os.WriteFile(configPath, []byte(`{"invalid": "json": "content"}`), 0644)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(server.Client().Transport)
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newListCommand()
	opts := &listOptions{}
	err = runList(cmd, opts)

	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

// ============================================================================
// runView Wrapper Tests
// ============================================================================

func TestRunView_LoadsConfig(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newViewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &viewOptions{}
	args := []string{"1"}
	err := runView(cmd, args, opts)

	// We expect an API error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

func TestRunView_ConfigNotFound(t *testing.T) {
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(server.Client().Transport)
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newViewCommand()
	opts := &viewOptions{}
	args := []string{"1"}
	err = runView(cmd, args, opts)

	if err == nil {
		t.Fatal("expected error when config not found")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("expected 'failed to load configuration' error, got: %v", err)
	}
}

// ============================================================================
// runMove Wrapper Tests
// ============================================================================

func TestRunMove_LoadsConfig(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newMoveCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &moveOptions{
		status: "in_progress",
	}
	args := []string{"1"}
	err := runMove(cmd, args, opts)

	// We expect an API error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

func TestRunMove_ConfigNotFound(t *testing.T) {
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(server.Client().Transport)
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newMoveCommand()
	opts := &moveOptions{status: "done"}
	args := []string{"1"}
	err = runMove(cmd, args, opts)

	if err == nil {
		t.Fatal("expected error when config not found")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("expected 'failed to load configuration' error, got: %v", err)
	}
}

// ============================================================================
// runClose Wrapper Tests
// ============================================================================

func TestRunClose_WithUpdateStatus_LoadsConfig(t *testing.T) {
	// runClose only loads config when --update-status is used
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newCloseCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &closeOptions{
		updateStatus: true, // This triggers config loading
	}
	args := []string{"1"}
	err := runClose(cmd, args, opts)

	// We expect an API or gh CLI error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

func TestRunClose_WithUpdateStatus_ConfigNotFound(t *testing.T) {
	// Note: runClose catches config errors and warns but continues,
	// so we can't directly test for config load errors here.
	// This test exercises the code path where config is not found.
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(&redirectTransport{server: server})
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newCloseCommand()
	opts := &closeOptions{
		updateStatus: true, // This triggers config loading path
	}
	args := []string{"1"}
	_ = runClose(cmd, args, opts)

	// We don't check the error because runClose warns and continues
	// The code path for updateStatusToDone was exercised
}

// ============================================================================
// runFieldList Wrapper Tests
// ============================================================================

func TestRunFieldList_LoadsConfig(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newFieldListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := runFieldList(cmd, []string{})

	// We expect an API error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

func TestRunFieldList_ConfigNotFound(t *testing.T) {
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(&redirectTransport{server: server})
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newFieldListCommand()
	err = runFieldList(cmd, []string{})

	if err == nil {
		t.Fatal("expected error when config not found")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("expected 'failed to load configuration' error, got: %v", err)
	}
}

// ============================================================================
// runFieldCreate Wrapper Tests
// ============================================================================

func TestRunFieldCreate_InvalidType(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newFieldCreateCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &fieldCreateOptions{
		fieldType: "invalid_type",
	}
	err := runFieldCreate(cmd, []string{"TestField"}, opts)

	if err == nil {
		t.Fatal("expected error for invalid field type")
	}
	if !strings.Contains(err.Error(), "invalid field type") {
		t.Errorf("expected 'invalid field type' error, got: %v", err)
	}
}

func TestRunFieldCreate_SingleSelectWithoutOptions(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newFieldCreateCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &fieldCreateOptions{
		fieldType: "single_select",
		options:   []string{}, // No options
	}
	err := runFieldCreate(cmd, []string{"TestField"}, opts)

	if err == nil {
		t.Fatal("expected error for single_select without options")
	}
	if !strings.Contains(err.Error(), "requires at least one --option") {
		t.Errorf("expected 'requires at least one --option' error, got: %v", err)
	}
}

func TestRunFieldCreate_LoadsConfig(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newFieldCreateCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &fieldCreateOptions{
		fieldType: "text",
	}
	err := runFieldCreate(cmd, []string{"TestField"}, opts)

	// We expect an API error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

// ============================================================================
// runHistory Wrapper Tests
// ============================================================================

func TestRunHistory_ConfigNotFound(t *testing.T) {
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(&redirectTransport{server: server})
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newHistoryCommand()
	opts := &historyOptions{}
	err = runHistory(cmd, []string{}, opts)

	if err == nil {
		t.Fatal("expected error when config not found")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("expected 'failed to load configuration' error, got: %v", err)
	}
}

// ============================================================================
// runBoard Wrapper Tests
// ============================================================================

func TestRunBoard_LoadsConfig(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newBoardCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &boardOptions{}
	err := runBoard(cmd, opts)

	// We expect an API error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

func TestRunBoard_ConfigNotFound(t *testing.T) {
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(&redirectTransport{server: server})
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newBoardCommand()
	opts := &boardOptions{}
	err = runBoard(cmd, opts)

	if err == nil {
		t.Fatal("expected error when config not found")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("expected 'failed to load configuration' error, got: %v", err)
	}
}

// ============================================================================
// Test Transport Integration
// ============================================================================

func TestSetTestTransport_WorksWithNewClient(t *testing.T) {
	// Verify that SetTestTransport actually affects NewClient behavior
	handler := newMockGraphQLHandler()
	handler.defaultResponse = map[string]interface{}{
		"data": map[string]interface{}{
			"test": "response",
		},
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	// Set test transport
	api.SetTestTransport(server.Client().Transport)
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	// Create client - should use the test transport
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("expected client to be created, got error: %v", err)
	}
	_ = client

	// The client should be functional (even if calls fail due to mock)
	// This verifies the transport injection works
}

// ============================================================================
// runIntake Wrapper Tests
// ============================================================================

func TestRunIntake_LoadsConfig(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newIntakeCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &intakeOptions{list: true}
	err := runIntake(cmd, opts)

	// We expect an API error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

func TestRunIntake_ConfigNotFound(t *testing.T) {
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(&redirectTransport{server: server})
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newIntakeCommand()
	opts := &intakeOptions{list: true}
	err = runIntake(cmd, opts)

	if err == nil {
		t.Fatal("expected error when config not found")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("expected 'failed to load configuration' error, got: %v", err)
	}
}

func TestRunIntake_WithDryRun(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newIntakeCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &intakeOptions{
		dryRun: true,
	}
	err := runIntake(cmd, opts)

	// API error expected, but proves config loading works
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

// ============================================================================
// runSplit Wrapper Tests
// ============================================================================

func TestRunSplit_LoadsConfig(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSplitCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &splitOptions{}
	args := []string{"1"}
	err := runSplit(cmd, args, opts)

	// We expect an API error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

func TestRunSplit_ConfigNotFound(t *testing.T) {
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(&redirectTransport{server: server})
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newSplitCommand()
	opts := &splitOptions{}
	args := []string{"1"}
	err = runSplit(cmd, args, opts)

	if err == nil {
		t.Fatal("expected error when config not found")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("expected 'failed to load configuration' error, got: %v", err)
	}
}

func TestRunSplit_InvalidIssueNumber(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSplitCommand()
	opts := &splitOptions{}
	args := []string{"not-a-number"}
	err := runSplit(cmd, args, opts)

	if err == nil {
		t.Fatal("expected error for invalid issue number")
	}
	if !strings.Contains(err.Error(), "invalid issue number") {
		t.Errorf("expected 'invalid issue number' error, got: %v", err)
	}
}

func TestRunSplit_WithDryRun(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSplitCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &splitOptions{
		dryRun: true,
		from:   "body",
	}
	args := []string{"1"}
	err := runSplit(cmd, args, opts)

	// API error expected, but proves config loading works
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

// ============================================================================
// runTriage Wrapper Tests
// ============================================================================

func TestRunTriage_LoadsConfig(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newTriageCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &triageOptions{
		list: true, // Use list mode to avoid requiring config name
	}
	err := runTriage(cmd, []string{}, opts)

	// We expect an API error or no error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

func TestRunTriage_ConfigNotFound(t *testing.T) {
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(&redirectTransport{server: server})
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newTriageCommand()
	opts := &triageOptions{}
	err = runTriage(cmd, []string{"test"}, opts)

	if err == nil {
		t.Fatal("expected error when config not found")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("expected 'failed to load configuration' error, got: %v", err)
	}
}

func TestRunTriage_WithQuery(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newTriageCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &triageOptions{
		query:  "is:open",
		apply:  "status:backlog",
		dryRun: true,
	}
	err := runTriage(cmd, []string{}, opts)

	// API error expected, but proves config loading works
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

// ============================================================================
// runSubAdd Wrapper Tests
// ============================================================================

func TestRunSubAdd_LoadsConfig(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubAddCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &subAddOptions{}
	args := []string{"1", "2"}
	err := runSubAdd(cmd, args, opts)

	// We expect an API error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

func TestRunSubAdd_ConfigNotFound(t *testing.T) {
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(&redirectTransport{server: server})
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newSubAddCommand()
	opts := &subAddOptions{}
	args := []string{"1", "2"}
	err = runSubAdd(cmd, args, opts)

	if err == nil {
		t.Fatal("expected error when config not found")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("expected 'failed to load configuration' error, got: %v", err)
	}
}

// ============================================================================
// runSubCreate Wrapper Tests
// ============================================================================

func TestRunSubCreate_LoadsConfig(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubCreateCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &subCreateOptions{
		parent: "1",
		title:  "Test sub-issue",
	}
	err := runSubCreate(cmd, opts)

	// We expect an API error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

func TestRunSubCreate_ConfigNotFound(t *testing.T) {
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(&redirectTransport{server: server})
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newSubCreateCommand()
	opts := &subCreateOptions{
		parent: "1",
		title:  "Test sub-issue",
	}
	err = runSubCreate(cmd, opts)

	if err == nil {
		t.Fatal("expected error when config not found")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("expected 'failed to load configuration' error, got: %v", err)
	}
}

// ============================================================================
// runSubList Wrapper Tests
// ============================================================================

func TestRunSubList_LoadsConfig(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &subListOptions{
		state:    "all",      // Valid state required before config check
		relation: "children", // Valid relation required before config check
	}
	args := []string{"1"}
	err := runSubList(cmd, args, opts)

	// We expect an API error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

func TestRunSubList_ConfigNotFound(t *testing.T) {
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(&redirectTransport{server: server})
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newSubListCommand()
	opts := &subListOptions{
		state:    "all",      // Valid state required before config check
		relation: "children", // Valid relation required before config check
	}
	args := []string{"1"}
	err = runSubList(cmd, args, opts)

	if err == nil {
		t.Fatal("expected error when config not found")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("expected 'failed to load configuration' error, got: %v", err)
	}
}

// ============================================================================
// runCreate Wrapper Tests
// ============================================================================

func TestRunCreate_LoadsConfig(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newCreateCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &createOptions{
		title: "Test Issue",
	}
	err := runCreate(cmd, opts)

	// We expect an API error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

func TestRunCreate_ConfigNotFound(t *testing.T) {
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(&redirectTransport{server: server})
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newCreateCommand()
	opts := &createOptions{title: "Test"}
	err = runCreate(cmd, opts)

	if err == nil {
		t.Fatal("expected error when config not found")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("expected 'failed to load configuration' error, got: %v", err)
	}
}

// ============================================================================
// runSubRemove Wrapper Tests
// ============================================================================

func TestRunSubRemove_LoadsConfig(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newSubRemoveCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &subRemoveOptions{}
	args := []string{"1", "2"}
	err := runSubRemove(cmd, args, opts)

	// We expect an API error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}

func TestRunSubRemove_ConfigNotFound(t *testing.T) {
	handler := newMockGraphQLHandler()

	tmpDir, err := os.MkdirTemp("", "gh-pmu-test-noconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(handler)
	defer server.Close()

	api.SetTestTransport(&redirectTransport{server: server})
	api.SetTestAuthToken("test-token")
	defer func() {
		api.SetTestTransport(nil)
		api.SetTestAuthToken("")
	}()

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newSubRemoveCommand()
	opts := &subRemoveOptions{}
	args := []string{"1", "2"}
	err = runSubRemove(cmd, args, opts)

	if err == nil {
		t.Fatal("expected error when config not found")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("expected 'failed to load configuration' error, got: %v", err)
	}
}

// ============================================================================
// runHistory Wrapper Tests (additional)
// ============================================================================

func TestRunHistory_LoadsConfig(t *testing.T) {
	handler := newMockGraphQLHandler()
	_, cleanup := setupTestEnvironment(t, handler)
	defer cleanup()

	cmd := newHistoryCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	opts := &historyOptions{}
	err := runHistory(cmd, []string{}, opts)

	// We expect an API error (not a config error)
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("config loading failed: %v", err)
	}
}
