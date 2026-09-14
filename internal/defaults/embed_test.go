package defaults

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoad(t *testing.T) {
	defs, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if defs == nil {
		t.Fatal("Load() returned nil")
	}
}

func TestLoad_HasLabels(t *testing.T) {
	defs, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	expectedLabels := []string{"branch", "epic", "story", "proposal", "prd", "bug", "enhancement", "qa-required", "test-plan", "security-required", "legal-required", "docs-required", "emergency", "approval-required", "blocked", "scope-creep", "tech-debt", "active", "reviewed", "pending", "security-finding", "assigned", "auto-filed"}

	if len(defs.Labels) != len(expectedLabels) {
		t.Errorf("expected %d labels, got %d", len(expectedLabels), len(defs.Labels))
	}

	labelNames := make(map[string]bool)
	for _, label := range defs.Labels {
		labelNames[label.Name] = true
	}

	for _, expected := range expectedLabels {
		if !labelNames[expected] {
			t.Errorf("expected label %q not found", expected)
		}
	}
}

// TestLoad_AutoFiledLabel (#915): the hall-monitor label, with a color no other
// standard label uses.
func TestLoad_AutoFiledLabel(t *testing.T) {
	defs := MustLoad()
	label := defs.GetLabel("auto-filed")
	if label == nil {
		t.Fatal("auto-filed label not defined")
	}
	if label.Description != "Issue filed by the hall-monitor" {
		t.Errorf("description = %q, want %q", label.Description, "Issue filed by the hall-monitor")
	}
	if label.Color != "116329" {
		t.Errorf("color = %q, want 116329", label.Color)
	}
	for _, other := range defs.Labels {
		if other.Name != "auto-filed" && strings.EqualFold(other.Color, "116329") {
			t.Errorf("color 116329 is also used by %q", other.Name)
		}
	}
}

func TestLoad_LabelProperties(t *testing.T) {
	defs, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	for _, label := range defs.Labels {
		if label.Name == "" {
			t.Error("label has empty name")
		}
		if label.Color == "" {
			t.Errorf("label %q has empty color", label.Name)
		}
		if label.Description == "" {
			t.Errorf("label %q has empty description", label.Name)
		}
	}
}

func TestLoad_HasRequiredFields(t *testing.T) {
	defs, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(defs.Fields.Required) == 0 {
		t.Error("expected at least one required field")
	}

	// Status should be required
	var statusField *FieldDef
	for i := range defs.Fields.Required {
		if defs.Fields.Required[i].Name == "Status" {
			statusField = &defs.Fields.Required[i]
			break
		}
	}

	if statusField == nil {
		t.Fatal("Status field not found in required fields")
	}

	if statusField.Type != "SINGLE_SELECT" {
		t.Errorf("Status field type = %q, want SINGLE_SELECT", statusField.Type)
	}

	// #917: the 9-value Status minimum, exact names, template order.
	expectedOptions := []string{"Backlog", "Up next", "Ready", "In progress", "In review", "QA required", "Done", "Parking Lot", "Notes"}
	if len(statusField.Options) != len(expectedOptions) {
		t.Fatalf("Status field has %d options, want %d: %v", len(statusField.Options), len(expectedOptions), statusField.Options)
	}
	for i, want := range expectedOptions {
		if statusField.Options[i] != want {
			t.Errorf("Status option[%d] = %q, want %q", i, statusField.Options[i], want)
		}
	}

	validColors := map[string]bool{"GRAY": true, "BLUE": true, "GREEN": true, "YELLOW": true, "ORANGE": true, "RED": true, "PINK": true, "PURPLE": true}
	if len(statusField.OptionDefs) != len(expectedOptions) {
		t.Fatalf("Status field has %d option definitions, want %d", len(statusField.OptionDefs), len(expectedOptions))
	}
	for i, def := range statusField.OptionDefs {
		if def.Name != expectedOptions[i] {
			t.Errorf("OptionDefs[%d].Name = %q, want %q", i, def.Name, expectedOptions[i])
		}
		if !validColors[def.Color] {
			t.Errorf("Status option %q has color %q, want one of GitHub's 8 option colors", def.Name, def.Color)
		}
		if def.Description == "" {
			t.Errorf("Status option %q has no default description", def.Name)
		}
	}
}

// TestFieldDef_OptionsAcceptStringsAndMappings: an option entry may be a bare
// name or a {name, color, description} mapping, and Options always carries the
// names so existing []string callers are unaffected (#917).
func TestFieldDef_OptionsAcceptStringsAndMappings(t *testing.T) {
	src := `
name: Mixed
type: SINGLE_SELECT
options:
  - Plain
  - name: Rich
    color: BLUE
    description: A described option
`
	var fd FieldDef
	if err := yaml.Unmarshal([]byte(src), &fd); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if want := []string{"Plain", "Rich"}; !reflect.DeepEqual(fd.Options, want) {
		t.Errorf("Options = %v, want %v", fd.Options, want)
	}
	wantDefs := []OptionDef{{Name: "Plain"}, {Name: "Rich", Color: "BLUE", Description: "A described option"}}
	if !reflect.DeepEqual(fd.OptionDefs, wantDefs) {
		t.Errorf("OptionDefs = %+v, want %+v", fd.OptionDefs, wantDefs)
	}

	var bad FieldDef
	if err := yaml.Unmarshal([]byte("name: X\noptions:\n  - [nested]\n"), &bad); err == nil {
		t.Error("expected an error for an option that is neither a string nor a mapping")
	}
}

func TestFieldDef_OptionDefByName(t *testing.T) {
	defs := MustLoad()
	status := defs.Fields.Required[0]
	if def := status.OptionDef("qa REQUIRED"); def == nil || def.Name != "QA required" {
		t.Errorf("OptionDef should match case-insensitively, got %+v", def)
	}
	if def := status.OptionDef("Nope"); def != nil {
		t.Errorf("OptionDef(Nope) = %+v, want nil", def)
	}
}

func TestLoad_HasCreateIfMissingFields(t *testing.T) {
	defs, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	expectedFields := map[string]string{
		"Priority": "SINGLE_SELECT",
		"Branch":   "TEXT",
	}

	if len(defs.Fields.CreateIfMissing) != len(expectedFields) {
		t.Errorf("expected %d create_if_missing fields, got %d", len(expectedFields), len(defs.Fields.CreateIfMissing))
	}

	for _, field := range defs.Fields.CreateIfMissing {
		expectedType, ok := expectedFields[field.Name]
		if !ok {
			t.Errorf("unexpected field %q in create_if_missing", field.Name)
			continue
		}
		if field.Type != expectedType {
			t.Errorf("field %q has type %q, want %q", field.Name, field.Type, expectedType)
		}
	}
}

func TestLoad_PriorityFieldHasOptions(t *testing.T) {
	defs, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	var priorityField *FieldDef
	for i := range defs.Fields.CreateIfMissing {
		if defs.Fields.CreateIfMissing[i].Name == "Priority" {
			priorityField = &defs.Fields.CreateIfMissing[i]
			break
		}
	}

	if priorityField == nil {
		t.Fatal("Priority field not found in create_if_missing fields")
	}

	expectedOptions := []string{"P0", "P1", "P2"}
	if len(priorityField.Options) != len(expectedOptions) {
		t.Errorf("Priority field has %d options, want %d", len(priorityField.Options), len(expectedOptions))
	}

	for i, opt := range priorityField.Options {
		if opt != expectedOptions[i] {
			t.Errorf("Priority option[%d] = %q, want %q", i, opt, expectedOptions[i])
		}
	}
}

func TestMustLoad(t *testing.T) {
	// MustLoad should not panic with valid embedded data
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MustLoad() panicked: %v", r)
		}
	}()

	defs := MustLoad()
	if defs == nil {
		t.Error("MustLoad() returned nil")
	}
}

func TestGetLabel_Found(t *testing.T) {
	defs := MustLoad()

	// Test each known label
	knownLabels := []string{"branch", "epic", "story", "proposal", "prd", "bug", "enhancement", "qa-required", "test-plan", "security-required", "legal-required", "docs-required", "emergency", "approval-required", "blocked", "scope-creep"}
	for _, name := range knownLabels {
		label := defs.GetLabel(name)
		if label == nil {
			t.Errorf("GetLabel(%q) returned nil, expected label", name)
			continue
		}
		if label.Name != name {
			t.Errorf("GetLabel(%q).Name = %q, want %q", name, label.Name, name)
		}
		if label.Color == "" {
			t.Errorf("GetLabel(%q).Color is empty", name)
		}
		if label.Description == "" {
			t.Errorf("GetLabel(%q).Description is empty", name)
		}
	}
}

func TestGetLabel_NotFound(t *testing.T) {
	defs := MustLoad()

	// Test non-existent labels
	nonExistent := []string{"nonexistent", "foo", "bar", "unknown-label"}
	for _, name := range nonExistent {
		label := defs.GetLabel(name)
		if label != nil {
			t.Errorf("GetLabel(%q) = %v, want nil", name, label)
		}
	}
}

func TestGetLabelNames(t *testing.T) {
	defs := MustLoad()

	names := defs.GetLabelNames()

	// Should have same count as Labels
	if len(names) != len(defs.Labels) {
		t.Errorf("GetLabelNames() returned %d names, want %d", len(names), len(defs.Labels))
	}

	// All standard labels should be in the list
	expectedLabels := []string{"branch", "epic", "story", "proposal", "prd", "bug", "enhancement", "qa-required", "test-plan", "security-required", "legal-required", "docs-required", "emergency", "approval-required", "blocked", "scope-creep", "tech-debt", "active", "reviewed", "pending", "security-finding", "assigned", "auto-filed"}
	nameSet := make(map[string]bool)
	for _, name := range names {
		nameSet[name] = true
	}

	for _, expected := range expectedLabels {
		if !nameSet[expected] {
			t.Errorf("GetLabelNames() missing expected label %q", expected)
		}
	}
}

func TestTerms_ReturnsNonEmptyText(t *testing.T) {
	// ACT
	text := Terms()

	// ASSERT: Text is not empty and contains key sections
	if text == "" {
		t.Fatal("Terms() returned empty string")
	}

	if len(text) < 100 {
		t.Errorf("Terms() text seems too short: %d chars", len(text))
	}
}

func TestTerms_ContainsRequiredSections(t *testing.T) {
	text := Terms()

	requiredPhrases := []string{
		"Terms and Conditions",
		"What this tool does",
		"Your responsibility",
		"No warranty",
		"Liability",
		"Shared acceptance",
		"as-is",
	}

	for _, phrase := range requiredPhrases {
		if !strings.Contains(text, phrase) {
			t.Errorf("Terms() missing required phrase: %q", phrase)
		}
	}
}

func TestTerms_ContainsPraxisName(t *testing.T) {
	text := Terms()

	if !strings.Contains(text, "Praxis Management Utility") {
		t.Error("Terms() should reference 'Praxis Management Utility'")
	}
}

func TestTerms_ContainsCopyright(t *testing.T) {
	text := Terms()

	if !strings.Contains(text, "Rubrical Works") {
		t.Error("Terms() should contain Rubrical Works copyright")
	}
}

func TestIsStandardLabel(t *testing.T) {
	defs := MustLoad()

	// Standard labels should return true
	standardLabels := []string{"branch", "bug", "enhancement", "security-required", "legal-required", "docs-required"}
	for _, name := range standardLabels {
		if !defs.IsStandardLabel(name) {
			t.Errorf("IsStandardLabel(%q) = false, want true", name)
		}
	}

	// Non-standard labels should return false
	nonStandardLabels := []string{"foo", "bar", "custom-label", "my-label"}
	for _, name := range nonStandardLabels {
		if defs.IsStandardLabel(name) {
			t.Errorf("IsStandardLabel(%q) = true, want false", name)
		}
	}
}
