# Standard Labels

Labels managed by `gh pmu init` from `internal/defaults/defaults.yml`.

## Color Audit

**Duplicates found:** 3 exact collisions, 3 near-clashes across 23 labels.

### Issue Types

<table>
<tr><th>Label</th><th>Current</th><th>Proposed</th><th>Notes</th><th>Description</th></tr>
<tr>
  <td><code>bug</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#d73a4a"/></svg> <code>#d73a4a</code></td>
  <td>—</td>
  <td>Keep. Distinct red for defects.</td>
  <td>Something isn't working</td>
</tr>
<tr>
  <td><code>enhancement</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#a2eeef"/></svg> <code>#a2eeef</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#1d76db"/></svg> <code>#1d76db</code></td>
  <td>Duplicate of <code>story</code>. Change to medium blue — GitHub's default enhancement color.</td>
  <td>New feature or request</td>
</tr>
<tr>
  <td><code>epic</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#3fb950"/></svg> <code>#3fb950</code></td>
  <td>—</td>
  <td>Keep. Bright green distinguishes parent issues.</td>
  <td>Parent issue with sub-issues</td>
</tr>
<tr>
  <td><code>story</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#a2eeef"/></svg> <code>#a2eeef</code></td>
  <td>—</td>
  <td>Keep. Light cyan for child issues (now distinct from enhancement).</td>
  <td>Child issue under an epic</td>
</tr>
<tr>
  <td><code>proposal</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#d4c5f9"/></svg> <code>#d4c5f9</code></td>
  <td>—</td>
  <td>Keep. Light purple — unique, fits "idea" concept.</td>
  <td>Idea awaiting implementation</td>
</tr>
<tr>
  <td><code>prd</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#f9d0c4"/></svg> <code>#f9d0c4</code></td>
  <td>—</td>
  <td>Keep. Light salmon — unique, pairs with proposal.</td>
  <td>Product Requirements Document</td>
</tr>
</table>

### Workflow State

<table>
<tr><th>Label</th><th>Current</th><th>Proposed</th><th>Notes</th><th>Description</th></tr>
<tr>
  <td><code>branch</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#0e8a16"/></svg> <code>#0e8a16</code></td>
  <td>—</td>
  <td>Keep. Dark green — distinct enough from <code>reviewed</code>.</td>
  <td>Branch tracker issue</td>
</tr>
<tr>
  <td><code>active</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#7B61FF"/></svg> <code>#7B61FF</code></td>
  <td>—</td>
  <td>Keep. Violet — unique in the palette.</td>
  <td>Currently active working branch</td>
</tr>
<tr>
  <td><code>assigned</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#FBCA04"/></svg> <code>#FBCA04</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#bfd4f2"/></svg> <code>#bfd4f2</code></td>
  <td>Duplicate of <code>qa-required</code> and <code>approval-required</code>. Change to light steel blue — neutral "tagged" feel.</td>
  <td>Issue is assigned to a branch</td>
</tr>
<tr>
  <td><code>reviewed</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#2EA44F"/></svg> <code>#2EA44F</code></td>
  <td>—</td>
  <td>Keep. GitHub-green for "done/resolved" — close to <code>branch</code> but different enough.</td>
  <td>Issue has been reviewed and findings resolved</td>
</tr>
<tr>
  <td><code>auto-filed</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#116329"/></svg> <code>#116329</code></td>
  <td>—</td>
  <td>New (#915). Dark green, expected to render with white text — darker than <code>branch</code> and <code>reviewed</code>; no other label uses it.</td>
  <td>Issue filed by the hall-monitor</td>
</tr>
<tr>
  <td><code>pending</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#D93F0B"/></svg> <code>#D93F0B</code></td>
  <td>—</td>
  <td>Keep. Orange-red for "unresolved" — pairs with <code>reviewed</code>.</td>
  <td>Review findings not yet resolved</td>
</tr>
</table>

### Gates / Requirements

<table>
<tr><th>Label</th><th>Current</th><th>Proposed</th><th>Notes</th><th>Description</th></tr>
<tr>
  <td><code>qa-required</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#fbca04"/></svg> <code>#fbca04</code></td>
  <td>—</td>
  <td>Keep. Yellow "gate" signal. The canonical holder of this color.</td>
  <td>Requires QA validation</td>
</tr>
<tr>
  <td><code>test-plan</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#c5def5"/></svg> <code>#c5def5</code></td>
  <td>—</td>
  <td>Keep. Light blue — unique, "documentation" feel.</td>
  <td>Issue includes or requires test plan</td>
</tr>
<tr>
  <td><code>security-required</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#b60205"/></svg> <code>#b60205</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#d93f0b"/></svg> <code>#d93f0b</code></td>
  <td>Duplicate of <code>emergency</code>. Change to dark orange — still urgent but distinct from emergency's red. Near-match with <code>pending</code> is acceptable since they're in different groups.</td>
  <td>Requires security review</td>
</tr>
<tr>
  <td><code>legal-required</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#5319e7"/></svg> <code>#5319e7</code></td>
  <td>—</td>
  <td>Keep. Deep purple — unique, authoritative feel.</td>
  <td>Requires legal review</td>
</tr>
<tr>
  <td><code>docs-required</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#0052cc"/></svg> <code>#0052cc</code></td>
  <td>—</td>
  <td>Keep. Dark blue — unique.</td>
  <td>Requires documentation updates</td>
</tr>
<tr>
  <td><code>approval-required</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#fbca04"/></svg> <code>#fbca04</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#e4e669"/></svg> <code>#e4e669</code></td>
  <td>Duplicate of <code>qa-required</code>. Change to yellow-green — still "gate" family but distinguishable.</td>
  <td>Requires explicit approval before proceeding</td>
</tr>
</table>

### Alerts / Flags

<table>
<tr><th>Label</th><th>Current</th><th>Proposed</th><th>Notes</th><th>Description</th></tr>
<tr>
  <td><code>emergency</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#b60205"/></svg> <code>#b60205</code></td>
  <td>—</td>
  <td>Keep. Darkest red in the palette — highest severity. Now unique (security-required moved).</td>
  <td>P0 emergency issue requiring immediate attention</td>
</tr>
<tr>
  <td><code>blocked</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#e11d21"/></svg> <code>#e11d21</code></td>
  <td>—</td>
  <td>Keep. Bright red — distinct from emergency's darker red.</td>
  <td>Issue is blocked by external dependency</td>
</tr>
<tr>
  <td><code>scope-creep</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#f9a825"/></svg> <code>#f9a825</code></td>
  <td>—</td>
  <td>Keep. Amber — warning tone, distinct enough from yellows.</td>
  <td>Issue has grown beyond original scope</td>
</tr>
<tr>
  <td><code>tech-debt</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#e6b800"/></svg> <code>#e6b800</code></td>
  <td>—</td>
  <td>Keep. Gold — near <code>scope-creep</code> but distinguishable.</td>
  <td>Technical debt requiring future attention</td>
</tr>
<tr>
  <td><code>security-finding</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#e36209"/></svg> <code>#e36209</code></td>
  <td>—</td>
  <td>Keep. Burnt orange — sits between the reds and yellows.</td>
  <td>A code review, audit, pen test, or similar revealed a security finding</td>
</tr>
</table>

## Summary of Proposed Changes

<table>
<tr><th>Label</th><th>Current</th><th></th><th>Proposed</th><th>Reason</th></tr>
<tr>
  <td><code>enhancement</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#a2eeef"/></svg> <code>#a2eeef</code></td>
  <td>→</td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#1d76db"/></svg> <code>#1d76db</code></td>
  <td>Identical to <code>story</code></td>
</tr>
<tr>
  <td><code>security-required</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#b60205"/></svg> <code>#b60205</code></td>
  <td>→</td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#d93f0b"/></svg> <code>#d93f0b</code></td>
  <td>Identical to <code>emergency</code></td>
</tr>
<tr>
  <td><code>approval-required</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#fbca04"/></svg> <code>#fbca04</code></td>
  <td>→</td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#e4e669"/></svg> <code>#e4e669</code></td>
  <td>Identical to <code>qa-required</code></td>
</tr>
<tr>
  <td><code>assigned</code></td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#FBCA04"/></svg> <code>#FBCA04</code></td>
  <td>→</td>
  <td><svg width="16" height="16"><rect width="16" height="16" rx="3" fill="#bfd4f2"/></svg> <code>#bfd4f2</code></td>
  <td>Identical to <code>qa-required</code></td>
</tr>
</table>

**19 labels unchanged, 4 labels proposed for new colors.** All duplicates resolved.
