package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/spf13/cobra"
)

// parseOwnerRepo extracts owner and repo from the first configured repository
func parseOwnerRepo(cfg *config.Config) (string, string, error) {
	if len(cfg.Repositories) == 0 {
		return "", "", fmt.Errorf("no repositories configured")
	}
	parts := strings.SplitN(cfg.Repositories[0], "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repository format: %s", cfg.Repositories[0])
	}
	return parts[0], parts[1], nil
}

// semverRegex matches valid semver versions with optional v prefix
var semverRegex = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// validateVersion validates that a version string is valid semver format
// Accepts X.Y.Z or vX.Y.Z format
func validateVersion(version string) error {
	if !semverRegex.MatchString(version) {
		return fmt.Errorf("Invalid version format. Use semver: X.Y.Z")
	}
	return nil
}

// compareVersions compares two semver versions
// Returns: positive if v1 > v2, negative if v1 < v2, zero if equal
func compareVersions(v1, v2 string) int {
	// Strip 'v' prefix
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	for i := 0; i < 3; i++ {
		var n1, n2 int
		if i < len(parts1) {
			_, _ = fmt.Sscanf(parts1[i], "%d", &n1)
		}
		if i < len(parts2) {
			_, _ = fmt.Sscanf(parts2[i], "%d", &n2)
		}
		if n1 != n2 {
			return n1 - n2
		}
	}
	return 0
}

// nextVersions contains calculated next version options
type nextVersions struct {
	patch string
	minor string
	major string
}

// calculateNextVersions computes the next patch, minor, and major versions
func calculateNextVersions(currentVersion string) (*nextVersions, error) {
	// Strip 'v' prefix for parsing
	version := strings.TrimPrefix(currentVersion, "v")
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid version format: %s", currentVersion)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid major version: %s", parts[0])
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid minor version: %s", parts[1])
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid patch version: %s", parts[2])
	}

	return &nextVersions{
		patch: fmt.Sprintf("v%d.%d.%d", major, minor, patch+1),
		minor: fmt.Sprintf("v%d.%d.0", major, minor+1),
		major: fmt.Sprintf("v%d.0.0", major+1),
	}, nil
}

// branchClient defines the interface for branch operations
// This allows mocking in tests
type branchClient interface {
	// CreateIssue creates a new issue in the repository
	CreateIssue(owner, repo, title, body string, labels []string) (*api.Issue, error)
	// GetOpenIssuesByLabel returns open issues with a specific label
	GetOpenIssuesByLabel(owner, repo, label string) ([]api.Issue, error)
	// GetOpenIssuesByLabels returns open issues matching ALL specified labels
	GetOpenIssuesByLabels(owner, repo string, labels []string) ([]api.Issue, error)
	// GetClosedIssuesByLabel returns closed issues with a specific label
	GetClosedIssuesByLabel(owner, repo, label string) ([]api.Issue, error)
	// AddIssueToProject adds an issue to a project and returns the item ID
	AddIssueToProject(projectID, issueID string) (string, error)
	// SetProjectItemField sets a field value on a project item
	SetProjectItemField(projectID, itemID, fieldID, value string) error
	// GetProject returns project details
	GetProject(owner string, number int) (*api.Project, error)
	// GetIssueByNumber returns an issue by its number
	GetIssueByNumber(owner, repo string, number int) (*api.Issue, error)
	// GetProjectItemID returns the project item ID for an issue
	GetProjectItemID(projectID, issueID string) (string, error)
	// GetProjectItemFieldValue returns the current value of a field on a project
	// item; the bool reports whether the field was present (empty is a value).
	GetProjectItemFieldValue(projectID, itemID, fieldID string) (string, bool, error)
	// GetProjectItems returns all items in a project with their field values
	GetProjectItems(projectID string, filter *api.ProjectItemsFilter) ([]api.ProjectItem, error)
	// GetProjectItemsMinimal returns project items with minimal issue data for filtering
	GetProjectItemsMinimal(projectID string, filter *api.ProjectItemsFilter) ([]api.MinimalProjectItem, error)
	// GetProjectItemsByIssues returns full project item details for specific issues
	GetProjectItemsByIssues(projectID string, refs []api.IssueRef) ([]api.ProjectItem, error)
	// UpdateIssueBody updates an issue's body
	UpdateIssueBody(issueID, body string) error
	// GetSubIssues returns sub-issues for a given issue
	GetSubIssues(owner, repo string, number int) ([]api.SubIssue, error)
	// WriteFile writes content to a file path
	WriteFile(path, content string) error
	// MkdirAll creates a directory and all parents
	MkdirAll(path string) error
	// GitAdd stages files to git
	GitAdd(paths ...string) error
	// CloseIssue closes an issue
	CloseIssue(issueID string, stateReason string) error
	// ReopenIssue reopens a closed issue
	ReopenIssue(issueID string) error
	// GitTag creates an annotated git tag
	GitTag(tag, message string) error
	// GitCheckoutNewBranch creates and checks out a new git branch
	GitCheckoutNewBranch(branch string) error
	// AddLabelToIssue adds a label to an issue, creating it if needed
	AddLabelToIssue(owner, repo, issueID, labelName string) error
	// RemoveLabelFromIssue removes a label from an issue
	RemoveLabelFromIssue(owner, repo, issueID, labelName string) error
	// AddSubIssue links a child issue as a sub-issue of a parent (tracker) issue
	AddSubIssue(parentIssueID, childIssueID string) error
	// RemoveSubIssue unlinks a child issue from its parent (tracker) issue
	RemoveSubIssue(parentIssueID, childIssueID string) error
}

// branchStartOptions holds the options for the branch start command
type branchStartOptions struct {
	branchName string
	dryRun     bool
}

// branchAddOptions holds the options for the branch add command
type branchAddOptions struct {
	issueNumber int
}

// branchRemoveOptions holds the options for the branch remove command
type branchRemoveOptions struct {
	issueNumber int
}

// branchCurrentOptions holds the options for the branch current command
type branchCurrentOptions struct {
	jsonFlag string // empty=text output, "*"=full JSON, "field,field"=selected fields
	jsonSet  bool   // whether --json was given a field list
}

// branchCloseOptions holds the options for the branch close command
type branchCloseOptions struct {
	tag        bool
	yes        bool
	dryRun     bool
	branchName string
}

// branchListOptions holds the options for the branch list command
type branchListOptions struct{}

// newBranchCommand creates the branch command group
func newBranchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "branch",
		Short: "Manage tracked branches",
		Long:  `Branch commands for managing release, patch, and feature branches.`,
	}

	cmd.AddCommand(newBranchStartCommand())
	cmd.AddCommand(newBranchAddCommand())
	cmd.AddCommand(newBranchRemoveCommand())
	cmd.AddCommand(newBranchCurrentCommand())
	cmd.AddCommand(newBranchCloseCommand())
	cmd.AddCommand(newBranchReopenCommand())
	cmd.AddCommand(newBranchListCommand())

	return cmd
}

// newBranchStartCommand creates the branch start subcommand
func newBranchStartCommand() *cobra.Command {
	opts := &branchStartOptions{}

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start tracking a new branch",
		Long: `Creates a tracker issue for a new branch and creates the git branch.

The --name flag is required and specifies the branch name to create.
The branch name is used literally for the tracker title, Branch field,
and artifact directory.

Examples:
  gh pmu branch start --name release/v2.0.0
  gh pmu branch start --name patch/v1.9.1
  gh pmu branch start --name hotfix-auth-bypass`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}
			cfg, err := config.LoadFromDirectory(cwd)
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}

			client, err := api.NewClient()
			if err != nil {
				return err
			}
			return runBranchStartWithDeps(cmd, opts, cfg, client)
		},
	}

	cmd.Flags().StringVar(&opts.branchName, "name", "", "Branch name to track (required)")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Check for an active branch, resolve the project and print what would be created, without creating the git branch or tracker")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

// newBranchAddCommand creates the release add subcommand
func newBranchAddCommand() *cobra.Command {
	opts := &branchAddOptions{}

	cmd := &cobra.Command{
		Use:   "add <issue-number>",
		Short: "Add an issue to the current branch",
		Long:  `Assigns an issue to the active branch by setting its Branch field.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var issueNum int
			if _, err := fmt.Sscanf(args[0], "%d", &issueNum); err != nil {
				return fmt.Errorf("invalid issue number: %s", args[0])
			}
			opts.issueNumber = issueNum

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}
			cfg, err := config.LoadFromDirectory(cwd)
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}
			client, err := api.NewClient()
			if err != nil {
				return err
			}
			return runBranchAddWithDeps(cmd, opts, cfg, client)
		},
	}

	return cmd
}

// newBranchRemoveCommand creates the release remove subcommand
func newBranchRemoveCommand() *cobra.Command {
	opts := &branchRemoveOptions{}

	cmd := &cobra.Command{
		Use:   "remove <issue-number>",
		Short: "Remove an issue from the current branch",
		Long:  `Clears the Branch field from an issue.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var issueNum int
			if _, err := fmt.Sscanf(args[0], "%d", &issueNum); err != nil {
				return fmt.Errorf("invalid issue number: %s", args[0])
			}
			opts.issueNumber = issueNum

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}
			cfg, err := config.LoadFromDirectory(cwd)
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}
			client, err := api.NewClient()
			if err != nil {
				return err
			}
			return runBranchRemoveWithDeps(cmd, opts, cfg, client)
		},
	}

	return cmd
}

// newBranchCurrentCommand creates the release current subcommand
func newBranchCurrentCommand() *cobra.Command {
	opts := &branchCurrentOptions{}

	cmd := &cobra.Command{
		Use:   "current",
		Short: "Show the active branch",
		Long:  `Displays details about the currently active branch.`,
		Example: `  # Show the active branch, its tracker and issue count
  gh pmu branch current

  # Tracker issue number only
  gh pmu branch current --json=tracker

  # Branch name and its issues as JSON
  gh pmu branch current --json=name,issues`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateBranchCurrentJSONFlag(cmd, opts)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// --json-fields needs no config or API access
			if listFields, _ := cmd.Flags().GetBool("json-fields"); listFields {
				return listAvailableFields(cmd, branchCurrentJSONFields)
			}
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}
			cfg, err := config.LoadFromDirectory(cwd)
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}
			client, err := api.NewClient()
			if err != nil {
				return err
			}
			opts.jsonSet = opts.jsonFlag != ""
			return runBranchCurrentWithDeps(cmd, opts, cfg, client)
		},
	}

	cmd.Flags().StringVar(&opts.jsonFlag, "json", "", "Output JSON with specified fields (comma-separated, e.g. tracker,issues). Use --json-fields to list available fields")
	cmd.Flags().Bool("json-fields", false, "List available JSON fields")

	return cmd
}

// branchCurrentJSONFields lists the fields `branch current --json` accepts
var branchCurrentJSONFields = []string{"issues", "name", "tracker"}

// validateBranchCurrentJSONFlag checks --json before any config load or API
// call: the value is required and every field must be known, so a typo exits
// non-zero rather than being silently dropped from the output (#926).
func validateBranchCurrentJSONFlag(cmd *cobra.Command, opts *branchCurrentOptions) error {
	if err := requireJSONValue(cmd); err != nil {
		return err
	}
	if opts.jsonFlag == "" {
		return nil
	}
	for _, f := range strings.Split(opts.jsonFlag, ",") {
		field := strings.TrimSpace(f)
		if field == "" {
			continue
		}
		known := false
		for _, available := range branchCurrentJSONFields {
			if field == available {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("unknown field %q in --json=%s. Run 'gh pmu branch current --json-fields' to see available fields", field, opts.jsonFlag)
		}
	}
	return nil
}

// newBranchCloseCommand creates the release close subcommand
func newBranchCloseCommand() *cobra.Command {
	opts := &branchCloseOptions{}

	cmd := &cobra.Command{
		Use:   "close [branch-name]",
		Short: "Close a branch",
		Long: `Closes a branch and optionally creates a git tag.

If no branch name is specified and exactly one branch is active, that branch
will be used. If multiple branches are active, you must specify which one to close.

Incomplete issues will be moved to backlog with Branch field cleared.
Release artifacts should be created beforehand using /prepare-release.

Examples:
  gh pmu branch close                    # Uses current branch if only one exists
  gh pmu branch close release/v2.0.0
  gh pmu branch close patch/v1.9.1 --tag
  gh pmu branch close --yes`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}
			cfg, err := config.LoadFromDirectory(cwd)
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}

			// If release name provided, use it
			if len(args) == 1 {
				opts.branchName = args[0]
			} else {
				// No argument provided - resolve from active releases
				client, err := api.NewClient()
				if err != nil {
					return err
				}
				releaseName, err := resolveCurrentBranch(cfg, client)
				if err != nil {
					return err
				}
				opts.branchName = releaseName
			}

			client, err := api.NewClient()
			if err != nil {
				return err
			}
			return runBranchCloseWithDeps(cmd, opts, cfg, client)
		},
	}

	cmd.Flags().BoolVar(&opts.tag, "tag", false, "Create a git tag for the release")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Preview what would happen without making changes")

	return cmd
}

// newBranchListCommand creates the release list subcommand
func newBranchListCommand() *cobra.Command {
	opts := &branchListOptions{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all branches",
		Long:  `Displays a table of all branches sorted by version.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}
			cfg, err := config.LoadFromDirectory(cwd)
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}
			client, err := api.NewClient()
			if err != nil {
				return err
			}
			return runBranchListWithDeps(cmd, opts, cfg, client)
		},
	}

	return cmd
}

// runBranchStartWithDeps is the testable entry point for branch start
// It receives all dependencies as parameters for easy mocking in tests
func runBranchStartWithDeps(cmd *cobra.Command, opts *branchStartOptions, cfg *config.Config, client branchClient) error {
	owner, repo, err := parseOwnerRepo(cfg)
	if err != nil {
		return err
	}

	// Check for existing active branch tracker
	existingIssues, err := client.GetOpenIssuesByLabel(owner, repo, "branch")
	if err != nil {
		return fmt.Errorf("failed to get existing branches: %w", err)
	}

	// Find any active branch tracker
	activeBranch := findActiveBranch(existingIssues)
	if activeBranch != nil {
		return fmt.Errorf("active branch exists: %s", activeBranch.Title)
	}

	if opts.dryRun {
		return printBranchStartDryRun(cmd, opts, cfg, client, owner, repo)
	}

	// Create the git branch
	err = client.GitCheckoutNewBranch(opts.branchName)
	if err != nil {
		return fmt.Errorf("failed to create branch: %w", err)
	}

	// Use branch name for tracker title and Release field
	title := fmt.Sprintf("Branch: %s", opts.branchName)
	body := generateBranchTrackerTemplate(opts.branchName)

	// Create tracker issue with branch label. From here on the git branch already
	// exists, so failures leave partial state — surface recovery guidance naming
	// the branch rather than a bare error.
	labels := []string{"branch"}
	issue, err := client.CreateIssue(owner, repo, title, body, labels)
	if err != nil {
		return fmt.Errorf("failed to create tracker issue: %w\n"+
			"Partial state: git branch %q was created but has no tracker. "+
			"Delete it with 'git branch -D %s' and retry, or create the tracker manually.",
			err, opts.branchName, opts.branchName)
	}

	// Get project
	project, err := client.GetProject(cfg.Project.Owner, cfg.Project.Number)
	if err != nil {
		return fmt.Errorf("failed to get project: %w\n"+
			"Partial state: git branch %q and tracker issue #%d exist but project setup did not complete. "+
			"Fix the problem and re-run, or use 'gh pmu branch reopen %s'.",
			err, opts.branchName, issue.Number, opts.branchName)
	}

	// Add issue to project
	itemID, err := client.AddIssueToProject(project.ID, issue.ID)
	if err != nil {
		return fmt.Errorf("failed to add issue to project: %w\n"+
			"Partial state: git branch %q and tracker issue #%d exist but the tracker is not on the project board. "+
			"Add it manually or delete #%d and retry.",
			err, opts.branchName, issue.Number, issue.Number)
	}

	// Set status to In Progress
	statusField, ok := cfg.Fields["status"]
	if ok {
		statusValue := statusField.Values["in_progress"]
		if statusValue == "" {
			statusValue = "In progress"
		}
		err = client.SetProjectItemField(project.ID, itemID, statusField.Field, statusValue)
		if err != nil {
			return fmt.Errorf("failed to set status: %w\n"+
				"Partial state: git branch %q and tracker issue #%d exist and are on the board but the status was not set. "+
				"Set it manually with 'gh pmu move %d --status in_progress'.",
				err, opts.branchName, issue.Number, issue.Number)
		}
	}

	// Output confirmation
	fmt.Fprintf(cmd.OutOrStdout(), "Created branch: %s\n", opts.branchName)
	fmt.Fprintf(cmd.OutOrStdout(), "Started tracking: %s\n", title)
	fmt.Fprintf(cmd.OutOrStdout(), "Tracker issue: #%d\n", issue.Number)

	return nil
}

// printBranchStartDryRun resolves the project and reports the git branch,
// tracker issue and status that branch start would create, writing nothing
// (#929). The active-branch check has already run, as in a real start.
func printBranchStartDryRun(cmd *cobra.Command, opts *branchStartOptions, cfg *config.Config, client branchClient, owner, repo string) error {
	project, err := client.GetProject(cfg.Project.Owner, cfg.Project.Number)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}
	status := "(not set: no status field configured)"
	if statusField, ok := cfg.Fields["status"]; ok {
		status = statusField.Values["in_progress"]
		if status == "" {
			status = "In progress"
		}
	}

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "Dry run: no git branch or tracker issue will be created.")
	fmt.Fprintf(w, "  Git branch: %s\n", opts.branchName)
	fmt.Fprintf(w, "  Tracker:    Branch: %s (in %s/%s)\n", opts.branchName, owner, repo)
	fmt.Fprintln(w, "  Labels:     branch")
	fmt.Fprintf(w, "  Project:    %s (%s #%d)\n", project.Title, cfg.Project.Owner, cfg.Project.Number)
	fmt.Fprintf(w, "  Status:     %s\n", status)
	return nil
}

// isBranchTracker checks if an issue title matches the branch tracker format
// Supports both "Branch: " (new) and "Release: " (legacy) prefixes
func isBranchTracker(title string) bool {
	return strings.HasPrefix(title, "Branch: ") || strings.HasPrefix(title, "Release: ")
}

// findActiveBranch finds any active branch tracker from a list of issues
// Returns nil if no active branch is found
// Supports both "Branch: " and "Release: " (legacy) title formats
func findActiveBranch(issues []api.Issue) *api.Issue {
	for i := range issues {
		if isBranchTracker(issues[i].Title) {
			return &issues[i]
		}
	}
	return nil
}

// findAllActiveBranches finds all active branch trackers from a list of issues
// Supports both "Branch: " and "Release: " (legacy) title formats
func findAllActiveBranches(issues []api.Issue) []api.Issue {
	var branches []api.Issue
	for i := range issues {
		if isBranchTracker(issues[i].Title) {
			branches = append(branches, issues[i])
		}
	}
	return branches
}

// errMultipleActiveBranches builds the disambiguation error listing active branch
// names. Shared by add/remove/current/close so the message format stays consistent.
func errMultipleActiveBranches(branches []api.Issue) error {
	names := make([]string, 0, len(branches))
	for i := range branches {
		names = append(names, extractBranchVersion(branches[i].Title))
	}
	return fmt.Errorf("multiple active branches. Specify one: %s", strings.Join(names, ", "))
}

// resolveSingleActiveBranchTracker returns the sole active branch tracker from the
// given issues. It errors with "no active branch found" when none are active and
// with a disambiguation list when more than one is active. This prevents add/remove
// from silently mutating an arbitrary tracker when multiple branches are active.
func resolveSingleActiveBranchTracker(issues []api.Issue) (*api.Issue, error) {
	active := findAllActiveBranches(issues)
	switch len(active) {
	case 0:
		return nil, fmt.Errorf("no active branch found")
	case 1:
		return &active[0], nil
	default:
		return nil, errMultipleActiveBranches(active)
	}
}

// resolveCurrentBranch resolves the current branch name when no argument is provided
// Returns error if no branches or multiple branches are active
func resolveCurrentBranch(cfg *config.Config, client branchClient) (string, error) {
	owner, repo, err := parseOwnerRepo(cfg)
	if err != nil {
		return "", err
	}

	issues, err := client.GetOpenIssuesByLabel(owner, repo, "branch")
	if err != nil {
		return "", fmt.Errorf("failed to get branch issues: %w", err)
	}

	tracker, err := resolveSingleActiveBranchTracker(issues)
	if err != nil {
		return "", err
	}
	// Extract branch name from title (e.g., "Branch: patch/0.9.7" -> "patch/0.9.7")
	return extractBranchVersion(tracker.Title), nil
}

// runBranchAddWithDeps is the testable entry point for branch add
// It receives all dependencies as parameters for easy mocking in tests
func runBranchAddWithDeps(cmd *cobra.Command, opts *branchAddOptions, cfg *config.Config, client branchClient) error {
	owner, repo, err := parseOwnerRepo(cfg)
	if err != nil {
		return err
	}

	// Get open release issues
	issues, err := client.GetOpenIssuesByLabel(owner, repo, "branch")
	if err != nil {
		return fmt.Errorf("failed to get release issues: %w", err)
	}

	// Resolve the single active branch tracker. Errors with a disambiguation list
	// when multiple branches are active rather than silently picking an arbitrary one.
	activeRelease, err := resolveSingleActiveBranchTracker(issues)
	if err != nil {
		return err
	}

	// Extract version from title (e.g., "Release: v1.2.0" or "Release: v1.2.0 (Phoenix)" -> "v1.2.0")
	releaseVersion := extractBranchVersion(activeRelease.Title)

	// Get the issue to add
	issue, err := client.GetIssueByNumber(owner, repo, opts.issueNumber)
	if err != nil {
		return fmt.Errorf("failed to get issue #%d: %w", opts.issueNumber, err)
	}

	// Get project
	project, err := client.GetProject(cfg.Project.Owner, cfg.Project.Number)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	// Get project item ID for the issue
	itemID, err := client.GetProjectItemID(project.ID, issue.ID)
	if err != nil {
		return fmt.Errorf("failed to get project item for issue #%d: %w", opts.issueNumber, err)
	}

	// Set the Branch text field
	branchField, ok := cfg.Fields["branch"]
	if !ok {
		return fmt.Errorf("branch field not configured")
	}

	err = client.SetProjectItemField(project.ID, itemID, branchField.Field, releaseVersion)
	if err != nil {
		return fmt.Errorf("failed to set branch field: %w", err)
	}

	// Link the issue as a sub-issue of the tracker so `branch close` (which
	// enumerates via GetSubIssues) sees it. Keeps add/close membership symmetric —
	// without this, an added issue is orphaned at close with a stale Branch field.
	if err := client.AddSubIssue(activeRelease.ID, issue.ID); err != nil {
		return fmt.Errorf("failed to link issue #%d as a sub-issue of tracker #%d: %w",
			opts.issueNumber, activeRelease.Number, err)
	}

	// Output confirmation (AC-019-2)
	fmt.Fprintf(cmd.OutOrStdout(), "Added #%d to release %s\n", opts.issueNumber, releaseVersion)

	return nil
}

// extractBranchVersion extracts the version from a branch tracker title
// Supports both "Branch: " and "Release: " (legacy) prefixes
// e.g., "Branch: v1.2.0" -> "v1.2.0", "Release: v1.2.0 (Phoenix)" -> "v1.2.0"
func extractBranchVersion(title string) string {
	// Remove "Branch: " or "Release: " prefix
	version := strings.TrimPrefix(title, "Branch: ")
	version = strings.TrimPrefix(version, "Release: ")
	// If there's a codename in parentheses, remove it
	if idx := strings.Index(version, " ("); idx > 0 {
		version = version[:idx]
	}
	return version
}

// runBranchRemoveWithDeps is the testable entry point for release remove
// It receives all dependencies as parameters for easy mocking in tests
func runBranchRemoveWithDeps(cmd *cobra.Command, opts *branchRemoveOptions, cfg *config.Config, client branchClient) error {
	owner, repo, err := parseOwnerRepo(cfg)
	if err != nil {
		return err
	}

	// Get open release issues
	issues, err := client.GetOpenIssuesByLabel(owner, repo, "branch")
	if err != nil {
		return fmt.Errorf("failed to get release issues: %w", err)
	}

	// Resolve the single active branch tracker. Errors with a disambiguation list
	// when multiple branches are active rather than silently picking an arbitrary one.
	activeRelease, err := resolveSingleActiveBranchTracker(issues)
	if err != nil {
		return err
	}

	// Extract version from title
	releaseVersion := extractBranchVersion(activeRelease.Title)

	// Get the issue to remove
	issue, err := client.GetIssueByNumber(owner, repo, opts.issueNumber)
	if err != nil {
		return fmt.Errorf("failed to get issue #%d: %w", opts.issueNumber, err)
	}

	// Get project
	project, err := client.GetProject(cfg.Project.Owner, cfg.Project.Number)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	// Get project item ID for the issue
	itemID, err := client.GetProjectItemID(project.ID, issue.ID)
	if err != nil {
		return fmt.Errorf("failed to get project item for issue #%d: %w", opts.issueNumber, err)
	}

	// Get branch field config
	branchField, ok := cfg.Fields["branch"]
	if !ok {
		return fmt.Errorf("branch field not configured")
	}

	// Check current field value (AC-039-3)
	currentValue, _, err := client.GetProjectItemFieldValue(project.ID, itemID, branchField.Field)
	if err != nil {
		return fmt.Errorf("failed to get current branch field value: %w", err)
	}

	// If not assigned to a release, warn and return
	if currentValue == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Issue #%d is not assigned to a release\n", opts.issueNumber)
		return nil
	}

	// Clear the Branch text field (AC-039-1)
	err = client.SetProjectItemField(project.ID, itemID, branchField.Field, "")
	if err != nil {
		return fmt.Errorf("failed to clear branch field: %w", err)
	}

	// Unlink the sub-issue relationship established by `branch add`. Non-fatal: an
	// issue assigned by field only (legacy, or another tool) may not be a sub-issue.
	if err := client.RemoveSubIssue(activeRelease.ID, issue.ID); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to unlink #%d from tracker #%d: %v\n",
			opts.issueNumber, activeRelease.Number, err)
	}

	// Remove 'assigned' label if issue is open
	if issue.State == "OPEN" || issue.State == "open" {
		if err := client.RemoveLabelFromIssue(owner, repo, issue.ID, "assigned"); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to remove 'assigned' label from #%d: %v\n", opts.issueNumber, err)
		}
	}

	// Output confirmation (AC-039-2)
	fmt.Fprintf(cmd.OutOrStdout(), "Removed #%d from release %s\n", opts.issueNumber, releaseVersion)

	return nil
}

// branchCurrentIssueJSON represents a sub-issue in the JSON output
type branchCurrentIssueJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
}

// runBranchCurrentWithDeps is the testable entry point for release current
// It receives all dependencies as parameters for easy mocking in tests
func runBranchCurrentWithDeps(cmd *cobra.Command, opts *branchCurrentOptions, cfg *config.Config, client branchClient) error {
	owner, repo, err := parseOwnerRepo(cfg)
	if err != nil {
		return err
	}

	// Fast path: try active+branch label query (O(1) lookup). Filter to genuine
	// branch trackers by title — an issue can carry both labels without being one.
	var activeRelease *api.Issue
	issues, err := client.GetOpenIssuesByLabels(owner, repo, []string{"active", "branch"})
	if err != nil {
		return fmt.Errorf("failed to get branch issues: %w", err)
	}
	fastTrackers := findAllActiveBranches(issues)
	switch len(fastTrackers) {
	case 1:
		activeRelease = &fastTrackers[0]
	case 0:
		// fall through to the title-scan fallback
	default:
		return errMultipleActiveBranches(fastTrackers)
	}

	// Fallback: scan all branch-labeled issues by title pattern
	if activeRelease == nil {
		fallbackIssues, err := client.GetOpenIssuesByLabel(owner, repo, "branch")
		if err != nil {
			return fmt.Errorf("failed to get release issues: %w", err)
		}
		fallbackTrackers := findAllActiveBranches(fallbackIssues)
		switch len(fallbackTrackers) {
		case 1:
			activeRelease = &fallbackTrackers[0]
		case 0:
			// no active branch
		default:
			return errMultipleActiveBranches(fallbackTrackers)
		}
	}

	if activeRelease == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "No active release\n")
		return nil
	}

	// Extract version from title
	releaseVersion := extractBranchVersion(activeRelease.Title)
	issueCount := activeRelease.SubIssueCount

	// JSON output mode
	if opts.jsonSet {
		return branchCurrentOutputJSON(cmd, opts, owner, repo, activeRelease, releaseVersion, issueCount, client)
	}

	// Default text output (unchanged format)
	fmt.Fprintf(cmd.OutOrStdout(), "Current Branch: %s\n", releaseVersion)
	fmt.Fprintf(cmd.OutOrStdout(), "Tracker: #%d\n", activeRelease.Number)
	fmt.Fprintf(cmd.OutOrStdout(), "Issues: %d\n", issueCount)

	return nil
}

// branchCurrentOutputJSON handles --json output for branch current
func branchCurrentOutputJSON(cmd *cobra.Command, opts *branchCurrentOptions, owner, repo string, tracker *api.Issue, branchName string, issueCount int, client branchClient) error {
	fields := opts.jsonFlag
	wantAll := fields == "*"

	// Parse requested fields
	wantName := wantAll
	wantTracker := wantAll
	wantIssues := wantAll
	if !wantAll {
		for _, f := range strings.Split(fields, ",") {
			switch strings.TrimSpace(f) {
			case "name":
				wantName = true
			case "tracker":
				wantTracker = true
			case "issues":
				wantIssues = true
			}
		}
	}

	// Build output using a map for field selection
	output := make(map[string]interface{})
	if wantName {
		output["name"] = branchName
	}
	if wantTracker {
		output["tracker"] = tracker.Number
	}
	if wantIssues {
		// Fetch sub-issues for detailed issue list
		subIssues, err := client.GetSubIssues(owner, repo, tracker.Number)
		if err != nil {
			return fmt.Errorf("failed to get sub-issues: %w", err)
		}
		var issueList []branchCurrentIssueJSON
		for _, si := range subIssues {
			state := "open"
			if si.State == "CLOSED" || si.State == "closed" {
				state = "done"
			}
			issueList = append(issueList, branchCurrentIssueJSON{
				Number: si.Number,
				Title:  si.Title,
				State:  state,
			})
		}
		if issueList == nil {
			issueList = []branchCurrentIssueJSON{}
		}
		output["issues"] = issueList
	}

	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

// generateBranchTrackerTemplate generates the initial body template for a branch tracker issue
func generateBranchTrackerTemplate(branchName string) string {
	return fmt.Sprintf(`> **Branch Tracker Issue**
>
> This issue tracks the branch %s. It is managed by gh pmu branch commands.
>
> **Do not manually:**
> - Close or reopen this issue
> - Change the title
> - Remove the %s label

## Commands

- %s - Add issues to this branch
- %s - Remove issues from this branch
- %s - Close this branch

## Issues in this branch

_Issues are tracked via the Branch field in the project._
`,
		"`"+branchName+"`",
		"`branch`",
		"`gh pmu branch add <issue>`",
		"`gh pmu branch remove <issue>`",
		"`gh pmu branch close "+branchName+"`",
	)
}

// runBranchCloseWithDeps is the testable entry point for release close
// It receives all dependencies as parameters for easy mocking in tests
func runBranchCloseWithDeps(cmd *cobra.Command, opts *branchCloseOptions, cfg *config.Config, client branchClient) error {
	owner, repo, err := parseOwnerRepo(cfg)
	if err != nil {
		return err
	}
	// The close carries on past per-issue failures, then reports them (#938).
	var closeFailures partialFailures
	movedCount := 0

	// Get open release issues
	issues, err := client.GetOpenIssuesByLabel(owner, repo, "branch")
	if err != nil {
		return fmt.Errorf("failed to get release issues: %w", err)
	}

	// Find the specified branch by name (supports both "Branch: " and "Release: " formats)
	var targetBranch *api.Issue
	expectedTitleNew := fmt.Sprintf("Branch: %s", opts.branchName)
	expectedTitleLegacy := fmt.Sprintf("Release: %s", opts.branchName)
	for i := range issues {
		title := issues[i].Title
		if title == expectedTitleNew || strings.HasPrefix(title, expectedTitleNew+" (") ||
			title == expectedTitleLegacy || strings.HasPrefix(title, expectedTitleLegacy+" (") {
			targetBranch = &issues[i]
			break
		}
	}
	if targetBranch == nil {
		return fmt.Errorf("branch not found: %s", opts.branchName)
	}

	// Extract version from title
	releaseVersion := extractBranchVersion(targetBranch.Title)

	// Get branch issues from tracker sub-issues (no project scan needed)
	subIssues, err := client.GetSubIssues(owner, repo, targetBranch.Number)
	if err != nil {
		return fmt.Errorf("failed to get branch issues: %w", err)
	}

	// Convert sub-issues to Issue structs and separate done vs incomplete
	var doneIssues, incompleteIssues []api.Issue
	for _, si := range subIssues {
		issue := api.Issue{
			ID:         si.ID,
			Number:     si.Number,
			Title:      si.Title,
			State:      si.State,
			URL:        si.URL,
			Repository: si.Repository,
		}
		if si.State == "CLOSED" || si.State == "closed" {
			doneIssues = append(doneIssues, issue)
		} else {
			incompleteIssues = append(incompleteIssues, issue)
		}
	}
	releaseIssues := append(doneIssues, incompleteIssues...)

	// Show branch summary
	fmt.Fprintf(cmd.OutOrStdout(), "Closing branch: %s\n", opts.branchName)
	fmt.Fprintf(cmd.OutOrStdout(), "  Tracker issue: #%d\n", targetBranch.Number)
	fmt.Fprintf(cmd.OutOrStdout(), "  Issues in release: %d (%d done, %d incomplete)\n",
		len(releaseIssues), len(doneIssues), len(incompleteIssues))
	fmt.Fprintln(cmd.OutOrStdout())

	// Lazy-load project only when needed for field operations on incomplete issues
	var project *api.Project
	getProject := func() (*api.Project, error) {
		if project == nil {
			var pErr error
			project, pErr = client.GetProject(cfg.Project.Owner, cfg.Project.Number)
			if pErr != nil {
				return nil, fmt.Errorf("failed to get project: %w", pErr)
			}
		}
		return project, nil
	}

	// Separate incomplete issues into parking lot and to-move categories
	var parkingLotIssues, issuesToMove []api.Issue
	statusFieldName := "Status"
	if statusField, ok := cfg.Fields["status"]; ok && statusField.Field != "" {
		statusFieldName = statusField.Field
	}
	parkingLotValue := "Parking Lot"
	if statusField, ok := cfg.Fields["status"]; ok {
		if val, exists := statusField.Values["parking_lot"]; exists {
			parkingLotValue = val
		}
	}

	for _, issue := range incompleteIssues {
		proj, pErr := getProject()
		if pErr != nil {
			return pErr
		}
		itemID, err := client.GetProjectItemID(proj.ID, issue.ID)
		if err != nil {
			// Can't determine status, include in move list
			issuesToMove = append(issuesToMove, issue)
			continue
		}

		status, _, sErr := client.GetProjectItemFieldValue(proj.ID, itemID, statusFieldName)
		if sErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "  Warning: could not read status for #%d (treating as movable): %v\n", issue.Number, sErr)
		}
		if status == parkingLotValue {
			parkingLotIssues = append(parkingLotIssues, issue)
		} else {
			issuesToMove = append(issuesToMove, issue)
		}
	}

	// Dry-run mode: show preview and exit
	if opts.dryRun {
		fmt.Fprintln(cmd.OutOrStdout(), "[DRY RUN] Preview of changes:")
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintf(cmd.OutOrStdout(), "Would close branch: %s\n", opts.branchName)
		if len(issuesToMove) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "Would move %d incomplete issue(s) to backlog:\n", len(issuesToMove))
			for _, issue := range issuesToMove {
				fmt.Fprintf(cmd.OutOrStdout(), "  #%d - %s\n", issue.Number, issue.Title)
			}
		}
		if len(parkingLotIssues) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "Would skip %d Parking Lot issue(s)\n", len(parkingLotIssues))
		}
		if opts.tag {
			fmt.Fprintf(cmd.OutOrStdout(), "Would create git tag: %s\n", releaseVersion)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Would close tracker issue #%d\n", targetBranch.Number)
		return nil
	}

	// Warn about incomplete issues and confirm
	if len(incompleteIssues) > 0 {
		if len(issuesToMove) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "⚠️  %d issue(s) are not done. They will be moved to backlog.\n", len(issuesToMove))
		}
		if len(parkingLotIssues) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "ℹ️  Skipping %d Parking Lot issue(s).\n", len(parkingLotIssues))
		}

		if !opts.yes {
			return fmt.Errorf("use --yes to confirm branch close")
		}
		fmt.Fprintln(cmd.OutOrStdout())

		// Move non-parking-lot incomplete issues to backlog and clear Branch field
		if len(issuesToMove) > 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "Moving incomplete issues to backlog...")

			proj, pErr := getProject()
			if pErr != nil {
				return pErr
			}

			for _, issue := range issuesToMove {
				// Get project item ID
				itemID, err := client.GetProjectItemID(proj.ID, issue.ID)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "  Warning: could not find project item for #%d: %v\n", issue.Number, err)
					closeFailures.add(fmt.Sprintf("move #%d to backlog", issue.Number), err)
					continue
				}
				moved := true

				// Clear Branch field
				if branchField, ok := cfg.Fields["branch"]; ok {
					if err := client.SetProjectItemField(proj.ID, itemID, branchField.Field, ""); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "  Warning: failed to clear branch field for #%d: %v\n", issue.Number, err)
						closeFailures.add(fmt.Sprintf("clear branch field for #%d", issue.Number), err)
						moved = false
					}
				}

				// Set status to backlog
				if statusField, ok := cfg.Fields["status"]; ok {
					backlogValue := statusField.Values["backlog"]
					if backlogValue == "" {
						backlogValue = "Backlog"
					}
					if err := client.SetProjectItemField(proj.ID, itemID, statusField.Field, backlogValue); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "  Warning: failed to move #%d to backlog: %v\n", issue.Number, err)
						closeFailures.add(fmt.Sprintf("move #%d to backlog", issue.Number), err)
						moved = false
					}
				}

				if moved {
					movedCount++
					fmt.Fprintf(cmd.OutOrStdout(), "  #%d - %s\n", issue.Number, issue.Title)
				}
			}
			fmt.Fprintln(cmd.OutOrStdout())
		}
	} else if !opts.yes {
		return fmt.Errorf("use --yes to confirm branch close")
	}

	// Remove 'assigned' label from all open branch issues
	for _, issue := range incompleteIssues {
		if err := client.RemoveLabelFromIssue(owner, repo, issue.ID, "assigned"); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to remove 'assigned' label from #%d: %v\n", issue.Number, err)
			closeFailures.add(fmt.Sprintf("remove 'assigned' label from #%d", issue.Number), err)
		}
	}

	// Create git tag if requested
	if opts.tag {
		tagMessage := fmt.Sprintf("Release %s", releaseVersion)
		err = client.GitTag(releaseVersion, tagMessage)
		if err != nil {
			return fmt.Errorf("failed to create git tag: %w", err)
		}
	}

	// Close the tracker issue
	err = client.CloseIssue(targetBranch.ID, "")
	if err != nil {
		return fmt.Errorf("failed to close tracker issue: %w", err)
	}

	// Output confirmation
	fmt.Fprintf(cmd.OutOrStdout(), "✓ Branch closed: %s\n", releaseVersion)
	if len(issuesToMove) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %d issue(s) moved to backlog (Branch cleared)\n", movedCount)
	}
	if opts.tag {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Tag created: %s\n", releaseVersion)
	}

	return closeFailures.err(fmt.Sprintf("closed branch %s", releaseVersion))
}

// newBranchReopenCommand creates the release reopen subcommand
func newBranchReopenCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reopen <branch-name>",
		Short: "Reopen a closed branch",
		Long: `Reopens a previously closed branch tracker issue.

Use this to continue work on a branch after it has been closed.
The branch name must be specified explicitly.

Examples:
  gh pmu branch reopen release/v2.0.0
  gh pmu branch reopen patch/v1.9.1`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			branchName := args[0]

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

			client, err := api.NewClient()
			if err != nil {
				return err
			}
			return runBranchReopenWithDeps(cmd, branchName, cfg, client)
		},
	}

	return cmd
}

func runBranchReopenWithDeps(cmd *cobra.Command, branchName string, cfg *config.Config, client branchClient) error {
	owner, repo, err := parseOwnerRepo(cfg)
	if err != nil {
		return err
	}

	// Get closed branch issues
	issues, err := client.GetClosedIssuesByLabel(owner, repo, "branch")
	if err != nil {
		return fmt.Errorf("failed to get closed branch issues: %w", err)
	}

	// Find the specified branch by name (supports both "Branch: " and "Release: " formats)
	var targetBranch *api.Issue
	expectedTitleNew := fmt.Sprintf("Branch: %s", branchName)
	expectedTitleLegacy := fmt.Sprintf("Release: %s", branchName)
	for i := range issues {
		title := issues[i].Title
		if title == expectedTitleNew || strings.HasPrefix(title, expectedTitleNew+" (") ||
			title == expectedTitleLegacy || strings.HasPrefix(title, expectedTitleLegacy+" (") {
			targetBranch = &issues[i]
			break
		}
	}

	if targetBranch == nil {
		return fmt.Errorf("closed branch not found: %s", branchName)
	}

	// Reopen the tracker issue
	err = client.ReopenIssue(targetBranch.ID)
	if err != nil {
		return fmt.Errorf("failed to reopen tracker issue: %w", err)
	}

	branchVersion := extractBranchVersion(targetBranch.Title)
	fmt.Fprintf(cmd.OutOrStdout(), "Reopened branch %s (tracker #%d)\n", branchVersion, targetBranch.Number)

	return nil
}

// extractBranchCodename extracts the codename from a release title
// e.g., "Release: v1.2.0 (Phoenix)" -> "Phoenix", "Release: v1.2.0" -> ""
func extractBranchCodename(title string) string {
	start := strings.Index(title, "(")
	end := strings.Index(title, ")")
	if start > 0 && end > start {
		return title[start+1 : end]
	}
	return ""
}

// runBranchListWithDeps is the testable entry point for branch list
// It receives all dependencies as parameters for easy mocking in tests
func runBranchListWithDeps(cmd *cobra.Command, opts *branchListOptions, cfg *config.Config, client branchClient) error {
	var branches []branchInfo

	// Fetch from API
	owner, repo, err := parseOwnerRepo(cfg)
	if err != nil {
		return err
	}

	openIssues, err := client.GetOpenIssuesByLabel(owner, repo, "branch")
	if err != nil {
		return fmt.Errorf("failed to get open branches: %w", err)
	}

	closedIssues, err := client.GetClosedIssuesByLabel(owner, repo, "branch")
	if err != nil {
		return fmt.Errorf("failed to get closed branches: %w", err)
	}

	// Combine and filter for branch trackers (supports both "Branch: " and "Release: " formats)
	for _, issue := range openIssues {
		if isBranchTracker(issue.Title) {
			branches = append(branches, extractBranchInfo(issue, "Active"))
		}
	}
	for _, issue := range closedIssues {
		if isBranchTracker(issue.Title) {
			branches = append(branches, extractBranchInfo(issue, "Closed"))
		}
	}

	if len(branches) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No branches found\n")
		return nil
	}

	// Sort by version descending
	sortBranchesByVersionDesc(branches)

	// Display table
	fmt.Fprintf(cmd.OutOrStdout(), "%-12s %-15s %-10s %-10s\n", "VERSION", "CODENAME", "TRACKER", "STATUS")
	fmt.Fprintf(cmd.OutOrStdout(), "%-12s %-15s %-10s %-10s\n", "-------", "--------", "-------", "------")
	for _, b := range branches {
		codenameDisplay := b.codename
		if codenameDisplay == "" {
			codenameDisplay = "-"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%-12s %-15s #%-9d %-10s\n", b.version, codenameDisplay, b.trackerNum, b.status)
	}

	return nil
}

// branchInfo holds parsed release information
type branchInfo struct {
	version    string
	codename   string
	trackerNum int
	status     string
}

// extractBranchInfo extracts release information from an issue
func extractBranchInfo(issue api.Issue, status string) branchInfo {
	version := extractBranchVersion(issue.Title)
	codename := extractBranchCodename(issue.Title)
	return branchInfo{
		version:    version,
		codename:   codename,
		trackerNum: issue.Number,
		status:     status,
	}
}

// sortBranchesByVersionDesc sorts releases by version in descending order
func sortBranchesByVersionDesc(releases []branchInfo) {
	sort.Slice(releases, func(i, j int) bool {
		return compareVersions(releases[i].version, releases[j].version) > 0
	})
}

// branchActiveEntry represents an active branch for config storage
type branchActiveEntry struct {
	Version      string `yaml:"version"`
	TrackerIssue int    `yaml:"tracker_issue"`
	Started      string `yaml:"started"`
	Track        string `yaml:"track"`
}

// parseBranchTitle parses a branch tracker title into version and track
// Supports both "Branch: " and "Release: " (legacy) prefixes
// Examples:
//
//	"Branch: v1.2.0" -> version="1.2.0", track="stable"
//	"Release: v1.2.0 (Phoenix)" -> version="1.2.0", track="stable"
//	"Branch: patch/1.1.1" -> version="1.1.1", track="patch"
//	"Release: beta/2.0.0" -> version="2.0.0", track="beta"
func parseBranchTitle(title string) (version, track string) {
	// Remove "Branch: " or "Release: " prefix
	remainder := strings.TrimPrefix(title, "Branch: ")
	remainder = strings.TrimPrefix(remainder, "Release: ")

	// Remove codename suffix if present (e.g., " (Phoenix)")
	if idx := strings.Index(remainder, " ("); idx != -1 {
		remainder = remainder[:idx]
	}

	// Check for track prefix (e.g., "patch/", "beta/")
	if strings.Contains(remainder, "/") {
		parts := strings.SplitN(remainder, "/", 2)
		track = parts[0]
		version = strings.TrimPrefix(parts[1], "v")
	} else {
		// Default track is "stable", version starts with v
		track = "stable"
		version = strings.TrimPrefix(remainder, "v")
	}

	return version, track
}

// SyncActiveBranches queries open branch issues and returns active branch entries
func SyncActiveBranches(client branchClient, owner, repo string) ([]branchActiveEntry, error) {
	issues, err := client.GetOpenIssuesByLabel(owner, repo, "branch")
	if err != nil {
		return nil, fmt.Errorf("failed to get branch issues: %w", err)
	}

	var entries []branchActiveEntry
	for _, issue := range issues {
		if !isBranchTracker(issue.Title) {
			continue
		}

		version, track := parseBranchTitle(issue.Title)
		entries = append(entries, branchActiveEntry{
			Version:      version,
			TrackerIssue: issue.Number,
			Started:      "",
			Track:        track,
		})
	}

	return entries, nil
}
