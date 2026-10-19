package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/config"
)

func TestRefreshConfigVersionInDir_StaleVersion_Rewrites(t *testing.T) {
	dir := t.TempDir()
	cfg := baseConfig()
	cfg.Version = "1.1.0"
	path := writeTestConfig(t, dir, cfg)

	var buf bytes.Buffer
	refreshConfigVersionInDir(dir, "1.5.3", &buf)

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Failed to reload config: %v", err)
	}
	if reloaded.Version != "1.5.3" {
		t.Errorf("Expected version rewritten to '1.5.3', got '%s'", reloaded.Version)
	}
	if buf.Len() != 0 {
		t.Errorf("Expected no warning on the success path, got: %s", buf.String())
	}
}

func TestRefreshConfigVersionInDir_NoConfig_SilentBail(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	refreshConfigVersionInDir(dir, "1.5.3", &buf)

	// An uninitialized repo is the normal case for a fresh checkout: no warning,
	// no file created, no error surfaced to the command.
	if buf.Len() != 0 {
		t.Errorf("Expected silence when no config exists, got: %s", buf.String())
	}
	if _, err := os.Stat(filepath.Join(dir, config.ConfigFileName)); !os.IsNotExist(err) {
		t.Error("Expected no config file to be created")
	}
}

func TestRefreshConfigVersionInDir_CorruptConfig_WarnsWithoutAborting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, config.ConfigFileName)
	if err := os.WriteFile(path, []byte("{not valid json"), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	var buf bytes.Buffer
	refreshConfigVersionInDir(dir, "1.5.3", &buf)

	if !strings.Contains(buf.String(), "Warning") {
		t.Errorf("Expected a warning for an unreadable config, got: %q", buf.String())
	}
}

func TestRefreshConfigVersionInDir_CurrentVersion_LeavesBytesUnchanged(t *testing.T) {
	dir := t.TempDir()
	cfg := baseConfig()
	cfg.Version = "1.5.3"
	path := writeTestConfig(t, dir, cfg)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	var buf bytes.Buffer
	refreshConfigVersionInDir(dir, "1.5.3", &buf)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to re-read config: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("Expected config bytes unchanged when the version already matches")
	}
}

func TestRefreshConfigVersionInDir_RepoRootProtected_DoesNotWrite(t *testing.T) {
	// go test ./cmd/ runs with the package directory as cwd, and FindConfigFile
	// walks up to the repository's own .gh-pmu.json. Without this guard every
	// command test that goes through PersistentPreRunE rewrites the real config
	// — the #436 class of defect, and the reason writeConfig carries the same
	// check.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0644); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}
	cfg := baseConfig()
	cfg.Version = "1.1.0"
	path := writeTestConfig(t, dir, cfg)

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	setRepoRootProtectionForTest(t, true)

	var buf bytes.Buffer
	refreshConfigVersionInDir(dir, "1.5.3", &buf)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to re-read config: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("Expected no write to a protected repo root")
	}
}

func TestRefreshConfigVersionInDir_RepoRootUnprotected_StillWrites(t *testing.T) {
	// The guard is scoped to test protection; real users running gh pmu at a
	// repository root must still get their version stamped.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0644); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}
	cfg := baseConfig()
	cfg.Version = "1.1.0"
	path := writeTestConfig(t, dir, cfg)

	setRepoRootProtectionForTest(t, false)

	var buf bytes.Buffer
	refreshConfigVersionInDir(dir, "1.5.3", &buf)

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Failed to reload config: %v", err)
	}
	if reloaded.Version != "1.5.3" {
		t.Errorf("Expected version rewritten to '1.5.3', got '%s'", reloaded.Version)
	}
}

func TestShouldRefreshVersion_HonorsExemptCommands(t *testing.T) {
	// checkAcceptance and runDailyIntegrityCheck both skip these; the version
	// refresh must agree or 'gh pmu help' would write the config.
	for name := range exemptCommands {
		if shouldRefreshVersion(name) {
			t.Errorf("Expected exempt command %q to skip the version refresh", name)
		}
	}

	for _, name := range []string{"list", "move", "board", "config"} {
		if !shouldRefreshVersion(name) {
			t.Errorf("Expected non-exempt command %q to run the version refresh", name)
		}
	}
}

func TestRootCommandHelp(t *testing.T) {
	// Test that root command executes and shows help
	cmd := NewRootCommand()

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Fatal("Expected help output, got empty string")
	}

	// Verify it contains expected content
	if !bytes.Contains([]byte(output), []byte("gh pmu")) {
		t.Errorf("Expected output to contain 'gh pmu', got: %s", output)
	}
}

func TestRootCommandVersion(t *testing.T) {
	cmd := NewRootCommand()

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--version"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	// Cobra uses first word of Use field for version output
	if !bytes.Contains([]byte(output), []byte("version")) {
		t.Errorf("Expected version output to contain 'version', got: %s", output)
	}
}

func TestRootCommandVersionContainsCopyright(t *testing.T) {
	cmd := NewRootCommand()

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--version"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Rubrical Works") {
		t.Errorf("Expected version output to contain 'Rubrical Works', got: %s", output)
	}
	if !strings.Contains(output, "(c)") {
		t.Errorf("Expected version output to contain '(c)' copyright marker, got: %s", output)
	}
}

func TestRootCommandVersionFormat(t *testing.T) {
	cmd := NewRootCommand()

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--version"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines in version output, got %d: %q", len(lines), output)
	}
	if !strings.HasPrefix(lines[0], "gh pmu version ") {
		t.Errorf("Expected first line to start with 'gh pmu version ', got: %q", lines[0])
	}
	if lines[1] != "Rubrical Works (c) 2026" {
		t.Errorf("Expected second line to be 'Rubrical Works (c) 2026', got: %q", lines[1])
	}
}

// TestSubcommandUsageLinePrefix verifies that `gh pmu <cmd> --help` renders
// `Usage: gh pmu <cmd>...` rather than the buggy `Usage: gh <cmd>...`.
func TestSubcommandUsageLinePrefix(t *testing.T) {
	subcommands := []string{"board", "view", "move", "create"}

	for _, sub := range subcommands {
		t.Run(sub, func(t *testing.T) {
			cmd := NewRootCommand()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetArgs([]string{sub, "--help"})

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Expected no error running %q --help, got: %v", sub, err)
			}

			output := buf.String()
			want := "Usage:\n  gh pmu " + sub
			if !strings.Contains(output, want) {
				t.Errorf("Expected output to contain %q for subcommand %q.\nFull output:\n%s", want, sub, output)
			}
			bad := "Usage:\n  gh " + sub
			if strings.Contains(output, bad) {
				t.Errorf("Output still contains buggy usage %q for subcommand %q.\nFull output:\n%s", bad, sub, output)
			}
		})
	}
}

// executeForErrorOutput runs the root command with args and returns everything
// it wrote (stdout and stderr share one buffer) plus the returned error.
func executeForErrorOutput(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// chdirNoConfig moves into an empty temp dir so a command fails on config load
// inside RunE — a runtime error reached without any API call. It skips if a
// .gh-pmu.json above the temp dir would let the command reach the network.
func chdirNoConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if _, err := config.FindConfigFile(dir); err == nil {
		t.Skip("a .gh-pmu.json above the temp dir would let this command reach the API")
	}
	chdirTemp(t, dir)
}

// TestArgumentError_NoExampleShowsUseLineOnly: a subcommand without an
// Example field still gets the usage block on an argument error, carrying its
// Use line and no Examples section (#927).
func TestArgumentError_NoExampleShowsUseLineOnly(t *testing.T) {
	root := NewRootCommand()
	addCmd, _, err := root.Find([]string{"branch", "add"})
	if err != nil {
		t.Fatalf("branch add command not found: %v", err)
	}
	if addCmd.Example != "" {
		t.Fatal("precondition: branch add must have no Example field")
	}

	out, err := executeForErrorOutput(t, "branch", "add")
	if err == nil {
		t.Fatal("Expected an argument error for branch add with no issue number")
	}
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "gh pmu branch add <issue-number>") {
		t.Errorf("Expected the usage block with the Use line, got:\n%s", out)
	}
	if strings.Contains(out, "Examples:") {
		t.Errorf("Expected no Examples section for a subcommand without Example, got:\n%s", out)
	}
}

// TestSubcommandExamples: the most-used commands carry an Example field, so
// --help shows an Examples section and an argument error can show one (#928).
func TestSubcommandExamples(t *testing.T) {
	tests := []struct {
		path []string
		want []string // substrings the Example must contain
	}{
		{[]string{"create"}, []string{"gh pmu create --title", "-F "}},
		{[]string{"view"}, []string{"gh pmu view 42 --json="}},
		{[]string{"list"}, []string{"gh pmu list"}},
		{[]string{"accept"}, []string{"gh pmu accept --yes"}},
		{[]string{"branch", "current"}, []string{"gh pmu branch current"}},
	}
	for _, tt := range tests {
		name := strings.Join(tt.path, " ")
		t.Run(name, func(t *testing.T) {
			sub, _, err := NewRootCommand().Find(tt.path)
			if err != nil {
				t.Fatalf("%s command not found: %v", name, err)
			}
			if strings.TrimSpace(sub.Example) == "" {
				t.Fatalf("Expected %s to define a non-empty Example field", name)
			}
			for _, w := range tt.want {
				if !strings.Contains(sub.Example, w) {
					t.Errorf("Expected %s Example to contain %q, got:\n%s", name, w, sub.Example)
				}
			}

			out, _ := executeForErrorOutput(t, append(tt.path, "--help")...)
			if !strings.Contains(out, "Examples:") {
				t.Errorf("Expected %s --help to show an Examples section, got:\n%s", name, out)
			}
		})
	}
}

// TestCreateExample_NonInteractiveUsesBodyFile: create's -F example sets every
// field a non-interactive run needs, so it can be copied as-is (#928).
func TestCreateExample_NonInteractiveUsesBodyFile(t *testing.T) {
	createCmd, _, _ := NewRootCommand().Find([]string{"create"})
	for _, line := range strings.Split(createCmd.Example, "\n") {
		if strings.Contains(line, "gh pmu create") && strings.Contains(line, "-F ") {
			for _, flag := range []string{"--title", "--status", "--priority"} {
				if !strings.Contains(line, flag) {
					t.Errorf("Expected the -F example to set %s, got: %s", flag, line)
				}
			}
			return
		}
	}
	t.Fatalf("Expected a create example using -F, got:\n%s", createCmd.Example)
}

// TestRootCommandUsageLinePrefix verifies that `gh pmu --help` renders
// `Usage: gh pmu [command]`.
func TestRootCommandUsageLinePrefix(t *testing.T) {
	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "gh pmu [command]") {
		t.Errorf("Expected root usage to contain 'gh pmu [command]', got:\n%s", output)
	}
}
