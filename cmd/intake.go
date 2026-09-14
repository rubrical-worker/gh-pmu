package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/spf13/cobra"
)

// intakeClient defines the interface for API methods used by intake functions.
// This allows for easier testing with mock implementations.
type intakeClient interface {
	assigneeResolver
	GetProject(owner string, number int) (*api.Project, error)
	SearchIntakeCandidates(owner, repo string, labels []string, projectID string) ([]api.IntakeCandidate, error)
	GetProjectFields(projectID string) ([]api.ProjectField, error)
	BatchAddIssuesToProject(projectID string, issueIDs []string) ([]api.BatchAddResult, error)
	BatchUpdateProjectItemFields(projectID string, updates []api.FieldUpdate, fields []api.ProjectField) ([]api.BatchUpdateResult, error)
}

type intakeOptions struct {
	list     bool
	apply    string
	dryRun   bool
	json     bool
	label    []string
	assignee []string
}

func newIntakeCommand() *cobra.Command {
	opts := &intakeOptions{}

	cmd := &cobra.Command{
		Use:   "intake",
		Short: "Find issues not yet added to the project",
		Long: `Find open issues in configured repositories that are not yet tracked in the project.

This helps ensure all work is captured on your project board.
Choose a mode: --list to list untracked issues, --dry-run to preview what
--apply would add, or --apply to add them. Without a mode, this help is shown.`,
		Aliases: []string{"in"},
		Example: `  # List untracked issues
  gh pmu intake --list

  # Filter by label
  gh pmu intake --list --label bug --label urgent

  # Filter by assignee
  gh pmu intake --list --assignee username

  # Preview what would be added
  gh pmu intake --dry-run

  # Add untracked issues to project (with defaults from config)
  gh pmu intake --apply

  # Add issues and set specific fields (note: --apply= form is required for values)
  gh pmu intake --apply=status:backlog,priority:p1

  # Output as JSON
  gh pmu intake --list --json`,
		// --apply uses NoOptDefVal so `--apply value` treats value as a positional.
		// NoArgs rejects such stray positionals loudly instead of silently ignoring them.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIntake(cmd, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.list, "list", false, "List untracked issues")
	cmd.Flags().StringVarP(&opts.apply, "apply", "a", "", "Add untracked issues to project (optionally set fields: status:backlog,priority:p1)")
	cmd.Flags().Lookup("apply").NoOptDefVal = " " // Allow --apply without a value (uses config defaults)
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show what would be added without making changes")
	cmd.Flags().BoolVar(&opts.json, "json", false, "Output in JSON format")
	cmd.Flags().StringArrayVarP(&opts.label, "label", "l", nil, "Filter issues by label (can be specified multiple times)")
	cmd.Flags().StringArrayVar(&opts.assignee, "assignee", nil, "Filter issues by assignee (can be specified multiple times)")
	cmd.MarkFlagsMutuallyExclusive("list", "apply")

	return cmd
}

func runIntake(cmd *cobra.Command, opts *intakeOptions) error {
	// No mode selected: show help before any config load or API traffic (#918).
	// --json, --label and --assignee only modify a mode, they do not select one.
	if !opts.list && !opts.dryRun && !cmd.Flags().Changed("apply") {
		return cmd.Help()
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

	if len(cfg.Repositories) == 0 {
		return fmt.Errorf("no repositories configured in .gh-pmu.json")
	}

	// Create API client
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	return runIntakeWithDeps(cmd, opts, cfg, client)
}

// runIntakeWithDeps is the testable implementation of runIntake
func runIntakeWithDeps(cmd *cobra.Command, opts *intakeOptions, cfg *config.Config, client intakeClient) error {
	// Get project
	project, err := client.GetProject(cfg.Project.Owner, cfg.Project.Number)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	// Find untracked issues from each repository. Membership comes from each open
	// issue's own projectItems, never from paging the project board (#918).
	var untrackedIssues []api.Issue
	for _, repoFullName := range cfg.Repositories {
		parts := strings.SplitN(repoFullName, "/", 2)
		if len(parts) != 2 {
			cmd.PrintErrf("Warning: invalid repository format %q, expected owner/repo\n", repoFullName)
			continue
		}
		owner, repo := parts[0], parts[1]

		// Label filtering happens server-side in the search query
		candidates, err := client.SearchIntakeCandidates(owner, repo, opts.label, project.ID)
		if err != nil {
			cmd.PrintErrf("Warning: failed to get issues from %s: %v\n", repoFullName, err)
			continue
		}

		for _, candidate := range candidates {
			issue := candidate.Issue
			if candidate.MembershipUnknown {
				cmd.PrintErrf("Warning: #%d in %s belongs to more than 20 projects; could not confirm whether it is on this project, so it is not listed\n",
					issue.Number, repoFullName)
				continue
			}
			if !candidate.InProject {
				issue.Repository = api.Repository{Owner: owner, Name: repo}
				untrackedIssues = append(untrackedIssues, issue)
			}
		}
	}

	// Apply assignee filter if specified. Resolve first — filterIntakeByAssignee
	// compares logins literally, so @me would match nothing.
	if len(opts.assignee) > 0 {
		resolved, err := client.ResolveAssignees(opts.assignee)
		if err != nil {
			return err
		}
		untrackedIssues = filterIntakeByAssignee(untrackedIssues, resolved)
	}

	// Handle output
	if len(untrackedIssues) == 0 {
		if !opts.json {
			cmd.Println("All issues are already tracked in the project")
		} else {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			_ = encoder.Encode(map[string]interface{}{"issues": []interface{}{}, "count": 0})
		}
		return nil
	}

	// Dry run - just show what would be added
	if opts.dryRun {
		if opts.json {
			return outputIntakeJSON(cmd, untrackedIssues, "dry-run")
		}
		cmd.Printf("Would add %d issue(s) to project:\n\n", len(untrackedIssues))
		return outputIntakeTable(cmd, untrackedIssues)
	}

	// Apply - add issues to project
	// Check if apply was specified (could be empty string "" for just --apply, or have key:value pairs)
	applyFlagSet := cmd.Flags().Changed("apply")
	if applyFlagSet {
		// Parse key:value pairs from apply string
		applyFields := parseApplyFields(opts.apply)

		// Fetch project fields ONCE before the issue loop (#833).
		// Previously each SetProjectItemField call internally fetched fields,
		// causing 1 + 3N API calls for N issues.
		projectFields, err := client.GetProjectFields(project.ID)
		if err != nil {
			return fmt.Errorf("failed to get project fields: %w", err)
		}

		// Phase 1: add every issue in batched requests (#918).
		issueIDs := make([]string, len(untrackedIssues))
		byID := make(map[string]api.Issue, len(untrackedIssues))
		for i, issue := range untrackedIssues {
			issueIDs[i] = issue.ID
			byID[issue.ID] = issue
		}
		addResults, err := client.BatchAddIssuesToProject(project.ID, issueIDs)
		if err != nil {
			return fmt.Errorf("failed to add issues to project: %w", err)
		}

		failedIDs := make(map[string]bool)
		itemIssue := make(map[string]api.Issue) // item ID -> issue, for field results
		var updates []api.FieldUpdate
		fieldValues := intakeFieldValues(cfg, applyFields)
		for _, result := range addResults {
			issue := byID[result.IssueID]
			if !result.Success {
				cmd.PrintErrf("Failed to add #%d: %s\n", issue.Number, result.Error)
				failedIDs[issue.ID] = true
				continue
			}
			itemIssue[result.ItemID] = issue
			for _, fv := range fieldValues {
				updates = append(updates, api.FieldUpdate{ItemID: result.ItemID, FieldName: fv.name, Value: fv.value})
			}
		}

		// Phase 2: set fields on the added items with the existing batch helper.
		if len(updates) > 0 {
			updateResults, err := client.BatchUpdateProjectItemFields(project.ID, updates, projectFields)
			if err != nil {
				return fmt.Errorf("failed to set fields on added issues: %w", err)
			}
			for _, result := range updateResults {
				if result.Success {
					continue
				}
				issue := itemIssue[result.ItemID]
				cmd.PrintErrf("Warning: failed to set %s on #%d: %s\n", result.FieldName, issue.Number, result.Error)
				failedIDs[issue.ID] = true
			}
		}

		// An issue counts as added only when it was added and every field was set.
		var added []api.Issue
		for _, issue := range untrackedIssues {
			if !failedIDs[issue.ID] {
				added = append(added, issue)
			}
		}

		if opts.json {
			return outputIntakeJSON(cmd, added, "applied")
		}

		cmd.Printf("Added %d issue(s) to project", len(added))
		if len(failedIDs) > 0 {
			cmd.Printf(" (%d failed)", len(failedIDs))
		}
		cmd.Println()
		return nil
	}

	// Default - just list untracked issues
	if opts.json {
		return outputIntakeJSON(cmd, untrackedIssues, "untracked")
	}

	cmd.Printf("Found %d untracked issue(s):\n\n", len(untrackedIssues))
	if err := outputIntakeTable(cmd, untrackedIssues); err != nil {
		return err
	}
	cmd.Println("\nUse --apply to add these issues to the project")
	return nil
}

type intakeFieldValue struct {
	name, value string
}

// intakeFieldValues resolves the fields to set on each added issue: --apply
// key:value pairs first (status and priority resolved through config aliases),
// then config defaults for status and priority when --apply did not set them.
// Keys are sorted so updates are sent in a stable order.
func intakeFieldValues(cfg *config.Config, applyFields map[string]string) []intakeFieldValue {
	keys := make([]string, 0, len(applyFields))
	for k := range applyFields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var values []intakeFieldValue
	statusSet, prioritySet := false, false
	for _, field := range keys {
		value := applyFields[field]
		switch strings.ToLower(field) {
		case "status":
			values = append(values, intakeFieldValue{"Status", cfg.ResolveFieldValue("status", value)})
			statusSet = true
		case "priority":
			values = append(values, intakeFieldValue{"Priority", cfg.ResolveFieldValue("priority", value)})
			prioritySet = true
		default:
			values = append(values, intakeFieldValue{field, value})
		}
	}
	if !statusSet && cfg.Defaults.Status != "" {
		values = append(values, intakeFieldValue{"Status", cfg.ResolveFieldValue("status", cfg.Defaults.Status)})
	}
	if !prioritySet && cfg.Defaults.Priority != "" {
		values = append(values, intakeFieldValue{"Priority", cfg.ResolveFieldValue("priority", cfg.Defaults.Priority)})
	}
	return values
}

func outputIntakeTable(cmd *cobra.Command, issues []api.Issue) error {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NUMBER\tTITLE\tREPOSITORY\tSTATE")

	for _, issue := range issues {
		title := issue.Title
		if len(title) > 50 {
			title = title[:47] + "..."
		}
		repoName := fmt.Sprintf("%s/%s", issue.Repository.Owner, issue.Repository.Name)
		fmt.Fprintf(w, "#%d\t%s\t%s\t%s\n", issue.Number, title, repoName, issue.State)
	}

	return w.Flush()
}

type intakeJSONOutput struct {
	Status string            `json:"status"`
	Count  int               `json:"count"`
	Issues []intakeJSONIssue `json:"issues"`
}

type intakeJSONIssue struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	State      string `json:"state"`
	URL        string `json:"url"`
	Repository string `json:"repository"`
}

func outputIntakeJSON(cmd *cobra.Command, issues []api.Issue, status string) error {
	output := intakeJSONOutput{
		Status: status,
		Count:  len(issues),
		Issues: make([]intakeJSONIssue, 0, len(issues)),
	}

	for _, issue := range issues {
		output.Issues = append(output.Issues, intakeJSONIssue{
			Number:     issue.Number,
			Title:      issue.Title,
			State:      issue.State,
			URL:        issue.URL,
			Repository: fmt.Sprintf("%s/%s", issue.Repository.Owner, issue.Repository.Name),
		})
	}

	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

// filterIntakeByLabel filters issues to only those with at least one of the specified labels
func filterIntakeByLabel(issues []api.Issue, labels []string) []api.Issue {
	var filtered []api.Issue
	for _, issue := range issues {
		for _, filterLabel := range labels {
			for _, issueLabel := range issue.Labels {
				if strings.EqualFold(issueLabel.Name, filterLabel) {
					filtered = append(filtered, issue)
					goto nextIssue
				}
			}
		}
	nextIssue:
	}
	return filtered
}

// filterIntakeByAssignee filters issues to only those with at least one of the specified assignees
func filterIntakeByAssignee(issues []api.Issue, assignees []string) []api.Issue {
	var filtered []api.Issue
	for _, issue := range issues {
		for _, filterAssignee := range assignees {
			for _, issueAssignee := range issue.Assignees {
				if strings.EqualFold(issueAssignee.Login, filterAssignee) {
					filtered = append(filtered, issue)
					goto nextIssue
				}
			}
		}
	nextIssue:
	}
	return filtered
}

// parseApplyFields parses a comma-separated list of key:value pairs
// Example: "status:backlog,priority:p1" -> {"status": "backlog", "priority": "p1"}
func parseApplyFields(s string) map[string]string {
	result := make(map[string]string)
	if s == "" {
		return result
	}

	pairs := strings.Split(s, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key != "" && value != "" {
				result[key] = value
			}
		}
	}
	return result
}
