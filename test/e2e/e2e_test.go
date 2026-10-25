//go:build e2e

// Package e2e provides end-to-end tests for gh-pmu that run against
// a real GitHub test project. These tests build and execute the local
// binary with coverage instrumentation.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/testutil/ratelimit"
)

var (
	// binaryPath holds the path to the built binary
	binaryPath string

	// projectRoot holds the path to the project root directory
	projectRoot string

	// limiter is the single pacing point for every live API call the suite
	// makes: runPMU and runGH both go through it (#920).
	limiter *ratelimit.Limiter
)

// TestMain builds the binary with coverage instrumentation before running tests.
func TestMain(m *testing.M) {
	// Find project root by looking for go.mod
	root, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to find project root: %v\n", err)
		os.Exit(1)
	}
	projectRoot = root

	// Pace and guard every live API call (#920). An invalid override is an
	// error, not a silent fallback to the defaults.
	rlCfg, err := ratelimit.ConfigFromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid rate-limit configuration: %v\n", err)
		os.Exit(1)
	}
	limiter = ratelimit.New(rlCfg, ratelimit.RealClock{})

	// Select fixtures (#940): user by default, so today's runs are unchanged.
	selectedFixtures, err = parseFixtureSelection(os.Getenv(envFixture))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid fixture selection: %v\n", err)
		os.Exit(1)
	}

	// Build binary with coverage instrumentation
	binPath, err := buildBinary(projectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build binary: %v\n", err)
		os.Exit(1)
	}
	binaryPath = binPath

	// Ensure binary is cleaned up after tests
	defer os.Remove(binaryPath)

	// Run tests
	code := m.Run()
	os.Exit(code)
}

// findProjectRoot walks up from the current directory to find go.mod.
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found in any parent directory")
		}
		dir = parent
	}
}

// buildBinary compiles the gh-pmu binary with coverage instrumentation.
func buildBinary(projectRoot string) (string, error) {
	// Determine binary name based on platform
	binaryName := "gh-pmu-e2e-test"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	// Build in temp directory
	tmpDir := os.TempDir()
	binPath := filepath.Join(tmpDir, binaryName)

	// Build with coverage instrumentation
	// -cover enables coverage collection
	// -o specifies output path
	cmd := exec.Command("go", "build", "-cover", "-o", binPath, ".")
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build failed: %w", err)
	}

	// Verify binary was created
	if _, err := os.Stat(binPath); err != nil {
		return "", fmt.Errorf("binary not found after build: %w", err)
	}

	fmt.Printf("Built E2E test binary: %s\n", binPath)
	return binPath, nil
}

// getBinaryPath returns the path to the built binary.
// This is used by test helper functions.
func getBinaryPath() string {
	return binaryPath
}

// getProjectRoot returns the path to the project root.
func getProjectRoot() string {
	return projectRoot
}
