package cmd

import (
	"fmt"
	"io"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/defaults"
)

// initPostCreateClient is the API surface runInitPostCreate touches. Kept
// minimal so the atomicity flow is unit-testable with a fake.
type initPostCreateClient interface {
	GetProjectFields(projectID string) ([]api.ProjectField, error)
	LabelExists(owner, repo, name string) (bool, error)
	CreateLabel(owner, repo, name, color, description string) error
	DeleteProject(projectID string) error
	FieldExists(projectID, name string) (bool, error)
	CreateProjectField(projectID, name, dataType string, singleSelectOptions []string) (*api.ProjectField, error)
	UpdateProjectFieldOptions(fieldID string, options []api.FieldOptionUpdate) ([]api.FieldOption, error)
}

// initPostCreateInputs bundles the data runInitPostCreate needs after the
// destination project has been created. Keeps the function signature small
// and the test setup readable.
type initPostCreateInputs struct {
	Cwd        string
	RepoOwner  string
	RepoName   string
	Framework  string
	NewProject *api.Project
	Cfg        *InitConfig
	Defs       *defaults.Defaults
	ErrOut     io.Writer
}

// runInitPostCreate performs the post-CopyProjectFromTemplate steps inside an
// atomicity boundary: any hard failure rolls back the just-created project
// (via client.DeleteProject) and emits the structured failure trailer. The
// label loop is warn+continue per #847 AC5 and never triggers rollback.
//
// configRewritten in the trailer is true only if writeConfigWithMetadata was
// attempted — the file may be partially written on Windows even on error.
func runInitPostCreate(client initPostCreateClient, in *initPostCreateInputs) error {
	rollback := func(step string, cause error, configRewritten bool) error {
		printInitFailureTrailer(in.ErrOut, in.NewProject.Number, step, configRewritten)
		if delErr := client.DeleteProject(in.NewProject.ID); delErr != nil {
			fmt.Fprintf(in.ErrOut, "warning: rollback DeleteProject(%s) failed: %v\n", in.NewProject.ID, delErr)
		}
		return cause
	}

	projectFields, err := client.GetProjectFields(in.NewProject.ID)
	if err != nil {
		return rollback(FailedStepGetFields, fmt.Errorf("could not fetch project fields: %w", err), false)
	}

	if in.Framework == "IDPF" {
		// The stderr hint is intentionally discarded: this path reports failures
		// through the rollback trailer, not a bare stderr line (#874).
		if _, err := validateRequiredFields(projectFields, in.Defs.Fields.Required); err != nil {
			return rollback(FailedStepValidateRequiredFields, err, false)
		}
		// Missing required Status values are added, not rejected (#917).
		if err := reconcileInitStatus(client, projectFields, in.Defs.Fields.Required, in.ErrOut); err != nil {
			return rollback(FailedStepValidateRequiredFields, err, false)
		}

		ensureOptionalProjectFields(client, in.NewProject.ID, in.Defs.Fields.CreateIfMissing, in.ErrOut)

		// Label loop — warn+continue (AC5 of #847). Never rolls back.
		for _, labelDef := range in.Defs.Labels {
			exists, err := client.LabelExists(in.RepoOwner, in.RepoName, labelDef.Name)
			if err != nil {
				fmt.Fprintf(in.ErrOut, "Warning: failed to check label %q: %v\n", labelDef.Name, err)
				continue
			}
			if !exists {
				if err := client.CreateLabel(in.RepoOwner, in.RepoName, labelDef.Name, labelDef.Color, labelDef.Description); err != nil {
					fmt.Fprintf(in.ErrOut, "Warning: failed to create label %q: %v\n", labelDef.Name, err)
				}
			}
		}
	}

	// Refetch — warn+continue today (metadata may be stale but config still writable).
	fields, err := client.GetProjectFields(in.NewProject.ID)
	if err != nil {
		fmt.Fprintf(in.ErrOut, "Warning: failed to refetch project fields: %v\n", err)
	}

	metadata := &ProjectMetadata{ProjectID: in.NewProject.ID}
	for _, f := range fields {
		fm := FieldMetadata{ID: f.ID, Name: f.Name, DataType: f.DataType}
		for _, opt := range f.Options {
			fm.Options = append(fm.Options, OptionMetadata{ID: opt.ID, Name: opt.Name})
		}
		metadata.Fields = append(metadata.Fields, fm)
	}

	if err := writeConfigWithMetadata(in.Cwd, in.Cfg, metadata); err != nil {
		// File may be partially written on error — flag configRewritten=true defensively.
		return rollback(FailedStepWriteConfig, fmt.Errorf("failed to write config: %w", err), true)
	}

	return nil
}
