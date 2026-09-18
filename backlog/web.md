# Semantic web backlog

The semantic browser client is an approved Post-R1 client. None of this file is
part of the initial production replacement gate. Core behavior remains owned by
the [core backlog](core.md); this file owns browser transport, presentation,
interaction, security, and accessibility.

## Post-R1 foundation and security

- [ ] WEB-BUILD-001 — Build Solid/Mica assets and embed production output in the
  Go executable without requiring Bun or Node at user runtime.
- [ ] WEB-SEC-001 — Enforce same-origin CSP, authenticated assets and APIs,
  untrusted-text rendering, and no CDN dependency.
- [ ] WEB-SEC-002 — Use secure browser sessions, validate Host and Origin, and
  apply upload/request/connection bounds supplied by the remote server. Depends
  on `CORE-REMOTE-001`, `CORE-REMOTE-002`, and `CORE-REMOTE-003`.
- [ ] WEB-SYNC-001 — Implement explicit connection phases, ordered reduction,
  reconnect, replay, snapshot fallback, and stale asynchronous-result guards.
  Depends on `CORE-PROTO-003` and `CORE-PROTO-006`.

## Post-R1 product experience

- [ ] WEB-SHELL-001 — Present transcript, streaming activity, tool state,
  composer, follow-up queue, abort, model/thinking selection, session naming,
  and commands with server-authoritative state.
- [ ] WEB-AUTH-001 — Present provider credential setup, replacement, logout, and
  actionable failures without exposing secrets. Depends on `CORE-AUTH-001`.
- [ ] WEB-MCP-001 — Present MCP status, authentication, logout, and bounded debug
  detail without exposing credentials. Depends on `CORE-MCP-004` and
  `CORE-MCP-005`.
- [ ] WEB-INT-001 — Present confirm, input, select, and guided-question flows
  with reconnect-safe pending state. Depends on `CORE-INT-001` and
  `CORE-INT-002`.
- [ ] WEB-ATT-001 — Upload, validate, preview, restore, submit, and clean up
  attachments with authenticated reads and responsive presentation. Depends on
  `CORE-ATT-001` and `CORE-REMOTE-003`.
- [ ] WEB-WORK-001 — Add activity, image, subagent roster/transcript, release,
  Mermaid, and other approved retained workspace panes.
- [ ] WEB-SESSION-001 — Add a bounded session explorer backed by the shared
  server catalog. Depends on `CORE-CATALOG-001`.
- [ ] WEB-REVIEW-001 — Add browser code-review workflows after the shared review
  contract exists. Depends on `CORE-REVIEW-001`.
- [ ] WEB-THEME-001 — Persist browser-local theme selection and per-session
  drafts without overriding server-owned settings.
- [ ] WEB-UPD-001 — Present update availability, release notes, and an explicit
  update action without blocking startup. Depends on `ROAD-R1-003`.
- [ ] WEB-MOBILE-001 — Support mobile layouts, software keyboards, safe areas,
  touch targets, and coarse-pointer submission behavior.
- [ ] WEB-A11Y-001 — Pass keyboard, focus, semantics, contrast, screen-reader,
  and reduced-motion verification.
- [ ] WEB-TEST-001 — Pass browser formatting, lint, typecheck, unit, integration,
  and accessibility suites.
