// Package defaults provides embedded default configuration for gh-pmu.
package defaults

import (
	_ "embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed defaults.yml
var defaultsYAML []byte

//go:embed terms.txt
var termsText string

// Defaults holds the parsed default configuration.
type Defaults struct {
	Labels []LabelDef `yaml:"labels"`
	Fields FieldsDef  `yaml:"fields"`
}

// LabelDef represents a label definition.
type LabelDef struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Color       string `yaml:"color"`
}

// FieldsDef holds field definitions separated by requirement level.
type FieldsDef struct {
	Required        []FieldDef `yaml:"required"`
	CreateIfMissing []FieldDef `yaml:"create_if_missing"`
}

// FieldDef represents a project field definition.
type FieldDef struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	// Options holds the option names, in declaration order.
	Options []string `yaml:"-"`
	// OptionDefs holds each option's full definition, parallel to Options.
	// Color and Description are empty for options declared as bare names.
	OptionDefs []OptionDef `yaml:"-"`
}

// OptionDef is a single-select option definition.
type OptionDef struct {
	Name        string `yaml:"name"`
	Color       string `yaml:"color"`
	Description string `yaml:"description"`
}

// UnmarshalYAML accepts each option as either a bare name (`- P0`) or a
// {name, color, description} mapping, so fields that only need names keep the
// short form (#917).
func (f *FieldDef) UnmarshalYAML(node *yaml.Node) error {
	var raw struct {
		Name    string      `yaml:"name"`
		Type    string      `yaml:"type"`
		Options []yaml.Node `yaml:"options"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}

	f.Name, f.Type = raw.Name, raw.Type
	f.Options, f.OptionDefs = nil, nil
	for i := range raw.Options {
		opt := &raw.Options[i]
		var def OptionDef
		switch opt.Kind {
		case yaml.ScalarNode:
			def.Name = opt.Value
		case yaml.MappingNode:
			if err := opt.Decode(&def); err != nil {
				return err
			}
		default:
			return fmt.Errorf("field %q option %d: expected a name or a {name, color, description} mapping", raw.Name, i)
		}
		f.Options = append(f.Options, def.Name)
		f.OptionDefs = append(f.OptionDefs, def)
	}
	return nil
}

// OptionDef returns the definition of the named option, matched
// case-insensitively, or nil when the field declares no such option.
func (f *FieldDef) OptionDef(name string) *OptionDef {
	for i := range f.OptionDefs {
		if strings.EqualFold(f.OptionDefs[i].Name, name) {
			return &f.OptionDefs[i]
		}
	}
	return nil
}

// Load parses and returns the embedded defaults.
func Load() (*Defaults, error) {
	var d Defaults
	if err := yaml.Unmarshal(defaultsYAML, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// MustLoad parses and returns the embedded defaults, panicking on error.
func MustLoad() *Defaults {
	d, err := Load()
	if err != nil {
		panic("failed to load embedded defaults: " + err.Error())
	}
	return d
}

// GetLabel returns the label definition for a given name, or nil if not found.
func (d *Defaults) GetLabel(name string) *LabelDef {
	for i := range d.Labels {
		if d.Labels[i].Name == name {
			return &d.Labels[i]
		}
	}
	return nil
}

// GetLabelNames returns a slice of all standard label names.
func (d *Defaults) GetLabelNames() []string {
	names := make([]string, len(d.Labels))
	for i := range d.Labels {
		names[i] = d.Labels[i].Name
	}
	return names
}

// IsStandardLabel returns true if the given label name is a standard label.
func (d *Defaults) IsStandardLabel(name string) bool {
	return d.GetLabel(name) != nil
}

// Terms returns the embedded terms and conditions text.
func Terms() string {
	return termsText
}
