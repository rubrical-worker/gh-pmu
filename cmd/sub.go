package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/spf13/cobra"
)

// requireResolvedIssue rejects an issue that decoded from a null GraphQL node.
//
// GetIssue does not fail on a bare {"repository":{"issue":null}} response with
// no errors envelope — shurcooL decodes it into a zero-value api.Issue — so a
// write path would otherwise send a mutation with an empty node id and report
// success. GitHub currently returns a NOT_FOUND envelope for a lookup by a
// nonexistent number, which is what makes GetIssue fail today; this guard means
// the write paths no longer depend on the server rejecting a malformed mutation
// (#889).
func requireResolvedIssue(issue *api.Issue, number int) error {
	if issue == nil || issue.ID == "" {
		return fmt.Errorf("issue #%d could not be resolved (empty node id); refusing to send a mutation", number)
	}
	return nil
}

func newSubCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sub",
		Short: "Manage sub-issues",
		Long: `Manage sub-issue relationships between issues.

Sub-issues allow you to create parent-child hierarchies between issues,
useful for breaking down epics into smaller tasks.`,
	}

	cmd.AddCommand(newSubAddCommand())
	cmd.AddCommand(newSubCreateCommand())
	cmd.AddCommand(newSubListCommand())
	cmd.AddCommand(newSubRemoveCommand())

	return cmd
}

type subAddOptions struct {
	repo string
}

func newSubAddCommand() *cobra.Command {
	opts := &subAddOptions{}

	cmd := &cobra.Command{
		Use:   "add <parent-issue> <child-issue>",
		Short: "Link an issue as a sub-issue of another",
		Long: `Link an existing issue as a sub-issue of a parent issue.

Both issues must already exist. The child issue will appear as a
sub-issue under the parent issue in GitHub's UI.

Accepts issue numbers, references (owner/repo#123), or full GitHub URLs.

Examples:
  gh pmu sub add 10 15        # Link issue #15 as sub-issue of #10
  gh pmu sub add #10 #15      # Same, with # prefix
  gh pmu sub add owner/repo#10 owner/repo#15  # Full references
  gh pmu sub add https://github.com/owner/repo/issues/10 15  # URL for parent
  gh pmu sub add 10 15 --repo owner/repo  # Specify default repository`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubAdd(cmd, args, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.repo, "repo", "R", "", "Default repository for issues (owner/repo format)")

	return cmd
}

func runSubAdd(cmd *cobra.Command, args []string, opts *subAddOptions) error {
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

	// Parse parent issue reference
	parentOwner, parentRepo, parentNumber, err := parseIssueReference(args[0])
	if err != nil {
		return fmt.Errorf("invalid parent issue: %w", err)
	}

	// Parse child issue reference
	childOwner, childRepo, childNumber, err := parseIssueReference(args[1])
	if err != nil {
		return fmt.Errorf("invalid child issue: %w", err)
	}

	// Determine default repository (--repo flag takes precedence over config).
	// Empty defaults are tolerated because issue args may be fully qualified; an
	// explicit --repo is validated with uniform empty-component rejection.
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

	// Apply defaults if not specified in reference
	if parentOwner == "" || parentRepo == "" {
		if defaultOwner == "" || defaultRepo == "" {
			return fmt.Errorf("no repository specified and none configured (use --repo or configure in .gh-pmu.json)")
		}
		parentOwner = defaultOwner
		parentRepo = defaultRepo
	}

	if childOwner == "" || childRepo == "" {
		if defaultOwner == "" || defaultRepo == "" {
			return fmt.Errorf("no repository specified and none configured (use --repo or configure in .gh-pmu.json)")
		}
		childOwner = defaultOwner
		childRepo = defaultRepo
	}

	// Create API client
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	// Validate parent issue exists
	parentIssue, err := client.GetIssue(parentOwner, parentRepo, parentNumber)
	if err != nil {
		return fmt.Errorf("failed to get parent issue #%d: %w", parentNumber, err)
	}
	if err := requireResolvedIssue(parentIssue, parentNumber); err != nil {
		return err
	}

	// Validate child issue exists
	childIssue, err := client.GetIssue(childOwner, childRepo, childNumber)
	if err != nil {
		return fmt.Errorf("failed to get child issue #%d: %w", childNumber, err)
	}
	if err := requireResolvedIssue(childIssue, childNumber); err != nil {
		return err
	}

	// Add sub-issue link
	err = client.AddSubIssue(parentIssue.ID, childIssue.ID)
	if err != nil {
		// Check if already linked (GitHub returns "duplicate" or "only have one parent" messages)
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "duplicate") || strings.Contains(errMsg, "only have one parent") {
			return fmt.Errorf("issue #%d is already a sub-issue (issues can only have one parent)", childNumber)
		}
		return fmt.Errorf("failed to add sub-issue link: %w", err)
	}

	// Output confirmation - show repo info if cross-repo
	w := cmd.OutOrStdout()
	isCrossRepo := (parentOwner != childOwner || parentRepo != childRepo)
	if isCrossRepo {
		fmt.Fprintf(w, "✓ Linked %s/%s#%d as sub-issue of %s/%s#%d\n",
			childOwner, childRepo, childNumber,
			parentOwner, parentRepo, parentNumber)
		fmt.Fprintf(w, "  Parent: %s (%s/%s)\n", parentIssue.Title, parentOwner, parentRepo)
		fmt.Fprintf(w, "  Child:  %s (%s/%s)\n", childIssue.Title, childOwner, childRepo)
	} else {
		fmt.Fprintf(w, "✓ Linked issue #%d as sub-issue of #%d\n", childNumber, parentNumber)
		fmt.Fprintf(w, "  Parent: %s\n", parentIssue.Title)
		fmt.Fprintf(w, "  Child:  %s\n", childIssue.Title)
	}

	return nil
}

type subCreateOptions struct {
	parent           string
	title            string
	body             string
	bodyFile         string
	repo             string // Target repository for the new issue (owner/repo format)
	labels           []string
	assignees        []string
	milestone        string
	project          int
	inheritLabels    bool
	inheritAssign    bool
	inheritMilestone bool
	dryRun           bool
}

func newSubCreateCommand() *cobra.Command {
	opts := &subCreateOptions{
		inheritLabels:    false,
		inheritAssign:    false,
		inheritMilestone: true,
	}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new issue as a sub-issue",
		Long: `Create a new issue and automatically link it as a sub-issue of a parent.

By default, the new issue is created in the same repository as the parent.
Use --repo to create the sub-issue in a different repository.

Labels from config defaults are applied. Use --inherit-labels to also inherit
labels from the parent issue (same repository only).

Examples:
  gh pmu sub create --parent 10 --title "Implement feature X"
  gh pmu sub create --parent #10 --title "Task" --body "Description"
  gh pmu sub create -p 10 -t "Task" --inherit-labels
  gh pmu sub create --parent owner/repo1#10 --repo owner/repo2 --title "Cross-repo task"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubCreate(cmd, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.parent, "parent", "p", "", "Parent issue number or reference (required)")
	cmd.Flags().StringVarP(&opts.title, "title", "t", "", "Issue title (required)")
	cmd.Flags().StringVarP(&opts.body, "body", "b", "", "Issue body")
	cmd.Flags().StringVarP(&opts.bodyFile, "body-file", "F", "", "Read body text from file (use \"-\" to read from standard input)")
	cmd.Flags().StringVarP(&opts.repo, "repo", "R", "", "Repository for the new issue (owner/repo format, defaults to parent's repo)")
	cmd.Flags().StringArrayVarP(&opts.labels, "label", "l", nil, "Add labels to the sub-issue (can be specified multiple times)")
	cmd.Flags().StringArrayVarP(&opts.assignees, "assignee", "a", nil, "Assign users to the sub-issue (can be specified multiple times)")
	cmd.Flags().StringVarP(&opts.milestone, "milestone", "m", "", "Set milestone (title or number)")
	cmd.Flags().IntVar(&opts.project, "project", 0, "Add to project (project number)")
	cmd.Flags().BoolVar(&opts.inheritLabels, "inherit-labels", false, "Inherit labels from parent (same repo only)")
	cmd.Flags().BoolVar(&opts.inheritAssign, "inherit-assignees", false, "Inherit assignees from parent (same repo only)")
	cmd.Flags().BoolVar(&opts.inheritMilestone, "inherit-milestone", true, "Inherit milestone from parent (same repo only)")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Resolve the parent, labels, assignees and milestone and print what would be created, without creating anything")

	_ = cmd.MarkFlagRequired("parent")
	_ = cmd.MarkFlagRequired("title")

	return cmd
}

// subCreatePreview is what sub create would write, for --dry-run.
type subCreatePreview struct {
	target, parent, parentTitle, milestone string
	labels, assignees                      []string
}

// printSubCreateDryRun reports the resolved parent, target repository, labels,
// assignees, milestone and project without creating or linking anything (#929).
// A --project that fails to resolve is an error here, unlike the real run's
// warning: a dry run exists to surface exactly that before anything is written.
func printSubCreateDryRun(cmd *cobra.Command, client *api.Client, cfg *config.Config, opts *subCreateOptions, p subCreatePreview) error {
	project := "(none)"
	if opts.project > 0 {
		proj, err := client.GetProject(cfg.Project.Owner, opts.project)
		if err != nil {
			return fmt.Errorf("failed to find project %d: %w", opts.project, err)
		}
		project = fmt.Sprintf("%s (%s #%d)", proj.Title, cfg.Project.Owner, opts.project)
	}
	orNone := func(values []string) string {
		if len(values) == 0 {
			return "(none)"
		}
		return strings.Join(values, ", ")
	}
	body := "(empty)"
	switch {
	case opts.bodyFile == "-":
		body = "standard input"
	case opts.bodyFile != "":
		body = "--body-file " + opts.bodyFile
	case opts.body != "":
		body = "--body"
	}
	milestone := p.milestone
	if milestone == "" {
		milestone = "(none)"
	}

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "Dry run: no issue will be created or linked.")
	fmt.Fprintf(w, "  Repository: %s\n", p.target)
	fmt.Fprintf(w, "  Parent:     %s: %s\n", p.parent, p.parentTitle)
	fmt.Fprintf(w, "  Title:      %s\n", opts.title)
	fmt.Fprintf(w, "  Body:       %s\n", body)
	fmt.Fprintf(w, "  Labels:     %s\n", orNone(p.labels))
	fmt.Fprintf(w, "  Assignees:  %s\n", orNone(p.assignees))
	fmt.Fprintf(w, "  Milestone:  %s\n", milestone)
	fmt.Fprintf(w, "  Project:    %s\n", project)
	return nil
}

func runSubCreate(cmd *cobra.Command, opts *subCreateOptions) error {
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

	// Validate body flags mutual exclusivity
	if opts.body != "" && opts.bodyFile != "" {
		return fmt.Errorf("cannot use --body and --body-file together")
	}

	// Read body from file if specified
	if opts.bodyFile != "" {
		content, err := readBodyFile(opts.bodyFile)
		if err != nil {
			return fmt.Errorf("failed to read body file: %w", err)
		}
		opts.body = content
	}

	// Parse parent issue reference
	parentOwner, parentRepo, parentNumber, err := parseIssueReference(opts.parent)
	if err != nil {
		return fmt.Errorf("invalid parent issue: %w", err)
	}

	// Default to the configured repo when the parent ref omits owner/repo.
	// sub create needs a resolved parent, so a missing/malformed config is fatal.
	if parentOwner == "" || parentRepo == "" {
		defOwner, defRepo, dErr := resolveRepoDefaults(cfg, "")
		if dErr != nil {
			return dErr
		}
		parentOwner, parentRepo = applyRepoDefaults(parentOwner, parentRepo, defOwner, defRepo)
	}

	// Determine target repository for new issue
	targetOwner := parentOwner
	targetRepo := parentRepo
	isCrossRepo := false

	if opts.repo != "" {
		var rErr error
		targetOwner, targetRepo, rErr = splitOwnerRepo(opts.repo, "--repo")
		if rErr != nil {
			return rErr
		}
		isCrossRepo = (targetOwner != parentOwner || targetRepo != parentRepo)
	}

	// Create API client
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	// Get parent issue to validate and optionally inherit from
	parentIssue, err := client.GetIssue(parentOwner, parentRepo, parentNumber)
	if err != nil {
		return fmt.Errorf("failed to get parent issue #%d: %w", parentNumber, err)
	}
	// Checked before CreateIssue: an unresolved parent must not leave a newly
	// created orphan issue behind.
	if err := requireResolvedIssue(parentIssue, parentNumber); err != nil {
		return err
	}

	// Build labels list: config defaults + explicit flags + optional inherited
	// Build deduplicated labels using map-based set
	seen := make(map[string]bool)
	var labels []string
	addLabel := func(name string) {
		if !seen[name] {
			seen[name] = true
			labels = append(labels, name)
		}
	}
	for _, l := range cfg.Defaults.Labels {
		addLabel(l)
	}
	for _, l := range opts.labels {
		addLabel(l)
	}
	if !isCrossRepo && opts.inheritLabels && len(parentIssue.Labels) > 0 {
		for _, l := range parentIssue.Labels {
			addLabel(l.Name)
		}
	}

	// Merge inherited assignees/milestone from the parent when requested.
	assignees := resolveInheritedAssignees(opts.assignees, parentIssue.Assignees, opts.inheritAssign, isCrossRepo)
	milestone := resolveInheritedMilestone(opts.milestone, parentIssue.Milestone, opts.inheritMilestone, isCrossRepo)

	// Resolve before creating so the confirmation below reports the account that
	// was actually assigned rather than echoing the sentinel back as "@@me".
	// CreateIssueWithOptions resolves again, served from the client's cache.
	assignees, resolveErr := client.ResolveAssignees(assignees)
	if resolveErr != nil {
		return resolveErr
	}

	if opts.dryRun {
		return printSubCreateDryRun(cmd, client, cfg, opts, subCreatePreview{
			target: targetOwner + "/" + targetRepo,
			parent: fmt.Sprintf("%s/%s#%d", parentOwner, parentRepo, parentNumber),
			parentTitle: parentIssue.Title, labels: labels, assignees: assignees, milestone: milestone,
		})
	}

	// Create the new issue in target repository with extended options
	newIssue, err := client.CreateIssueWithOptions(targetOwner, targetRepo, opts.title, opts.body, labels, assignees, milestone)
	if err != nil {
		return fmt.Errorf("failed to create issue in %s/%s: %w", targetOwner, targetRepo, err)
	}

	// Link as sub-issue
	w := cmd.OutOrStdout()
	err = client.AddSubIssue(parentIssue.ID, newIssue.ID)
	if err != nil {
		// Issue was created but linking failed - report as partial failure
		fmt.Fprintf(w, "Created issue #%d: %s\n", newIssue.Number, newIssue.Title)
		fmt.Fprintf(w, "%s\n", newIssue.URL)
		return fmt.Errorf("issue #%d created but failed to link as sub-issue of #%d: %w", newIssue.Number, parentNumber, err)
	}

	// Add to project if specified
	if opts.project > 0 {
		project, err := client.GetProject(cfg.Project.Owner, opts.project)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to find project %d: %v\n", opts.project, err)
		} else {
			_, err := client.AddIssueToProject(project.ID, newIssue.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to add issue to project: %v\n", err)
			}
		}
	}

	// Output confirmation
	if isCrossRepo {
		fmt.Fprintf(w, "✓ Created cross-repo sub-issue %s/%s#%d under parent %s/%s#%d\n",
			targetOwner, targetRepo, newIssue.Number,
			parentOwner, parentRepo, parentNumber)
	} else {
		fmt.Fprintf(w, "✓ Created sub-issue #%d under parent #%d\n", newIssue.Number, parentNumber)
	}
	fmt.Fprintf(w, "  Title:  %s\n", newIssue.Title)
	fmt.Fprintf(w, "  Parent: %s\n", parentIssue.Title)
	if isCrossRepo {
		fmt.Fprintf(w, "  Repo:   %s/%s\n", targetOwner, targetRepo)
	}
	if len(labels) > 0 {
		fmt.Fprintf(w, "  Labels: %s\n", strings.Join(labels, ", "))
	}
	if len(assignees) > 0 {
		fmt.Fprintf(w, "  Assignees: @%s\n", strings.Join(assignees, ", @"))
	}
	if milestone != "" {
		fmt.Fprintf(w, "  Milestone: %s\n", milestone)
	}
	if opts.project > 0 {
		fmt.Fprintf(w, "  Project: #%d\n", opts.project)
	}
	fmt.Fprintf(w, "🔗 %s\n", newIssue.URL)

	return nil
}

// resolveInheritedAssignees returns the assignee logins to apply to a new
// sub-issue: the explicitly-requested ones plus, when --inherit-assignees is set
// and the sub-issue is created in the parent's repository, the parent's
// assignees. The result is deduplicated and order-stable (explicit first).
// Cross-repo inheritance is skipped because user membership does not carry across
// repositories.
func resolveInheritedAssignees(explicit []string, parentAssignees []api.Actor, inherit, isCrossRepo bool) []string {
	seen := make(map[string]bool)
	var result []string
	add := func(login string) {
		if login != "" && !seen[login] {
			seen[login] = true
			result = append(result, login)
		}
	}
	for _, a := range explicit {
		add(a)
	}
	if inherit && !isCrossRepo {
		for _, a := range parentAssignees {
			add(a.Login)
		}
	}
	return result
}

// resolveInheritedMilestone returns the milestone title to apply to a new
// sub-issue: the explicitly-requested one, or the parent's when --inherit-milestone
// is set and the sub-issue is created in the parent's repository. Cross-repo
// milestone references are not portable, so inheritance is skipped in that case.
func resolveInheritedMilestone(explicit string, parentMilestone *api.Milestone, inherit, isCrossRepo bool) string {
	if explicit != "" {
		return explicit
	}
	if inherit && !isCrossRepo && parentMilestone != nil {
		return parentMilestone.Title
	}
	return ""
}

type subListOptions struct {
	json     bool
	state    string
	limit    int
	relation string
	repo     string
}

func newSubListCommand() *cobra.Command {
	opts := &subListOptions{
		state:    "all",
		relation: "children",
	}

	cmd := &cobra.Command{
		Use:   "list <issue>",
		Short: "List sub-issues of an issue",
		Long: `List sub-issues related to an issue.

By default, shows children (sub-issues) of the given issue.
Use --relation to show parent, siblings, or all related issues.

Displays the title, state, and assignee for each sub-issue,
along with a completion count.

Examples:
  gh pmu sub list 10              # List sub-issues of issue #10
  gh pmu sub list #10             # Same, with # prefix
  gh pmu sub list 10 --json       # Output as JSON
  gh pmu sub list 10 -s open      # Show only open sub-issues
  gh pmu sub list 10 -n 5         # Limit to 5 results
  gh pmu sub list 10 --relation parent    # Show parent issue
  gh pmu sub list 10 --relation siblings  # Show sibling issues
  gh pmu sub list 10 --relation all       # Show parent, siblings, and children
  gh pmu sub list 10 --repo owner/repo    # Specify repository explicitly`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubList(cmd, args, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.json, "json", false, "Output in JSON format")
	cmd.Flags().StringVarP(&opts.state, "state", "s", "all", "Filter by state: open, closed, all")
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 0, "Maximum number of items to display (0 for no limit)")
	cmd.Flags().StringVar(&opts.relation, "relation", "children", "Relation to show: children, parent, siblings, all")
	cmd.Flags().StringVarP(&opts.repo, "repo", "R", "", "Repository for the issue (owner/repo format)")

	return cmd
}

func runSubList(cmd *cobra.Command, args []string, opts *subListOptions) error {
	// Validate state option
	opts.state = strings.ToLower(opts.state)
	if opts.state != "open" && opts.state != "closed" && opts.state != "all" {
		return fmt.Errorf("invalid state: %s (must be open, closed, or all)", opts.state)
	}

	// Validate relation option
	opts.relation = strings.ToLower(opts.relation)
	if opts.relation != "children" && opts.relation != "parent" && opts.relation != "siblings" && opts.relation != "all" {
		return fmt.Errorf("invalid relation: %s (must be children, parent, siblings, or all)", opts.relation)
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

	// Parse issue reference
	issueOwner, issueRepo, issueNumber, err := parseIssueReference(args[0])
	if err != nil {
		return fmt.Errorf("invalid issue: %w", err)
	}

	// Determine default repository (--repo flag takes precedence over config).
	// Empty defaults are tolerated because issue args may be fully qualified; an
	// explicit --repo is validated with uniform empty-component rejection.
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

	// Apply defaults if not specified in reference
	if issueOwner == "" || issueRepo == "" {
		if defaultOwner == "" || defaultRepo == "" {
			return fmt.Errorf("no repository specified and none configured (use --repo or configure in .gh-pmu.json)")
		}
		issueOwner = defaultOwner
		issueRepo = defaultRepo
	}

	// Create API client
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	// Get the issue to validate it exists
	issue, err := client.GetIssue(issueOwner, issueRepo, issueNumber)
	if err != nil {
		return fmt.Errorf("failed to get issue #%d: %w", issueNumber, err)
	}

	// Build the result based on relation
	var result SubListResult
	result.Issue = issue

	// Get children (sub-issues)
	if opts.relation == "children" || opts.relation == "all" {
		subIssues, err := client.GetSubIssues(issueOwner, issueRepo, issueNumber)
		if err != nil {
			return fmt.Errorf("failed to get sub-issues: %w", err)
		}
		result.Children = filterSubIssuesByState(subIssues, opts.state)
	}

	// Get parent
	if opts.relation == "parent" || opts.relation == "siblings" || opts.relation == "all" {
		parentIssue, err := client.GetParentIssue(issueOwner, issueRepo, issueNumber)
		if err == nil && parentIssue != nil {
			result.Parent = parentIssue
		}
	}

	// Get siblings (other children of parent)
	if opts.relation == "siblings" || opts.relation == "all" {
		if result.Parent != nil {
			// Get parent's repo info
			parentOwner := result.Parent.Repository.Owner
			parentRepo := result.Parent.Repository.Name
			if parentOwner == "" {
				parentOwner = issueOwner
			}
			if parentRepo == "" {
				parentRepo = issueRepo
			}

			siblings, err := client.GetSubIssues(parentOwner, parentRepo, result.Parent.Number)
			if err == nil {
				// Filter out the current issue from siblings
				var filteredSiblings []api.SubIssue
				for _, sib := range siblings {
					if sib.Number != issueNumber {
						filteredSiblings = append(filteredSiblings, sib)
					}
				}
				result.Siblings = filterSubIssuesByState(filteredSiblings, opts.state)
			}
		}
	}

	// Apply limit
	if opts.limit > 0 {
		if len(result.Children) > opts.limit {
			result.Children = result.Children[:opts.limit]
		}
		if len(result.Siblings) > opts.limit {
			result.Siblings = result.Siblings[:opts.limit]
		}
	}

	// Output
	w := cmd.OutOrStdout()
	if opts.json {
		return outputSubListJSONExtended(w, result, opts.relation)
	}

	return outputSubListTableExtended(w, result, opts.relation)
}

// SubListResult holds all the data for sub list output
type SubListResult struct {
	Issue    *api.Issue
	Parent   *api.Issue
	Children []api.SubIssue
	Siblings []api.SubIssue
}

// filterSubIssuesByState filters sub-issues by state (open, closed, all)
func filterSubIssuesByState(subIssues []api.SubIssue, state string) []api.SubIssue {
	if state == "all" {
		return subIssues
	}

	var filtered []api.SubIssue
	for _, sub := range subIssues {
		subState := strings.ToUpper(sub.State)
		if state == "open" && subState == "OPEN" {
			filtered = append(filtered, sub)
		} else if state == "closed" && subState == "CLOSED" {
			filtered = append(filtered, sub)
		}
	}
	return filtered
}

// SubListJSONOutput represents the JSON output for sub list command
type SubListJSONOutput struct {
	Parent    SubListParent  `json:"parent"`
	SubIssues []SubListItem  `json:"subIssues"`
	Summary   SubListSummary `json:"summary"`
}

type SubListParent struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type SubListItem struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	State      string `json:"state"`
	URL        string `json:"url"`
	Repository string `json:"repository"` // owner/repo format
}

type SubListSummary struct {
	Total  int `json:"total"`
	Open   int `json:"open"`
	Closed int `json:"closed"`
}

func outputSubListJSON(w io.Writer, subIssues []api.SubIssue, parent *api.Issue) error {
	output := SubListJSONOutput{
		Parent: SubListParent{
			Number: parent.Number,
			Title:  parent.Title,
		},
		SubIssues: make([]SubListItem, 0, len(subIssues)),
		Summary: SubListSummary{
			Total: len(subIssues),
		},
	}

	for _, sub := range subIssues {
		repoStr := ""
		if sub.Repository.Owner != "" && sub.Repository.Name != "" {
			repoStr = sub.Repository.Owner + "/" + sub.Repository.Name
		}
		output.SubIssues = append(output.SubIssues, SubListItem{
			Number:     sub.Number,
			Title:      sub.Title,
			State:      sub.State,
			URL:        sub.URL,
			Repository: repoStr,
		})

		if sub.State == "CLOSED" {
			output.Summary.Closed++
		} else {
			output.Summary.Open++
		}
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func outputSubListTable(w io.Writer, subIssues []api.SubIssue, parent *api.Issue) error {
	fmt.Fprintf(w, "Sub-issues of #%d: %s\n\n", parent.Number, parent.Title)

	if len(subIssues) == 0 {
		fmt.Fprintln(w, "No sub-issues found.")
		return nil
	}

	// Check if any sub-issues are in different repos
	parentRepo := parent.Repository.Owner + "/" + parent.Repository.Name
	hasCrossRepo := false
	for _, sub := range subIssues {
		subRepo := sub.Repository.Owner + "/" + sub.Repository.Name
		if subRepo != parentRepo && subRepo != "/" {
			hasCrossRepo = true
			break
		}
	}

	closedCount := 0
	for _, sub := range subIssues {
		state := "[ ]"
		if sub.State == "CLOSED" {
			state = "[x]"
			closedCount++
		}

		// Show repo info if there are cross-repo sub-issues
		if hasCrossRepo && sub.Repository.Owner != "" && sub.Repository.Name != "" {
			subRepo := sub.Repository.Owner + "/" + sub.Repository.Name
			fmt.Fprintf(w, "  %s %s#%d - %s\n", state, subRepo, sub.Number, sub.Title)
		} else {
			fmt.Fprintf(w, "  %s #%d - %s\n", state, sub.Number, sub.Title)
		}
	}

	fmt.Fprintf(w, "\nProgress: %d/%d complete\n", closedCount, len(subIssues))

	return nil
}

// SubListJSONExtended represents extended JSON output for sub list with relation support
type SubListJSONExtended struct {
	Issue    SubListIssueJSON   `json:"issue"`
	Parent   *SubListParentJSON `json:"parent,omitempty"`
	Children []SubListItem      `json:"children,omitempty"`
	Siblings []SubListItem      `json:"siblings,omitempty"`
	Summary  SubListSummary     `json:"summary"`
}

type SubListIssueJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	URL    string `json:"url"`
}

type SubListParentJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	URL    string `json:"url"`
}

func outputSubListJSONExtended(w io.Writer, result SubListResult, relation string) error {
	output := SubListJSONExtended{
		Issue: SubListIssueJSON{
			Number: result.Issue.Number,
			Title:  result.Issue.Title,
			State:  result.Issue.State,
			URL:    result.Issue.URL,
		},
	}

	// Add parent if present
	if result.Parent != nil {
		output.Parent = &SubListParentJSON{
			Number: result.Parent.Number,
			Title:  result.Parent.Title,
			State:  result.Parent.State,
			URL:    result.Parent.URL,
		}
	}

	// Add children
	if len(result.Children) > 0 {
		output.Children = make([]SubListItem, 0, len(result.Children))
		for _, sub := range result.Children {
			repoStr := ""
			if sub.Repository.Owner != "" && sub.Repository.Name != "" {
				repoStr = sub.Repository.Owner + "/" + sub.Repository.Name
			}
			output.Children = append(output.Children, SubListItem{
				Number:     sub.Number,
				Title:      sub.Title,
				State:      sub.State,
				URL:        sub.URL,
				Repository: repoStr,
			})
		}
	}

	// Add siblings
	if len(result.Siblings) > 0 {
		output.Siblings = make([]SubListItem, 0, len(result.Siblings))
		for _, sub := range result.Siblings {
			repoStr := ""
			if sub.Repository.Owner != "" && sub.Repository.Name != "" {
				repoStr = sub.Repository.Owner + "/" + sub.Repository.Name
			}
			output.Siblings = append(output.Siblings, SubListItem{
				Number:     sub.Number,
				Title:      sub.Title,
				State:      sub.State,
				URL:        sub.URL,
				Repository: repoStr,
			})
		}
	}

	// Build summary
	for _, sub := range result.Children {
		output.Summary.Total++
		if sub.State == "CLOSED" {
			output.Summary.Closed++
		} else {
			output.Summary.Open++
		}
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func outputSubListTableExtended(w io.Writer, result SubListResult, relation string) error {
	// Header
	fmt.Fprintf(w, "Issue #%d: %s\n", result.Issue.Number, result.Issue.Title)
	fmt.Fprintln(w)

	// Show parent if requested and present
	if (relation == "parent" || relation == "all") && result.Parent != nil {
		fmt.Fprintln(w, "Parent:")
		state := "OPEN"
		if result.Parent.State == "CLOSED" {
			state = "CLOSED"
		}
		fmt.Fprintf(w, "  #%d - %s [%s]\n", result.Parent.Number, result.Parent.Title, state)
		fmt.Fprintln(w)
	}

	// Show children if requested
	if relation == "children" || relation == "all" {
		fmt.Fprintln(w, "Children:")
		if len(result.Children) == 0 {
			fmt.Fprintln(w, "  No sub-issues found.")
		} else {
			printSubIssueList(w, result.Children, result.Issue)
		}
		fmt.Fprintln(w)
	}

	// Show siblings if requested
	if relation == "siblings" || relation == "all" {
		fmt.Fprintln(w, "Siblings:")
		if result.Parent == nil {
			fmt.Fprintln(w, "  No parent issue (not a sub-issue).")
		} else if len(result.Siblings) == 0 {
			fmt.Fprintln(w, "  No sibling issues found.")
		} else {
			printSubIssueList(w, result.Siblings, result.Issue)
		}
		fmt.Fprintln(w)
	}

	// Show progress summary for children
	if (relation == "children" || relation == "all") && len(result.Children) > 0 {
		closedCount := 0
		for _, sub := range result.Children {
			if sub.State == "CLOSED" {
				closedCount++
			}
		}
		fmt.Fprintf(w, "Progress: %d/%d complete\n", closedCount, len(result.Children))
	}

	return nil
}

// printSubIssueList prints a list of sub-issues with state checkboxes
func printSubIssueList(w io.Writer, subIssues []api.SubIssue, referenceIssue *api.Issue) {
	// Check if any sub-issues are in different repos
	refRepo := ""
	if referenceIssue.Repository.Owner != "" && referenceIssue.Repository.Name != "" {
		refRepo = referenceIssue.Repository.Owner + "/" + referenceIssue.Repository.Name
	}

	hasCrossRepo := false
	for _, sub := range subIssues {
		subRepo := sub.Repository.Owner + "/" + sub.Repository.Name
		if subRepo != refRepo && subRepo != "/" {
			hasCrossRepo = true
			break
		}
	}

	for _, sub := range subIssues {
		state := "[ ]"
		if sub.State == "CLOSED" {
			state = "[x]"
		}

		// Show repo info if there are cross-repo sub-issues
		if hasCrossRepo && sub.Repository.Owner != "" && sub.Repository.Name != "" {
			subRepo := sub.Repository.Owner + "/" + sub.Repository.Name
			fmt.Fprintf(w, "  %s %s#%d - %s\n", state, subRepo, sub.Number, sub.Title)
		} else {
			fmt.Fprintf(w, "  %s #%d - %s\n", state, sub.Number, sub.Title)
		}
	}
}

type subRemoveOptions struct {
	force bool
	repo  string
}

func newSubRemoveCommand() *cobra.Command {
	opts := &subRemoveOptions{}

	cmd := &cobra.Command{
		Use:   "remove <parent-issue> <child-issue>...",
		Short: "Remove sub-issue links from a parent issue",
		Long: `Remove the sub-issue relationship between a parent and one or more child issues.

This does NOT delete the child issues, only removes the parent-child links.
The child issues will become standalone issues again.

Multiple child issues can be specified to remove in batch.

Examples:
  gh pmu sub remove 10 15           # Unlink issue #15 from parent #10
  gh pmu sub remove #10 #15         # Same, with # prefix
  gh pmu sub remove 10 15 16 17     # Unlink multiple sub-issues at once
  gh pmu sub remove 10 15 --force   # Skip any confirmation prompts
  gh pmu sub remove owner/repo#10 owner/repo#15  # Full references`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubRemove(cmd, args, opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Skip confirmation prompts")
	cmd.Flags().StringVarP(&opts.repo, "repo", "R", "", "Default repository for issues (owner/repo format)")

	return cmd
}

func runSubRemove(cmd *cobra.Command, args []string, opts *subRemoveOptions) error {
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

	// Parse parent issue reference
	parentOwner, parentRepo, parentNumber, err := parseIssueReference(args[0])
	if err != nil {
		return fmt.Errorf("invalid parent issue: %w", err)
	}

	// Determine default repository (--repo flag takes precedence over config).
	// Empty defaults are tolerated because issue args may be fully qualified; an
	// explicit --repo is validated with uniform empty-component rejection.
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

	if parentOwner == "" || parentRepo == "" {
		if defaultOwner == "" || defaultRepo == "" {
			return fmt.Errorf("no repository specified and none configured")
		}
		parentOwner = defaultOwner
		parentRepo = defaultRepo
	}

	// Create API client
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	// Validate parent issue exists
	parentIssue, err := client.GetIssue(parentOwner, parentRepo, parentNumber)
	if err != nil {
		return fmt.Errorf("failed to get parent issue #%d: %w", parentNumber, err)
	}
	if err := requireResolvedIssue(parentIssue, parentNumber); err != nil {
		return err
	}

	// Parse all child issue references (args[1:])
	type childRef struct {
		owner  string
		repo   string
		number int
	}
	var children []childRef

	for i := 1; i < len(args); i++ {
		childOwner, childRepo, childNumber, err := parseIssueReference(args[i])
		if err != nil {
			return fmt.Errorf("invalid child issue %s: %w", args[i], err)
		}

		// Default to configured repo if not specified
		if childOwner == "" || childRepo == "" {
			if defaultOwner == "" || defaultRepo == "" {
				return fmt.Errorf("no repository specified for issue %s and none configured", args[i])
			}
			childOwner = defaultOwner
			childRepo = defaultRepo
		}

		children = append(children, childRef{
			owner:  childOwner,
			repo:   childRepo,
			number: childNumber,
		})
	}

	// Track results for batch operations
	var successCount, failCount int
	var results []string

	// Process each child issue
	for _, child := range children {
		// Get child issue
		childIssue, err := client.GetIssue(child.owner, child.repo, child.number)
		if err != nil {
			failCount++
			results = append(results, fmt.Sprintf("✗ #%d: failed to get issue: %v", child.number, err))
			continue
		}
		if err := requireResolvedIssue(childIssue, child.number); err != nil {
			failCount++
			results = append(results, fmt.Sprintf("✗ #%d: %v", child.number, err))
			continue
		}

		// Remove sub-issue link
		err = client.RemoveSubIssue(parentIssue.ID, childIssue.ID)
		if err != nil {
			failCount++
			// Check if not linked
			errMsg := strings.ToLower(err.Error())
			if strings.Contains(errMsg, "not a sub-issue") || strings.Contains(errMsg, "not found") {
				results = append(results, fmt.Sprintf("✗ #%d: not a sub-issue of #%d", child.number, parentNumber))
			} else {
				results = append(results, fmt.Sprintf("✗ #%d: %v", child.number, err))
			}
			continue
		}

		successCount++
		results = append(results, fmt.Sprintf("✓ #%d: %s", child.number, childIssue.Title))
	}

	// Output results
	w := cmd.OutOrStdout()
	if len(children) == 1 {
		// Single child - use simple output format
		if successCount == 1 {
			fmt.Fprintf(w, "✓ Removed sub-issue link: #%d is no longer a sub-issue of #%d\n", children[0].number, parentNumber)
			fmt.Fprintf(w, "  Former parent: %s\n", parentIssue.Title)
		} else {
			// Print the failure message
			for _, r := range results {
				fmt.Fprintln(w, r)
			}
			return fmt.Errorf("failed to remove sub-issue")
		}
	} else {
		// Multiple children - use batch output format
		fmt.Fprintf(w, "Removing sub-issues from parent #%d: %s\n\n", parentNumber, parentIssue.Title)
		for _, r := range results {
			fmt.Fprintln(w, "  "+r)
		}
		fmt.Fprintf(w, "\nSummary: %d succeeded, %d failed\n", successCount, failCount)

		if err := subRemoveBatchError(successCount, failCount, len(children)); err != nil {
			return err
		}
	}

	return nil
}

// subRemoveBatchError returns the error for a batch sub-remove result: any
// failure yields a non-nil error (partial or total), so scripts see a non-zero
// exit consistent with the single-child path. Returns nil only when nothing
// failed.
func subRemoveBatchError(successCount, failCount, total int) error {
	if failCount == 0 {
		return nil
	}
	if successCount == 0 {
		return fmt.Errorf("all %d removals failed", failCount)
	}
	return fmt.Errorf("%d of %d removals failed", failCount, total)
}
