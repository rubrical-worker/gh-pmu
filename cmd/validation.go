package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
)

// Regex patterns for checkbox detection.
// GitHub renders task-list items using any of the -, *, + bullet markers and
// treats [x] and [X] as checked; these patterns match all of those forms.
var (
	checkedBoxRegex    = regexp.MustCompile(`(?m)^\s*[-*+] \[[xX]\]`)
	uncheckedBoxRegex  = regexp.MustCompile(`(?m)^\s*[-*+] \[ \]`)
	uncheckedItemRegex = regexp.MustCompile(`(?m)^\s*[-*+] \[ \] (.+)`)
)

// ValidationError represents a validation failure with actionable message
type ValidationError struct {
	IssueNumber int
	Message     string
	Suggestion  string
}

func (e *ValidationError) Error() string {
	if e.Suggestion != "" {
		return fmt.Sprintf("Issue #%d: %s\n\n%s", e.IssueNumber, e.Message, e.Suggestion)
	}
	return fmt.Sprintf("Issue #%d: %s", e.IssueNumber, e.Message)
}

// ValidationErrors collects multiple validation failures
type ValidationErrors struct {
	Errors []ValidationError
}

func (e *ValidationErrors) Error() string {
	if len(e.Errors) == 0 {
		return ""
	}
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Validation failed for %d issues:\n", len(e.Errors)))
	for _, err := range e.Errors {
		sb.WriteString(fmt.Sprintf("\n  - Issue #%d: %s", err.IssueNumber, err.Message))
	}
	return sb.String()
}

func (e *ValidationErrors) Add(err ValidationError) {
	e.Errors = append(e.Errors, err)
}

func (e *ValidationErrors) HasErrors() bool {
	return len(e.Errors) > 0
}

// issueValidationContext holds all info needed to validate an issue
type issueValidationContext struct {
	Number         int
	CurrentStatus  string
	CurrentRelease string
	Body           string
	ActiveReleases []string // Discovered from GitHub release tracker issues
}

// validateStatusTransition checks IDPF rules for a status transition
// Set force=true to bypass checkbox validation (but NOT body or release requirements)
func validateStatusTransition(cfg *config.Config, ctx *issueValidationContext, targetStatus, targetRelease string, force bool) *ValidationError {
	// Skip validation if not using IDPF
	if !cfg.IsIDPF() {
		return nil
	}

	// Normalize status values for comparison
	currentStatus := strings.ToLower(ctx.CurrentStatus)
	targetStatusLower := strings.ToLower(targetStatus)

	// Rule 1: Body required for in_review/done (NOT bypassed by --force)
	if targetStatusLower == "in_review" || targetStatusLower == "in review" || targetStatusLower == "done" {
		if isBodyEmpty(ctx.Body) {
			return &ValidationError{
				IssueNumber: ctx.Number,
				Message:     fmt.Sprintf("Empty body. Cannot move to '%s' without issue content.", targetStatus),
				Suggestion:  fmt.Sprintf("Use: gh issue edit %d --body \"<description>\"", ctx.Number),
			}
		}
	}

	// Rule 2: All checkboxes must be checked for in_review/done (bypassed by --force).
	// A line ending in a configured gate marker is unchecked by design and
	// does not count (#923).
	if checksCheckboxes(targetStatus) {
		blocking, _ := partitionUncheckedItems(ctx.Body, cfg.CheckboxGateMarkers)
		if len(blocking) > 0 && !force {
			itemList := "\n" + strings.Join(blocking, "\n")
			return &ValidationError{
				IssueNumber: ctx.Number,
				Message:     fmt.Sprintf("Has %d unchecked checkbox(es):%s", len(blocking), itemList),
				Suggestion:  fmt.Sprintf("Complete these items before moving to %s, or use --force to bypass.\nClaude: Review GitHub-Workflow rules before using --force.", targetStatus),
			}
		}
	}

	// Rule 3: Release required for backlog → ready/in_progress
	if currentStatus == "backlog" && (targetStatusLower == "ready" || targetStatusLower == "in progress" || targetStatusLower == "in_progress") {
		// Check if release is being set or already set
		releaseValue := targetRelease
		if releaseValue == "" {
			releaseValue = ctx.CurrentRelease
		}

		if releaseValue == "" {
			return &ValidationError{
				IssueNumber: ctx.Number,
				Message:     fmt.Sprintf("No branch assignment. Cannot move from 'backlog' to '%s' without a branch.", targetStatus),
				Suggestion:  fmt.Sprintf("Use: gh pmu move %d --branch vX.Y.Z", ctx.Number),
			}
		}

		// Validate release exists in active releases (if we have discovered releases)
		if !isReleaseActiveInContext(ctx.ActiveReleases, releaseValue) {
			suggestion := "Use 'gh pmu branch start' to create a new branch."
			if len(ctx.ActiveReleases) > 0 {
				suggestion = fmt.Sprintf("Available branches: %s\n\n%s", strings.Join(ctx.ActiveReleases, ", "), suggestion)
			}
			return &ValidationError{
				IssueNumber: ctx.Number,
				Message:     fmt.Sprintf("Branch \"%s\" not found in active branches.", releaseValue),
				Suggestion:  suggestion,
			}
		}
	}

	return nil
}

// isReleaseActiveInContext checks if a release name exists in the discovered active releases
func isReleaseActiveInContext(activeReleases []string, releaseName string) bool {
	// If no active releases discovered, allow any release (backwards compatibility)
	if len(activeReleases) == 0 {
		return true
	}

	for _, active := range activeReleases {
		if strings.EqualFold(active, releaseName) {
			return true
		}
	}

	return false
}

// discoverActiveReleases fetches active branch names from GitHub issues with "branch" label
// Returns the extracted branch names (e.g., "release/v1.2.0") from issue titles like "Branch: release/v1.2.0"
// Supports both "Branch: " (new) and "Release: " (legacy) prefixes for backwards compatibility
func discoverActiveReleases(issues []api.Issue) []string {
	var releases []string
	for _, issue := range issues {
		var version string
		if strings.HasPrefix(issue.Title, "Branch: ") {
			// Extract version from title (e.g., "Branch: release/v1.2.0" or "Branch: release/v1.2.0 (Phoenix)")
			version = strings.TrimPrefix(issue.Title, "Branch: ")
		} else if strings.HasPrefix(issue.Title, "Release: ") {
			// Legacy format: "Release: v1.2.0" or "Release: v1.2.0 (Phoenix)"
			version = strings.TrimPrefix(issue.Title, "Release: ")
		} else {
			continue
		}
		// Remove codename in parentheses if present
		if idx := strings.Index(version, " ("); idx > 0 {
			version = version[:idx]
		}
		releases = append(releases, strings.TrimSpace(version))
	}
	return releases
}

// isBodyEmpty checks if the body is empty (empty string or whitespace only)
func isBodyEmpty(body string) bool {
	return strings.TrimSpace(body) == ""
}

// fenceDelimiter describes a line that opens or closes a fenced code block.
type fenceDelimiter struct {
	char   byte // '`' or '~'
	length int  // how many fence characters the run holds
}

// stripBlockquotePrefix removes a leading blockquote marker run and returns the
// rest of the line. Nested markers ("> > ") are consumed together, and the one
// optional space after the final marker belongs to the marker rather than to
// the content.
func stripBlockquotePrefix(line string) string {
	i := 0
	for {
		j := i
		for j < len(line) && line[j] == ' ' {
			j++
		}
		if j >= len(line) || line[j] != '>' {
			break
		}
		i = j + 1
	}
	if i > 0 && i < len(line) && line[i] == ' ' {
		i++
	}
	return line[i:]
}

// parseFenceDelimiter reports whether line is a fenced-code delimiter.
//
// The test runs against the RAW line, not a trimmed one. Trimming first was the
// defect (#907): it made indentation invisible, so a fence at any depth toggled
// the block state, and a fence indented inside an open block closed it early.
//
// The rules implemented are the ones the issue names: an optional blockquote
// prefix, then at most three spaces of indent, then a run of at least three
// backticks or tildes. A run indented four or more is content, and a leading
// tab counts as indented code for the same reason.
func parseFenceDelimiter(line string) (fenceDelimiter, bool) {
	rest := stripBlockquotePrefix(line)

	indent := 0
	for indent < len(rest) && rest[indent] == ' ' {
		indent++
	}
	if indent > 3 {
		return fenceDelimiter{}, false
	}
	rest = rest[indent:]
	if len(rest) < 3 {
		return fenceDelimiter{}, false
	}

	char := rest[0]
	if char != '`' && char != '~' {
		return fenceDelimiter{}, false
	}
	length := 0
	for length < len(rest) && rest[length] == char {
		length++
	}
	if length < 3 {
		return fenceDelimiter{}, false
	}

	return fenceDelimiter{char: char, length: length}, true
}

// listItemIndent returns the visual indent of a markdown list item line and
// whether the line is one. A tab counts as four columns, matching the width
// the indented-code test uses.
func listItemIndent(line string) (int, bool) {
	indent := 0
	i := 0
	for i < len(line) {
		if line[i] == 0x20 {
			indent++
		} else if line[i] == 0x09 {
			indent += 4
		} else {
			break
		}
		i++
	}

	rest := line[i:]
	if len(rest) < 2 || rest[1] != 0x20 {
		return 0, false
	}
	if rest[0] != 0x2d && rest[0] != 0x2a && rest[0] != 0x2b {
		return 0, false
	}

	return indent, true
}

// continuesShallowerListItem reports whether the line at index i is a nested
// list item rather than the start of an indented code block: an indented list
// item whose nearest preceding non-blank line is itself a list item at a
// shallower indent.
//
// This is the discriminator that replaced the checkbox carve-out (#908). The
// carve-out exempted any line containing "- [ ]" or "- [x]" from indented-code
// stripping, which disabled the branch for exactly the lines it exists to
// strip. It was protecting something real — the originating proposal requires
// that nested and indented checkboxes be validated equally — but it read the
// glyph instead of the structure, so it also exempted prose that merely
// mentioned a checkbox.
func continuesShallowerListItem(lines []string, i int) bool {
	indent, isItem := listItemIndent(lines[i])
	if !isItem {
		return false
	}

	for j := i - 1; j >= 0; j-- {
		if strings.TrimSpace(lines[j]) == "" {
			continue // a blank line separates a loose list, it does not end one
		}
		prevIndent, prevIsItem := listItemIndent(lines[j])
		return prevIsItem && prevIndent < indent
	}

	return false
}

// isBlockquoted reports whether line begins with a blockquote marker, allowing
// the same 0-3 spaces of indent CommonMark allows before one. Four or more is
// indented code and is handled by that branch instead.
func isBlockquoted(line string) bool {
	indent := 0
	for indent < len(line) && line[indent] == 0x20 {
		indent++
	}
	if indent > 3 {
		return false
	}

	return indent < len(line) && line[indent] == 0x3e
}

// stripCodeBlocks removes content inside fenced code blocks (``` or ~~~) and
// indented code blocks (4 spaces or tab) from the body. This prevents example
// checkboxes in code blocks from being counted as acceptance criteria.
func stripCodeBlocks(body string) string {
	lines := strings.Split(body, "\n")
	var filteredLines []string
	inFencedCodeBlock := false
	var openFence fenceDelimiter
	inIndentedCodeBlock := false

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Fenced code block start/end (``` or ~~~), tested against the raw line
		// so indentation and any blockquote prefix are visible (#907).
		if fence, isDelimiter := parseFenceDelimiter(line); isDelimiter {
			if !inFencedCodeBlock {
				inFencedCodeBlock = true
				openFence = fence
				continue // Skip the opening fence line
			}
			// A closer must use the same character and be at least as long as
			// its opener, so a longer outer fence encloses a shorter quoted one.
			if fence.char == openFence.char && fence.length >= openFence.length {
				inFencedCodeBlock = false
				openFence = fenceDelimiter{}
				continue // Skip the closing fence line
			}
			// Too short, or the other fence character: content, not a delimiter.
			continue
		}

		// Skip content inside fenced code blocks. A fence indented four or more
		// spaces reaches here rather than the branch above, which is what makes
		// it content instead of an early closer.
		if inFencedCodeBlock {
			continue
		}

		// A blockquote is quoted content — an example, a citation, or someone
		// else's criterion — so its checkboxes are not criteria of this issue
		// (#911). Excluding them here rather than teaching the checkbox patterns
		// a > prefix keeps the exclusion symmetric and stated: a quoted - [x] and
		// a quoted - [ ] are removed by one named mechanism instead of both
		// happening to score zero for unrelated reasons.
		//
		// This runs AFTER the fence branch on purpose. A blockquoted fence is a
		// delimiter (#907), and consuming it here would leave its block unclosed.
		if isBlockquoted(line) {
			continue
		}

		// Check for indented code block (4+ spaces or tab at start)
		// But not if it's a list item (- [ ] or - [x] pattern)
		isIndented := strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")

		// List context, not the checkbox glyph, decides whether an indented line
		// is code (#908). A nested list item is legitimately indented and must
		// keep counting; a line following a paragraph is code and must not.
		isIndentedCode := isIndented && !continuesShallowerListItem(lines, i)

		// Check if previous line was blank (indented code blocks are preceded by blank line)
		prevLineBlank := i == 0 || strings.TrimSpace(lines[i-1]) == ""

		if isIndentedCode && (inIndentedCodeBlock || prevLineBlank) {
			inIndentedCodeBlock = true
			continue // Skip this line (it's part of an indented code block)
		} else if trimmedLine == "" && inIndentedCodeBlock {
			// Blank line might end the code block, but keep checking
			continue
		} else {
			inIndentedCodeBlock = false
			filteredLines = append(filteredLines, line)
		}
	}

	return strings.Join(filteredLines, "\n")
}

// countUncheckedBoxes counts the number of unchecked checkboxes in the body,
// excluding checkboxes inside code blocks (which are examples, not criteria).
func countUncheckedBoxes(body string) int {
	strippedBody := stripCodeBlocks(body)
	return len(uncheckedBoxRegex.FindAllString(strippedBody, -1))
}

// countCheckedBoxes counts the number of checked checkboxes in the body,
// excluding checkboxes inside code blocks.
func countCheckedBoxes(body string) int {
	strippedBody := stripCodeBlocks(body)
	return len(checkedBoxRegex.FindAllString(strippedBody, -1))
}

// countCodeBlockCheckboxes counts checkboxes inside code blocks (for informational messages).
// Returns the count of both checked and unchecked checkboxes found in code blocks.
func countCodeBlockCheckboxes(body string) int {
	totalBefore := len(uncheckedBoxRegex.FindAllString(body, -1)) + len(checkedBoxRegex.FindAllString(body, -1))
	strippedBody := stripCodeBlocks(body)
	totalAfter := len(uncheckedBoxRegex.FindAllString(strippedBody, -1)) + len(checkedBoxRegex.FindAllString(strippedBody, -1))
	return totalBefore - totalAfter
}

// getUncheckedItems extracts the text of all unchecked checkbox items,
// excluding checkboxes inside code blocks.
func getUncheckedItems(body string) []string {
	items, _ := partitionUncheckedItems(body, nil)
	return items
}

// partitionUncheckedItems splits the unchecked checkbox items outside code
// blocks into those that block a transition and those waived because they end
// in a configured gate marker (#923). Both are formatted as getUncheckedItems
// formats them. With no markers configured, every item blocks.
func partitionUncheckedItems(body string, markers []string) (blocking, waived []string) {
	matches := uncheckedItemRegex.FindAllStringSubmatch(stripCodeBlocks(body), -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		item := "  [ ] " + strings.TrimSpace(match[1])
		if matchesGateMarker(match[1], markers) {
			waived = append(waived, item)
		} else {
			blocking = append(blocking, item)
		}
	}
	return blocking, waived
}

// checksCheckboxes reports whether a target status is one whose transition
// validates checkboxes (in_review and done).
func checksCheckboxes(targetStatus string) bool {
	s := strings.ToLower(targetStatus)
	return s == "in_review" || s == "in review" || s == "done"
}

// trimItemPrefixes strips the "  [ ] " prefix partitionUncheckedItems adds, for
// reports that list items under their own heading.
func trimItemPrefixes(items []string) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = strings.TrimPrefix(item, "  [ ] ")
	}
	return out
}

// matchesGateMarker reports whether a checkbox line's text ends with one of
// the configured markers, ignoring trailing whitespace. A marker ending in
// "#N" matches "#" followed by one or more digits, so "→ QA: #N" recognizes
// "→ QA: #2755". Empty entries match nothing, so a stray "" cannot waive
// every line.
func matchesGateMarker(line string, markers []string) bool {
	line = strings.TrimRight(line, " \t\r")
	for _, marker := range markers {
		if strings.TrimSpace(marker) == "" {
			continue
		}
		if prefix, ok := strings.CutSuffix(marker, "#N"); ok {
			trimmed := strings.TrimRight(line, "0123456789")
			if len(trimmed) < len(line) && strings.HasSuffix(trimmed, prefix+"#") {
				return true
			}
			continue
		}
		if strings.HasSuffix(line, marker) {
			return true
		}
	}
	return false
}

// getFieldValueFromSlice extracts a field value from a slice of field values
func getFieldValueFromSlice(fieldValues []api.FieldValue, fieldName string) string {
	for _, fv := range fieldValues {
		if strings.EqualFold(fv.Field, fieldName) {
			return fv.Value
		}
	}
	return ""
}

// buildValidationContext creates a validation context from project item data
func buildValidationContext(number int, body string, fieldValues []api.FieldValue, activeReleases []string) *issueValidationContext {
	// Check both "Branch" (new) and "Release" (legacy) field names for backward compatibility
	currentBranch := getFieldValueFromSlice(fieldValues, BranchFieldName)
	if currentBranch == "" {
		currentBranch = getFieldValueFromSlice(fieldValues, LegacyReleaseFieldName)
	}
	return &issueValidationContext{
		Number:         number,
		CurrentStatus:  getFieldValueFromSlice(fieldValues, "Status"),
		CurrentRelease: currentBranch,
		Body:           body,
		ActiveReleases: activeReleases,
	}
}
