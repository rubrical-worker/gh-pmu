package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/rubrical-works/gh-pmu/internal/integrity"
	"github.com/spf13/cobra"
)

type fieldCreateOptions struct {
	fieldType string
	options   []string
	yes       bool
}

// fieldClient defines the interface for API methods used by field commands.
type fieldClient interface {
	GetProject(owner string, number int) (*api.Project, error)
	GetProjectFields(projectID string) ([]api.ProjectField, error)
	CreateProjectField(projectID, name, dataType string, singleSelectOptions []string) (*api.ProjectField, error)
}

func newFieldCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "field",
		Short: "Manage project fields",
		Long: `Manage custom fields in your GitHub project.

Use subcommands to create, list, or manage project fields.`,
	}

	cmd.AddCommand(newFieldCreateCommand())
	cmd.AddCommand(newFieldListCommand())

	return cmd
}

func newFieldCreateCommand() *cobra.Command {
	opts := &fieldCreateOptions{}

	cmd := &cobra.Command{
		Use:   "create <field-name>",
		Short: "Create a new project field",
		Long: `Create a new custom field in the configured GitHub project.

Supported field types:
  - text (default): A text field for free-form input
  - number: A numeric field
  - date: A date field
  - single_select: A single-select dropdown field (requires --option flags)

Examples:
  # Create a TEXT field
  gh pmu field create PRD --type text

  # Create a NUMBER field
  gh pmu field create "Story Points" --type number

  # Create a SINGLE_SELECT field with options
  gh pmu field create "Environment" --type single_select --option "Development" --option "Staging" --option "Production"

  # Skip confirmation prompt
  gh pmu field create PRD --type text --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFieldCreate(cmd, args, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.fieldType, "type", "t", "text", "Field type: text, number, date, single_select")
	cmd.Flags().StringArrayVarP(&opts.options, "option", "o", nil, "Options for single_select field (can be repeated)")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

func newFieldListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all project fields",
		Long: `List all fields in the configured GitHub project.

Shows the field name, type, and available options for single-select fields.`,
		RunE: runFieldList,
	}

	return cmd
}

func runFieldCreate(cmd *cobra.Command, args []string, opts *fieldCreateOptions) error {
	fieldName := args[0]

	// Validate field type
	dataType := strings.ToUpper(opts.fieldType)
	switch dataType {
	case "TEXT", "NUMBER", "DATE", "SINGLE_SELECT":
		// valid
	default:
		return fmt.Errorf("invalid field type %q: must be text, number, date, or single_select", opts.fieldType)
	}

	// Validate single_select has options
	if dataType == "SINGLE_SELECT" && len(opts.options) == 0 {
		return fmt.Errorf("single_select field requires at least one --option")
	}

	// Load configuration
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	cfg, err := config.LoadFromDirectory(cwd)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'gh pmu init' to create a configuration file", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create API client
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	return runFieldCreateWithDeps(cmd, fieldName, opts, cfg, client)
}

func runFieldCreateWithDeps(cmd *cobra.Command, fieldName string, opts *fieldCreateOptions, cfg *config.Config, client fieldClient) error {
	dataType := strings.ToUpper(opts.fieldType)

	// Get project
	project, err := client.GetProject(cfg.Project.Owner, cfg.Project.Number)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	// Check if field already exists
	fields, err := client.GetProjectFields(project.ID)
	if err != nil {
		return fmt.Errorf("failed to get project fields: %w", err)
	}

	for _, f := range fields {
		if strings.EqualFold(f.Name, fieldName) {
			return fmt.Errorf("field %q already exists in the project (type: %s)", fieldName, f.DataType)
		}
	}

	// Require --yes for confirmation
	if !opts.yes {
		fmt.Fprintf(cmd.OutOrStdout(), "Create field in project %q:\n", project.Title)
		fmt.Fprintf(cmd.OutOrStdout(), "  Name: %s\n", fieldName)
		fmt.Fprintf(cmd.OutOrStdout(), "  Type: %s\n", dataType)
		if len(opts.options) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "  Options: %s\n", strings.Join(opts.options, ", "))
		}
		return fmt.Errorf("use --yes to confirm field creation")
	}

	// Create the field
	createdField, err := client.CreateProjectField(project.ID, fieldName, dataType, opts.options)
	if err != nil {
		return fmt.Errorf("failed to create field: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "✓ Created field %q (type: %s)\n", createdField.Name, createdField.DataType)

	// Update config metadata
	configPath, err := config.FindConfigFile(mustGetwd())
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "⚠ Could not find config file to update metadata\n")
		return nil
	}

	fieldMeta := config.FieldMetadata{
		Name:     createdField.Name,
		ID:       createdField.ID,
		DataType: createdField.DataType,
		Kind:     api.FieldKindProject,
	}
	for _, opt := range createdField.Options {
		fieldMeta.Options = append(fieldMeta.Options, config.OptionMetadata{
			Name: opt.Name,
			ID:   opt.ID,
		})
	}

	cfg.AddFieldMetadata(fieldMeta)
	if err := cfg.Save(filepath.Dir(configPath)); err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "⚠ Created field but failed to update config: %v\n", err)
		return nil
	}

	// Update checksum after saving config
	_ = integrity.UpdateChecksumForConfig(configPath)

	fmt.Fprintf(cmd.OutOrStdout(), "✓ Updated .gh-pmu.json metadata\n")

	return nil
}

func runFieldList(cmd *cobra.Command, args []string) error {
	// Load configuration
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	cfg, err := config.LoadFromDirectory(cwd)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'gh pmu init' to create a configuration file", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create API client
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	return runFieldListWithDeps(cmd, cfg, client)
}

func runFieldListWithDeps(cmd *cobra.Command, cfg *config.Config, client fieldClient) error {
	// Get project
	project, err := client.GetProject(cfg.Project.Owner, cfg.Project.Number)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	// Get fields
	fields, err := client.GetProjectFields(project.ID)
	if err != nil {
		return fmt.Errorf("failed to get project fields: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Fields in project %q:\n\n", project.Title)

	for _, f := range fields {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s (%s)\n", f.Name, f.DataType)
		if len(f.Options) > 0 {
			var optNames []string
			for _, opt := range f.Options {
				optNames = append(optNames, opt.Name)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "    Options: %s\n", strings.Join(optNames, ", "))
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nTotal: %d fields\n", len(fields))

	return nil
}

func mustGetwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
