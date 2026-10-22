package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/spf13/cobra"
)

// viewClient defines the interface for API methods used by view functions.
// This allows for easier testing with mock implementations.
type viewClient interface {
	GetIssue(owner, repo string, number int) (*api.Issue, error)
	GetIssueWithProjectFields(projectID, owner, repo string, number int) (*api.Issue, []api.FieldValue, error)
	GetSubIssues(owner, repo string, number int) ([]api.SubIssue, error)
	GetParentIssue(owner, repo string, number int) (*api.Issue, error)
	GetIssueComments(owner, repo string, number int) ([]api.Comment, error)
	// Batch methods for multi-issue mode
	GetIssuesWithProjectFieldsBatch(projectID, owner, repo string, numbers []int) (map[int]*api.Issue, map[int][]api.FieldValue, map[int]error, error)
	GetSubIssuesBatch(owner, repo string, numbers []int) (map[int][]api.SubIssue, error)
	GetParentIssueBatch(owner, repo string, numbers []int) (map[int]*api.Issue, error)
}

// viewIssueRef represents a parsed issue reference for the view command
type viewIssueRef struct {
	owner  string
	repo   string
	number int
}

// issueKey uniquely identifies an issue by repository and number, so multi-issue
// result maps do not collide across repositories that reuse issue numbers.
type issueKey struct {
	owner  string
	repo   string
	number int
}

// viewResult holds all data for a single issue in multi-issue mode
type viewResult struct {
	number      int
	issue       *api.Issue
	fieldValues []api.FieldValue
	subIssues   []api.SubIssue
	parentIssue *api.Issue
	comments    []api.Comment
}

type viewOptions struct {
	jsonFields string // Empty = not set, "all" = all fields, "field1,field2" = specific fields
	jq         string
	template   string // Go template (not supported - returns helpful error)
	comments   bool
	repo       string
	bodyFile   bool
	bodyStdout bool
	// projectID is the configured project's node ID, resolved once in runView and
	// threaded to the batch/single field-fetch calls so an issue on multiple
	// boards surfaces only the configured project's field values (#856). Empty
	// when the project could not be resolved, which disables the filter.
	projectID string
}

// viewAvailableFields lists all available JSON fields for the view command
var viewAvailableFields = []string{
	"assignees",
	"author",
	"body",
	"branch",
	"comments",
	"fieldValues",
	"labels",
	"milestone",
	"number",
	"parentIssue",
	"priority",
	"state",
	"status",
	"subIssues",
	"subProgress",
	"title",
	"url",
}

func newViewCommand() *cobra.Command {
	opts := &viewOptions{}

	cmd := &cobra.Command{
		Use:   "view <issue-number> [issue-number...]",
		Short: "View one or more issues with project metadata",
		Long: `View one or more issues with project field values.

Displays issue details including title, body, state, labels, assignees,
and all project-specific fields like Status and Priority.

When viewing multiple issues, results are shown sequentially with separators.
JSON output (--json) returns an array for multiple issues, a single object for one.

Also shows sub-issues if any exist, and parent issue if this is a sub-issue.

Note: --body-file and --body-stdout are only supported for single issues.`,
		Example: `  # View an issue with its project fields
  gh pmu view 42

  # Select JSON fields and extract one with jq
  gh pmu view 42 --json=status --jq='.status'

  # Several issues at once (JSON array)
  gh pmu view 42 43 --json=number,title,status

  # Write the raw body to stdout for editing
  gh pmu view 42 --body-stdout > body.md`,
		Args: func(cmd *cobra.Command, args []string) error {
			// --json-fields doesn't require an issue number
			if listFields, _ := cmd.Flags().GetBool("json-fields"); listFields {
				return nil
			}
			if len(args) < 1 {
				return fmt.Errorf("requires at least 1 arg(s), received %d", len(args))
			}
			return nil
		},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := requireJSONValue(cmd); err != nil {
				return err
			}
			return canonicalizeViewJSONFlag(cmd)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runView(cmd, args, opts)
		},
	}

	cmd.Flags().StringVar(&opts.jsonFields, "json", "", "Output JSON with specified fields (comma-separated). Use --json-fields to list available fields")
	cmd.Flags().Bool("json-fields", false, "List available JSON fields")
	cmd.Flags().StringVarP(&opts.jq, "jq", "q", "", "Filter JSON output using a jq expression")
	cmd.Flags().StringVarP(&opts.template, "template", "t", "", "Format output using a Go template (not supported; see error for alternatives)")
	cmd.Flags().BoolVarP(&opts.comments, "comments", "c", false, "Show issue comments")
	cmd.Flags().StringVarP(&opts.repo, "repo", "R", "", "Repository for the issue (owner/repo format)")
	cmd.Flags().BoolVarP(&opts.bodyFile, "body-file", "b", false, "Write issue body to tmp/issue-{number}.md")
	cmd.Flags().BoolVar(&opts.bodyStdout, "body-stdout", false, "Output issue body to stdout (raw markdown)")

	return cmd
}

func runView(cmd *cobra.Command, args []string, opts *viewOptions) error {
	// Handle --json-fields: list available fields (no issue number needed)
	if listFields, _ := cmd.Flags().GetBool("json-fields"); listFields {
		return listAvailableFields(cmd, viewAvailableFields)
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

	// Determine default repository (--repo flag takes precedence over config).
	// view tolerates empty defaults because issue args may be fully qualified;
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

	// Parse all issue references
	var refs []viewIssueRef
	var parseErrors []string

	for _, arg := range args {
		owner, repo, number, err := parseIssueReference(arg)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("%s: %v", arg, err))
			continue
		}
		if owner == "" || repo == "" {
			if defaultOwner == "" || defaultRepo == "" {
				return fmt.Errorf("no repository specified and none configured (use --repo or configure in .gh-pmu.json)")
			}
			owner = defaultOwner
			repo = defaultRepo
		}
		refs = append(refs, viewIssueRef{owner, repo, number})
	}

	// Create API client
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	// Resolve the configured project's node ID once so field-value fetches can
	// scope to this board only — an issue on multiple ProjectV2 boards would
	// otherwise merge foreign Status/Priority values (#856). Non-fatal: if the
	// project cannot be resolved, opts.projectID stays empty and the fetch falls
	// back to returning values from every board (prior behavior).
	if project, perr := client.GetProject(cfg.Project.Owner, cfg.Project.Number); perr == nil {
		opts.projectID = project.ID
	} else {
		fmt.Fprintf(os.Stderr, "Warning: could not resolve configured project; cross-project field filtering disabled: %v\n", perr)
	}

	// Single-issue path (backward compatible, unchanged behavior)
	if len(refs) == 1 && len(parseErrors) == 0 {
		return runViewWithDeps(cmd, opts, client, refs[0].owner, refs[0].repo, refs[0].number)
	}

	// Multi-issue path
	return runViewMulti(cmd, opts, client, refs, parseErrors)
}

// runViewMulti handles viewing multiple issues with batch API calls
func runViewMulti(cmd *cobra.Command, opts *viewOptions, client viewClient, refs []viewIssueRef, parseErrors []string) error {
	// Validate single-issue-only flags
	if opts.bodyFile {
		return fmt.Errorf("--body-file is only supported for single issue")
	}
	if opts.bodyStdout {
		return fmt.Errorf("--body-stdout is only supported for single issue")
	}
	if opts.template != "" {
		return fmt.Errorf("--template is not supported")
	}
	if opts.jq != "" && opts.jsonFields == "" {
		return fmt.Errorf("--jq requires --json")
	}
	if opts.jsonFields != "" && opts.comments {
		return fmt.Errorf("cannot use --json with --comments; use --json comments instead")
	}

	// Report parse errors to stderr
	for _, pe := range parseErrors {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", pe)
	}

	if len(refs) == 0 {
		return fmt.Errorf("no valid issue numbers provided")
	}

	// Group by repo (all issues typically in same repo). Result maps are keyed by
	// (owner, repo, number) — not number alone — so two same-numbered issues from
	// different repos do not collide (one overwriting the other's data).
	type repoKey struct{ owner, repo string }
	grouped := make(map[repoKey][]int)
	refOrder := make([]issueKey, 0, len(refs))
	for _, ref := range refs {
		key := repoKey{ref.owner, ref.repo}
		grouped[key] = append(grouped[key], ref.number)
		refOrder = append(refOrder, issueKey(ref))
	}

	// Issues, sub-issues, parents or comments that fail to load are reported
	// once the rest are printed (#938, #939).
	var loadFailures partialFailures

	// Fetch all data with batch calls
	allIssues := make(map[issueKey]*api.Issue)
	allFieldValues := make(map[issueKey][]api.FieldValue)
	allSubIssues := make(map[issueKey][]api.SubIssue)
	allParentIssues := make(map[issueKey]*api.Issue)
	allErrors := make(map[issueKey]error)

	for key, numbers := range grouped {
		// Call 1: Issues + project fields
		issues, fieldValues, issueErrors, err := client.GetIssuesWithProjectFieldsBatch(opts.projectID, key.owner, key.repo, numbers)
		if err != nil {
			return fmt.Errorf("failed to fetch issues: %w", err)
		}
		for num, issue := range issues {
			allIssues[issueKey{key.owner, key.repo, num}] = issue
		}
		for num, fvs := range fieldValues {
			allFieldValues[issueKey{key.owner, key.repo, num}] = fvs
		}
		for num, e := range issueErrors {
			allErrors[issueKey{key.owner, key.repo, num}] = e
		}

		// Collect successfully fetched issue numbers for sub-issue/parent batch
		var validNumbers []int
		for _, num := range numbers {
			if _, ok := allIssues[issueKey{key.owner, key.repo, num}]; ok {
				validNumbers = append(validNumbers, num)
			}
		}

		// Calls 2-3: Sub-issues + parent issues (parallel). A failed batch is
		// reported after the output rather than shown as "none" (#939).
		var subIssuesMap map[int][]api.SubIssue
		var parentIssuesMap map[int]*api.Issue
		var subErr, parentErr error
		var wg sync.WaitGroup

		wg.Add(2)
		go func() {
			defer wg.Done()
			subIssuesMap, subErr = client.GetSubIssuesBatch(key.owner, key.repo, validNumbers)
		}()
		go func() {
			defer wg.Done()
			parentIssuesMap, parentErr = client.GetParentIssueBatch(key.owner, key.repo, validNumbers)
		}()
		wg.Wait()
		if subErr != nil {
			loadFailures.add(fmt.Sprintf("fetch sub-issues for %s/%s", key.owner, key.repo), subErr)
		}
		if parentErr != nil {
			loadFailures.add(fmt.Sprintf("fetch parent issues for %s/%s", key.owner, key.repo), parentErr)
		}

		for num, subs := range subIssuesMap {
			allSubIssues[issueKey{key.owner, key.repo, num}] = subs
		}
		for num, parent := range parentIssuesMap {
			allParentIssues[issueKey{key.owner, key.repo, num}] = parent
		}
	}

	// Fetch comments per-issue if requested (sequential, opt-in)
	allComments := make(map[issueKey][]api.Comment)
	if opts.comments {
		for _, ref := range refs {
			k := issueKey(ref)
			if _, ok := allIssues[k]; ok {
				comments, err := client.GetIssueComments(ref.owner, ref.repo, ref.number)
				if err != nil {
					loadFailures.add(fmt.Sprintf("fetch comments for #%d", ref.number), err)
				}
				allComments[k] = comments
			}
		}
	}

	// Build results in argument order. Issues that fail to load are skipped and
	// reported once the rest are printed (#938).
	var results []viewResult
	for _, k := range refOrder {
		if e, ok := allErrors[k]; ok && e != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get issue #%d: %v\n", k.number, e)
			loadFailures.add(fmt.Sprintf("load #%d", k.number), e)
			continue
		}
		issue, ok := allIssues[k]
		if !ok {
			fmt.Fprintf(os.Stderr, "Warning: issue #%d not found\n", k.number)
			loadFailures.add(fmt.Sprintf("load #%d", k.number), fmt.Errorf("issue #%d not found", k.number))
			continue
		}
		results = append(results, viewResult{
			number:      k.number,
			issue:       issue,
			fieldValues: allFieldValues[k],
			subIssues:   allSubIssues[k],
			parentIssue: allParentIssues[k],
			comments:    allComments[k],
		})
	}

	if len(results) == 0 {
		return fmt.Errorf("all issues failed to load")
	}

	// Output
	var outErr error
	if opts.jsonFields != "" {
		outErr = outputViewMultiJSON(cmd, opts, results)
	} else {
		outErr = outputViewMultiTable(cmd, results)
	}
	if outErr != nil {
		return outErr
	}
	return loadFailures.err(fmt.Sprintf("showed %d of %d issues", len(results), len(refOrder)))
}

// outputViewMultiJSON outputs multiple issues as a JSON array
func outputViewMultiJSON(cmd *cobra.Command, opts *viewOptions, results []viewResult) error {
	requestedFields := parseJSONFields(opts.jsonFields, viewAvailableFields)

	var jsonArray []map[string]interface{}
	for _, r := range results {
		fullOutput := buildViewJSONOutput(r.issue, r.fieldValues, r.subIssues, r.parentIssue, r.comments)
		filteredOutput := filterViewJSONFields(fullOutput, requestedFields)
		jsonArray = append(jsonArray, filteredOutput)
	}

	jsonBytes, err := json.MarshalIndent(jsonArray, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	if opts.jq != "" {
		return applyJQFilter(jsonBytes, opts.jq)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, string(jsonBytes))
	return nil
}

// outputViewMultiTable outputs multiple issues sequentially with separators
func outputViewMultiTable(cmd *cobra.Command, results []viewResult) error {
	w := cmd.OutOrStdout()
	for i, r := range results {
		if i > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, strings.Repeat("\u2550", 60))
			fmt.Fprintln(w)
		}
		if err := outputViewTable(cmd, r.issue, r.fieldValues, r.subIssues, r.parentIssue, r.comments); err != nil {
			return err
		}
	}
	return nil
}

// runViewWithDeps is the testable implementation of runView
func runViewWithDeps(cmd *cobra.Command, opts *viewOptions, client viewClient, owner, repo string, number int) error {
	// Validate flag mutual exclusivity
	if opts.jsonFields != "" {
		if opts.bodyFile {
			return fmt.Errorf("cannot use --json with --body-file")
		}
		if opts.bodyStdout {
			return fmt.Errorf("cannot use --json with --body-stdout")
		}
		if opts.comments {
			return fmt.Errorf("cannot use --json with --comments; use --json comments instead")
		}
	}

	// Validate --jq requires --json
	if opts.jq != "" && opts.jsonFields == "" {
		return fmt.Errorf("--jq requires --json")
	}

	// --template is not supported; provide helpful error with alternatives
	if opts.template != "" {
		return fmt.Errorf("--template is not supported; use 'gh issue view %d --template' for standard fields, or use --jq for project fields", number)
	}

	// Fetch issue with project field values in a single query (optimized)
	issue, fieldValues, err := client.GetIssueWithProjectFields(opts.projectID, owner, repo, number)
	if err != nil {
		return fmt.Errorf("failed to get issue: %w", err)
	}

	// Handle --body-file flag: write body to tmp/issue-{number}.md
	if opts.bodyFile {
		return writeBodyToFile(cmd.OutOrStdout(), issue.Number, issue.Body)
	}

	// Handle --body-stdout flag: output body directly to stdout
	if opts.bodyStdout {
		fmt.Fprint(cmd.OutOrStdout(), issue.Body)
		return nil
	}

	// Fetch sub-issues and parent issue in parallel. A failed fetch is reported
	// after the output rather than shown as "none" (#939).
	var subIssues []api.SubIssue
	var parentIssue *api.Issue
	var subErr, parentErr error
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		subIssues, subErr = client.GetSubIssues(owner, repo, number)
	}()
	go func() {
		defer wg.Done()
		parentIssue, parentErr = client.GetParentIssue(owner, repo, number)
	}()
	wg.Wait()

	var fetchFailures partialFailures
	if subErr != nil {
		fetchFailures.add("fetch sub-issues", subErr)
	}
	if parentErr != nil {
		fetchFailures.add("fetch parent issue", parentErr)
	}

	// Fetch comments if requested (not parallelized - only when needed)
	var comments []api.Comment
	if opts.comments {
		var commentsErr error
		comments, commentsErr = client.GetIssueComments(owner, repo, number)
		if commentsErr != nil {
			fetchFailures.add("fetch comments", commentsErr)
		}
	}

	// Output
	var outErr error
	if opts.jsonFields != "" {
		outErr = outputViewJSON(cmd, opts, issue, fieldValues, subIssues, parentIssue, comments)
	} else {
		outErr = outputViewTable(cmd, issue, fieldValues, subIssues, parentIssue, comments)
	}
	if outErr != nil {
		return outErr
	}
	return fetchFailures.err(fmt.Sprintf("showed issue #%d", number))
}

// ViewJSONOutput represents the JSON output for view command
type ViewJSONOutput struct {
	Number      int               `json:"number"`
	Title       string            `json:"title"`
	State       string            `json:"state"`
	Body        string            `json:"body"`
	URL         string            `json:"url"`
	Author      string            `json:"author"`
	Assignees   []string          `json:"assignees"`
	Labels      []string          `json:"labels"`
	Milestone   string            `json:"milestone,omitempty"`
	FieldValues map[string]string `json:"fieldValues"`
	SubIssues   []SubIssueJSON    `json:"subIssues,omitempty"`
	SubProgress *SubProgressJSON  `json:"subProgress,omitempty"`
	ParentIssue *ParentIssueJSON  `json:"parentIssue,omitempty"`
	Comments    []CommentJSON     `json:"comments,omitempty"`
}

// CommentJSON represents a comment in JSON output
type CommentJSON struct {
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

// SubProgressJSON represents sub-issue progress in JSON output
type SubProgressJSON struct {
	Total      int `json:"total"`
	Completed  int `json:"completed"`
	Percentage int `json:"percentage"`
}

// SubIssueJSON represents a sub-issue in JSON output
type SubIssueJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	URL    string `json:"url"`
}

// ParentIssueJSON represents the parent issue in JSON output
type ParentIssueJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

func outputViewJSON(cmd *cobra.Command, opts *viewOptions, issue *api.Issue, fieldValues []api.FieldValue, subIssues []api.SubIssue, parentIssue *api.Issue, comments []api.Comment) error {
	// Build full output first
	fullOutput := buildViewJSONOutput(issue, fieldValues, subIssues, parentIssue, comments)

	// Parse requested fields
	requestedFields := parseJSONFields(opts.jsonFields, viewAvailableFields)

	// Filter output to only requested fields
	filteredOutput := filterViewJSONFields(fullOutput, requestedFields)

	// Encode to JSON
	jsonBytes, err := json.MarshalIndent(filteredOutput, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	// Apply jq filter if specified
	if opts.jq != "" {
		return applyJQFilter(jsonBytes, opts.jq)
	}

	// Output JSON
	fmt.Fprintln(cmd.OutOrStdout(), string(jsonBytes))
	return nil
}

// buildViewJSONOutput constructs the full JSON output structure
func buildViewJSONOutput(issue *api.Issue, fieldValues []api.FieldValue, subIssues []api.SubIssue, parentIssue *api.Issue, comments []api.Comment) ViewJSONOutput {
	output := ViewJSONOutput{
		Number:      issue.Number,
		Title:       issue.Title,
		State:       issue.State,
		Body:        issue.Body,
		URL:         issue.URL,
		Author:      issue.Author.Login,
		Assignees:   make([]string, 0),
		Labels:      make([]string, 0),
		FieldValues: make(map[string]string),
	}

	for _, a := range issue.Assignees {
		output.Assignees = append(output.Assignees, a.Login)
	}

	for _, l := range issue.Labels {
		output.Labels = append(output.Labels, l.Name)
	}

	if issue.Milestone != nil {
		output.Milestone = issue.Milestone.Title
	}

	for _, fv := range fieldValues {
		output.FieldValues[fv.Field] = fv.Value
	}

	if len(subIssues) > 0 {
		output.SubIssues = make([]SubIssueJSON, 0, len(subIssues))
		closedCount := 0
		for _, sub := range subIssues {
			output.SubIssues = append(output.SubIssues, SubIssueJSON{
				Number: sub.Number,
				Title:  sub.Title,
				State:  sub.State,
				URL:    sub.URL,
			})
			if sub.State == "CLOSED" {
				closedCount++
			}
		}

		// Add progress info
		total := len(subIssues)
		percentage := 0
		if total > 0 {
			percentage = (closedCount * 100) / total
		}
		output.SubProgress = &SubProgressJSON{
			Total:      total,
			Completed:  closedCount,
			Percentage: percentage,
		}
	}

	if parentIssue != nil {
		output.ParentIssue = &ParentIssueJSON{
			Number: parentIssue.Number,
			Title:  parentIssue.Title,
			URL:    parentIssue.URL,
		}
	}

	if len(comments) > 0 {
		output.Comments = make([]CommentJSON, 0, len(comments))
		for _, c := range comments {
			output.Comments = append(output.Comments, CommentJSON{
				Author:    c.Author,
				Body:      c.Body,
				CreatedAt: c.CreatedAt,
			})
		}
	}

	return output
}

// filterViewJSONFields returns a map with only the requested fields
func filterViewJSONFields(output ViewJSONOutput, fields []string) map[string]interface{} {
	result := make(map[string]interface{})

	for _, field := range fields {
		switch field {
		case "number":
			result["number"] = output.Number
		case "title":
			result["title"] = output.Title
		case "state":
			result["state"] = output.State
		case "body":
			result["body"] = output.Body
		case "url":
			result["url"] = output.URL
		case "author":
			result["author"] = output.Author
		case "assignees":
			result["assignees"] = output.Assignees
		case "labels":
			result["labels"] = output.Labels
		case "milestone":
			if output.Milestone != "" {
				result["milestone"] = output.Milestone
			}
		case "fieldValues":
			result["fieldValues"] = output.FieldValues
		case "status":
			// Extract status from fieldValues (project field)
			if val, ok := output.FieldValues["Status"]; ok && val != "" {
				result["status"] = val
			} else {
				result["status"] = nil
			}
		case "priority":
			// Extract priority from fieldValues (project field)
			if val, ok := output.FieldValues["Priority"]; ok && val != "" {
				result["priority"] = val
			} else {
				result["priority"] = nil
			}
		case "branch":
			// Extract branch from fieldValues (project field)
			if val, ok := output.FieldValues["Branch"]; ok && val != "" {
				result["branch"] = val
			} else {
				result["branch"] = nil
			}
		case "subIssues":
			if output.SubIssues != nil {
				result["subIssues"] = output.SubIssues
			}
		case "subProgress":
			if output.SubProgress != nil {
				result["subProgress"] = output.SubProgress
			}
		case "parentIssue":
			if output.ParentIssue != nil {
				result["parentIssue"] = output.ParentIssue
			}
		case "comments":
			if output.Comments != nil {
				result["comments"] = output.Comments
			}
		}
	}

	return result
}

func outputViewTable(cmd *cobra.Command, issue *api.Issue, fieldValues []api.FieldValue, subIssues []api.SubIssue, parentIssue *api.Issue, comments []api.Comment) error {
	w := cmd.OutOrStdout()

	// Title and state
	fmt.Fprintf(w, "%s #%d\n", issue.Title, issue.Number)
	fmt.Fprintf(w, "State: %s\n", issue.State)
	fmt.Fprintf(w, "URL: %s\n", issue.URL)
	fmt.Fprintln(w)

	// Author
	fmt.Fprintf(w, "Author: @%s\n", issue.Author.Login)

	// Assignees
	if len(issue.Assignees) > 0 {
		var assignees []string
		for _, a := range issue.Assignees {
			assignees = append(assignees, "@"+a.Login)
		}
		fmt.Fprintf(w, "Assignees: %s\n", strings.Join(assignees, ", "))
	}

	// Labels
	if len(issue.Labels) > 0 {
		var labels []string
		for _, l := range issue.Labels {
			labels = append(labels, l.Name)
		}
		fmt.Fprintf(w, "Labels: %s\n", strings.Join(labels, ", "))
	}

	// Milestone
	if issue.Milestone != nil {
		fmt.Fprintf(w, "Milestone: %s\n", issue.Milestone.Title)
	}

	// Project field values
	if len(fieldValues) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Project Fields:")
		for _, fv := range fieldValues {
			fmt.Fprintf(w, "  %s: %s\n", fv.Field, fv.Value)
		}
	}

	// Parent issue
	if parentIssue != nil {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Parent Issue: #%d - %s\n", parentIssue.Number, parentIssue.Title)
	}

	// Sub-issues with progress bar
	if len(subIssues) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Sub-Issues:")
		closedCount := 0
		for _, sub := range subIssues {
			state := "[ ]"
			if sub.State == "CLOSED" {
				state = "[x]"
				closedCount++
			}
			// Show repo info if cross-repo
			if sub.Repository.Owner != "" && sub.Repository.Name != "" {
				parentRepo := issue.Repository.Owner + "/" + issue.Repository.Name
				subRepo := sub.Repository.Owner + "/" + sub.Repository.Name
				if subRepo != parentRepo {
					fmt.Fprintf(w, "  %s %s#%d - %s\n", state, subRepo, sub.Number, sub.Title)
					continue
				}
			}
			fmt.Fprintf(w, "  %s #%d - %s\n", state, sub.Number, sub.Title)
		}

		// Progress bar and percentage
		total := len(subIssues)
		percentage := 0
		if total > 0 {
			percentage = (closedCount * 100) / total
		}
		progressBar := renderProgressBar(closedCount, total, 20)
		fmt.Fprintf(w, "\n%s %d of %d sub-issues complete (%d%%)\n", progressBar, closedCount, total, percentage)
	}

	// Body. Branch trackers created before the template fix carry a dead
	// "### Sub-Issues" placeholder section; strip it so the rendered view
	// doesn't show the redundant empty section after the top-block list.
	body := issue.Body
	if issueHasLabel(issue.Labels, "branch") {
		body = stripEmptySubIssuesSection(body)
	}
	if body != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "---")
		fmt.Fprintln(w, body)
	}

	// Comments
	if len(comments) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Comments (%d):\n", len(comments))
		for _, c := range comments {
			fmt.Fprintln(w)
			fmt.Fprintf(w, "@%s commented on %s:\n", c.Author, c.CreatedAt)
			fmt.Fprintln(w, c.Body)
		}
	}

	return nil
}

// stripEmptySubIssuesSection removes a trailing "### Sub-Issues" section whose
// body is only the branch-tracker template placeholder. Existing trackers
// created from .claude/commands/create-branch.md before the template fix
// carry this dead text; rendering it after the top-block sub-issue list reads
// as missing data. Bodies whose Sub-Issues section has real content are left
// unchanged.
func stripEmptySubIssuesSection(body string) string {
	const (
		heading     = "### Sub-Issues"
		placeholder = "Issues assigned to this branch appear as sub-issues below."
	)
	idx := strings.LastIndex(body, heading)
	if idx == -1 {
		return body
	}
	if strings.TrimSpace(body[idx+len(heading):]) != placeholder {
		return body
	}
	return strings.TrimRight(body[:idx], " \t\n")
}

// issueHasLabel reports whether the labels slice contains one with the given name.
func issueHasLabel(labels []api.Label, name string) bool {
	for _, l := range labels {
		if l.Name == name {
			return true
		}
	}
	return false
}

// renderProgressBar creates a visual progress bar
// Example: [████████░░░░░░░░░░░░] for 40% complete
func renderProgressBar(completed, total, width int) string {
	if total == 0 {
		return "[" + strings.Repeat("░", width) + "]"
	}

	filled := (completed * width) / total
	if filled > width {
		filled = width
	}

	empty := width - filled
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", empty) + "]"
}

// writeBodyToFile writes the issue body to tmp/issue-{number}.md
// Creates the tmp directory if it doesn't exist
func writeBodyToFile(w io.Writer, number int, body string) error {
	// Create tmp directory if it doesn't exist
	tmpDir := "tmp"
	if err := os.MkdirAll(tmpDir, 0750); err != nil {
		return fmt.Errorf("failed to create tmp directory: %w", err)
	}

	// Write body to file
	filename := fmt.Sprintf("issue-%d.md", number)
	filePath := filepath.Join(tmpDir, filename)

	if err := os.WriteFile(filePath, []byte(body), 0600); err != nil {
		return fmt.Errorf("failed to write body file: %w", err)
	}

	fmt.Fprintln(w, filePath)
	return nil
}

// parseIssueNumber parses a string into an issue number
// Accepts formats: "123" or "#123"
func parseIssueNumber(s string) (int, error) {
	// Strip leading # if present
	s = strings.TrimPrefix(s, "#")

	num, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid issue number: %s", s)
	}

	if num <= 0 {
		return 0, fmt.Errorf("issue number must be positive: %d", num)
	}

	return num, nil
}

// parseIssueReference parses an issue reference string
// Accepts formats: "123", "#123", "owner/repo#123", or full GitHub issue URL
// Returns owner, repo, number (owner/repo may be empty if not specified)
func parseIssueReference(s string) (owner, repo string, number int, err error) {
	// Check for GitHub URL format
	// Formats: https://github.com/owner/repo/issues/123
	//          https://github.com/owner/repo/issues/123#issuecomment-...
	if strings.HasPrefix(s, "https://github.com/") || strings.HasPrefix(s, "http://github.com/") {
		owner, repo, number, err = parseIssueURL(s)
		if err != nil {
			return "", "", 0, err
		}
		return owner, repo, number, nil
	}

	// Check for owner/repo#number format
	if idx := strings.Index(s, "#"); idx > 0 {
		// Has # with something before it - could be owner/repo#number
		repoRef := s[:idx]
		numStr := s[idx+1:]

		if slashIdx := strings.Index(repoRef, "/"); slashIdx > 0 {
			owner = repoRef[:slashIdx]
			repo = repoRef[slashIdx+1:]

			number, err = parseIssueNumber(numStr)
			if err != nil {
				return "", "", 0, err
			}
			return owner, repo, number, nil
		}
	}

	// Try parsing as simple number or #number
	number, err = parseIssueNumber(s)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid issue reference: %s", s)
	}

	return "", "", number, nil
}

// parseIssueURL parses a GitHub issue URL and extracts owner, repo, and number
// Supports formats:
//   - https://github.com/owner/repo/issues/123
//   - https://github.com/owner/repo/issues/123#issuecomment-...
func parseIssueURL(url string) (owner, repo string, number int, err error) {
	// Remove protocol prefix
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")

	// Remove github.com prefix
	if !strings.HasPrefix(url, "github.com/") {
		return "", "", 0, fmt.Errorf("invalid GitHub URL: not a github.com URL")
	}
	url = strings.TrimPrefix(url, "github.com/")

	// Split path parts: owner/repo/issues/number[#anchor]
	parts := strings.Split(url, "/")
	if len(parts) < 4 {
		return "", "", 0, fmt.Errorf("invalid GitHub issue URL format")
	}

	owner = parts[0]
	repo = parts[1]

	if parts[2] != "issues" {
		return "", "", 0, fmt.Errorf("URL is not an issue URL (expected /issues/)")
	}

	// Parse issue number (may have anchor suffix like #issuecomment-123)
	numStr := parts[3]
	if anchorIdx := strings.Index(numStr, "#"); anchorIdx > 0 {
		numStr = numStr[:anchorIdx]
	}

	number, err = parseIssueNumber(numStr)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid issue number in URL: %s", numStr)
	}

	return owner, repo, number, nil
}

// listAvailableFields outputs the list of available JSON fields
func listAvailableFields(cmd *cobra.Command, fields []string) error {
	sorted := make([]string, len(fields))
	copy(sorted, fields)
	sort.Strings(sorted)

	fmt.Fprintln(cmd.OutOrStdout(), "Specify one or more comma-separated fields for `--json`:")
	for _, field := range sorted {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", field)
	}
	return nil
}

// requireJSONValue rejects an explicitly empty --json=. list, view and branch
// current all take a required field list (#926); an empty one would otherwise
// fall through to text output and exit 0, the silent-ignore this guards.
func requireJSONValue(cmd *cobra.Command) error {
	flag := cmd.Flags().Lookup("json")
	if flag == nil || !flag.Changed || strings.TrimSpace(flag.Value.String()) != "" {
		return nil
	}
	return fmt.Errorf("--json requires a comma-separated field list; run '%s --json-fields' to list available fields", cmd.CommandPath())
}

// canonicalizeViewJSONFlag rejects an unknown --json field before any API call,
// rather than letting the output silently drop it (#941), and rewrites the flag
// to the canonical field names so mixed-case input selects the same fields.
func canonicalizeViewJSONFlag(cmd *cobra.Command) error {
	flag := cmd.Flags().Lookup("json")
	if flag == nil || !flag.Changed {
		return nil
	}
	input := flag.Value.String()
	var fields []string
	for _, p := range strings.Split(input, ",") {
		field := strings.TrimSpace(p)
		if field == "" {
			continue
		}
		canonical := ""
		for _, available := range viewAvailableFields {
			if strings.EqualFold(field, available) {
				canonical = available
				break
			}
		}
		if canonical == "" {
			return fmt.Errorf("unknown field %q in --json=%s. Run 'gh pmu view --json-fields' to see available fields", field, input)
		}
		fields = append(fields, canonical)
	}
	if len(fields) == 0 {
		return fmt.Errorf("no valid fields in --json=%s. Run 'gh pmu view --json-fields' to see available fields", input)
	}
	return cmd.Flags().Set("json", strings.Join(fields, ","))
}

// parseJSONFields parses comma-separated field names and returns a slice
// If input is empty or contains only whitespace, returns all available fields
func parseJSONFields(input string, available []string) []string {
	input = strings.TrimSpace(input)
	if input == "" || input == "_list_" {
		return available
	}

	parts := strings.Split(input, ",")
	var result []string
	for _, p := range parts {
		field := strings.TrimSpace(p)
		if field != "" {
			result = append(result, field)
		}
	}

	if len(result) == 0 {
		return available
	}
	return result
}

// applyJQFilter applies a jq expression to JSON input and outputs the result
func applyJQFilter(jsonBytes []byte, jqExpr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "jq", jqExpr)
	cmd.Stdin = strings.NewReader(string(jsonBytes))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
