package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/spf13/cobra"
)

// listClient defines the interface for API methods used by list functions.
// This allows for easier testing with mock implementations.
type listClient interface {
	assigneeResolver
	GetProject(owner string, number int) (*api.Project, error)
	GetProjectItems(projectID string, filter *api.ProjectItemsFilter) ([]api.ProjectItem, error)
	GetOpenIssuesByLabel(owner, repo, label string) ([]api.Issue, error)
	GetSubIssueCounts(owner, repo string, numbers []int) (map[int]int, error)
	SearchRepositoryIssues(owner, repo string, filters api.SearchFilters, limit int) ([]api.Issue, error)
	GetProjectFieldsForIssues(projectID string, issueIDs []string) (map[string][]api.FieldValue, error)
}

type listOptions struct {
	status       string
	priority     string
	assignee     string
	label        string
	search       string
	branch       string
	noBranch     bool
	state        string
	limit        int
	hasSubIssues bool
	jsonFields   string
	jq           string
	repo         string
}

// listCommandJSONFields lists all available JSON fields for the list command
var listCommandJSONFields = []string{
	"assignees",
	"fieldValues",
	"number",
	"repository",
	"state",
	"title",
	"url",
}

func newListCommand() *cobra.Command {
	opts := &listOptions{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues from the configured project",
		Long: `List issues from the configured GitHub project with their field values.

By default, displays Title, Status, Priority, and Assignees for each issue.
Use filters to narrow down the results.`,
		Example: `  # Issues in a status, highest priority only
  gh pmu list --status ready --priority p1

  # Issues on the active branch
  gh pmu list --branch current

  # JSON with selected fields, filtered with jq
  gh pmu list --status backlog --json=number,title --jq='.items[].number'

  # Show which fields --json accepts
  gh pmu list --json-fields`,
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateListJSONFlag(cmd, opts)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// --json-fields needs no config or API access
			if listFields, _ := cmd.Flags().GetBool("json-fields"); listFields {
				return listListAvailableFields(cmd)
			}
			return runList(cmd, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.status, "status", "s", "", "Filter by status (e.g., backlog, in_progress, done)")
	cmd.Flags().StringVarP(&opts.priority, "priority", "p", "", "Filter by priority (e.g., p0, p1, p2)")
	cmd.Flags().StringVarP(&opts.assignee, "assignee", "a", "", "Filter by assignee login")
	cmd.Flags().StringVarP(&opts.label, "label", "l", "", "Filter by label name")
	cmd.Flags().StringVarP(&opts.search, "search", "q", "", "Search in issue title and body")
	cmd.Flags().StringVarP(&opts.branch, "branch", "b", "", "Filter by branch (e.g., release/v1.0.0, current)")
	cmd.Flags().BoolVar(&opts.noBranch, "no-branch", false, "Filter to issues without a branch assignment")
	cmd.Flags().StringVar(&opts.state, "state", "", "Filter by issue state (open or closed)")
	cmd.MarkFlagsMutuallyExclusive("branch", "no-branch")
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 0, "Limit number of results (0 for no limit)")
	cmd.Flags().BoolVar(&opts.hasSubIssues, "has-sub-issues", false, "Filter to only show parent issues (issues with sub-issues)")
	cmd.Flags().StringVar(&opts.jsonFields, "json", "", "Output JSON with specified fields (comma-separated). Use --json-fields to list available fields")
	cmd.Flags().Bool("json-fields", false, "List available JSON fields")
	cmd.Flags().StringVar(&opts.jq, "jq", "", "Filter JSON output using a jq expression")
	cmd.Flags().StringVarP(&opts.repo, "repo", "R", "", "Filter by repository (owner/repo format)")

	return cmd
}

func runList(cmd *cobra.Command, opts *listOptions) error {
	// Load configuration from current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	cfg, err := config.LoadFromDirectory(cwd)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'gh pmu init' to create a configuration file", err)
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create API client
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	return runListWithDeps(cmd, opts, cfg, client)
}

// runListWithDeps is the testable implementation of runList
func runListWithDeps(cmd *cobra.Command, opts *listOptions, cfg *config.Config, client listClient) error {
	// Resolve --assignee before either consumer sees it. The search path passes
	// it to GitHub as an "assignee:" qualifier and the fallback compares it
	// literally against issue logins, so an unresolved @me worked in one and
	// silently matched nothing in the other.
	if opts.assignee != "" {
		resolved, err := client.ResolveAssignee(opts.assignee)
		if err != nil {
			return err
		}
		opts.assignee = resolved
	}

	// Validate state filter
	if opts.state != "" {
		stateLower := strings.ToLower(opts.state)
		if stateLower != "open" && stateLower != "closed" && stateLower != "all" {
			return fmt.Errorf("invalid --state value: expected 'open', 'closed', or 'all', got %q", opts.state)
		}
	}

	// Get project
	project, err := client.GetProject(cfg.Project.Owner, cfg.Project.Number)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	// Determine repository filter (--repo flag takes precedence over config).
	// repoFilter stays as an "owner/repo" string; --repo is validated via the
	// shared helper (uniform empty-component rejection).
	repoFilter := ""
	if opts.repo != "" {
		if _, _, err := splitOwnerRepo(opts.repo, "--repo"); err != nil {
			return err
		}
		repoFilter = opts.repo
	} else if len(cfg.Repositories) > 0 {
		repoFilter = cfg.Repositories[0]
	}

	// Determine query strategy based on state filter and repo availability
	// When repo is available: always use SearchRepositoryIssues (optimized)
	//   - "all" state: dual calls (open + closed) merged
	//   - "open"/"closed"/default: single call
	// When no repo: fall back to GetProjectItems (full project scan)
	stateLower := strings.ToLower(opts.state)
	useSearchAPI := repoFilter != ""

	// When a client-side-only filter (a project field like status/priority/branch,
	// no-branch, or sub-issue presence) runs after the fetch, capping the fetch at
	// --limit can starve it — e.g. `--status done --limit 5` fetches 5 issues of any
	// status, then filters to done and can yield 0. In that case fetch without a cap
	// and apply --limit after filtering (below). Server-side filters (assignee/label/
	// search/state in the search path) are already applied during the fetch and do
	// not require over-fetching.
	clientSideFilterActive := opts.status != "" || opts.priority != "" ||
		opts.branch != "" || opts.noBranch || opts.hasSubIssues
	if !useSearchAPI {
		clientSideFilterActive = clientSideFilterActive ||
			opts.assignee != "" || opts.label != "" || opts.search != "" ||
			(opts.state != "" && stateLower != "all")
	}
	fetchLimit := opts.limit
	if clientSideFilterActive {
		fetchLimit = 0 // fetch all; --limit is applied after client-side filtering
	}

	var items []api.ProjectItem

	if useSearchAPI {
		repoParts := strings.Split(repoFilter, "/")

		if stateLower == "all" {
			// Dual-call strategy: fetch open and closed separately, then merge
			openFilters := api.SearchFilters{
				State:    "open",
				Assignee: opts.assignee,
				Search:   opts.search,
			}
			if opts.label != "" {
				openFilters.Labels = []string{opts.label}
			}
			closedFilters := api.SearchFilters{
				State:    "closed",
				Assignee: opts.assignee,
				Search:   opts.search,
			}
			if opts.label != "" {
				closedFilters.Labels = []string{opts.label}
			}

			openIssues, err := client.SearchRepositoryIssues(repoParts[0], repoParts[1], openFilters, fetchLimit)
			if err != nil {
				return fmt.Errorf("failed to search open issues: %w", err)
			}
			closedIssues, err := client.SearchRepositoryIssues(repoParts[0], repoParts[1], closedFilters, fetchLimit)
			if err != nil {
				return fmt.Errorf("failed to search closed issues: %w", err)
			}

			// Merge results
			allIssues := append(openIssues, closedIssues...)

			items, err = enrichIssuesWithProjectFields(client, project.ID, allIssues)
			if err != nil {
				return fmt.Errorf("failed to enrich issues with project fields: %w", err)
			}
		} else {
			// Single-call strategy: Use GitHub Search API for filtered queries
			searchFilters := api.SearchFilters{
				State:    stateLower, // defaults to "open" if empty
				Assignee: opts.assignee,
				Search:   opts.search,
			}
			if opts.label != "" {
				searchFilters.Labels = []string{opts.label}
			}

			issues, err := client.SearchRepositoryIssues(repoParts[0], repoParts[1], searchFilters, fetchLimit)
			if err != nil {
				return fmt.Errorf("failed to search issues: %w", err)
			}

			items, err = enrichIssuesWithProjectFields(client, project.ID, issues)
			if err != nil {
				return fmt.Errorf("failed to enrich issues with project fields: %w", err)
			}
		}
	} else {
		// Full fetch strategy: Use GetProjectItems when no repo filter is available
		var filter *api.ProjectItemsFilter
		if fetchLimit > 0 {
			filter = &api.ProjectItemsFilter{
				Limit: fetchLimit,
			}
		}

		items, err = client.GetProjectItems(project.ID, filter)
		if err != nil {
			return fmt.Errorf("failed to get project items: %w", err)
		}
	}

	// Apply status filter (always client-side since it's a project field)
	if opts.status != "" {
		targetStatus := cfg.ResolveFieldValue("status", opts.status)
		items = filterByFieldValue(items, "Status", targetStatus)
	}

	// Apply priority filter (always client-side since it's a project field)
	if opts.priority != "" {
		targetPriority := cfg.ResolveFieldValue("priority", opts.priority)
		items = filterByFieldValue(items, "Priority", targetPriority)
	}

	// Apply assignee filter (skip if search API was used - already filtered server-side)
	if opts.assignee != "" && !useSearchAPI {
		items = filterByAssignee(items, opts.assignee)
	}

	// Apply label filter (skip if search API was used - already filtered server-side)
	if opts.label != "" && !useSearchAPI {
		items = filterByLabel(items, opts.label)
	}

	// Apply search filter (skip if search API was used - already filtered server-side)
	if opts.search != "" && !useSearchAPI {
		items = filterBySearch(items, opts.search)
	}

	// Apply branch filter
	if opts.branch != "" {
		targetBranch := opts.branch
		if opts.branch == "current" {
			// Resolve "current" to the active branch. A failure here must surface
			// rather than silently filtering by the literal string "current"
			// (which matches nothing) — mirrors move.go's "no active branch found".
			resolved, err := resolveCurrentBranchForList(client, repoFilter)
			if err != nil {
				return err
			}
			targetBranch = resolved
		}
		// Filter by both "Branch" and "Release" field names for backward compatibility
		items = filterByBranchFieldValue(items, targetBranch)
	}

	// Apply no-branch filter (issues without branch assignment)
	if opts.noBranch {
		items = filterByEmptyBranchField(items)
	}

	// Apply state filter (skip if search API was used - already filtered server-side)
	// For "all" state with GetProjectItems, no filtering needed
	if opts.state != "" && !useSearchAPI && stateLower != "all" {
		items = filterByState(items, opts.state)
	}

	// Apply has-sub-issues filter
	var hasSubIssuesFilterFailed bool
	if opts.hasSubIssues {
		items, hasSubIssuesFilterFailed = filterByHasSubIssues(cmd, client, items)
	}

	// Apply limit
	if opts.limit > 0 && len(items) > opts.limit {
		items = items[:opts.limit]
	}

	// Handle --json flag
	if opts.jsonFields != "" {
		// Output JSON with field filtering and jq
		return outputListJSON(cmd, items, opts)
	}

	// Table output
	outputErr := outputTable(cmd, items)

	// Print note if sub-issue filter had failures
	if hasSubIssuesFilterFailed && outputErr == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "\nNote: some items included without sub-issue verification")
	}

	return outputErr
}

// filterByFieldValue filters items by a specific field value
func filterByFieldValue(items []api.ProjectItem, fieldName, value string) []api.ProjectItem {
	filtered := make([]api.ProjectItem, 0, len(items))
	for _, item := range items {
		for _, fv := range item.FieldValues {
			if strings.EqualFold(fv.Field, fieldName) && strings.EqualFold(fv.Value, value) {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

// filterByEmptyField filters items where a specific field is empty or not set
func filterByEmptyField(items []api.ProjectItem, fieldName string) []api.ProjectItem {
	var filtered []api.ProjectItem
	for _, item := range items {
		hasValue := false
		for _, fv := range item.FieldValues {
			if strings.EqualFold(fv.Field, fieldName) && fv.Value != "" {
				hasValue = true
				break
			}
		}
		if !hasValue {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// resolveCurrentBranchForList resolves --branch current to the active branch
// tracker's name. It returns an error on every failure path (no repository, a
// malformed repo, a lookup failure, or no matching tracker) so the caller never
// silently filters by the literal string "current".
func resolveCurrentBranchForList(client listClient, repoFilter string) (string, error) {
	if repoFilter == "" {
		return "", fmt.Errorf("cannot resolve --branch current: no repository configured (use --repo owner/repo)")
	}
	parts := strings.Split(repoFilter, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("cannot resolve --branch current: invalid repository %q", repoFilter)
	}

	releaseIssues, err := client.GetOpenIssuesByLabel(parts[0], parts[1], "branch")
	if err != nil {
		return "", fmt.Errorf("cannot resolve --branch current: %w", err)
	}

	for _, issue := range releaseIssues {
		var branch string
		switch {
		case strings.HasPrefix(issue.Title, "Branch: "):
			branch = strings.TrimPrefix(issue.Title, "Branch: ")
		case strings.HasPrefix(issue.Title, "Release: "):
			branch = strings.TrimPrefix(issue.Title, "Release: ")
		default:
			continue
		}
		if idx := strings.Index(branch, " ("); idx > 0 {
			branch = branch[:idx]
		}
		return strings.TrimSpace(branch), nil
	}

	return "", fmt.Errorf("no active branch found")
}

// filterByBranchFieldValue filters items by the branch field value
// Checks both "Branch" (new) and "Release" (legacy) field names for backward compatibility
func filterByBranchFieldValue(items []api.ProjectItem, value string) []api.ProjectItem {
	var filtered []api.ProjectItem
	for _, item := range items {
		for _, fv := range item.FieldValues {
			if (strings.EqualFold(fv.Field, BranchFieldName) || strings.EqualFold(fv.Field, LegacyReleaseFieldName)) &&
				strings.EqualFold(fv.Value, value) {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

// filterByEmptyBranchField filters items where the branch field is empty or not set
// Checks both "Branch" (new) and "Release" (legacy) field names for backward compatibility
func filterByEmptyBranchField(items []api.ProjectItem) []api.ProjectItem {
	var filtered []api.ProjectItem
	for _, item := range items {
		hasValue := false
		for _, fv := range item.FieldValues {
			if (strings.EqualFold(fv.Field, BranchFieldName) || strings.EqualFold(fv.Field, LegacyReleaseFieldName)) &&
				fv.Value != "" {
				hasValue = true
				break
			}
		}
		if !hasValue {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// filterByHasSubIssues filters items to only those with sub-issues.
// Uses a batch query to fetch sub-issue counts efficiently.
// Returns the filtered items and a boolean indicating if any repo queries failed.
func filterByHasSubIssues(cmd *cobra.Command, client listClient, items []api.ProjectItem) ([]api.ProjectItem, bool) {
	if len(items) == 0 {
		return nil, false
	}

	// Group items by repository for batch queries
	type repoKey struct {
		owner string
		repo  string
	}
	repoItems := make(map[repoKey][]api.ProjectItem)
	repoNumbers := make(map[repoKey][]int)

	for _, item := range items {
		if item.Issue == nil {
			continue
		}
		key := repoKey{
			owner: item.Issue.Repository.Owner,
			repo:  item.Issue.Repository.Name,
		}
		repoItems[key] = append(repoItems[key], item)
		repoNumbers[key] = append(repoNumbers[key], item.Issue.Number)
	}

	// Fetch sub-issue counts for each repository (one query per repo)
	subIssueCounts := make(map[repoKey]map[int]int)
	var failedRepos []repoKey
	for key, numbers := range repoNumbers {
		counts, err := client.GetSubIssueCounts(key.owner, key.repo, numbers)
		if err != nil {
			// Warn about the failure but don't exclude items
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not fetch sub-issue counts for %s/%s: %v\n", key.owner, key.repo, err)
			failedRepos = append(failedRepos, key)
			continue
		}
		subIssueCounts[key] = counts
	}

	// Filter items based on sub-issue counts
	var filtered []api.ProjectItem
	for key, keyItems := range repoItems {
		counts, ok := subIssueCounts[key]
		if !ok {
			// Include items from failed repos (prefer false positives over silent exclusion)
			filtered = append(filtered, keyItems...)
			continue
		}
		for _, item := range keyItems {
			if count, exists := counts[item.Issue.Number]; exists && count > 0 {
				filtered = append(filtered, item)
			}
		}
	}

	return filtered, len(failedRepos) > 0
}

// getFieldValue gets a field value from an item
func getFieldValue(item api.ProjectItem, fieldName string) string {
	for _, fv := range item.FieldValues {
		if strings.EqualFold(fv.Field, fieldName) {
			return fv.Value
		}
	}
	return ""
}

// outputTable outputs items in a table format
func outputTable(cmd *cobra.Command, items []api.ProjectItem) error {
	if len(items) == 0 {
		cmd.Println("No issues found")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NUMBER\tTITLE\tSTATUS\tPRIORITY\tASSIGNEES")

	for _, item := range items {
		if item.Issue == nil {
			continue
		}

		// Get field values
		status := getFieldValue(item, "Status")
		priority := getFieldValue(item, "Priority")

		// Format assignees
		var assignees []string
		for _, a := range item.Issue.Assignees {
			assignees = append(assignees, a.Login)
		}
		assigneeStr := strings.Join(assignees, ", ")
		if assigneeStr == "" {
			assigneeStr = "-"
		}

		// Truncate title if too long
		title := item.Issue.Title
		if len(title) > 50 {
			title = title[:47] + "..."
		}

		fmt.Fprintf(w, "#%d\t%s\t%s\t%s\t%s\n",
			item.Issue.Number,
			title,
			status,
			priority,
			assigneeStr,
		)
	}

	return w.Flush()
}

// JSONOutput represents the JSON output structure
type JSONOutput struct {
	Items []JSONItem `json:"items"`
}

// JSONItem represents an item in JSON output
type JSONItem struct {
	Number      int               `json:"number"`
	Title       string            `json:"title"`
	State       string            `json:"state"`
	URL         string            `json:"url"`
	Repository  string            `json:"repository"`
	Assignees   []string          `json:"assignees"`
	FieldValues map[string]string `json:"fieldValues"`
}

// outputJSON outputs items in JSON format
func outputJSON(cmd *cobra.Command, items []api.ProjectItem) error {
	output := JSONOutput{
		Items: make([]JSONItem, 0, len(items)),
	}

	for _, item := range items {
		if item.Issue == nil {
			continue
		}

		jsonItem := JSONItem{
			Number:      item.Issue.Number,
			Title:       item.Issue.Title,
			State:       item.Issue.State,
			URL:         item.Issue.URL,
			Repository:  fmt.Sprintf("%s/%s", item.Issue.Repository.Owner, item.Issue.Repository.Name),
			Assignees:   make([]string, 0),
			FieldValues: make(map[string]string),
		}

		for _, a := range item.Issue.Assignees {
			jsonItem.Assignees = append(jsonItem.Assignees, a.Login)
		}

		for _, fv := range item.FieldValues {
			jsonItem.FieldValues[fv.Field] = fv.Value
		}

		output.Items = append(output.Items, jsonItem)
	}

	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

// filterByAssignee filters items by assignee login
func filterByAssignee(items []api.ProjectItem, assignee string) []api.ProjectItem {
	var filtered []api.ProjectItem
	for _, item := range items {
		if item.Issue == nil {
			continue
		}
		for _, a := range item.Issue.Assignees {
			if strings.EqualFold(a.Login, assignee) {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

// filterByLabel filters items by label name
func filterByLabel(items []api.ProjectItem, label string) []api.ProjectItem {
	var filtered []api.ProjectItem
	for _, item := range items {
		if item.Issue == nil {
			continue
		}
		for _, l := range item.Issue.Labels {
			if strings.EqualFold(l.Name, label) {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

// filterBySearch filters items by searching in title and body
func filterBySearch(items []api.ProjectItem, search string) []api.ProjectItem {
	var filtered []api.ProjectItem
	searchLower := strings.ToLower(search)
	for _, item := range items {
		if item.Issue == nil {
			continue
		}
		titleLower := strings.ToLower(item.Issue.Title)
		bodyLower := strings.ToLower(item.Issue.Body)
		if strings.Contains(titleLower, searchLower) || strings.Contains(bodyLower, searchLower) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// filterByState filters items by GitHub issue state (open or closed)
func filterByState(items []api.ProjectItem, state string) []api.ProjectItem {
	var filtered []api.ProjectItem
	stateLower := strings.ToLower(state)
	for _, item := range items {
		if item.Issue == nil {
			continue
		}
		// Issue.State is OPEN or CLOSED (uppercase from GitHub API)
		if strings.EqualFold(item.Issue.State, stateLower) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// enrichIssuesWithProjectFields converts issues to ProjectItems and enriches them with project field values.
// This is used when fetching via SearchRepositoryIssues to add project-specific fields (Status, Priority, etc.).
func enrichIssuesWithProjectFields(client listClient, projectID string, issues []api.Issue) ([]api.ProjectItem, error) {
	if len(issues) == 0 {
		return nil, nil
	}

	// Collect issue IDs for batch enrichment
	issueIDs := make([]string, 0, len(issues))
	issueByID := make(map[string]*api.Issue)
	for i := range issues {
		issueIDs = append(issueIDs, issues[i].ID)
		issueByID[issues[i].ID] = &issues[i]
	}

	// Fetch project field values for all issues
	fieldValuesByID, err := client.GetProjectFieldsForIssues(projectID, issueIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get project fields: %w", err)
	}

	// Convert to ProjectItems
	items := make([]api.ProjectItem, 0, len(issues))
	for _, issue := range issues {
		item := api.ProjectItem{
			ID:    issue.ID,
			Issue: &issue,
		}

		// Add field values if available
		if fieldValues, ok := fieldValuesByID[issue.ID]; ok {
			item.FieldValues = fieldValues
		}

		items = append(items, item)
	}

	return items, nil
}

// listListAvailableFields prints available JSON fields for the list command
func listListAvailableFields(cmd *cobra.Command) error {
	return listAvailableFields(cmd, listCommandJSONFields)
}

// validateListJSONFlag checks --json before any config load or API call, so
// `list --json status` fails fast and non-zero instead of after a fetch (#926).
func validateListJSONFlag(cmd *cobra.Command, opts *listOptions) error {
	if err := requireJSONValue(cmd); err != nil {
		return err
	}
	if opts.jsonFields == "" {
		return nil
	}
	_, err := parseListJSONFields(opts.jsonFields)
	return err
}

// parseListJSONFields parses and validates comma-separated field names
func parseListJSONFields(fieldsStr string) ([]string, error) {
	parts := strings.Split(fieldsStr, ",")
	fields := make([]string, 0, len(parts))

	for _, p := range parts {
		field := strings.TrimSpace(p)
		if field == "" {
			continue
		}

		// Validate field name
		valid := false
		for _, available := range listCommandJSONFields {
			if strings.EqualFold(field, available) {
				fields = append(fields, available) // Use canonical name
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("unknown field %q in --json=%s. Run 'gh pmu list --json-fields' to see available fields", field, fieldsStr)
		}
	}

	if len(fields) == 0 {
		return nil, fmt.Errorf("no valid fields in --json=%s. Run 'gh pmu list --json-fields' to see available fields", fieldsStr)
	}

	return fields, nil
}

// applyListJQFilter applies a jq filter to JSON data
func applyListJQFilter(data []byte, jqExpr string) ([]byte, error) {
	jqCmd := exec.Command("jq", jqExpr)
	jqCmd.Stdin = strings.NewReader(string(data))

	output, err := jqCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("jq error: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to run jq: %w", err)
	}

	return output, nil
}

// filterListJSONFields filters JSON output to include only specified fields
func filterListJSONFields(items []api.ProjectItem, fields []string) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(items))

	// Create a set of requested fields for quick lookup
	fieldSet := make(map[string]bool)
	for _, f := range fields {
		fieldSet[strings.ToLower(f)] = true
	}

	for _, item := range items {
		if item.Issue == nil {
			continue
		}

		filtered := make(map[string]interface{})

		if fieldSet["number"] {
			filtered["number"] = item.Issue.Number
		}
		if fieldSet["title"] {
			filtered["title"] = item.Issue.Title
		}
		if fieldSet["state"] {
			filtered["state"] = item.Issue.State
		}
		if fieldSet["url"] {
			filtered["url"] = item.Issue.URL
		}
		if fieldSet["repository"] {
			filtered["repository"] = fmt.Sprintf("%s/%s", item.Issue.Repository.Owner, item.Issue.Repository.Name)
		}
		if fieldSet["assignees"] {
			assignees := make([]string, 0, len(item.Issue.Assignees))
			for _, a := range item.Issue.Assignees {
				assignees = append(assignees, a.Login)
			}
			filtered["assignees"] = assignees
		}
		if fieldSet["fieldvalues"] {
			fieldValues := make(map[string]string)
			for _, fv := range item.FieldValues {
				fieldValues[fv.Field] = fv.Value
			}
			filtered["fieldValues"] = fieldValues
		}

		result = append(result, filtered)
	}

	return result
}

// outputListJSON outputs items in JSON format with optional field filtering and jq
func outputListJSON(cmd *cobra.Command, items []api.ProjectItem, opts *listOptions) error {
	// Parse requested fields
	fields, err := parseListJSONFields(opts.jsonFields)
	if err != nil {
		return err
	}

	// Build filtered JSON output
	filteredItems := filterListJSONFields(items, fields)

	// Wrap in items array to match existing format
	output := map[string]interface{}{
		"items": filteredItems,
	}

	// Encode to JSON
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	// Apply jq filter if specified
	if opts.jq != "" {
		data, err = applyListJQFilter(data, opts.jq)
		if err != nil {
			return err
		}
	}

	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}
