//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
)

// Fixture is one live GitHub owner the suite runs against (#940). The user
// fixture is the original target. The org fixture covers what a user-owned
// board cannot reach: organization(login:) resolution, org-scoped init, the
// organization.issueFields query and setIssueFieldValue writes.
//
// This file is the only place owner, repository and template-project literals
// may appear in test/e2e.
type Fixture struct {
	// Name is the E2E_FIXTURE value that selects this fixture.
	Name string
	// Owner is the GitHub login that owns the repository and projects.
	Owner string
	// OwnerType is "user" or "org".
	OwnerType string
	// Repo is the test repository, owner/name.
	Repo string
	// TemplateProject is the project init copies from. Orphan cleanup never
	// deletes it. Zero means not yet created: every test needing the fixture
	// skips, and cleanup leaves the owner alone.
	TemplateProject int
	// ProjectName is the template project's title.
	ProjectName string
}

// RepoName returns the repository name without its owner. init titles the
// projects it creates after it, which is how orphan cleanup finds them.
func (f Fixture) RepoName() string {
	_, name, _ := strings.Cut(f.Repo, "/")
	return name
}

var (
	userFixture = Fixture{
		Name:            "user",
		Owner:           "rubrical-worker",
		OwnerType:       "user",
		Repo:            "rubrical-worker/gh-pmu-e2e-test",
		TemplateProject: 41,
		ProjectName:     "IDPF-gh-pmu-testing",
	}

	// orgFixture's repository and template project are created by hand (see
	// test/e2e/README.md, "Fixtures"). Record the template's number here once
	// it exists; until then every org test skips naming this file.
	orgFixture = Fixture{
		Name:            "org",
		Owner:           "rubrical-works",
		OwnerType:       "org",
		Repo:            "rubrical-works/gh-pmu-e2e-test",
		TemplateProject: 0,
		ProjectName:     "gh-pmu-e2e-org-template",
	}

	allFixtures = []Fixture{userFixture, orgFixture}

	// selectedFixtures is set in TestMain from E2E_FIXTURE.
	selectedFixtures map[string]bool
)

// envFixture selects which fixtures run: user (default), org, or all. The
// default keeps today's runs and API volume unchanged.
const envFixture = "E2E_FIXTURE"

// parseFixtureSelection maps an E2E_FIXTURE value to the fixtures it selects.
func parseFixtureSelection(value string) (map[string]bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "user":
		return map[string]bool{"user": true}, nil
	case "org":
		return map[string]bool{"org": true}, nil
	case "all":
		return map[string]bool{"user": true, "org": true}, nil
	default:
		return nil, fmt.Errorf("%s=%q: want user, org or all", envFixture, value)
	}
}

// requireFixture skips the test unless f is selected and its template
// project exists, and returns f for convenience.
func requireFixture(t *testing.T, f Fixture) Fixture {
	t.Helper()
	if !selectedFixtures[f.Name] {
		t.Skipf("%s fixture not selected (set %s=%s or %s=all)", f.Name, envFixture, f.Name, envFixture)
	}
	if f.TemplateProject == 0 {
		t.Skipf("%s fixture's template project is not recorded in test/e2e/fixture_test.go yet (see test/e2e/README.md, \"Fixtures\")", f.Name)
	}
	return f
}
