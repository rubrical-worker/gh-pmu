package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	fromFile  string

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
	cmd.Flags().StringVarP(&opts.fromFile, "from-file", "f", "", "Create issue from YAML/JSON file")

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
	targets, err := resolveCreateTargets(client, cfg, owner, repo, opts.release)
	if err != nil {
		return err
	}

	// Create the issue with extended options
	issue, err := client.CreateIssueWithOptions(owner, repo, title, body, labels, opts.assignees, opts.milestone)
	if err != nil {
		return fmt.Errorf("failed to create issue: %w", err)
	}

	itemID, err := client.AddIssueToProject(targets.project.ID, issue.ID)
	if err != nil {
		return fmt.Errorf("failed to add issue to project: %w", err)
	}

	setCreateFields(client, cfg, targets, itemID, opts.status, opts.priority)

	// Output the result
	fmt.Printf("Created issue #%d: %s\n", issue.Number, issue.Title)
	fmt.Printf("%s\n", issue.URL)

	return nil
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
	targets, err := resolveCreateTargets(client, cfg, owner, repo, opts.release)
	if err != nil {
		return err
	}

	// Create the issue
	issue, err := client.CreateIssueWithOptions(owner, repo, title, body, labels, assignees, milestone)
	if err != nil {
		return fmt.Errorf("failed to create issue: %w", err)
	}

	itemID, err := client.AddIssueToProject(targets.project.ID, issue.ID)
	if err != nil {
		return fmt.Errorf("failed to add issue to project: %w", err)
	}

	setCreateFields(client, cfg, targets, itemID, status, priority)

	// Output the result
	fmt.Printf("Created issue #%d: %s\n", issue.Number, issue.Title)
	fmt.Printf("%s\n", issue.URL)

	return nil
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
	branchFieldName string
	branchValue     string // resolved --branch value; "" when not set
}

// resolveCreateTargets performs every lookup create depends on — the project,
// its fields and the --branch current tracker — none of which depend on the new
// issue. Running them before CreateIssueWithOptions leaves add-to-project and
// the field writes as the only post-create failure points (#929).
func resolveCreateTargets(client createClient, cfg *config.Config, owner, repo, branchFlag string) (*createTargets, error) {
	project, err := client.GetProject(cfg.Project.Owner, cfg.Project.Number)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
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
		branchFieldName: ResolveBranchFieldName(projectFields),
		branchValue:     branchValue,
	}, nil
}

// setCreateFields writes status, priority (or their config defaults) and branch
// onto the new project item. Field names come from cfg.Fields, falling back to
// the capitalized defaults. Failures warn and continue, so one bad field does
// not block the rest.
func setCreateFields(client createClient, cfg *config.Config, targets *createTargets, itemID, status, priority string) {
	projectID := targets.project.ID
	statusFieldName := cfg.GetFieldNameOr("status", "Status")
	priorityFieldName := cfg.GetFieldNameOr("priority", "Priority")

	if status != "" {
		if err := client.SetProjectItemField(projectID, itemID, statusFieldName, cfg.ResolveFieldValue("status", status)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to set status: %v\n", err)
		}
	} else if cfg.Defaults.Status != "" {
		if err := client.SetProjectItemField(projectID, itemID, statusFieldName, cfg.ResolveFieldValue("status", cfg.Defaults.Status)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to set default status: %v\n", err)
		}
	}

	if priority != "" {
		if err := client.SetProjectItemField(projectID, itemID, priorityFieldName, cfg.ResolveFieldValue("priority", priority)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to set priority: %v\n", err)
		}
	} else if cfg.Defaults.Priority != "" {
		if err := client.SetProjectItemField(projectID, itemID, priorityFieldName, cfg.ResolveFieldValue("priority", cfg.Defaults.Priority)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to set default priority: %v\n", err)
		}
	}

	if targets.branchValue != "" {
		if err := client.SetProjectItemField(projectID, itemID, targets.branchFieldName, targets.branchValue); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to set branch: %v\n", err)
		}
	}
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
