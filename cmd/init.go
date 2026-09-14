package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/rubrical-works/gh-pmu/internal/defaults"
	"github.com/spf13/cobra"
)

// ErrRepoRootProtected is returned when attempting to write config to repo root during tests
var ErrRepoRootProtected = errors.New("cannot write config to repository root during tests")

// protectRepoRoot enables protection against writing to repo root (set by tests).
// Uses atomic.Bool for thread safety under parallel test execution.
var protectRepoRoot atomic.Bool

// SetRepoRootProtection enables or disables repo root write protection.
// This should be called by test setup to prevent accidental config writes.
func SetRepoRootProtection(enabled bool) {
	protectRepoRoot.Store(enabled)
}

// isRepoRoot checks if the given directory is the repository root by looking for go.mod
func isRepoRoot(dir string) bool {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(absDir, "go.mod"))
	return err == nil
}

// initOptions holds the command-line options for init
type initOptions struct {
	nonInteractive bool
	sourceProject  int
	project        int
	repo           string
	owner          string
	framework      string
	yes            bool
	force          bool
}

func newInitCommand() *cobra.Command {
	opts := &initOptions{}

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize gh-pmu configuration for the current project",
		Long: `Initialize gh-pmu configuration by creating a .gh-pmu.json file.

Use --source-project to copy from a template project, or --project to
connect to an existing project. Both require --repo.

Flags --project and --source-project are mutually exclusive.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, args, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.nonInteractive, "non-interactive", false, "Disable UI and prompts (requires --source-project and --repo)")
	_ = cmd.Flags().MarkDeprecated("non-interactive", "init is always non-interactive; this flag will be removed in a future release")
	cmd.Flags().IntVar(&opts.sourceProject, "source-project", 0, "Source project number to copy from")
	cmd.Flags().IntVar(&opts.project, "project", 0, "Existing project number to connect to")
	cmd.Flags().StringVar(&opts.repo, "repo", "", "Repository (owner/repo format)")
	cmd.Flags().StringVar(&opts.owner, "owner", "", "Project owner (defaults to repo owner)")
	cmd.Flags().StringVar(&opts.framework, "framework", "IDPF", "Framework type (IDPF or none)")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Auto-confirm prompts")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Allow --source-project to replace an existing project pointer in .gh-pmu.json (otherwise refused; prefer --project <existing>)")
	cmd.MarkFlagsMutuallyExclusive("project", "source-project")

	return cmd
}

func runInit(cmd *cobra.Command, args []string, opts *initOptions) error {
	// --non-interactive is now a no-op (all modes are non-interactive)
	if opts.project > 0 {
		return runInitExistingProject(cmd, opts)
	}
	return runInitNonInteractive(cmd, opts)
}

// runInitNonInteractive handles init in non-interactive mode (for CI/CD).
// It copies from a source project specified by --source-project, creates a new
// project, links the repository, and writes config with the new project number.
func runInitNonInteractive(cmd *cobra.Command, opts *initOptions) error {
	// Validate required flags
	var missingFlags []string
	if opts.sourceProject == 0 {
		missingFlags = append(missingFlags, "--source-project (or --project)")
	}
	if opts.repo == "" {
		missingFlags = append(missingFlags, "--repo")
	}

	if len(missingFlags) > 0 {
		fmt.Fprintf(os.Stderr, "error: required flags: %s\n", strings.Join(missingFlags, ", "))
		return fmt.Errorf("missing required flags: %s", strings.Join(missingFlags, ", "))
	}

	// Validate repo format
	repoOwner, repoName := splitRepository(opts.repo)
	if repoOwner == "" || repoName == "" {
		fmt.Fprintf(os.Stderr, "error: --repo must be in owner/repo format\n")
		return fmt.Errorf("invalid repo format: %s", opts.repo)
	}

	// Determine owner (from --owner flag or infer from repo)
	owner := opts.owner
	if owner == "" {
		owner = repoOwner
	}

	// Determine framework (defaults to IDPF)
	framework := opts.framework
	if framework == "" {
		framework = "IDPF"
	}

	// Check if a config already exists in the current directory OR any parent
	// (config.FindConfigFile walks up). Running init from a subdirectory of an
	// initialized project must not silently create a shadowing nested config or
	// bypass the sourceProjectGuard.
	var existingFramework string
	var existingProjectNumber int
	if existingPath, findErr := config.FindConfigFile("."); findErr == nil {
		if existingRaw, err := loadExistingRaw("."); err == nil {
			existingFramework = existingRaw.Framework
			existingProjectNumber = existingRaw.Project.Number
		}
		if !opts.yes {
			fmt.Fprintf(os.Stderr, "error: config already exists at %s (use --yes to overwrite)\n", existingPath)
			return fmt.Errorf("config already exists")
		}
	}

	// Stricter guard: --source-project + existing project pointer requires --force
	if err := sourceProjectGuard(existingProjectNumber, opts.sourceProject, opts.force); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return err
	}

	// Preserve existing framework on re-init
	if existingFramework != "" {
		framework = existingFramework
	}

	// Initialize API client
	client, clientErr := api.NewClient()
	if clientErr != nil {
		return clientErr
	}

	// Get owner ID for the new project (no project created yet — no rollback needed on failure)
	ownerID, err := client.GetOwnerID(owner)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to get owner ID for %s: %v\n", owner, err)
		return fmt.Errorf("failed to get owner ID: %w", err)
	}

	// Fetch source project to copy from
	sourceProject, err := client.GetProject(owner, opts.sourceProject)
	if err != nil {
		printInitFailureTrailer(os.Stderr, 0, FailedStepSourceCopy, false)
		fmt.Fprintf(os.Stderr, "error: failed to find source project %s/%d: %v\n", owner, opts.sourceProject, err)
		return fmt.Errorf("failed to find source project: %w", err)
	}

	projectTitle := deriveProjectTitle(repoName)

	// Copy project from source — once this succeeds, every subsequent hard
	// failure must roll back via client.DeleteProject (handled by runInitPostCreate).
	newProject, err := client.CopyProjectFromTemplate(ownerID, sourceProject.ID, projectTitle)
	if err != nil {
		printInitFailureTrailer(os.Stderr, 0, FailedStepCreateProject, false)
		fmt.Fprintf(os.Stderr, "error: failed to create project from source: %v\n", err)
		return fmt.Errorf("failed to create project from source: %w", err)
	}

	// Link repository — warn-only (non-essential to atomicity).
	repoID, err := client.GetRepositoryID(repoOwner, repoName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not get repository ID: %v\n", err)
	} else {
		if linkErr := client.LinkProjectToRepository(newProject.ID, repoID); linkErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not link repository: %v\n", linkErr)
		}
	}

	defs, err := defaults.Load()
	if err != nil {
		// Defaults are embedded; this is a build-time issue, not a runtime one.
		// Roll back since we can't proceed.
		printInitFailureTrailer(os.Stderr, newProject.Number, FailedStepGetFields, false)
		if delErr := client.DeleteProject(newProject.ID); delErr != nil {
			fmt.Fprintf(os.Stderr, "warning: rollback DeleteProject(%s) failed: %v\n", newProject.ID, delErr)
		}
		fmt.Fprintf(os.Stderr, "error: failed to load defaults: %v\n", err)
		return fmt.Errorf("failed to load embedded defaults: %w", err)
	}

	cfg := &InitConfig{
		ProjectName:   newProject.Title,
		ProjectOwner:  owner,
		ProjectNumber: newProject.Number,
		Repositories:  []string{opts.repo},
		Framework:     framework,
	}

	cwd, _ := os.Getwd()
	if err := runInitPostCreate(client, &initPostCreateInputs{
		Cwd:        cwd,
		RepoOwner:  repoOwner,
		RepoName:   repoName,
		Framework:  framework,
		NewProject: newProject,
		Cfg:        cfg,
		Defs:       defs,
		ErrOut:     cmd.ErrOrStderr(),
	}); err != nil {
		// Trailer + rollback already emitted by runInitPostCreate.
		return err
	}

	// Output success to stdout (minimal for CI/CD parsing)
	fmt.Fprintf(cmd.OutOrStdout(), "Created .gh-pmu.json for %s (#%d) [copied from source project #%d]\n",
		newProject.Title, newProject.Number, opts.sourceProject)

	return nil
}

// runInitExistingProject connects to an existing project (--project flag).
func runInitExistingProject(cmd *cobra.Command, opts *initOptions) error {
	// Validate --repo is required
	if opts.repo == "" {
		fmt.Fprintf(os.Stderr, "error: --project requires --repo flag\n")
		return fmt.Errorf("missing required flag: --repo")
	}

	// Validate repo format
	repoOwner, repoName := splitRepository(opts.repo)
	if repoOwner == "" || repoName == "" {
		fmt.Fprintf(os.Stderr, "error: --repo must be in owner/repo format\n")
		return fmt.Errorf("invalid repo format: %s", opts.repo)
	}

	// Determine owner
	owner := opts.owner
	if owner == "" {
		owner = repoOwner
	}

	// Determine framework
	framework := opts.framework
	if framework == "" {
		framework = "IDPF"
	}

	// Check if a config already exists in the current directory OR any parent
	// (config.FindConfigFile walks up). Running init from a subdirectory of an
	// initialized project must not silently create a shadowing nested config.
	var existingFramework string
	if existingPath, findErr := config.FindConfigFile("."); findErr == nil {
		if existingCfg, err := loadExistingFramework("."); err == nil {
			existingFramework = existingCfg
		}
		if !opts.yes {
			fmt.Fprintf(os.Stderr, "error: config already exists at %s (use --yes to overwrite)\n", existingPath)
			return fmt.Errorf("config already exists")
		}
	}

	// Preserve existing framework on re-init
	if existingFramework != "" {
		framework = existingFramework
	}

	// Initialize API client
	client, clientErr := api.NewClient()
	if clientErr != nil {
		return clientErr
	}

	// Fetch existing project
	project, err := client.GetProject(owner, opts.project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: project %s/%d not found: %v\n", owner, opts.project, err)
		return fmt.Errorf("project not found: %w", err)
	}

	// Load embedded defaults for field validation
	defs, err := defaults.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to load defaults: %v\n", err)
		return fmt.Errorf("failed to load embedded defaults: %w", err)
	}

	// Fetch project fields. Init runs with NoCacheFallback so we never read
	// metadata.fields[] back into the in-memory state we're about to
	// rewrite — the cache exists for runtime commands, not init itself
	// (see #853 AC7). If the bulk resolver is crashing, fall back to a
	// per-name fetch against the documented standard set so we can still
	// recover a usable cache.
	prevFallback := api.CurrentFallbackOptions()
	api.SetFallbackOptions(api.FallbackOptions{NoCacheFallback: true})
	defer api.SetFallbackOptions(prevFallback)

	projectFields, err := client.GetProjectFields(project.ID)
	if err != nil {
		if errors.Is(err, api.ErrFieldResolverUnavailable) {
			recovered, missing := client.FetchFieldsByNames(project.ID, api.StandardProjectV2FieldNames)
			total := len(api.StandardProjectV2FieldNames)
			fmt.Fprintf(cmd.ErrOrStderr(),
				"warning: ProjectV2 fields resolver unavailable (GraphQL 5xx); recovered %d/%d standard fields via per-name lookup.\n",
				len(recovered), total)
			if len(missing) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"  unreadable: %s\n  re-run 'gh pmu init' once GitHub recovers to capture the full set.\n",
					strings.Join(missing, ", "))
			}
			if len(recovered) == 0 {
				fmt.Fprintf(os.Stderr, "error: could not fetch any project fields (resolver unavailable, no fields recovered)\n")
				return fmt.Errorf("could not fetch project fields: %w", err)
			}
			projectFields = recovered
		} else {
			fmt.Fprintf(os.Stderr, "error: could not fetch project fields: %v\n", err)
			return fmt.Errorf("could not fetch project fields: %w", err)
		}
	}

	// Validate required fields exist (IDPF only)
	if framework == "IDPF" {
		if hint, err := validateRequiredFields(projectFields, defs.Fields.Required); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v%s\n", err, hint)
			return err
		}
		if err := reconcileInitStatus(client, projectFields, defs.Fields.Required, cmd.ErrOrStderr()); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return err
		}

		// Create optional fields if missing (Priority, Branch, …) and refetch
		// so metadata captures any newly-created field IDs.
		ensureOptionalProjectFields(client, project.ID, defs.Fields.CreateIfMissing, cmd.ErrOrStderr())
		refetched, err := client.GetProjectFields(project.ID)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to refetch project fields: %v\n", err)
		} else {
			projectFields = refetched
		}
	}

	// Link repository to the project
	repoID, err := client.GetRepositoryID(repoOwner, repoName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not get repository ID: %v\n", err)
	} else {
		if linkErr := client.LinkProjectToRepository(project.ID, repoID); linkErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not link repository: %v\n", linkErr)
		}
	}

	// Convert fields to metadata
	metadata := &ProjectMetadata{
		ProjectID: project.ID,
	}
	for _, f := range projectFields {
		fm := FieldMetadata{
			ID:       f.ID,
			Name:     f.Name,
			DataType: f.DataType,
		}
		for _, opt := range f.Options {
			fm.Options = append(fm.Options, OptionMetadata{
				ID:   opt.ID,
				Name: opt.Name,
			})
		}
		metadata.Fields = append(metadata.Fields, fm)
	}

	// Create config
	cfg := &InitConfig{
		ProjectName:   project.Title,
		ProjectOwner:  owner,
		ProjectNumber: project.Number,
		Repositories:  []string{opts.repo},
		Framework:     framework,
	}

	// Write config
	cwd, _ := os.Getwd()
	if err := writeConfigWithMetadata(cwd, cfg, metadata); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to write config: %v\n", err)
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Created .gh-pmu.json for %s (#%d) [connected to existing project]\n",
		project.Title, project.Number)

	return nil
}

// existingConfigRaw is used for JSON unmarshaling to get framework + project number
type existingConfigRaw struct {
	Framework string `json:"framework"`
	Project   struct {
		Number int `json:"number"`
	} `json:"project"`
}

// sourceProjectGuard refuses --source-project when the repo already has a
// .gh-pmu.json declaring a project number, unless --force is passed. Steers
// the user toward the safer --project <existing> re-link path. Returns nil
// when there is nothing to guard (no existing project, --source-project not
// used, or --force present).
func sourceProjectGuard(existingProjectNumber, sourceProject int, force bool) error {
	if sourceProject == 0 || existingProjectNumber == 0 || force {
		return nil
	}
	return fmt.Errorf(
		".gh-pmu.json already declares project #%d. Refusing to replace it via --source-project.\n"+
			"  To re-link to the existing project: gh pmu init --project %d --repo <owner>/<repo>\n"+
			"  To intentionally replace it:        rerun with --force",
		existingProjectNumber, existingProjectNumber,
	)
}

// loadExistingFramework loads framework from existing config
func loadExistingFramework(dir string) (string, error) {
	raw, err := loadExistingRaw(dir)
	if err != nil {
		return "", err
	}
	return raw.Framework, nil
}

// loadExistingRaw reads the existing .gh-pmu.json (if present) and returns
// the fields needed for re-init guards: framework and current project number.
func loadExistingRaw(dir string) (*existingConfigRaw, error) {
	configPath, err := config.FindConfigFile(dir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var raw existingConfigRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return &raw, nil
}

// splitRepository splits "owner/repo" into owner and repo parts.
func splitRepository(repo string) (owner, name string) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

// deriveProjectTitle returns the project title for a given repository name.
func deriveProjectTitle(repoName string) string {
	return repoName
}

// InitConfig holds the configuration gathered during init.
type InitConfig struct {
	ProjectName   string
	ProjectOwner  string
	ProjectNumber int
	Repositories  []string
	Framework     string
}

// ConfigFile represents the .gh-pmu.json file structure.
type ConfigFile struct {
	Version      string                  `yaml:"version,omitempty" json:"version,omitempty"`
	Project      ProjectConfig           `yaml:"project" json:"project"`
	Repositories []string                `yaml:"repositories" json:"repositories"`
	Defaults     DefaultsConfig          `yaml:"defaults" json:"defaults"`
	Fields       map[string]FieldMapping `yaml:"fields" json:"fields"`
	Triage       map[string]TriageRule   `yaml:"triage,omitempty" json:"triage,omitempty"`
}

// ProjectConfig represents the project section of config.
type ProjectConfig struct {
	Name   string `yaml:"name,omitempty" json:"name,omitempty"`
	Number int    `yaml:"number" json:"number"`
	Owner  string `yaml:"owner" json:"owner"`
}

// DefaultsConfig represents default values for new items.
type DefaultsConfig struct {
	Priority string   `yaml:"priority" json:"priority"`
	Status   string   `yaml:"status" json:"status"`
	Labels   []string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// FieldMapping represents a field alias mapping.
type FieldMapping struct {
	Field  string            `yaml:"field" json:"field"`
	Values map[string]string `yaml:"values" json:"values"`
}

// ProjectMetadata holds cached project information from GitHub API.
type ProjectMetadata struct {
	ProjectID string
	Fields    []FieldMetadata
}

// FieldMetadata holds cached field information.
type FieldMetadata struct {
	ID       string
	Name     string
	DataType string
	Options  []OptionMetadata
}

// OptionMetadata holds option information for single-select fields.
type OptionMetadata struct {
	ID   string
	Name string
}

// MetadataSection represents the metadata section in config file.
type MetadataSection struct {
	Project MetadataProject `yaml:"project" json:"project"`
	Fields  []MetadataField `yaml:"fields" json:"fields"`
}

// MetadataProject holds the project ID.
type MetadataProject struct {
	ID string `yaml:"id" json:"id"`
}

// MetadataField represents a field in the metadata section.
type MetadataField struct {
	Name     string                `yaml:"name" json:"name"`
	ID       string                `yaml:"id" json:"id"`
	DataType string                `yaml:"data_type" json:"data_type"`
	Options  []MetadataFieldOption `yaml:"options,omitempty" json:"options,omitempty"`
}

// MetadataFieldOption represents a field option.
type MetadataFieldOption struct {
	Name string `yaml:"name" json:"name"`
	ID   string `yaml:"id" json:"id"`
}

// TriageRule represents a single triage rule configuration.
type TriageRule struct {
	Query       string          `yaml:"query" json:"query"`
	Apply       TriageApply     `yaml:"apply" json:"apply"`
	Interactive map[string]bool `yaml:"interactive,omitempty" json:"interactive,omitempty"`
}

// TriageApply represents what to apply when a triage rule matches.
type TriageApply struct {
	Labels []string          `yaml:"labels,omitempty" json:"labels,omitempty"`
	Fields map[string]string `yaml:"fields,omitempty" json:"fields,omitempty"`
}

// ConfigFileWithMetadata extends ConfigFile with metadata section.
type ConfigFileWithMetadata struct {
	Version      string                  `yaml:"version,omitempty" json:"version,omitempty"`
	Project      ProjectConfig           `yaml:"project" json:"project"`
	Repositories []string                `yaml:"repositories" json:"repositories"`
	Framework    string                  `yaml:"framework,omitempty" json:"framework,omitempty"`
	Defaults     DefaultsConfig          `yaml:"defaults" json:"defaults"`
	Fields       map[string]FieldMapping `yaml:"fields" json:"fields"`
	Triage       map[string]TriageRule   `yaml:"triage,omitempty" json:"triage,omitempty"`
	Acceptance   *config.Acceptance      `yaml:"acceptance,omitempty" json:"acceptance,omitempty"`
	Metadata     MetadataSection         `yaml:"metadata" json:"metadata"`
}

// ProjectValidator is the interface for validating projects.
type ProjectValidator interface {
	GetProject(owner string, number int) (interface{}, error)
}

// validateProject checks if the project exists.
func validateProject(client ProjectValidator, owner string, number int) error {
	_, err := client.GetProject(owner, number)
	return err
}

// writeConfig writes the configuration to a .gh-pmu.json file.
func writeConfig(dir string, cfg *InitConfig) error {
	// Safety check: prevent accidental writes to repo root during tests
	if protectRepoRoot.Load() && isRepoRoot(dir) {
		return ErrRepoRootProtected
	}

	configFile := &ConfigFile{
		Version: getVersion(),
		Project: ProjectConfig{
			Name:   cfg.ProjectName,
			Owner:  cfg.ProjectOwner,
			Number: cfg.ProjectNumber,
		},
		Repositories: cfg.Repositories,
		Defaults: DefaultsConfig{
			Priority: "p2",
			Status:   "backlog",
		},
		Fields: map[string]FieldMapping{
			"priority": {
				Field: "Priority",
				Values: map[string]string{
					"p0": "P0",
					"p1": "P1",
					"p2": "P2",
				},
			},
			"status": {
				Field: "Status",
				Values: map[string]string{
					"backlog":     "Backlog",
					"ready":       "Ready",
					"in_progress": "In progress",
					"in_review":   "In review",
					"done":        "Done",
				},
			},
		},
		Triage: map[string]TriageRule{
			"estimate": {
				Query: "is:issue is:open -has:estimate",
				Apply: TriageApply{},
				Interactive: map[string]bool{
					"estimate": true,
				},
			},
		},
	}

	jsonData, err := json.MarshalIndent(configFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON config: %w", err)
	}
	jsonData = append(jsonData, '\n')
	jsonPath := filepath.Join(dir, config.ConfigFileName)
	if err := os.WriteFile(jsonPath, jsonData, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// writeConfigWithMetadata writes the configuration with project metadata.
func writeConfigWithMetadata(dir string, cfg *InitConfig, metadata *ProjectMetadata) error {
	// Safety check: prevent accidental writes to repo root during tests
	if protectRepoRoot.Load() && isRepoRoot(dir) {
		return ErrRepoRootProtected
	}

	// Convert metadata to YAML format
	var metadataFields []MetadataField
	for _, f := range metadata.Fields {
		mf := MetadataField{
			Name:     f.Name,
			ID:       f.ID,
			DataType: f.DataType,
		}
		for _, opt := range f.Options {
			mf.Options = append(mf.Options, MetadataFieldOption{
				Name: opt.Name,
				ID:   opt.ID,
			})
		}
		metadataFields = append(metadataFields, mf)
	}

	// Build field mappings dynamically from metadata
	fieldMappings := buildFieldMappingsFromMetadata(metadata)

	// Read existing acceptance + preserve user-renamed branch alias on re-init
	var existingAcceptance *config.Acceptance
	existingJSONPath := filepath.Join(dir, config.ConfigFileName)
	if existingCfg, err := config.Load(existingJSONPath); err == nil {
		if existingCfg.Acceptance != nil && !config.RequiresReAcceptance(existingCfg.Acceptance.Version, getVersion()) {
			existingAcceptance = existingCfg.Acceptance
		}
		if prev, ok := existingCfg.Fields["branch"]; ok && prev.Field != "" {
			fieldMappings["branch"] = FieldMapping{Field: prev.Field}
		}
	}

	configFile := &ConfigFileWithMetadata{
		Version: getVersion(),
		Project: ProjectConfig{
			Name:   cfg.ProjectName,
			Owner:  cfg.ProjectOwner,
			Number: cfg.ProjectNumber,
		},
		Repositories: cfg.Repositories,
		Framework:    cfg.Framework,
		Defaults: DefaultsConfig{
			Priority: "p2",
			Status:   "backlog",
		},
		Fields: fieldMappings,
		Triage: map[string]TriageRule{
			"estimate": {
				Query: "is:issue is:open -has:estimate",
				Apply: TriageApply{},
				Interactive: map[string]bool{
					"estimate": true,
				},
			},
		},
		Acceptance: existingAcceptance,
		Metadata: MetadataSection{
			Project: MetadataProject{
				ID: metadata.ProjectID,
			},
			Fields: metadataFields,
		},
	}

	jsonData, err := json.MarshalIndent(configFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON config: %w", err)
	}
	jsonData = append(jsonData, '\n')
	jsonPath := filepath.Join(dir, config.ConfigFileName)
	if err := os.WriteFile(jsonPath, jsonData, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// buildFieldMappingsFromMetadata builds field mappings dynamically from project metadata.
// This ensures all field options (including "Parking Lot") are included in the config.
func buildFieldMappingsFromMetadata(metadata *ProjectMetadata) map[string]FieldMapping {
	mappings := make(map[string]FieldMapping)

	// Find Status and Priority fields in metadata
	for _, field := range metadata.Fields {
		fieldNameLower := strings.ToLower(field.Name)

		if fieldNameLower == "status" && len(field.Options) > 0 {
			values := make(map[string]string)
			for _, opt := range field.Options {
				alias := optionNameToAlias(opt.Name)
				values[alias] = opt.Name
			}
			mappings["status"] = FieldMapping{
				Field:  field.Name,
				Values: values,
			}
		}

		if fieldNameLower == "priority" && len(field.Options) > 0 {
			values := make(map[string]string)
			for _, opt := range field.Options {
				alias := optionNameToAlias(opt.Name)
				values[alias] = opt.Name
			}
			mappings["priority"] = FieldMapping{
				Field:  field.Name,
				Values: values,
			}
		}

		if fieldNameLower == "branch" {
			mappings["branch"] = FieldMapping{Field: field.Name}
		}
	}

	// Fallback to defaults if fields not found in metadata
	if _, ok := mappings["status"]; !ok {
		mappings["status"] = FieldMapping{
			Field: "Status",
			Values: map[string]string{
				"backlog":     "Backlog",
				"ready":       "Ready",
				"in_progress": "In progress",
				"in_review":   "In review",
				"done":        "Done",
			},
		}
	}

	if _, ok := mappings["priority"]; !ok {
		mappings["priority"] = FieldMapping{
			Field: "Priority",
			Values: map[string]string{
				"p0": "P0",
				"p1": "P1",
				"p2": "P2",
			},
		}
	}

	return mappings
}

// optionNameToAlias converts a field option name to a CLI-friendly alias.
// Examples: "In progress" -> "in_progress", "🅿️ Parking Lot" -> "parking_lot"
func optionNameToAlias(name string) string {
	// Remove common emoji prefixes (strip all non-ASCII characters)
	var cleaned strings.Builder
	for _, r := range name {
		if r < 128 { // ASCII only
			cleaned.WriteRune(r)
		}
	}
	result := strings.TrimSpace(cleaned.String())

	// Convert to lowercase and replace spaces with underscores
	result = strings.ToLower(result)
	result = strings.ReplaceAll(result, " ", "_")

	// Remove any double underscores
	for strings.Contains(result, "__") {
		result = strings.ReplaceAll(result, "__", "_")
	}

	// Trim leading/trailing underscores
	result = strings.Trim(result, "_")

	return result
}

// optionalFieldClient is the subset of *api.Client used by
// ensureOptionalProjectFields — kept minimal so init's create-if-missing
// helper is unit-testable with a mock.
type optionalFieldClient interface {
	FieldExists(projectID, name string) (bool, error)
	CreateProjectField(projectID, name, dataType string, singleSelectOptions []string) (*api.ProjectField, error)
}

// ensureOptionalProjectFields creates each field in defs on the project if it
// does not already exist. Failures emit a warning to errOut and never abort
// the loop — every field gets a chance to be created independently. Used by
// both init paths (--source-project and --project) so they have parity.
func ensureOptionalProjectFields(client optionalFieldClient, projectID string, defs []defaults.FieldDef, errOut io.Writer) {
	for _, optField := range defs {
		exists, err := client.FieldExists(projectID, optField.Name)
		if err != nil {
			fmt.Fprintf(errOut, "Warning: failed to check field %q: %v\n", optField.Name, err)
			continue
		}
		if exists {
			continue
		}
		if _, err := client.CreateProjectField(projectID, optField.Name, optField.Type, optField.Options); err != nil {
			fmt.Fprintf(errOut, "Warning: failed to create field %q: %v\n", optField.Name, err)
		}
	}
}

// missingFieldHint is operator guidance appended to stderr when a required
// field is absent. It is deliberately not part of the returned error: both init
// paths returned byte-identical error strings before this validation was
// shared, and only runInitExistingProject printed the longer hint (#874).
const missingFieldHint = " — create it in the project settings before connecting"

// validateRequiredFields checks that every IDPF-required field exists on the
// project with the expected data type.
//
// Required options are not checked here: a missing Status value is added by
// reconcileInitStatus rather than rejected (#917).
//
// The failure is returned as an error plus an optional stderr-only hint.
// Callers that print to stderr append the hint; callers that route failures
// through rollback plumbing ignore it.
func validateRequiredFields(projectFields []api.ProjectField, required []defaults.FieldDef) (string, error) {
	for _, reqField := range required {
		field := findFieldByName(projectFields, reqField.Name)
		if field == nil {
			return missingFieldHint, fmt.Errorf("required field %q not found in project", reqField.Name)
		}

		if field.DataType != reqField.Type {
			return "", fmt.Errorf("field %q has type %s, expected %s", reqField.Name, field.DataType, reqField.Type)
		}
	}
	return "", nil
}

// statusOptionsUpdater is the API surface a Status reconcile mutates through.
type statusOptionsUpdater interface {
	UpdateProjectFieldOptions(fieldID string, options []api.FieldOptionUpdate) ([]api.FieldOption, error)
}

// reconcileInitStatus adds missing required Status values and fixes case-only
// name differences in place (#917). Existing colors and descriptions are left
// as found; recoloring is `gh pmu status --update`'s job.
//
// A Status field served from cached metadata is skipped with a warning: cached
// options carry no colors or descriptions and may be stale, and the update
// replaces the whole option list. An update failure is returned.
func reconcileInitStatus(client statusOptionsUpdater, projectFields []api.ProjectField, required []defaults.FieldDef, errOut io.Writer) error {
	var canonical []defaults.OptionDef
	for _, reqField := range required {
		if reqField.Name == "Status" {
			canonical = reqField.OptionDefs
		}
	}
	field := findFieldByName(projectFields, "Status")
	if len(canonical) == 0 || field == nil {
		return nil
	}

	if field.FromCache {
		fmt.Fprintf(errOut, "Warning: skipping Status reconcile: live field data is unavailable (cached metadata in use); run 'gh pmu status --update' once GitHub recovers\n")
		return nil
	}

	plan, err := planStatusReconcile(field.Options, canonical, statusReconcileInit)
	if err != nil {
		fmt.Fprintf(errOut, "Warning: skipping Status reconcile: %v\n", err)
		return nil
	}
	if !plan.Changed {
		return nil
	}

	if _, err := client.UpdateProjectFieldOptions(field.ID, plan.Options); err != nil {
		return fmt.Errorf("failed to add required Status values: %w", err)
	}
	for _, entry := range plan.Entries {
		switch {
		case entry.has(statusActionAdded):
			fmt.Fprintf(errOut, "Status: added %q\n", entry.Name)
		case entry.has(statusActionRenamed):
			fmt.Fprintf(errOut, "Status: renamed %q to %q\n", entry.Previous, entry.Name)
		}
	}
	return nil
}

// findFieldByName searches for a field by name in a slice of ProjectFields.
// Returns nil if not found.
func findFieldByName(fields []api.ProjectField, name string) *api.ProjectField {
	for i := range fields {
		if fields[i].Name == name {
			return &fields[i]
		}
	}
	return nil
}
