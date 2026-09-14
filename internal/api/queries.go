package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"

	graphql "github.com/cli/shurcooL-graphql"
)

// safeGraphQLInt safely converts an int to graphql.Int with bounds checking.
// Returns an error if the value exceeds int32 range.
func safeGraphQLInt(n int) (graphql.Int, error) {
	if n > math.MaxInt32 || n < math.MinInt32 {
		return 0, fmt.Errorf("integer value %d exceeds int32 range", n)
	}
	return graphql.Int(n), nil
}

// GetProject fetches a project by owner and number.
//
// It tries the user-owned project first, then falls back to an org-owned
// project. If both fail, both errors are preserved (errors.Join) so a genuine
// user-path failure (e.g. a transient error on a user-owned project) is not
// masked by the org fallback's "Could not resolve to an Organization" (#861).
func (c *Client) GetProject(owner string, number int) (*Project, error) {

	// First try as user project
	project, userErr := c.getUserProject(owner, number)
	if userErr == nil {
		return project, nil
	}

	// If that fails, try as organization project
	project, orgErr := c.getOrgProject(owner, number)
	if orgErr != nil {
		return nil, fmt.Errorf("failed to get project %s/%d: %w", owner, number, errors.Join(userErr, orgErr))
	}

	return project, nil
}

func (c *Client) getUserProject(owner string, number int) (*Project, error) {
	var query struct {
		User struct {
			ProjectV2 struct {
				ID     string
				Number int
				Title  string
				URL    string `graphql:"url"`
				Closed bool
			} `graphql:"projectV2(number: $number)"`
		} `graphql:"user(login: $owner)"`
	}

	gqlNumber, err := safeGraphQLInt(number)
	if err != nil {
		return nil, err
	}

	variables := map[string]interface{}{
		"owner":  graphql.String(owner),
		"number": gqlNumber,
	}

	err = c.gql.Query("GetUserProject", &query, variables)
	if err != nil {
		return nil, err
	}

	return &Project{
		ID:     query.User.ProjectV2.ID,
		Number: query.User.ProjectV2.Number,
		Title:  query.User.ProjectV2.Title,
		URL:    query.User.ProjectV2.URL,
		Closed: query.User.ProjectV2.Closed,
		Owner: ProjectOwner{
			Type:  "User",
			Login: owner,
		},
	}, nil
}

func (c *Client) getOrgProject(owner string, number int) (*Project, error) {
	var query struct {
		Organization struct {
			ProjectV2 struct {
				ID     string
				Number int
				Title  string
				URL    string `graphql:"url"`
				Closed bool
			} `graphql:"projectV2(number: $number)"`
		} `graphql:"organization(login: $owner)"`
	}

	gqlNumber, err := safeGraphQLInt(number)
	if err != nil {
		return nil, err
	}

	variables := map[string]interface{}{
		"owner":  graphql.String(owner),
		"number": gqlNumber,
	}

	err = c.gql.Query("GetOrgProject", &query, variables)
	if err != nil {
		return nil, err
	}

	return &Project{
		ID:     query.Organization.ProjectV2.ID,
		Number: query.Organization.ProjectV2.Number,
		Title:  query.Organization.ProjectV2.Title,
		URL:    query.Organization.ProjectV2.URL,
		Closed: query.Organization.ProjectV2.Closed,
		Owner: ProjectOwner{
			Type:  "Organization",
			Login: owner,
		},
	}, nil
}

// GetProjectFields fetches all fields for a project.
// Uses cursor-based pagination to retrieve all fields regardless of project size.
//
// When GitHub's ProjectV2.fields resolver is unavailable (see
// ErrFieldResolverUnavailable / issue #853), falls back to cached field
// metadata from .gh-pmu.json so commands that need field name → option ID
// translation keep working. The fallback emits a single stderr warning per
// process and preserves the resolver sentinel through the error path when
// the cache itself is missing.
func (c *Client) GetProjectFields(projectID string) ([]ProjectField, error) {

	var allFields []ProjectField
	var cursor *string

	for {
		fields, pInfo, err := c.getProjectFieldsPage(projectID, cursor)
		if err != nil {
			if errors.Is(err, ErrFieldResolverUnavailable) && !fallbackOptions.NoCacheFallback {
				cached, fallbackErr := loadCachedProjectFieldsOrError(err)
				if fallbackErr != nil {
					return nil, fallbackErr
				}
				if fallbackOptions.RefreshCachedFields {
					fetcher := projectFieldByNameFetcher
					if fetcher == nil {
						fetcher = c.fetchProjectFieldByName
					}
					refreshed, ok, fail := refreshCachedFields(fetcher, projectID, cached)
					if fail > 0 {
						fmt.Fprintf(os.Stderr, "warning: refreshed %d/%d cached fields via field(name:); %d failed (cached values retained).\n",
							ok, ok+fail, fail)
					}
					cached = refreshed
				}
				return cached, nil
			}
			return nil, err
		}

		allFields = append(allFields, fields...)

		if !pInfo.HasNextPage {
			break
		}
		cursor = &pInfo.EndCursor
	}

	return allFields, nil
}

// getProjectFieldsPage fetches a single page of project fields
func (c *Client) getProjectFieldsPage(projectID string, cursor *string) ([]ProjectField, pageInfo, error) {
	var query struct {
		Node struct {
			ProjectV2 struct {
				Fields struct {
					Nodes []struct {
						TypeName string `graphql:"__typename"`
						// Common fields
						ProjectV2Field struct {
							ID       string
							Name     string
							DataType string
						} `graphql:"... on ProjectV2Field"`
						// Single select fields have options
						ProjectV2SingleSelectField struct {
							ID       string
							Name     string
							DataType string
							Options  []struct {
								ID          string
								Name        string
								Color       string
								Description string
							}
						} `graphql:"... on ProjectV2SingleSelectField"`
					}
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
				} `graphql:"fields(first: 100, after: $cursor)"`
			} `graphql:"... on ProjectV2"`
		} `graphql:"node(id: $projectId)"`
	}

	variables := map[string]interface{}{
		"projectId": graphql.ID(projectID),
		"cursor":    (*graphql.String)(nil),
	}
	if cursor != nil {
		variables["cursor"] = graphql.String(*cursor)
	}

	err := c.gql.Query("GetProjectFields", &query, variables)
	if err != nil {
		// When GitHub's ProjectV2.fields resolver is crashing (e.g., the
		// 2026-05-14 timestamp-fields auto-rollout), wrap the original error
		// with ErrFieldResolverUnavailable so upstream callers can detect it
		// via errors.Is and engage the cached-metadata fallback. The original
		// error chain (including any trace IDs) is preserved so users still
		// see the underlying detail.
		if IsFieldResolverUnavailable(err) {
			err = errors.Join(ErrFieldResolverUnavailable, err)
		}
		return nil, pageInfo{}, fmt.Errorf("failed to get project fields: %w", err)
	}

	var fields []ProjectField
	for _, node := range query.Node.ProjectV2.Fields.Nodes {
		field := ProjectField{}

		switch node.TypeName {
		case "ProjectV2SingleSelectField":
			field.ID = node.ProjectV2SingleSelectField.ID
			field.Name = node.ProjectV2SingleSelectField.Name
			field.DataType = node.ProjectV2SingleSelectField.DataType
			for _, opt := range node.ProjectV2SingleSelectField.Options {
				field.Options = append(field.Options, FieldOption{
					ID:          opt.ID,
					Name:        opt.Name,
					Color:       opt.Color,
					Description: opt.Description,
				})
			}
		case "ProjectV2Field":
			field.ID = node.ProjectV2Field.ID
			field.Name = node.ProjectV2Field.Name
			field.DataType = node.ProjectV2Field.DataType
		default:
			// Skip iteration/other field types for now
			continue
		}

		fields = append(fields, field)
	}

	return fields, pageInfo{
		HasNextPage: query.Node.ProjectV2.Fields.PageInfo.HasNextPage,
		EndCursor:   query.Node.ProjectV2.Fields.PageInfo.EndCursor,
	}, nil
}

// fetchProjectFieldByName resolves a single ProjectV2 field by name.
//
// This path uses ProjectV2.field(name: $name) rather than the
// fields-connection iterator, which is the resolver that survives the
// 2026-05-14 timestamp-fields rollout (see #853). Used by the Layer 3
// refresh path when fallbackOptions.RefreshCachedFields is set.
//
// Returns (nil, nil) if the field is not found (so the refresh path can
// distinguish "missing" from "fetch failed"). Returns an error for any
// transport-level failure or partial response.
func (c *Client) fetchProjectFieldByName(projectID, name string) (*ProjectField, error) {
	var query struct {
		Node struct {
			ProjectV2 struct {
				Field struct {
					TypeName       string `graphql:"__typename"`
					ProjectV2Field struct {
						ID       string
						Name     string
						DataType string
					} `graphql:"... on ProjectV2Field"`
					ProjectV2SingleSelectField struct {
						ID       string
						Name     string
						DataType string
						Options  []struct {
							ID          string
							Name        string
							Color       string
							Description string
						}
					} `graphql:"... on ProjectV2SingleSelectField"`
				} `graphql:"field(name: $name)"`
			} `graphql:"... on ProjectV2"`
		} `graphql:"node(id: $projectId)"`
	}

	variables := map[string]interface{}{
		"projectId": graphql.ID(projectID),
		"name":      graphql.String(name),
	}

	if err := c.gql.Query("GetProjectFieldByName", &query, variables); err != nil {
		return nil, fmt.Errorf("failed to fetch field %q: %w", name, err)
	}

	node := query.Node.ProjectV2.Field
	field := ProjectField{}
	switch node.TypeName {
	case "ProjectV2SingleSelectField":
		field.ID = node.ProjectV2SingleSelectField.ID
		field.Name = node.ProjectV2SingleSelectField.Name
		field.DataType = node.ProjectV2SingleSelectField.DataType
		for _, opt := range node.ProjectV2SingleSelectField.Options {
			field.Options = append(field.Options, FieldOption{ID: opt.ID, Name: opt.Name, Color: opt.Color, Description: opt.Description})
		}
	case "ProjectV2Field":
		field.ID = node.ProjectV2Field.ID
		field.Name = node.ProjectV2Field.Name
		field.DataType = node.ProjectV2Field.DataType
	default:
		// Field not found, or unsupported type (e.g., iteration field).
		// Return nil so the refresh path retains the cached entry untouched.
		return nil, nil
	}
	return &field, nil
}

// FetchFieldsByNames issues one ProjectV2.field(name:) lookup per name and
// returns the fields it could resolve plus the names it couldn't. Used by
// gh pmu init as a fallback when the bulk fields-connection fetch crashes
// (see #853 / ErrFieldResolverUnavailable).
//
// The per-name resolver path is on a different code path from the bulk
// fields connection — verified live to keep working when the bulk path
// crashes from the timestamp-fields rollout. Errors against individual
// names are absorbed into the `missing` return value so a partial recovery
// can still surface to the caller.
func (c *Client) FetchFieldsByNames(projectID string, names []string) (fields []ProjectField, missing []string) {
	for _, name := range names {
		f, err := c.fetchProjectFieldByName(projectID, name)
		if err != nil || f == nil {
			missing = append(missing, name)
			continue
		}
		fields = append(fields, *f)
	}
	return fields, missing
}

// GetIssue fetches an issue by repository and number
func (c *Client) GetIssue(owner, repo string, number int) (*Issue, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}

	var query struct {
		Repository struct {
			Issue struct {
				ID     string
				Number int
				Title  string
				Body   string
				State  string
				URL    string `graphql:"url"`
				Author struct {
					Login string
				}
				Assignees struct {
					Nodes []struct {
						Login string
					}
				} `graphql:"assignees(first: 10)"`
				Labels struct {
					Nodes []struct {
						Name  string
						Color string
					}
				} `graphql:"labels(first: 20)"`
				Milestone struct {
					Title string
				}
			} `graphql:"issue(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	gqlNumber, err := safeGraphQLInt(number)
	if err != nil {
		return nil, err
	}

	variables := map[string]interface{}{
		"owner":  graphql.String(owner),
		"repo":   graphql.String(repo),
		"number": gqlNumber,
	}

	err = c.gql.Query("GetIssue", &query, variables)
	if err != nil {
		return nil, fmt.Errorf("failed to get issue %s/%s#%d: %w", owner, repo, number, err)
	}

	issue := &Issue{
		ID:     query.Repository.Issue.ID,
		Number: query.Repository.Issue.Number,
		Title:  query.Repository.Issue.Title,
		Body:   query.Repository.Issue.Body,
		State:  query.Repository.Issue.State,
		URL:    query.Repository.Issue.URL,
		Repository: Repository{
			Owner: owner,
			Name:  repo,
		},
		Author: Actor{Login: query.Repository.Issue.Author.Login},
	}

	for _, a := range query.Repository.Issue.Assignees.Nodes {
		issue.Assignees = append(issue.Assignees, Actor{Login: a.Login})
	}

	for _, l := range query.Repository.Issue.Labels.Nodes {
		issue.Labels = append(issue.Labels, Label{Name: l.Name, Color: l.Color})
	}

	if query.Repository.Issue.Milestone.Title != "" {
		issue.Milestone = &Milestone{Title: query.Repository.Issue.Milestone.Title}
	}

	return issue, nil
}

// GetIssueWithProjectFields fetches an issue and its project field values in a single query.
// This is more efficient than calling GetIssue + GetProjectItems when you only need one issue.
//
// projectID scopes the returned field values to a single project board: an issue
// can belong to several ProjectV2 boards (each typically defining its own Status
// field), so only project items whose project ID matches projectID contribute
// field values. An empty projectID disables filtering and returns values from all
// boards (backward-compatible behavior for callers that cannot resolve a project).
//
// Only the first 10 project items are inspected; if the issue belongs to more
// boards a truncation warning is emitted to stderr, since the configured
// project's item could lie beyond the fetched page (#860).
func (c *Client) GetIssueWithProjectFields(projectID, owner, repo string, number int) (*Issue, []FieldValue, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, nil, err
	}

	var query struct {
		Repository struct {
			Issue struct {
				ID     string
				Number int
				Title  string
				Body   string
				State  string
				URL    string `graphql:"url"`
				Author struct {
					Login string
				}
				Assignees struct {
					Nodes []struct {
						Login string
					}
				} `graphql:"assignees(first: 10)"`
				Labels struct {
					Nodes []struct {
						Name  string
						Color string
					}
				} `graphql:"labels(first: 20)"`
				Milestone struct {
					Title string
				}
				ProjectItems struct {
					Nodes []struct {
						Project struct {
							ID string
						} `graphql:"project"`
						FieldValues struct {
							Nodes []typedFieldValueNode
						} `graphql:"fieldValues(first: 20)"`
					}
					PageInfo struct {
						HasNextPage bool
					}
				} `graphql:"projectItems(first: 10)"`
			} `graphql:"issue(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	gqlNumber, err := safeGraphQLInt(number)
	if err != nil {
		return nil, nil, err
	}

	variables := map[string]interface{}{
		"owner":  graphql.String(owner),
		"repo":   graphql.String(repo),
		"number": gqlNumber,
	}

	err = c.gql.Query("GetIssueWithProjectFields", &query, variables)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get issue %s/%s#%d: %w", owner, repo, number, err)
	}

	if query.Repository.Issue.ProjectItems.PageInfo.HasNextPage {
		fmt.Fprintf(os.Stderr, "Warning: issue #%d belongs to more than 10 projects; only the first 10 were read, so its field values may be incomplete.\n", number)
	}

	issue := &Issue{
		ID:     query.Repository.Issue.ID,
		Number: query.Repository.Issue.Number,
		Title:  query.Repository.Issue.Title,
		Body:   query.Repository.Issue.Body,
		State:  query.Repository.Issue.State,
		URL:    query.Repository.Issue.URL,
		Repository: Repository{
			Owner: owner,
			Name:  repo,
		},
		Author: Actor{Login: query.Repository.Issue.Author.Login},
	}

	for _, a := range query.Repository.Issue.Assignees.Nodes {
		issue.Assignees = append(issue.Assignees, Actor{Login: a.Login})
	}

	for _, l := range query.Repository.Issue.Labels.Nodes {
		issue.Labels = append(issue.Labels, Label{Name: l.Name, Color: l.Color})
	}

	if query.Repository.Issue.Milestone.Title != "" {
		issue.Milestone = &Milestone{Title: query.Repository.Issue.Milestone.Title}
	}

	// Extract field values from project items, scoped to the configured project.
	// An issue on multiple boards would otherwise merge foreign Status/Priority
	// values (#856). An empty projectID disables the filter.
	var fieldValues []FieldValue
	for _, projectItem := range query.Repository.Issue.ProjectItems.Nodes {
		if projectID != "" && projectItem.Project.ID != projectID {
			continue
		}
		fieldValues = appendTypedFieldValues(fieldValues, projectItem.FieldValues.Nodes)
	}

	return issue, fieldValues, nil
}

// GetProjectItemIDForIssue looks up the project item ID for a specific issue in a project.
// This is more efficient than fetching all project items when you only need one.
//
// It inspects only the first 20 project items on the issue (issues rarely belong
// to more than a handful of projects). If the target project is not among those
// 20 and more exist, a truncation warning is emitted to stderr so a false
// "not in the project" result is visible rather than silent (#860).
func (c *Client) GetProjectItemIDForIssue(projectID, owner, repo string, number int) (string, error) {

	var query struct {
		Repository struct {
			Issue struct {
				ProjectItems struct {
					Nodes []struct {
						ID      string
						Project struct {
							ID string
						}
					}
					PageInfo struct {
						HasNextPage bool
					}
				} `graphql:"projectItems(first: 20)"`
			} `graphql:"issue(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	gqlNumber, err := safeGraphQLInt(number)
	if err != nil {
		return "", err
	}

	variables := map[string]interface{}{
		"owner":  graphql.String(owner),
		"repo":   graphql.String(repo),
		"number": gqlNumber,
	}

	err = c.gql.Query("GetProjectItemIDForIssue", &query, variables)
	if err != nil {
		return "", fmt.Errorf("failed to get project item for issue %s/%s#%d: %w", owner, repo, number, err)
	}

	// Find the project item in the target project
	for _, item := range query.Repository.Issue.ProjectItems.Nodes {
		if item.Project.ID == projectID {
			return item.ID, nil
		}
	}

	if query.Repository.Issue.ProjectItems.PageInfo.HasNextPage {
		fmt.Fprintf(os.Stderr, "Warning: issue #%d belongs to more than 20 projects; only the first 20 were checked, so \"not in the project\" may be inaccurate.\n", number)
	}

	return "", fmt.Errorf("issue #%d is not in the project", number)
}

// ProjectItemsFilter allows filtering project items
type ProjectItemsFilter struct {
	Repository string  // Filter by repository (owner/repo format)
	State      *string // Filter by issue state: "OPEN", "CLOSED", or nil for all
	// Limit is the maximum number of items to return. 0 is not unlimited: it
	// applies GetProjectItems' default cap of 10000, above which items are still
	// truncated (#860).
	Limit int
}

// GetProjectItems fetches all items from a project with their field values.
// Uses cursor-based pagination to retrieve all items regardless of project size.
// If filter.Limit > 0, pagination terminates early once the limit is reached.
func (c *Client) GetProjectItems(projectID string, filter *ProjectItemsFilter) ([]ProjectItem, error) {

	const defaultMaxItems = 10000
	limit := 0
	if filter != nil {
		limit = filter.Limit
	}
	if limit == 0 {
		limit = defaultMaxItems
	}

	return collectPages(
		func(cursor *string) ([]ProjectItem, pageInfo, error) {
			return c.getProjectItemsPage(projectID, cursor)
		},
		func(item ProjectItem) bool {
			// Apply repository filter if specified
			if filter != nil && filter.Repository != "" {
				if item.Issue != nil && item.Issue.Repository.Owner != "" {
					repoName := item.Issue.Repository.Owner + "/" + item.Issue.Repository.Name
					if repoName != filter.Repository {
						return false
					}
				}
			}

			// Apply state filter if specified
			if filter != nil && filter.State != nil {
				if item.Issue == nil || item.Issue.State != *filter.State {
					return false
				}
			}

			return true
		},
		limit,
	)
}

// backlogViewName is the view name resolution looks for. Matching is
// whitespace-trimmed and case-insensitive, but it is equality — "Backlog
// Archive" is a different view, not a Backlog.
const backlogViewName = "backlog"

// boardLayout is the ProjectV2ViewLayout value identifying a board view.
//
// ProjectV2ViewLayout is a closed three-value enum (BOARD_LAYOUT,
// TABLE_LAYOUT, ROADMAP_LAYOUT), none deprecated, so comparing the decoded
// string against a literal is safe. A future fourth value simply fails to
// match rather than selecting the wrong view.
const boardLayout = "BOARD_LAYOUT"

// isBacklogBoardView reports whether v is the Backlog board.
//
// Layout is checked before name deliberately. A project can hold several views
// named "Backlog" in different layouts; filtering on position alone would pick
// whichever came first, which may be the table.
func isBacklogBoardView(v ProjectView) bool {
	return v.Layout == boardLayout &&
		strings.EqualFold(strings.TrimSpace(v.Name), backlogViewName)
}

// ResolveBacklogViewNumber returns the number of the project's first Backlog
// view with a BOARD_LAYOUT layout, in POSITION order — the left-to-right tab
// order the user sees.
//
// The three ways this can come up empty are deliberately kept apart (#901):
//
//	no such owner    -> ErrOwnerNotFound
//	no such project  -> ErrProjectNotFound
//	no Backlog view  -> (0, false, nil)
//
// Collapsing them would make a typo'd owner login read as a missing view and
// send the user looking in the wrong place. The third is not an error at all:
// GitHub has no createProjectV2View mutation, so a board without a Backlog
// view is a fact to report, never something to repair.
//
// A single query serves both personal and org-owned boards via repositoryOwner
// -> ... on ProjectV2Owner, which User and Organization both implement. This
// differs from GetProject's try-user-then-org pattern on purpose: that pattern
// joins the two failures with errors.Join, and a joined error cannot tell a
// missing owner from a missing project — the misattribution fixed in #861.
func (c *Client) ResolveBacklogViewNumber(owner string, number int) (int, bool, error) {
	matches, err := collectPages(
		func(cursor *string) ([]ProjectView, pageInfo, error) {
			return c.getProjectViewsPage(owner, number, cursor)
		},
		isBacklogBoardView,
		1, // stop at the first match — later pages cannot beat it in POSITION order
	)
	if err != nil {
		return 0, false, err
	}
	if len(matches) == 0 {
		return 0, false, nil
	}
	return matches[0].Number, true, nil
}

// getProjectViewsPage fetches a single page of a project's views.
//
// orderBy is passed explicitly even though the schema already defaults it to
// {field: POSITION, direction: ASC}. Relying on a server-side default means a
// change to it silently reorders our results; stating the intent keeps the
// "first match wins" contract in ResolveBacklogViewNumber honest.
func (c *Client) getProjectViewsPage(owner string, number int, cursor *string) ([]ProjectView, pageInfo, error) {
	var query struct {
		RepositoryOwner struct {
			TypeName       string `graphql:"__typename"`
			ProjectV2Owner struct {
				ProjectV2 struct {
					ID    string
					Views struct {
						Nodes []struct {
							ID     string
							Number int
							Name   string
							Layout string
						}
						PageInfo struct {
							HasNextPage bool
							EndCursor   string
						}
						// first: 50 is a literal because shurcooL-graphql reads the page
						// size out of this struct tag — it cannot be a constant. GitHub caps
						// connection pages at 100; 50 keeps responses small and the cursor
						// loop below still runs, because a second page is rare, not absent (#860).
					} `graphql:"views(first: 50, after: $cursor, orderBy: {field: POSITION, direction: ASC})"`
				} `graphql:"projectV2(number: $number)"`
			} `graphql:"... on ProjectV2Owner"`
		} `graphql:"repositoryOwner(login: $owner)"`
	}

	if err := validateOwner(owner); err != nil {
		return nil, pageInfo{}, err
	}

	gqlNumber, err := safeGraphQLInt(number)
	if err != nil {
		return nil, pageInfo{}, err
	}

	// owner and number travel as variables, never interpolated into the
	// document text (#832, #835).
	variables := map[string]interface{}{
		"owner":  graphql.String(owner),
		"number": gqlNumber,
		"cursor": (*graphql.String)(nil),
	}
	if cursor != nil {
		variables["cursor"] = graphql.String(*cursor)
	}

	if err := c.gql.Query("GetProjectViews", &query, variables); err != nil {
		// Propagate rather than treating a failed request as an empty view
		// list — that would report "no Backlog view" for a transient 502 or a
		// token missing read:project.
		return nil, pageInfo{}, fmt.Errorf("failed to fetch views for project %s/%d: %w", owner, number, err)
	}

	// A null repositoryOwner leaves __typename empty; a null projectV2 leaves
	// its non-null ID empty. Checking owner first matters — when the login does
	// not resolve, the project is absent too, and reporting that as a missing
	// project would point at the wrong argument.
	if query.RepositoryOwner.TypeName == "" {
		return nil, pageInfo{}, fmt.Errorf("%w: no user or organization named %q", ErrOwnerNotFound, owner)
	}
	project := query.RepositoryOwner.ProjectV2Owner.ProjectV2
	if project.ID == "" {
		return nil, pageInfo{}, fmt.Errorf("%w: %s has no project number %d", ErrProjectNotFound, owner, number)
	}

	views := make([]ProjectView, 0, len(project.Views.Nodes))
	for _, n := range project.Views.Nodes {
		views = append(views, ProjectView{
			ID:     n.ID,
			Number: n.Number,
			Name:   n.Name,
			Layout: n.Layout,
		})
	}

	return views, pageInfo{
		HasNextPage: project.Views.PageInfo.HasNextPage,
		EndCursor:   project.Views.PageInfo.EndCursor,
	}, nil
}

// pageInfo holds pagination information from GraphQL responses
type pageInfo struct {
	HasNextPage bool
	EndCursor   string
}

// getProjectItemsPage fetches a single page of project items
func (c *Client) getProjectItemsPage(projectID string, cursor *string) ([]ProjectItem, pageInfo, error) {
	var query struct {
		Node struct {
			ProjectV2 struct {
				Items struct {
					Nodes []struct {
						ID      string
						Content struct {
							TypeName string `graphql:"__typename"`
							Issue    struct {
								ID         string
								Number     int
								Title      string
								Body       string
								State      string
								URL        string `graphql:"url"`
								Repository struct {
									NameWithOwner string
								}
								Assignees struct {
									Nodes []struct {
										Login string
									}
								} `graphql:"assignees(first: 10)"`
								Labels struct {
									Nodes []struct {
										Name string
									}
								} `graphql:"labels(first: 20)"`
							} `graphql:"... on Issue"`
						}
						FieldValues struct {
							Nodes []typedFieldValueNode
						} `graphql:"fieldValues(first: 20)"`
					}
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
				} `graphql:"items(first: 100, after: $cursor)"`
			} `graphql:"... on ProjectV2"`
		} `graphql:"node(id: $projectId)"`
	}

	variables := map[string]interface{}{
		"projectId": graphql.ID(projectID),
		"cursor":    (*graphql.String)(nil),
	}
	if cursor != nil {
		variables["cursor"] = graphql.String(*cursor)
	}

	err := c.gql.Query("GetProjectItems", &query, variables)
	if err != nil {
		return nil, pageInfo{}, fmt.Errorf("failed to get project items: %w", err)
	}

	var items []ProjectItem
	for _, node := range query.Node.ProjectV2.Items.Nodes {
		// Skip non-issue items (like draft issues or PRs)
		if node.Content.TypeName != "Issue" {
			continue
		}

		item := ProjectItem{
			ID: node.ID,
			Issue: &Issue{
				ID:     node.Content.Issue.ID,
				Number: node.Content.Issue.Number,
				Title:  node.Content.Issue.Title,
				Body:   node.Content.Issue.Body,
				State:  node.Content.Issue.State,
				URL:    node.Content.Issue.URL,
			},
		}

		// Parse repository
		if node.Content.Issue.Repository.NameWithOwner != "" {
			parts := splitRepoName(node.Content.Issue.Repository.NameWithOwner)
			if len(parts) == 2 {
				item.Issue.Repository = Repository{
					Owner: parts[0],
					Name:  parts[1],
				}
			}
		}

		// Parse assignees
		for _, a := range node.Content.Issue.Assignees.Nodes {
			item.Issue.Assignees = append(item.Issue.Assignees, Actor{Login: a.Login})
		}

		// Parse labels
		for _, l := range node.Content.Issue.Labels.Nodes {
			item.Issue.Labels = append(item.Issue.Labels, Label{Name: l.Name})
		}

		// Parse field values
		item.FieldValues = appendTypedFieldValues(item.FieldValues, node.FieldValues.Nodes)

		items = append(items, item)
	}

	return items, pageInfo{
		HasNextPage: query.Node.ProjectV2.Items.PageInfo.HasNextPage,
		EndCursor:   query.Node.ProjectV2.Items.PageInfo.EndCursor,
	}, nil
}

// splitRepoName splits "owner/repo" into parts
func splitRepoName(nameWithOwner string) []string {
	for i, c := range nameWithOwner {
		if c == '/' {
			return []string{nameWithOwner[:i], nameWithOwner[i+1:]}
		}
	}
	return nil
}

// GetProjectItemsMinimal fetches project items with minimal issue data.
// This is optimized for filtering by field values (like Branch) without fetching
// full issue details (Body, Title, Assignees, Labels) which can be large.
// Use this for two-phase queries: first filter with minimal data, then fetch full details
// for matching items only.
func (c *Client) GetProjectItemsMinimal(projectID string, filter *ProjectItemsFilter) ([]MinimalProjectItem, error) {

	return collectPages(
		func(cursor *string) ([]MinimalProjectItem, pageInfo, error) {
			return c.getMinimalProjectItemsPage(projectID, cursor)
		},
		func(item MinimalProjectItem) bool {
			// Apply repository filter if specified
			if filter != nil && filter.Repository != "" {
				if item.Repository != filter.Repository {
					return false
				}
			}

			// Apply state filter if specified
			if filter != nil && filter.State != nil {
				if item.IssueState != *filter.State {
					return false
				}
			}

			return true
		},
		0,
	)
}

// getMinimalProjectItemsPage fetches a single page of project items with minimal data
func (c *Client) getMinimalProjectItemsPage(projectID string, cursor *string) ([]MinimalProjectItem, pageInfo, error) {
	var query struct {
		Node struct {
			ProjectV2 struct {
				Items struct {
					Nodes []struct {
						Content struct {
							TypeName string `graphql:"__typename"`
							Issue    struct {
								ID         string
								Number     int
								State      string
								Repository struct {
									NameWithOwner string
								}
							} `graphql:"... on Issue"`
						}
						FieldValues struct {
							Nodes []typedFieldValueNode
						} `graphql:"fieldValues(first: 20)"`
					}
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
				} `graphql:"items(first: 100, after: $cursor)"`
			} `graphql:"... on ProjectV2"`
		} `graphql:"node(id: $projectId)"`
	}

	variables := map[string]interface{}{
		"projectId": graphql.ID(projectID),
		"cursor":    (*graphql.String)(nil),
	}
	if cursor != nil {
		variables["cursor"] = graphql.String(*cursor)
	}

	err := c.gql.Query("GetProjectItemsMinimal", &query, variables)
	if err != nil {
		return nil, pageInfo{}, fmt.Errorf("failed to get minimal project items: %w", err)
	}

	var items []MinimalProjectItem
	for _, node := range query.Node.ProjectV2.Items.Nodes {
		// Skip non-issue items
		if node.Content.TypeName != "Issue" {
			continue
		}

		item := MinimalProjectItem{
			IssueID:     node.Content.Issue.ID,
			IssueNumber: node.Content.Issue.Number,
			IssueState:  node.Content.Issue.State,
			Repository:  node.Content.Issue.Repository.NameWithOwner,
		}

		// Parse field values
		item.FieldValues = appendTypedFieldValues(item.FieldValues, node.FieldValues.Nodes)

		items = append(items, item)
	}

	return items, pageInfo{
		HasNextPage: query.Node.ProjectV2.Items.PageInfo.HasNextPage,
		EndCursor:   query.Node.ProjectV2.Items.PageInfo.EndCursor,
	}, nil
}

// doRawGraphQL executes a raw GraphQL query via the go-gh HTTP client.
// Returns the full JSON response bytes (including {"data": ...} wrapper).
// Extra headers are merged into the request (e.g. feature previews).
func (c *Client) doRawGraphQL(query string, extraHeaders map[string]string) ([]byte, error) {
	if c.rawGQL == nil {
		return nil, fmt.Errorf("raw GraphQL client not initialized")
	}
	return c.rawGQL.DoRaw(query, extraHeaders)
}

// doRawGraphQLBody sends a pre-built JSON request body via the go-gh HTTP client.
// Use for mutations with variables where the body is already marshalled.
func (c *Client) doRawGraphQLBody(body []byte, extraHeaders map[string]string) ([]byte, error) {
	if c.rawGQL == nil {
		return nil, fmt.Errorf("raw GraphQL client not initialized")
	}
	return c.rawGQL.DoRawBody(body, extraHeaders)
}

// GetProjectItemsByIssues fetches project items for specific issues using a targeted query.
// This is more efficient than GetProjectItems when you know which issues you need,
// as it only fetches the specified issues rather than the entire project.
// Returns items only for issues that exist in the specified project.
func (c *Client) GetProjectItemsByIssues(projectID string, refs []IssueRef) ([]ProjectItem, error) {
	if len(refs) == 0 {
		return []ProjectItem{}, nil
	}

	if err := validateNodeID(projectID); err != nil {
		return nil, fmt.Errorf("projectID: %w", err)
	}
	for _, ref := range refs {
		if err := validateOwnerRepo(ref.Owner, ref.Repo); err != nil {
			return nil, err
		}
	}

	// Group issues by repository for efficient querying
	type repoKey struct {
		owner string
		repo  string
	}
	repoIssues := make(map[repoKey][]int)
	for _, ref := range refs {
		key := repoKey{owner: ref.Owner, repo: ref.Repo}
		repoIssues[key] = append(repoIssues[key], ref.Number)
	}

	// Build GraphQL query with aliases for each repository and issue
	var queryParts []string
	aliasMap := make(map[string]IssueRef) // alias -> IssueRef for result mapping

	repoIdx := 0
	for key, numbers := range repoIssues {
		var issueParts []string
		for issueIdx, num := range numbers {
			alias := fmt.Sprintf("i%d_%d", repoIdx, issueIdx)
			aliasMap[alias] = IssueRef{Owner: key.owner, Repo: key.repo, Number: num}
			issueParts = append(issueParts, fmt.Sprintf(`%s: issue(number: %d) {
				id
				number
				title
				body
				state
				url
				repository { nameWithOwner }
				assignees(first: 10) { nodes { login } }
				labels(first: 20) { nodes { name } }
				projectItems(first: 20) {
					nodes {
						id
						project { id }
						fieldValues(first: 20) {
							nodes {
								__typename
								... on ProjectV2ItemFieldSingleSelectValue {
									name
									field { ... on ProjectV2SingleSelectField { name } }
								}
								... on ProjectV2ItemFieldTextValue {
									text
									field { ... on ProjectV2Field { name } }
								}
							}
						}
					}
				}
			}`, alias, num))
		}
		repoAlias := fmt.Sprintf("r%d", repoIdx)
		queryParts = append(queryParts, fmt.Sprintf(`%s: repository(owner: %q, name: %q) { %s }`,
			repoAlias, key.owner, key.repo, strings.Join(issueParts, " ")))
		repoIdx++
	}

	query := fmt.Sprintf(`query { %s }`, strings.Join(queryParts, " "))

	// Execute via go-gh HTTP client
	output, err := c.doRawGraphQL(query, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to execute targeted project items query: %w", err)
	}

	// Parse the response - use generic map structure due to dynamic aliases
	var response struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to parse targeted project items response: %w", err)
	}

	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL errors: %s", response.Errors[0].Message)
	}

	// Process results
	var items []ProjectItem

	for repoAlias, repoData := range response.Data {
		if !strings.HasPrefix(repoAlias, "r") {
			continue
		}

		var repoIssuesData map[string]json.RawMessage
		if err := json.Unmarshal(repoData, &repoIssuesData); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to parse batch response for %s: %v\n", repoAlias, err)
			continue
		}

		for issueAlias, issueData := range repoIssuesData {
			if !strings.HasPrefix(issueAlias, "i") {
				continue
			}

			var issue struct {
				ID         string `json:"id"`
				Number     int    `json:"number"`
				Title      string `json:"title"`
				Body       string `json:"body"`
				State      string `json:"state"`
				URL        string `json:"url"`
				Repository struct {
					NameWithOwner string `json:"nameWithOwner"`
				} `json:"repository"`
				Assignees struct {
					Nodes []struct {
						Login string `json:"login"`
					} `json:"nodes"`
				} `json:"assignees"`
				Labels struct {
					Nodes []struct {
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"labels"`
				ProjectItems struct {
					Nodes []struct {
						ID      string `json:"id"`
						Project struct {
							ID string `json:"id"`
						} `json:"project"`
						FieldValues struct {
							Nodes []rawFieldValueNode `json:"nodes"`
						} `json:"fieldValues"`
					} `json:"nodes"`
				} `json:"projectItems"`
			}

			if err := json.Unmarshal(issueData, &issue); err != nil {
				// Warn instead of silently dropping the issue from the batch.
				fmt.Fprintf(os.Stderr, "Warning: failed to parse batch project item %s/%s; skipping: %v\n", repoAlias, issueAlias, err)
				continue
			}

			// Find the project item for our target project
			for _, pItem := range issue.ProjectItems.Nodes {
				if pItem.Project.ID != projectID {
					continue
				}

				// Parse repository owner/name
				repoParts := splitRepoName(issue.Repository.NameWithOwner)
				var repoOwner, repoName string
				if len(repoParts) == 2 {
					repoOwner = repoParts[0]
					repoName = repoParts[1]
				}

				// Build assignees
				var assignees []Actor
				for _, a := range issue.Assignees.Nodes {
					assignees = append(assignees, Actor{Login: a.Login})
				}

				// Build labels
				var labels []Label
				for _, l := range issue.Labels.Nodes {
					labels = append(labels, Label{Name: l.Name})
				}

				// Build field values
				var fieldValues []FieldValue
				fieldValues = appendRawFieldValues(fieldValues, pItem.FieldValues.Nodes)

				items = append(items, ProjectItem{
					ID: pItem.ID,
					Issue: &Issue{
						ID:     issue.ID,
						Number: issue.Number,
						Title:  issue.Title,
						Body:   issue.Body,
						State:  issue.State,
						URL:    issue.URL,
						Repository: Repository{
							Owner: repoOwner,
							Name:  repoName,
						},
						Assignees: assignees,
						Labels:    labels,
					},
					FieldValues: fieldValues,
				})
				break // Found the item for this project, move to next issue
			}
		}
	}

	return items, nil
}

// BoardItemsFilter allows filtering board items
type BoardItemsFilter struct {
	Repository string  // Filter by repository (owner/repo format)
	State      *string // Filter by issue state: "OPEN", "CLOSED", or nil for all
}

// GetProjectItemsForBoard fetches minimal project item data optimized for board display.
// Only retrieves: Number, Title, Status, Priority, and Repository.
// Uses cursor-based pagination to retrieve all items regardless of project size.
func (c *Client) GetProjectItemsForBoard(projectID string, filter *BoardItemsFilter) ([]BoardItem, error) {

	return collectPages(
		func(cursor *string) ([]BoardItem, pageInfo, error) {
			return c.getBoardItemsPage(projectID, cursor)
		},
		func(item BoardItem) bool {
			if filter != nil && filter.Repository != "" {
				if item.Repository != filter.Repository {
					return false
				}
			}

			// Apply state filter if specified
			if filter != nil && filter.State != nil {
				if item.State != *filter.State {
					return false
				}
			}

			return true
		},
		0,
	)
}

// getBoardItemsPage fetches a single page of board items with minimal data
func (c *Client) getBoardItemsPage(projectID string, cursor *string) ([]BoardItem, pageInfo, error) {
	var query struct {
		Node struct {
			ProjectV2 struct {
				Items struct {
					Nodes []struct {
						Content struct {
							TypeName string `graphql:"__typename"`
							Issue    struct {
								Number     int
								Title      string
								State      string
								Repository struct {
									NameWithOwner string
								}
							} `graphql:"... on Issue"`
						}
						FieldValues struct {
							Nodes []struct {
								TypeName                            string `graphql:"__typename"`
								ProjectV2ItemFieldSingleSelectValue struct {
									Name  string
									Field struct {
										ProjectV2SingleSelectField struct {
											Name string
										} `graphql:"... on ProjectV2SingleSelectField"`
									}
								} `graphql:"... on ProjectV2ItemFieldSingleSelectValue"`
							}
						} `graphql:"fieldValues(first: 10)"`
					}
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
				} `graphql:"items(first: 100, after: $cursor)"`
			} `graphql:"... on ProjectV2"`
		} `graphql:"node(id: $projectId)"`
	}

	variables := map[string]interface{}{
		"projectId": graphql.ID(projectID),
		"cursor":    (*graphql.String)(nil),
	}
	if cursor != nil {
		variables["cursor"] = graphql.String(*cursor)
	}

	err := c.gql.Query("GetProjectItemsForBoard", &query, variables)
	if err != nil {
		return nil, pageInfo{}, fmt.Errorf("failed to get board items: %w", err)
	}

	var items []BoardItem
	for _, node := range query.Node.ProjectV2.Items.Nodes {
		// Skip non-issue items
		if node.Content.TypeName != "Issue" {
			continue
		}

		item := BoardItem{
			Number:     node.Content.Issue.Number,
			Title:      node.Content.Issue.Title,
			State:      node.Content.Issue.State,
			Repository: node.Content.Issue.Repository.NameWithOwner,
		}

		// Extract Status and Priority from field values
		for _, fv := range node.FieldValues.Nodes {
			if fv.TypeName == "ProjectV2ItemFieldSingleSelectValue" {
				fieldName := fv.ProjectV2ItemFieldSingleSelectValue.Field.ProjectV2SingleSelectField.Name
				value := fv.ProjectV2ItemFieldSingleSelectValue.Name
				switch fieldName {
				case "Status":
					item.Status = value
				case "Priority":
					item.Priority = value
				}
			}
		}

		items = append(items, item)
	}

	return items, pageInfo{
		HasNextPage: query.Node.ProjectV2.Items.PageInfo.HasNextPage,
		EndCursor:   query.Node.ProjectV2.Items.PageInfo.EndCursor,
	}, nil
}

// GetSubIssues fetches all sub-issues for a given issue with pagination support
func (c *Client) GetSubIssues(owner, repo string, number int) ([]SubIssue, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}

	var subIssues []SubIssue
	var cursor *graphql.String
	pageCount := 0

	for {
		var query struct {
			Repository struct {
				Issue struct {
					SubIssues struct {
						Nodes []struct {
							ID         string
							Number     int
							Title      string
							State      string
							URL        string `graphql:"url"`
							Repository struct {
								Name  string
								Owner struct {
									Login string
								}
							}
						}
						PageInfo struct {
							HasNextPage bool
							EndCursor   string
						}
					} `graphql:"subIssues(first: 100, after: $cursor)"`
				} `graphql:"issue(number: $number)"`
			} `graphql:"repository(owner: $owner, name: $repo)"`
		}

		gqlNumber, err := safeGraphQLInt(number)
		if err != nil {
			return nil, err
		}

		variables := map[string]interface{}{
			"owner":  graphql.String(owner),
			"repo":   graphql.String(repo),
			"number": gqlNumber,
			"cursor": cursor,
		}

		err = c.gql.Query("GetSubIssues", &query, variables)
		if err != nil {
			return nil, fmt.Errorf("failed to get sub-issues for %s/%s#%d: %w", owner, repo, number, err)
		}

		for _, node := range query.Repository.Issue.SubIssues.Nodes {
			subIssues = append(subIssues, SubIssue{
				ID:     node.ID,
				Number: node.Number,
				Title:  node.Title,
				State:  node.State,
				URL:    node.URL,
				Repository: Repository{
					Owner: node.Repository.Owner.Login,
					Name:  node.Repository.Name,
				},
			})
		}

		pageCount++
		if !query.Repository.Issue.SubIssues.PageInfo.HasNextPage {
			break
		}

		endCursor := graphql.String(query.Repository.Issue.SubIssues.PageInfo.EndCursor)
		cursor = &endCursor

		// Warn if we're fetching many pages (performance awareness)
		if pageCount >= 2 {
			fmt.Fprintf(os.Stderr, "Note: Issue #%d has many sub-issues (%d+ fetched), this may take a moment...\n", number, len(subIssues))
		}
	}

	return subIssues, nil
}

// GetSubIssueCounts fetches sub-issue counts for multiple issues in a single query.
// This is more efficient than calling GetSubIssues for each issue individually.
// Returns a map of issue number to sub-issue count.
func (c *Client) GetSubIssueCounts(owner, repo string, numbers []int) (map[int]int, error) {
	if len(numbers) == 0 {
		return make(map[int]int), nil
	}

	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}

	// Build a GraphQL query with aliases for each issue
	// Example: query { repository(owner:"o", name:"r") { i1: issue(number:1) { subIssues { totalCount } } } }
	var queryParts []string
	for i, num := range numbers {
		queryParts = append(queryParts, fmt.Sprintf("i%d: issue(number: %d) { subIssues { totalCount } }", i, num))
	}

	query := fmt.Sprintf(`query { repository(owner: %q, name: %q) { %s } }`,
		owner, repo, strings.Join(queryParts, " "))

	// Execute via go-gh HTTP client
	output, err := c.doRawGraphQL(query, map[string]string{"X-Github-Next": "sub_issues"})
	if err != nil {
		return nil, fmt.Errorf("failed to execute batch sub-issue query: %w", err)
	}

	return parseSubIssueCountsResponse(output, numbers)
}

// parseSubIssueCountsResponse parses the JSON response from a batch sub-issue count query.
// Returns a map of issue number to sub-issue count.
func parseSubIssueCountsResponse(data []byte, numbers []int) (map[int]int, error) {
	var response struct {
		Data struct {
			Repository map[string]struct {
				SubIssues struct {
					TotalCount int `json:"totalCount"`
				} `json:"subIssues"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("failed to parse batch sub-issue response: %w", err)
	}

	// Inspect the GraphQL errors array (as parseSubIssuesBatchResponse does).
	// Without this, a partial/total failure silently yields count 0 for every
	// issue — indistinguishable from a genuine "no sub-issues".
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL errors: %s", response.Errors[0].Message)
	}

	result := make(map[int]int)
	for i, num := range numbers {
		alias := fmt.Sprintf("i%d", i)
		if issueData, ok := response.Data.Repository[alias]; ok {
			result[num] = issueData.SubIssues.TotalCount
		} else {
			result[num] = 0
		}
	}

	return result, nil
}

// GetSubIssuesBatch fetches sub-issues for multiple parent issues in a single query.
// All parent issues must be from the same repository.
// Returns a map of parent issue number to their sub-issues.
// This is more efficient than calling GetSubIssues for each parent individually,
// reducing API calls from O(N) to O(1) per batch.
func (c *Client) GetSubIssuesBatch(owner, repo string, numbers []int) (map[int][]SubIssue, error) {
	if len(numbers) == 0 {
		return make(map[int][]SubIssue), nil
	}

	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}

	// Build a GraphQL query with aliases for each issue
	// Fetches first 100 sub-issues per parent, includes pageInfo to detect truncation
	var queryParts []string
	for i, num := range numbers {
		queryParts = append(queryParts, fmt.Sprintf(`i%d: issue(number: %d) {
			subIssues(first: 100) {
				nodes {
					id
					number
					title
					state
					url
					repository {
						name
						owner { login }
					}
				}
				pageInfo {
					hasNextPage
				}
			}
		}`, i, num))
	}

	query := fmt.Sprintf(`query { repository(owner: %q, name: %q) { %s } }`,
		owner, repo, strings.Join(queryParts, " "))

	// Execute via go-gh HTTP client
	output, err := c.doRawGraphQL(query, map[string]string{"X-Github-Next": "sub_issues"})
	if err != nil {
		return nil, fmt.Errorf("failed to execute batch sub-issue query: %w", err)
	}

	result, needsPagination, err := parseSubIssuesBatchResponse(output, numbers)
	if err != nil {
		return nil, err
	}

	// Handle pagination fallback for issues with >100 sub-issues
	for _, num := range needsPagination {
		fmt.Fprintf(os.Stderr, "Note: Issue #%d has >100 sub-issues, fetching all pages...\n", num)
		allSubIssues, err := c.GetSubIssues(owner, repo, num)
		if err != nil {
			// If fallback fails, keep the truncated batch results but warn so the
			// caller knows #num's sub-issue list is incomplete, not authoritative.
			fmt.Fprintf(os.Stderr, "Warning: failed to fetch remaining sub-issues for #%d; results are truncated to the first 100: %v\n", num, err)
			continue
		}
		result[num] = allSubIssues
	}

	return result, nil
}

// parseSubIssuesBatchResponse parses the JSON response from a batch sub-issues query.
// Returns the sub-issues map, a list of issue numbers that need pagination (hasNextPage=true),
// and any error. Issues needing pagination will have partial results in the map.
func parseSubIssuesBatchResponse(data []byte, numbers []int) (map[int][]SubIssue, []int, error) {
	var response struct {
		Data struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return nil, nil, fmt.Errorf("failed to parse batch sub-issue response: %w", err)
	}

	if len(response.Errors) > 0 {
		return nil, nil, fmt.Errorf("GraphQL errors: %s", response.Errors[0].Message)
	}

	result := make(map[int][]SubIssue)
	var needsPagination []int

	for i, num := range numbers {
		alias := fmt.Sprintf("i%d", i)
		issueData, ok := response.Data.Repository[alias]
		if !ok {
			result[num] = []SubIssue{}
			continue
		}

		var issueResponse struct {
			SubIssues struct {
				Nodes []struct {
					ID         string `json:"id"`
					Number     int    `json:"number"`
					Title      string `json:"title"`
					State      string `json:"state"`
					URL        string `json:"url"`
					Repository struct {
						Name  string `json:"name"`
						Owner struct {
							Login string `json:"login"`
						} `json:"owner"`
					} `json:"repository"`
				} `json:"nodes"`
				PageInfo struct {
					HasNextPage bool `json:"hasNextPage"`
				} `json:"pageInfo"`
			} `json:"subIssues"`
		}

		if err := json.Unmarshal(issueData, &issueResponse); err != nil {
			// Warn rather than silently returning an empty list, which reads as
			// "no sub-issues" when in fact the per-item response was malformed.
			fmt.Fprintf(os.Stderr, "Warning: failed to parse sub-issues for #%d; treating as none: %v\n", num, err)
			result[num] = []SubIssue{}
			continue
		}

		var subIssues []SubIssue
		for _, node := range issueResponse.SubIssues.Nodes {
			subIssues = append(subIssues, SubIssue{
				ID:     node.ID,
				Number: node.Number,
				Title:  node.Title,
				State:  node.State,
				URL:    node.URL,
				Repository: Repository{
					Owner: node.Repository.Owner.Login,
					Name:  node.Repository.Name,
				},
			})
		}
		result[num] = subIssues

		if issueResponse.SubIssues.PageInfo.HasNextPage {
			needsPagination = append(needsPagination, num)
		}
	}

	return result, needsPagination, nil
}

// GetRepositoryIssues fetches issues from a repository with the given state filter
func (c *Client) GetRepositoryIssues(owner, repo, state string) ([]Issue, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}

	// Map state to GraphQL enum values (IssueState enum, not String)
	var states []IssueState
	switch state {
	case "open":
		states = []IssueState{IssueStateOpen}
	case "closed":
		states = []IssueState{IssueStateClosed}
	case "all", "":
		states = []IssueState{IssueStateOpen, IssueStateClosed}
	default:
		states = []IssueState{IssueState(state)}
	}

	// Use cursor-based pagination to fetch all issues
	var allIssues []Issue
	var cursor *string

	for {
		issues, pi, err := c.getRepositoryIssuesPage(owner, repo, states, cursor)
		if err != nil {
			return nil, err
		}
		allIssues = append(allIssues, issues...)

		if !pi.HasNextPage {
			break
		}
		cursor = &pi.EndCursor
	}

	return allIssues, nil
}

// getRepositoryIssuesPage fetches a single page of repository issues
func (c *Client) getRepositoryIssuesPage(owner, repo string, states []IssueState, cursor *string) ([]Issue, pageInfo, error) {
	var query struct {
		Repository struct {
			Issues struct {
				Nodes []struct {
					ID     string
					Number int
					Title  string
					State  string
					URL    string `graphql:"url"`
				}
				PageInfo struct {
					HasNextPage bool
					EndCursor   string
				}
			} `graphql:"issues(first: 100, after: $cursor, states: $states)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	variables := map[string]interface{}{
		"owner":  graphql.String(owner),
		"repo":   graphql.String(repo),
		"states": states,
		"cursor": (*graphql.String)(nil),
	}
	if cursor != nil {
		variables["cursor"] = graphql.String(*cursor)
	}

	err := c.gql.Query("GetRepositoryIssues", &query, variables)
	if err != nil {
		return nil, pageInfo{}, fmt.Errorf("failed to get issues from %s/%s: %w", owner, repo, err)
	}

	var issues []Issue
	for _, node := range query.Repository.Issues.Nodes {
		issues = append(issues, Issue{
			ID:     node.ID,
			Number: node.Number,
			Title:  node.Title,
			State:  node.State,
			URL:    node.URL,
			Repository: Repository{
				Owner: owner,
				Name:  repo,
			},
		})
	}

	return issues, pageInfo{
		HasNextPage: query.Repository.Issues.PageInfo.HasNextPage,
		EndCursor:   query.Repository.Issues.PageInfo.EndCursor,
	}, nil
}

// SearchRepositoryIssues searches for issues in a repository using GitHub Search API.
// This is more efficient than fetching all issues when filtering by state, labels, or text.
// The limit parameter controls maximum results (0 = no limit, uses pagination).
func (c *Client) SearchRepositoryIssues(owner, repo string, filters SearchFilters, limit int) ([]Issue, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}
	for i, label := range filters.Labels {
		if err := validateLabelName(label); err != nil {
			return nil, fmt.Errorf("filters.Labels[%d]: %w", i, err)
		}
	}

	// Build the search query string
	queryParts := []string{
		fmt.Sprintf("repo:%s/%s", owner, repo),
		"is:issue",
	}

	// Add state filter (default to open if not specified)
	switch filters.State {
	case "closed":
		queryParts = append(queryParts, "is:closed")
	case "all":
		// No state filter - include both open and closed
	case "open", "":
		queryParts = append(queryParts, "is:open")
	default:
		queryParts = append(queryParts, "is:open") // Default to open for unknown states
	}

	// Add label filters
	for _, label := range filters.Labels {
		queryParts = append(queryParts, fmt.Sprintf("label:%q", label))
	}

	// Add assignee filter
	if filters.Assignee != "" {
		queryParts = append(queryParts, fmt.Sprintf("assignee:%s", filters.Assignee))
	}

	// Add free-text search
	if filters.Search != "" {
		queryParts = append(queryParts, filters.Search)
	}

	searchQuery := strings.Join(queryParts, " ")

	var allIssues []Issue
	var cursor *string
	pageSize := 100
	if limit > 0 && limit < pageSize {
		pageSize = limit
	}

	for {
		issues, pageInfo, err := c.searchIssuesPage(searchQuery, pageSize, cursor)
		if err != nil {
			return nil, err
		}

		for _, issue := range issues {
			// Set repository info since search doesn't always include it
			if issue.Repository.Owner == "" {
				issue.Repository = Repository{Owner: owner, Name: repo}
			}
			allIssues = append(allIssues, issue)

			// Check if we've reached the limit
			if limit > 0 && len(allIssues) >= limit {
				return allIssues[:limit], nil
			}
		}

		if !pageInfo.HasNextPage {
			break
		}
		cursor = &pageInfo.EndCursor
	}

	return allIssues, nil
}

// GetProjectFieldsForIssues fetches project field values for multiple issues in batched queries.
// Returns a map from issue node ID to its field values. Uses batching to avoid query size limits.
func (c *Client) GetProjectFieldsForIssues(projectID string, issueIDs []string) (map[string][]FieldValue, error) {
	if len(issueIDs) == 0 {
		return make(map[string][]FieldValue), nil
	}

	// Batch issues to avoid GraphQL query limits (max ~100 aliases per query)
	const batchSize = 50
	result := make(map[string][]FieldValue)

	for i := 0; i < len(issueIDs); i += batchSize {
		end := i + batchSize
		if end > len(issueIDs) {
			end = len(issueIDs)
		}
		batch := issueIDs[i:end]

		batchResult, err := c.getProjectFieldsForIssuesBatch(projectID, batch)
		if err != nil {
			return nil, err
		}

		for id, fields := range batchResult {
			result[id] = fields
		}
	}

	return result, nil
}

// getProjectFieldsForIssuesBatch fetches project fields for a batch of issues using aliases
func (c *Client) getProjectFieldsForIssuesBatch(projectID string, issueIDs []string) (map[string][]FieldValue, error) {
	for i, id := range issueIDs {
		if err := validateNodeID(id); err != nil {
			return nil, fmt.Errorf("issueIDs[%d]: %w", i, err)
		}
	}

	// Build a GraphQL query with aliases for each issue
	var queryParts []string
	for i, id := range issueIDs {
		queryParts = append(queryParts, fmt.Sprintf(`n%d: node(id: %q) {
			... on Issue {
				id
				projectItems(first: 10) {
					nodes {
						project { id }
						fieldValues(first: 20) {
							nodes {
								__typename
								... on ProjectV2ItemFieldSingleSelectValue {
									name
									field { ... on ProjectV2SingleSelectField { name } }
								}
								... on ProjectV2ItemFieldTextValue {
									text
									field { ... on ProjectV2Field { name } }
								}
							}
						}
					}
				}
			}
		}`, i, id))
	}

	query := fmt.Sprintf("query { %s }", strings.Join(queryParts, " "))

	// Execute via go-gh HTTP client
	output, err := c.doRawGraphQL(query, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to execute batch project fields query: %w", err)
	}

	// Parse the response
	var response struct {
		Data map[string]struct {
			ID           string `json:"id"`
			ProjectItems struct {
				Nodes []struct {
					Project struct {
						ID string `json:"id"`
					} `json:"project"`
					FieldValues struct {
						Nodes []rawFieldValueNode `json:"nodes"`
					} `json:"fieldValues"`
				} `json:"nodes"`
			} `json:"projectItems"`
		} `json:"data"`
	}

	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to parse batch project fields response: %w", err)
	}

	// Build result map
	result := make(map[string][]FieldValue)
	for _, nodeData := range response.Data {
		if nodeData.ID == "" {
			continue
		}

		var fieldValues []FieldValue
		for _, projectItem := range nodeData.ProjectItems.Nodes {
			// Only include field values from the target project
			if projectItem.Project.ID != projectID {
				continue
			}

			fieldValues = appendRawFieldValues(fieldValues, projectItem.FieldValues.Nodes)
		}

		result[nodeData.ID] = fieldValues
	}

	return result, nil
}

// searchIssuesPage fetches a single page of search results
func (c *Client) searchIssuesPage(query string, pageSize int, cursor *string) ([]Issue, pageInfo, error) {
	var gqlQuery struct {
		Search struct {
			Nodes []struct {
				TypeName string `graphql:"__typename"`
				Issue    struct {
					ID         string
					Number     int
					Title      string
					Body       string
					State      string
					URL        string `graphql:"url"`
					Repository struct {
						NameWithOwner string
					}
					Author struct {
						Login string
					}
					Assignees struct {
						Nodes []struct {
							Login string
						}
					} `graphql:"assignees(first: 10)"`
					Labels struct {
						Nodes []struct {
							Name  string
							Color string
						}
					} `graphql:"labels(first: 20)"`
					Milestone struct {
						Title string
					}
				} `graphql:"... on Issue"`
			}
			PageInfo struct {
				HasNextPage bool
				EndCursor   string
			}
		} `graphql:"search(query: $query, type: ISSUE, first: $first, after: $cursor)"`
	}

	gqlPageSize, err := safeGraphQLInt(pageSize)
	if err != nil {
		return nil, pageInfo{}, err
	}

	variables := map[string]interface{}{
		"query":  graphql.String(query),
		"first":  gqlPageSize,
		"cursor": (*graphql.String)(nil),
	}
	if cursor != nil {
		variables["cursor"] = graphql.String(*cursor)
	}

	err = c.gql.Query("SearchIssues", &gqlQuery, variables)
	if err != nil {
		return nil, pageInfo{}, fmt.Errorf("failed to search issues: %w", err)
	}

	var issues []Issue
	for _, node := range gqlQuery.Search.Nodes {
		if node.TypeName != "Issue" {
			continue
		}

		issue := Issue{
			ID:     node.Issue.ID,
			Number: node.Issue.Number,
			Title:  node.Issue.Title,
			Body:   node.Issue.Body,
			State:  node.Issue.State,
			URL:    node.Issue.URL,
			Author: Actor{Login: node.Issue.Author.Login},
		}

		// Parse repository
		if node.Issue.Repository.NameWithOwner != "" {
			parts := splitRepoName(node.Issue.Repository.NameWithOwner)
			if len(parts) == 2 {
				issue.Repository = Repository{
					Owner: parts[0],
					Name:  parts[1],
				}
			}
		}

		// Parse assignees
		for _, a := range node.Issue.Assignees.Nodes {
			issue.Assignees = append(issue.Assignees, Actor{Login: a.Login})
		}

		// Parse labels
		for _, l := range node.Issue.Labels.Nodes {
			issue.Labels = append(issue.Labels, Label{Name: l.Name, Color: l.Color})
		}

		// Parse milestone
		if node.Issue.Milestone.Title != "" {
			issue.Milestone = &Milestone{Title: node.Issue.Milestone.Title}
		}

		issues = append(issues, issue)
	}

	return issues, pageInfo{
		HasNextPage: gqlQuery.Search.PageInfo.HasNextPage,
		EndCursor:   gqlQuery.Search.PageInfo.EndCursor,
	}, nil
}

// IntakeCandidate is an open issue with its membership in one project resolved.
type IntakeCandidate struct {
	Issue Issue
	// InProject reports whether the issue is already an item on the project.
	InProject bool
	// MembershipUnknown is true when the issue belongs to more projects than a
	// single projectItems page returns and the project was not among them, so
	// neither "tracked" nor "untracked" can be claimed (#918, #860).
	MembershipUnknown bool
}

// intakeSearchNode is one search result as intake needs it: the fields intake
// outputs or filters on, plus the issue's project memberships. Body, author and
// milestone are deliberately absent (#918).
type intakeSearchNode struct {
	TypeName string `graphql:"__typename"`
	Issue    struct {
		ID         string
		Number     int
		Title      string
		State      string
		URL        string `graphql:"url"`
		Repository struct {
			NameWithOwner string
		}
		Assignees struct {
			Nodes []struct {
				Login string
			}
		} `graphql:"assignees(first: 10)"`
		Labels struct {
			Nodes []struct {
				Name string
			}
		} `graphql:"labels(first: 20)"`
		ProjectItems struct {
			Nodes []struct {
				Project struct {
					ID string
				}
			}
			PageInfo struct {
				HasNextPage bool
			}
		} `graphql:"projectItems(first: 20)"`
	} `graphql:"... on Issue"`
}

// SearchIntakeCandidates returns the open issues in a repository together with
// whether each is already on the given project.
//
// Membership is read from each issue's own projectItems rather than by scanning
// the project board, so the cost scales with open issues instead of with every
// item ever added to the board (#918). An issue on more than one page of
// projects that does not show the project on the first page is reported as
// MembershipUnknown rather than guessed at.
func (c *Client) SearchIntakeCandidates(owner, repo string, labels []string, projectID string) ([]IntakeCandidate, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required")
	}
	for i, label := range labels {
		if err := validateLabelName(label); err != nil {
			return nil, fmt.Errorf("labels[%d]: %w", i, err)
		}
	}

	queryParts := []string{
		fmt.Sprintf("repo:%s/%s", owner, repo),
		"is:issue",
		"is:open",
	}
	for _, label := range labels {
		queryParts = append(queryParts, fmt.Sprintf("label:%q", label))
	}
	searchQuery := strings.Join(queryParts, " ")

	var candidates []IntakeCandidate
	var cursor *string
	for {
		nodes, page, err := c.searchIntakeCandidatesPage(searchQuery, cursor)
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			if node.TypeName != "Issue" {
				continue
			}
			candidates = append(candidates, intakeCandidateFromNode(node, projectID, owner, repo))
		}
		if !page.HasNextPage {
			break
		}
		endCursor := page.EndCursor
		cursor = &endCursor
	}

	return candidates, nil
}

// searchIntakeCandidatesPage fetches one page of intake search results.
func (c *Client) searchIntakeCandidatesPage(searchQuery string, cursor *string) ([]intakeSearchNode, pageInfo, error) {
	var gqlQuery struct {
		Search struct {
			Nodes    []intakeSearchNode
			PageInfo struct {
				HasNextPage bool
				EndCursor   string
			}
		} `graphql:"search(query: $query, type: ISSUE, first: 100, after: $cursor)"`
	}

	variables := map[string]interface{}{
		"query":  graphql.String(searchQuery),
		"cursor": (*graphql.String)(nil),
	}
	if cursor != nil {
		variables["cursor"] = graphql.String(*cursor)
	}

	if err := c.gql.Query("SearchIntakeCandidates", &gqlQuery, variables); err != nil {
		return nil, pageInfo{}, fmt.Errorf("failed to search intake candidates: %w", err)
	}

	return gqlQuery.Search.Nodes, pageInfo{
		HasNextPage: gqlQuery.Search.PageInfo.HasNextPage,
		EndCursor:   gqlQuery.Search.PageInfo.EndCursor,
	}, nil
}

// intakeCandidateFromNode maps a search node to a candidate and resolves its
// membership in projectID.
func intakeCandidateFromNode(node intakeSearchNode, projectID, owner, repo string) IntakeCandidate {
	issue := Issue{
		ID:         node.Issue.ID,
		Number:     node.Issue.Number,
		Title:      node.Issue.Title,
		State:      node.Issue.State,
		URL:        node.Issue.URL,
		Repository: Repository{Owner: owner, Name: repo},
	}
	if parts := splitRepoName(node.Issue.Repository.NameWithOwner); len(parts) == 2 {
		issue.Repository = Repository{Owner: parts[0], Name: parts[1]}
	}
	for _, a := range node.Issue.Assignees.Nodes {
		issue.Assignees = append(issue.Assignees, Actor{Login: a.Login})
	}
	for _, l := range node.Issue.Labels.Nodes {
		issue.Labels = append(issue.Labels, Label{Name: l.Name})
	}

	candidate := IntakeCandidate{Issue: issue}
	for _, item := range node.Issue.ProjectItems.Nodes {
		if item.Project.ID == projectID {
			candidate.InProject = true
			break
		}
	}
	if !candidate.InProject && node.Issue.ProjectItems.PageInfo.HasNextPage {
		candidate.MembershipUnknown = true
	}
	return candidate
}

// GetOpenIssuesByLabel fetches open issues with a specific label
func (c *Client) GetOpenIssuesByLabel(owner, repo, label string) ([]Issue, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}
	if err := validateLabelName(label); err != nil {
		return nil, err
	}
	return c.getIssuesByLabelPaginated(owner, repo, label, []IssueState{IssueStateOpen})
}

// GetOpenIssuesByLabels fetches open issues matching ALL specified labels.
// Includes SubIssueCount for each issue (from subIssues.totalCount).
func (c *Client) GetOpenIssuesByLabels(owner, repo string, labels []string) ([]Issue, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}
	if err := validateLabelNames(labels); err != nil {
		return nil, err
	}

	var gqlLabels []graphql.String
	for _, l := range labels {
		gqlLabels = append(gqlLabels, graphql.String(l))
	}

	var allIssues []Issue
	var cursor *string
	for {
		issues, pi, err := c.getOpenIssuesByLabelsPage(owner, repo, labels, gqlLabels, cursor)
		if err != nil {
			return nil, err
		}
		allIssues = append(allIssues, issues...)
		if !pi.HasNextPage {
			break
		}
		cursor = &pi.EndCursor
	}

	return allIssues, nil
}

// getOpenIssuesByLabelsPage fetches a single page of open issues carrying all of
// the given labels, using cursor-based pagination so callers see every match
// rather than only the first 100 (#860). labels is passed through solely for
// error messages.
func (c *Client) getOpenIssuesByLabelsPage(owner, repo string, labels []string, gqlLabels []graphql.String, cursor *string) ([]Issue, pageInfo, error) {
	var query struct {
		Repository struct {
			Issues struct {
				Nodes []struct {
					ID     string
					Number int
					Title  string
					State  string
					URL    string `graphql:"url"`
					Labels struct {
						Nodes []struct {
							Name string
						}
					} `graphql:"labels(first: 10)"`
					SubIssues struct {
						TotalCount int
					} `graphql:"subIssues(first: 0)"`
				}
				PageInfo struct {
					HasNextPage bool
					EndCursor   string
				}
			} `graphql:"issues(first: 100, after: $cursor, states: $states, labels: $labels)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	variables := map[string]interface{}{
		"owner":  graphql.String(owner),
		"repo":   graphql.String(repo),
		"labels": gqlLabels,
		"states": []IssueState{IssueStateOpen},
		"cursor": (*graphql.String)(nil),
	}
	if cursor != nil {
		variables["cursor"] = graphql.String(*cursor)
	}

	err := c.gql.Query("GetIssuesByLabels", &query, variables)
	if err != nil {
		return nil, pageInfo{}, fmt.Errorf("failed to get issues with labels %v from %s/%s: %w", labels, owner, repo, err)
	}

	var issues []Issue
	for _, node := range query.Repository.Issues.Nodes {
		var issueLabels []Label
		for _, l := range node.Labels.Nodes {
			issueLabels = append(issueLabels, Label{Name: l.Name})
		}
		issues = append(issues, Issue{
			ID:     node.ID,
			Number: node.Number,
			Title:  node.Title,
			State:  node.State,
			URL:    node.URL,
			Labels: issueLabels,
			Repository: Repository{
				Owner: owner,
				Name:  repo,
			},
			SubIssueCount: node.SubIssues.TotalCount,
		})
	}

	return issues, pageInfo{
		HasNextPage: query.Repository.Issues.PageInfo.HasNextPage,
		EndCursor:   query.Repository.Issues.PageInfo.EndCursor,
	}, nil
}

// getIssuesByLabelPaginated fetches all issues with a specific label using cursor-based pagination
func (c *Client) getIssuesByLabelPaginated(owner, repo, label string, states []IssueState) ([]Issue, error) {

	var allIssues []Issue
	var cursor *string

	for {
		issues, pi, err := c.getIssuesByLabelPage(owner, repo, label, states, cursor)
		if err != nil {
			return nil, err
		}
		allIssues = append(allIssues, issues...)

		if !pi.HasNextPage {
			break
		}
		cursor = &pi.EndCursor
	}

	return allIssues, nil
}

// getIssuesByLabelPage fetches a single page of issues with a specific label
func (c *Client) getIssuesByLabelPage(owner, repo, label string, states []IssueState, cursor *string) ([]Issue, pageInfo, error) {
	var query struct {
		Repository struct {
			Issues struct {
				Nodes []struct {
					ID     string
					Number int
					Title  string
					State  string
					URL    string `graphql:"url"`
					Labels struct {
						Nodes []struct {
							Name string
						}
					} `graphql:"labels(first: 10)"`
				}
				PageInfo struct {
					HasNextPage bool
					EndCursor   string
				}
			} `graphql:"issues(first: 100, after: $cursor, states: $states, labels: [$label])"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	variables := map[string]interface{}{
		"owner":  graphql.String(owner),
		"repo":   graphql.String(repo),
		"label":  graphql.String(label),
		"states": states,
		"cursor": (*graphql.String)(nil),
	}
	if cursor != nil {
		variables["cursor"] = graphql.String(*cursor)
	}

	err := c.gql.Query("GetIssuesByLabel", &query, variables)
	if err != nil {
		return nil, pageInfo{}, fmt.Errorf("failed to get issues with label %s from %s/%s: %w", label, owner, repo, err)
	}

	var issues []Issue
	for _, node := range query.Repository.Issues.Nodes {
		var labels []Label
		for _, l := range node.Labels.Nodes {
			labels = append(labels, Label{Name: l.Name})
		}
		issues = append(issues, Issue{
			ID:     node.ID,
			Number: node.Number,
			Title:  node.Title,
			State:  node.State,
			URL:    node.URL,
			Labels: labels,
			Repository: Repository{
				Owner: owner,
				Name:  repo,
			},
		})
	}

	return issues, pageInfo{
		HasNextPage: query.Repository.Issues.PageInfo.HasNextPage,
		EndCursor:   query.Repository.Issues.PageInfo.EndCursor,
	}, nil
}

// GetClosedIssuesByLabel fetches closed issues with a specific label
func (c *Client) GetClosedIssuesByLabel(owner, repo, label string) ([]Issue, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}
	if err := validateLabelName(label); err != nil {
		return nil, err
	}
	return c.getIssuesByLabelPaginated(owner, repo, label, []IssueState{IssueStateClosed})
}

// GetParentIssue fetches the parent issue for a given sub-issue
func (c *Client) GetParentIssue(owner, repo string, number int) (*Issue, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}

	var query struct {
		Repository struct {
			Issue struct {
				Parent struct {
					ID     string
					Number int
					Title  string
					State  string
					URL    string `graphql:"url"`
				} `graphql:"parent"`
			} `graphql:"issue(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	gqlNumber, err := safeGraphQLInt(number)
	if err != nil {
		return nil, err
	}

	variables := map[string]interface{}{
		"owner":  graphql.String(owner),
		"repo":   graphql.String(repo),
		"number": gqlNumber,
	}

	err = c.gql.Query("GetParentIssue", &query, variables)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent issue for %s/%s#%d: %w", owner, repo, number, err)
	}

	// If no parent issue, return nil
	if query.Repository.Issue.Parent.ID == "" {
		return nil, nil
	}

	return &Issue{
		ID:     query.Repository.Issue.Parent.ID,
		Number: query.Repository.Issue.Parent.Number,
		Title:  query.Repository.Issue.Parent.Title,
		State:  query.Repository.Issue.Parent.State,
		URL:    query.Repository.Issue.Parent.URL,
	}, nil
}

// ListProjects fetches all projects for an owner (user or organization)
func (c *Client) ListProjects(owner string) ([]Project, error) {

	// First try as user projects. A successful user query means the owner IS a
	// user, so return its projects even when empty — a user with zero open
	// projects must yield an empty list, not fall through to the org path and
	// surface "Could not resolve to an Organization" (#861).
	projects, userErr := c.listUserProjects(owner)
	if userErr == nil {
		return projects, nil
	}

	// User path failed (owner is likely an organization); try org projects.
	orgProjects, orgErr := c.listOrgProjects(owner)
	if orgErr != nil {
		return nil, fmt.Errorf("failed to list projects for %s: %w", owner, errors.Join(userErr, orgErr))
	}

	return orgProjects, nil
}

func (c *Client) listUserProjects(owner string) ([]Project, error) {
	var projects []Project
	var cursor *string
	for {
		page, pi, err := c.listUserProjectsPage(owner, cursor)
		if err != nil {
			return nil, err
		}
		projects = append(projects, page...)
		if !pi.HasNextPage {
			break
		}
		cursor = &pi.EndCursor
	}
	return projects, nil
}

// listUserProjectsPage fetches a single page of a user's open projects, using
// cursor-based pagination so older open projects past the first page are not
// dropped (#860). Closed projects are filtered client-side.
func (c *Client) listUserProjectsPage(owner string, cursor *string) ([]Project, pageInfo, error) {
	var query struct {
		User struct {
			ProjectsV2 struct {
				Nodes []struct {
					ID     string
					Number int
					Title  string
					URL    string `graphql:"url"`
					Closed bool
				}
				PageInfo struct {
					HasNextPage bool
					EndCursor   string
				}
			} `graphql:"projectsV2(first: 100, after: $cursor, orderBy: {field: UPDATED_AT, direction: DESC})"`
		} `graphql:"user(login: $owner)"`
	}

	variables := map[string]interface{}{
		"owner":  graphql.String(owner),
		"cursor": (*graphql.String)(nil),
	}
	if cursor != nil {
		variables["cursor"] = graphql.String(*cursor)
	}

	if err := c.gql.Query("ListUserProjects", &query, variables); err != nil {
		return nil, pageInfo{}, err
	}

	var projects []Project
	for _, node := range query.User.ProjectsV2.Nodes {
		if node.Closed {
			continue // Skip closed projects
		}
		projects = append(projects, Project{
			ID:     node.ID,
			Number: node.Number,
			Title:  node.Title,
			URL:    node.URL,
			Closed: node.Closed,
			Owner: ProjectOwner{
				Type:  "User",
				Login: owner,
			},
		})
	}

	return projects, pageInfo{
		HasNextPage: query.User.ProjectsV2.PageInfo.HasNextPage,
		EndCursor:   query.User.ProjectsV2.PageInfo.EndCursor,
	}, nil
}

// Comment represents an issue comment
type Comment struct {
	ID         string
	DatabaseId int
	Author     string
	Body       string
	CreatedAt  string
}

// GetIssueComments fetches comments for an issue
func (c *Client) GetIssueComments(owner, repo string, number int) ([]Comment, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}

	gqlNumber, err := safeGraphQLInt(number)
	if err != nil {
		return nil, err
	}

	var comments []Comment
	var cursor *string
	for {
		page, pi, err := c.getIssueCommentsPage(owner, repo, gqlNumber, cursor)
		if err != nil {
			return nil, fmt.Errorf("failed to get comments for %s/%s#%d: %w", owner, repo, number, err)
		}
		comments = append(comments, page...)
		if !pi.HasNextPage {
			break
		}
		cursor = &pi.EndCursor
	}

	return comments, nil
}

// getIssueCommentsPage fetches a single page of an issue's comments, using
// cursor-based pagination so all comments are returned rather than only the
// first 50 (#860).
func (c *Client) getIssueCommentsPage(owner, repo string, gqlNumber graphql.Int, cursor *string) ([]Comment, pageInfo, error) {
	var query struct {
		Repository struct {
			Issue struct {
				Comments struct {
					Nodes []struct {
						ID         string
						DatabaseId int `graphql:"databaseId"`
						Body       string
						CreatedAt  string
						Author     struct {
							Login string
						}
					}
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
				} `graphql:"comments(first: 100, after: $cursor)"`
			} `graphql:"issue(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	variables := map[string]interface{}{
		"owner":  graphql.String(owner),
		"repo":   graphql.String(repo),
		"number": gqlNumber,
		"cursor": (*graphql.String)(nil),
	}
	if cursor != nil {
		variables["cursor"] = graphql.String(*cursor)
	}

	if err := c.gql.Query("GetIssueComments", &query, variables); err != nil {
		return nil, pageInfo{}, err
	}

	var comments []Comment
	for _, node := range query.Repository.Issue.Comments.Nodes {
		comments = append(comments, Comment{
			ID:         node.ID,
			DatabaseId: node.DatabaseId,
			Author:     node.Author.Login,
			Body:       node.Body,
			CreatedAt:  node.CreatedAt,
		})
	}

	return comments, pageInfo{
		HasNextPage: query.Repository.Issue.Comments.PageInfo.HasNextPage,
		EndCursor:   query.Repository.Issue.Comments.PageInfo.EndCursor,
	}, nil
}

func (c *Client) listOrgProjects(owner string) ([]Project, error) {
	var projects []Project
	var cursor *string
	for {
		page, pi, err := c.listOrgProjectsPage(owner, cursor)
		if err != nil {
			return nil, err
		}
		projects = append(projects, page...)
		if !pi.HasNextPage {
			break
		}
		cursor = &pi.EndCursor
	}
	return projects, nil
}

// listOrgProjectsPage fetches a single page of an org's open projects, using
// cursor-based pagination so older open projects past the first page are not
// dropped (#860). Closed projects are filtered client-side.
func (c *Client) listOrgProjectsPage(owner string, cursor *string) ([]Project, pageInfo, error) {
	var query struct {
		Organization struct {
			ProjectsV2 struct {
				Nodes []struct {
					ID     string
					Number int
					Title  string
					URL    string `graphql:"url"`
					Closed bool
				}
				PageInfo struct {
					HasNextPage bool
					EndCursor   string
				}
			} `graphql:"projectsV2(first: 100, after: $cursor, orderBy: {field: UPDATED_AT, direction: DESC})"`
		} `graphql:"organization(login: $owner)"`
	}

	variables := map[string]interface{}{
		"owner":  graphql.String(owner),
		"cursor": (*graphql.String)(nil),
	}
	if cursor != nil {
		variables["cursor"] = graphql.String(*cursor)
	}

	if err := c.gql.Query("ListOrgProjects", &query, variables); err != nil {
		return nil, pageInfo{}, err
	}

	var projects []Project
	for _, node := range query.Organization.ProjectsV2.Nodes {
		if node.Closed {
			continue // Skip closed projects
		}
		projects = append(projects, Project{
			ID:     node.ID,
			Number: node.Number,
			Title:  node.Title,
			URL:    node.URL,
			Closed: node.Closed,
			Owner: ProjectOwner{
				Type:  "Organization",
				Login: owner,
			},
		})
	}

	return projects, pageInfo{
		HasNextPage: query.Organization.ProjectsV2.PageInfo.HasNextPage,
		EndCursor:   query.Organization.ProjectsV2.PageInfo.EndCursor,
	}, nil
}

// GetIssuesWithProjectFieldsBatch fetches multiple issues with full detail
// (including author, milestone, labels, assignees) and project field values
// in a single GraphQL query. Optimized for the view command's batch mode.
//
// projectID scopes the returned field values to a single project board; see
// GetIssueWithProjectFields for the rationale. An empty projectID disables the
// filter.
func (c *Client) GetIssuesWithProjectFieldsBatch(projectID, owner, repo string, numbers []int) (map[int]*Issue, map[int][]FieldValue, map[int]error, error) {
	issues := make(map[int]*Issue)
	fieldValues := make(map[int][]FieldValue)
	issueErrors := make(map[int]error)

	if len(numbers) == 0 {
		return issues, fieldValues, issueErrors, nil
	}

	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, nil, nil, err
	}

	var queryParts []string
	for i, num := range numbers {
		queryParts = append(queryParts, fmt.Sprintf(`i%d: issue(number: %d) {
			id
			number
			title
			body
			state
			url
			author { login }
			assignees(first: 10) { nodes { login } }
			labels(first: 20) { nodes { name color } }
			milestone { title }
			projectItems(first: 10) {
				nodes {
					project { id }
					fieldValues(first: 20) {
						nodes {
							__typename
							... on ProjectV2ItemFieldSingleSelectValue {
								name
								field { ... on ProjectV2SingleSelectField { name } }
							}
							... on ProjectV2ItemFieldTextValue {
								text
								field { ... on ProjectV2Field { name } }
							}
						}
					}
				}
			}
		}`, i, num))
	}

	query := fmt.Sprintf(`query { repository(owner: %q, name: %q) { %s } }`,
		owner, repo, strings.Join(queryParts, " "))

	// Execute via go-gh HTTP client
	output, err := c.doRawGraphQL(query, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to execute batch issues query: %w", err)
	}

	return parseIssuesBatchResponse(output, numbers, projectID, owner, repo)
}

// parseIssuesBatchResponse parses the JSON response from a batch issues query.
// projectID scopes field values to a single project board (#856); an empty
// projectID disables that filter.
func parseIssuesBatchResponse(data []byte, numbers []int, projectID, owner, repo string) (map[int]*Issue, map[int][]FieldValue, map[int]error, error) {
	var response struct {
		Data struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
			// Path segments may be strings (field/alias names) or integers (list
			// indices) per the GraphQL spec, so decode as []interface{}; a numeric
			// segment must not fail the whole response unmarshal and abort the
			// batch (#861).
			Path []interface{} `json:"path"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse batch issues response: %w", err)
	}

	issues := make(map[int]*Issue)
	fieldValues := make(map[int][]FieldValue)
	issueErrors := make(map[int]error)

	// Map GraphQL errors to specific aliases. The alias segment (path[1], e.g.
	// "i0") is a string; ignore errors whose second segment is not a string.
	aliasErrors := make(map[string]string)
	for _, gqlErr := range response.Errors {
		if len(gqlErr.Path) >= 2 {
			if alias, ok := gqlErr.Path[1].(string); ok {
				aliasErrors[alias] = gqlErr.Message
			}
		}
	}

	for i, num := range numbers {
		alias := fmt.Sprintf("i%d", i)

		if errMsg, ok := aliasErrors[alias]; ok {
			issueErrors[num] = fmt.Errorf("%s", errMsg)
			continue
		}

		issueData, ok := response.Data.Repository[alias]
		if !ok || string(issueData) == "null" {
			issueErrors[num] = fmt.Errorf("issue #%d not found", num)
			continue
		}

		var raw struct {
			ID     string `json:"id"`
			Number int    `json:"number"`
			Title  string `json:"title"`
			Body   string `json:"body"`
			State  string `json:"state"`
			URL    string `json:"url"`
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			Assignees struct {
				Nodes []struct {
					Login string `json:"login"`
				} `json:"nodes"`
			} `json:"assignees"`
			Labels struct {
				Nodes []struct {
					Name  string `json:"name"`
					Color string `json:"color"`
				} `json:"nodes"`
			} `json:"labels"`
			Milestone struct {
				Title string `json:"title"`
			} `json:"milestone"`
			ProjectItems struct {
				Nodes []struct {
					Project struct {
						ID string `json:"id"`
					} `json:"project"`
					FieldValues struct {
						Nodes []rawFieldValueNode `json:"nodes"`
					} `json:"fieldValues"`
				} `json:"nodes"`
			} `json:"projectItems"`
		}

		if err := json.Unmarshal(issueData, &raw); err != nil {
			issueErrors[num] = fmt.Errorf("failed to parse issue #%d: %w", num, err)
			continue
		}

		issue := &Issue{
			ID:     raw.ID,
			Number: raw.Number,
			Title:  raw.Title,
			Body:   raw.Body,
			State:  raw.State,
			URL:    raw.URL,
			Repository: Repository{
				Owner: owner,
				Name:  repo,
			},
			Author: Actor{Login: raw.Author.Login},
		}

		for _, a := range raw.Assignees.Nodes {
			issue.Assignees = append(issue.Assignees, Actor{Login: a.Login})
		}
		for _, l := range raw.Labels.Nodes {
			issue.Labels = append(issue.Labels, Label{Name: l.Name, Color: l.Color})
		}
		if raw.Milestone.Title != "" {
			issue.Milestone = &Milestone{Title: raw.Milestone.Title}
		}

		issues[num] = issue

		var fvs []FieldValue
		for _, projectItem := range raw.ProjectItems.Nodes {
			if projectID != "" && projectItem.Project.ID != projectID {
				continue
			}
			fvs = appendRawFieldValues(fvs, projectItem.FieldValues.Nodes)
		}
		fieldValues[num] = fvs
	}

	return issues, fieldValues, issueErrors, nil
}

// GetParentIssueBatch fetches parent issues for multiple issues in a single query.
// Returns a map from child issue number to parent *Issue (nil if no parent).
func (c *Client) GetParentIssueBatch(owner, repo string, numbers []int) (map[int]*Issue, error) {
	result := make(map[int]*Issue)
	if len(numbers) == 0 {
		return result, nil
	}

	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}

	var queryParts []string
	for i, num := range numbers {
		queryParts = append(queryParts, fmt.Sprintf(`i%d: issue(number: %d) {
			parent {
				id
				number
				title
				state
				url
			}
		}`, i, num))
	}

	query := fmt.Sprintf(`query { repository(owner: %q, name: %q) { %s } }`,
		owner, repo, strings.Join(queryParts, " "))

	// Execute via go-gh HTTP client
	output, err := c.doRawGraphQL(query, map[string]string{"X-Github-Next": "sub_issues"})
	if err != nil {
		return nil, fmt.Errorf("failed to execute batch parent issue query: %w", err)
	}

	return parseParentIssueBatchResponse(output, numbers)
}

// parseParentIssueBatchResponse parses the JSON response from a batch parent issue query.
func parseParentIssueBatchResponse(data []byte, numbers []int) (map[int]*Issue, error) {
	var response struct {
		Data struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("failed to parse batch parent issue response: %w", err)
	}

	result := make(map[int]*Issue)

	for i, num := range numbers {
		alias := fmt.Sprintf("i%d", i)
		issueData, ok := response.Data.Repository[alias]
		if !ok || string(issueData) == "null" {
			result[num] = nil
			continue
		}

		var raw struct {
			Parent *struct {
				ID     string `json:"id"`
				Number int    `json:"number"`
				Title  string `json:"title"`
				State  string `json:"state"`
				URL    string `json:"url"`
			} `json:"parent"`
		}

		if err := json.Unmarshal(issueData, &raw); err != nil {
			// Warn rather than silently returning "no parent" for a malformed item.
			fmt.Fprintf(os.Stderr, "Warning: failed to parse parent issue for #%d; treating as no parent: %v\n", num, err)
			result[num] = nil
			continue
		}

		if raw.Parent == nil || raw.Parent.ID == "" {
			result[num] = nil
			continue
		}

		result[num] = &Issue{
			ID:     raw.Parent.ID,
			Number: raw.Parent.Number,
			Title:  raw.Parent.Title,
			State:  raw.Parent.State,
			URL:    raw.Parent.URL,
		}
	}

	return result, nil
}

// ListLabels fetches all labels from a repository with their name, color, and description.
func (c *Client) ListLabels(owner, repo string) ([]RepoLabel, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}

	var allLabels []RepoLabel
	var cursor *string

	for {
		var query struct {
			Repository struct {
				Labels struct {
					Nodes []struct {
						Name        string
						Color       string
						Description string
					}
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
				} `graphql:"labels(first: 100, after: $cursor)"`
			} `graphql:"repository(owner: $owner, name: $repo)"`
		}

		variables := map[string]interface{}{
			"owner":  graphql.String(owner),
			"repo":   graphql.String(repo),
			"cursor": (*graphql.String)(nil),
		}
		if cursor != nil {
			variables["cursor"] = graphql.String(*cursor)
		}

		err := c.gql.Query("ListLabels", &query, variables)
		if err != nil {
			return nil, fmt.Errorf("failed to list labels for %s/%s: %w", owner, repo, err)
		}

		for _, node := range query.Repository.Labels.Nodes {
			allLabels = append(allLabels, RepoLabel{
				Name:        node.Name,
				Color:       node.Color,
				Description: node.Description,
			})
		}

		if !query.Repository.Labels.PageInfo.HasNextPage {
			break
		}
		endCursor := query.Repository.Labels.PageInfo.EndCursor
		cursor = &endCursor
	}

	return allLabels, nil
}

// GetLabel fetches a single label from a repository with full metadata.
func (c *Client) GetLabel(owner, repo, labelName string) (*RepoLabel, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return nil, err
	}
	if err := validateLabelName(labelName); err != nil {
		return nil, err
	}

	var query struct {
		Repository struct {
			Label struct {
				Name        string
				Color       string
				Description string
			} `graphql:"label(name: $labelName)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	variables := map[string]interface{}{
		"owner":     graphql.String(owner),
		"repo":      graphql.String(repo),
		"labelName": graphql.String(labelName),
	}

	err := c.gql.Query("GetLabel", &query, variables)
	if err != nil {
		return nil, fmt.Errorf("failed to get label %q: %w", labelName, err)
	}

	if query.Repository.Label.Name == "" {
		return nil, nil
	}

	return &RepoLabel{
		Name:        query.Repository.Label.Name,
		Color:       query.Repository.Label.Color,
		Description: query.Repository.Label.Description,
	}, nil
}
