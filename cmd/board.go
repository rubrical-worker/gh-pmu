package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// boardClient defines the interface for API methods used by board functions.
// This allows for easier testing with mock implementations.
type boardClient interface {
	GetProject(owner string, number int) (*api.Project, error)
	GetProjectItemsForBoard(projectID string, filter *api.BoardItemsFilter) ([]api.BoardItem, error)
	// Search API methods for optimized queries
	SearchRepositoryIssues(owner, repo string, filters api.SearchFilters, limit int) ([]api.Issue, error)
	GetProjectFieldsForIssues(projectID string, issueIDs []string) (map[string][]api.FieldValue, error)
}

type boardOptions struct {
	status   string
	priority string
	state    string // Issue state filter: "open", "closed", or "all"
	limit    int
	noBorder bool
	json     bool
	repo     string
}

// Box drawing characters
const (
	boardTopLeft     = "┌"
	boardTopRight    = "┐"
	boardBottomLeft  = "└"
	boardBottomRight = "┘"
	boardHorizontal  = "─"
	boardVertical    = "│"
	boardTopTee      = "┬"
	boardBottomTee   = "┴"
	boardCross       = "┼"
	boardLeftTee     = "├"
	boardRightTee    = "┤"
)

func newBoardCommand() *cobra.Command {
	opts := &boardOptions{
		limit: 10,
		state: "open", // Default to open issues only
	}

	cmd := &cobra.Command{
		Use:   "board",
		Short: "Display issues in a columnar board view",
		Long: `Display issues grouped by status in a terminal board view.

Shows issues organized in columns by their status field, similar to
a Kanban board. Each column displays issue numbers and truncated titles.

Examples:
  # Show full board with all status columns (open issues only, default)
  gh pmu board

  # Include closed issues
  gh pmu board --state all

  # Show only closed issues
  gh pmu board --state closed

  # Show only a single status column
  gh pmu board --status in_progress

  # Filter by priority across all columns
  gh pmu board --priority p0

  # Limit items per column
  gh pmu board --limit 5

  # Output without borders (simpler display)
  gh pmu board --no-border

  # Output as JSON grouped by status
  gh pmu board --json

  # Show board for a different repository
  gh pmu board --repo owner/other-repo`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBoard(cmd, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.status, "status", "s", "", "Show only specified status column")
	cmd.Flags().StringVarP(&opts.priority, "priority", "p", "", "Filter by priority")
	cmd.Flags().StringVar(&opts.state, "state", "open", "Filter by issue state: open, closed, or all")
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 10, "Limit issues per column")
	cmd.Flags().BoolVar(&opts.noBorder, "no-border", false, "Display without box borders")
	cmd.Flags().BoolVar(&opts.json, "json", false, "Output as JSON grouped by status")
	cmd.Flags().StringVarP(&opts.repo, "repo", "R", "", "Filter by repository (owner/repo format)")

	return cmd
}

func runBoard(cmd *cobra.Command, opts *boardOptions) error {
	// Load configuration
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	cfg, err := config.LoadFromDirectory(cwd)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'gh pmu init' to create a configuration file", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create API client
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	return runBoardWithDeps(cmd, opts, cfg, client)
}

// runBoardWithDeps is the testable implementation of runBoard
func runBoardWithDeps(cmd *cobra.Command, opts *boardOptions, cfg *config.Config, client boardClient) error {
	// Validate --state up front so an invalid value fails loudly instead of
	// silently filtering out every item and rendering an empty board.
	switch strings.ToLower(opts.state) {
	case "", "open", "closed", "all":
		// valid
	default:
		return fmt.Errorf("invalid --state %q: must be one of open, closed, all", opts.state)
	}

	// Get project
	project, err := client.GetProject(cfg.Project.Owner, cfg.Project.Number)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	// Determine repository filter (--repo flag takes precedence over config)
	repoFilter := ""
	if opts.repo != "" {
		// Validate repo format
		parts := strings.Split(opts.repo, "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid --repo format: expected owner/repo, got %s", opts.repo)
		}
		repoFilter = opts.repo
	} else if len(cfg.Repositories) > 0 {
		repoFilter = cfg.Repositories[0]
	}

	var items []api.BoardItem

	// Determine query strategy based on repo availability
	// When repo is available: always use SearchRepositoryIssues (optimized)
	//   - "all" state: dual calls (open + closed) merged
	//   - "open"/"closed": single call
	// When no repo: fall back to GetProjectItemsForBoard (full project scan)
	useSearchAPI := repoFilter != ""

	if useSearchAPI {
		repoParts := strings.SplitN(repoFilter, "/", 2)
		if len(repoParts) != 2 {
			return fmt.Errorf("invalid repository format: %s (expected owner/repo)", repoFilter)
		}

		if opts.state == "all" {
			// Dual-call strategy: fetch open and closed separately, then merge
			openIssues, err := client.SearchRepositoryIssues(repoParts[0], repoParts[1], api.SearchFilters{State: "open"}, 0)
			if err != nil {
				return fmt.Errorf("failed to search open issues: %w", err)
			}
			closedIssues, err := client.SearchRepositoryIssues(repoParts[0], repoParts[1], api.SearchFilters{State: "closed"}, 0)
			if err != nil {
				return fmt.Errorf("failed to search closed issues: %w", err)
			}

			allIssues := append(openIssues, closedIssues...)
			items, err = enrichIssuesToBoardItems(client, project.ID, allIssues, repoFilter)
			if err != nil {
				return fmt.Errorf("failed to enrich issues: %w", err)
			}
		} else {
			// Single-call strategy
			searchFilters := api.SearchFilters{
				State: opts.state,
			}

			issues, err := client.SearchRepositoryIssues(repoParts[0], repoParts[1], searchFilters, 0)
			if err != nil {
				return fmt.Errorf("failed to search issues: %w", err)
			}

			items, err = enrichIssuesToBoardItems(client, project.ID, issues, repoFilter)
			if err != nil {
				return fmt.Errorf("failed to enrich issues: %w", err)
			}
		}
	} else {
		// Fallback: Use GetProjectItemsForBoard when no repo filter available
		items, err = client.GetProjectItemsForBoard(project.ID, nil)
		if err != nil {
			return fmt.Errorf("failed to get project items: %w", err)
		}
		warnTruncatedBoardItems(cmd.ErrOrStderr(), items)

		// Apply state filter client-side (only needed for fallback path)
		if opts.state != "" && opts.state != "all" {
			items = filterBoardItemsByState(items, opts.state)
		}
	}

	// Apply priority filter if specified
	if opts.priority != "" {
		targetPriority := cfg.ResolveFieldValue("priority", opts.priority)
		items = filterBoardItemsByPriority(items, targetPriority)
	}

	// Get status columns from config
	columns := getStatusColumns(cfg)

	// If --status is specified, filter to single column
	if opts.status != "" {
		targetStatus := cfg.ResolveFieldValue("status", opts.status)
		filtered := []statusColumn{}
		for _, col := range columns {
			if strings.EqualFold(col.value, targetStatus) {
				filtered = append(filtered, col)
				break
			}
		}
		if len(filtered) == 0 {
			// If not found in config, create a column for the raw value
			filtered = append(filtered, statusColumn{alias: opts.status, value: targetStatus})
		}
		columns = filtered
	}

	// Group items by status
	grouped := groupBoardItemsByStatus(items, columns)

	// Surface items whose status is not a configured column (empty/"(none)" or an
	// unrecognized value) as extra columns so they are not silently dropped from
	// every output mode. Skipped when --status narrows the board to one column,
	// where excluding other statuses is the requested behavior.
	if opts.status == "" {
		columns = appendUnrecognizedColumns(columns, grouped)
	}

	// Apply limit per column
	for status, columnItems := range grouped {
		if opts.limit > 0 && len(columnItems) > opts.limit {
			grouped[status] = columnItems[:opts.limit]
		}
	}

	// Output
	if opts.json {
		return outputBoardJSON(cmd, grouped, columns)
	}

	if opts.noBorder {
		return outputBoardSimple(cmd, grouped, columns)
	}

	return outputBoardBox(cmd, grouped, columns, opts.limit)
}

// statusColumn represents a status column for the board
type statusColumn struct {
	alias string
	value string
}

// getStatusColumns extracts status columns from config in order
func getStatusColumns(cfg *config.Config) []statusColumn {
	var columns []statusColumn

	// Try to get from config fields
	if statusField, ok := cfg.Fields["status"]; ok && len(statusField.Values) > 0 {
		// Note: Go maps are unordered, so we'll use a predefined order
		// that matches common workflow patterns
		preferredOrder := []string{"backlog", "ready", "in_progress", "in_review", "done"}

		for _, alias := range preferredOrder {
			if value, ok := statusField.Values[alias]; ok {
				columns = append(columns, statusColumn{alias: alias, value: value})
			}
		}

		// Add any remaining statuses not in preferred order
		for alias, value := range statusField.Values {
			found := false
			for _, col := range columns {
				if col.alias == alias {
					found = true
					break
				}
			}
			if !found {
				columns = append(columns, statusColumn{alias: alias, value: value})
			}
		}
	}

	// Fallback to defaults if no config
	if len(columns) == 0 {
		columns = []statusColumn{
			{alias: "backlog", value: "Backlog"},
			{alias: "in_progress", value: "In progress"},
			{alias: "in_review", value: "In review"},
			{alias: "done", value: "Done"},
		}
	}

	return columns
}

// filterBoardItemsByPriority filters board items by priority value
func filterBoardItemsByPriority(items []api.BoardItem, priority string) []api.BoardItem {
	filtered := make([]api.BoardItem, 0, len(items))
	for _, item := range items {
		if strings.EqualFold(item.Priority, priority) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// filterBoardItemsByState filters board items by issue state (open/closed)
func filterBoardItemsByState(items []api.BoardItem, state string) []api.BoardItem {
	var filtered []api.BoardItem
	stateLower := strings.ToLower(state)
	for _, item := range items {
		// BoardItem.State is OPEN or CLOSED (uppercase from GitHub API)
		if strings.EqualFold(item.State, stateLower) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// warnTruncatedBoardItems names the items whose field values ran past one page,
// whose Status or Priority may therefore show as unset (#934).
func warnTruncatedBoardItems(w io.Writer, items []api.BoardItem) {
	var refs []string
	for _, item := range items {
		if item.FieldValuesTruncated {
			refs = append(refs, fmt.Sprintf("#%d", item.Number))
		}
	}
	if len(refs) == 0 {
		return
	}
	fmt.Fprintf(w, "warning: field values truncated for %s; Status or Priority may be missing\n", strings.Join(refs, ", "))
}

// enrichIssuesToBoardItems converts issues to BoardItems and enriches them with project field values.
// This is used when fetching via SearchRepositoryIssues to add project-specific fields (Status, Priority, etc.).
func enrichIssuesToBoardItems(client boardClient, projectID string, issues []api.Issue, repository string) ([]api.BoardItem, error) {
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

	// Convert to BoardItems
	items := make([]api.BoardItem, 0, len(issues))
	for _, issue := range issues {
		item := api.BoardItem{
			Number:     issue.Number,
			Title:      issue.Title,
			State:      issue.State,
			Repository: repository,
		}

		// Add field values if available
		if fieldValues, ok := fieldValuesByID[issue.ID]; ok {
			for _, fv := range fieldValues {
				switch strings.ToLower(fv.Field) {
				case "status":
					item.Status = fv.Value
				case "priority":
					item.Priority = fv.Value
				}
			}
		}

		items = append(items, item)
	}

	return items, nil
}

// groupBoardItemsByStatus groups board items by their status field value
func groupBoardItemsByStatus(items []api.BoardItem, columns []statusColumn) map[string][]api.BoardItem {
	grouped := make(map[string][]api.BoardItem)

	// Initialize all columns
	for _, col := range columns {
		grouped[col.value] = []api.BoardItem{}
	}

	// Group items
	for _, item := range items {
		status := item.Status
		if status == "" {
			status = "(none)"
		}
		grouped[status] = append(grouped[status], item)
	}

	return grouped
}

// appendUnrecognizedColumns appends synthetic columns for any grouped status
// bucket that holds items but is not already a configured column. Without this,
// items whose status is empty ("(none)") or an unrecognized value are dropped
// from every output mode, which iterates only over the configured columns.
// Unknown statuses are appended in a stable (sorted) order.
func appendUnrecognizedColumns(columns []statusColumn, grouped map[string][]api.BoardItem) []statusColumn {
	known := make(map[string]bool, len(columns))
	for _, col := range columns {
		known[col.value] = true
	}

	var unknown []string
	for status, items := range grouped {
		if len(items) > 0 && !known[status] {
			unknown = append(unknown, status)
		}
	}
	sort.Strings(unknown)

	for _, status := range unknown {
		columns = append(columns, statusColumn{alias: status, value: status})
	}
	return columns
}

// safeFdToInt converts a uintptr file descriptor to int safely.
// Returns (0, false) if the value would overflow int.
func safeFdToInt(fd uintptr) (int, bool) {
	if fd > uintptr(math.MaxInt) {
		return 0, false
	}
	return int(fd), true
}

// getTerminalWidth returns the terminal width or a default
func getTerminalWidth() int {
	fd, ok := safeFdToInt(os.Stdout.Fd())
	if !ok {
		return 120 // default width; fd overflows int
	}
	width, _, err := term.GetSize(fd)
	if err != nil || width <= 0 {
		return 120 // default width
	}
	return width
}

// truncateString truncates a string to maxLen runes (not bytes) with ellipsis,
// so multi-byte UTF-8 titles are never sliced mid-rune.
func truncateString(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// outputBoardBox outputs the board with box drawing characters
func outputBoardBox(cmd *cobra.Command, grouped map[string][]api.BoardItem, columns []statusColumn, limit int) error {
	termWidth := getTerminalWidth()
	numCols := len(columns)
	if numCols == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No status columns configured")
		return nil
	}

	// Calculate column width
	colWidth := (termWidth - numCols - 1) / numCols
	if colWidth < 15 {
		colWidth = 15
	}
	if colWidth > 30 {
		colWidth = 30
	}

	// Find max rows needed
	maxRows := 0
	for _, col := range columns {
		items := grouped[col.value]
		if len(items) > maxRows {
			maxRows = len(items)
		}
	}
	if limit > 0 && maxRows > limit {
		maxRows = limit
	}

	out := cmd.OutOrStdout()

	// Top border
	fmt.Fprint(out, boardTopLeft)
	for i := range columns {
		fmt.Fprint(out, strings.Repeat(boardHorizontal, colWidth))
		if i < numCols-1 {
			fmt.Fprint(out, boardTopTee)
		}
	}
	fmt.Fprintln(out, boardTopRight)

	// Header row
	fmt.Fprint(out, boardVertical)
	for _, col := range columns {
		items := grouped[col.value]
		header := fmt.Sprintf("%s (%d)", col.value, len(items))
		header = truncateString(header, colWidth-2)
		padding := colWidth - utf8.RuneCountInString(header) - 1
		if padding < 0 {
			padding = 0
		}
		fmt.Fprintf(out, " %s%s", header, strings.Repeat(" ", padding))
		fmt.Fprint(out, boardVertical)
	}
	fmt.Fprintln(out)

	// Header separator
	fmt.Fprint(out, boardLeftTee)
	for i := range columns {
		fmt.Fprint(out, strings.Repeat(boardHorizontal, colWidth))
		if i < numCols-1 {
			fmt.Fprint(out, boardCross)
		}
	}
	fmt.Fprintln(out, boardRightTee)

	// Data rows
	for row := 0; row < maxRows; row++ {
		fmt.Fprint(out, boardVertical)
		for _, col := range columns {
			items := grouped[col.value]
			var cell string
			if row < len(items) {
				item := items[row]
				cell = fmt.Sprintf("#%d %s", item.Number, item.Title)
			}
			cell = truncateString(cell, colWidth-2)
			padding := colWidth - utf8.RuneCountInString(cell) - 1
			if padding < 0 {
				padding = 0
			}
			fmt.Fprintf(out, " %s%s", cell, strings.Repeat(" ", padding))
			fmt.Fprint(out, boardVertical)
		}
		fmt.Fprintln(out)
	}

	// Bottom border
	fmt.Fprint(out, boardBottomLeft)
	for i := range columns {
		fmt.Fprint(out, strings.Repeat(boardHorizontal, colWidth))
		if i < numCols-1 {
			fmt.Fprint(out, boardBottomTee)
		}
	}
	fmt.Fprintln(out, boardBottomRight)

	return nil
}

// outputBoardSimple outputs the board without borders
func outputBoardSimple(cmd *cobra.Command, grouped map[string][]api.BoardItem, columns []statusColumn) error {
	out := cmd.OutOrStdout()

	for _, col := range columns {
		items := grouped[col.value]
		fmt.Fprintf(out, "\n## %s (%d)\n", col.value, len(items))
		if len(items) == 0 {
			fmt.Fprintln(out, "  (empty)")
			continue
		}
		for _, item := range items {
			fmt.Fprintf(out, "  #%d %s\n", item.Number, item.Title)
		}
	}
	fmt.Fprintln(out)

	return nil
}

// outputBoardJSON outputs the board as JSON
func outputBoardJSON(cmd *cobra.Command, grouped map[string][]api.BoardItem, columns []statusColumn) error {
	type jsonIssue struct {
		Number   int    `json:"number"`
		Title    string `json:"title"`
		Priority string `json:"priority,omitempty"`
	}
	type jsonColumn struct {
		Status string      `json:"status"`
		Count  int         `json:"count"`
		Issues []jsonIssue `json:"issues"`
	}

	var output []jsonColumn
	for _, col := range columns {
		items := grouped[col.value]
		jc := jsonColumn{
			Status: col.value,
			Count:  len(items),
			Issues: []jsonIssue{},
		}
		for _, item := range items {
			jc.Issues = append(jc.Issues, jsonIssue{
				Number:   item.Number,
				Title:    item.Title,
				Priority: item.Priority,
			})
		}
		output = append(output, jc)
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}
