package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/rubrical-works/gh-pmu/internal/defaults"
	"github.com/rubrical-works/gh-pmu/internal/integrity"
	"github.com/spf13/cobra"
)

type statusOptions struct {
	update bool
}

// statusUpdateClient is the API surface `gh pmu status --update` uses.
type statusUpdateClient interface {
	GetProject(owner string, number int) (*api.Project, error)
	GetProjectFields(projectID string) ([]api.ProjectField, error)
	UpdateProjectFieldOptions(fieldID string, options []api.FieldOptionUpdate) ([]api.FieldOption, error)
}

func newStatusCommand() *cobra.Command {
	opts := &statusOptions{}

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Manage the project's Status field values",
		Long: `Manage the project's Status field values.

--update brings the Status field up to the 9 values every gh-pmu project
carries (Backlog, Up next, Ready, In progress, In review, QA required, Done,
Parking Lot, Notes):
  - adds missing values next to their template neighbors
  - renames values that differ only in capitalization, keeping their items
  - sets each of the 9 values to its default color
  - fills an empty description with the default (non-empty ones are kept)

Nothing is deleted or reordered, and values outside the 9 are left untouched.
The Status aliases in .gh-pmu.json are refreshed afterwards.`,
		Example: `  # Add missing Status values and apply default colors
  gh pmu status --update`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.update, "update", false, "Add missing Status values, fix capitalization, and apply default colors")

	return cmd
}

func runStatus(cmd *cobra.Command, opts *statusOptions) error {
	if !opts.update {
		return cmd.Help()
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	configPath, err := config.FindConfigFile(cwd)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'gh pmu init' to create a configuration file", err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	client, err := api.NewClient()
	if err != nil {
		return err
	}

	return runStatusUpdateWithDeps(cmd, cfg, filepath.Dir(configPath), client)
}

// runStatusUpdateWithDeps is the testable implementation of `status --update`.
func runStatusUpdateWithDeps(cmd *cobra.Command, cfg *config.Config, configDir string, client statusUpdateClient) error {
	defs, err := defaults.Load()
	if err != nil {
		return fmt.Errorf("failed to load embedded defaults: %w", err)
	}
	var canonical []defaults.OptionDef
	for _, f := range defs.Fields.Required {
		if f.Name == "Status" {
			canonical = f.OptionDefs
		}
	}

	project, err := client.GetProject(cfg.Project.Owner, cfg.Project.Number)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}
	fields, err := client.GetProjectFields(project.ID)
	if err != nil {
		return fmt.Errorf("failed to get project fields: %w", err)
	}
	field := findFieldByName(fields, "Status")
	if field == nil {
		return fmt.Errorf("project has no Status field")
	}
	if field.FromCache {
		return fmt.Errorf("live field data is unavailable (the ProjectV2 field resolver failed and only cached metadata could be read); Status options were not updated — retry once GitHub recovers")
	}

	plan, err := planStatusReconcile(field.Options, canonical, statusReconcileUpdate)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	current := field.Options
	if plan.Changed {
		updated, err := client.UpdateProjectFieldOptions(field.ID, plan.Options)
		if err != nil {
			return err
		}
		current = updated
		fmt.Fprintf(out, "Updated Status field on %s:\n", project.Title)
	} else {
		fmt.Fprintf(out, "Status field on %s already has the required values and default colors.\n", project.Title)
	}
	for _, entry := range plan.Entries {
		line := fmt.Sprintf("  %-18s %s", entry.Name, strings.Join(entry.Actions, ", "))
		if entry.Previous != "" {
			line += fmt.Sprintf(" (was %q)", entry.Previous)
		}
		fmt.Fprintln(out, line)
	}

	refreshStatusConfig(cfg, field, current, plan)
	if err := cfg.Save(configDir); err != nil {
		return fmt.Errorf("status field updated but .gh-pmu.json could not be saved: %w", err)
	}
	_ = integrity.UpdateChecksumForConfig(filepath.Join(configDir, config.ConfigFileName))
	return nil
}

// refreshStatusConfig brings .gh-pmu.json in line with the Status options now
// on the board: every value gets its derived alias, aliases that pointed at a
// renamed value follow the rename, and the cached Status metadata is replaced.
// Aliases for values not on the board are left alone — they may be deliberate.
func refreshStatusConfig(cfg *config.Config, field *api.ProjectField, current []api.FieldOption, plan statusPlan) {
	if cfg.Fields == nil {
		cfg.Fields = map[string]config.Field{}
	}
	mapping := cfg.Fields["status"]
	if mapping.Field == "" {
		mapping.Field = field.Name
	}
	if mapping.Values == nil {
		mapping.Values = map[string]string{}
	}

	renamed := map[string]string{}
	for _, entry := range plan.Entries {
		if entry.Previous != "" {
			renamed[entry.Previous] = entry.Name
		}
	}
	for alias, value := range mapping.Values {
		if to, ok := renamed[value]; ok {
			mapping.Values[alias] = to
		}
	}

	names := make([]string, 0, len(current))
	for _, opt := range current {
		names = append(names, opt.Name)
	}
	if len(names) == 0 {
		for _, u := range plan.Options {
			names = append(names, u.Name)
		}
	}
	for _, name := range names {
		mapping.Values[optionNameToAlias(name)] = name
	}
	cfg.Fields["status"] = mapping

	if len(current) > 0 {
		meta := config.FieldMetadata{Name: field.Name, ID: field.ID, DataType: field.DataType}
		for _, opt := range current {
			meta.Options = append(meta.Options, config.OptionMetadata{Name: opt.Name, ID: opt.ID})
		}
		cfg.AddFieldMetadata(meta)
	}
}

// statusReconcileMode selects how far a Status reconcile goes (#917).
type statusReconcileMode int

const (
	// statusReconcileInit adds missing canonical values and fixes case-only
	// name differences. Existing colors and descriptions are sent unchanged.
	statusReconcileInit statusReconcileMode = iota
	// statusReconcileUpdate also sets canonical colors to the defaults and
	// fills empty canonical descriptions. Non-empty descriptions are kept.
	statusReconcileUpdate
)

// Actions recorded per option in a reconcile report.
const (
	statusActionAdded       = "added"
	statusActionRenamed     = "renamed"
	statusActionRecolored   = "recolored"
	statusActionDescription = "description set"
	statusActionUnchanged   = "unchanged"
	statusActionPreserved   = "preserved"
)

// statusPlanEntry reports what the plan does to one option.
type statusPlanEntry struct {
	Name     string   // name after the update
	Previous string   // name before a rename; empty otherwise
	Actions  []string // one or more statusAction* values
}

func (e *statusPlanEntry) has(action string) bool {
	for _, a := range e.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// statusPlan is the full option list to send plus a per-option report.
type statusPlan struct {
	// Options is the complete singleSelectOptions list, in board order. Every
	// pre-existing option is present with its ID; added options have none.
	Options []api.FieldOptionUpdate
	Entries []statusPlanEntry // parallel to Options
	Changed bool
}

func (p *statusPlan) entry(name string) *statusPlanEntry {
	for i := range p.Entries {
		if p.Entries[i].Name == name {
			return &p.Entries[i]
		}
	}
	return nil
}

// planStatusReconcile computes the Status option list that brings existing up
// to the canonical minimum without deleting or reordering anything.
//
// The ground rules are the point of this function, because updateProjectV2Field
// replaces the whole option list:
//   - every existing option is sent, in its current position, with its ID —
//     an option sent without its ID loses its value on every item;
//   - an existing option's color and description are sent as found unless the
//     mode deliberately changes them, since both are overwritten when provided;
//   - a canonical name that differs only in case is renamed in place via its ID;
//   - a missing canonical value is inserted after its nearest preceding canonical
//     neighbor (or before its nearest following one), never by moving others.
func planStatusReconcile(existing []api.FieldOption, canonical []defaults.OptionDef, mode statusReconcileMode) (statusPlan, error) {
	var plan statusPlan

	for _, opt := range existing {
		if opt.Color == "" {
			return statusPlan{}, fmt.Errorf("status option %q has no color; live option data is required to update options safely", opt.Name)
		}
	}

	// matchedAt[canonical index] = position in plan.Options of the existing
	// option claiming it. The first case-insensitive match claims a value, so a
	// duplicate stays non-canonical rather than being renamed onto a clash.
	matchedAt := make(map[int]int)
	for _, opt := range existing {
		update := api.FieldOptionUpdate(opt)
		entry := statusPlanEntry{Name: opt.Name}

		ci := canonicalIndex(canonical, opt.Name)
		if _, claimed := matchedAt[ci]; ci < 0 || claimed {
			entry.Actions = []string{statusActionPreserved}
			plan.Options = append(plan.Options, update)
			plan.Entries = append(plan.Entries, entry)
			continue
		}

		def := canonical[ci]
		matchedAt[ci] = len(plan.Options)
		if opt.Name != def.Name {
			update.Name = def.Name
			entry.Name = def.Name
			entry.Previous = opt.Name
			entry.Actions = append(entry.Actions, statusActionRenamed)
		}
		if mode == statusReconcileUpdate {
			if def.Color != "" && opt.Color != def.Color {
				update.Color = def.Color
				entry.Actions = append(entry.Actions, statusActionRecolored)
			}
			if opt.Description == "" && def.Description != "" {
				update.Description = def.Description
				entry.Actions = append(entry.Actions, statusActionDescription)
			}
		}
		if len(entry.Actions) == 0 {
			entry.Actions = []string{statusActionUnchanged}
		} else {
			plan.Changed = true
		}
		plan.Options = append(plan.Options, update)
		plan.Entries = append(plan.Entries, entry)
	}

	for ci, def := range canonical {
		if _, present := matchedAt[ci]; present {
			continue
		}
		pos := insertionPoint(matchedAt, ci, len(canonical), len(plan.Options))
		added := api.FieldOptionUpdate{Name: def.Name, Color: def.Color, Description: def.Description}
		plan.Options = append(plan.Options[:pos], append([]api.FieldOptionUpdate{added}, plan.Options[pos:]...)...)
		plan.Entries = append(plan.Entries[:pos], append([]statusPlanEntry{{Name: def.Name, Actions: []string{statusActionAdded}}}, plan.Entries[pos:]...)...)
		for k, at := range matchedAt {
			if at >= pos {
				matchedAt[k] = at + 1
			}
		}
		matchedAt[ci] = pos
		plan.Changed = true
	}

	return plan, nil
}

// insertionPoint returns where canonical value ci goes: after the nearest
// preceding canonical value already placed, else before the nearest following
// one, else at the end.
func insertionPoint(placed map[int]int, ci, canonicalCount, optionCount int) int {
	for j := ci - 1; j >= 0; j-- {
		if at, ok := placed[j]; ok {
			return at + 1
		}
	}
	for j := ci + 1; j < canonicalCount; j++ {
		if at, ok := placed[j]; ok {
			return at
		}
	}
	return optionCount
}

// canonicalIndex returns the index of the canonical value matching name
// case-insensitively, or -1.
func canonicalIndex(canonical []defaults.OptionDef, name string) int {
	for i, def := range canonical {
		if strings.EqualFold(def.Name, name) {
			return i
		}
	}
	return -1
}

// findOptionDef returns the canonical definition matching name
// case-insensitively, or nil.
func findOptionDef(canonical []defaults.OptionDef, name string) *defaults.OptionDef {
	if i := canonicalIndex(canonical, name); i >= 0 {
		return &canonical[i]
	}
	return nil
}
