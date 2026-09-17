# Native macOS roadmap

This ledger owns native desktop presentation, interaction, and client lifecycle.
Server behavior belongs in the [core roadmap](core.md); dependencies below refer
to its stable IDs. All macOS work is **Post-R1** relative to the initial Kit v2
release. Section order groups feature areas, not release commitments.

Requirement states and completion rules follow the [main roadmap](../roadmap.md#requirement-states).
`MAC-*` IDs are stable and never reused. This document lists only outstanding
work; Git history retains completed work and verification details. Dependencies on completed macOS items are omitted. Testing accompanies
feature work rather than appearing as separate roadmap items.

Protocol coverage was reviewed against `kit-v2` at `0b40a72b`, which is included
in this branch. Items marked **Server-ready** do not require new server functionality.

## Windows, sessions, and local state

- [ ] MAC-DRAFT-001 — Persist per-server/session composer text and restore it
  across app launches without mixing drafts between windows. Surface save or
  recovery failures while retaining the editable draft.

## Protocol validation

- [ ] MAC-PROTO-002 — Reject malformed event and snapshot payloads with bounded
  memory use and actionable errors, including missing identities, invalid
  values, oversized content, and unsupported capabilities. Depends on
  `CORE-PROTO-001` and `CORE-PROTO-006`.

## Run operations

- [~] MAC-RUN-002 — Add terminal-run retry actions when supported by the
  server. Depends on `CORE-RUN-004`.

## Composer commands and shell execution

- [~] MAC-BASH-001 — Discover excluded shell executions that finish in another
  client between polling intervals. Requires server shell lifecycle events or an
  execution list/cursor endpoint; the current protocol exposes only the active
  ID and lookup by ID. See [direct shell notes](../design/macos-client.md#direct-shell-execution).

## Subagents and cross-session tools

- [ ] MAC-SUB-003 — **Deferred by product decision.** Direct user-to-subagent
  task creation and follow-up messaging are implemented but disabled. Keep
  subagent panes read-only until explicitly revisiting this feature.

- [ ] MAC-PEER-002 — Handle `peer_query.changed` invalidation so externally
  delivered peer work refreshes relevant session state without waiting for a
  parent run to finish. Keep `peer_query`, `peer_result`, and `subagent_result`
  transcript records hidden by product decision. Standalone request tracking
  needs a server query surface beyond the current invalidation event and
  recorded model-tool results.

## Images and attachments

- [ ] MAC-ATT-004 — Restore attachment drafts across launches, verify local
  source availability, and clean up abandoned staging without deleting
  referenced content. Depends on `MAC-DRAFT-001`.

## File, review, and scratchpad workspace

- [ ] MAC-DIFF-001 — Populate the review pane with server-backed changed files
  and revision-aware diffs while preserving highlighting, line numbers, and
  navigation. Expose loading, empty, stale, and failure states. Depends on
  `CORE-DIFF-001` and `CORE-REVIEW-001`.
- [ ] MAC-REVIEW-001 — Keep review notes pinned to file revisions and submit
  them as structured attachments; preserve local edits across refresh and
  failed submission. Depends on `MAC-DIFF-001` and `CORE-REVIEW-002`.
- [ ] MAC-SCRATCH-001 — Load scratchpad content from the session's server and
  persist guarded edits with autosave, conflict, and retry feedback. Depends on
  `CORE-SCRATCH-001`.

## Connections and daemon lifecycle

- [ ] MAC-CONN-001 — Switch connections in-app with explicit loading and
  failure states, keeping windows, drafts, and subscriptions isolated by server
  identity.
- [ ] MAC-DAEMON-001 — Package a compatible local daemon with the app and
  verify discovery/version compatibility in the produced bundle.
- [ ] MAC-DAEMON-002 — Start or attach to the local daemon through its
  canonical lifecycle, with actionable startup failures and no competing daemon
  instances. Closing the app must respect server ownership. Depends on
  `MAC-DAEMON-001`.
- [ ] MAC-REMOTE-001 — Configure named remote endpoints and store credentials
  in Keychain; expose validation and authentication recovery without logging
  secrets. Depends on `MAC-CONN-001` and `CORE-REMOTE-001`.
- [ ] MAC-REMOTE-002 — Negotiate remote capabilities and disable unsupported
  operations with clear reasons. Verify session, attachment, and workspace
  requests target the selected server. Depends on `MAC-REMOTE-001`,
  `CORE-PROTO-001`, and `CORE-REMOTE-003`.

## Packaging and distribution

- [ ] MAC-DIST-001 — Produce a signed app bundle with deliberate entitlements
  and verify launch on a clean machine outside the development checkout.
  Depends on `MAC-DAEMON-001`.
- [ ] MAC-DIST-002 — Notarize and staple the distribution artifact and verify
  Gatekeeper acceptance after download. Depends on `MAC-DIST-001`.
- [ ] MAC-DIST-003 — Provide an update path that preserves settings, drafts,
  and window restoration while keeping the app and daemon protocol-compatible;
  surface update failure and recovery. Depends on `MAC-DIST-002` and
  `MAC-DRAFT-001`.
