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

## Composer commands and shell execution

- [~] MAC-BASH-001 — Discover excluded shell executions that finish in another
  client between polling intervals. Requires server shell lifecycle events or an
  execution list/cursor endpoint; the current protocol exposes only the active
  ID and lookup by ID. Depends on `CORE-BASH-001`. See [direct shell notes](../docs/design/macos-client.md#direct-shell-execution).

## File, review, and scratchpad workspace

- [ ] MAC-SCRATCH-001 — Load scratchpad content from the session's server and
  persist guarded edits with autosave, conflict, and retry feedback. Depends on
  `CORE-SCRATCH-001`.

## Connections and daemon lifecycle

- [ ] MAC-DAEMON-001 — Package a compatible local daemon with the app and
  verify discovery/version compatibility in the produced bundle.
- [ ] MAC-DAEMON-002 — Start or attach to the local daemon through its
  canonical lifecycle, with actionable startup failures and no competing daemon
  instances. Closing the app must respect server ownership. Depends on
  `MAC-DAEMON-001`.
- [ ] MAC-REMOTE-001 — Configure named remote endpoints and store credentials
  in Keychain; expose validation and authentication recovery without logging
  secrets. Depends on `CORE-REMOTE-001`.
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
  surface update failure and recovery. Depends on `MAC-DIST-002`.
