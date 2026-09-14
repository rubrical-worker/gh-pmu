package api

// IssueState represents GitHub issue state enum for GraphQL queries
type IssueState string

const (
	IssueStateOpen   IssueState = "OPEN"
	IssueStateClosed IssueState = "CLOSED"
)

// SearchFilters contains filters for searching repository issues
type SearchFilters struct {
	State    string   // "open", "closed", or "all"
	Labels   []string // Filter by label names
	Assignee string   // Filter by assignee login
	Search   string   // Free-text search in title/body
}

// Project represents a GitHub Projects v2 project
type Project struct {
	ID     string
	Number int
	Title  string
	URL    string
	Owner  ProjectOwner
	Closed bool
}

// ProjectOwner represents the owner of a project
type ProjectOwner struct {
	Type  string // "User" or "Organization"
	Login string
}

// ProjectView represents a view (board, table or roadmap tab) within a
// GitHub Projects v2 project.
//
// Number is a creation ordinal starting at 1 that is never backfilled, so it
// is not a position: deleting the first view leaves the second numbered 2.
// Order comes from the connection, not from this field (#901).
type ProjectView struct {
	ID     string
	Number int
	Name   string
	Layout string // ProjectV2ViewLayout: BOARD_LAYOUT, TABLE_LAYOUT or ROADMAP_LAYOUT
}

// ProjectField represents a field in a GitHub project
type ProjectField struct {
	ID       string
	Name     string
	DataType string
	Options  []FieldOption // For SINGLE_SELECT fields
	// FromCache is true when the field came from .gh-pmu.json cached metadata
	// because the live resolver was unavailable. Cached fields carry only IDs
	// and names and may be stale, so they must never drive a mutation (#917).
	FromCache bool
}

// FieldOption represents an option for a single-select field
type FieldOption struct {
	ID          string
	Name        string
	Color       string
	Description string
}

// Issue represents a GitHub issue
type Issue struct {
	ID            string
	Number        int
	Title         string
	Body          string
	State         string
	URL           string
	Repository    Repository
	Author        Actor
	Assignees     []Actor
	Labels        []Label
	Milestone     *Milestone
	SubIssueCount int
}

// Repository represents a GitHub repository
type Repository struct {
	Owner string
	Name  string
}

// Actor represents a GitHub user or bot
type Actor struct {
	Login string
}

// Label represents a GitHub label
type Label struct {
	Name  string
	Color string
}

// RepoLabel represents a GitHub label with full metadata
type RepoLabel struct {
	Name        string
	Color       string
	Description string
}

// Milestone represents a GitHub milestone
type Milestone struct {
	Title string
}

// ProjectItem represents an issue or PR within a project
type ProjectItem struct {
	ID          string
	Issue       *Issue
	FieldValues []FieldValue
}

// FieldValue represents a field value on a project item
type FieldValue struct {
	Field string // Field name
	Value string // Resolved value
}

// SubIssue represents a sub-issue relationship
type SubIssue struct {
	ID         string
	Number     int
	Title      string
	State      string
	URL        string
	ParentID   string
	Repository Repository // Repository where the sub-issue lives
}

// BoardItem represents a minimal project item for board display.
// Contains only the fields needed for the board view to minimize API data transfer.
type BoardItem struct {
	Number     int
	Title      string
	State      string // Issue state: "OPEN" or "CLOSED"
	Status     string
	Priority   string
	Repository string // "owner/repo" format for filtering
}

// IssueRef represents a reference to a GitHub issue by owner/repo/number.
// Used for targeted queries that fetch specific issues instead of all project items.
type IssueRef struct {
	Owner  string
	Repo   string
	Number int
}

// MinimalProjectItem represents a project item with minimal issue data.
// Used for two-phase queries: first fetch minimal data for filtering,
// then fetch full details only for matching items.
// This avoids fetching Title, Body, Assignees, Labels for non-matching items.
type MinimalProjectItem struct {
	IssueID     string       // GitHub node ID for API operations
	IssueNumber int          // Issue number for display and IssueRef
	IssueState  string       // "OPEN" or "CLOSED" for filtering
	Repository  string       // "owner/repo" format
	FieldValues []FieldValue // Project field values for filtering
}
