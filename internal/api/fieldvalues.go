package api

import (
	"strconv"
	"strings"
)

// Shared extraction of ProjectV2 field values (#874).
//
// Six call sites in queries.go previously duplicated the __typename switch that
// turns ProjectV2ItemFieldValue union members into []FieldValue — three against
// the typed shurcooL-graphql selection and three against raw JSON. Their guards
// had drifted apart: some required a non-empty value, one required a non-empty
// field name, one required both, and one required neither consistently. That
// drift is the same class of divergence behind #856.
//
// These helpers collapse the switch to one place and normalize the guard: a
// node contributes a FieldValue only when it is a supported union member and
// both its field name and its value are non-empty. Dropping empty field names
// matters because the ProjectV2 field resolver can return rows whose name is
// null (see ErrFieldResolverUnavailable) — those would otherwise surface as
// FieldValue entries with an empty Field, silently unmatched by every caller
// that looks a field up by name.

const (
	typeSingleSelectValue = "ProjectV2ItemFieldSingleSelectValue"
	typeTextValue         = "ProjectV2ItemFieldTextValue"
	// typeIssueFieldValue is the value of an organization issue field the
	// project displays (#932). It carries the project field's name and the
	// issue field value, single-select or date.
	typeIssueFieldValue = "ProjectV2ItemIssueFieldValue"
)

// Members of the IssueFieldValue union whose rendering is not a plain string
// (#934). Single-select, date and text need no case: they render as their
// option name or value.
const (
	typeIssueFieldNumberValue      = "IssueFieldNumberValue"
	typeIssueFieldMultiSelectValue = "IssueFieldMultiSelectValue"
)

// issueFieldValueParts is one decoded IssueFieldValue, from either the typed or
// the raw selection.
type issueFieldValueParts struct {
	TypeName string
	Name     string   // single-select option name
	Value    string   // date (YYYY-MM-DD) or text
	Number   *float64 // number
	Options  []string // multi-select option names
}

// text renders an issue field value: a number without a trailing .0, a
// multi-select as its comma-joined option names, a single-select as its option
// name, otherwise its value (a date's YYYY-MM-DD or a text).
func (p issueFieldValueParts) text() string {
	switch p.TypeName {
	case typeIssueFieldNumberValue:
		if p.Number == nil {
			return ""
		}
		return strconv.FormatFloat(*p.Number, 'f', -1, 64)
	case typeIssueFieldMultiSelectValue:
		return strings.Join(p.Options, ", ")
	}
	if p.Name != "" {
		return p.Name
	}
	return p.Value
}

// fieldOptionNames returns the option names of a multi-select value.
func fieldOptionNames(options []struct{ Name string }) []string {
	names := make([]string, 0, len(options))
	for _, o := range options {
		names = append(names, o.Name)
	}
	return names
}

// typedFieldValueNode mirrors the ProjectV2ItemFieldValue union selection used
// by the typed project-item queries. It is a named type so the three typed call
// sites share one definition rather than three identical anonymous structs.
type typedFieldValueNode struct {
	TypeName                            string `graphql:"__typename"`
	ProjectV2ItemFieldSingleSelectValue struct {
		Name  string
		Field struct {
			ProjectV2SingleSelectField struct {
				Name string
			} `graphql:"... on ProjectV2SingleSelectField"`
		}
	} `graphql:"... on ProjectV2ItemFieldSingleSelectValue"`
	ProjectV2ItemFieldTextValue struct {
		Text  string
		Field struct {
			ProjectV2Field struct {
				Name string
			} `graphql:"... on ProjectV2Field"`
		}
	} `graphql:"... on ProjectV2ItemFieldTextValue"`
	ProjectV2ItemIssueFieldValue struct {
		Field struct {
			Common struct {
				Name string
			} `graphql:"... on ProjectV2FieldCommon"`
		}
		// Number's value is aliased: `value` is String! on date and text,
		// Float! on number and nullable String on multi-select, and
		// GraphQL's FieldsInSetCanMerge rule rejects the unaliased set, which
		// would fail every query sharing this selection (#934).
		IssueFieldValue struct {
			TypeName                    string `graphql:"__typename"`
			IssueFieldSingleSelectValue struct {
				Name string
			} `graphql:"... on IssueFieldSingleSelectValue"`
			IssueFieldDateValue struct {
				Value string
			} `graphql:"... on IssueFieldDateValue"`
			IssueFieldTextValue struct {
				Value string
			} `graphql:"... on IssueFieldTextValue"`
			IssueFieldNumberValue struct {
				NumberValue float64 `graphql:"numberValue: value"`
			} `graphql:"... on IssueFieldNumberValue"`
			IssueFieldMultiSelectValue struct {
				Options []struct{ Name string }
			} `graphql:"... on IssueFieldMultiSelectValue"`
		}
	} `graphql:"... on ProjectV2ItemIssueFieldValue"`
}

// rawIssueFieldValueSelection is the ProjectV2ItemIssueFieldValue member of the
// field-value union for the raw-JSON queries, decoded by rawFieldValueNode.
// numberValue is aliased for the same FieldsInSetCanMerge reason as the typed
// selection.
const rawIssueFieldValueSelection = `... on ProjectV2ItemIssueFieldValue {
									field { ... on ProjectV2FieldCommon { name } }
									issueFieldValue {
										__typename
										... on IssueFieldSingleSelectValue { name }
										... on IssueFieldDateValue { value }
										... on IssueFieldTextValue { value }
										... on IssueFieldNumberValue { numberValue: value }
										... on IssueFieldMultiSelectValue { options { name } }
									}
								}`

// rawFieldValueNode mirrors the same union as decoded from raw JSON, where the
// aliased selection flattens every member's name/text onto one object.
type rawFieldValueNode struct {
	TypeName string `json:"__typename"`
	Name     string `json:"name"`
	Text     string `json:"text"`
	Field    struct {
		Name string `json:"name"`
	} `json:"field"`
	IssueFieldValue struct {
		TypeName    string                  `json:"__typename"`
		Name        string                  `json:"name"`
		Value       string                  `json:"value"`
		NumberValue *float64                `json:"numberValue"`
		Options     []struct{ Name string } `json:"options"`
	} `json:"issueFieldValue"`
}

// newFieldValue normalizes one union member into a FieldValue. ok is false when
// typeName is not a supported member, or when either fieldName or value is
// empty.
func newFieldValue(typeName, fieldName, value string) (FieldValue, bool) {
	switch typeName {
	case typeSingleSelectValue, typeTextValue, typeIssueFieldValue:
	default:
		return FieldValue{}, false
	}
	if fieldName == "" || value == "" {
		return FieldValue{}, false
	}
	return FieldValue{Field: fieldName, Value: value}, true
}

// appendTypedFieldValues appends every extractable field value in nodes to dst.
func appendTypedFieldValues(dst []FieldValue, nodes []typedFieldValueNode) []FieldValue {
	for _, n := range nodes {
		var fieldName, value string
		switch n.TypeName {
		case typeSingleSelectValue:
			fieldName = n.ProjectV2ItemFieldSingleSelectValue.Field.ProjectV2SingleSelectField.Name
			value = n.ProjectV2ItemFieldSingleSelectValue.Name
		case typeTextValue:
			fieldName = n.ProjectV2ItemFieldTextValue.Field.ProjectV2Field.Name
			value = n.ProjectV2ItemFieldTextValue.Text
		case typeIssueFieldValue:
			v := n.ProjectV2ItemIssueFieldValue
			fieldName = v.Field.Common.Name
			ifv := v.IssueFieldValue
			// Date and text both select `value`, so the decoder fills both
			// fragments from the one JSON key; either holds the value.
			dateOrText := ifv.IssueFieldDateValue.Value
			if dateOrText == "" {
				dateOrText = ifv.IssueFieldTextValue.Value
			}
			value = issueFieldValueParts{
				TypeName: ifv.TypeName,
				Name:     ifv.IssueFieldSingleSelectValue.Name,
				Value:    dateOrText,
				Number:   &ifv.IssueFieldNumberValue.NumberValue,
				Options:  fieldOptionNames(ifv.IssueFieldMultiSelectValue.Options),
			}.text()
		}
		if fv, ok := newFieldValue(n.TypeName, fieldName, value); ok {
			dst = append(dst, fv)
		}
	}
	return dst
}

// appendRawFieldValues appends every extractable field value in nodes to dst.
func appendRawFieldValues(dst []FieldValue, nodes []rawFieldValueNode) []FieldValue {
	for _, n := range nodes {
		var value string
		switch n.TypeName {
		case typeSingleSelectValue:
			value = n.Name
		case typeTextValue:
			value = n.Text
		case typeIssueFieldValue:
			ifv := n.IssueFieldValue
			value = issueFieldValueParts{
				TypeName: ifv.TypeName,
				Name:     ifv.Name,
				Value:    ifv.Value,
				Number:   ifv.NumberValue,
				Options:  fieldOptionNames(ifv.Options),
			}.text()
		}
		if fv, ok := newFieldValue(n.TypeName, n.Field.Name, value); ok {
			dst = append(dst, fv)
		}
	}
	return dst
}
