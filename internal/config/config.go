package config

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the .gh-pmu.json configuration file
type Config struct {
	Version      string            `yaml:"version,omitempty" json:"version,omitempty"`
	Project      Project           `yaml:"project" json:"project"`
	Repositories []string          `yaml:"repositories" json:"repositories"`
	Framework    string            `yaml:"framework,omitempty" json:"framework,omitempty"`
	Defaults     Defaults          `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Fields       map[string]Field  `yaml:"fields,omitempty" json:"fields,omitempty"`
	Triage       map[string]Triage `yaml:"triage,omitempty" json:"triage,omitempty"`
	Acceptance   *Acceptance       `yaml:"acceptance,omitempty" json:"acceptance,omitempty"`
	Metadata     *Metadata         `yaml:"metadata,omitempty" json:"metadata,omitempty"`

	// unknown holds the top-level keys the file carried that this struct does
	// not model, so Save can put them back rather than deleting them (#910).
	// Unexported on purpose: encoding/json ignores it, so the overflow can never
	// itself become a config key.
	//
	// Populated on the JSON path only. FindConfigFile resolves .gh-pmu.json and
	// Save writes JSON, so no reachable path round-trips YAML.
	unknown map[string]json.RawMessage
}

// modeledConfigKeys is the set of top-level JSON keys Config models, derived
// from the struct tags rather than listed by hand. A hand-kept list would fall
// out of date the first time a field was added, and the failure would be quiet:
// the new field would be marshalled by the struct *and* replayed from the
// overflow map, emitting the same key twice.
var modeledConfigKeys = jsonObjectKeys(reflect.TypeOf(Config{}))

// jsonObjectKeys returns the JSON key each exported field of t encodes to.
func jsonObjectKeys(t reflect.Type) map[string]struct{} {
	keys := make(map[string]struct{}, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue // unexported — never a JSON key
		}
		name := field.Name
		if tag, ok := field.Tag.Lookup("json"); ok {
			if tag == "-" {
				continue // explicitly not encoded
			}
			if tagName, _, _ := strings.Cut(tag, ","); tagName != "" {
				name = tagName
			}
		}
		keys[name] = struct{}{}
	}
	return keys
}

// UnmarshalJSON decodes a config and captures every top-level key Config does
// not model, so a later Save can write it back unchanged (#910).
//
// The read side stays exactly as tolerant as it was — unmodeled keys are still
// accepted with no error and no warning, which is what #902's fourth acceptance
// criterion requires of a config still carrying a populated `release` block.
// What changes is only that the keys are now remembered instead of discarded.
func (c *Config) UnmarshalJSON(data []byte) error {
	// The alias sheds Config's methods, so this does not recurse.
	type alias Config
	if err := json.Unmarshal(data, (*alias)(c)); err != nil {
		return err
	}

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	for key := range all {
		if _, modeled := modeledConfigKeys[key]; modeled {
			delete(all, key)
		}
	}
	if len(all) == 0 {
		all = nil
	}
	c.unknown = all

	return nil
}

// MarshalJSON writes the modeled fields followed by any keys captured at decode
// time (#910).
//
// Those keys are spliced into the encoded struct rather than merged into a map
// first. encoding/json emits struct fields in declaration order but map keys in
// sorted order, so merging would silently realphabetise every existing config —
// a diff in every repository on the next write, for no reason a user could see.
// Splicing leaves the modeled keys where they have always been and appends the
// rest.
//
// The result is compact. Save marshals through json.MarshalIndent, which
// re-indents whatever this returns, so the captured values pick up the
// surrounding indentation rather than keeping the whitespace they had on disk.
func (c *Config) MarshalJSON() ([]byte, error) {
	type alias Config
	encoded, err := json.Marshal((*alias)(c))
	if err != nil {
		return nil, err
	}
	if len(c.unknown) == 0 {
		return encoded, nil
	}
	// Splicing assumes an object; a struct always encodes to one. Checked so a
	// future change to that assumption fails here rather than writing a
	// corrupted config file.
	if len(encoded) == 0 || encoded[len(encoded)-1] != '}' {
		return nil, fmt.Errorf("failed to marshal %s: expected a JSON object, got %q", ConfigFileName, encoded)
	}

	// Sorted for determinism. The order these keys had in the file is not
	// recoverable from encoding/json without a token-level decoder, and nothing
	// depends on it.
	keys := make([]string, 0, len(c.unknown))
	for key := range c.unknown {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	buf.Write(encoded[:len(encoded)-1])
	for _, key := range keys {
		if buf.Len() > 1 { // not the first member of the object
			buf.WriteByte(',')
		}
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf.Write(encodedKey)
		buf.WriteByte(':')
		buf.Write(c.unknown[key])
	}
	buf.WriteByte('}')

	return buf.Bytes(), nil
}

// Project contains GitHub project configuration
type Project struct {
	Name   string `yaml:"name,omitempty" json:"name,omitempty"`
	Number int    `yaml:"number" json:"number"`
	Owner  string `yaml:"owner" json:"owner"`
	// View is the number of the project's first Backlog view with a BOARD_LAYOUT
	// layout, resolved from the API and cached here (#901). omitempty keeps configs
	// that predate resolution byte-identical. Zero means unresolved, never view 1:
	// view numbers are creation ordinals starting at 1 and are never backfilled, so
	// org-owned boards routinely start at 2.
	View int `yaml:"view,omitempty" json:"view,omitempty"`
}

// Defaults contains default values for new issues
type Defaults struct {
	Priority string   `yaml:"priority,omitempty" json:"priority,omitempty"`
	Status   string   `yaml:"status,omitempty" json:"status,omitempty"`
	Labels   []string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// Field maps field aliases to GitHub project field names and values
type Field struct {
	Field  string            `yaml:"field" json:"field"`
	Values map[string]string `yaml:"values,omitempty" json:"values,omitempty"`
	// IssueField overrides org issue field detection for this field (#932):
	// an issue field ID (IFSS_…/IFD_…) pins it, "none" keeps it a project field.
	IssueField string `yaml:"issue_field,omitempty" json:"issue_field,omitempty"`
}

// Triage contains configuration for triage rules
type Triage struct {
	Query       string            `yaml:"query" json:"query"`
	Apply       TriageApply       `yaml:"apply,omitempty" json:"apply,omitempty"`
	Interactive TriageInteractive `yaml:"interactive,omitempty" json:"interactive,omitempty"`
}

// TriageApply contains fields to apply during triage
type TriageApply struct {
	Labels []string          `yaml:"labels,omitempty" json:"labels,omitempty"`
	Fields map[string]string `yaml:"fields,omitempty" json:"fields,omitempty"`
}

// TriageInteractive contains interactive prompts for triage
type TriageInteractive struct {
	Status   bool `yaml:"status,omitempty" json:"status,omitempty"`
	Estimate bool `yaml:"estimate,omitempty" json:"estimate,omitempty"`
}

// Metadata contains cached project metadata from GitHub API
type Metadata struct {
	Project ProjectMetadata `yaml:"project,omitempty" json:"project,omitempty"`
	Fields  []FieldMetadata `yaml:"fields,omitempty" json:"fields,omitempty"`
}

// ProjectMetadata contains cached project info
type ProjectMetadata struct {
	ID string `yaml:"id,omitempty" json:"id,omitempty"`
}

// FieldMetadata contains cached field info
type FieldMetadata struct {
	Name     string           `yaml:"name" json:"name"`
	ID       string           `yaml:"id" json:"id"`
	DataType string           `yaml:"data_type" json:"data_type"`
	Options  []OptionMetadata `yaml:"options,omitempty" json:"options,omitempty"`
	// Kind is PROJECT_FIELD or ISSUE_FIELD (#932). Absent in caches written
	// before org issue fields were detected.
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
	// IssueFieldID and IssueFieldDataType identify the org issue field behind
	// an ISSUE_FIELD entry; Options then holds that issue field's options.
	IssueFieldID       string `yaml:"issue_field_id,omitempty" json:"issue_field_id,omitempty"`
	IssueFieldDataType string `yaml:"issue_field_data_type,omitempty" json:"issue_field_data_type,omitempty"`
}

// OptionMetadata contains cached field option info
type OptionMetadata struct {
	Name string `yaml:"name" json:"name"`
	ID   string `yaml:"id" json:"id"`
}

// ConfigFileName is the configuration file name
const ConfigFileName = ".gh-pmu.json"

// Load reads and parses a configuration file from the given path.
// Detects format (YAML or JSON) based on file extension.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if strings.HasSuffix(path, ".json") {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config file: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	return &cfg, nil
}

// LoadFromDirectory finds and loads the config file from the given directory.
// It searches up the directory tree until it finds a .gh-pmu.json file or
// reaches the filesystem root.
func LoadFromDirectory(dir string) (*Config, error) {
	configPath, err := FindConfigFile(dir)
	if err != nil {
		return nil, err
	}
	return Load(configPath)
}

// LoadFromDirectoryAndNormalize loads the config and normalizes the framework field.
// If the framework field is empty, it sets it to "IDPF" and saves the config.
// This ensures the config file is self-documenting about which framework is in use.
func LoadFromDirectoryAndNormalize(dir string) (*Config, error) {
	configPath, err := FindConfigFile(dir)
	if err != nil {
		return nil, err
	}

	cfg, err := Load(configPath)
	if err != nil {
		return nil, err
	}

	// Normalize: missing framework defaults to IDPF
	if cfg.Framework == "" {
		cfg.Framework = "IDPF"
		if err := cfg.Save(filepath.Dir(configPath)); err != nil {
			// Log warning but don't fail - config is still usable
			// The next save operation will include the framework
			return cfg, nil
		}
	}

	return cfg, nil
}

// FindConfigFile searches for .gh-pmu.json starting from dir and walking up
// the directory tree until found or filesystem root is reached.
func FindConfigFile(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	searchDir := dir
	for {
		configPath := filepath.Join(searchDir, ConfigFileName)
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}

		parent := filepath.Dir(searchDir)
		if parent == searchDir {
			break
		}
		searchDir = parent
	}

	return "", fmt.Errorf("no %s found in %s or any parent directory", ConfigFileName, startDir)
}

// Validate checks that required configuration fields are present
func (c *Config) Validate() error {
	if c.Project.Owner == "" {
		return fmt.Errorf("project.owner is required")
	}

	if c.Project.Number == 0 {
		return fmt.Errorf("project.number is required")
	}

	if len(c.Repositories) == 0 {
		return fmt.Errorf("at least one repository is required")
	}

	return nil
}

// HasResolvedView reports whether project.view holds a usable view number.
//
// GitHub view numbers are creation ordinals starting at 1, so zero means the
// field was absent (omitempty) and anything below zero means the config was
// hand-edited into an invalid state. Both are treated as unresolved rather than
// as a value, which keeps a bogus {projectUrl}/views/0 URL from being built (#901).
func (c *Config) HasResolvedView() bool {
	return c.Project.View >= 1
}

// ResolveFieldValue maps an alias to its actual GitHub field value.
// If no alias is found, returns the original value unchanged.
func (c *Config) ResolveFieldValue(fieldKey, alias string) string {
	field, ok := c.Fields[fieldKey]
	if !ok {
		return alias
	}

	if actual, ok := field.Values[alias]; ok {
		return actual
	}

	// Fall back to case-insensitive alias matching, consistent with
	// ValidateFieldValue, so a case-variant alias (e.g. "In_Progress") resolves to
	// the configured GitHub field value rather than passing through unchanged.
	for a, actual := range field.Values {
		if strings.EqualFold(a, alias) {
			return actual
		}
	}

	return alias
}

// ValidateFieldValue checks if the given value is a valid alias for the field.
// Returns an error listing available values if the value is not found.
// Returns nil if the field is not configured (allowing pass-through behavior).
func (c *Config) ValidateFieldValue(fieldKey, value string) error {
	field, ok := c.Fields[fieldKey]
	if !ok {
		// Field not configured, allow any value
		return nil
	}

	if len(field.Values) == 0 {
		// No values defined for field, allow any value
		return nil
	}

	// Check if value exists in the field's values map (case-insensitive)
	valueLower := strings.ToLower(value)
	for alias := range field.Values {
		if strings.ToLower(alias) == valueLower {
			return nil
		}
	}

	// Value not found, build error with available values
	var available []string
	for alias := range field.Values {
		available = append(available, alias)
	}

	return fmt.Errorf("invalid %s value %q\nAvailable values: %s", fieldKey, value, strings.Join(available, ", "))
}

// GetFieldName returns the actual GitHub field name for a given key.
// If no mapping exists, returns the original key unchanged.
func (c *Config) GetFieldName(fieldKey string) string {
	field, ok := c.Fields[fieldKey]
	if !ok {
		return fieldKey
	}

	if field.Field != "" {
		return field.Field
	}

	return fieldKey
}

// GetFieldNameOr returns the actual GitHub field name for a key, or the provided
// fallback when the key has no explicit, non-empty Field mapping. Unlike
// GetFieldName, callers control the default (e.g. "Status"/"Priority") rather than
// falling back to the lowercase key.
func (c *Config) GetFieldNameOr(fieldKey, fallback string) string {
	if field, ok := c.Fields[fieldKey]; ok && field.Field != "" {
		return field.Field
	}
	return fallback
}

// ApplyEnvOverrides applies environment variable overrides to the config.
// Supported environment variables:
//   - GH_PM_PROJECT_OWNER: overrides project.owner
//   - GH_PM_PROJECT_NUMBER: overrides project.number
func (c *Config) ApplyEnvOverrides() {
	if owner := os.Getenv("GH_PM_PROJECT_OWNER"); owner != "" {
		c.Project.Owner = owner
	}

	if numberStr := os.Getenv("GH_PM_PROJECT_NUMBER"); numberStr != "" {
		if number, err := strconv.Atoi(numberStr); err == nil {
			c.Project.Number = number
		} else {
			fmt.Fprintf(os.Stderr, "Warning: GH_PM_PROJECT_NUMBER=%q is not a valid number: %v\n", numberStr, err)
		}
	}
}

// Save writes the configuration to ConfigFileName inside dir.
//
// It takes a directory, not a file path. The previous signature accepted a full
// path and silently discarded the basename, so any caller passing a
// differently-named path had its data written elsewhere without warning (#874).
func (c *Config) Save(dir string) error {
	// Changing the parameter from a path to a directory is not compile-enforced
	// — both are strings — so catch the most likely stale call directly.
	if filepath.Base(dir) == ConfigFileName {
		return fmt.Errorf("Save expects the directory containing %s, got a file path: %s", ConfigFileName, dir)
	}

	jsonData, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON config: %w", err)
	}
	jsonData = append(jsonData, '\n')

	jsonPath := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(jsonPath, jsonData, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// RefreshVersion stamps the running version into the config in dir, recording
// which gh pmu version last wrote the file.
//
// The comparison runs on every call; the write does not. When the stored version
// already equals currentVersion the file is left untouched and false is returned.
// That matters because Save rewrites the whole document: an unconditional save
// would bump the file's mtime on every command and re-normalize line endings each
// time (Save writes LF; Windows checkouts normalize to CRLF), producing
// working-tree churn no user asked for.
//
// Only the top-level version field moves. acceptance.version is a separate field
// with its own re-acceptance gating and is not touched here.
func RefreshVersion(dir string, currentVersion string) (bool, error) {
	configPath := filepath.Join(dir, ConfigFileName)

	cfg, err := Load(configPath)
	if err != nil {
		return false, fmt.Errorf("failed to load config for version refresh: %w", err)
	}

	if cfg.Version == currentVersion {
		return false, nil
	}

	cfg.Version = currentVersion
	if err := cfg.Save(dir); err != nil {
		return false, fmt.Errorf("failed to save refreshed config: %w", err)
	}

	return true, nil
}

// IsIDPF returns true if the config uses IDPF framework validation.
// Returns true for any framework value starting with "IDPF" (case-insensitive),
// including "IDPF", "IDPF-Agile", "idpf", etc.
// IDPF is the default framework when not specified.
func (c *Config) IsIDPF() bool {
	return strings.HasPrefix(strings.ToUpper(c.Framework), "IDPF")
}

// AddFieldMetadata adds or updates field metadata in the config
func (c *Config) AddFieldMetadata(field FieldMetadata) {
	if c.Metadata == nil {
		c.Metadata = &Metadata{}
	}

	// Check if field already exists, update if so
	for i, f := range c.Metadata.Fields {
		if f.Name == field.Name {
			c.Metadata.Fields[i] = field
			return
		}
	}

	// Add new field
	c.Metadata.Fields = append(c.Metadata.Fields, field)
}

// TempDirName is the name of the temporary directory within the project root
const TempDirName = "tmp"

// GetProjectRoot returns the directory containing .gh-pmu.json.
// It searches from the current working directory up the directory tree.
func GetProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	configPath, err := FindConfigFile(cwd)
	if err != nil {
		return "", err
	}

	return filepath.Dir(configPath), nil
}

// GetTempDir returns the path to the project's tmp directory and creates it if needed.
// It also ensures tmp/ is in .gitignore.
func GetTempDir() (string, error) {
	projectRoot, err := GetProjectRoot()
	if err != nil {
		return "", err
	}

	tempDir := filepath.Join(projectRoot, TempDirName)

	// Create tmp directory if it doesn't exist
	if err := os.MkdirAll(tempDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Ensure tmp/ is in .gitignore
	if err := ensureGitignore(projectRoot); err != nil {
		// Log warning but don't fail - gitignore is nice-to-have
		fmt.Fprintf(os.Stderr, "Warning: could not update .gitignore: %v\n", err)
	}

	return tempDir, nil
}

// CreateTempFile creates a temporary file in the project's tmp directory.
// The pattern follows os.CreateTemp conventions (e.g., "prefix-*.suffix").
// The caller is responsible for closing and removing the file.
func CreateTempFile(pattern string) (*os.File, error) {
	tempDir, err := GetTempDir()
	if err != nil {
		return nil, err
	}

	return os.CreateTemp(tempDir, pattern)
}

// ensureGitignore adds tmp/ to .gitignore if not already present
func ensureGitignore(projectRoot string) error {
	gitignorePath := filepath.Join(projectRoot, ".gitignore")

	// Read existing content (single read, then close)
	existing, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read .gitignore: %w", err)
	}

	// Check if entry already present
	if len(existing) > 0 {
		scanner := bufio.NewScanner(strings.NewReader(string(existing)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == TempDirName || line == TempDirName+"/" {
				return nil // Already present
			}
		}
	}

	// Build content to append
	var content string
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		content = "\n"
	}
	content += TempDirName + "/\n"

	// Write (single handle, then close)
	file, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open .gitignore: %w", err)
	}
	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		return fmt.Errorf("failed to write to .gitignore: %w", err)
	}

	return nil
}
