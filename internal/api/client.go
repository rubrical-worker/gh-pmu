package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
)

// FeatureSubIssues is the GitHub API preview header for sub-issues
const FeatureSubIssues = "sub_issues"

// FeatureIssueTypes is the GitHub API preview header for issue types
const FeatureIssueTypes = "issue_types"

// testClientHook is set by SetTestTransport/SetTestAuthToken to override
// client options during testing. nil in production use.
var (
	testMu         sync.Mutex
	testClientHook func(*ClientOptions)
)

// testState holds the current test override values, guarded by testMu.
var testState struct {
	transport http.RoundTripper
	token     string
}

// SetTestTransport sets a custom HTTP transport for testing purposes.
// This overrides the transport used by NewClient(). Call with nil to clear.
// Intended for use in test code only.
func SetTestTransport(t http.RoundTripper) {
	testMu.Lock()
	defer testMu.Unlock()
	testState.transport = t
	rebuildTestHook()
}

// SetTestAuthToken sets a custom auth token for testing purposes.
// This overrides the token used by NewClient(). Call with "" to clear.
// Intended for use in test code only.
func SetTestAuthToken(token string) {
	testMu.Lock()
	defer testMu.Unlock()
	testState.token = token
	rebuildTestHook()
}

// rebuildTestHook updates testClientHook from current testState. Caller must hold testMu.
func rebuildTestHook() {
	if testState.transport == nil && testState.token == "" {
		testClientHook = nil
		return
	}
	t, tok := testState.transport, testState.token
	testClientHook = func(opts *ClientOptions) {
		if t != nil {
			opts.Transport = t
		}
		if tok != "" {
			opts.AuthToken = tok
		}
	}
}

// GraphQLClient interface allows mocking the GitHub GraphQL client for testing
type GraphQLClient interface {
	Query(name string, query interface{}, variables map[string]interface{}) error
	Mutate(name string, mutation interface{}, variables map[string]interface{}) error
}

// RawGraphQLDoer executes raw GraphQL queries and returns the full response bytes.
// This is used for dynamic/batch queries with aliased fields that don't fit typed clients.
type RawGraphQLDoer interface {
	DoRaw(query string, headers map[string]string) ([]byte, error)
	DoRawBody(body []byte, headers map[string]string) ([]byte, error)
}

// Client wraps the GitHub GraphQL API client with project management features
type Client struct {
	gql    GraphQLClient
	rawGQL RawGraphQLDoer
	opts   ClientOptions

	// assigneeCache memoizes ResolveAssignee results for the life of the client,
	// which is one CLI invocation. Lazily created.
	assigneeCache map[string]resolvedAssignee

	// issueFieldsUnsupported records, for the life of the client, that the
	// server's schema rejected the organization issue field fragment, so reads
	// omit it up front (#935).
	issueFieldsUnsupported bool

	// warnOut receives client warnings; nil means os.Stderr.
	warnOut io.Writer
}

// ClientOptions configures the API client
type ClientOptions struct {
	// Host is the GitHub hostname (default: github.com)
	Host string

	// EnableSubIssues enables the sub_issues feature preview
	EnableSubIssues bool

	// EnableIssueTypes enables the issue_types feature preview
	EnableIssueTypes bool

	// Transport specifies the HTTP transport for API requests (for testing)
	Transport http.RoundTripper

	// AuthToken is the authorization token (for testing)
	AuthToken string
}

// NewClient creates a new API client with default options
func NewClient() (*Client, error) {
	opts := ClientOptions{
		EnableSubIssues:  true,
		EnableIssueTypes: true,
	}
	// Apply test overrides if set (hook is nil in production builds)
	if testClientHook != nil {
		testClientHook(&opts)
	}
	return NewClientWithOptions(opts)
}

// NewClientWithOptions creates a new API client with custom options
func NewClientWithOptions(opts ClientOptions) (*Client, error) {
	// Build headers with feature previews
	headers := make(map[string]string)

	// Add GraphQL feature preview headers
	// These enable beta features in the GitHub API
	featureHeaders := []string{}
	if opts.EnableSubIssues {
		featureHeaders = append(featureHeaders, FeatureSubIssues)
	}
	if opts.EnableIssueTypes {
		featureHeaders = append(featureHeaders, FeatureIssueTypes)
	}

	if len(featureHeaders) > 0 {
		// GitHub uses X-Github-Next for feature previews
		headers["X-Github-Next"] = joinFeatures(featureHeaders)
	}

	// Create GraphQL client options
	apiOpts := api.ClientOptions{
		Headers: headers,
	}

	if opts.Host != "" {
		apiOpts.Host = opts.Host
	}
	if opts.Transport != nil {
		apiOpts.Transport = opts.Transport
	}
	if opts.AuthToken != "" {
		apiOpts.AuthToken = opts.AuthToken
	}

	// Create the GraphQL client
	gql, err := api.NewGraphQLClient(apiOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create API client: %w", err)
	}

	// Create raw HTTP client for dynamic/batch GraphQL queries
	httpClient, err := api.NewHTTPClient(apiOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &Client{
		gql:    newRetryingGraphQL(gql, defaultClientMaxRetries, nil),
		rawGQL: newRetryingRawGraphQL(&httpRawGraphQL{httpClient: httpClient, host: opts.Host}, defaultClientMaxRetries, nil),
		opts:   opts,
	}, nil
}

// NewClientWithGraphQL creates a Client with a custom GraphQL client (for testing)
func NewClientWithGraphQL(gql GraphQLClient) *Client {
	return &Client{gql: gql}
}

// httpRawGraphQL implements RawGraphQLDoer using go-gh's HTTP client.
type httpRawGraphQL struct {
	httpClient *http.Client
	host       string
}

// DoRaw executes a raw GraphQL query and returns the full JSON response bytes.
// Extra headers (e.g. feature previews) are merged into the request.
func (h *httpRawGraphQL) DoRaw(query string, headers map[string]string) ([]byte, error) {
	body, err := json.Marshal(map[string]interface{}{"query": query})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL request: %w", err)
	}
	return h.DoRawBody(body, headers)
}

// DoRawBody sends a pre-built JSON request body to the GraphQL endpoint.
// Use this for mutations with variables where the body is already constructed.
func (h *httpRawGraphQL) DoRawBody(body []byte, headers map[string]string) ([]byte, error) {
	host := h.host
	if host == "" {
		// Resolve the default host the same way go-gh does (GH_HOST, then gh
		// config hosts, then github.com) so the raw path targets the configured
		// host — not a hardcoded api.github.com — and stays in sync with the
		// typed client (#859).
		host, _ = auth.DefaultHost()
	}
	url := graphQLEndpoint(host)

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create GraphQL request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GraphQL request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		return nil, fmt.Errorf("failed to read GraphQL response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &rawHTTPError{statusCode: resp.StatusCode, body: string(data)}
	}

	return data, nil
}

// graphQLEndpoint builds the GraphQL API endpoint URL for host, mirroring go-gh's
// (unexported) api.graphQLEndpoint so the raw client matches the typed client in
// every configuration (#859). GitHub Enterprise Server uses HOST/api/graphql;
// GitHub.com (and its subdomains, which NormalizeHostname collapses) uses
// api.HOST/graphql. The niche github.localhost and internal "garage" cases the
// typed client special-cases are unexported and not reachable by gh-pmu users.
func graphQLEndpoint(host string) string {
	host = auth.NormalizeHostname(host)
	if auth.IsEnterprise(host) {
		return fmt.Sprintf("https://%s/api/graphql", host)
	}
	return fmt.Sprintf("https://api.%s/graphql", host)
}

// rawHTTPError surfaces the upstream status code through the httpStatusCoder
// interface so IsRetryable / IsTransient5xx can classify failures from the
// raw GraphQL path the same way as typed go-gh errors.
type rawHTTPError struct {
	statusCode int
	body       string
}

func (e *rawHTTPError) Error() string {
	return fmt.Sprintf("GraphQL request returned status %d: %s", e.statusCode, e.body)
}

func (e *rawHTTPError) HTTPStatusCode() int {
	return e.statusCode
}

// joinFeatures joins feature names with commas
func joinFeatures(features []string) string {
	return strings.Join(features, ",")
}

// GetLatestGitTag returns the latest git tag using git describe
func (c *Client) GetLatestGitTag() (string, error) {
	cmd := exec.Command("git", "describe", "--tags", "--abbrev=0")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("no git tags found: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
