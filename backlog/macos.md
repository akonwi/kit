# Native macOS backlog

This ledger owns native desktop presentation, interaction, and client lifecycle.
Server behavior belongs in the [core backlog](core.md); dependencies below refer
to its stable IDs. All macOS work is in deferred scope. Section order groups
feature areas.

Requirement states and completion rules follow the [main backlog](README.md#requirement-states).
`MAC-*` IDs are stable and never reused. This document lists only outstanding
work; Git history retains completed work and verification details. Dependencies
on completed macOS items are omitted. Testing accompanies feature work rather
than appearing as separate backlog items.

Protocol coverage was reviewed against `kit-v2` at `0b40a72b`, which is included
in this branch. Items marked **Server-ready** do not require new server functionality.

## Composer commands and shell execution

- [~] MAC-BASH-001 — Discover excluded shell executions that finish in another
  client between polling intervals. Requires server shell lifecycle events or an
  execution list/cursor endpoint; the current protocol exposes only the active
  ID and lookup by ID. Depends on `CORE-BASH-001`. See [direct shell notes](../docs/design/macos-client.md#direct-shell-execution).

## Plugin contributions

- [x] MAC-PLUGIN-001 — Present session-owned plugin contributions through native
  client contracts. The command palette now projects generation-owned commands,
  captures literal arguments separately from the composer, and executes with
  scoped progress/error feedback and stale-selection rejection. Shared plugin
  dialogs retain initial input, empty answers, and confirmation labels/defaults.
  Select dialogs deliberately show plain lists without filtering in the macOS
  app; model-run boundaries preserve runless requests.
  Live plugin notifications now feed existing session alerts with severity,
  plugin provenance, persistent dismissal, and bounded retention. Attachment-owned
  subscriptions reconnect fresh without replay and reject late callbacks after
  switching or detaching. Transport, validation, and lifecycle tests cover this
  path. The user verified plugin notification delivery through macOS session
  alerts. Static styled footer items and aggregate location hiding now appear
  in the bottom-right region, independently of status and workspace controls.
  The macOS footer uses its full allocated width; overflow opens a scrollable,
  selectable popover with all styled contributions and visible location data.
  The user verified static footer rendering and the revised width/overflow UI.
  Plugin footer clicks, URL opening, dedicated
  diagnostics, and per-plugin restart are outside native scope, not deferred
  work; built-in PR links remain. Persistent failure alerts use the existing
  session feedback surface, with detailed evidence in the private server log.
  Depends on `CORE-PLUGIN-004`, `CORE-PLUGIN-006`.
- [x] MAC-GH-001 — Present cached GitHub pull requests in the built-in footer.
  Depends on `CORE-GH-001`.
  The workspace location now shows `cwd (branch* · PR #123)` from server-cached
  pull request metadata; only the PR label is underlined and opens the validated
  target through the system `openURL` path; `PullRequestLink` re-validates number and URL at the client
  boundary and `HTTPClient.vcs` strips unsafe or non-branch metadata. The
  replica preserves pull request state across same-cwd snapshot deliveries,
  consumes an attachment-owned VCS stream (no client polling), and fences held
  updates across cwd transitions (A→B→A) with a dedicated VCS generation.
  Fresh snapshot reconnects, transient backoff, terminal protocol/auth handling,
  and strict bounded NDJSON validation cover client transport ownership.
  Manual verification of the clickable footer pull request remains.

## File, review, and scratchpad workspace

- [x] MAC-SCRATCH-001 — Load scratchpad content from the session's server and
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

## Verification

- [ ] MAC-TEST-001 — Stabilize the attachment-cache read-count test under
  full-suite execution. `TranscriptAttachmentTests.cachePreservesFullDownloadsAndBoundsTextPreviews`
  failed its exact read-count assertions once during PR-footer Xcode validation;
  isolated and full-suite reruns passed. Investigate whether `NSCache` eviction
  under parallel test load invalidates the test's deterministic-cache assumption.
