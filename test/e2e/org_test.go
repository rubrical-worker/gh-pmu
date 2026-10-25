//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Org fixture tests (#940): live coverage of organization issue fields (#932),
// which the user-owned fixture cannot reach. Each test inits its own copy of
// the org template project and deletes it in cleanup, so no test depends on
// another's board state. Every test skips unless E2E_FIXTURE selects the org
// fixture and its template project is recorded in fixture_test.go.

// setupOrgProject inits a project from the org fixture's template in a fresh
// working directory, registers its deletion, and returns the directory and the
// new project's number.
func setupOrgProject(t *testing.T) (string, int) {
	t.Helper()
	f := requireFixture(t, orgFixture)

	dir := t.TempDir()
	initGitRepo(t, dir)

	result := runPMU(t, dir, "init",
		"--non-interactive",
		"--source-project", strconv.Itoa(f.TemplateProject),
		"--repo", f.Repo,
		"--owner", f.Owner,
	)
	if result.ExitCode != 0 {
		t.Fatalf("init against the org template failed: %s\n%s", result.Stdout, result.Stderr)
	}
	number := extractProjectNumber(t, result.Stdout)
	if number <= 0 {
		t.Fatalf("could not read the created project number from init output: %s", result.Stdout)
	}
	t.Cleanup(func() { deleteFixtureProject(t, f, number) })
	return dir, number
}

// orgConfig reads the working directory's .gh-pmu.json as a generic map.
func orgConfig(t *testing.T, dir string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".gh-pmu.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config: %v\n%s", err, data)
	}
	return cfg
}

// setPriorityAlias rewrites fields.priority in the working directory's config.
// No value aliases are written, so a test passes real option names straight
// through without depending on how init named the aliases.
func setPriorityAlias(t *testing.T, dir, issueField string) {
	t.Helper()
	cfg := orgConfig(t, dir)
	fields, _ := cfg["fields"].(map[string]interface{})
	if fields == nil {
		fields = map[string]interface{}{}
		cfg["fields"] = fields
	}
	alias := map[string]interface{}{"field": "Priority"}
	if issueField != "" {
		alias["issue_field"] = issueField
	}
	fields["priority"] = alias

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gh-pmu.json"), data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// cachedIssueFieldID returns the issue field ID init cached for a field.
func cachedIssueFieldID(t *testing.T, dir, name string) string {
	t.Helper()
	metadata, _ := orgConfig(t, dir)["metadata"].(map[string]interface{})
	entries, _ := metadata["fields"].([]interface{})
	for _, e := range entries {
		entry, _ := e.(map[string]interface{})
		if entry["name"] == name && entry["kind"] == "ISSUE_FIELD" {
			if id, _ := entry["issue_field_id"].(string); id != "" {
				return id
			}
		}
	}
	t.Fatalf("init cached no ISSUE_FIELD entry with an issue_field_id for %q", name)
	return ""
}

// fieldListing returns the `field list` line for a field and its options.
func fieldListing(t *testing.T, dir, name string) (string, []string) {
	t.Helper()
	result := runPMU(t, dir, "field", "list")
	assertExitCode(t, result, 0)

	lines := strings.Split(result.Stdout, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), name+" (") {
			continue
		}
		var options []string
		if i+1 < len(lines) {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(lines[i+1]), "Options: "); ok {
				options = strings.Split(rest, ", ")
			}
		}
		return line, options
	}
	t.Fatalf("field %q not listed:\n%s", name, result.Stdout)
	return "", nil
}

// createOrgTestIssue creates an [E2E] issue in the org repository and
// registers its cleanup. No status or priority is set, so creation does not
// depend on the template's option names.
func createOrgTestIssue(t *testing.T, dir, title string) int {
	t.Helper()
	result := runPMU(t, dir, "create", "--title", fmt.Sprintf("[E2E] %s", title))
	if result.ExitCode != 0 {
		t.Fatalf("create in the org repository failed: %s\n%s", result.Stdout, result.Stderr)
	}
	number := extractIssueNumber(t, result.Stdout)
	t.Cleanup(func() { runCleanupAfterTestIn(t, orgFixture.Repo, number) })
	return number
}

// TestOrgIssueFieldClassification: a Priority project field backed by the org
// issue field is classified as one both by the name-and-type match (a fresh
// board with no values) and once an item carries a value, when item-value
// pairing sees it. Which rule fired is not visible from the CLI; the test
// asserts the classification holds in both board states.
func TestOrgIssueFieldClassification(t *testing.T) {
	dir, _ := setupOrgProject(t)

	t.Run("name_and_type_on_a_board_with_no_values", func(t *testing.T) {
		line, _ := fieldListing(t, dir, "Priority")
		assertContains(t, line, "org issue field")
	})

	t.Run("item_value_pairing_after_a_value_is_set", func(t *testing.T) {
		setPriorityAlias(t, dir, "")
		_, options := fieldListing(t, dir, "Priority")
		if len(options) == 0 {
			t.Fatal("org Priority field lists no options")
		}
		issue := createOrgTestIssue(t, dir, "Org classification pairing")
		assertExitCode(t, runPMU(t, dir, "move", strconv.Itoa(issue), "--priority", options[0]), 0)

		line, _ := fieldListing(t, dir, "Priority")
		assertContains(t, line, "org issue field")
	})
}

// TestOrgIssueFieldOverride: fields.<alias>.issue_field pins the field to an
// issue field by ID, and "none" keeps it on the project-field path.
func TestOrgIssueFieldOverride(t *testing.T) {
	dir, _ := setupOrgProject(t)
	id := cachedIssueFieldID(t, dir, "Priority")

	t.Run("id_pins_the_issue_field", func(t *testing.T) {
		setPriorityAlias(t, dir, id)
		line, _ := fieldListing(t, dir, "Priority")
		assertContains(t, line, "org issue field")
	})

	t.Run("none_keeps_a_project_field", func(t *testing.T) {
		setPriorityAlias(t, dir, "none")
		line, _ := fieldListing(t, dir, "Priority")
		assertNotContains(t, line, "org issue field")
	})
}

// TestOrgMovePriorityWritesIssueField: `move --priority` on an issue-backed
// Priority goes through setIssueFieldValue, and `view` reads the value back.
// The DATE write path is unit-tested (TestSetProjectItemField_DateIssueField):
// no gh pmu command writes an arbitrary DATE field.
func TestOrgMovePriorityWritesIssueField(t *testing.T) {
	dir, _ := setupOrgProject(t)
	setPriorityAlias(t, dir, "")

	line, options := fieldListing(t, dir, "Priority")
	assertContains(t, line, "org issue field")
	if len(options) == 0 {
		t.Fatal("org Priority field lists no options")
	}
	want := options[len(options)-1]

	issue := createOrgTestIssue(t, dir, "Org priority write")
	assertExitCode(t, runPMU(t, dir, "move", strconv.Itoa(issue), "--priority", want), 0)

	var got string
	for attempt := 0; attempt < 5; attempt++ {
		result := runPMU(t, dir, "view", strconv.Itoa(issue), "--json", "fieldValues")
		assertExitCode(t, result, 0)
		var out struct {
			FieldValues map[string]interface{} `json:"fieldValues"`
		}
		if err := json.Unmarshal([]byte(result.Stdout), &out); err != nil {
			t.Fatalf("parse view output: %v\n%s", err, result.Stdout)
		}
		got, _ = out.FieldValues["Priority"].(string)
		if got == want {
			return
		}
		time.Sleep(2 * time.Second) // eventual consistency, as waitForProjectSync
	}
	t.Errorf("view shows Priority %q, want %q", got, want)
}

// TestOrgInitCreatesOrgProjectAndCache: init --owner <org> creates an
// org-owned project, caches the issue field classification in .gh-pmu.json,
// and the project is deleted in cleanup (registered by setupOrgProject).
func TestOrgInitCreatesOrgProjectAndCache(t *testing.T) {
	dir, number := setupOrgProject(t)
	cfg := orgConfig(t, dir)

	project, _ := cfg["project"].(map[string]interface{})
	if project["owner"] != orgFixture.Owner {
		t.Errorf("project.owner = %v, want %q", project["owner"], orgFixture.Owner)
	}
	if n, _ := project["number"].(float64); int(n) != number {
		t.Errorf("project.number = %v, want the created project %d", project["number"], number)
	}
	if id := cachedIssueFieldID(t, dir, "Priority"); !strings.HasPrefix(id, "IF") {
		t.Errorf("cached Priority issue_field_id = %q, want an issue field ID (IF…)", id)
	}
}
