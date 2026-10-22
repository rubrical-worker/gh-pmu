package api

import (
	"fmt"
	"os"
	"strings"
	"time"

	graphql "github.com/cli/shurcooL-graphql"

	"github.com/rubrical-works/gh-pmu/internal/config"
)

// issue_fields.go: organization-level issue fields (#932).
//
// An org can define issue fields (Priority, Effort, Start date, …) that a
// project merely displays. The ProjectV2 fields API returns such a field as an
// ordinary PVTSSF_/PVTF_ field — a single-select with no options — and the
// schema exposes no link from that project field to the issue field behind it.
// So each project field is classified by correlating two sources:
//
//  1. a .gh-pmu.json override (fields.<alias>.issue_field) pins or excludes it;
//  2. otherwise an item value pairing — a ProjectV2ItemIssueFieldValue carries
//     both the project field ID and the issue field ID — is authoritative;
//  3. otherwise an org issue field with the same name and data type matches,
//     and for single-select only when the project field has no options of its
//     own (an org board may hold an ordinary Priority project field).
//
// Owner type alone never selects the issue field path: it only decides whether
// the org issueFields query runs at all.

// IssueFieldOverrideNone, as a fields.<alias>.issue_field value, keeps a field
// on the project-field path even when an org issue field would match it.
const IssueFieldOverrideNone = "none"

// issueFieldPairsItemSample bounds the item-value pairing query. Pairing only
// confirms or corrects the name+type match, so a sample is enough; fields with
// no value on any sampled item fall through to the name+type rule.
const issueFieldPairsItemSample = 100

// orgIssueField is one organization issue field definition.
type orgIssueField struct {
	ID       string
	Name     string
	DataType string
	Options  []FieldOption
}

// normalizeIssueFieldDataType maps IssueFieldDataType values to the ProjectV2
// spelling ("Single Select" / "SINGLE_SELECT" -> "SINGLE_SELECT").
func normalizeIssueFieldDataType(s string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), " ", "_"))
}

// issueFieldOverridesLoader returns project field name -> override value
// (an issue field ID, or IssueFieldOverrideNone).
var issueFieldOverridesLoader = defaultIssueFieldOverridesLoader

func defaultIssueFieldOverridesLoader() (map[string]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil
	}
	cfg, err := config.LoadFromDirectory(cwd)
	if err != nil || cfg == nil {
		// No readable config means no overrides; config errors surface
		// through the command's own config load.
		return nil, nil
	}
	overrides := map[string]string{}
	for _, f := range cfg.Fields {
		if f.Field != "" && f.IssueField != "" {
			overrides[f.Field] = f.IssueField
		}
	}
	return overrides, nil
}

// SetIssueFieldOverridesLoaderForTesting overrides the loader. Returns a
// defer-friendly restore function.
func SetIssueFieldOverridesLoaderForTesting(fn func() (map[string]string, error)) func() {
	prev := issueFieldOverridesLoader
	issueFieldOverridesLoader = fn
	return func() { issueFieldOverridesLoader = prev }
}

// classifyIssueFields marks each project field backed by an org issue field.
func (c *Client) classifyIssueFields(projectID, orgLogin string, fields []ProjectField) ([]ProjectField, error) {
	issueFields, err := c.fetchOrgIssueFields(orgLogin)
	if err != nil {
		return nil, err
	}
	if len(issueFields) == 0 {
		return fields, nil
	}

	pairs, err := c.fetchIssueFieldPairs(projectID)
	if err != nil {
		return nil, err
	}

	overrides, err := issueFieldOverridesLoader()
	if err != nil {
		return nil, err
	}

	return correlateIssueFields(fields, issueFields, pairs, overrides)
}

// correlateIssueFields applies the override → pairing → name+type rule. Pure,
// so the rule is testable without GraphQL.
func correlateIssueFields(fields []ProjectField, issueFields []orgIssueField, pairs map[string]string, overrides map[string]string) ([]ProjectField, error) {
	byID := make(map[string]orgIssueField, len(issueFields))
	for _, f := range issueFields {
		byID[f.ID] = f
	}

	out := make([]ProjectField, len(fields))
	copy(out, fields)
	for i := range out {
		f := &out[i]

		var match *orgIssueField
		if ov, ok := overrides[f.Name]; ok {
			if ov == IssueFieldOverrideNone {
				continue
			}
			target, found := byID[ov]
			if !found {
				return nil, fmt.Errorf("field %q is pinned to issue field %q in .gh-pmu.json, but the organization has no such issue field", f.Name, ov)
			}
			match = &target
		} else if id, ok := pairs[f.ID]; ok {
			if target, found := byID[id]; found {
				match = &target
			}
		} else {
			match = nameTypeMatch(*f, issueFields)
		}

		if match == nil {
			continue
		}
		f.IssueField = &IssueFieldInfo{ID: match.ID, DataType: match.DataType}
		if match.DataType == "SINGLE_SELECT" {
			f.Options = append([]FieldOption(nil), match.Options...)
		}
	}
	return out, nil
}

// nameTypeMatch returns the org issue field with f's name and data type, or nil.
// A project single-select that has options of its own is an ordinary project
// field. Multi-select issue fields have no ProjectV2 data type to compare
// against, so they are only ever matched by pairing or override.
func nameTypeMatch(f ProjectField, issueFields []orgIssueField) *orgIssueField {
	if f.DataType == "SINGLE_SELECT" && len(f.Options) > 0 {
		return nil
	}
	for i := range issueFields {
		candidate := issueFields[i]
		if candidate.Name == f.Name && candidate.DataType == f.DataType && candidate.DataType != "MULTI_SELECT" {
			return &candidate
		}
	}
	return nil
}

// fetchOrgIssueFields returns every issue field the organization defines,
// paginating to completion. A failure — typically a token without access to
// the org's issue fields — is returned, never treated as "no issue fields":
// silently falling back to the project-field path is what drops Priority.
func (c *Client) fetchOrgIssueFields(orgLogin string) ([]orgIssueField, error) {
	var all []orgIssueField
	var cursor *string
	for {
		var query struct {
			Organization struct {
				IssueFields struct {
					Nodes []struct {
						TypeName string `graphql:"__typename"`
						Common   struct {
							Name     string
							DataType string
						} `graphql:"... on IssueFieldCommon"`
						Node struct {
							ID string
						} `graphql:"... on Node"`
						IssueFieldSingleSelect struct {
							Options []struct {
								ID   string
								Name string
							}
						} `graphql:"... on IssueFieldSingleSelect"`
					}
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
				} `graphql:"issueFields(first: 100, after: $cursor)"`
			} `graphql:"organization(login: $login)"`
		}

		variables := map[string]interface{}{
			"login":  graphql.String(orgLogin),
			"cursor": (*graphql.String)(nil),
		}
		if cursor != nil {
			variables["cursor"] = graphql.String(*cursor)
		}

		if err := c.gql.Query("GetOrganizationIssueFields", &query, variables); err != nil {
			return nil, fmt.Errorf("failed to query organization.issueFields for %q (the token needs read access to the organization's issue fields): %w", orgLogin, err)
		}

		for _, n := range query.Organization.IssueFields.Nodes {
			f := orgIssueField{
				ID:       n.Node.ID,
				Name:     n.Common.Name,
				DataType: normalizeIssueFieldDataType(n.Common.DataType),
			}
			for _, opt := range n.IssueFieldSingleSelect.Options {
				f.Options = append(f.Options, FieldOption{ID: opt.ID, Name: opt.Name})
			}
			all = append(all, f)
		}

		if !query.Organization.IssueFields.PageInfo.HasNextPage {
			break
		}
		next := query.Organization.IssueFields.PageInfo.EndCursor
		cursor = &next
	}
	return all, nil
}

// fetchIssueFieldPairs samples project items for issue field values and
// returns project field ID -> issue field ID for every pairing seen.
func (c *Client) fetchIssueFieldPairs(projectID string) (map[string]string, error) {
	var query struct {
		Node struct {
			ProjectV2 struct {
				Items struct {
					Nodes []struct {
						FieldValues struct {
							Nodes []struct {
								TypeName                     string `graphql:"__typename"`
								ProjectV2ItemIssueFieldValue struct {
									Field struct {
										Common struct {
											ID string
										} `graphql:"... on ProjectV2FieldCommon"`
									}
									IssueFieldValue struct {
										Common struct {
											Field struct {
												Node struct {
													ID string
												} `graphql:"... on Node"`
											}
										} `graphql:"... on IssueFieldValueCommon"`
									}
								} `graphql:"... on ProjectV2ItemIssueFieldValue"`
							}
						} `graphql:"fieldValues(first: 50)"`
					}
				} `graphql:"items(first: $first)"`
			} `graphql:"... on ProjectV2"`
		} `graphql:"node(id: $projectId)"`
	}

	variables := map[string]interface{}{
		"projectId": graphql.ID(projectID),
		"first":     graphql.Int(issueFieldPairsItemSample),
	}
	if err := c.gql.Query("GetProjectIssueFieldPairs", &query, variables); err != nil {
		return nil, fmt.Errorf("failed to read issue field values on project items: %w", err)
	}

	pairs := map[string]string{}
	for _, item := range query.Node.ProjectV2.Items.Nodes {
		for _, v := range item.FieldValues.Nodes {
			if v.TypeName != "ProjectV2ItemIssueFieldValue" {
				continue
			}
			projectFieldID := v.ProjectV2ItemIssueFieldValue.Field.Common.ID
			issueFieldID := v.ProjectV2ItemIssueFieldValue.IssueFieldValue.Common.Field.Node.ID
			if projectFieldID != "" && issueFieldID != "" {
				pairs[projectFieldID] = issueFieldID
			}
		}
	}
	return pairs, nil
}

// SetIssueFieldValueInput is the input of the setIssueFieldValue mutation.
// The Go type name is the GraphQL input type name the mutation document uses.
type SetIssueFieldValueInput struct {
	IssueID     graphql.ID                      `json:"issueId"`
	IssueFields []IssueFieldCreateOrUpdateInput `json:"issueFields"`
}

// IssueFieldCreateOrUpdateInput sets or deletes one issue field value.
type IssueFieldCreateOrUpdateInput struct {
	FieldID              graphql.ID `json:"fieldId"`
	SingleSelectOptionID string     `json:"singleSelectOptionId,omitempty"`
	DateValue            string     `json:"dateValue,omitempty"`
	Delete               bool       `json:"delete,omitempty"`
}

// issueFieldEntry resolves value into the setIssueFieldValue entry for an
// issue-backed field, or returns why it cannot be written. Nothing is sent
// when this fails.
func issueFieldEntry(field *ProjectField, value string) (IssueFieldCreateOrUpdateInput, error) {
	entry := IssueFieldCreateOrUpdateInput{FieldID: graphql.ID(field.IssueField.ID)}

	// Cached metadata may be stale and must never drive a mutation (#917);
	// issue field option IDs come only from a live organization.issueFields
	// query.
	if field.FromCache {
		return entry, fmt.Errorf("field %q is an organization issue field, and its option IDs came from cached .gh-pmu.json metadata because the live field query was unavailable; cached metadata cannot drive a write — retry when GitHub is reachable", field.Name)
	}

	switch field.IssueField.DataType {
	case "SINGLE_SELECT":
		if value == "" {
			entry.Delete = true
			return entry, nil
		}
		for _, opt := range field.Options {
			if opt.Name == value {
				entry.SingleSelectOptionID = opt.ID
				return entry, nil
			}
		}
		return entry, fmt.Errorf("option %q not found for field %q", value, field.Name)
	case "DATE":
		if value == "" {
			entry.Delete = true
			return entry, nil
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return entry, fmt.Errorf("invalid date format for field %q (expected YYYY-MM-DD): %s", field.Name, value)
		}
		entry.DateValue = value
		return entry, nil
	default:
		return entry, fmt.Errorf("field %q is backed by an organization issue field of kind %s, which gh pmu cannot set (supported: SINGLE_SELECT, DATE)", field.Name, field.IssueField.DataType)
	}
}

// setIssueFieldValue writes an issue-backed field through setIssueFieldValue.
// That mutation is keyed on the Issue node ID, so the project item's content
// ID is resolved first.
func (c *Client) setIssueFieldValue(itemID string, field *ProjectField, value string) error {
	entry, err := issueFieldEntry(field, value)
	if err != nil {
		return err
	}
	issueID, err := c.issueIDForProjectItem(itemID)
	if err != nil {
		return err
	}
	return c.mutateIssueFieldValue(issueID, entry, field.Name)
}

func (c *Client) mutateIssueFieldValue(issueID string, entry IssueFieldCreateOrUpdateInput, fieldName string) error {
	var mutation struct {
		SetIssueFieldValue struct {
			ClientMutationID string `graphql:"clientMutationId"`
		} `graphql:"setIssueFieldValue(input: $input)"`
	}
	input := SetIssueFieldValueInput{
		IssueID:     graphql.ID(issueID),
		IssueFields: []IssueFieldCreateOrUpdateInput{entry},
	}
	if err := c.gql.Mutate("SetIssueFieldValue", &mutation, map[string]interface{}{"input": input}); err != nil {
		return fmt.Errorf("failed to set issue field %q: %w", fieldName, err)
	}
	return nil
}

// issueIDForProjectItem returns the Issue node ID behind a project item.
func (c *Client) issueIDForProjectItem(itemID string) (string, error) {
	var query struct {
		Node struct {
			ProjectV2Item struct {
				Content struct {
					TypeName string `graphql:"__typename"`
					Issue    struct {
						ID string
					} `graphql:"... on Issue"`
				}
			} `graphql:"... on ProjectV2Item"`
		} `graphql:"node(id: $itemId)"`
	}
	if err := c.gql.Query("GetProjectItemContent", &query, map[string]interface{}{"itemId": graphql.ID(itemID)}); err != nil {
		return "", fmt.Errorf("failed to resolve the issue behind project item %s: %w", itemID, err)
	}
	content := query.Node.ProjectV2Item.Content
	if content.TypeName != "Issue" || content.Issue.ID == "" {
		return "", fmt.Errorf("project item %s is not an issue (content type %q); organization issue fields apply only to issues", itemID, content.TypeName)
	}
	return content.Issue.ID, nil
}
