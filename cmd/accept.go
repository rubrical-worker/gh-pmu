package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/rubrical-works/gh-pmu/internal/defaults"
	"github.com/rubrical-works/gh-pmu/internal/integrity"
	"github.com/spf13/cobra"
)

type acceptOptions struct {
	yes bool
	dir string
}

func newAcceptCommand() *cobra.Command {
	opts := &acceptOptions{}

	cmd := &cobra.Command{
		Use:   "accept",
		Short: "Accept terms and conditions",
		Long: `Accept the Praxis Management Utility (gh-pmu) terms and conditions for this repository.

Terms must be accepted before using gh-pmu commands. Acceptance is
recorded in .gh-pmu.json and covers all repository collaborators.

The --yes flag is required to confirm acceptance.`,
		Example: `  # Accept the terms for this repository
  gh pmu accept --yes

  # Accept for a config in another directory
  gh pmu accept --yes --dir ../other-repo`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccept(cmd, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Accept terms without interactive prompt")
	cmd.Flags().StringVar(&opts.dir, "dir", "", "Directory to search for config (default: current directory)")

	return cmd
}

func runAccept(cmd *cobra.Command, opts *acceptOptions) error {
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
		return fmt.Errorf("no .gh-pmu.json found: %w", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	out := cmd.OutOrStdout()

	// Display terms
	fmt.Fprintln(out)
	fmt.Fprintln(out, defaults.Terms())

	// Shared acceptance notice
	fmt.Fprintln(out, "NOTE: By accepting, you accept on behalf of all users and")
	fmt.Fprintln(out, "collaborators of this repository's gh-pmu configuration.")
	fmt.Fprintln(out)

	if !opts.yes {
		return fmt.Errorf("--yes flag is required to accept terms")
	}

	// Record acceptance
	user := detectGitUser()
	cfg.Acceptance = &config.Acceptance{
		Accepted: true,
		User:     user,
		Date:     time.Now().Format("2006-01-02"),
		Version:  getVersion(),
	}

	if err := cfg.Save(filepath.Dir(configPath)); err != nil {
		return fmt.Errorf("failed to save acceptance: %w", err)
	}

	// Update checksum after saving config
	if err := integrity.UpdateChecksumForConfig(configPath); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not update checksum: %v\n", err)
	}

	fmt.Fprintf(out, "Terms accepted by %s (v%s).\n", user, getVersion())
	return nil
}

// detectGitUser returns the git user name, or "unknown" if not configured.
func detectGitUser() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return "unknown"
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "unknown"
	}
	return name
}
