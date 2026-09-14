package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/config"
)

func writeTestConfig(t *testing.T, dir string, cfg *config.Config) string {
	t.Helper()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}
	configPath := filepath.Join(dir, ".gh-pmu.json")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	return configPath
}

func baseConfig() *config.Config {
	return &config.Config{
		Project: config.Project{
			Owner:  "test-owner",
			Number: 1,
		},
		Repositories: []string{"test-owner/test-repo"},
	}
}

func chdirTemp(t *testing.T, dir string) {
	t.Helper()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
}

// setTestVersion sets the package version for gate testing and restores it on cleanup.
func setTestVersion(t *testing.T, v string) {
	t.Helper()
	origVersion := version
	version = v
	t.Cleanup(func() { version = origVersion })
}

func TestAcceptanceGate_BlocksCommandWithoutAcceptance(t *testing.T) {
	// ARRANGE: Config without acceptance, real version to enable gate
	tmpDir := t.TempDir()
	writeTestConfig(t, tmpDir, baseConfig())
	chdirTemp(t, tmpDir)
	setTestVersion(t, "0.15.0")

	// ACT: Run a command that requires acceptance
	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"board"})
	err := cmd.Execute()

	// ASSERT: Should fail specifically with acceptance error
	if err == nil {
		t.Fatal("Expected error for missing acceptance, got nil")
	}
	if !containsAcceptanceError(err.Error()) {
		t.Errorf("Expected acceptance error, got: %v", err)
	}
}

func TestAcceptanceGate_AllowsAcceptedConfig(t *testing.T) {
	// ARRANGE: Config with valid acceptance, real version to enable gate
	tmpDir := t.TempDir()
	setTestVersion(t, "0.15.0")
	cfg := baseConfig()
	cfg.Acceptance = &config.Acceptance{
		Accepted: true,
		User:     "test-user",
		Date:     "2026-02-20",
		Version:  "0.15.0",
	}
	writeTestConfig(t, tmpDir, cfg)
	chdirTemp(t, tmpDir)

	// ACT: Run a command (board will fail for API reasons, but should pass gate)
	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"board"})
	err := cmd.Execute()

	// ASSERT: Error should NOT be about acceptance
	if err != nil && containsAcceptanceError(err.Error()) {
		t.Errorf("Expected to pass acceptance gate, got acceptance error: %v", err)
	}
}

// TestAcceptanceGate_MinorBumpAcceptancePassesGate (#919): acceptance recorded on
// an earlier minor version of the same major lets a real command through, with
// no terms text shown.
func TestAcceptanceGate_MinorBumpAcceptancePassesGate(t *testing.T) {
	tmpDir := t.TempDir()
	setTestVersion(t, "1.6.0")
	cfg := baseConfig()
	cfg.Acceptance = &config.Acceptance{
		Accepted: true,
		User:     "test-user",
		Date:     "2026-07-23",
		Version:  "1.5.3",
	}
	writeTestConfig(t, tmpDir, cfg)
	chdirTemp(t, tmpDir)

	cmd := NewRootCommand()
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"board"})
	err := cmd.Execute()

	if err != nil && containsAcceptanceError(err.Error()) {
		t.Errorf("acceptance from 1.5.3 must pass the gate on 1.6.0, got acceptance error: %v", err)
	}
	if strings.Contains(errBuf.String(), "Terms and Conditions") {
		t.Errorf("no terms text expected for a minor bump, got: %s", errBuf.String())
	}
}

func TestAcceptanceGate_ExemptInit(t *testing.T) {
	// ARRANGE: Config without acceptance, real version to enable gate
	tmpDir := t.TempDir()
	writeTestConfig(t, tmpDir, baseConfig())
	chdirTemp(t, tmpDir)
	setTestVersion(t, "0.15.0")

	// ACT: Run init --help (exempt command)
	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"init", "--help"})
	err := cmd.Execute()

	// ASSERT: Should not fail with acceptance error
	if err != nil && containsAcceptanceError(err.Error()) {
		t.Errorf("init should be exempt from acceptance gate, got: %v", err)
	}
}

func TestAcceptanceGate_ExemptAccept(t *testing.T) {
	// ARRANGE: Config without acceptance, real version to enable gate
	tmpDir := t.TempDir()
	setTestVersion(t, "0.15.0")
	writeTestConfig(t, tmpDir, baseConfig())

	// ACT: Run accept command (exempt)
	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"accept", "--yes", "--dir", tmpDir})
	err := cmd.Execute()

	// ASSERT: Should succeed (accept is exempt from gate)
	if err != nil && containsAcceptanceError(err.Error()) {
		t.Errorf("accept should be exempt from acceptance gate, got: %v", err)
	}
}

func TestAcceptanceGate_ExemptVersion(t *testing.T) {
	setTestVersion(t, "0.15.0")
	// ACT: Run --version
	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--version"})
	err := cmd.Execute()

	// ASSERT: Should not fail with acceptance error
	if err != nil && containsAcceptanceError(err.Error()) {
		t.Errorf("--version should be exempt from acceptance gate, got: %v", err)
	}
}

func TestAcceptanceGate_ExemptHelp(t *testing.T) {
	setTestVersion(t, "0.15.0")
	// ACT: Run --help
	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()

	// ASSERT: Should not fail with acceptance error
	if err != nil && containsAcceptanceError(err.Error()) {
		t.Errorf("--help should be exempt from acceptance gate, got: %v", err)
	}
}

func TestAcceptanceGate_VersionBumpRequiresReAcceptance(t *testing.T) {
	// Direct logic test for version comparison (#919: major bumps only)
	if !config.RequiresReAcceptance("1.1.0", "2.0.0") {
		t.Error("Expected re-acceptance for major version bump")
	}

	if config.RequiresReAcceptance("1.1.0", "1.2.0") {
		t.Error("Expected no re-acceptance for minor version bump")
	}

	if config.RequiresReAcceptance("1.1.0", "1.1.1") {
		t.Error("Expected no re-acceptance for patch bump")
	}
}

func TestAcceptanceGate_DeclinedExitsNonZero(t *testing.T) {
	// ARRANGE: Config without acceptance, real version to enable gate
	tmpDir := t.TempDir()
	writeTestConfig(t, tmpDir, baseConfig())
	chdirTemp(t, tmpDir)
	setTestVersion(t, "0.15.0")

	// ACT: Run a gated command — should error
	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"board"})
	err := cmd.Execute()

	// ASSERT: Error is non-nil (non-zero exit)
	if err == nil {
		t.Fatal("Expected non-zero exit for missing acceptance")
	}
}

func containsAcceptanceError(errMsg string) bool {
	return strings.Contains(errMsg, "terms") || strings.Contains(errMsg, "acceptance") || strings.Contains(errMsg, "accept")
}

func TestAcceptanceGate_NotAccepted_DisplaysTermsText(t *testing.T) {
	// ARRANGE: Config without acceptance, real version to enable gate
	tmpDir := t.TempDir()
	writeTestConfig(t, tmpDir, baseConfig())
	chdirTemp(t, tmpDir)
	setTestVersion(t, "0.15.0")

	// ACT: Run a command that requires acceptance
	cmd := NewRootCommand()
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"board"})
	_ = cmd.Execute()

	// ASSERT: Stderr should contain the full terms text
	stderr := errBuf.String()
	if !strings.Contains(stderr, "Terms and Conditions") {
		t.Errorf("Expected stderr to contain terms text, got: %s", stderr)
	}
}

func TestAcceptanceGate_NotAccepted_DisplaysYesHint(t *testing.T) {
	// ARRANGE: Config without acceptance, real version to enable gate
	tmpDir := t.TempDir()
	writeTestConfig(t, tmpDir, baseConfig())
	chdirTemp(t, tmpDir)
	setTestVersion(t, "0.15.0")

	// ACT: Run a command that requires acceptance
	cmd := NewRootCommand()
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"board"})
	_ = cmd.Execute()

	// ASSERT: Stderr should contain --yes flag hint
	stderr := errBuf.String()
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("Expected stderr to contain --yes hint, got: %s", stderr)
	}
}

func TestAcceptanceGate_Outdated_DisplaysTermsAndYesHint(t *testing.T) {
	// ARRANGE: Config with old acceptance version, real version to enable gate
	tmpDir := t.TempDir()
	setTestVersion(t, "2.0.0")
	cfg := baseConfig()
	cfg.Acceptance = &config.Acceptance{
		Accepted: true,
		User:     "test-user",
		Date:     "2026-02-20",
		Version:  "1.9.0",
	}
	writeTestConfig(t, tmpDir, cfg)
	chdirTemp(t, tmpDir)

	// ACT: Run a command (acceptance outdated due to major version bump)
	cmd := NewRootCommand()
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"board"})
	_ = cmd.Execute()

	// ASSERT: Stderr should contain terms text and --yes hint
	stderr := errBuf.String()
	if !strings.Contains(stderr, "Terms and Conditions") {
		t.Errorf("Expected stderr to contain terms text for outdated acceptance, got: %s", stderr)
	}
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("Expected stderr to contain --yes hint for outdated acceptance, got: %s", stderr)
	}
}
