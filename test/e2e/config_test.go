//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureConfig returns the .gh-pmu.json for a fixture's template project.
// The user fixture's aliases are the ones its tests were written against.
func fixtureConfig(f Fixture) string {
	return fmt.Sprintf(testConfigTemplate, f.ProjectName, f.TemplateProject, f.Owner, f.Repo)
}

// testConfigTemplate is filled in by fixtureConfig: project name, number,
// owner, repository.
const testConfigTemplate = `{
  "project": {
    "name": %q,
    "number": %d,
    "owner": %q
  },
  "framework": "IDPF-Agile",
  "repositories": [%q],
  "defaults": {
    "priority": "p2",
    "status": "backlog"
  },
  "fields": {
    "priority": {
      "field": "Priority",
      "values": {"p0": "P0", "p1": "P1", "p2": "P2"}
    },
    "status": {
      "field": "Status",
      "values": {"backlog": "Backlog", "done": "Done", "in_progress": "In progress", "in_review": "In review", "ready": "Ready"}
    },
    "release": {
      "field": "Release"
    }
  }
}`

// TestConfig holds the paths for a test configuration
type TestConfig struct {
	// Dir is the temporary directory containing the config
	Dir string
	// ConfigPath is the full path to the .gh-pmu.json file
	ConfigPath string
}

// setupTestConfig creates a temporary directory with a .gh-pmu.json file
// configured for the user fixture's template project, skipping the test when
// the user fixture is not selected (#940). The directory is automatically
// cleaned up when the test completes. Also initializes a git repository for
// tests that require git operations (like branch commands).
func setupTestConfig(t *testing.T) *TestConfig {
	t.Helper()
	f := requireFixture(t, userFixture)

	// Create temp directory - automatically cleaned up by t.TempDir()
	tmpDir := t.TempDir()

	// Write config file
	configPath := filepath.Join(tmpDir, ".gh-pmu.json")
	err := os.WriteFile(configPath, []byte(fixtureConfig(f)), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Initialize git repo for tests that require git operations
	initGitRepo(t, tmpDir)

	return &TestConfig{
		Dir:        tmpDir,
		ConfigPath: configPath,
	}
}

// TestConfigVerifyResolveView_PersistsViewNumber drives resolution against the
// real E2E project and asserts the number lands in .gh-pmu.json (#901).
//
// The unit tests prove the predicate, the paging and the error split against
// canned responses. What they cannot prove is that the query this package
// actually sends is accepted by the live API and that repositoryOwner ->
// ... on ProjectV2Owner really resolves for this owner — the vendored schema
// says it should, but the vendored schema is a copy.
func TestConfigVerifyResolveView_PersistsViewNumber(t *testing.T) {
	cfg := setupTestConfig(t)

	// Baseline: the fixture config carries no view key.
	before, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}
	if strings.Contains(string(before), `"view"`) {
		t.Fatalf("Fixture config already has a view key, nothing to resolve: %s", before)
	}

	// ACT
	result := runPMU(t, cfg.Dir, "config", "verify", "--resolve-view")
	assertExitCode(t, result, 0)

	// ASSERT: the resolved number is on disk and usable
	var parsed struct {
		Project struct {
			View int `json:"view"`
		} `json:"project"`
	}
	after, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		t.Fatalf("Failed to re-read config: %v", err)
	}
	if err := json.Unmarshal(after, &parsed); err != nil {
		t.Fatalf("Config is not valid JSON after resolution: %v\n%s", err, after)
	}

	// The board may legitimately have no Backlog view — that outcome is
	// reported rather than written, and is not a test failure. Distinguish the
	// two rather than asserting a number blindly.
	if strings.Contains(result.Stdout, "has no Backlog board view") {
		if parsed.Project.View != 0 {
			t.Errorf("Reported no Backlog view but still wrote view %d", parsed.Project.View)
		}
		t.Skip("E2E project has no Backlog board view — resolution reported it correctly")
	}

	if parsed.Project.View < 1 {
		t.Errorf("Expected a resolved view number >= 1, got %d\noutput: %s", parsed.Project.View, result.Stdout)
	}
	assertContains(t, result.Stdout, "Resolved project.view")
}

// A second run must not re-resolve: the value is now the config's, and only an
// explicit --resolve-view may replace it (which is what this flag is).
func TestConfigVerifyResolveView_SecondRunIsStable(t *testing.T) {
	cfg := setupTestConfig(t)

	first := runPMU(t, cfg.Dir, "config", "verify", "--resolve-view")
	assertExitCode(t, first, 0)
	if strings.Contains(first.Stdout, "has no Backlog board view") {
		t.Skip("E2E project has no Backlog board view — nothing to re-resolve")
	}
	afterFirst, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	second := runPMU(t, cfg.Dir, "config", "verify", "--resolve-view")
	assertExitCode(t, second, 0)
	afterSecond, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	if string(afterFirst) != string(afterSecond) {
		t.Errorf("Expected a re-resolve to be idempotent\nfirst:  %s\nsecond: %s", afterFirst, afterSecond)
	}
}

// Plain verify against a config carrying a resolved view must report no drift
// once that view is committed — and must not report drift for the view itself
// even when it is not (#901).
func TestConfigVerify_ResolvedViewDoesNotReportDrift(t *testing.T) {
	cfg := setupTestConfig(t)

	// setupTestConfig already commits the fixture, so HEAD is the baseline:
	// a config with no view key.

	resolve := runPMU(t, cfg.Dir, "config", "verify", "--resolve-view")
	assertExitCode(t, resolve, 0)
	if strings.Contains(resolve.Stdout, "has no Backlog board view") {
		t.Skip("E2E project has no Backlog board view — no write to check drift against")
	}

	// ACT: plain verify, with the resolved view now differing from HEAD
	result := runPMU(t, cfg.Dir, "config", "verify")

	// ASSERT: the resolved view is invisible to the drift check, and nothing
	// else drifts either. Save writes only keys the config actually sets since
	// the dead release block was removed (#902), so a resolved view is the only
	// difference from HEAD — and it is excluded from the comparison.
	assertExitCode(t, result, 0)
	assertNotContains(t, result.Stdout, "project.view")
	assertContains(t, result.Stdout, "No drift detected")
}

// initGitRepo initializes a git repository in the given directory
// with minimal configuration for testing.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	// Run git init
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Configure git user for commits (required for some git operations)
	cmd = exec.Command("git", "config", "user.email", "test@e2e.local")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to configure git email: %v", err)
	}

	cmd = exec.Command("git", "config", "user.name", "E2E Test")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to configure git name: %v", err)
	}

	// Create initial commit (some git operations require at least one commit)
	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# E2E Test Repository\n"), 0644); err != nil {
		t.Fatalf("Failed to create README: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to create initial commit: %v", err)
	}
}
