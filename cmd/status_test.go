package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/defaults"
)

// canonicalStatusDefs returns the embedded 9-value Status definitions.
func canonicalStatusDefs(t *testing.T) []defaults.OptionDef {
	t.Helper()
	defs, err := defaults.Load()
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	for _, f := range defs.Fields.Required {
		if f.Name == "Status" {
			return f.OptionDefs
		}
	}
	t.Fatal("no Status field in defaults")
	return nil
}

// project11Status mirrors project #11: missing "Up next" and "QA required",
// several colors off the defaults, one empty description.
func project11Status() []api.FieldOption {
	return []api.FieldOption{
		{ID: "bl", Name: "Backlog", Color: "GREEN", Description: "This item hasn't been started"},
		{ID: "rd", Name: "Ready", Color: "BLUE", Description: "This is ready to be picked up"},
		{ID: "ip", Name: "In progress", Color: "YELLOW", Description: "This is actively being worked on"},
		{ID: "ir", Name: "In review", Color: "PURPLE", Description: ""},
		{ID: "dn", Name: "Done", Color: "ORANGE", Description: "This has been completed"},
		{ID: "pl", Name: "Parking Lot", Color: "YELLOW", Description: "Custom parking text"},
		{ID: "nt", Name: "Notes", Color: "GRAY", Description: "Keep draft issues and notes here"},
	}
}

// project7Status mirrors project #7: all nine present, two with case-only
// differences, plus a non-canonical option.
func project7Status() []api.FieldOption {
	return []api.FieldOption{
		{ID: "bl", Name: "Backlog", Color: "GREEN", Description: "d"},
		{ID: "un", Name: "Up Next", Color: "YELLOW", Description: "d"},
		{ID: "rd", Name: "Ready", Color: "BLUE", Description: "d"},
		{ID: "ip", Name: "In progress", Color: "YELLOW", Description: "d"},
		{ID: "ir", Name: "In review", Color: "PURPLE", Description: "d"},
		{ID: "qa", Name: "QA Required", Color: "ORANGE", Description: "d"},
		{ID: "dn", Name: "Done", Color: "ORANGE", Description: "d"},
		{ID: "pl", Name: "Parking Lot", Color: "PURPLE", Description: "d"},
		{ID: "pp", Name: "PropParking Lot", Color: "PINK", Description: ""},
		{ID: "nt", Name: "Notes", Color: "GRAY", Description: "d"},
	}
}

func updateByID(updates []api.FieldOptionUpdate, id string) *api.FieldOptionUpdate {
	for i := range updates {
		if updates[i].ID == id {
			return &updates[i]
		}
	}
	return nil
}

func updateNames(updates []api.FieldOptionUpdate) []string {
	names := make([]string, len(updates))
	for i, u := range updates {
		names[i] = u.Name
	}
	return names
}

func TestPlanStatusReconcile_AddsMissingInTemplatePosition(t *testing.T) {
	canonical := canonicalStatusDefs(t)
	plan, err := planStatusReconcile(project11Status(), canonical, statusReconcileInit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"Backlog", "Up next", "Ready", "In progress", "In review", "QA required", "Done", "Parking Lot", "Notes"}
	if got := updateNames(plan.Options); !reflect.DeepEqual(got, want) {
		t.Errorf("option order = %v, want %v", got, want)
	}
	if !plan.Changed {
		t.Error("adding options is a change")
	}

	// Added options carry the default color and description and no id.
	for _, name := range []string{"Up next", "QA required"} {
		def := findOptionDef(canonical, name)
		var added *api.FieldOptionUpdate
		for i := range plan.Options {
			if plan.Options[i].Name == name {
				added = &plan.Options[i]
			}
		}
		if added == nil || added.ID != "" || added.Color != def.Color || added.Description != def.Description {
			t.Errorf("added %q = %+v, want no id, color %s, description %q", name, added, def.Color, def.Description)
		}
	}
}

// TestPlanStatusReconcile_InitKeepsExistingColorAndDescription: in init mode an
// existing option is sent exactly as found — no recolor, no description fill.
func TestPlanStatusReconcile_InitKeepsExistingColorAndDescription(t *testing.T) {
	existing := project11Status()
	plan, err := planStatusReconcile(existing, canonicalStatusDefs(t), statusReconcileInit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, opt := range existing {
		u := updateByID(plan.Options, opt.ID)
		if u == nil {
			t.Fatalf("existing option %q (id %s) missing from payload", opt.Name, opt.ID)
		}
		if u.Name != opt.Name || u.Color != opt.Color || u.Description != opt.Description {
			t.Errorf("init changed %q: sent %+v, had %+v", opt.Name, u, opt)
		}
	}
}

func TestPlanStatusReconcile_UpdateRecolorsAndFillsOnlyEmptyDescriptions(t *testing.T) {
	canonical := canonicalStatusDefs(t)
	existing := project11Status()
	plan, err := planStatusReconcile(existing, canonical, statusReconcileUpdate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, opt := range existing {
		u := updateByID(plan.Options, opt.ID)
		def := findOptionDef(canonical, opt.Name)
		if u.Color != def.Color {
			t.Errorf("%q color = %s, want default %s", opt.Name, u.Color, def.Color)
		}
		switch {
		case opt.Description == "" && u.Description != def.Description:
			t.Errorf("%q empty description should be filled with %q, got %q", opt.Name, def.Description, u.Description)
		case opt.Description != "" && u.Description != opt.Description:
			t.Errorf("%q non-empty description must be sent unchanged, got %q", opt.Name, u.Description)
		}
	}
	if updateByID(plan.Options, "pl").Description != "Custom parking text" {
		t.Error("a customized description must survive status --update")
	}
}

func TestPlanStatusReconcile_RenamesCaseOnlyMismatchWithExistingID(t *testing.T) {
	plan, err := planStatusReconcile(project7Status(), canonicalStatusDefs(t), statusReconcileInit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for id, want := range map[string]string{"un": "Up next", "qa": "QA required"} {
		u := updateByID(plan.Options, id)
		if u == nil || u.Name != want {
			t.Errorf("option %s = %+v, want renamed to %q with its id kept", id, u, want)
		}
	}
	if len(plan.Options) != len(project7Status()) {
		t.Errorf("a case-only rename must not add options: got %d, want %d", len(plan.Options), len(project7Status()))
	}
	if entry := plan.entry("Up next"); entry == nil || entry.Previous != "Up Next" || !entry.has(statusActionRenamed) {
		t.Errorf("report should record the rename from Up Next, got %+v", entry)
	}
}

func TestPlanStatusReconcile_NeverDropsOrReordersExistingOptions(t *testing.T) {
	for name, existing := range map[string][]api.FieldOption{"project11": project11Status(), "project7": project7Status()} {
		for _, mode := range []statusReconcileMode{statusReconcileInit, statusReconcileUpdate} {
			plan, err := planStatusReconcile(existing, canonicalStatusDefs(t), mode)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", name, err)
			}
			// Every existing id is present, and existing ids keep their relative order.
			var sentIDs []string
			for _, u := range plan.Options {
				if u.ID != "" {
					sentIDs = append(sentIDs, u.ID)
				}
			}
			var existingIDs []string
			for _, o := range existing {
				existingIDs = append(existingIDs, o.ID)
			}
			if !reflect.DeepEqual(sentIDs, existingIDs) {
				t.Errorf("%s mode %v: existing ids sent as %v, want %v (all present, same order)", name, mode, sentIDs, existingIDs)
			}
		}
	}
}

func TestPlanStatusReconcile_PreservesNonCanonicalOptions(t *testing.T) {
	for _, mode := range []statusReconcileMode{statusReconcileInit, statusReconcileUpdate} {
		plan, err := planStatusReconcile(project7Status(), canonicalStatusDefs(t), mode)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		u := updateByID(plan.Options, "pp")
		want := api.FieldOptionUpdate{ID: "pp", Name: "PropParking Lot", Color: "PINK", Description: ""}
		if u == nil || *u != want {
			t.Errorf("mode %v: non-canonical option sent as %+v, want unchanged %+v", mode, u, want)
		}
		if entry := plan.entry("PropParking Lot"); entry == nil || !entry.has(statusActionPreserved) {
			t.Errorf("mode %v: report should mark PropParking Lot preserved, got %+v", mode, entry)
		}
	}
}

func TestPlanStatusReconcile_NoChangeWhenAlreadyCanonical(t *testing.T) {
	canonical := canonicalStatusDefs(t)
	var existing []api.FieldOption
	for i, def := range canonical {
		existing = append(existing, api.FieldOption{ID: string(rune('a' + i)), Name: def.Name, Color: def.Color, Description: def.Description})
	}
	plan, err := planStatusReconcile(existing, canonical, statusReconcileUpdate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Changed {
		t.Errorf("a board already matching the defaults needs no update, got %+v", plan.Entries)
	}
}

func TestPlanStatusReconcile_RefusesOptionsWithoutColor(t *testing.T) {
	existing := []api.FieldOption{{ID: "bl", Name: "Backlog"}}
	if _, err := planStatusReconcile(existing, canonicalStatusDefs(t), statusReconcileInit); err == nil ||
		!strings.Contains(err.Error(), "color") {
		t.Errorf("an option with no color cannot be round-tripped; want an error naming color, got %v", err)
	}
}

func TestPlanStatusReconcile_MissingLeadingValueGoesFirst(t *testing.T) {
	existing := []api.FieldOption{
		{ID: "x", Name: "Custom", Color: "RED"},
		{ID: "rd", Name: "Ready", Color: "GREEN"},
	}
	plan, err := planStatusReconcile(existing, canonicalStatusDefs(t), statusReconcileInit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := updateNames(plan.Options)
	// Backlog and Up next precede Ready in the template, so they go before it;
	// the non-canonical Custom keeps its place ahead of them.
	want := []string{"Custom", "Backlog", "Up next", "Ready", "In progress", "In review", "QA required", "Done", "Parking Lot", "Notes"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
