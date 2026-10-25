package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	ghapi "github.com/cli/go-gh/v2/pkg/api"
	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// createClient defines the interface for API methods used by create functions.
// This allows for easier testing with mock implementations.
type createClient interface {
	CreateIssueWithOptions(owner, repo, title, body string, labels, assignees []string, milestone string) (*api.Issue, error)
	GetProject(owner string, number int) (*api.Project, error)
	GetProjectFields(projectID string) ([]api.ProjectField, error)
	AddIssueToProject(projectID, issueID string) (string, error)
	SetProjectItemField(projectID, itemID, fieldName, value string) error
	GetOpenIssuesByLabel(owner, repo, label string) ([]api.Issue, error)
}

type createOptions struct {
	title     string
	body      string
	bodyFile  string
	bodyStdin bool
	template  string
	status    string
	priority  string
	release   string
	labels    []string
	assignees []string
	milestone string
	repo      string
	project   string // --project owner/number (or bare number under the local owner); overrides the configured project (#924)
	fromFile  string
	dryRun    bool

	// parsedFile carries the file parsed during pre-flight validation so the
	// execution path does not re-read it (single parse). Nil when the execution
	// entry point is reached without pre-flight (e.g. tests), which then parses.
	parsedFile *issueFromFile
}

func newCreateCommand() *cobra.Command {
	opts := &createOptions{}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an issue with project metadata",
		Long: `Create a new issue and add it to the configured project.

The issue is automatically added to the configured project and
any specified field values (status, priority) are set.`,
		Example: `  # Create non-interactively with the body read from a file
  gh pmu create --title "Fix login redirect" --label bug --status backlog --priority p1 -F body.md

  # Create on the active branch (ready/in_progress statuses require --branch)
  gh pmu create --title "Add retry" --status ready --priority p2 --branch current -F body.md

  # Read the body from standard input
  cat body.md | gh pmu create --title "Update docs" --status backlog -F -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.title, "title", "t", "", "Issue title (required for non-interactive mode)")
	cmd.Flags().StringVarP(&opts.body, "body", "b", "", "Issue body")
	cmd.Flags().StringVarP(&opts.bodyFile, "body-file", "F", "", "Read body text from file (use \"-\" to read from standard input)")
	cmd.Flags().BoolVar(&opts.bodyStdin, "body-stdin", false, "Read body text from standard input")
	cmd.Flags().StringVarP(&opts.template, "template", "T", "", "Template name to use as starting body text")
	cmd.Flags().StringVarP(&opts.status, "status", "s", "", "Set project status field (e.g., backlog, in_progress)")
	cmd.Flags().StringVarP(&opts.priority, "priority", "p", "", "Set project priority field (e.g., p0, p1, p2)")
	cmd.Flags().StringVar(&opts.release, "branch", "", "Set branch field (use 'current' for active branch)")
	cmd.Flags().StringVarP(&opts.release, "release", "r", "", "[DEPRECATED] Use --branch instead")
	cmd.MarkFlagsMutuallyExclusive("branch", "release")
	cmd.Flags().StringArrayVarP(&opts.labels, "label", "l", nil, "Add labels (can be specified multiple times)")
	cmd.Flags().StringArrayVarP(&opts.assignees, "assignee", "a", nil, "Assign users (can be specified multiple times)")
	cmd.Flags().StringVarP(&opts.milestone, "milestone", "m", "", "Set milestone (title or number)")
	cmd.Flags().StringVarP(&opts.repo, "repo", "R", "", "Target repository (owner/repo format)")
	cmd.Flags().StringVar(&opts.project, "project", "", "Project to add the issue to (owner/number), overriding the configured one; required when --repo names a repository not listed in .gh-pmu.json")
	cmd.Flags().StringVarP(&opts.fromFile, "from-file", "f", "", "Create issue from YAML/JSON file")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Resolve the repository, project and fields and print what would be created, without creating anything")

	return cmd
}

// issueFromFile represents an issue definition in a YAML/JSON file
type issueFromFile struct {
	Title     string   `json:"title" yaml:"title"`
	Body      string   `json:"body" yaml:"body"`
	Labels    []string `json:"labels" yaml:"labels"`
	Assignees []string `json:"assignees" yaml:"assignees"`
	Milestone string   `json:"milestone" yaml:"milestone"`
	Status    string   `json:"status" yaml:"status"`
	Priority  string   `json:"priority" yaml:"priority"`
}

// validateCreateFlags checks flag constraints that can be evaluated without
// network access, so errors surface before creating an API client.
func validateCreateFlags(opts *createOptions) error {
	if opts.fromFile != "" {
		return validateCreateFromFileFlags(opts)
	}
	if opts.title == "" {
		return fmt.Errorf("--title is required")
	}
	if opts.body != "" && opts.bodyFile != "" {
		return fmt.Errorf("cannot use --body and --body-file together")
	}
	if opts.body != "" && opts.bodyStdin {
		return fmt.Errorf("cannot use --body and --body-stdin together")
	}
	if opts.bodyFile != "" && opts.bodyStdin {
		return fmt.Errorf("cannot use --body-file and --body-stdin together")
	}
	if opts.template != "" && (opts.body != "" || opts.bodyFile != "" || opts.bodyStdin) {
		return fmt.Errorf("cannot use --template with --body or --body-file")
	}
	return nil
}

// parseIssueFromFile reads and parses a YAML/JSON issue definition file. It is the
// single parse implementation shared by pre-flight validation and execution.
func parseIssueFromFile(path string) (*issueFromFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	var issueData issueFromFile
	if strings.HasSuffix(path, ".json") {
		if err := json.Unmarshal(data, &issueData); err != nil {
			return nil, fmt.Errorf("failed to parse JSON file: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &issueData); err != nil {
			return nil, fmt.Errorf("failed to parse YAML file: %w", err)
		}
	}
	return &issueData, nil
}

// validateCreateFromFileFlags validates --from-file inputs before API client creation.
// It rejects flags whose semantics are undefined in from-file mode (rather than
// silently ignoring them) and parses the file once, stashing the result on opts so
// the execution path can reuse it.
func validateCreateFromFileFlags(opts *createOptions) error {
	// Reject body-source flags that would be silently ignored in from-file mode.
	// The body comes from the file's `body:` field or --body. --title and --branch
	// ARE honored (merged in runCreateFromFileWithDeps).
	var rejected []string
	if opts.bodyFile != "" {
		rejected = append(rejected, "--body-file")
	}
	if opts.bodyStdin {
		rejected = append(rejected, "--body-stdin")
	}
	if opts.template != "" {
		rejected = append(rejected, "--template")
	}
	if len(rejected) > 0 {
		return fmt.Errorf("%s cannot be used with --from-file; set the body in the file or use --body",
			strings.Join(rejected, ", "))
	}

	issueData, err := parseIssueFromFile(opts.fromFile)
	if err != nil {
		return err
	}

	// Title may come from the file or be supplied/overridden by --title.
	if issueData.Title == "" && opts.title == "" {
		return fmt.Errorf("title is required in file")
	}

	opts.parsedFile = issueData
	return nil
}

func runCreate(cmd *cobra.Command, opts *createOptions) error {
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

	// Determine repository
	var owner, repo string
	if opts.repo != "" {
		// Use --repo flag
		repoParts := strings.Split(opts.repo, "/")
		if len(repoParts) != 2 {
			return fmt.Errorf("invalid --repo format: expected owner/repo, got %s", opts.repo)
		}
		owner, repo = repoParts[0], repoParts[1]
	} else {
		// Use config
		if len(cfg.Repositories) == 0 {
			return fmt.Errorf("no repository configured")
		}
		repoParts := strings.Split(cfg.Repositories[0], "/")
		if len(repoParts) != 2 {
			return fmt.Errorf("invalid repository format in config: %s", cfg.Repositories[0])
		}
		owner, repo = repoParts[0], repoParts[1]
	}

	// Validate flags before creating API client (fail fast without network)
	if err := validateCreateFlags(opts); err != nil {
		return err
	}

	// Create API client
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	return runCreateWithDeps(cmd, opts, cfg, client, owner, repo)
}

// runCreateWithDeps is the testable implementation of runCreate
func runCreateWithDeps(cmd *cobra.Command, opts *createOptions, cfg *config.Config, client createClient, owner, repo string) error {
	// Decide which board the issue lands on before anything else, so a refusal
	// creates nothing (#924).
	if err := checkCreateProjectTarget(cfg, opts.project, opts.repo, owner, repo); err != nil {
		return err
	}

	// Handle --from-file
	if opts.fromFile != "" {
		return runCreateFromFileWithDeps(cmd, opts, cfg, client, owner, repo)
	}

	// Handle non-interactive mode
	title := opts.title
	body := opts.body

	// Process --body-stdin: read body from stdin
	if opts.bodyStdin {
		if body != "" {
			return fmt.Errorf("cannot use --body and --body-stdin together")
		}
		if opts.bodyFile != "" {
			return fmt.Errorf("cannot use --body-file and --body-stdin together")
		}
		content, err := io.ReadAll(io.LimitReader(os.Stdin, maxBodyFileSize+1))
		if err != nil {
			return fmt.Errorf("failed to read body from stdin: %w", err)
		}
		if int64(len(content)) > maxBodyFileSize {
			return fmt.Errorf("stdin input exceeds maximum size of 1MB")
		}
		body = string(content)
	}

	// Process --body-file: read body from file
	if opts.bodyFile != "" {
		if body != "" {
			return fmt.Errorf("cannot use --body and --body-file together")
		}
		content, err := readBodyFile(opts.bodyFile)
		if err != nil {
			return fmt.Errorf("failed to read body file: %w", err)
		}
		body = content
	}

	// Process --template: load issue template
	if opts.template != "" {
		if body != "" {
			return fmt.Errorf("cannot use --template with --body or --body-file")
		}
		content, err := loadIssueTemplate(owner, repo, opts.template)
		if err != nil {
			return fmt.Errorf("failed to load template: %w", err)
		}
		body = content
	}

	if title == "" {
		return fmt.Errorf("--title is required")
	}

	// IDPF validation for create
	if cfg.IsIDPF() && opts.status != "" {
		statusValue := cfg.ResolveFieldValue("status", opts.status)
		if err := validateCreateOptions(statusValue, body, opts.release); err != nil {
			return err
		}
	}

	// Merge labels: config defaults + command line
	labels := append([]string{}, cfg.Defaults.Labels...)
	labels = append(labels, opts.labels...)

	// Resolve everything create reads from the API before writing anything, so
	// a failed lookup leaves no issue behind (#929).
	targets, err := resolveCreateTargets(client, cfg, opts.project, owner, repo, opts.release)
	if err != nil {
		return err
	}

	if opts.dryRun {
		printCreateDryRun(cmd, cfg, targets, createPreview{
			repo: owner + "/" + repo, title: title, bodySource: createBodySource(opts),
			labels: labels, assignees: opts.assignees, milestone: opts.milestone,
			status: opts.status, priority: opts.priority,
		})
		return nil
	}

	// Create the issue with extended options
	issue, err := client.CreateIssueWithOptions(owner, repo, title, body, labels, opts.assignees, opts.milestone)
	if err != nil {
		return fmt.Errorf("failed to create issue: %w", err)
	}

	return finishCreate(client, cfg, targets, issue, opts.status, opts.priority)
}

// finishCreate adds the new issue to the project, writes its fields and reports
// it. The issue already exists here, so any failure is a partial success that
// names the issue and the failed steps, never a plain error a caller would
// retry into a duplicate (#929).
func finishCreate(client createClient, cfg *config.Config, targets *createTargets, issue *api.Issue, status, priority string) error {
	itemID, err := client.AddIssueToProject(targets.project.ID, issue.ID)
	if err != nil {
		printCreatedIssue(issue)
		return createPartialSuccess(issue, []string{"add to project"}, fmt.Errorf("failed to add issue to project: %w", err))
	}

	failedSteps, unset, firstErr := setCreateFields(client, cfg, targets, itemID, status, priority)
	printCreatedIssue(issue)
	for _, u := range unset {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", u)
	}
	if len(failedSteps) > 0 {
		return createPartialSuccess(issue, failedSteps, firstErr)
	}
	return nil
}

// createPreview is what create would write, for --dry-run.
type createPreview struct {
	repo, title, bodySource, milestone, status, priority string
	labels, assignees                                    []string
}

// printCreateDryRun reports the resolved repository, project, fields and body
// source without creating anything (#929). Status and priority fall back to
// the config defaults exactly as a real create would apply them.
func printCreateDryRun(cmd *cobra.Command, cfg *config.Config, targets *createTargets, p createPreview) {
	w := cmd.OutOrStdout()
	orNone := func(values []string) string {
		if len(values) == 0 {
			return "(none)"
		}
		return strings.Join(values, ", ")
	}
	fieldValue := func(field, value, defaultValue string) string {
		switch {
		case value != "":
			return cfg.ResolveFieldValue(field, value)
		case defaultValue != "":
			return cfg.ResolveFieldValue(field, defaultValue) + " (config default)"
		default:
			return "(none)"
		}
	}
	branch := "(none)"
	if targets.branchValue != "" {
		branch = fmt.Sprintf("%s (%s field)", targets.branchValue, targets.branchFieldName)
	}
	milestone := p.milestone
	if milestone == "" {
		milestone = "(none)"
	}

	fmt.Fprintln(w, "Dry run: no issue will be created.")
	fmt.Fprintf(w, "  Repository: %s\n", p.repo)
	fmt.Fprintf(w, "  Project:    %s (%s #%d)\n", targets.project.Title, targets.projectOwner, targets.projectNumber)
	fmt.Fprintf(w, "  Title:      %s\n", p.title)
	fmt.Fprintf(w, "  Body:       %s\n", p.bodySource)
	fmt.Fprintf(w, "  Labels:     %s\n", orNone(p.labels))
	fmt.Fprintf(w, "  Assignees:  %s\n", orNone(p.assignees))
	fmt.Fprintf(w, "  Milestone:  %s\n", milestone)
	fmt.Fprintf(w, "  Status:     %s\n", fieldValue("status", p.status, cfg.Defaults.Status))
	fmt.Fprintf(w, "  Priority:   %s\n", fieldValue("priority", p.priority, cfg.Defaults.Priority))
	fmt.Fprintf(w, "  Branch:     %s\n", branch)
}

// createBodySource names where create takes the issue body from.
func createBodySource(opts *createOptions) string {
	switch {
	case opts.fromFile != "" && opts.body == "":
		return "--from-file " + opts.fromFile
	case opts.bodyStdin, opts.bodyFile == "-":
		return "standard input"
	case opts.bodyFile != "":
		return "--body-file " + opts.bodyFile
	case opts.template != "":
		return "--template " + opts.template
	case opts.body != "":
		return "--body"
	default:
		return "(empty)"
	}
}

func printCreatedIssue(issue *api.Issue) {
	fmt.Printf("Created issue #%d: %s\n", issue.Number, issue.Title)
	fmt.Printf("%s\n", issue.URL)
}

func createPartialSuccess(issue *api.Issue, steps []string, err error) error {
	return &api.PartialSuccessError{
		Resource: fmt.Sprintf("issue #%d", issue.Number),
		URL:      issue.URL,
		Steps:    steps,
		Err:      err,
	}
}

// runCreateFromFileWithDeps is the testable implementation of runCreateFromFile
func runCreateFromFileWithDeps(cmd *cobra.Command, opts *createOptions, cfg *config.Config, client createClient, owner, repo string) error {
	// Single parse: reuse the file parsed during pre-flight validation when
	// available, otherwise parse now (direct-call entry points, e.g. tests).
	issueData := opts.parsedFile
	if issueData == nil {
		parsed, err := parseIssueFromFile(opts.fromFile)
		if err != nil {
			return err
		}
		issueData = parsed
	}

	// Merge with command line options (command line takes precedence)
	title := issueData.Title
	if opts.title != "" {
		title = opts.title
	}
	if title == "" {
		return fmt.Errorf("title is required in file")
	}

	body := issueData.Body
	if opts.body != "" {
		body = opts.body
	}

	labels := append([]string{}, cfg.Defaults.Labels...)
	labels = append(labels, issueData.Labels...)
	labels = append(labels, opts.labels...)

	assignees := append([]string{}, issueData.Assignees...)
	assignees = append(assignees, opts.assignees...)

	milestone := issueData.Milestone
	if opts.milestone != "" {
		milestone = opts.milestone
	}

	status := issueData.Status
	if opts.status != "" {
		status = opts.status
	}

	priority := issueData.Priority
	if opts.priority != "" {
		priority = opts.priority
	}

	// IDPF validation on the merged values — same gate as the normal create path,
	// so from-file cannot bypass the IDPF rules (done needs body + checked boxes,
	// ready/in_progress/... need a branch).
	if cfg.IsIDPF() && status != "" {
		statusValue := cfg.ResolveFieldValue("status", status)
		if err := validateCreateOptions(statusValue, body, opts.release); err != nil {
			return err
		}
	}

	// Resolve everything create reads from the API before writing anything, so
	// a failed lookup leaves no issue behind (#929). --branch/--release is
	// honored in from-file mode too.
	targets, err := resolveCreateTargets(client, cfg, opts.project, owner, repo, opts.release)
	if err != nil {
		return err
	}

	if opts.dryRun {
		printCreateDryRun(cmd, cfg, targets, createPreview{
			repo: owner + "/" + repo, title: title, bodySource: createBodySource(opts),
			labels: labels, assignees: assignees, milestone: milestone,
			status: status, priority: priority,
		})
		return nil
	}

	// Create the issue
	issue, err := client.CreateIssueWithOptions(owner, repo, title, body, labels, assignees, milestone)
	if err != nil {
		return fmt.Errorf("failed to create issue: %w", err)
	}

	return finishCreate(client, cfg, targets, issue, status, priority)
}

// validateCreateOptions validates IDPF rules for issue creation
func validateCreateOptions(status, body, release string) error {
	statusLower := strings.ToLower(status)

	// Rule 1: Body required for in_review/done
	if statusLower == "in_review" || statusLower == "in review" || statusLower == "done" {
		if isBodyEmpty(body) {
			return fmt.Errorf("cannot create issue with status '%s' without body content", status)
		}
	}

	// Rule 2: All checkboxes must be checked for done
	if statusLower == "done" {
		unchecked := countUncheckedBoxes(body)
		if unchecked > 0 {
			return fmt.Errorf("cannot create issue as 'done' with %d unchecked checkbox(es)", unchecked)
		}
	}

	// Rule 3: Release required for ready/in_progress/in_review/done
	requiresRelease := statusLower == "ready" ||
		statusLower == "in progress" || statusLower == "in_progress" ||
		statusLower == "in_review" || statusLower == "in review" ||
		statusLower == "done"
	if requiresRelease && release == "" {
		return fmt.Errorf("cannot create issue with status '%s' without --branch flag\nUse: gh pmu create --status %s --branch \"release/vX.Y.Z\"", status, strings.ToLower(status))
	}

	return nil
}

// findActiveBranchForCreate finds the active branch tracker from a list of issues
// Returns nil if no active branch is found
// Supports both "Branch: " (new) and "Release: " (legacy) title formats
func findActiveBranchForCreate(issues []api.Issue) *api.Issue {
	for i := range issues {
		if strings.HasPrefix(issues[i].Title, "Branch: ") || strings.HasPrefix(issues[i].Title, "Release: ") {
			return &issues[i]
		}
	}
	return nil
}

// stripBranchPrefix removes a single branch tracker prefix ("Branch: " or "Release: ") from a title.
func stripBranchPrefix(title string) string {
	if strings.HasPrefix(title, "Branch: ") {
		return strings.TrimPrefix(title, "Branch: ")
	}
	return strings.TrimPrefix(title, "Release: ")
}

// resolveCreateBranchValue resolves the --branch/--release value: "current"
// becomes the active branch name, anything else passes through, and "" means
// no branch. An unresolved "current" is a hard error.
func resolveCreateBranchValue(client createClient, owner, repo, branchFlag string) (string, error) {
	if branchFlag != "current" {
		return branchFlag, nil
	}
	releaseIssues, err := client.GetOpenIssuesByLabel(owner, repo, "branch")
	if err != nil {
		return "", fmt.Errorf("failed to get branch issues: %w", err)
	}
	activeRelease := findActiveBranchForCreate(releaseIssues)
	if activeRelease == nil {
		return "", fmt.Errorf("no active branch found. Run 'gh pmu branch start' to create one")
	}
	// Support both "Branch: " and "Release: " prefixes
	return stripBranchPrefix(activeRelease.Title), nil
}

// createTargets is everything create reads from the API before it writes.
type createTargets struct {
	project         *api.Project
	projectOwner    string // owner of the selected project (configured, or --project)
	projectNumber   int
	overridden      bool // true when --project selected the project (#924)
	fields          []api.ProjectField
	branchFieldName string
	branchValue     string // resolved --branch value; "" when not set
}

// parseProjectRef parses a --project value: "owner/number", or a bare number
// meaning a project of defaultOwner (#924).
func parseProjectRef(ref, defaultOwner string) (string, int, error) {
	ref = strings.TrimSpace(ref)
	owner, numStr := defaultOwner, ref
	if o, n, ok := strings.Cut(ref, "/"); ok {
		owner, numStr = o, n
	}
	number, err := strconv.Atoi(numStr)
	if owner == "" || err != nil || number <= 0 || strings.Contains(numStr, "/") {
		return "", 0, fmt.Errorf("invalid --project %q: expected owner/number (e.g. my-org/7)", ref)
	}
	return owner, number, nil
}

// isConfiguredRepo reports whether owner/repo is listed in the config's
// repositories (case-insensitive, as GitHub treats names).
func isConfiguredRepo(cfg *config.Config, owner, repo string) bool {
	for _, r := range cfg.Repositories {
		if strings.EqualFold(r, owner+"/"+repo) {
			return true
		}
	}
	return false
}

// checkCreateProjectTarget refuses a --repo naming a repository the config does
// not list unless --project names the board. Without it, the issue would be
// added to the local project — the wrong board, silently (#924). It also
// validates a supplied --project before anything is created. repoFlag is the
// raw --repo value; without it the repository came from the config itself.
func checkCreateProjectTarget(cfg *config.Config, projectRef, repoFlag, owner, repo string) error {
	if projectRef != "" {
		_, _, err := parseProjectRef(projectRef, cfg.Project.Owner)
		return err
	}
	if repoFlag != "" && !isConfiguredRepo(cfg, owner, repo) {
		return fmt.Errorf("%s/%s is not listed in .gh-pmu.json repositories; pass --project <owner/number> to choose the project it is added to (refusing to add it to the local project %s #%d)",
			owner, repo, cfg.Project.Owner, cfg.Project.Number)
	}
	return nil
}

// resolveCreateTargets performs every lookup create depends on — the project,
// its fields and the --branch current tracker — none of which depend on the new
// issue. Running them before CreateIssueWithOptions leaves add-to-project and
// the field writes as the only post-create failure points (#929). projectRef
// is the --project value; "" selects the configured project.
func resolveCreateTargets(client createClient, cfg *config.Config, projectRef, owner, repo, branchFlag string) (*createTargets, error) {
	projectOwner, projectNumber := cfg.Project.Owner, cfg.Project.Number
	if projectRef != "" {
		var err error
		if projectOwner, projectNumber, err = parseProjectRef(projectRef, cfg.Project.Owner); err != nil {
			return nil, err
		}
	}
	project, err := client.GetProject(projectOwner, projectNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get project %s #%d: %w", projectOwner, projectNumber, err)
	}
	projectFields, err := client.GetProjectFields(project.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get project fields: %w", err)
	}
	branchValue, err := resolveCreateBranchValue(client, owner, repo, branchFlag)
	if err != nil {
		return nil, err
	}
	return &createTargets{
		project:         project,
		projectOwner:    projectOwner,
		projectNumber:   projectNumber,
		overridden:      projectRef != "",
		fields:          projectFields,
		branchFieldName: ResolveBranchFieldName(projectFields),
		branchValue:     branchValue,
	}, nil
}

// setCreateFields writes status, priority (or their config defaults) and branch
// onto the new project item. Field names come from cfg.Fields, falling back to
// the capitalized defaults. Every write is attempted even after one fails, so a
// single bad field does not leave the others unset; the failed steps and the
// first error are returned for the partial-success report.
//
// On a --project override, status and priority are checked against the
// selected project's own fields first: a field or option that project lacks is
// returned in unset, named, and not written — never guessed, and never a
// failure of the creation (#924).
func setCreateFields(client createClient, cfg *config.Config, targets *createTargets, itemID, status, priority string) (failedSteps, unset []string, firstErr error) {
	projectID := targets.project.ID
	set := func(step, fieldName, value string) {
		if targets.overridden && fieldName != targets.branchFieldName {
			if reason := unresolvableOnProject(targets.fields, fieldName, value); reason != "" {
				unset = append(unset, fmt.Sprintf("%s left unset: %s on %s #%d", fieldName, reason, targets.projectOwner, targets.projectNumber))
				return
			}
		}
		if err := client.SetProjectItemField(projectID, itemID, fieldName, value); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to %s: %v\n", step, err)
			failedSteps = append(failedSteps, step)
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to %s: %w", step, err)
			}
		}
	}

	statusFieldName := cfg.GetFieldNameOr("status", "Status")
	priorityFieldName := cfg.GetFieldNameOr("priority", "Priority")

	if status != "" {
		set("set status", statusFieldName, cfg.ResolveFieldValue("status", status))
	} else if cfg.Defaults.Status != "" {
		set("set status (config default)", statusFieldName, cfg.ResolveFieldValue("status", cfg.Defaults.Status))
	}

	if priority != "" {
		set("set priority", priorityFieldName, cfg.ResolveFieldValue("priority", priority))
	} else if cfg.Defaults.Priority != "" {
		set("set priority (config default)", priorityFieldName, cfg.ResolveFieldValue("priority", cfg.Defaults.Priority))
	}

	if targets.branchValue != "" {
		set("set branch", targets.branchFieldName, targets.branchValue)
	}
	return failedSteps, unset, firstErr
}

// unresolvableOnProject says why value cannot be written to fieldName on a
// project with these fields, or "" when it can — or cannot be checked, as for
// a text field, where the write itself decides.
func unresolvableOnProject(fields []api.ProjectField, fieldName, value string) string {
	for _, f := range fields {
		if f.Name != fieldName {
			continue
		}
		if len(f.Options) == 0 {
			return ""
		}
		for _, o := range f.Options {
			if o.Name == value {
				return ""
			}
		}
		return fmt.Sprintf("%q is not an option of %s", value, fieldName)
	}
	return fmt.Sprintf("the project has no %s field", fieldName)
}

// maxBodyFileSize is the maximum size for body/template file reads (1MB).
const maxBodyFileSize = 1 * 1024 * 1024

// readBodyFile reads the body content from a file or stdin with a 1MB size limit.
func readBodyFile(path string) (string, error) {
	var content []byte
	var err error

	if path == "-" {
		content, err = io.ReadAll(io.LimitReader(os.Stdin, maxBodyFileSize+1))
	} else {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return "", statErr
		}
		if info.Size() > maxBodyFileSize {
			return "", fmt.Errorf("file %s exceeds maximum size of 1MB", path)
		}
		content, err = os.ReadFile(path)
	}

	if err != nil {
		return "", err
	}
	if int64(len(content)) > maxBodyFileSize {
		return "", fmt.Errorf("input exceeds maximum size of 1MB")
	}

	return string(content), nil
}

// loadIssueTemplate loads an issue template from the repository
func loadIssueTemplate(owner, repo, templateName string) (string, error) {
	// First, try to find it locally in .github/ISSUE_TEMPLATE/
	localPaths := []string{
		filepath.Join(".github", "ISSUE_TEMPLATE", templateName+".md"),
		filepath.Join(".github", "ISSUE_TEMPLATE", templateName+".yml"),
		filepath.Join(".github", "ISSUE_TEMPLATE", templateName+".yaml"),
		filepath.Join(".github", "ISSUE_TEMPLATE", templateName),
	}

	for _, path := range localPaths {
		info, statErr := os.Stat(path)
		if statErr != nil {
			continue
		}
		if info.Size() > maxBodyFileSize {
			continue
		}
		if content, err := os.ReadFile(path); err == nil {
			return extractTemplateBody(string(content)), nil
		}
	}

	// Fall back to fetching from GitHub via go-gh REST client
	restClient, err := ghapi.DefaultRESTClient()
	if err != nil {
		return "", fmt.Errorf("template '%s' not found (failed to create REST client: %w)", templateName, err)
	}

	var fileContent struct {
		Content string `json:"content"`
	}
	endpoint := fmt.Sprintf("repos/%s/%s/contents/.github/ISSUE_TEMPLATE/%s.md", owner, repo, templateName)
	err = restClient.Get(endpoint, &fileContent)
	if err != nil {
		return "", fmt.Errorf("template '%s' not found", templateName)
	}

	// Decode base64 content
	decoded, err := decodeBase64Content(strings.TrimSpace(fileContent.Content))
	if err != nil {
		return "", fmt.Errorf("failed to decode template: %w", err)
	}

	return extractTemplateBody(decoded), nil
}

// extractTemplateBody extracts the body from a template file, handling YAML frontmatter
func extractTemplateBody(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return content
	}

	// Check for YAML frontmatter
	if strings.TrimSpace(lines[0]) == "---" {
		inFrontmatter := true
		var bodyLines []string
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" && inFrontmatter {
				inFrontmatter = false
				continue
			}
			if !inFrontmatter {
				bodyLines = append(bodyLines, lines[i])
			}
		}
		return strings.TrimSpace(strings.Join(bodyLines, "\n"))
	}

	return content
}

// decodeBase64Content decodes base64 encoded content from GitHub API
func decodeBase64Content(encoded string) (string, error) {
	// Remove newlines that GitHub API includes
	encoded = strings.ReplaceAll(encoded, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
