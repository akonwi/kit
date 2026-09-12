# Kit v2 roadmap

## Purpose

Kit v2 is a production replacement for the current Kit release, not an attempt
to reproduce every historical feature before shipping. This roadmap defines the
first releasable v2 scope, records deliberate deferrals, and tracks later client
and platform work.

The fixed comparison baseline is Git commit
[`5c6e112`](https://github.com/akonwi/kit/tree/5c6e112). Relevant changes added
to production `main` after that commit must enter the rolling-delta table and be
assigned to the initial release, a later milestone, or an accepted decision.
They do not become release requirements implicitly.

Historical documentation in `app/docs/features/` is a behavioral reference,
not an architectural constraint. The v2 architecture is defined by
[`adrs/0001-native-go-architecture.md`](adrs/0001-native-go-architecture.md).

## Roadmap organization

Each requirement has one canonical owner. Cross-cutting client work depends on
core requirement IDs instead of repeating the server requirement.

- [Core and protocol](roadmap/core.md): daemon, runtime, persistence, migration,
  protocol, tools, integrations, headless operation, and shared client contracts.
- [Native TUI](roadmap/tui.md): terminal presentation and interaction.
- [Semantic web](roadmap/web.md): browser presentation, synchronization,
  accessibility, and browser-specific security.

A desktop roadmap will be added if that client is approved. It is not part of
the initial release.

## Requirement states

- `[ ]` not started
- `[~]` in progress or only partially implemented
- `[x]` complete; retained only as a dependency tombstone
- `[-]` rejected or superseded by an accepted decision

Stable IDs are never reused. An initial-release decision must be resolved into
**R1 required** or **Post-R1** before a release candidate. Completed checklist
items are removed after implementation, persistence and migration implications,
and automated or recorded manual verification are complete. A stable ID remains
as a terse `[x]` tombstone while an outstanding requirement depends on it. Git
history and referenced implementation plans retain the full completion record.

When adding a requirement:

1. Give it one owner and a stable `ROAD-*`, `CORE-*`, `TUI-*`, or `WEB-*` ID.
2. State an observable outcome rather than an implementation task.
3. Add explicit dependencies on requirements owned by another ledger.
4. Identify how completion will be verified when that is not self-evident.
5. Add production changes to the rolling delta until they are triaged.

## Initial production release contract

The first v2 release replaces the existing installation and production
`~/.kit` state. It is a local, terminal-first coding agent for macOS and Linux.
It must provide:

- safe backup and idempotent migration before using production data;
- one CGO-free executable with reliable local daemon, TUI, and headless modes;
- durable sessions, transcripts, configuration, concurrent isolation, and
  recovery;
- supported provider authentication, model/thinking selection, compaction,
  cancellation, follow-up queueing, and automatic naming;
- essential coding tools, supervised subagents, and production-ready MCP;
- interactive agent questions and confirmations;
- end-to-end image and attachment validation, persistence, submission,
  restoration, cleanup, transcript previews, and `show_image` behavior;
- theme discovery, selection, persistence, and current custom-theme
  compatibility in the native TUI;
- native directory exploration plus retained file and diff viewing with complete
  keyboard, mouse, narrow-layout, loading, empty, and failure UX;
- safe install and upgrade paths, actionable diagnostics, bounded resource use,
  and release verification.

The release does not require the semantic web client, a desktop client, remote
serving or attach, external plugins, code-review workspaces, scratchpad, pager,
or complete historical feature parity. Those features remain visible in their
Post-R1 roadmaps rather than silently expanding the release gate.

## R1 decision

R1 has one unresolved scope decision:
[`TUI-KEY-001`](roadmap/tui.md#r1-decision), which determines whether
user-configurable keybindings and current keybinding configuration compatibility
ship in the initial release. It must be resolved before a release candidate.

## Accepted decisions

- [-] `web-tui` is removed.
- [-] OpenTUI is replaced by the native vaxis/ui client.
- [-] The TypeScript/Pi runtime is replaced by Kit's private Go/droids runtime.
- [-] Subagents are concurrent supervised executions with durable mailboxes.
- [-] In-process TypeScript plugins are not restored; future custom plugins use
  subprocess RPC.
- [-] Windows is unsupported.
- [-] The semantic web client is Post-R1.
- [-] `CLAUDE.md` context discovery is not restored; Kit uses `AGENTS.md`.

## Initial-release distribution and verification

- [~] ROAD-R1-001 — Publish CGO-free macOS and Linux artifacts through a native
  release workflow.
- [ ] ROAD-R1-002 — Provide a supported install and upgrade path for existing
  npm-installed users.
- [ ] ROAD-R1-003 — Provide update checks, bounded paginated release history,
  and native release packaging without delaying startup.
- [ ] ROAD-R1-004 — Verify installed artifacts, upgrades, and local-daemon
  lifecycle on macOS and Linux after `CORE-TEST-004` and `TUI-TEST-001` pass.
- [~] ROAD-R1-005 — Reconcile user documentation, CLI help, ADRs, and roadmap
  links with the shipped R1 surface.

R1 is releasable only when every R1 requirement in this index and the core/TUI
roadmaps is complete, `TUI-KEY-001` is resolved, Post-R1 work is not on the
critical path, and production `main` has received a final rolling-delta audit.

## Rolling production delta

Add every relevant unresolved or intentionally superseded behavior merged after
`5c6e112`. Remove a row after its decision is represented by a stable roadmap
requirement or accepted decision and has been verified.

Last audited against production `main` at `c9abdf2` (Kit v0.35.1). Version-only
commits are omitted.

| Change | R1 decision | Tracking |
| --- | --- | --- |
| Add validated `show_image` and explicit transcript image previews (`a2c7434`) | Required | `CORE-ATT-002`, `TUI-ATT-002` |
| Open transcript images in retained workspace panes (`e6c181f`) | Required | `TUI-ATT-002` |
| Submit structured code-review feedback without a generic prompt preamble (`0fc9f94`) | Post-R1 | `CORE-REVIEW-001`, `TUI-REVIEW-001` |
| Upgrade the TypeScript Pi runtime to 0.85 (`1971d71`) | Superseded | Native droids decision; ADRs 0001 and 0006 |
