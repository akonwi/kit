# Kit v2 backlog

## Purpose

Kit v2 is a production replacement for the current Kit release, not an attempt
to reproduce every historical feature. This backlog tracks open production
work, deliberate deferrals, and later client and platform work.

`backlog/` is the sole source of outstanding work. Documents under `docs/`
describe accepted architecture, behavior, design, and research; they must link
to backlog requirements rather than maintain separate work lists. Focused plans
may live beside these ledgers when they add implementation context, but the
ledger requirement remains the canonical status and must not be duplicated.

The fixed comparison baseline is Git commit
[`5c6e112`](https://github.com/akonwi/kit/tree/5c6e112). Relevant changes added
to production `main` after that commit must enter the rolling-delta table and be
assigned to release scope, deferred scope, or an accepted decision. They do not
become release requirements implicitly.

Historical documentation in `apps/web/docs/features/` is a behavioral reference,
not an architectural constraint. The v2 architecture is defined by
[`docs/adrs/0001-native-go-architecture.md`](../docs/adrs/0001-native-go-architecture.md).

## Backlog organization

Each requirement has one canonical owner. Cross-cutting client work depends on
core requirement IDs instead of repeating the server requirement.

- [Core and protocol](core.md): daemon, runtime, persistence, migration,
  protocol, tools, integrations, headless operation, and shared client contracts.
- [Native TUI](tui.md): terminal presentation and interaction.
- [Semantic web](web.md): browser presentation, synchronization,
  accessibility, and browser-specific security.
- [Native macOS](macos.md): desktop foundation and native client work.
  This client is not in production release scope.

Focused implementation and design notes:

- [Hidden workspace pane suspension](hidden-workspace-pane-suspension.md)
- [Workspace pane stack limitations](workspace-pane-stack.md)
- [Workspace tab ceiling recovery](workspace-tab-ceiling-recovery.md)
- [Ataraxy review integration](ataraxy-review-integration.md)

## Requirement states

- `[ ]` not started
- `[~]` actively in progress; not a summary for mixed completed and outstanding
  scope
- `[x]` complete; retained only as a dependency tombstone
- `[-]` rejected or superseded by an accepted decision

Stable IDs are never reused. A release-scope decision must be resolved into
**release scope** or **deferred scope** before a release candidate. Do not leave
a broad requirement partial after its completed and outstanding scope can be
identified.
Remove the completed scope and replace the remainder with concrete, observable
`[ ]` requirements using new stable IDs. Completed checklist items are removed
after implementation, persistence and migration implications, and automated or
recorded manual verification are complete. A stable ID remains as a terse `[x]`
tombstone only while an outstanding requirement depends on it. Git history
retains the full completion record.

When adding a requirement:

1. Give it one owner and a stable `ROAD-*`, `CORE-*`, `TUI-*`, `WEB-*`, or `MAC-*` ID.
2. State an observable outcome rather than an implementation task.
3. Add explicit dependencies on requirements owned by another ledger.
4. Identify how completion will be verified when that is not self-evident.
5. Add production changes to the rolling delta until they are triaged.

## Production release scope

The production release replaces the existing application installation with native
Kit. It is a local, terminal-first coding agent for macOS and Linux. The release
keeps `~/.kit` and reuses compatible configuration in place. Native Kit starts
with fresh sessions and runtime data; legacy history is left untouched, not
imported or resumed. Users reauthenticate through the login UX rather than
running a credential importer. It must provide:

- configuration reuse, migration documentation and a built-in guidance skill,
  with legacy runtime data isolated from native storage;
- one self-contained native executable per supported OS/architecture, with
  reliable local daemon, TUI, and headless modes;
- durable sessions, transcripts, configuration, concurrent isolation, semantic
  session forking with lineage, and recovery;
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
or complete historical feature parity. Those features remain visible in the
Deferred scope sections rather than silently expanding production release scope.

## Accepted decisions

- [-] `web-tui` is removed.
- [-] OpenTUI is replaced by the native vaxis/ui client.
- [-] The TypeScript/Pi runtime is replaced by Kit's private Go/droids runtime.
- [-] Subagents are concurrent supervised executions with durable mailboxes.
- [-] In-process TypeScript plugins are not restored; future custom plugins use
  subprocess RPC.
- [-] Windows is unsupported.
- [-] Legacy runtime-data import and session continuation are excluded, not
  deferred.
- [-] No `kit migrate` command, automated compatibility scanner, configuration
  conversion framework, or credential importer is required. Existing users reuse
  compatible configuration and reauthenticate; a guide and built-in skill cover
  manual configuration adjustments.
- [-] npm distribution is discontinued; native Kit supports Homebrew and manual
  binary installation only.
- [-] The semantic web client is deferred.
- [-] `CLAUDE.md` context discovery is not restored; Kit uses `AGENTS.md`.
- [-] Native Tree-sitter highlighting permits CGO under ADR 0021; published
  artifacts statically include the curated grammars.

## Release distribution and verification

- [~] ROAD-R1-001 — Publish self-contained macOS and Linux arm64/amd64 artifacts
  through a pinned native CGO toolchain matrix with an explicit Linux libc and
  macOS deployment-target policy. The release workflow builds and smoke-tests
  Go-only archives on all four platforms, targeting macOS 14.0 and Ubuntu 24.04
  (glibc 2.39). Exact C/SDK toolchain pinning and installed-artifact verification
  remain outstanding; runner labels alone do not pin those toolchains.
- [ ] ROAD-R1-002 — Provide Homebrew and manual binary installation and upgrade
  paths for existing npm-installed users. Document npm removal, PATH/version
  verification, configuration reuse, fresh sessions, and provider/MCP
  reauthentication; npm is not a native Kit distribution channel.
- [ ] ROAD-R1-003 — Provide update checks, bounded paginated release history,
  and native release packaging without delaying startup.
- [ ] ROAD-R1-004 — Verify installed artifacts, upgrades, and local-daemon
  lifecycle on macOS and Linux after `CORE-TEST-004` and `TUI-TEST-001` pass.
- [~] ROAD-R1-005 — Reconcile user documentation, CLI help, ADRs, and backlog
  links with the production release surface.

## Rolling production delta

Add every relevant unresolved or intentionally superseded behavior merged after
`5c6e112`. Remove a row after its decision is represented by a stable backlog
requirement or accepted decision and has been verified.

Last audited against production `main` at `c9abdf2` (Kit v0.35.1). Version-only
commits are omitted.

| Change | Release decision | Tracking |
| --- | --- | --- |
| Submit structured code-review feedback without a generic prompt preamble (`0fc9f94`) | Deferred | `CORE-REVIEW-001`, `TUI-REVIEW-001` |
| Upgrade the TypeScript Pi runtime to 0.85 (`1971d71`) | Superseded | Native droids decision; ADRs 0001 and 0006 |
