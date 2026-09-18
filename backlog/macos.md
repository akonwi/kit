# Native macOS backlog

This ledger owns native desktop presentation, interaction, and client lifecycle.
Server behavior belongs in the [core backlog](core.md); dependencies below refer
to its stable IDs. All macOS work is **Post-R1** relative to the initial Kit v2
release. Section order groups feature areas, not release commitments.

Requirement states and completion rules follow the [main backlog](README.md#requirement-states).
`MAC-*` IDs are stable and never reused. This document lists only outstanding
work; Git history retains completed work and verification details. Dependencies on completed macOS items are omitted. Testing accompanies
feature work rather than appearing as separate backlog items.

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
  ID and lookup by ID. See [direct shell notes](../docs/design/macos-client.md#direct-shell-execution).

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

- [x] MAC-ANNOTATION-001 — Native workspace-file annotations under ADR 0022:
  inline inputs/notes, gutter range highlighting, shared composer chips with
  overflow, retained-tab reveal, frozen transcript groups, and stale replacement.
  The maintained CodeEdit layout adaptation preserves source text and line numbers.
  Synthetic editor/transport checks and temporary-session CRUD/SSE validation cover
  this flow. The user manually verified annotation-only prompt acceptance on September 18,
  2026; the automated equivalent remains opt-in (`KIT_ANNOTATION_SEND_LIVE_TEST`).
- [x] MAC-DIFF-001 — Native Diff pane with server-issued working-tree, branch,
  and commit targets; paged changed files and semantic hunks; old/new gutters,
  retained source selection/highlighting, and loading/empty/partial/nontext/stale
  failure states. Continuations pin target and file revisions.
- [x] MAC-REVIEW-001 — Diff gutter ranges create side- and revision-pinned
  annotations through the shared input/chip/submission flow. Chip reveal uses
  annotation-authorized reconstruction; refresh and failed writes preserve drafts
  and require explicit reselection before replacing stale evidence.
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
