package cmd

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/config"
	"github.com/rubrical-works/gh-pmu/internal/integrity"
)

func TestConfigVerify_NoConfig_ReturnsError(t *testing.T) {
	// ARRANGE: Empty dir with no config
	dir := t.TempDir()

	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "verify", "--dir", dir})

	// ACT
	err := cmd.Execute()

	// ASSERT
	if err == nil {
		t.Fatal("Expected error when no config file exists")
	}
}

func TestConfigVerify_CleanConfig_ReportsNoIssues(t *testing.T) {
	// ARRANGE: Create a temp dir with a config and init a git repo
	dir := t.TempDir()
	configContent := []byte(`{"project":{"owner":"test","number":1},"repositories":["test/repo"]}`)
	configPath := filepath.Join(dir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, configContent, 0644); err != nil {
		t.Fatal(err)
	}

	// Init git repo and commit config
	runGit(t, dir, "init")
	runGit(t, dir, "add", ".gh-pmu.json")
	runGit(t, dir, "commit", "-m", "init")

	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "verify", "--dir", dir})

	// ACT
	err := cmd.Execute()

	// ASSERT
	if err != nil {
		t.Fatalf("Expected no error, got: %v\nOutput: %s", err, buf.String())
	}
	output := buf.String()
	if !containsStr(output, "No drift detected") {
		t.Errorf("Expected 'No drift detected' in output, got: %s", output)
	}
}

func TestConfigVerify_DriftedConfig_ReportsChanges(t *testing.T) {
	// ARRANGE: Create config, commit, then modify
	dir := t.TempDir()
	original := []byte(`{"project":{"owner":"original","number":1},"repositories":["test/repo"]}`)
	configPath := filepath.Join(dir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, original, 0644); err != nil {
		t.Fatal(err)
	}

	runGit(t, dir, "init")
	runGit(t, dir, "add", ".gh-pmu.json")
	runGit(t, dir, "commit", "-m", "init")

	// Modify config locally
	modified := []byte(`{"project":{"owner":"changed","number":1},"repositories":["test/repo"]}`)
	if err := os.WriteFile(configPath, modified, 0644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "verify", "--dir", dir})

	// ACT
	err := cmd.Execute()

	// ASSERT: Should report drift (not error — verify reports findings)
	if err != nil {
		t.Fatalf("Expected no error (drift is reported, not errored), got: %v", err)
	}
	output := buf.String()
	if !containsStr(output, "Drift detected") {
		t.Errorf("Expected 'Drift detected' in output, got: %s", output)
	}
	if !containsStr(output, "project.owner") {
		t.Errorf("Expected change detail mentioning 'project.owner', got: %s", output)
	}
	// Verify unchanged sections are shown
	if !containsStr(output, "Unchanged:") {
		t.Errorf("Expected 'Unchanged:' section in drift report, got: %s", output)
	}
	if !containsStr(output, "repositories") {
		t.Errorf("Expected 'repositories' in unchanged list, got: %s", output)
	}
	// Verify changed/unchanged visual distinction
	if !containsStr(output, "Changed:") {
		t.Errorf("Expected 'Changed:' header in drift report, got: %s", output)
	}
}

func TestConfigVerify_StrictMode_ErrorsOnDrift(t *testing.T) {
	// ARRANGE: Config with strict integrity setting, drifted
	dir := t.TempDir()
	original := []byte(`{"project":{"owner":"original","number":1},"repositories":["test/repo"],"configIntegrity":"strict"}`)
	configPath := filepath.Join(dir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, original, 0644); err != nil {
		t.Fatal(err)
	}

	runGit(t, dir, "init")
	runGit(t, dir, "add", ".gh-pmu.json")
	runGit(t, dir, "commit", "-m", "init")

	// Modify config
	modified := []byte(`{"project":{"owner":"changed","number":1},"repositories":["test/repo"],"configIntegrity":"strict"}`)
	if err := os.WriteFile(configPath, modified, 0644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "verify", "--dir", dir})

	// ACT
	err := cmd.Execute()

	// ASSERT: Strict mode returns error on drift
	if err == nil {
		t.Fatal("Expected error in strict mode with drift")
	}
}

// runVerifyOnCommittedConfig commits content as .gh-pmu.json in a fresh repo and
// runs `config verify`, returning stdout, stderr and the error.
func runVerifyOnCommittedConfig(t *testing.T, content string) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gh-pmu.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init")
	runGit(t, dir, "add", ".gh-pmu.json")
	runGit(t, dir, "commit", "-m", "init")

	cmd := NewRootCommand()
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"config", "verify", "--dir", dir})
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// TestConfigVerify_MissingStatusValues_Alert (#917): a config whose Status
// aliases lack required values gets a boxed stderr alert naming each one.
func TestConfigVerify_MissingStatusValues_Alert(t *testing.T) {
	content := `{"project":{"owner":"test","number":1},"repositories":["test/repo"],
		"fields":{"status":{"field":"Status","values":{"backlog":"Backlog","ready":"Ready","in_progress":"In progress","in_review":"In review","done":"Done","parking_lot":"Parking Lot","notes":"Notes"}}}}`
	stdout, stderr, err := runVerifyOnCommittedConfig(t, content)
	if err != nil {
		t.Fatalf("the alert is advisory; expected no error, got: %v", err)
	}
	if !strings.Contains(stderr, "┌") || !strings.Contains(stderr, "REQUIRED STATUS VALUES MISSING") {
		t.Errorf("expected a boxed alert on stderr, got: %s", stderr)
	}
	for _, want := range []string{"Up next", "QA required", "gh pmu status --update"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("alert should mention %q, got: %s", want, stderr)
		}
	}
	for _, present := range []string{"Backlog", "Parking Lot"} {
		if strings.Contains(stderr, present) {
			t.Errorf("alert must name only missing values, but mentions %q: %s", present, stderr)
		}
	}
	if strings.Contains(stdout, "REQUIRED STATUS VALUES MISSING") {
		t.Error("the alert belongs on stderr, not stdout")
	}
}

func TestConfigVerify_AllStatusValuesPresentCaseInsensitive_NoAlert(t *testing.T) {
	content := `{"project":{"owner":"test","number":1},"repositories":["test/repo"],
		"fields":{"status":{"field":"Status","values":{"backlog":"Backlog","up_next":"Up Next","ready":"Ready","in_progress":"In progress","in_review":"In review","qa_required":"QA Required","done":"Done","parking_lot":"Parking Lot","notes":"Notes"}}}}`
	_, stderr, err := runVerifyOnCommittedConfig(t, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(stderr, "REQUIRED STATUS VALUES MISSING") {
		t.Errorf("values differing only in case count as present; got alert: %s", stderr)
	}
}

// TestConfigVerify_MissingStatusValues_StrictModeExitUnchanged: strict mode fails
// on drift, never on the Status alert.
func TestConfigVerify_MissingStatusValues_StrictModeExitUnchanged(t *testing.T) {
	content := `{"project":{"owner":"test","number":1},"repositories":["test/repo"],"configIntegrity":"strict",
		"fields":{"status":{"field":"Status","values":{"backlog":"Backlog"}}}}`
	_, stderr, err := runVerifyOnCommittedConfig(t, content)
	if err != nil {
		t.Errorf("missing Status values must not fail verify, even in strict mode; got: %v", err)
	}
	if !strings.Contains(stderr, "REQUIRED STATUS VALUES MISSING") {
		t.Errorf("expected the alert to still be shown in strict mode, got: %s", stderr)
	}
}

// runGit is a test helper to run git commands in a directory.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func containsStr(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}

// ============================================================================
// project.view resolve-and-persist Tests (#901)
// ============================================================================

// stubViewResolver stands in for the API client so these tests need neither
// network nor auth.
type stubViewResolver struct {
	number int
	found  bool
	err    error
	calls  int
}

func (s *stubViewResolver) ResolveBacklogViewNumber(owner string, number int) (int, bool, error) {
	s.calls++
	return s.number, s.found, s.err
}

// writeViewTestConfig writes a config and returns its path.
func writeViewTestConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, ".gh-pmu.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveAndPersistView_ResolvesWhenAbsent(t *testing.T) {
	// ARRANGE
	dir := t.TempDir()
	path := writeViewTestConfig(t, dir, `{"project":{"owner":"o","number":11},"repositories":["o/r"]}`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &stubViewResolver{number: 2, found: true}
	buf := new(bytes.Buffer)

	// ACT
	if err := resolveAndPersistView(cfg, path, resolver, buf, false); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// ASSERT: written to disk, not just to the in-memory struct
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Project.View != 2 {
		t.Errorf("Expected project.view 2 persisted, got %d", reloaded.Project.View)
	}
	if resolver.calls != 1 {
		t.Errorf("Expected exactly 1 resolve call, got %d", resolver.calls)
	}
}

func TestResolveAndPersistView_UpdatesChecksumAfterWrite(t *testing.T) {
	// ARRANGE
	dir := t.TempDir()
	path := writeViewTestConfig(t, dir, `{"project":{"owner":"o","number":11},"repositories":["o/r"]}`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	// ACT
	if err := resolveAndPersistView(cfg, path, &stubViewResolver{number: 2, found: true}, new(bytes.Buffer), false); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// ASSERT: the stored checksum describes the file as it now stands
	stored, err := integrity.LoadChecksum(dir)
	if err != nil {
		t.Fatal(err)
	}
	current, err := integrity.ComputeChecksum(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored != current {
		t.Errorf("Expected the stored checksum to match the written file\n stored: %s\ncurrent: %s", stored, current)
	}
}

func TestResolveAndPersistView_HandEditedValueIsAuthoritative(t *testing.T) {
	// A value already in the config is the user's, not ours. A plain run must
	// neither call the API nor overwrite it.
	dir := t.TempDir()
	path := writeViewTestConfig(t, dir, `{"project":{"owner":"o","number":11,"view":7},"repositories":["o/r"]}`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &stubViewResolver{number: 2, found: true}

	// ACT
	if err := resolveAndPersistView(cfg, path, resolver, new(bytes.Buffer), false); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// ASSERT
	if resolver.calls != 0 {
		t.Errorf("Expected no API call when view is already set, got %d", resolver.calls)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Project.View != 7 {
		t.Errorf("Expected the hand-edited view 7 to survive, got %d", reloaded.Project.View)
	}
}

func TestResolveAndPersistView_ForceReResolvesExistingValue(t *testing.T) {
	// The explicit opt-in is the one path that may replace an existing value.
	dir := t.TempDir()
	path := writeViewTestConfig(t, dir, `{"project":{"owner":"o","number":11,"view":7},"repositories":["o/r"]}`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &stubViewResolver{number: 2, found: true}

	// ACT
	if err := resolveAndPersistView(cfg, path, resolver, new(bytes.Buffer), true); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// ASSERT
	if resolver.calls != 1 {
		t.Errorf("Expected 1 resolve call when forced, got %d", resolver.calls)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Project.View != 2 {
		t.Errorf("Expected the forced re-resolve to write 2, got %d", reloaded.Project.View)
	}
}

func TestResolveAndPersistView_OfflineWarnsAndContinues(t *testing.T) {
	// A resolve failure must not turn config verify into a command that
	// requires network and auth. Warn, leave view unset, carry on.
	dir := t.TempDir()
	path := writeViewTestConfig(t, dir, `{"project":{"owner":"o","number":11},"repositories":["o/r"]}`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	buf := new(bytes.Buffer)

	// ACT
	err = resolveAndPersistView(cfg, path, &stubViewResolver{err: errors.New("dial tcp: no such host")}, buf, false)

	// ASSERT
	if err != nil {
		t.Fatalf("Expected a resolve failure to be non-fatal, got: %v", err)
	}
	if !strings.Contains(buf.String(), "could not resolve") {
		t.Errorf("Expected a warning on the writer, got: %q", buf.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("Expected the config to be left untouched on resolve failure\nbefore: %s\n after: %s", before, after)
	}
}

func TestResolveAndPersistView_NoBacklogViewIsReportedNotWritten(t *testing.T) {
	// There is no createProjectV2View mutation, so this is reported and never
	// repaired — and never defaulted to 1.
	dir := t.TempDir()
	path := writeViewTestConfig(t, dir, `{"project":{"owner":"o","number":11},"repositories":["o/r"]}`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	buf := new(bytes.Buffer)

	// ACT
	if err := resolveAndPersistView(cfg, path, &stubViewResolver{found: false}, buf, false); err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// ASSERT
	if !strings.Contains(buf.String(), "no Backlog") {
		t.Errorf("Expected the missing view to be reported, got: %q", buf.String())
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Project.View != 0 {
		t.Errorf("Expected view to stay unset, got %d — must not default to 1", reloaded.Project.View)
	}
}

func TestResolveAndPersistView_SaveFailureIsFailLoud(t *testing.T) {
	// ARRANGE: a config directory that does not exist, so Save cannot write
	dir := t.TempDir()
	path := writeViewTestConfig(t, dir, `{"project":{"owner":"o","number":11},"repositories":["o/r"]}`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "gone", ".gh-pmu.json")

	// ACT
	err = resolveAndPersistView(cfg, missing, &stubViewResolver{number: 2, found: true}, new(bytes.Buffer), false)

	// ASSERT: reported, not swallowed
	if err == nil {
		t.Fatal("Expected a save failure to be reported")
	}
	// And no checksum was recorded for a file that was never written.
	if stored, _ := integrity.LoadChecksum(filepath.Join(dir, "gone")); stored != "" {
		t.Errorf("Expected no checksum after a failed save, got %q", stored)
	}
}

// Plain `config verify` must not resolve or write. Its name promises a
// read-only check, and resolution would make a command that needs neither
// network nor auth depend on both (#901).
func TestConfigVerify_WithoutResolveViewFlag_DoesNotWrite(t *testing.T) {
	// ARRANGE
	dir := t.TempDir()
	original := `{"project":{"owner":"o","number":11},"repositories":["o/r"]}`
	configPath := writeViewTestConfig(t, dir, original)

	runGit(t, dir, "init")
	runGit(t, dir, "add", ".gh-pmu.json")
	runGit(t, dir, "commit", "-m", "init")

	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "verify", "--dir", dir})

	// ACT
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// ASSERT: byte-identical, and no view key appeared
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Errorf("Expected verify to leave the config untouched\nbefore: %s\n after: %s", original, after)
	}
	if strings.Contains(buf.String(), "Resolved project.view") {
		t.Errorf("Expected no resolution without the flag, got: %s", buf.String())
	}
}

func TestConfigVerify_CriticalFieldChange_SingleField(t *testing.T) {
	dir := t.TempDir()
	original := []byte(`{"project":{"owner":"test","number":1},"repositories":["test/repo"]}`)
	configPath := filepath.Join(dir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, original, 0644); err != nil {
		t.Fatal(err)
	}

	runGit(t, dir, "init")
	runGit(t, dir, "add", ".gh-pmu.json")
	runGit(t, dir, "commit", "-m", "init")

	// Change only project.number
	modified := []byte(`{"project":{"owner":"test","number":42},"repositories":["test/repo"]}`)
	if err := os.WriteFile(configPath, modified, 0644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"config", "verify", "--dir", dir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Alert should be on stderr
	errOutput := stderr.String()
	if !containsStr(errOutput, "CRITICAL CONFIG CHANGE DETECTED") {
		t.Errorf("Expected critical alert on stderr, got: %s", errOutput)
	}
	if !containsStr(errOutput, "project.number") {
		t.Errorf("Expected 'project.number' in alert, got: %s", errOutput)
	}
	if !containsStr(errOutput, "1") || !containsStr(errOutput, "42") {
		t.Errorf("Expected old (1) and new (42) values in alert, got: %s", errOutput)
	}
	// Should NOT mention owner or repositories since those didn't change
	if containsStr(errOutput, "project.owner") {
		t.Errorf("Should not mention project.owner when it didn't change")
	}

	// Stdout should still have the normal drift report
	if !containsStr(stdout.String(), "Drift detected") {
		t.Errorf("Expected drift report on stdout")
	}
}

func TestConfigVerify_CriticalFieldChange_MultipleFields(t *testing.T) {
	dir := t.TempDir()
	original := []byte(`{"project":{"owner":"old-owner","number":1},"repositories":["old/repo"]}`)
	configPath := filepath.Join(dir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, original, 0644); err != nil {
		t.Fatal(err)
	}

	runGit(t, dir, "init")
	runGit(t, dir, "add", ".gh-pmu.json")
	runGit(t, dir, "commit", "-m", "init")

	modified := []byte(`{"project":{"owner":"new-owner","number":99},"repositories":["new/repo"]}`)
	if err := os.WriteFile(configPath, modified, 0644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"config", "verify", "--dir", dir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	errOutput := stderr.String()
	if !containsStr(errOutput, "project.owner") {
		t.Errorf("Expected 'project.owner' in alert")
	}
	if !containsStr(errOutput, "project.number") {
		t.Errorf("Expected 'project.number' in alert")
	}
	if !containsStr(errOutput, "repositories[0]") {
		t.Errorf("Expected 'repositories[0]' in alert")
	}
}

func TestConfigVerify_NoCriticalChange_WithGeneralDrift(t *testing.T) {
	dir := t.TempDir()
	original := []byte(`{"project":{"owner":"test","number":1},"repositories":["test/repo"],"defaults":{"status":"backlog"}}`)
	configPath := filepath.Join(dir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, original, 0644); err != nil {
		t.Fatal(err)
	}

	runGit(t, dir, "init")
	runGit(t, dir, "add", ".gh-pmu.json")
	runGit(t, dir, "commit", "-m", "init")

	// Only change defaults (non-critical)
	modified := []byte(`{"project":{"owner":"test","number":1},"repositories":["test/repo"],"defaults":{"status":"ready"}}`)
	if err := os.WriteFile(configPath, modified, 0644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"config", "verify", "--dir", dir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should report general drift on stdout
	if !containsStr(stdout.String(), "Drift detected") {
		t.Errorf("Expected drift report on stdout")
	}

	// Should NOT have critical alert on stderr
	if containsStr(stderr.String(), "CRITICAL CONFIG CHANGE DETECTED") {
		t.Errorf("Should not show critical alert when only non-critical fields changed")
	}
}

func TestConfigVerify_StrictMode_CriticalDrift_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	original := []byte(`{"project":{"owner":"test","number":1},"repositories":["test/repo"],"configIntegrity":"strict"}`)
	configPath := filepath.Join(dir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, original, 0644); err != nil {
		t.Fatal(err)
	}

	runGit(t, dir, "init")
	runGit(t, dir, "add", ".gh-pmu.json")
	runGit(t, dir, "commit", "-m", "init")

	modified := []byte(`{"project":{"owner":"changed","number":1},"repositories":["test/repo"],"configIntegrity":"strict"}`)
	if err := os.WriteFile(configPath, modified, 0644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"config", "verify", "--dir", dir})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error in strict mode with critical drift")
	}

	// Should also have the critical alert on stderr
	if !containsStr(stderr.String(), "CRITICAL CONFIG CHANGE DETECTED") {
		t.Errorf("Expected critical alert on stderr in strict mode")
	}
}

// TestConfigVerify_Remote_CriticalAlert_CapturedOnStderr asserts that when
// --remote surfaces a critical field change vs origin/main, the alert is
// written to cobra's cmd.ErrOrStderr() rather than os.Stderr directly. Prior
// to #834, the remote path wrote to os.Stderr and the alert leaked out of
// tests that captured stderr via SetErr.
func TestConfigVerify_Remote_CriticalAlert_CapturedOnStderr(t *testing.T) {
	// Set up an upstream bare repo + a local clone so origin/main exists.
	remote := t.TempDir()
	runGit(t, remote, "init", "--bare")

	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "remote", "add", "origin", remote)

	// Initial config committed to local and pushed to origin/main.
	original := []byte(`{"project":{"owner":"upstream-owner","number":1},"repositories":["test/repo"]}`)
	configPath := filepath.Join(dir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, original, 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".gh-pmu.json")
	runGit(t, dir, "commit", "-m", "init")
	runGit(t, dir, "branch", "-M", "main")
	runGit(t, dir, "push", "-u", "origin", "main")

	// Change project.owner locally AND commit to HEAD, so HEAD and
	// origin/main match but both disagree with the new local file... scratch
	// that — to isolate the *remote* critical-alert path, we want the local
	// file to match HEAD but differ from origin/main. Do that by amending
	// HEAD without pushing.
	modified := []byte(`{"project":{"owner":"new-owner","number":1},"repositories":["test/repo"]}`)
	if err := os.WriteFile(configPath, modified, 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".gh-pmu.json")
	runGit(t, dir, "commit", "-m", "local change")

	cmd := NewRootCommand()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"config", "verify", "--dir", dir, "--remote"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	errOutput := stderr.String()
	if !containsStr(errOutput, "CRITICAL CONFIG CHANGE DETECTED") {
		t.Errorf("Expected remote critical alert on captured stderr, got: %q", errOutput)
	}
	if !containsStr(errOutput, "project.owner") {
		t.Errorf("Expected 'project.owner' in remote alert on stderr, got: %q", errOutput)
	}
}
