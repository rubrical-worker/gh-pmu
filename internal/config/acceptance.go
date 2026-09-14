package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Acceptance holds the terms acceptance state for a repository.
type Acceptance struct {
	Accepted bool   `yaml:"accepted" json:"accepted"`
	User     string `yaml:"user,omitempty" json:"user,omitempty"`
	Date     string `yaml:"date,omitempty" json:"date,omitempty"`
	Version  string `yaml:"version,omitempty" json:"version,omitempty"`
}

// RequiresReAcceptance returns true if the user needs to (re-)accept terms.
// Re-acceptance is required when:
//   - No prior acceptance (empty acceptedVersion)
//   - Major version changed, in either direction
//   - Prior acceptance was on a dev build
//   - Either version cannot be parsed
//
// Re-acceptance is NOT required when:
//   - Only the minor or patch version changed (#919)
//   - Current build is "dev"
//   - Versions are identical
func RequiresReAcceptance(acceptedVersion, currentVersion string) bool {
	if acceptedVersion == "" {
		return true
	}

	// Dev builds never require re-acceptance
	if currentVersion == "dev" {
		return false
	}

	// Accepted on dev always requires re-acceptance for real versions
	if acceptedVersion == "dev" {
		return true
	}

	acceptedMajor, _, err := parseMajorMinor(acceptedVersion)
	if err != nil {
		return true
	}

	currentMajor, _, err := parseMajorMinor(currentVersion)
	if err != nil {
		return true
	}

	return acceptedMajor != currentMajor
}

// parseMajorMinor extracts major and minor version numbers from a semver string.
// Both components must be numeric for the version to count as parseable, even
// though only the major is compared.
func parseMajorMinor(version string) (int, int, error) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("invalid version: %s", version)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid major version: %s", parts[0])
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid minor version: %s", parts[1])
	}

	return major, minor, nil
}
