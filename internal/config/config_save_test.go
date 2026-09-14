package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Save previously accepted a full file path and silently discarded the
// basename, writing to filepath.Join(filepath.Dir(path), ConfigFileName)
// regardless of the filename passed. The signature now names what it actually
// takes — a directory (#874).

func TestSave_WritesConfigFileIntoGivenDirectory(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Version: "1.4.10"}
	cfg.Project.Name = "gh-pmu"

	if err := cfg.Save(dir); err != nil {
		t.Fatalf("Expected Save to succeed; got %v", err)
	}

	written := filepath.Join(dir, ConfigFileName)
	if _, err := os.Stat(written); err != nil {
		t.Fatalf("Expected %s to exist; got %v", written, err)
	}

	loaded, err := Load(written)
	if err != nil {
		t.Fatalf("Expected the written config to load; got %v", err)
	}
	if loaded.Project.Name != "gh-pmu" {
		t.Errorf("Expected round-tripped project name gh-pmu; got %q", loaded.Project.Name)
	}
}

// The path-to-directory change is not compile-enforced (both are strings), so a
// stale caller passing the config file path must fail loudly rather than write
// to <config>/.gh-pmu.json.
func TestSave_RejectsAConfigFilePath(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Version: "1.4.10"}

	err := cfg.Save(filepath.Join(dir, ConfigFileName))
	if err == nil {
		t.Errorf("Expected Save to reject a %s file path", ConfigFileName)
	}
}

// A nested directory that does not exist must surface an error rather than
// writing somewhere unexpected.
func TestSave_MissingDirectoryReturnsError(t *testing.T) {
	cfg := &Config{Version: "1.4.10"}

	err := cfg.Save(filepath.Join(t.TempDir(), "does", "not", "exist"))
	if err == nil {
		t.Fatal("Expected an error when the target directory is absent")
	}
}

// Config.Save re-marshalled the struct, so any top-level key the running binary
// did not model was dropped on write (#910). The read side has always been
// tolerant — json.Unmarshal with no DisallowUnknownFields — which is what made
// the loss silent: the file loaded cleanly, then came back shorter.
//
// Approach 1 was chosen: capture unmodeled keys on decode, re-emit them on
// encode. The tests below assert on raw bytes throughout. A round-trip through
// Config cannot observe any of this, because the keys are absent from the
// struct on both sides — a struct comparison passes whether or not they
// survived, which is precisely the blind spot that let the defect ship.

// unmodeledConfigJSON carries two keys Config does not model. Neither is
// `release`: #902 removed that field, and a test keyed to it would assert on
// the aftermath of one specific removal rather than on the general property.
const unmodeledConfigJSON = `{
  "version": "1.5.2",
  "project": {
    "number": 11,
    "owner": "test-owner"
  },
  "repositories": [
    "test-owner/test-repo"
  ],
  "customField": "value",
  "experimental": {
    "nested": true,
    "count": 3
  }
}
`

// topLevelKeyOrder returns the top-level object keys in the order they appear
// in data. json.Unmarshal into a map cannot answer this — Go maps have no
// order — and key order is exactly what the seventh acceptance criterion
// protects, so the token stream is read directly.
func topLevelKeyOrder(t *testing.T, data []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("Failed to read the opening token: %v", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		t.Fatalf("Expected a JSON object, got %v", tok)
	}
	var keys []string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			t.Fatalf("Failed to read a key: %v", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			t.Fatalf("Expected a string key, got %v", keyTok)
		}
		keys = append(keys, key)
		var discard json.RawMessage
		if err := dec.Decode(&discard); err != nil {
			t.Fatalf("Failed to read the value for %q: %v", key, err)
		}
	}
	return keys
}

// seedConfigFile writes contents to ConfigFileName in a fresh temp directory
// and returns the directory and the file path.
func seedConfigFile(t *testing.T, contents string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("Failed to seed %s: %v", ConfigFileName, err)
	}
	return dir, path
}

func TestSave_PreservesKeysTheStructDoesNotModel(t *testing.T) {
	// ARRANGE
	dir, path := seedConfigFile(t, unmodeledConfigJSON)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Expected the seeded config to load; got %v", err)
	}

	// ACT
	if err := cfg.Save(dir); err != nil {
		t.Fatalf("Expected Save to succeed; got %v", err)
	}

	// ASSERT
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read the saved config: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Saved config is not valid JSON: %v\n%s", err, data)
	}

	// Values, not just presence. A key that survives with its value replaced is
	// still data loss, and asserting only on presence would not catch it.
	want := map[string]string{
		"customField":  `"value"`,
		"experimental": `{"nested":true,"count":3}`,
	}
	for key, wantValue := range want {
		raw, ok := got[key]
		if !ok {
			t.Errorf("Save dropped unmodeled key %q. Saved file:\n%s", key, data)
			continue
		}
		var compacted bytes.Buffer
		if err := json.Compact(&compacted, raw); err != nil {
			t.Fatalf("Value of %q is not valid JSON: %v", key, err)
		}
		if compacted.String() != wantValue {
			t.Errorf("Value of unmodeled key %q changed.\n  want: %s\n  got:  %s", key, wantValue, compacted.String())
		}
	}
}

func TestSave_ModeledKeysKeepDeclarationOrderAlongsideUnmodeledKeys(t *testing.T) {
	// ARRANGE
	dir, path := seedConfigFile(t, unmodeledConfigJSON)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Expected the seeded config to load; got %v", err)
	}

	// ACT
	if err := cfg.Save(dir); err != nil {
		t.Fatalf("Expected Save to succeed; got %v", err)
	}

	// ASSERT: modeled keys first in struct-declaration order, unmodeled keys
	// after in sorted order. Merging both into one map before marshalling would
	// have reordered the modeled keys alphabetically — every existing config
	// would show a diff on its next write, which is what this pins down.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read the saved config: %v", err)
	}
	// "defaults" is present although the seeded file has no such key: Defaults
	// is a struct, and omitempty has no effect on structs, so Save has always
	// emitted "defaults": {}. That predates this issue and the seventh
	// acceptance criterion protects current behaviour, so it is asserted rather
	// than corrected here.
	want := []string{"version", "project", "repositories", "defaults", "customField", "experimental"}
	got := topLevelKeyOrder(t, data)
	if len(got) != len(want) {
		t.Fatalf("Expected keys %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Key order changed.\n  want: %v\n  got:  %v", want, got)
		}
	}
}

// fullyModeledConfig populates every field Config models, so a golden
// comparison over its output covers the whole document rather than the handful
// of keys a minimal fixture happens to emit.
func fullyModeledConfig() *Config {
	return &Config{
		Version:      "1.5.3",
		Project:      Project{Name: "gh-pmu", Number: 11, Owner: "test-owner", View: 2},
		Repositories: []string{"test-owner/test-repo", "test-owner/other-repo"},
		Framework:    "IDPF-Agile",
		Defaults:     Defaults{Priority: "P2", Status: "Backlog", Labels: []string{"needs-triage"}},
		Fields:       map[string]Field{"status": {Field: "Status", Values: map[string]string{"wip": "In Progress"}}},
		Triage: map[string]Triage{
			"stale": {Query: "is:open updated:<2026-01-01", Apply: TriageApply{Labels: []string{"stale"}}, Interactive: TriageInteractive{Status: true}},
		},
		Acceptance: &Acceptance{Accepted: true, User: "test-owner", Date: "2026-08-25", Version: "1.5.0"},
		Metadata:   &Metadata{Project: ProjectMetadata{ID: "PVT_kwTest"}, Fields: []FieldMetadata{{Name: "Status", ID: "PVTF_test"}}},
	}
}

// savedConfigGolden is the exact output Save produced for fullyModeledConfig
// before #910 changed the write path. It was captured from the pre-change
// binary and diffed against the post-change binary, so it pins the seventh
// acceptance criterion to observed behaviour rather than to intent.
//
// It carries three guarantees in one comparison: top-level key order follows
// struct declaration rather than the alphabet, indentation is two spaces, and
// the file ends in exactly one newline. The \u003c is not a typo --
// encoding/json escapes <, > and & by default, and that escaping is part of
// the behaviour being held still.
const savedConfigGolden = `{
  "version": "1.5.3",
  "project": {
    "name": "gh-pmu",
    "number": 11,
    "owner": "test-owner",
    "view": 2
  },
  "repositories": [
    "test-owner/test-repo",
    "test-owner/other-repo"
  ],
  "framework": "IDPF-Agile",
  "defaults": {
    "priority": "P2",
    "status": "Backlog",
    "labels": [
      "needs-triage"
    ]
  },
  "fields": {
    "status": {
      "field": "Status",
      "values": {
        "wip": "In Progress"
      }
    }
  },
  "triage": {
    "stale": {
      "query": "is:open updated:\u003c2026-01-01",
      "apply": {
        "labels": [
          "stale"
        ]
      },
      "interactive": {
        "status": true
      }
    }
  },
  "acceptance": {
    "accepted": true,
    "user": "test-owner",
    "date": "2026-08-25",
    "version": "1.5.0"
  },
  "metadata": {
    "project": {
      "id": "PVT_kwTest"
    },
    "fields": [
      {
        "name": "Status",
        "id": "PVTF_test",
        "data_type": ""
      }
    ]
  }
}
`

func TestSave_OutputIsUnchangedWhenNoUnmodeledKeysArePresent(t *testing.T) {
	// ARRANGE
	dir := t.TempDir()
	cfg := fullyModeledConfig()

	// ACT
	if err := cfg.Save(dir); err != nil {
		t.Fatalf("Expected Save to succeed; got %v", err)
	}

	// ASSERT: raw bytes. Adding MarshalJSON to Config routed every write through
	// a new code path, including the writes carrying nothing unmodeled -- which
	// is every config in existence today. A struct-level assertion would not
	// notice if that path reordered or reindented the document.
	got, err := os.ReadFile(filepath.Join(dir, ConfigFileName))
	if err != nil {
		t.Fatalf("Failed to read the saved config: %v", err)
	}
	if string(got) != savedConfigGolden {
		t.Errorf("Saved config differs from the pre-#910 output.\n--- want ---\n%s\n--- got ---\n%s", savedConfigGolden, got)
	}
}

// The five call sites of Save must all behave the same way (#910). They do,
// because preservation is a property of Save itself rather than of any caller —
// so what these tests cover is the two conditions that could break that:
// a caller saving a Config it built rather than one it loaded, and a sixth
// caller appearing that does neither.
//
// The two in-package call sites are exercised directly below. The three in cmd/
// share one shape — load, mutate, save — which TestLoadMutateSave_... pins down
// at the package boundary, since reaching them for real would mean standing up
// command plumbing to test a property that is not theirs.

func TestLoadFromDirectoryAndNormalize_PreservesUnmodeledKeysWhenItSavesUnasked(t *testing.T) {
	// ARRANGE: an unmodeled key and no framework key. The missing framework is
	// what makes this path write at all — it normalises to IDPF and saves. This
	// is the worst of the five triggers, because it is a load function that
	// writes: no command the user typed reads as a write, and before this change
	// the key was gone by the time anything else ran.
	dir, path := seedConfigFile(t, `{
  "version": "1.5.2",
  "project": {"number": 11, "owner": "test-owner"},
  "repositories": ["test-owner/test-repo"],
  "customField": "value"
}
`)

	// ACT
	cfg, err := LoadFromDirectoryAndNormalize(dir)
	if err != nil {
		t.Fatalf("Expected the config to load; got %v", err)
	}

	// ASSERT: the normalisation happened, and it did not cost the unmodeled key.
	if cfg.Framework != "IDPF" {
		t.Errorf("Expected framework to normalise to IDPF, got %q", cfg.Framework)
	}
	assertRawKeyValue(t, path, "customField", `"value"`)
}

func TestRefreshVersion_PreservesUnmodeledKeys(t *testing.T) {
	// ARRANGE: a stale version, so the refresh writes.
	dir, path := seedConfigFile(t, `{
  "version": "1.1.0",
  "project": {"number": 11, "owner": "test-owner"},
  "repositories": ["test-owner/test-repo"],
  "framework": "IDPF-Agile",
  "customField": "value"
}
`)

	// ACT
	wrote, err := RefreshVersion(dir, "1.5.3")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !wrote {
		t.Fatal("Expected RefreshVersion to report a write for a stale version")
	}

	// ASSERT
	assertRawKeyValue(t, path, "version", `"1.5.3"`)
	assertRawKeyValue(t, path, "customField", `"value"`)
}

func TestLoadMutateSave_PreservesUnmodeledKeys(t *testing.T) {
	// ARRANGE: the shape cmd/accept.go, cmd/field.go and cmd/config.go all share
	// — each loads the existing config, changes one modeled field, and saves the
	// same value it loaded. That last part is what makes preservation reach them:
	// a caller that built a fresh Config instead would carry no overflow, and no
	// amount of care inside Save could recover it.
	dir, path := seedConfigFile(t, unmodeledConfigJSON)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Expected the seeded config to load; got %v", err)
	}

	// ACT
	cfg.Framework = "IDPF-Agile"
	if err := cfg.Save(dir); err != nil {
		t.Fatalf("Expected Save to succeed; got %v", err)
	}

	// ASSERT
	assertRawKeyValue(t, path, "framework", `"IDPF-Agile"`)
	assertRawKeyValue(t, path, "customField", `"value"`)
	assertRawKeyValue(t, path, "experimental", `{"nested":true,"count":3}`)
}

// assertRawKeyValue reads the file at path and asserts that key holds exactly
// wantCompact, compared as compacted JSON so indentation does not enter into it.
func assertRawKeyValue(t *testing.T, path, key, wantCompact string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read %s: %v", path, err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("%s is not valid JSON: %v", path, err)
	}
	raw, ok := got[key]
	if !ok {
		t.Errorf("Key %q is absent from the saved config. File:\n%s", key, data)
		return
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, raw); err != nil {
		t.Fatalf("Value of %q is not valid JSON: %v", key, err)
	}
	if compacted.String() != wantCompact {
		t.Errorf("Value of %q:\n  want: %s\n  got:  %s", key, wantCompact, compacted.String())
	}
}

// TestSaveCallSiteInventoryIsUnchanged fails when the set of Save callers
// changes, so a new one gets the same look the five current ones were given.
//
// What it proves: the inventory the fourth acceptance criterion enumerates is
// still the whole inventory. What it does not prove: that a caller saves the
// Config it loaded rather than one it built. That distinction is the one thing
// preservation actually depends on at the call site, and it is not decidable
// from a line-level scan — so it is checked by hand when this test fails, which
// is the only moment it needs checking.
//
// Complementary to the guard in config_test.go covering os.WriteFile against
// ConfigFileName: that one catches a writer that bypasses Save altogether, this
// one catches a caller of Save that nobody has looked at yet. Neither subsumes
// the other.
func TestSaveCallSiteInventoryIsUnchanged(t *testing.T) {
	want := map[string]int{
		"cmd/accept.go":             1, // gh pmu accept
		"cmd/field.go":              1, // gh pmu field mutations
		"cmd/config.go":             1, // resolveAndPersistView
		"cmd/status.go":             1, // gh pmu status --update (saves the Config runStatus loaded, #917)
		"internal/config/config.go": 2, // LoadFromDirectoryAndNormalize, RefreshVersion
	}

	got := map[string][]int{}
	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "vendor", "testdata", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		for i, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			// A prose mention is not a call site. internal/integrity/integrity.go
			// says "Call this after config.Save()" in a doc comment.
			if strings.HasPrefix(trimmed, "//") || !strings.Contains(line, ".Save(") {
				continue
			}
			got[rel] = append(got[rel], i+1)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk %s: %v", root, err)
	}

	for file, lines := range got {
		if want[file] == 0 {
			t.Errorf("New caller of Save in %s at line(s) %v. Confirm it saves a Config it loaded "+
				"rather than one it constructed, then add it here and to the fourth acceptance "+
				"criterion of #910.", file, lines)
			continue
		}
		if len(lines) != want[file] {
			t.Errorf("%s: expected %d Save call site(s), found %d at line(s) %v",
				file, want[file], len(lines), lines)
		}
	}
	for file, n := range want {
		if len(got[file]) == 0 {
			t.Errorf("%s: expected %d Save call site(s), found none — if one moved, update this "+
				"guard so it keeps covering it", file, n)
		}
	}
}

// legacyReleaseConfigJSON is a config in the pre-#902 shape, with release fully
// populated. It matches the fixture in TestLoad_PopulatedReleaseBlock_IsIgnored
// deliberately: that test owns the load half of this guarantee, this one owns
// the write half, and they should be describing the same file.
const legacyReleaseConfigJSON = `{
  "project": {"name": "gh-pmu", "number": 11, "owner": "rubrical-worker"},
  "repositories": ["rubrical-worker/gh-pmu"],
  "release": {
    "tracks": {"stable": {"prefix": "v", "default": true}},
    "artifacts": {"directory": "Releases", "release_notes": true},
    "coverage": {"enabled": true, "threshold": 80}
  }
}
`

// TestLoad_PopulatedReleaseBlock_LoadsSilentlyAndSurvivesSave records what #910
// changes about the release block, which is the one unmodeled key known to
// exist in real configs today.
//
// #902's fourth acceptance criterion still holds unchanged: the block loads with
// no error and no warning. That half is guaranteed by construction rather than
// by assertion — Load takes no writer and returns only (*Config, error), so
// there is nowhere for a warning to go.
//
// What is new is the second half. Before #910 the block loaded silently and was
// then deleted by the next write. Now it loads silently and stays. That is a
// deliberate consequence of preserving unmodeled keys generally, not a partial
// revert of #902: the Release struct is still gone, nothing reads the block, and
// TestSave_OmitsReleaseKey still requires that a config which never carried one
// does not acquire one.
func TestLoad_PopulatedReleaseBlock_LoadsSilentlyAndSurvivesSave(t *testing.T) {
	// ARRANGE
	dir, path := seedConfigFile(t, legacyReleaseConfigJSON)

	// ACT
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Expected a populated release block to load cleanly, got: %v", err)
	}
	if err := cfg.Save(dir); err != nil {
		t.Fatalf("Expected Save to succeed; got %v", err)
	}

	// ASSERT: the block came back whole, not merely present.
	assertRawKeyValue(t, path, "release",
		`{"tracks":{"stable":{"prefix":"v","default":true}},`+
			`"artifacts":{"directory":"Releases","release_notes":true},`+
			`"coverage":{"enabled":true,"threshold":80}}`)

	// And the struct still does not model it: nothing here re-introduces a
	// Release field, it is carried as opaque bytes.
	if _, modeled := modeledConfigKeys["release"]; modeled {
		t.Error("Config models a release key again — #902 removed it and #910 does not restore it")
	}
}
