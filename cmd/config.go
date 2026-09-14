package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rubrical-works/gh-pmu/internal/api"
	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/rubrical-works/gh-pmu/internal/defaults"
	"github.com/rubrical-works/gh-pmu/internal/integrity"
	"github.com/spf13/cobra"
)

type configVerifyOptions struct {
	dir         string
	remote      bool
	resolveView bool
}

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management commands",
	}

	cmd.AddCommand(newConfigVerifyCommand())

	return cmd
}

func newConfigVerifyCommand() *cobra.Command {
	opts := &configVerifyOptions{}

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify config integrity against git HEAD",
		Long: `Check .gh-pmu.json for unauthorized or accidental modifications.

Compares the local config against the last committed version (git HEAD)
and reports any differences. Optionally compares against origin/main.

In strict mode (configIntegrity: "strict" in .gh-pmu.json), returns
a non-zero exit code when drift is detected.

Verification is read-only. Pass --resolve-view to additionally resolve
the Backlog board view number from the API and write it to project.view;
that is the one mode in which this command modifies the config.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigVerify(cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.dir, "dir", "", "Directory to search for config (default: current directory)")
	cmd.Flags().BoolVar(&opts.remote, "remote", false, "Also compare against origin/main")
	cmd.Flags().BoolVar(&opts.resolveView, "resolve-view", false, "Re-resolve project.view even when it is already set")

	return cmd
}

func runConfigVerify(cmd *cobra.Command, opts *configVerifyOptions) error {
	dir := opts.dir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	configPath, err := config.FindConfigFile(dir)
	if err != nil {
		return fmt.Errorf("no config file found: %w", err)
	}

	configDir := filepath.Dir(configPath)
	configName := filepath.Base(configPath)
	out := cmd.OutOrStdout()

	// project.view resolution is opt-in. Plain `config verify` stays read-only,
	// as its name and help text promise — a verification run that rewrites the
	// thing it verifies is a surprise, and it would need network and auth for a
	// command that otherwise needs neither.
	//
	// When it does run, it runs before the local file is read: the comparison
	// below hashes whatever is on disk at that moment, so resolving afterwards
	// would make this run report the write it just performed.
	if opts.resolveView {
		cfg, cfgErr := config.Load(configPath)
		if cfgErr != nil {
			return fmt.Errorf("cannot resolve project.view: %w", cfgErr)
		}
		client, clientErr := api.NewClient()
		if clientErr != nil {
			// Explicitly asked for, so say why it did not happen — but do not
			// fail the verification the user also asked for.
			fmt.Fprintf(out, "Warning: could not resolve project.view: %v\n", clientErr)
		} else if err := resolveAndPersistView(cfg, configPath, client, out, true); err != nil {
			return err
		}
	}

	// Read local config
	localContent, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read local config: %w", err)
	}

	// Read committed config via git show (cwd-relative pathspec — correct in a
	// monorepo subdirectory, not just at the repo root)
	committedContent, err := gitShowFile(configDir, configPathspec("HEAD", configName))
	if err != nil {
		fmt.Fprintf(out, "Warning: could not read committed config: %v\n", err)
		committedContent = nil
	}

	// Compare local vs HEAD
	result, err := integrity.CompareContent(localContent, committedContent)
	if err != nil {
		return fmt.Errorf("comparison failed: %w", err)
	}

	// Checksum comparison — surface I/O errors to stderr rather than
	// silently discarding them. LoadChecksum already returns ("", nil) when
	// no checksum file is present, so this only fires for real failures.
	currentChecksum, err := integrity.ComputeChecksum(configPath)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not compute checksum: %v\n", err)
	}
	storedChecksum, err := integrity.LoadChecksum(configDir)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not read stored checksum: %v\n", err)
	}

	fmt.Fprintf(out, "Config: %s\n", configPath)
	fmt.Fprintf(out, "SHA-256: %s\n", currentChecksum)

	if storedChecksum != "" {
		if currentChecksum == storedChecksum {
			fmt.Fprintf(out, "Checksum: matches stored value\n")
		} else {
			fmt.Fprintf(out, "Checksum: MISMATCH (stored: %s)\n", storedChecksum)
		}
	}

	if !result.Drifted {
		fmt.Fprintf(out, "\nNo drift detected — local config matches HEAD.\n")
	} else {
		fmt.Fprintf(out, "\nDrift detected — local config differs from HEAD:\n")
		fmt.Fprintf(out, "  Changed:\n")
		for _, change := range result.Changes {
			fmt.Fprintf(out, "    • %s\n", change)
		}
		if len(result.Unchanged) > 0 {
			fmt.Fprintf(out, "  Unchanged:\n")
			for _, section := range result.Unchanged {
				fmt.Fprintf(out, "    - %s\n", section)
			}
		}
	}

	// Critical field check against HEAD
	var hasCriticalDrift bool
	if committedContent != nil {
		criticalChanges := compareCriticalFields(localContent, committedContent)
		if len(criticalChanges) > 0 {
			hasCriticalDrift = true
			writeCriticalAlert(cmd.ErrOrStderr(), criticalChanges)
		}
	}

	// Remote comparison
	if opts.remote {
		remoteContent, err := gitShowFile(configDir, configPathspec("origin/main", configName))
		if err != nil {
			fmt.Fprintf(out, "\nRemote: could not read origin/main config: %v\n", err)
		} else {
			remoteResult, err := integrity.CompareContent(localContent, remoteContent)
			if err == nil {
				if !remoteResult.Drifted {
					fmt.Fprintf(out, "\nRemote: local config matches origin/main.\n")
				} else {
					fmt.Fprintf(out, "\nRemote: local config differs from origin/main:\n")
					fmt.Fprintf(out, "  Changed:\n")
					for _, change := range remoteResult.Changes {
						fmt.Fprintf(out, "    • %s\n", change)
					}
					if len(remoteResult.Unchanged) > 0 {
						fmt.Fprintf(out, "  Unchanged:\n")
						for _, section := range remoteResult.Unchanged {
							fmt.Fprintf(out, "    - %s\n", section)
						}
					}
				}

				// Critical field check against remote
				remoteCritical := compareCriticalFields(localContent, remoteContent)
				if len(remoteCritical) > 0 {
					hasCriticalDrift = true
					writeCriticalAlert(cmd.ErrOrStderr(), remoteCritical)
				}
			}
		}
	}

	// Required Status values (#917). Read from the local config only, so verify
	// stays offline and needs no auth. Advisory: never affects the exit code,
	// including in strict mode.
	if missing := missingRequiredStatusValues(localContent); len(missing) > 0 {
		writeMissingStatusAlert(cmd.ErrOrStderr(), missing)
	}

	// Strict mode check — decided from HEAD-or-local so removing the strict key
	// locally cannot disable enforcement while HEAD still declares it.
	if (result.Drifted || hasCriticalDrift) && isStrictModeEither(localContent, committedContent) {
		return fmt.Errorf("config integrity check failed (strict mode) — resolve drift before continuing")
	}

	return nil
}

// backlogViewResolver is the slice of the API client that view resolution
// needs. Narrowing it to one method keeps the resolve logic testable without a
// network client or a gh auth token.
type backlogViewResolver interface {
	ResolveBacklogViewNumber(owner string, number int) (int, bool, error)
}

// resolveAndPersistView fills in project.view and writes it to configPath.
//
// This is the single explicit resolve site, deliberately not a side effect of
// config.LoadFromDirectory. The config is read up to four times per invocation
// (refreshConfigVersion, checkAcceptance, runIntegrityCheck, then the subcommand's
// own load), so a hook on load would fire a network call and a file write
// several times for one command.
//
// Three outcomes, kept apart:
//
//   - a view is found      -> written, checksum refreshed
//   - the board has none   -> reported, nothing written, never defaulted to 1
//   - resolution failed    -> warned, nothing written, and NOT an error
//
// The last one matters: making a resolve failure fatal would turn config verify
// into a command that cannot run offline or unauthenticated, which is a worse
// outcome than an unresolved cache field.
//
// force re-resolves a value that is already present. Without it an existing
// value is left alone, because a number already in the config is the user's —
// possibly hand-corrected — and silently overwriting it would discard that.
func resolveAndPersistView(cfg *config.Config, configPath string, resolver backlogViewResolver, w io.Writer, force bool) error {
	if cfg.HasResolvedView() && !force {
		return nil
	}

	number, found, err := resolver.ResolveBacklogViewNumber(cfg.Project.Owner, cfg.Project.Number)
	if err != nil {
		fmt.Fprintf(w, "Warning: could not resolve the Backlog board view: %v\n", err)
		return nil
	}
	if !found {
		fmt.Fprintf(w, "Note: project %s/%d has no Backlog board view — project.view left unset.\n",
			cfg.Project.Owner, cfg.Project.Number)
		return nil
	}

	cfg.Project.View = number
	if err := cfg.Save(filepath.Dir(configPath)); err != nil {
		// Fail loud. The checksum is deliberately not touched: recording a
		// checksum for a file that was not written would describe a state that
		// does not exist on disk.
		return fmt.Errorf("failed to persist project.view: %w", err)
	}

	if err := integrity.UpdateChecksumForConfig(configPath); err != nil {
		return fmt.Errorf("project.view was written but its checksum could not be updated: %w", err)
	}

	fmt.Fprintf(w, "Resolved project.view = %d (Backlog board view).\n", number)
	return nil
}

// criticalFieldChange represents a change to a critical config field.
type criticalFieldChange struct {
	Field    string
	OldValue string
	NewValue string
}

// compareCriticalFields compares identity fields between local and reference configs.
// Returns nil if no critical fields changed.
func compareCriticalFields(local, reference []byte) []criticalFieldChange {
	type configShape struct {
		Project struct {
			Owner  string `json:"owner"`
			Number int    `json:"number"`
		} `json:"project"`
		Repositories []string `json:"repositories"`
	}

	var localCfg, refCfg configShape
	if err := json.Unmarshal(local, &localCfg); err != nil {
		return nil
	}
	if err := json.Unmarshal(reference, &refCfg); err != nil {
		return nil
	}

	var changes []criticalFieldChange

	if localCfg.Project.Owner != refCfg.Project.Owner {
		changes = append(changes, criticalFieldChange{
			Field:    "project.owner",
			OldValue: refCfg.Project.Owner,
			NewValue: localCfg.Project.Owner,
		})
	}
	if localCfg.Project.Number != refCfg.Project.Number {
		changes = append(changes, criticalFieldChange{
			Field:    "project.number",
			OldValue: fmt.Sprintf("%d", refCfg.Project.Number),
			NewValue: fmt.Sprintf("%d", localCfg.Project.Number),
		})
	}

	localRepo := ""
	if len(localCfg.Repositories) > 0 {
		localRepo = localCfg.Repositories[0]
	}
	refRepo := ""
	if len(refCfg.Repositories) > 0 {
		refRepo = refCfg.Repositories[0]
	}
	if localRepo != refRepo {
		changes = append(changes, criticalFieldChange{
			Field:    "repositories[0]",
			OldValue: refRepo,
			NewValue: localRepo,
		})
	}

	return changes
}

// missingRequiredStatusValues returns the required Status values from the
// embedded defaults that have no case-insensitive match among the config's
// fields.status.values, in template order. Unparseable content reports nothing:
// verify's drift checks already cover a broken file.
func missingRequiredStatusValues(content []byte) []string {
	var cfg struct {
		Fields map[string]struct {
			Values map[string]string `json:"values"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil
	}
	defs, err := defaults.Load()
	if err != nil {
		return nil
	}

	present := map[string]bool{}
	for _, value := range cfg.Fields["status"].Values {
		present[strings.ToLower(value)] = true
	}

	var missing []string
	for _, f := range defs.Fields.Required {
		if f.Name != "Status" {
			continue
		}
		for _, name := range f.Options {
			if !present[strings.ToLower(name)] {
				missing = append(missing, name)
			}
		}
	}
	return missing
}

// writeMissingStatusAlert writes a boxed warning naming each missing required
// Status value and the command that adds them. Same box as writeCriticalAlert.
func writeMissingStatusAlert(w io.Writer, missing []string) {
	const width = 63
	border := strings.Repeat("─", width)
	row := func(text string) {
		padding := width + 2 - len([]rune(text))
		if padding < 1 {
			padding = 1
		}
		fmt.Fprintf(w, "│%s%s│\n", text, strings.Repeat(" ", padding))
	}

	fmt.Fprintf(w, "\n┌─%s─┐\n", border)
	row("  ⚠ REQUIRED STATUS VALUES MISSING")
	fmt.Fprintf(w, "├─%s─┤\n", border)
	row("")
	row("  .gh-pmu.json has no Status alias for these required values:")
	row("")
	for _, name := range missing {
		row("    • " + name)
	}
	row("")
	row("  Add them to the project board and refresh the aliases with:")
	row("    gh pmu status --update")
	row("")
	fmt.Fprintf(w, "└─%s─┘\n", border)
}

// writeCriticalAlert writes a boxed warning to the given writer for critical field changes.
func writeCriticalAlert(w io.Writer, changes []criticalFieldChange) {
	const width = 63
	border := strings.Repeat("─", width)

	fmt.Fprintf(w, "\n┌─%s─┐\n", border)
	fmt.Fprintf(w, "│  ⚠ CRITICAL CONFIG CHANGE DETECTED%s│\n", strings.Repeat(" ", width-35))
	fmt.Fprintf(w, "├─%s─┤\n", border)
	fmt.Fprintf(w, "│%s│\n", strings.Repeat(" ", width+2))
	fmt.Fprintf(w, "│  The following identity fields have changed from HEAD:%s│\n", strings.Repeat(" ", width-55))
	fmt.Fprintf(w, "│%s│\n", strings.Repeat(" ", width+2))

	for _, c := range changes {
		line := fmt.Sprintf("    %s:  %s  →  %s", c.Field, c.OldValue, c.NewValue)
		padding := width + 2 - len([]rune(line))
		if padding < 1 {
			padding = 1
		}
		fmt.Fprintf(w, "│%s%s│\n", line, strings.Repeat(" ", padding))
	}

	fmt.Fprintf(w, "│%s│\n", strings.Repeat(" ", width+2))
	fmt.Fprintf(w, "│  All gh pmu commands will now target the NEW values.%s│\n", strings.Repeat(" ", width-53))
	fmt.Fprintf(w, "│  If this is unintentional, restore with:%s│\n", strings.Repeat(" ", width-41))
	fmt.Fprintf(w, "│    git checkout -- .gh-pmu.json%s│\n", strings.Repeat(" ", width-31))
	fmt.Fprintf(w, "│%s│\n", strings.Repeat(" ", width+2))
	fmt.Fprintf(w, "└─%s─┘\n", border)
}

// gitShowFile runs git show to read a file from a given ref.
func gitShowFile(dir, ref string) ([]byte, error) {
	cmd := exec.Command("git", "show", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git show %s: %w", ref, err)
	}
	return out, nil
}

// configPathspec builds a git pathspec (e.g. "HEAD:./.gh-pmu.json") that resolves
// the config file relative to the invocation directory (gitShowFile's cmd.Dir),
// NOT the repository root. git resolves "<rev>:<path>" from the repo root unless
// the path is prefixed with "./", which silently compares against the wrong file
// (or none) when the config lives in a monorepo subdirectory.
func configPathspec(ref, name string) string {
	return ref + ":./" + name
}

// isStrictMode checks if the config has configIntegrity set to "strict".
func isStrictMode(content []byte) bool {
	var raw map[string]interface{}
	if err := json.Unmarshal(content, &raw); err != nil {
		return false
	}
	val, ok := raw["configIntegrity"]
	if !ok {
		return false
	}
	s, ok := val.(string)
	return ok && strings.EqualFold(s, "strict")
}

// isStrictModeEither reports strict mode when EITHER the committed (HEAD) config or
// the local config declares configIntegrity: "strict". Deciding strict mode from
// the local file alone is self-referential — an edit to .gh-pmu.json can remove the
// key and disable its own enforcement. Consulting HEAD makes strict mode monotonic:
// it cannot be turned off by editing the working copy while HEAD still declares it.
// committedContent may be nil (no HEAD), in which case it falls back to local only.
func isStrictModeEither(localContent, committedContent []byte) bool {
	return isStrictMode(localContent) || isStrictMode(committedContent)
}
