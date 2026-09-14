package cmd

import (
	"fmt"
	"strings"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/defaults"
)

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
