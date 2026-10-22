package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/spf13/cobra"
)

type moveOptions struct {
	status    string
	priority  string
	branch    string // branch field (formerly release)
	backlog   bool
	recursive bool
	depth     int
	dryRun    bool
	force     bool   // bypass checkbox validation
	yes       bool   // skip confirmation
	repo      string // repository override (owner/repo format)
}

// moveClient defines the interface for API methods used by move functions.
// This allows for easier testing with mock implementations.
type moveClient interface {
	GetIssue(owner, repo string, number int) (*api.Issue, error)
	GetProject(owner string, number int) (*api.Project, error)
	GetProjectFields(projectID string) ([]api.ProjectField, error)
	GetProjectItems(projectID string, filter *api.ProjectItemsFilter) ([]api.ProjectItem, error)
	GetProjectItemsByIssues(projectID string, refs []api.IssueRef) ([]api.ProjectItem, error)
	GetSubIssues(owner, repo string, number int) ([]api.SubIssue, error)
	GetSubIssuesBatch(owner, repo string, numbers []int) (map[int][]api.SubIssue, error)
	SetProjectItemField(projectID, itemID, fieldName, value string) error
	SetProjectItemFieldWithFields(projectID, itemID, fieldName, value string, fields []api.ProjectField) error
	BatchUpdateProjectItemFields(projectID string, updates []api.FieldUpdate, fields []api.ProjectField) ([]api.BatchUpdateResult, error)
	GetOpenIssuesByLabel(owner, repo, label string) ([]api.Issue, error)
	AddLabelToIssue(owner, repo, issueID, labelName string) error
	RemoveLabelFromIssue(owner, repo, issueID, labelName string) error
}

func newMoveCommand() *cobra.Command {
	opts := &moveOptions{
		depth: 10, // default max depth
	}

	cmd := &cobra.Command{
		Use:   "move <issue-number> [issue-number...]",
		Short: "Update project fields for multiple issues at once",
		Long: `Update project field values for one or more issues.

Changes the status, priority, or other project fields for issues
that are already in the configured project.

Field values are resolved through config aliases, so you can use
shorthand values like "in_progress" which will be mapped to "In Progress".

Use --recursive to update all sub-issues as well. This will traverse
the issue tree and apply the same changes to all descendants.`,
		Example: `  # Move a single issue to "In Progress"
  gh pmu move 42 --status in_progress

  # Move multiple issues at once
  gh pmu move 42 43 44 --status done

  # Set both status and priority
  gh pmu move 42 --status done --priority p1

  # Add issue to the current active branch
  gh pmu move 42 --branch current

  # Add issue to a specific branch
  gh pmu move 42 --branch release/v1.2.0

  # Return an issue to backlog (clears branch field)
  gh pmu move 42 --backlog

  # Recursively update an epic and all its sub-issues
  gh pmu move 10 --status in_progress --recursive

  # Preview recursive changes without applying (dry-run)
  gh pmu move 10 --status done --recursive --dry-run

  # Recursively update, skip confirmation prompt
  gh pmu move 10 --status backlog --recursive --yes

  # Limit recursion depth (default is 10)
  gh pmu move 10 --status in_progress --recursive --depth 2

  # Specify repository explicitly
  gh pmu move 42 --status done --repo owner/repo`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMove(cmd, args, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.status, "status", "s", "", "Set project status field")
	cmd.Flags().StringVarP(&opts.priority, "priority", "p", "", "Set project priority field")
	cmd.Flags().StringVarP(&opts.branch, "branch", "b", "", "Set branch field (use 'current' for active branch)")
	cmd.Flags().BoolVar(&opts.backlog, "backlog", false, "Clear branch field (return to backlog)")
	cmd.Flags().BoolVarP(&opts.recursive, "recursive", "r", false, "Apply changes to all sub-issues recursively")
	cmd.Flags().IntVar(&opts.depth, "depth", 10, "Maximum depth for recursive operations")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show what would be changed without making changes")
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Bypass checkbox validation (still requires body and branch)")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompts (for --recursive)")
	cmd.Flags().StringVarP(&opts.repo, "repo", "R", "", "Repository for the issue (owner/repo format)")

	return cmd
}

// issueInfo holds information about an issue to be updated
type issueInfo struct {
	Owner       string
	Repo        string
	Number      int
	Title       string
	Body        string
	IssueID     string // GitHub node ID for label operations
	State       string // Issue state (OPEN, CLOSED)
	ItemID      string
	Depth       int
	FieldValues []api.FieldValue
}

func runMove(cmd *cobra.Command, args []string, opts *moveOptions) error {
	// Validate at least one flag is provided
	if opts.status == "" && opts.priority == "" && opts.branch == "" && !opts.backlog {
		return fmt.Errorf("at least one of --status, --priority, --branch, or --backlog is required")
	}

	// Validate --backlog cannot be combined with --branch
	if opts.backlog && opts.branch != "" {
		return fmt.Errorf("--backlog cannot be combined with --branch")
	}

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

	return runMoveWithDeps(cmd, args, opts, cfg, client)
}

// runMoveWithDeps is the testable implementation of runMove
// runMoveWithDeps is the testable implementation of runMove
func runMoveWithDeps(cmd *cobra.Command, args []string, opts *moveOptions, cfg *config.Config, client moveClient) error {
	// Determine default repository (--repo flag takes precedence over config).
	// move tolerates empty defaults because issue args may be fully qualified
	// (owner/repo#N), so a malformed config repo is skipped rather than fatal — but
	// an explicit --repo is validated with uniform empty-component rejection.
	defaultOwner, defaultRepo := "", ""
	if opts.repo != "" {
		var rErr error
		defaultOwner, defaultRepo, rErr = splitOwnerRepo(opts.repo, "--repo")
		if rErr != nil {
			return rErr
		}
	} else if len(cfg.Repositories) > 0 {
		if o, r, cErr := splitOwnerRepo(cfg.Repositories[0], "configured repository"); cErr == nil {
			defaultOwner, defaultRepo = o, r
		}
	}

	// Get project (once for all issues)
	project, err := client.GetProject(cfg.Project.Owner, cfg.Project.Number)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	// Pre-parse all arguments to build issue references
	var issueRefs []api.IssueRef
	var parseErrors []string
	for _, arg := range args {
		owner, repo, number, err := parseIssueReference(arg)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("#%s: %v", arg, err))
			continue
		}
		if owner == "" || repo == "" {
			if defaultOwner == "" || defaultRepo == "" {
				parseErrors = append(parseErrors, fmt.Sprintf("#%d: no repository specified", number))
				continue
			}
			owner = defaultOwner
			repo = defaultRepo
		}
		issueRefs = append(issueRefs, api.IssueRef{Owner: owner, Repo: repo, Number: number})
	}

	// Get project items using targeted queries (never full project scan)
	var items []api.ProjectItem
	if len(issueRefs) > 0 {
		// Targeted query for specific issues
		items, err = client.GetProjectItemsByIssues(project.ID, issueRefs)
		if err != nil {
			// Fall back to full fetch if targeted query fails
			items, err = client.GetProjectItems(project.ID, nil)
			if err != nil {
				return fmt.Errorf("failed to get project items: %w", err)
			}
		}
	} else {
		// No valid issue refs, still need to fetch to report errors properly
		items, err = client.GetProjectItems(project.ID, nil)
		if err != nil {
			return fmt.Errorf("failed to get project items: %w", err)
		}
	}

	// Build maps for quick lookup: item IDs, field values, and issue data (title/body)
	itemIDMap := make(map[string]string)
	itemFieldsMap := make(map[string][]api.FieldValue)
	itemDataMap := make(map[string]*api.Issue)
	for _, item := range items {
		if item.Issue != nil {
			key := fmt.Sprintf("%s/%s#%d", item.Issue.Repository.Owner, item.Issue.Repository.Name, item.Issue.Number)
			itemIDMap[key] = item.ID
			itemFieldsMap[key] = item.FieldValues
			itemDataMap[key] = item.Issue
		}
	}

	// Collect all issues to update from all args
	var issuesToUpdate []issueInfo
	var collectionErrors []string
	// Sub-issue trees that could not be listed or enriched; the rest of the
	// move goes ahead and the gap is reported at the end (#938).
	var collectFailures partialFailures
	hasErrors := false

	// Add parse errors to collection errors
	collectionErrors = append(collectionErrors, parseErrors...)
	if len(parseErrors) > 0 {
		hasErrors = true
	}

	for _, ref := range issueRefs {
		owner, repo, number := ref.Owner, ref.Repo, ref.Number

		rootKey := fmt.Sprintf("%s/%s#%d", owner, repo, number)
		rootItemID, inProject := itemIDMap[rootKey]
		if !inProject {
			collectionErrors = append(collectionErrors, fmt.Sprintf("#%d: not in project", number))
			hasErrors = true
			continue
		}

		// Use issue data from project items (batch-fetched) instead of individual API call
		issueData, hasData := itemDataMap[rootKey]
		if !hasData {
			// Fallback to API call if somehow not in map (shouldn't happen for items in project)
			issue, err := client.GetIssue(owner, repo, number)
			if err != nil {
				collectionErrors = append(collectionErrors, fmt.Sprintf("#%d: %v", number, err))
				hasErrors = true
				continue
			}
			issueData = issue
		}

		issuesToUpdate = append(issuesToUpdate, issueInfo{
			Owner:       owner,
			Repo:        repo,
			Number:      number,
			Title:       issueData.Title,
			Body:        issueData.Body,
			IssueID:     issueData.ID,
			State:       issueData.State,
			ItemID:      rootItemID,
			Depth:       0,
			FieldValues: itemFieldsMap[rootKey],
		})

		if opts.recursive {
			// Collect sub-issue tree (maps may be partially populated from root query)
			subIssues, err := collectSubIssuesRecursive(client, owner, repo, number, itemIDMap, itemFieldsMap, itemDataMap, 1, opts.depth)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to collect sub-issues for #%d: %v\n", number, err)
				collectFailures.add(fmt.Sprintf("move sub-issues of #%d", number), err)
			}
			{
				// Enrich sub-issues not yet in the maps with targeted project item query
				var missingRefs []api.IssueRef
				for _, sub := range subIssues {
					subKey := fmt.Sprintf("%s/%s#%d", sub.Owner, sub.Repo, sub.Number)
					if _, exists := itemIDMap[subKey]; !exists {
						missingRefs = append(missingRefs, api.IssueRef{Owner: sub.Owner, Repo: sub.Repo, Number: sub.Number})
					}
				}
				if len(missingRefs) > 0 {
					subItems, serr := client.GetProjectItemsByIssues(project.ID, missingRefs)
					if serr != nil {
						// Swallowing this made the affected sub-issues look like they
						// were "(not in project, will skip)". Surface the real cause.
						fmt.Fprintf(os.Stderr, "Warning: failed to enrich %d sub-issue(s) with project data; they may be misreported as not in project: %v\n", len(missingRefs), serr)
						collectFailures.add(fmt.Sprintf("move %d sub-issue(s) of #%d", len(missingRefs), number), serr)
					}
					if serr == nil {
						for _, item := range subItems {
							if item.Issue != nil {
								key := fmt.Sprintf("%s/%s#%d", item.Issue.Repository.Owner, item.Issue.Repository.Name, item.Issue.Number)
								itemIDMap[key] = item.ID
								itemFieldsMap[key] = item.FieldValues
								itemDataMap[key] = item.Issue
							}
						}
						// Re-populate sub-issue item IDs from enriched maps
						for i := range subIssues {
							subKey := fmt.Sprintf("%s/%s#%d", subIssues[i].Owner, subIssues[i].Repo, subIssues[i].Number)
							subIssues[i].ItemID = itemIDMap[subKey]
							subIssues[i].FieldValues = itemFieldsMap[subKey]
							if data, ok := itemDataMap[subKey]; ok {
								subIssues[i].Body = data.Body
								subIssues[i].IssueID = data.ID
								subIssues[i].State = data.State
							}
						}
					}
				}
				issuesToUpdate = append(issuesToUpdate, subIssues...)
			}
		}
	}

	for _, errMsg := range collectionErrors {
		fmt.Fprintf(os.Stderr, "Error: %s\n", errMsg)
	}

	if len(issuesToUpdate) == 0 {
		return fmt.Errorf("no valid issues to update")
	}

	statusValue := ""
	priorityValue := ""
	releaseValue := ""
	clearRelease := false
	var changeDescriptions []string

	if opts.status != "" {
		if err := cfg.ValidateFieldValue("status", opts.status); err != nil {
			return err
		}
		statusValue = cfg.ResolveFieldValue("status", opts.status)
		changeDescriptions = append(changeDescriptions, fmt.Sprintf("Status -> %s", statusValue))
	}
	if opts.priority != "" {
		if err := cfg.ValidateFieldValue("priority", opts.priority); err != nil {
			return err
		}
		priorityValue = cfg.ResolveFieldValue("priority", opts.priority)
		changeDescriptions = append(changeDescriptions, fmt.Sprintf("Priority -> %s", priorityValue))
	}
	if opts.backlog {
		// --backlog clears branch field and sets status to Backlog (unless --status explicitly provided)
		clearRelease = true
		changeDescriptions = append(changeDescriptions, "Branch -> (cleared)")
		if opts.status == "" {
			statusValue = cfg.ResolveFieldValue("status", "backlog")
			changeDescriptions = append(changeDescriptions, fmt.Sprintf("Status -> %s", statusValue))
		}
	}
	if opts.branch != "" {
		if opts.branch == "current" {
			firstOwner := issuesToUpdate[0].Owner
			firstRepo := issuesToUpdate[0].Repo
			branchIssues, err := client.GetOpenIssuesByLabel(firstOwner, firstRepo, "branch")
			if err != nil {
				return fmt.Errorf("failed to get branch issues: %w", err)
			}
			activeTracker := findActiveBranchForMove(branchIssues)
			if activeTracker == nil {
				return fmt.Errorf("no active branch found")
			}
			// Support both "Branch: " and "Release: " prefixes for backwards compatibility
			releaseValue = strings.TrimPrefix(activeTracker.Title, "Branch: ")
			releaseValue = strings.TrimPrefix(releaseValue, "Release: ")
		} else {
			releaseValue = opts.branch
		}
		changeDescriptions = append(changeDescriptions, fmt.Sprintf("Branch -> %s", releaseValue))
	}

	// Validate IDPF rules before making any changes (all-or-nothing)
	// Build validation results map for dry-run display
	var validationErrors ValidationErrors
	var forceWarnings []string
	validationResults := make(map[int]string) // issue number -> validation status

	if cfg.IsIDPF() && statusValue != "" {
		// Discover active releases from GitHub
		var activeReleases []string
		if len(issuesToUpdate) > 0 {
			firstIssue := issuesToUpdate[0]
			releaseIssues, err := client.GetOpenIssuesByLabel(firstIssue.Owner, firstIssue.Repo, "branch")
			if err == nil {
				activeReleases = discoverActiveReleases(releaseIssues)
			}
		}

		for _, info := range issuesToUpdate {
			if info.ItemID == "" {
				validationResults[info.Number] = "skip"
				continue // Skip issues not in project
			}
			ctx := buildValidationContext(info.Number, info.Body, info.FieldValues, activeReleases)
			if err := validateStatusTransition(cfg, ctx, statusValue, releaseValue, opts.force); err != nil {
				validationErrors.Add(*err)
				validationResults[info.Number] = err.Message
			} else if opts.force && countUncheckedBoxes(info.Body) > 0 {
				// Track --force bypasses for warning
				forceWarnings = append(forceWarnings, fmt.Sprintf("#%d has %d unchecked checkbox(es)", info.Number, countUncheckedBoxes(info.Body)))
				validationResults[info.Number] = "pass (--force)"
			} else {
				validationResults[info.Number] = "pass"
			}
		}

		// In non-dry-run mode, fail early on validation errors
		if !opts.dryRun && validationErrors.HasErrors() {
			return &validationErrors
		}
		// Warn when --force bypasses checkbox validation. forceWarnings is only
		// populated when opts.force is set, and --force is itself the explicit
		// confirmation to bypass (issue #778: --force implies --yes), so no
		// additional confirmation gate is required here. The previous
		// `!opts.yes && !opts.force` guard was unreachable dead code.
		if !opts.dryRun && len(forceWarnings) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "Warning: --force bypasses checkbox validation:\n")
			for _, w := range forceWarnings {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", w)
			}
			fmt.Fprintln(cmd.OutOrStdout())
		}
	}

	multiIssueMode := len(args) > 1 || opts.recursive
	w := cmd.OutOrStdout()

	if multiIssueMode || opts.dryRun {
		if opts.dryRun {
			fmt.Fprintln(w, "Dry run - no changes will be made")
			fmt.Fprintln(w)
		}

		fmt.Fprintf(w, "Issues to update (%d):\n", len(issuesToUpdate))
		for _, info := range issuesToUpdate {
			indent := strings.Repeat("  ", info.Depth)
			status := validationResults[info.Number]
			if info.ItemID == "" {
				fmt.Fprintf(w, "%s* #%d - %s (not in project, will skip)\n", indent, info.Number, info.Title)
			} else if opts.dryRun && status != "" && status != "pass" && status != "pass (--force)" && status != "skip" {
				// Show validation failure in dry-run mode
				fmt.Fprintf(w, "%s* #%d - %s [FAIL: %s]\n", indent, info.Number, info.Title, status)
			} else if opts.dryRun && status == "pass (--force)" {
				fmt.Fprintf(w, "%s* #%d - %s [PASS with --force]\n", indent, info.Number, info.Title)
			} else {
				fmt.Fprintf(w, "%s* #%d - %s\n", indent, info.Number, info.Title)
			}
		}

		fmt.Fprintln(w, "\nChanges to apply:")
		for _, desc := range changeDescriptions {
			fmt.Fprintf(w, "  * %s\n", desc)
		}

		if opts.dryRun {
			// Show validation summary in dry-run mode
			if validationErrors.HasErrors() {
				fmt.Fprintln(w)
				fmt.Fprintln(w, "Validation would FAIL:")
				for _, e := range validationErrors.Errors {
					fmt.Fprintf(w, "  - Issue #%d: %s\n", e.IssueNumber, e.Message)
				}
				fmt.Fprintln(w, "\nFix all issues or use --force to bypass.")
			} else {
				fmt.Fprintln(w, "\nValidation: PASS")
			}
			return nil
		}

		if !opts.yes {
			return fmt.Errorf("use --yes to confirm bulk update of %d issues", len(issuesToUpdate))
		}
		fmt.Fprintln(w)
	}

	// Cache project fields once before the update loop to avoid N+1 API calls
	projectFields, err := client.GetProjectFields(project.ID)
	if err != nil {
		return fmt.Errorf("failed to get project fields: %w", err)
	}

	// Resolve branch field name (Branch for new projects, Release for legacy)
	branchFieldName := ResolveBranchFieldName(projectFields)

	updatedCount := 0
	skippedCount := 0
	errorCount := 0

	// Collect all field updates for batch execution
	var allUpdates []api.FieldUpdate
	itemToIssue := make(map[string]*issueInfo) // itemID -> issueInfo for result processing

	for i := range issuesToUpdate {
		info := &issuesToUpdate[i]
		if info.ItemID == "" {
			// Skip issues without project item IDs (already reported as warnings)
			continue
		}

		itemToIssue[info.ItemID] = info

		// Collect updates for this issue
		if statusValue != "" {
			allUpdates = append(allUpdates, api.FieldUpdate{
				ItemID:    info.ItemID,
				FieldName: "Status",
				Value:     statusValue,
			})
		}
		if priorityValue != "" {
			allUpdates = append(allUpdates, api.FieldUpdate{
				ItemID:    info.ItemID,
				FieldName: "Priority",
				Value:     priorityValue,
			})
		}
		if releaseValue != "" {
			allUpdates = append(allUpdates, api.FieldUpdate{
				ItemID:    info.ItemID,
				FieldName: branchFieldName,
				Value:     releaseValue,
			})
		}
		if clearRelease {
			allUpdates = append(allUpdates, api.FieldUpdate{
				ItemID:    info.ItemID,
				FieldName: branchFieldName,
				Value:     "",
			})
		}
	}

	// Execute batch mutations
	var results []api.BatchUpdateResult
	if len(allUpdates) > 0 {
		results, err = client.BatchUpdateProjectItemFields(project.ID, allUpdates, projectFields)
		if err != nil {
			// Batch failed entirely - fall back to sequential updates
			fmt.Fprintf(os.Stderr, "Warning: batch update failed, falling back to sequential: %v\n", err)
			results = nil // Process sequentially below
		}
	}

	// Process results and report per-issue
	// Group results by itemID
	itemResults := make(map[string][]api.BatchUpdateResult)
	for _, result := range results {
		itemResults[result.ItemID] = append(itemResults[result.ItemID], result)
	}

	// Report results for each issue in order
	for _, info := range issuesToUpdate {
		indent := strings.Repeat("  ", info.Depth)
		if multiIssueMode {
			fmt.Fprintf(w, "%sUpdating #%d... ", indent, info.Number)
		}

		if info.ItemID == "" {
			skippedCount++
			if multiIssueMode {
				fmt.Fprintln(w, "skipped (not in project)")
			}
			continue
		}

		// Check if we have batch results for this item
		itemRes, hasBatchResults := itemResults[info.ItemID]

		if hasBatchResults {
			// Process batch results
			updateFailed := false
			for _, res := range itemRes {
				if !res.Success {
					fmt.Fprintf(os.Stderr, "Warning: failed to set %s for #%d: %s\n", res.FieldName, info.Number, res.Error)
					updateFailed = true
				}
			}

			if updateFailed {
				errorCount++
				hasErrors = true
				if multiIssueMode {
					fmt.Fprintln(w, "failed")
				}
				continue
			}
		} else if len(allUpdates) > 0 {
			// Fallback: execute updates sequentially for this issue
			updateFailed := false

			if statusValue != "" {
				if err := api.WithRetry(func() error {
					return client.SetProjectItemFieldWithFields(project.ID, info.ItemID, "Status", statusValue, projectFields)
				}, 3); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to set status for #%d: %v\n", info.Number, err)
					updateFailed = true
				}
			}

			if priorityValue != "" && !updateFailed {
				if err := api.WithRetry(func() error {
					return client.SetProjectItemFieldWithFields(project.ID, info.ItemID, "Priority", priorityValue, projectFields)
				}, 3); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to set priority for #%d: %v\n", info.Number, err)
					updateFailed = true
				}
			}

			if releaseValue != "" && !updateFailed {
				if err := api.WithRetry(func() error {
					return client.SetProjectItemFieldWithFields(project.ID, info.ItemID, branchFieldName, releaseValue, projectFields)
				}, 3); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to set branch for #%d: %v\n", info.Number, err)
					updateFailed = true
				}
			}

			if clearRelease && !updateFailed {
				if err := api.WithRetry(func() error {
					return client.SetProjectItemFieldWithFields(project.ID, info.ItemID, branchFieldName, "", projectFields)
				}, 3); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to clear branch for #%d: %v\n", info.Number, err)
					updateFailed = true
				}
			}

			if updateFailed {
				errorCount++
				hasErrors = true
				if multiIssueMode {
					fmt.Fprintln(w, "failed")
				}
				continue
			}
		}

		// Manage 'assigned' label based on branch field changes
		if info.IssueID != "" {
			if releaseValue != "" {
				// Adding to a branch — add 'assigned' label
				if err := client.AddLabelToIssue(info.Owner, info.Repo, info.IssueID, "assigned"); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to add 'assigned' label to #%d: %v\n", info.Number, err)
				}
			} else if clearRelease && (info.State == "OPEN" || info.State == "open" || info.State == "") {
				// Returning to backlog — remove 'assigned' label (only for open issues)
				if err := client.RemoveLabelFromIssue(info.Owner, info.Repo, info.IssueID, "assigned"); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to remove 'assigned' label from #%d: %v\n", info.Number, err)
				}
			}
		}

		updatedCount++
		if multiIssueMode {
			fmt.Fprintln(w, "done")
		} else {
			fmt.Fprintf(w, "Updated issue #%d: %s\n", info.Number, info.Title)
			for _, desc := range changeDescriptions {
				fmt.Fprintf(w, "  * %s\n", desc)
			}
			fmt.Fprintf(w, "https://github.com/%s/%s/issues/%d\n", info.Owner, info.Repo, info.Number)
		}
	}

	if multiIssueMode {
		fmt.Fprintf(w, "\nSummary: %d updated, %d skipped, %d failed\n", updatedCount, skippedCount, errorCount)
	}

	// IDPF: Warn about potential workflow rule violations after --force bypass
	if cfg.IsIDPF() && len(forceWarnings) > 0 && updatedCount > 0 {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), "WARNING: Workflow rules may have been violated.")
	}

	if hasErrors {
		return fmt.Errorf("some issues could not be updated")
	}

	return collectFailures.err(fmt.Sprintf("updated %d issue(s)", updatedCount))
}

// subIssueCollector holds shared state for recursive sub-issue collection.
type subIssueCollector struct {
	client        moveClient
	itemIDMap     map[string]string
	itemFieldsMap map[string][]api.FieldValue
	itemDataMap   map[string]*api.Issue
	// unlisted names each parent whose sub-issues could not be fetched, so the
	// subtree's absence is reported rather than read as "no sub-issues" (#938).
	unlisted []string
	firstErr error
}

// parentInfo tracks a parent issue for level-by-level traversal.
type parentInfo struct {
	owner  string
	repo   string
	number int
	depth  int
}

func collectSubIssuesRecursive(client moveClient, owner, repo string, number int, itemIDMap map[string]string, itemFieldsMap map[string][]api.FieldValue, itemDataMap map[string]*api.Issue, currentDepth, maxDepth int) ([]issueInfo, error) {
	if currentDepth > maxDepth {
		return nil, nil
	}

	c := &subIssueCollector{
		client:        client,
		itemIDMap:     itemIDMap,
		itemFieldsMap: itemFieldsMap,
		itemDataMap:   itemDataMap,
	}

	currentLevel := []parentInfo{{owner: owner, repo: repo, number: number, depth: currentDepth}}
	var result []issueInfo

	for len(currentLevel) > 0 && currentLevel[0].depth <= maxDepth {
		repoParents := groupByRepo(currentLevel)
		var nextLevel []parentInfo

		for repoKey, parents := range repoParents {
			parts := strings.SplitN(repoKey, "/", 2)
			if len(parts) != 2 {
				continue
			}

			subIssuesMap := c.fetchSubIssues(parts[0], parts[1], parents)

			for _, p := range parents {
				infos, children := c.processParentSubIssues(p, subIssuesMap[p.number], maxDepth)
				result = append(result, infos...)
				nextLevel = append(nextLevel, children...)
			}
		}

		currentLevel = nextLevel
	}

	// The sub-issues that were listed are still returned with the error.
	if len(c.unlisted) > 0 {
		return result, fmt.Errorf("could not list sub-issues of %s: %w", strings.Join(c.unlisted, ", "), c.firstErr)
	}
	return result, nil
}

// groupByRepo groups parents by "owner/repo" key for batch fetching.
func groupByRepo(parents []parentInfo) map[string][]parentInfo {
	grouped := make(map[string][]parentInfo)
	for _, p := range parents {
		key := p.owner + "/" + p.repo
		grouped[key] = append(grouped[key], p)
	}
	return grouped
}

// fetchSubIssues batch-fetches sub-issues for a repository, falling back to individual calls on error.
func (c *subIssueCollector) fetchSubIssues(repoOwner, repoName string, parents []parentInfo) map[int][]api.SubIssue {
	numbers := make([]int, len(parents))
	for i, p := range parents {
		numbers[i] = p.number
	}

	subIssuesMap, err := c.client.GetSubIssuesBatch(repoOwner, repoName, numbers)
	if err != nil {
		if subIssuesMap == nil {
			subIssuesMap = make(map[int][]api.SubIssue)
		}
		for _, p := range parents {
			subIssues, ferr := c.client.GetSubIssues(p.owner, p.repo, p.number)
			if ferr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get sub-issues for #%d: %v\n", p.number, ferr)
				c.unlisted = append(c.unlisted, fmt.Sprintf("#%d", p.number))
				if c.firstErr == nil {
					c.firstErr = ferr
				}
				continue
			}
			subIssuesMap[p.number] = subIssues
		}
	}
	return subIssuesMap
}

// processParentSubIssues builds issueInfo entries for a parent's sub-issues and returns next-level parents.
func (c *subIssueCollector) processParentSubIssues(parent parentInfo, subIssues []api.SubIssue, maxDepth int) ([]issueInfo, []parentInfo) {
	var infos []issueInfo
	var nextLevel []parentInfo
	childDepth := parent.depth + 1

	for _, sub := range subIssues {
		subOwner := sub.Repository.Owner
		subRepo := sub.Repository.Name
		if subOwner == "" {
			subOwner = parent.owner
		}
		if subRepo == "" {
			subRepo = parent.repo
		}

		key := fmt.Sprintf("%s/%s#%d", subOwner, subRepo, sub.Number)
		itemID := c.itemIDMap[key]

		var body, issueID, state string
		if issueData, ok := c.itemDataMap[key]; ok {
			body = issueData.Body
			issueID = issueData.ID
			state = issueData.State
		} else if issue, gerr := c.client.GetIssue(subOwner, subRepo, sub.Number); gerr == nil {
			body = issue.Body
			issueID = issue.ID
			state = issue.State
		}

		infos = append(infos, issueInfo{
			Owner:       subOwner,
			Repo:        subRepo,
			Number:      sub.Number,
			Title:       sub.Title,
			Body:        body,
			IssueID:     issueID,
			State:       state,
			ItemID:      itemID,
			FieldValues: c.itemFieldsMap[key],
			Depth:       parent.depth,
		})

		if childDepth <= maxDepth {
			nextLevel = append(nextLevel, parentInfo{
				owner:  subOwner,
				repo:   subRepo,
				number: sub.Number,
				depth:  childDepth,
			})
		}
	}

	return infos, nextLevel
}

// findActiveBranchForMove finds the active branch tracker from a list of issues
// Returns the first open branch issue found (there should only be one active at a time)
// Supports both "Branch: " (new) and "Release: " (legacy) prefixes for backwards compatibility
func findActiveBranchForMove(issues []api.Issue) *api.Issue {
	for i := range issues {
		if strings.HasPrefix(issues[i].Title, "Branch: ") || strings.HasPrefix(issues[i].Title, "Release: ") {
			return &issues[i]
		}
	}
	return nil
}
