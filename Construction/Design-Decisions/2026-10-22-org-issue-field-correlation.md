# Design Decision: Correlating project fields with organization issue fields

**Date:** 2026-10-22
**Status:** Accepted
**Context:** Issue #932 — Handle organization-level issue fields (e.g. Priority) alongside project fields

## Decision

Each project field is classified individually as an ordinary ProjectV2 field or a display of an organization issue field, using the first rule that applies:

1. **Override.** `fields.<alias>.issue_field` in `.gh-pmu.json` either pins the field to an issue field ID, or keeps it a project field when set to `none`.
2. **Item-value pairing.** A `ProjectV2ItemIssueFieldValue` carries both the project field ID and the issue field ID, and is authoritative. Discovery samples the first 100 project items for such pairings.
3. **Name and data type.** An org issue field with the same name and data type matches. For a single-select, it matches only when the project field has no options of its own. Multi-select issue fields are never matched this way.

The org `issueFields` query runs only when the project is organization-owned. Owner type gates the query but never classifies a field.

An issue-backed field is written through `setIssueFieldValue`. That mutation is keyed on the Issue node ID, which is resolved from the project item first. Ordinary fields keep `updateProjectV2ItemFieldValue`.

## Rationale

The ProjectV2 fields API returns an issue-backed field as an ordinary, option-less `PVTSSF_` or `PVTF_` field. The schema has no link from that project field to its `IFSS_` or `IFD_` issue field: `ProjectV2FieldConfiguration` has no issue-field member. The only place both IDs appear together is an item's `ProjectV2ItemIssueFieldValue`, and it exists only once some item has a value. So the classification has to correlate several sources:
- Pairing is exact where it exists.
- Name plus type covers boards where no item has a value yet.
- The override covers the case where name plus type gets it wrong.

## Alternatives Considered

- **Match by ID prefix (`IFSS_` vs `PVTSSF_`).** This was the first proposal in #932. Rejected because the project API never returns an `IFSS_` ID for a project field, so the prefix cannot tell the two kinds apart.
- **Classify by owner type (org board means issue field).** Rejected because an org board can hold an ordinary Priority project field, and a mixed org board has both kinds.
- **Paginate every project item for pairings.** Rejected because discovery runs on most commands, and a full item scan multiplies its cost on large boards. A bounded sample plus the name-and-type fallback covers the case.
- **Fall back to the project-field path when `organization.issueFields` is denied.** Rejected because silently dropping Priority is the defect #932 fixes. A denied query now fails discovery, naming the query and GitHub's message.

## Consequences

- Org boards make two extra queries per field discovery: `organization.issueFields`, paginated, and the item-value pairing sample. User-owned boards make neither.
- A token without read access to the org's issue fields can no longer run field-setting commands against an org board until that access is granted. This is fail-loud by design.
- If a native project field shares a name and type with an org issue field and no item carries a pairing, it is misclassified until an override is set.
- Issue field option IDs are cached in `.gh-pmu.json` for the read fallback only. Writes never use cached issue field metadata (#917). A cache written before this change is refused, with `gh pmu init` as the remedy.
- Only SINGLE_SELECT and DATE issue fields can be written. MULTI_SELECT, NUMBER and TEXT fail with the kind named.

## Issues Encountered

- `IssueFieldCommon` does not expose `id`, so issue field IDs are read through `... on Node`. Item values reach their issue field through `IssueFieldValueCommon.field`.
- No live org board was exercised. The response shapes come from the vendored schema (`testdata/graphql/schema.docs.graphql`), and every new document passes the offline schema validation.

---
*Documented during completion of #932*
