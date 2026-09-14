package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/defaults"
)

// Tests for the shared IDPF required-field validation extracted from
// runInitExistingProject and runInitPostCreate (#874). The two init paths
// returned byte-identical error strings; they differed only in that init.go
// printed a longer stderr hint while init_atomic.go routed through rollback.
// The hint is therefore returned separately from the error.

func requiredStatusField() []defaults.FieldDef {
	return []defaults.FieldDef{{
		Name:    "Status",
		Type:    "SINGLE_SELECT",
		Options: []string{"Backlog", "Done"},
	}}
}

func projectStatusField(options ...string) []api.ProjectField {
	opts := make([]api.FieldOption, 0, len(options))
	for i, name := range options {
		opts = append(opts, api.FieldOption{ID: string(rune('a' + i)), Name: name})
	}
	return []api.ProjectField{{ID: "f1", Name: "Status", DataType: "SINGLE_SELECT", Options: opts}}
}

func TestValidateRequiredFields_AllPresent(t *testing.T) {
	hint, err := validateRequiredFields(projectStatusField("Backlog", "Done"), requiredStatusField())
	if err != nil {
		t.Fatalf("Expected validation to pass; got %v", err)
	}
	if hint != "" {
		t.Errorf("Expected no hint on success; got %q", hint)
	}
}

func TestValidateRequiredFields_MissingFieldCarriesHint(t *testing.T) {
	hint, err := validateRequiredFields(nil, requiredStatusField())
	if err == nil {
		t.Fatal("Expected an error when the required field is absent")
	}
	// Byte-identical to what both init paths returned before extraction.
	if err.Error() != `required field "Status" not found in project` {
		t.Errorf("Unexpected error string: %q", err.Error())
	}
	if !strings.Contains(hint, "create it in the project settings before connecting") {
		t.Errorf("Expected the operator hint for a missing field; got %q", hint)
	}
}

func TestValidateRequiredFields_TypeMismatchHasNoHint(t *testing.T) {
	fields := []api.ProjectField{{ID: "f1", Name: "Status", DataType: "TEXT"}}

	hint, err := validateRequiredFields(fields, requiredStatusField())
	if err == nil {
		t.Fatal("Expected an error when the field type differs")
	}
	if err.Error() != `field "Status" has type TEXT, expected SINGLE_SELECT` {
		t.Errorf("Unexpected error string: %q", err.Error())
	}
	if hint != "" {
		t.Errorf("Expected no hint for a type mismatch; got %q", hint)
	}
}

// TestValidateRequiredFields_MissingOptionIsNotAnError (#917): a missing
// required option is added by reconcileInitStatus, so validation no longer
// rejects the project for it.
func TestValidateRequiredFields_MissingOptionIsNotAnError(t *testing.T) {
	if _, err := validateRequiredFields(projectStatusField("Backlog"), requiredStatusField()); err != nil {
		t.Errorf("a missing option must not fail validation (it is reconciled); got %v", err)
	}
}

// fakeStatusUpdater records UpdateProjectFieldOptions calls.
type fakeStatusUpdater struct {
	calls   [][]api.FieldOptionUpdate
	fieldID string
	err     error
}

func (f *fakeStatusUpdater) UpdateProjectFieldOptions(fieldID string, options []api.FieldOptionUpdate) ([]api.FieldOption, error) {
	f.fieldID = fieldID
	f.calls = append(f.calls, options)
	return nil, f.err
}

func embeddedRequired(t *testing.T) []defaults.FieldDef {
	t.Helper()
	defs, err := defaults.Load()
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	return defs.Fields.Required
}

func TestReconcileInitStatus_AddsMissingValuesKeepingExistingColors(t *testing.T) {
	existing := project11Status()
	fields := []api.ProjectField{{ID: "PVTSSF_status", Name: "Status", DataType: "SINGLE_SELECT", Options: existing}}
	updater := &fakeStatusUpdater{}
	var errOut strings.Builder

	if err := reconcileInitStatus(updater, fields, embeddedRequired(t), &errOut); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updater.calls) != 1 || updater.fieldID != "PVTSSF_status" {
		t.Fatalf("expected one update of the Status field, got %d calls on %q", len(updater.calls), updater.fieldID)
	}
	sent := updater.calls[0]
	if len(sent) != 9 {
		t.Errorf("expected 7 existing + 2 added options, got %d", len(sent))
	}
	for _, opt := range existing {
		u := updateByID(sent, opt.ID)
		if u == nil || u.Color != opt.Color || u.Description != opt.Description || u.Name != opt.Name {
			t.Errorf("init must send %q unchanged, got %+v", opt.Name, u)
		}
	}
	if out := errOut.String(); !strings.Contains(out, "Up next") || !strings.Contains(out, "QA required") {
		t.Errorf("expected the added values to be reported, got: %s", out)
	}
}

func TestReconcileInitStatus_NoChangeSendsNothing(t *testing.T) {
	var opts []api.FieldOption
	for i, def := range canonicalStatusDefs(t) {
		opts = append(opts, api.FieldOption{ID: string(rune('a' + i)), Name: def.Name, Color: "GRAY", Description: ""})
	}
	fields := []api.ProjectField{{ID: "F", Name: "Status", DataType: "SINGLE_SELECT", Options: opts}}
	updater := &fakeStatusUpdater{}
	if err := reconcileInitStatus(updater, fields, embeddedRequired(t), &strings.Builder{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updater.calls) != 0 {
		t.Errorf("init does not recolor, so a board with all 9 values needs no update; got %d calls", len(updater.calls))
	}
}

func TestReconcileInitStatus_CachedFieldSkipsWithWarning(t *testing.T) {
	fields := []api.ProjectField{{ID: "F", Name: "Status", DataType: "SINGLE_SELECT", FromCache: true,
		Options: []api.FieldOption{{ID: "bl", Name: "Backlog"}}}}
	updater := &fakeStatusUpdater{}
	var errOut strings.Builder
	if err := reconcileInitStatus(updater, fields, embeddedRequired(t), &errOut); err != nil {
		t.Fatalf("a cached Status field is skipped, not an error; got %v", err)
	}
	if len(updater.calls) != 0 {
		t.Error("no updateProjectV2Field may be sent from cached field data")
	}
	if !strings.Contains(errOut.String(), "skipping Status reconcile") || !strings.Contains(errOut.String(), "cached") {
		t.Errorf("expected a warning explaining the skip, got: %s", errOut.String())
	}
}

func TestReconcileInitStatus_UpdateFailureIsReturned(t *testing.T) {
	fields := []api.ProjectField{{ID: "F", Name: "Status", DataType: "SINGLE_SELECT", Options: project11Status()}}
	updater := &fakeStatusUpdater{err: errors.New("forbidden")}
	err := reconcileInitStatus(updater, fields, embeddedRequired(t), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("expected the update error, got %v", err)
	}
}

func TestReconcileInitStatus_NothingToReconcile(t *testing.T) {
	updater := &fakeStatusUpdater{}
	// No Status field on the project: validateRequiredFields reports that.
	if err := reconcileInitStatus(updater, nil, embeddedRequired(t), &strings.Builder{}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Requirements without option definitions: nothing to reconcile.
	fields := []api.ProjectField{{ID: "F", Name: "Status", DataType: "SINGLE_SELECT", Options: project11Status()}}
	if err := reconcileInitStatus(updater, fields, requiredStatusField(), &strings.Builder{}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(updater.calls) != 0 {
		t.Errorf("expected no updates, got %d", len(updater.calls))
	}
}

// Options are only checked for SINGLE_SELECT definitions that declare them.
func TestValidateRequiredFields_NonSingleSelectSkipsOptionCheck(t *testing.T) {
	required := []defaults.FieldDef{{Name: "Branch", Type: "TEXT", Options: []string{"ignored"}}}
	fields := []api.ProjectField{{ID: "f1", Name: "Branch", DataType: "TEXT"}}

	if _, err := validateRequiredFields(fields, required); err != nil {
		t.Errorf("Expected TEXT field to pass without an option check; got %v", err)
	}
}

func TestValidateRequiredFields_NoRequirementsPasses(t *testing.T) {
	if _, err := validateRequiredFields(nil, nil); err != nil {
		t.Errorf("Expected no requirements to pass; got %v", err)
	}
}
