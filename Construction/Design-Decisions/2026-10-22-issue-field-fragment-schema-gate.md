# Design Decision: Dropping the issue field fragment when the schema rejects it

**Date:** 2026-10-22
**Status:** Accepted
**Context:** Issue #935 — Gate the issue field value fragment on schema support

## Decision

Every project item and field-value read selects `... on ProjectV2ItemIssueFieldValue` (#932). When a query fails with a GraphQL error that names that type, or any `IssueField*Value` type, the client does three things:
- It records the server as unsupported for the rest of its life, which is one CLI invocation.
- It prints one warning.
- It re-runs that query once without the fragment.

Later reads on the same client omit the fragment up front. This is `Client.issueFieldsRejected` in `internal/api/fieldvalues.go`.

The typed (shurcooL) queries get their GraphQL document from the Go struct, so leaving the fragment out means using a different node type:
- `projectFieldValueNode` holds the project-field union members.
- `typedFieldValueNode` embeds it and adds the issue field member.
- Each typed query is a generic `…As[N typedFieldValue]` function. Its method wrapper instantiates it with the full node first and with the project-only node after a rejection.

Raw queries take their selection from `Client.issueFieldValueSelection()`, which returns an empty string once the client is marked unsupported.

## Rationale

The trigger is what the server actually rejects, not a guess about which servers lack the type. On github.com the first query succeeds, so there are no extra round trips and nothing changes there. On a GitHub Enterprise Server without the issue fields rollout, a command makes exactly one extra call and then runs as it did before #932.

## Alternatives Considered

- **Gate on org ownership plus a capability probe.** Rejected in #935's review. Org-repo issues placed on a user-owned project would lose their issue field values on github.com. The probe also costs a round trip on every invocation, even on servers that support issue fields.
- **`@include(if: $issueFields)` on the fragment.** Rejected because GraphQL validation resolves the fragment's type condition no matter what the directive says, so a server without the type still rejects the document.
- **Two hand-written copies of each typed query.** Rejected because it duplicates four large query structs. Generics instantiate one struct twice.

## Consequences

- github.com reads are unchanged, and the existing #932 read tests pass as they did.
- On a server without issue fields, the first read in a command pays one rejected call, and the user sees one warning per command.
- Detection depends on the error naming the type. A server that rejects the fragment with a message that names no `IssueField*` type would still fail loudly, as it did before this change. It would not fail silently.
- `appendTypedFieldValues` is generic now. A call with an untyped `nil` slice needs an explicit type argument.

## Issues Encountered

- shurcooL decodes every inline fragment that shares a JSON key, so the date and text `value` fragments are both filled from one key (#934). The decoder takes whichever one is set rather than concatenating them.
- shurcooL's decoder rejects JSON keys a query did not select, so the test fixtures give each typed query (view, list, minimal list, board) its own item shape.

---
*Documented during completion of #935*
