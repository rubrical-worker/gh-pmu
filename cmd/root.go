package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/rubrical-works/gh-pmu/internal/defaults"
	pkgversion "github.com/rubrical-works/gh-pmu/internal/version"
	"github.com/spf13/cobra"
)

// version is set by ldflags during goreleaser builds.
// When empty (default), falls back to the source constant in internal/version.
var version = ""

func getVersion() string {
	if version != "" {
		return version
	}
	return pkgversion.Version
}

// exemptCommands are commands that do not require terms acceptance.
var exemptCommands = map[string]bool{
	"init":   true,
	"accept": true,
	"help":   true,
}

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "pmu",
		Annotations: map[string]string{
			cobra.CommandDisplayNameAnnotation: "gh pmu",
		},
		Short: "GitHub Praxis Management Utility",
		Long: `gh pmu — GitHub Praxis Management Utility.

A GitHub CLI extension for project workflows, sub-issue hierarchies,
and batch operations. Designed for Kanban-style GitHub Projects with
status-based columns (Backlog, In Progress, In Review, Done).

Works seamlessly with the IDPF-Praxis framework for structured
development workflows, or standalone without any framework.

This extension combines and replaces:
  - gh-pm (https://github.com/yahsan2/gh-pm) - Project management
  - gh-sub-issue (https://github.com/yahsan2/gh-sub-issue) - Sub-issue hierarchy

Use 'gh pmu <command> --help' for more information about a command.`,
		Version: getVersion(),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			applyFallbackFlags(cmd)
			refreshConfigVersion(cmd)
			if err := checkAcceptance(cmd); err != nil {
				return err
			}
			return runDailyIntegrityCheck(cmd)
		},
	}

	cmd.SetVersionTemplate("{{.CommandPath}} version {{.Version}}\nRubrical Works (c) 2026\n")

	// Persistent flags wired from #853 — resilience controls for the
	// ProjectV2 fields resolver. Available on every subcommand; consumed
	// by api.GetProjectFields via the package-level FallbackOptions.
	cmd.PersistentFlags().Bool("no-cache-fallback", false,
		"Disable cached-metadata fallback; propagate ProjectV2 fields resolver errors unchanged (fail-loud).")
	cmd.PersistentFlags().Bool("refresh-cached-fields", false,
		"When fallback engages, refresh cached field metadata via per-name ProjectV2.field(name:) queries.")

	cmd.AddCommand(newInitCommand())
	cmd.AddCommand(newListCommand())
	cmd.AddCommand(newViewCommand())
	cmd.AddCommand(newCreateCommand())
	cmd.AddCommand(newEditCommand())
	cmd.AddCommand(newCommentCommand())
	cmd.AddCommand(newMoveCommand())
	cmd.AddCommand(newCloseCommand())
	cmd.AddCommand(newBoardCommand())
	cmd.AddCommand(newSubCommand())
	cmd.AddCommand(newFieldCommand())
	cmd.AddCommand(newIntakeCommand())
	cmd.AddCommand(newTriageCommand())
	cmd.AddCommand(newSplitCommand())
	cmd.AddCommand(newHistoryCommand())
	cmd.AddCommand(newFilterCommand())
	cmd.AddCommand(newBranchCommand())
	cmd.AddCommand(newAcceptCommand())
	cmd.AddCommand(newLabelCommand())
	cmd.AddCommand(newConfigCommand())
	cmd.AddCommand(newStatusCommand())

	return cmd
}

func Execute() error {
	return NewRootCommand().Execute()
}

// applyFallbackFlags propagates the resilience flags from cobra into the
// api package so api.GetProjectFields can honour them. Both flags are
// silent no-ops when unset. Errors from flag lookup are ignored — these are
// optional controls and missing values default to the safe production path.
func applyFallbackFlags(cmd *cobra.Command) {
	noFallback, _ := cmd.Flags().GetBool("no-cache-fallback")
	refresh, _ := cmd.Flags().GetBool("refresh-cached-fields")
	api.SetFallbackOptions(api.FallbackOptions{
		NoCacheFallback:     noFallback,
		RefreshCachedFields: refresh,
	})
}

// shouldRefreshVersion reports whether the version refresh runs for a command.
//
// It mirrors the guard checkAcceptance and runDailyIntegrityCheck already apply:
// the commands a user reaches for before a repo is configured must not write to
// the config as a side effect of being run.
func shouldRefreshVersion(name string) bool {
	return !exemptCommands[name]
}

// refreshConfigVersion stamps the running version into .gh-pmu.json so the file
// records which gh pmu version last wrote it.
//
// The check runs on every non-exempt command; the write happens only when the
// stored version is stale (see config.RefreshVersion). Failures warn and never
// abort the command — a version stamp is bookkeeping, not a precondition.
func refreshConfigVersion(cmd *cobra.Command) {
	if !shouldRefreshVersion(cmd.Name()) {
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		if os.Getenv("GH_PMU_DEBUG") != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "Debug: version refresh skipped (getwd failed): %v\n", err)
		}
		return
	}

	jsonPath, err := config.FindConfigFile(cwd)
	if err != nil {
		// No config file — expected for uninitialized repos
		return
	}

	// Only refresh a JSON config (not a YAML fallback)
	if filepath.Ext(jsonPath) != ".json" {
		return
	}

	refreshConfigVersionInDir(filepath.Dir(jsonPath), getVersion(), cmd.ErrOrStderr())
}

// refreshConfigVersionInDir is the testable half of refreshConfigVersion: it
// takes the config directory outright rather than resolving it from the process
// working directory.
func refreshConfigVersionInDir(dir string, currentVersion string, w io.Writer) {
	// Same safety check writeConfig carries. go test ./cmd/ runs with the package
	// directory as cwd and FindConfigFile walks up to the repository's own
	// .gh-pmu.json, so without this every command test that reaches
	// PersistentPreRunE would rewrite the real config (#436).
	if protectRepoRoot.Load() && isRepoRoot(dir) {
		return
	}

	if _, err := os.Stat(filepath.Join(dir, config.ConfigFileName)); err != nil {
		// No config file — expected for uninitialized repos
		return
	}

	if _, err := config.RefreshVersion(dir, currentVersion); err != nil {
		fmt.Fprintf(w, "Warning: config version refresh failed: %v\n", err)
	}
}

// checkAcceptance verifies terms have been accepted before running commands.
func checkAcceptance(cmd *cobra.Command) error {
	// Dev/source builds skip acceptance gate — only ldflags-injected builds enforce it
	if version == "" {
		return nil
	}

	// Check if this is an exempt command
	name := cmd.Name()
	if exemptCommands[name] {
		return nil
	}

	// --help flag on any command is always allowed
	if help, _ := cmd.Flags().GetBool("help"); help {
		return nil
	}

	// Try to load config — if no config exists, skip gate (init not run yet)
	cwd, err := os.Getwd()
	if err != nil {
		if os.Getenv("GH_PMU_DEBUG") != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "Debug: acceptance check skipped (getwd failed): %v\n", err)
		}
		return nil
	}

	cfg, err := config.LoadFromDirectory(cwd)
	if err != nil {
		// No config file — not initialized yet, skip acceptance check
		return nil
	}

	// Check acceptance state
	if cfg.Acceptance == nil || !cfg.Acceptance.Accepted {
		printTermsAndHint(cmd)
		return fmt.Errorf("terms not accepted — run 'gh pmu accept' first")
	}

	// Check version — re-acceptance needed on major/minor bump
	if config.RequiresReAcceptance(cfg.Acceptance.Version, getVersion()) {
		printTermsAndHint(cmd)
		return fmt.Errorf("terms acceptance outdated (accepted v%s, current v%s) — run 'gh pmu accept' to re-accept",
			cfg.Acceptance.Version, getVersion())
	}

	return nil
}

// runDailyIntegrityCheck performs the throttled config integrity check.
// Runs silently if no config exists or if already checked today.
func runDailyIntegrityCheck(cmd *cobra.Command) error {
	// Skip for exempt commands
	name := cmd.Name()
	if exemptCommands[name] || name == "config" {
		return nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}

	configPath, err := config.FindConfigFile(cwd)
	if err != nil {
		return nil // No config — skip
	}

	return runIntegrityCheck(configPath, cmd.ErrOrStderr())
}

// printTermsAndHint writes the full terms text and acceptance hints to stderr.
func printTermsAndHint(cmd *cobra.Command) {
	w := cmd.ErrOrStderr()
	fmt.Fprintln(w)
	fmt.Fprintln(w, defaults.Terms())
	fmt.Fprintln(w, "Run 'gh pmu accept --yes' for non-interactive acceptance.")
	fmt.Fprintln(w, "Acceptance persists in .gh-pmu.json (one-time per repo).")
	fmt.Fprintln(w)
}
