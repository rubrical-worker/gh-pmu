package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
)

// =============================================================================
// REQ-366: Release-Gated Progression Validation Tests
// =============================================================================

func TestValidateStatusTransition_NonIDPF(t *testing.T) {
	cfg := &config.Config{Framework: "none"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "backlog",
	}

	err := validateStatusTransition(cfg, ctx, "ready", "", false)
	if err != nil {
		t.Errorf("Expected no error for non-IDPF framework, got: %v", err)
	}
}

func TestValidateStatusTransition_BacklogToReadyWithoutRelease(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:         1,
		CurrentStatus:  "backlog",
		CurrentRelease: "",
	}

	err := validateStatusTransition(cfg, ctx, "ready", "", false)
	if err == nil {
		t.Fatal("Expected validation error for backlog->ready without release")
	}

	if err.IssueNumber != 1 {
		t.Errorf("Expected issue number 1, got %d", err.IssueNumber)
	}

	if !strings.Contains(err.Message, "No branch assignment") {
		t.Errorf("Expected message about branch assignment, got: %s", err.Message)
	}

	if !strings.Contains(err.Suggestion, "--branch") {
		t.Errorf("Expected suggestion with --branch flag, got: %s", err.Suggestion)
	}
}

func TestValidateStatusTransition_BacklogToInProgressWithoutRelease(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:         2,
		CurrentStatus:  "backlog",
		CurrentRelease: "",
	}

	err := validateStatusTransition(cfg, ctx, "in_progress", "", false)
	if err == nil {
		t.Fatal("Expected validation error for backlog->in_progress without release")
	}

	if !strings.Contains(err.Message, "in_progress") {
		t.Errorf("Expected message mentioning target status, got: %s", err.Message)
	}
}

func TestValidateStatusTransition_BacklogToReadyWithExistingRelease(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:         1,
		CurrentStatus:  "backlog",
		CurrentRelease: "release/v1.0.0",
	}

	err := validateStatusTransition(cfg, ctx, "ready", "", false)
	if err != nil {
		t.Errorf("Expected no error when issue already has release, got: %v", err)
	}
}

func TestValidateStatusTransition_BacklogToReadyWithNewRelease(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:         1,
		CurrentStatus:  "backlog",
		CurrentRelease: "",
	}

	err := validateStatusTransition(cfg, ctx, "ready", "release/v2.0.0", false)
	if err != nil {
		t.Errorf("Expected no error when setting release with move, got: %v", err)
	}
}

func TestValidateStatusTransition_ReadyToInProgress(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "ready",
		Body:          "Issue content",
	}

	err := validateStatusTransition(cfg, ctx, "in_progress", "", false)
	if err != nil {
		t.Errorf("Expected no error for ready->in_progress, got: %v", err)
	}
}

func TestValidateStatusTransition_InProgressToInReview(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_progress",
		Body:          "Issue content for review",
	}

	err := validateStatusTransition(cfg, ctx, "in_review", "", false)
	if err != nil {
		t.Errorf("Expected no error for in_progress->in_review, got: %v", err)
	}
}

func TestValidateStatusTransition_CaseInsensitiveStatus(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:         1,
		CurrentStatus:  "Backlog",
		CurrentRelease: "",
	}

	err := validateStatusTransition(cfg, ctx, "Ready", "", false)
	if err == nil {
		t.Fatal("Expected validation error with mixed case status values")
	}
}

func TestValidateStatusTransition_InProgressSpaceVariant(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:         1,
		CurrentStatus:  "backlog",
		CurrentRelease: "",
	}

	// Test "in progress" with space (common variant)
	err := validateStatusTransition(cfg, ctx, "in progress", "", false)
	if err == nil {
		t.Fatal("Expected validation error for backlog->in progress without release")
	}
}

// =============================================================================
// #923: Configured gate markers
// =============================================================================

var testGateMarkers = []string{"→ QA: #N", "→ GATE: review", "→ GATE: release"}

func TestMatchesGateMarker(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"QA with number", "Manual check passes → QA: #2755", true},
		{"QA with trailing whitespace", "Manual check passes → QA: #2755  \r", true},
		{"QA without number", "Manual check passes → QA: #", false},
		{"QA with trailing text", "Manual check → QA: #2755 (later)", false},
		{"QA with non-digit", "Manual check → QA: #abc", false},
		{"GATE review", "Reviewer approves the wording → GATE: review", true},
		{"GATE release", "Asset verified → GATE: release", true},
		{"bare GATE", "Something → GATE:", false},
		{"unconfigured token", "Something → GATE: later", false},
		{"marker mid-line only", "→ GATE: review then more work", false},
		{"no marker", "Implement the parser", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesGateMarker(tt.line, testGateMarkers); got != tt.want {
				t.Errorf("matchesGateMarker(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestMatchesGateMarker_EmptyEntryMatchesNothing(t *testing.T) {
	// An empty entry would otherwise be a suffix of every line and waive all.
	if matchesGateMarker("Unfinished work", []string{"", "   "}) {
		t.Error("empty/whitespace markers must not match")
	}
}

func TestPartitionUncheckedItems(t *testing.T) {
	body := "## AC\n- [x] Done\n- [ ] Real work\n- [ ] Manual check → QA: #12\n* [ ] Approve → GATE: review\n" +
		"```\n- [ ] example in code → GATE: review\n```\n"

	blocking, waived := partitionUncheckedItems(body, testGateMarkers)

	if len(blocking) != 1 || !strings.Contains(blocking[0], "Real work") {
		t.Errorf("blocking = %v, want only 'Real work'", blocking)
	}
	if len(waived) != 2 {
		t.Errorf("waived = %v, want the QA and GATE lines (code block excluded)", waived)
	}
}

func TestValidateStatusTransition_MarkerOnlyUncheckedPasses(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF", CheckboxGateMarkers: testGateMarkers}
	ctx := &issueValidationContext{
		Number:        7,
		CurrentStatus: "in_progress",
		Body:          "## AC\n- [x] Built\n- [ ] Manual check → QA: #2755\n- [ ] Notes reviewed → GATE: review",
	}

	if err := validateStatusTransition(cfg, ctx, "in_review", "", false); err != nil {
		t.Errorf("Expected marker-only unchecked lines to pass, got: %v", err)
	}
}

func TestValidateStatusTransition_MixedUncheckedBlocksNamingBareLines(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF", CheckboxGateMarkers: testGateMarkers}
	ctx := &issueValidationContext{
		Number:        8,
		CurrentStatus: "in_progress",
		Body:          "## AC\n- [ ] Manual check → QA: #2755\n- [ ] Still to build\n- [ ] Odd token → GATE:",
	}

	err := validateStatusTransition(cfg, ctx, "in_review", "", false)
	if err == nil {
		t.Fatal("Expected mixed unchecked lines to block")
	}
	if !strings.Contains(err.Message, "Has 2 unchecked checkbox(es)") {
		t.Errorf("Expected count of the 2 non-marker lines, got: %s", err.Message)
	}
	if !strings.Contains(err.Message, "Still to build") || !strings.Contains(err.Message, "Odd token") {
		t.Errorf("Expected blocking lines named, got: %s", err.Message)
	}
	if strings.Contains(err.Message, "QA: #2755") {
		t.Errorf("Marker line must not be listed as blocking, got: %s", err.Message)
	}
}

func TestValidateStatusTransition_NoMarkersConfiguredUnchanged(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        9,
		CurrentStatus: "in_progress",
		Body:          "## AC\n- [ ] Manual check → QA: #2755",
	}

	err := validateStatusTransition(cfg, ctx, "in_review", "", false)
	if err == nil || !strings.Contains(err.Message, "Has 1 unchecked checkbox(es)") {
		t.Errorf("Expected unconfigured default to block as before, got: %v", err)
	}
}

// =============================================================================
// REQ-367: Body Required for Review/Done
// =============================================================================

func TestValidateStatusTransition_InReviewWithEmptyBody(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_progress",
		Body:          "",
	}

	err := validateStatusTransition(cfg, ctx, "in_review", "", false)
	if err == nil {
		t.Fatal("Expected validation error for in_review with empty body")
	}

	if !strings.Contains(err.Message, "Empty body") {
		t.Errorf("Expected message about empty body, got: %s", err.Message)
	}
}

func TestValidateStatusTransition_DoneWithEmptyBody(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        2,
		CurrentStatus: "in_review",
		Body:          "   \n\t  ", // whitespace only
	}

	err := validateStatusTransition(cfg, ctx, "done", "", false)
	if err == nil {
		t.Fatal("Expected validation error for done with whitespace-only body")
	}
}

func TestValidateStatusTransition_InReviewWithBody(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_progress",
		Body:          "This issue has actual content.",
	}

	err := validateStatusTransition(cfg, ctx, "in_review", "", false)
	if err != nil {
		t.Errorf("Expected no error for in_review with body content, got: %v", err)
	}
}

func TestValidateStatusTransition_DoneWithBody(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_review",
		Body:          "Acceptance criteria met.",
	}

	err := validateStatusTransition(cfg, ctx, "done", "", false)
	if err != nil {
		t.Errorf("Expected no error for done with body content, got: %v", err)
	}
}

func TestValidateStatusTransition_InReviewSpaceVariant(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_progress",
		Body:          "",
	}

	// Test "in review" with space (common variant)
	err := validateStatusTransition(cfg, ctx, "in review", "", false)
	if err == nil {
		t.Fatal("Expected validation error for 'in review' with empty body")
	}
}

// =============================================================================
// REQ-368: Checkbox Validation
// =============================================================================

func TestValidateStatusTransition_DoneWithUncheckedBoxes(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_review",
		Body: `## Acceptance Criteria
- [x] First task completed
- [ ] Second task not done
- [x] Third task completed`,
	}

	err := validateStatusTransition(cfg, ctx, "done", "", false)
	if err == nil {
		t.Fatal("Expected validation error for done with unchecked boxes")
	}

	if !strings.Contains(err.Message, "1 unchecked") {
		t.Errorf("Expected message about unchecked boxes, got: %s", err.Message)
	}
}

func TestValidateStatusTransition_DoneWithAllBoxesChecked(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_review",
		Body: `## Acceptance Criteria
- [x] First task completed
- [x] Second task completed
- [x] Third task completed`,
	}

	err := validateStatusTransition(cfg, ctx, "done", "", false)
	if err != nil {
		t.Errorf("Expected no error for done with all boxes checked, got: %v", err)
	}
}

func TestValidateStatusTransition_DoneWithNoCheckboxes(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_review",
		Body:          "Simple issue with no checkboxes.",
	}

	err := validateStatusTransition(cfg, ctx, "done", "", false)
	if err != nil {
		t.Errorf("Expected no error for done with no checkboxes, got: %v", err)
	}
}

func TestValidateStatusTransition_DoneWithMultipleUnchecked(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_review",
		Body: `- [ ] Task 1
- [ ] Task 2
- [ ] Task 3`,
	}

	err := validateStatusTransition(cfg, ctx, "done", "", false)
	if err == nil {
		t.Fatal("Expected validation error for multiple unchecked boxes")
	}

	if !strings.Contains(err.Message, "3 unchecked") {
		t.Errorf("Expected message about 3 unchecked boxes, got: %s", err.Message)
	}
}

func TestValidateStatusTransition_InReviewBlocksUncheckedBoxes(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_progress",
		Body: `- [ ] Not done yet
- [x] This one is done`,
	}

	err := validateStatusTransition(cfg, ctx, "in_review", "", false)
	if err == nil {
		t.Fatal("Expected validation error for in_review with unchecked boxes")
	}

	if !strings.Contains(err.Message, "1 unchecked") {
		t.Errorf("Expected message about unchecked boxes, got: %s", err.Message)
	}
}

func TestValidateStatusTransition_InReviewWithForce(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_progress",
		Body: `- [ ] Not done yet
- [x] This one is done`,
	}

	// With force=true, should allow unchecked boxes
	err := validateStatusTransition(cfg, ctx, "in_review", "", true)
	if err != nil {
		t.Errorf("Expected no error for in_review with --force, got: %v", err)
	}
}

func TestValidateStatusTransition_DoneWithForce(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_review",
		Body: `- [ ] Task 1
- [ ] Task 2`,
	}

	// With force=true, should allow unchecked boxes
	err := validateStatusTransition(cfg, ctx, "done", "", true)
	if err != nil {
		t.Errorf("Expected no error for done with --force, got: %v", err)
	}
}

func TestValidateStatusTransition_ForceDoesNotBypassEmptyBody(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_progress",
		Body:          "", // Empty body
	}

	// Force should NOT bypass body requirement
	err := validateStatusTransition(cfg, ctx, "done", "", true)
	if err == nil {
		t.Fatal("Expected validation error for empty body even with --force")
	}

	if !strings.Contains(err.Message, "Empty body") {
		t.Errorf("Expected error about empty body, got: %s", err.Message)
	}
}

func TestValidateStatusTransition_ForceDoesNotBypassReleaseRequirement(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "backlog",
		Body:          "Content",
	}

	// Force should NOT bypass branch requirement
	err := validateStatusTransition(cfg, ctx, "ready", "", true)
	if err == nil {
		t.Fatal("Expected validation error for missing branch even with --force")
	}

	if !strings.Contains(err.Message, "No branch") {
		t.Errorf("Expected error about branch, got: %s", err.Message)
	}
}

func TestCountUncheckedBoxes(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected int
	}{
		{"no checkboxes", "Plain text", 0},
		{"all checked", "- [x] Done\n- [x] Also done", 0},
		{"one unchecked", "- [ ] Todo\n- [x] Done", 1},
		{"all unchecked", "- [ ] One\n- [ ] Two\n- [ ] Three", 3},
		{"mixed", "- [x] A\n- [ ] B\n- [x] C\n- [ ] D", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countUncheckedBoxes(tt.body)
			if result != tt.expected {
				t.Errorf("countUncheckedBoxes() = %d, want %d", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// #866: Alternate bullet markers (* / +) and uppercase [X] Tests
// =============================================================================

func TestCountUncheckedBoxes_AlternateBullets(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected int
	}{
		{"asterisk bullet unchecked", "* [ ] Todo", 1},
		{"plus bullet unchecked", "+ [ ] Todo", 1},
		{"mixed bullet markers", "- [ ] A\n* [ ] B\n+ [ ] C", 3},
		{"leading whitespace unchecked", "  * [ ] Indented todo", 1},
		{"asterisk checked not counted as unchecked", "* [x] Done", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countUncheckedBoxes(tt.body)
			if result != tt.expected {
				t.Errorf("countUncheckedBoxes() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestCountCheckedBoxes_AlternateBulletsAndUppercase(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected int
	}{
		{"uppercase X with dash", "- [X] Done", 1},
		{"asterisk bullet checked", "* [x] Done", 1},
		{"plus bullet uppercase X", "+ [X] Done", 1},
		{"mixed markers and case", "- [x] A\n* [X] B\n+ [x] C", 3},
		{"leading whitespace uppercase", "  - [X] Indented done", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countCheckedBoxes(tt.body)
			if result != tt.expected {
				t.Errorf("countCheckedBoxes() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestGetUncheckedItems_AlternateBullets(t *testing.T) {
	body := "* [ ] First item\n+ [ ] Second item\n- [x] Done item"
	items := getUncheckedItems(body)
	if len(items) != 2 {
		t.Fatalf("getUncheckedItems() returned %d items, want 2: %v", len(items), items)
	}
	if !strings.Contains(items[0], "First item") {
		t.Errorf("first item = %q, want to contain 'First item'", items[0])
	}
	if !strings.Contains(items[1], "Second item") {
		t.Errorf("second item = %q, want to contain 'Second item'", items[1])
	}
}

func TestValidateStatusTransition_BlocksAlternateBulletUnchecked(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF-Agile"}
	ctx := &issueValidationContext{
		Number:        866,
		CurrentStatus: "In progress",
		Body:          "## Acceptance Criteria\n* [ ] Not done yet",
	}

	err := validateStatusTransition(cfg, ctx, "done", "", false)
	if err == nil {
		t.Fatal("expected validation error for unchecked '* [ ]' checkbox, got nil (silent bypass)")
	}
}

func TestValidateStatusTransition_UppercaseXCountsAsChecked(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF-Agile"}
	ctx := &issueValidationContext{
		Number:        866,
		CurrentStatus: "In progress",
		Body:          "## Acceptance Criteria\n- [X] All done",
	}

	err := validateStatusTransition(cfg, ctx, "done", "", false)
	if err != nil {
		t.Fatalf("expected no error for checked '- [X]' checkbox, got: %v", err)
	}
}

// =============================================================================
// REQ-419: Code Block Checkbox Exclusion Tests
// =============================================================================

func TestStripCodeBlocks(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "no code blocks",
			body:     "Plain text\n- [ ] Todo",
			expected: "Plain text\n- [ ] Todo",
		},
		{
			name:     "fenced code block with backticks",
			body:     "Before\n```\n- [ ] Example\n```\nAfter",
			expected: "Before\nAfter",
		},
		{
			name:     "fenced code block with tildes",
			body:     "Before\n~~~\n- [ ] Example\n~~~\nAfter",
			expected: "Before\nAfter",
		},
		{
			name:     "fenced code block with language",
			body:     "Before\n```markdown\n- [ ] Example\n```\nAfter",
			expected: "Before\nAfter",
		},
		{
			name: "multiple fenced code blocks",
			body: `- [ ] Real task
` + "```" + `
- [ ] Example 1
` + "```" + `
- [ ] Another real task
` + "```" + `
- [ ] Example 2
` + "```",
			expected: "- [ ] Real task\n- [ ] Another real task",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripCodeBlocks(tt.body)
			if result != tt.expected {
				t.Errorf("stripCodeBlocks() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestCountUncheckedBoxes_ExcludesCodeBlocks(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected int
	}{
		{
			name: "checkboxes in fenced code block excluded",
			body: `## Acceptance Criteria
- [ ] Real task 1
- [x] Real task 2

` + "```" + `markdown
### Example
- [ ] Example checkbox 1
- [ ] Example checkbox 2
` + "```",
			expected: 1, // Only the real unchecked task, not the 2 in code block
		},
		{
			name: "mixed real and example checkboxes",
			body: `- [ ] Do this
- [x] Done

` + "```" + `
- [ ] Not a real task
` + "```" + `

- [ ] Also do this`,
			expected: 2, // Two real unchecked tasks
		},
		{
			name:     "only code block checkboxes",
			body:     "```\n- [ ] Example\n- [ ] Another example\n```",
			expected: 0, // All are in code block
		},
		{
			name: "nested markdown in code block",
			body: `## Proposed Structure

` + "```" + `markdown
### Manual Testing Checklist
- [ ] ` + "`gh pmu init --non-interactive`" + ` - non-interactive setup works
- [ ] ` + "`gh pmu board`" + ` - kanban display renders correctly
` + "```" + `

## Real Criteria
- [x] All done`,
			expected: 0, // No unchecked outside code block
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countUncheckedBoxes(tt.body)
			if result != tt.expected {
				t.Errorf("countUncheckedBoxes() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestCountCodeBlockCheckboxes(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected int
	}{
		{
			name:     "no code blocks",
			body:     "- [ ] Task\n- [x] Done",
			expected: 0,
		},
		{
			name:     "checkboxes in code block",
			body:     "```\n- [ ] Example\n- [x] Done example\n```",
			expected: 2,
		},
		{
			name: "mixed",
			body: `- [ ] Real
` + "```" + `
- [ ] Example 1
- [x] Example 2
` + "```" + `
- [x] Real done`,
			expected: 2, // 2 in code block
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countCodeBlockCheckboxes(tt.body)
			if result != tt.expected {
				t.Errorf("countCodeBlockCheckboxes() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestGetUncheckedItems_ExcludesCodeBlocks(t *testing.T) {
	body := `## Criteria
- [ ] Real task 1
- [x] Done task

` + "```" + `
- [ ] Example in code
` + "```" + `

- [ ] Real task 2`

	items := getUncheckedItems(body)

	if len(items) != 2 {
		t.Errorf("Expected 2 unchecked items, got %d", len(items))
	}

	// Verify the example checkbox is not included
	for _, item := range items {
		if strings.Contains(item, "Example") {
			t.Errorf("Code block checkbox should not be included: %s", item)
		}
	}
}

func TestValidateStatusTransition_IgnoresCodeBlockCheckboxes(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_review",
		Body: `## Summary
This issue proposes a new testing structure.

## Proposed TESTING.md Structure

` + "```" + `markdown
### Manual Testing Checklist
- [ ] ` + "`gh pmu init --non-interactive`" + ` - non-interactive setup works
- [ ] ` + "`gh pmu board`" + ` - kanban display renders correctly
` + "```" + `

## Acceptance Criteria
- [x] Create TESTING.md
- [x] Document coverage philosophy`,
	}

	// Should pass validation - code block checkboxes are ignored
	err := validateStatusTransition(cfg, ctx, "done", "", false)
	if err != nil {
		t.Errorf("Expected no error when code block checkboxes are present but real criteria are checked, got: %v", err)
	}
}

func TestValidateStatusTransition_StillFailsWithRealUnchecked(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:        1,
		CurrentStatus: "in_review",
		Body: `## Example

` + "```" + `
- [ ] Example checkbox (ignored)
` + "```" + `

## Acceptance Criteria
- [x] Done task
- [ ] Not done yet`,
	}

	// Should fail - there's a real unchecked checkbox
	err := validateStatusTransition(cfg, ctx, "done", "", false)
	if err == nil {
		t.Fatal("Expected validation error for real unchecked checkbox")
	}

	if !strings.Contains(err.Message, "1 unchecked") {
		t.Errorf("Expected message about 1 unchecked checkbox, got: %s", err.Message)
	}
}

func TestCountCheckedBoxes(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected int
	}{
		{"no checkboxes", "Plain text", 0},
		{"all checked", "- [x] Done\n- [x] Also done", 2},
		{"one checked", "- [ ] Todo\n- [x] Done", 1},
		{"none checked", "- [ ] One\n- [ ] Two\n- [ ] Three", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countCheckedBoxes(tt.body)
			if result != tt.expected {
				t.Errorf("countCheckedBoxes() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestGetUncheckedItems(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected []string
	}{
		{"no checkboxes", "Plain text", nil},
		{"all checked", "- [x] Done\n- [x] Also done", nil},
		{"one unchecked", "- [ ] Todo\n- [x] Done", []string{"  [ ] Todo"}},
		{"multiple unchecked", "- [ ] First\n- [ ] Second\n- [x] Done", []string{"  [ ] First", "  [ ] Second"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getUncheckedItems(tt.body)
			if len(result) != len(tt.expected) {
				t.Errorf("getUncheckedItems() returned %d items, want %d", len(result), len(tt.expected))
				return
			}
			for i, item := range tt.expected {
				if result[i] != item {
					t.Errorf("getUncheckedItems()[%d] = %q, want %q", i, result[i], item)
				}
			}
		})
	}
}

func TestIsBodyEmpty(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected bool
	}{
		{"empty string", "", true},
		{"only spaces", "   ", true},
		{"only newlines", "\n\n", true},
		{"only tabs", "\t\t", true},
		{"mixed whitespace", "  \n\t  ", true},
		{"actual content", "Hello", false},
		{"content with whitespace", "  Hello  ", false},
		{"single character", "x", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBodyEmpty(tt.body)
			if result != tt.expected {
				t.Errorf("isBodyEmpty(%q) = %v, want %v", tt.body, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// REQ-369: Batch and Recursive Validation
// =============================================================================

func TestValidationErrors_BatchCollection(t *testing.T) {
	// Test that multiple validation errors are collected and reported together
	cfg := &config.Config{Framework: "IDPF"}
	var errs ValidationErrors

	// Issue 1: No branch for backlog->ready
	ctx1 := &issueValidationContext{
		Number:        1,
		CurrentStatus: "backlog",
		Body:          "Issue 1 content",
	}
	if err := validateStatusTransition(cfg, ctx1, "ready", "", false); err != nil {
		errs.Add(*err)
	}

	// Issue 2: Empty body for done
	ctx2 := &issueValidationContext{
		Number:        2,
		CurrentStatus: "in_review",
		Body:          "",
	}
	if err := validateStatusTransition(cfg, ctx2, "done", "", false); err != nil {
		errs.Add(*err)
	}

	// Issue 3: Unchecked boxes for done
	ctx3 := &issueValidationContext{
		Number:        3,
		CurrentStatus: "in_review",
		Body:          "- [ ] Not done",
	}
	if err := validateStatusTransition(cfg, ctx3, "done", "", false); err != nil {
		errs.Add(*err)
	}

	if !errs.HasErrors() {
		t.Fatal("Expected validation errors to be collected")
	}

	if len(errs.Errors) != 3 {
		t.Errorf("Expected 3 errors, got %d", len(errs.Errors))
	}

	errorStr := errs.Error()
	if !strings.Contains(errorStr, "#1") || !strings.Contains(errorStr, "#2") || !strings.Contains(errorStr, "#3") {
		t.Errorf("Expected all issue numbers in batch error message, got: %s", errorStr)
	}
}

func TestValidationErrors_PartialBatchSuccess(t *testing.T) {
	// Test that valid issues don't generate errors while invalid ones do
	cfg := &config.Config{Framework: "IDPF"}
	var errs ValidationErrors

	// Valid transition - has release
	ctx1 := &issueValidationContext{
		Number:         1,
		CurrentStatus:  "backlog",
		CurrentRelease: "v1.0.0",
		Body:           "Issue content",
	}
	if err := validateStatusTransition(cfg, ctx1, "ready", "", false); err != nil {
		errs.Add(*err)
	}

	// Invalid transition - no branch
	ctx2 := &issueValidationContext{
		Number:        2,
		CurrentStatus: "backlog",
		Body:          "Issue content",
	}
	if err := validateStatusTransition(cfg, ctx2, "ready", "", false); err != nil {
		errs.Add(*err)
	}

	if len(errs.Errors) != 1 {
		t.Errorf("Expected 1 error (only invalid issue), got %d", len(errs.Errors))
	}

	if errs.Errors[0].IssueNumber != 2 {
		t.Errorf("Expected error for issue #2, got #%d", errs.Errors[0].IssueNumber)
	}
}

func TestValidationErrors_AllOrNothingBehavior(t *testing.T) {
	// Simulate the all-or-nothing pattern: collect all errors before returning
	cfg := &config.Config{Framework: "IDPF"}

	issues := []struct {
		number int
		status string
		body   string
	}{
		{1, "in_review", ""},           // Invalid: empty body
		{2, "in_review", "Content"},    // Valid
		{3, "in_review", "- [ ] Task"}, // Invalid: unchecked box
		{4, "in_review", "- [x] Done"}, // Valid
	}

	var errs ValidationErrors
	for _, issue := range issues {
		ctx := &issueValidationContext{
			Number:        issue.number,
			CurrentStatus: issue.status,
			Body:          issue.body,
		}
		if err := validateStatusTransition(cfg, ctx, "done", "", false); err != nil {
			errs.Add(*err)
		}
	}

	// Should have collected 2 errors (issues 1 and 3)
	if len(errs.Errors) != 2 {
		t.Errorf("Expected 2 errors, got %d", len(errs.Errors))
	}

	// Verify error messages are useful for batch operations
	fullError := errs.Error()
	if !strings.Contains(fullError, "2 issues") {
		t.Errorf("Expected batch error header, got: %s", fullError)
	}
}

// =============================================================================
// ValidationError Tests
// =============================================================================

func TestValidationError_ErrorString(t *testing.T) {
	err := &ValidationError{
		IssueNumber: 42,
		Message:     "Test error message",
	}

	result := err.Error()
	if !strings.Contains(result, "#42") {
		t.Errorf("Expected issue number in error, got: %s", result)
	}
	if !strings.Contains(result, "Test error message") {
		t.Errorf("Expected message in error, got: %s", result)
	}
}

func TestValidationError_ErrorStringWithSuggestion(t *testing.T) {
	err := &ValidationError{
		IssueNumber: 42,
		Message:     "Test error",
		Suggestion:  "Try this instead",
	}

	result := err.Error()
	if !strings.Contains(result, "Try this instead") {
		t.Errorf("Expected suggestion in error, got: %s", result)
	}
}

// =============================================================================
// ValidationErrors Tests
// =============================================================================

func TestValidationErrors_Empty(t *testing.T) {
	errs := &ValidationErrors{}

	if errs.HasErrors() {
		t.Error("Expected HasErrors() to return false for empty errors")
	}

	if errs.Error() != "" {
		t.Errorf("Expected empty error string, got: %s", errs.Error())
	}
}

func TestValidationErrors_SingleError(t *testing.T) {
	errs := &ValidationErrors{}
	errs.Add(ValidationError{
		IssueNumber: 1,
		Message:     "First error",
	})

	if !errs.HasErrors() {
		t.Error("Expected HasErrors() to return true")
	}

	result := errs.Error()
	if !strings.Contains(result, "#1") {
		t.Errorf("Expected issue number in error, got: %s", result)
	}
}

func TestValidationErrors_MultipleErrors(t *testing.T) {
	errs := &ValidationErrors{}
	errs.Add(ValidationError{IssueNumber: 1, Message: "First"})
	errs.Add(ValidationError{IssueNumber: 2, Message: "Second"})
	errs.Add(ValidationError{IssueNumber: 3, Message: "Third"})

	result := errs.Error()
	if !strings.Contains(result, "3 issues") {
		t.Errorf("Expected count in error header, got: %s", result)
	}
	if !strings.Contains(result, "#1") || !strings.Contains(result, "#2") || !strings.Contains(result, "#3") {
		t.Errorf("Expected all issue numbers in error, got: %s", result)
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestGetFieldValueFromSlice(t *testing.T) {
	fieldValues := []api.FieldValue{
		{Field: "Status", Value: "In Progress"},
		{Field: "Release", Value: "v1.0.0"},
		{Field: "Priority", Value: "High"},
	}

	status := getFieldValueFromSlice(fieldValues, "Status")
	if status != "In Progress" {
		t.Errorf("Expected 'In Progress', got '%s'", status)
	}

	release := getFieldValueFromSlice(fieldValues, "Release")
	if release != "v1.0.0" {
		t.Errorf("Expected 'v1.0.0', got '%s'", release)
	}

	missing := getFieldValueFromSlice(fieldValues, "NonExistent")
	if missing != "" {
		t.Errorf("Expected empty string for missing field, got '%s'", missing)
	}
}

func TestGetFieldValueFromSlice_CaseInsensitive(t *testing.T) {
	fieldValues := []api.FieldValue{
		{Field: "Status", Value: "Done"},
	}

	result := getFieldValueFromSlice(fieldValues, "status")
	if result != "Done" {
		t.Errorf("Expected case-insensitive match, got '%s'", result)
	}
}

// =============================================================================
// REQ-371: Active Release Discovery
// =============================================================================

func TestDiscoverActiveReleases(t *testing.T) {
	tests := []struct {
		name     string
		issues   []api.Issue
		expected []string
	}{
		{
			name:     "no issues",
			issues:   nil,
			expected: nil,
		},
		{
			name: "single release",
			issues: []api.Issue{
				{Title: "Release: v1.0.0"},
			},
			expected: []string{"v1.0.0"},
		},
		{
			name: "multiple releases",
			issues: []api.Issue{
				{Title: "Release: v1.0.0"},
				{Title: "Release: v1.1.0"},
				{Title: "Release: v2.0.0"},
			},
			expected: []string{"v1.0.0", "v1.1.0", "v2.0.0"},
		},
		{
			name: "release with codename",
			issues: []api.Issue{
				{Title: "Release: v1.0.0 (Phoenix)"},
			},
			expected: []string{"v1.0.0"},
		},
		{
			name: "non-release issues filtered",
			issues: []api.Issue{
				{Title: "Release: v1.0.0"},
				{Title: "Some other issue"},
				{Title: "Feature: New feature"},
			},
			expected: []string{"v1.0.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := discoverActiveReleases(tt.issues)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d releases, got %d", len(tt.expected), len(result))
				return
			}
			for i, exp := range tt.expected {
				if result[i] != exp {
					t.Errorf("Expected release[%d] = %q, got %q", i, exp, result[i])
				}
			}
		})
	}
}

func TestIsReleaseActiveInContext(t *testing.T) {
	tests := []struct {
		name           string
		activeReleases []string
		releaseName    string
		expected       bool
	}{
		{"no active releases - allow any", nil, "v1.0.0", true},
		{"empty active releases - allow any", []string{}, "v1.0.0", true},
		{"exact match", []string{"v1.0.0", "v2.0.0"}, "v1.0.0", true},
		{"not found", []string{"v1.0.0", "v2.0.0"}, "v3.0.0", false},
		{"case insensitive", []string{"v1.0.0"}, "V1.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isReleaseActiveInContext(tt.activeReleases, tt.releaseName)
			if result != tt.expected {
				t.Errorf("isReleaseActiveInContext(%v, %q) = %v, want %v", tt.activeReleases, tt.releaseName, result, tt.expected)
			}
		})
	}
}

func TestValidateStatusTransition_InvalidRelease(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:         1,
		CurrentStatus:  "backlog",
		Body:           "Issue content",
		ActiveReleases: []string{"v1.0.0", "v2.0.0"},
	}

	err := validateStatusTransition(cfg, ctx, "ready", "v3.0.0", false)
	if err == nil {
		t.Fatal("Expected validation error for invalid branch")
	}

	if !strings.Contains(err.Message, "not found in active branches") {
		t.Errorf("Expected message about invalid branch, got: %s", err.Message)
	}

	if !strings.Contains(err.Suggestion, "v1.0.0") {
		t.Errorf("Expected suggestion to list available branches, got: %s", err.Suggestion)
	}
}

func TestValidateStatusTransition_ValidRelease(t *testing.T) {
	cfg := &config.Config{Framework: "IDPF"}
	ctx := &issueValidationContext{
		Number:         1,
		CurrentStatus:  "backlog",
		Body:           "Issue content",
		ActiveReleases: []string{"v1.0.0", "v2.0.0"},
	}

	err := validateStatusTransition(cfg, ctx, "ready", "v1.0.0", false)
	if err != nil {
		t.Errorf("Expected no error for valid release, got: %v", err)
	}
}

func TestBuildValidationContext(t *testing.T) {
	fieldValues := []api.FieldValue{
		{Field: "Status", Value: "Backlog"},
		{Field: "Release", Value: "v2.0.0"},
	}
	activeReleases := []string{"v1.0.0", "v2.0.0"}

	ctx := buildValidationContext(42, "Issue body content", fieldValues, activeReleases)

	if ctx.Number != 42 {
		t.Errorf("Expected number 42, got %d", ctx.Number)
	}
	if ctx.CurrentStatus != "Backlog" {
		t.Errorf("Expected status 'Backlog', got '%s'", ctx.CurrentStatus)
	}
	if ctx.CurrentRelease != "v2.0.0" {
		t.Errorf("Expected release 'v2.0.0', got '%s'", ctx.CurrentRelease)
	}
	if ctx.Body != "Issue body content" {
		t.Errorf("Expected body 'Issue body content', got '%s'", ctx.Body)
	}
	if len(ctx.ActiveReleases) != 2 {
		t.Errorf("Expected 2 active releases, got %d", len(ctx.ActiveReleases))
	}
}

// stripCodeBlocks tested the fence delimiter against the TRIMMED line, so any
// indentation was invisible to it and a fence indented to any depth toggled the
// block state (#907). It also compared only the first three characters, so a
// short quoted fence closed a longer outer one. The cases below are the four
// the issue's acceptance criteria name; the existing five in TestStripCodeBlocks
// cover the flat, column-0 shapes and stay green.
func TestStripCodeBlocks_FenceDelimiterRules(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			// A closing fence may be indented 0-3 spaces. At 4+ it is content,
			// so the block stays open and everything after it belongs to the
			// block — including a checkbox that would otherwise be counted.
			name:     "fence indented four spaces while a block is open is content",
			body:     "```\n- [ ] example\n    ```\n- [ ] swallowed",
			expected: "",
		},
		{
			// A quoted fence shorter than its opener must not close it. This is
			// how a markdown sample containing a fence is written.
			name:     "closing fence shorter than its opener does not close",
			body:     "````\n- [ ] example\n```\n- [ ] still inside\n````\n- [ ] real",
			expected: "- [ ] real",
		},
		{
			// The 0-3 bound is a bound, not a requirement of column 0.
			name:     "fence at three spaces is still a delimiter",
			body:     "Before\n   ```\n- [ ] example\n   ```\nAfter",
			expected: "Before\nAfter",
		},
		{
			// A fence written inside a blockquote delimits a block like any
			// other. Without this, its contents are never stripped.
			name:     "blockquote-prefixed fence is recognized",
			body:     "Before\n> ```\n> - [ ] example\n> ```\nAfter",
			expected: "Before\nAfter",
		},
		{
			// The case that keeps this criterion earning its place after #911
			// strips blockquoted lines: the fence lines carry the > prefix but
			// the content does not, so a blockquote-stripping rule would leave
			// the checkbox behind and only fence recognition reaches it.
			name:     "blockquote-prefixed fence strips non-blockquoted content",
			body:     "> ```\n- [ ] example\n> ```",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result := stripCodeBlocks(tt.body); result != tt.expected {
				t.Errorf("stripCodeBlocks() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestFenceRulesReachTheCountingFunctions asserts the shared-path inheritance
// rather than assuming it. All four counters call stripCodeBlocks, but
// countCodeBlockCheckboxes is the one that could still be wrong when the other
// three are right: it subtracts a post-strip count from a pre-strip count, so a
// change in stripping moves both of its operands.
func TestFenceRulesReachTheCountingFunctions(t *testing.T) {
	// One real unchecked criterion, one real checked one, and two examples
	// inside a blockquoted fence that the old delimiter test never recognized.
	//
	// The example lines deliberately carry NO blockquote prefix. Prefixing them
	// would make them invisible to the checkbox patterns themselves (#911), and
	// countCodeBlockCheckboxes would then correctly report 0 — it counts boxes
	// the patterns can see that stripping removed, so a box the patterns never
	// matched cannot be one of them. Leaving them unprefixed also makes this the
	// mixed-fence case, which is the one blockquote stripping alone cannot reach.
	body := "- [ ] real unchecked\n- [x] real checked\n> ```\n- [ ] example one\n- [x] example two\n> ```"

	if got := countUncheckedBoxes(body); got != 1 {
		t.Errorf("countUncheckedBoxes() = %d, want 1", got)
	}
	if got := countCheckedBoxes(body); got != 1 {
		t.Errorf("countCheckedBoxes() = %d, want 1", got)
	}
	if got := countCodeBlockCheckboxes(body); got != 2 {
		t.Errorf("countCodeBlockCheckboxes() = %d, want 2", got)
	}
	items := getUncheckedItems(body)
	if len(items) != 1 {
		t.Fatalf("getUncheckedItems() returned %d items, want 1: %v", len(items), items)
	}
	if !strings.Contains(items[0], "real unchecked") {
		t.Errorf("getUncheckedItems() = %v, want the real criterion", items)
	}
}

// isIndentedCode exempted any line containing "- [ ]" or "- [x]" from
// indented-code stripping (#908), which disabled the branch precisely for the
// lines it exists to strip. The carve-out was not arbitrary: the originating
// proposal requires that all checkboxes be validated equally, including nested
// and indented ones, so deleting it outright drops a real nested list item.
//
// The discriminator is list context, not the checkbox glyph. An indented
// checkbox whose nearest preceding non-blank line is a shallower list item is
// a nested list item; one that follows a paragraph is code.
func TestStripCodeBlocks_IndentedCodeVersusNestedList(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			// The defect. Prose, blank line, indented checkbox: that is a code
			// block, and the carve-out was letting it through.
			name:     "checkbox in a four-space indented code block is stripped",
			body:     "Prose.\n\n    - [ ] example\n\n- [ ] real",
			expected: "Prose.\n\n- [ ] real",
		},
		{
			// The case that justifies a discriminator rather than a deletion. On
			// pure deletion of the carve-out this drops from 1 to 0 — a real
			// criterion silently lost, which is the dangerous direction.
			name:     "loose nested list item is not stripped",
			body:     "- parent\n\n    - [ ] child",
			expected: "- parent\n\n    - [ ] child",
		},
		{
			// Same regression, tab-indented. Measured separately because the
			// indent test has two arms and only one of them is spaces.
			name:     "tab-indented loose nested list item is not stripped",
			body:     "- parent\n\n\t- [ ] child",
			expected: "- parent\n\n\t- [ ] child",
		},
		{
			// A no-regression case, not the one that proves the fix: it already
			// passes on pure deletion, guarded by prevLineBlank.
			name:     "tight nested list item is not stripped",
			body:     "- parent\n    - [ ] child",
			expected: "- parent\n    - [ ] child",
		},
		{
			// The strings.Contains test was unanchored, so an indented line that
			// merely mentioned the glyph escaped stripping too.
			name:     "indented prose mentioning a checkbox glyph is stripped",
			body:     "Prose.\n\n    write - [ ] to mark a task\n\n- [ ] real",
			expected: "Prose.\n\n- [ ] real",
		},
		{
			name:     "tab-indented code block is stripped like a four-space one",
			body:     "Prose.\n\n\t- [ ] example\n\n- [ ] real",
			expected: "Prose.\n\n- [ ] real",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result := stripCodeBlocks(tt.body); result != tt.expected {
				t.Errorf("stripCodeBlocks() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestStripCodeBlocks_RecordedRegressionBody exercises the real body that
// surfaced both defects — rubrical-worker/idpf-praxis-dev#2600 — rather than a
// reduction of it. A reduction is what the existing fixture space already was,
// and it is why neither defect was caught: every prior case used flat,
// column-0 fences containing a single unindented checkbox.
//
// Measured across all three states of the tree, so the number below is a
// verified change rather than a restatement of current behaviour:
//
//	neither fix   1 unchecked ("quoted example box")
//	#907 only     0 unchecked
//	both          0 unchecked
//
// Note the middle row. This issue's Dependency section states that the body
// "only yields 0 unchecked once both land". That is not what the tree does:
// #907 alone is sufficient for this body, and #908 changes nothing about it.
// The ordering #907 before #908 was still the right call, but not for the
// reason recorded — see the correction on the issue.
func TestStripCodeBlocks_RecordedRegressionBody(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "idpf-praxis-dev-2600.md"))
	if err != nil {
		t.Fatalf("Failed to read the recorded body: %v", err)
	}
	body := string(data)

	if got := countUncheckedBoxes(body); got != 0 {
		t.Errorf("countUncheckedBoxes() = %d, want 0. Unchecked: %v", got, getUncheckedItems(body))
	}

	// A body that strips down to nothing would also report 0 unchecked, which
	// would pass the assertion above for entirely the wrong reason. Asserting
	// the checked count holds the stripping to removing only the examples.
	if got := countCheckedBoxes(body); got != 18 {
		t.Errorf("countCheckedBoxes() = %d, want 18 — stripping removed real content", got)
	}
}

// The Open Question on #911 was answered "do not count it": a blockquote is
// quoted content, so its checkboxes are not criteria of the issue quoting them.
// Implemented as a stripping rule rather than a blockquote-tolerant regex.
//
// These cases assert on stripCodeBlocks OUTPUT, not on the counting functions,
// and that is deliberate — see TestBlockquoteRuleDoesNotChangeAnyCount below
// for why counting cannot show this change.
func TestStripCodeBlocks_BlockquotedContent(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "single blockquote marker",
			body:     "- [ ] real\n> - [ ] quoted",
			expected: "- [ ] real",
		},
		{
			name:     "nested blockquote markers",
			body:     "- [ ] real\n> > - [ ] quoted",
			expected: "- [ ] real",
		},
		{
			// No space after the marker is still a blockquote in CommonMark.
			name:     "no space after the marker",
			body:     "- [ ] real\n>- [ ] quoted",
			expected: "- [ ] real",
		},
		{
			name:     "two spaces after the marker",
			body:     "- [ ] real\n>  - [ ] quoted",
			expected: "- [ ] real",
		},
		{
			// Symmetry: the checked form is excluded by the same mechanism. Before
			// this rule both forms scored zero, but for unrelated reasons — the
			// unchecked one unmatched, the checked one unmatched differently.
			name:     "checked and unchecked quoted forms are both removed",
			body:     "- [ ] real\n> - [ ] quoted unchecked\n> - [x] quoted checked",
			expected: "- [ ] real",
		},
		{
			// Three spaces is still a blockquote; four is indented code and is
			// handled by the other branch, which must keep its list-context test.
			name:     "blockquote indented three spaces",
			body:     "- [ ] real\n   > - [ ] quoted",
			expected: "- [ ] real",
		},
		{
			// The rule must not consume a blockquoted FENCE, which is a delimiter
			// (#907). Consuming it here would leave the block unclosed and swallow
			// everything after it.
			name:     "blockquoted fence still delimits rather than being stripped",
			body:     "Before\n> ```\n- [ ] example\n> ```\nAfter",
			expected: "Before\nAfter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result := stripCodeBlocks(tt.body); result != tt.expected {
				t.Errorf("stripCodeBlocks() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestBlockquoteRuleDoesNotChangeAnyCount records what the blockquote rule is
// and is not worth. Measured against the tree with and without it: all four
// counting functions return identical values either way.
//
// The reason is that the checkbox patterns anchor to ^\\s*[-*+], and \\s does not
// match >. A blockquoted checkbox was never matched, so it was never counted,
// so removing it from the count removes nothing. The rule buys two things
// instead: the exclusion becomes a stated mechanism rather than an accident of
// regex anchoring, and it survives any future change that teaches those
// patterns a > prefix — at which point this test would start failing loudly
// rather than quoted examples silently becoming criteria.
//
// This is the honest version of the sixth acceptance criterion. Asserting a
// count changed here would have required a fixture the rule does not actually
// affect.
func TestBlockquoteRuleDoesNotChangeAnyCount(t *testing.T) {
	body := "- [ ] real unchecked\n- [x] real checked\n> - [ ] quoted unchecked\n> - [x] quoted checked"

	if got := countUncheckedBoxes(body); got != 1 {
		t.Errorf("countUncheckedBoxes() = %d, want 1", got)
	}
	if got := countCheckedBoxes(body); got != 1 {
		t.Errorf("countCheckedBoxes() = %d, want 1", got)
	}
	// Zero, not two. countCodeBlockCheckboxes subtracts a post-strip count from
	// a pre-strip count using the same patterns, so a box the patterns never
	// matched cannot appear in either operand.
	if got := countCodeBlockCheckboxes(body); got != 0 {
		t.Errorf("countCodeBlockCheckboxes() = %d, want 0", got)
	}
	items := getUncheckedItems(body)
	if len(items) != 1 || !strings.Contains(items[0], "real unchecked") {
		t.Errorf("getUncheckedItems() = %v, want just the real criterion", items)
	}

	// Where the rule IS observable: the quoted lines are gone from the stripped
	// document, so anything reading that output sees the exclusion.
	if got := stripCodeBlocks(body); got != "- [ ] real unchecked\n- [x] real checked" {
		t.Errorf("stripCodeBlocks() = %q, want the quoted lines removed", got)
	}
}
