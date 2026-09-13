# TUI transcript lifecycle presentation implementation plan

## Status

In progress. This file is an implementation checklist and working reference, not
an accepted architecture record. Promote durable protocol or ownership decisions
into an ADR when necessary.

Related roadmap entries:

- [`TUI-TRANSCRIPT-003`](../docs/roadmap/tui.md)
- [`CORE-RUN-003`](../docs/roadmap/core.md): cancellation
- [`CORE-RUN-004`](../docs/roadmap/core.md): provider retry state
- [`CORE-RUN-005`](../docs/roadmap/core.md): compaction lifecycle

## Current assessment

- [x] Present model and thinking selection.
- [x] Present context pressure and model capacity.
- [ ] Add presentation coverage for context pressure thresholds and live updates.
- [x] Present provider retry countdowns from authoritative snapshot and ordered
  lifecycle state, including reconnect and restart recovery.
- [x] Present automatic compaction lifecycle from authoritative snapshots and
  ordered events. Pending state survives daemon restart/reconnect, stale events
  cannot replace newer operations, and replayed outcomes are deduplicated.
- [~] Present cancellation. The live stopping state works, but invocation,
  repeated requests, failures, and dependent core behavior need hardening.
- [x] Present terminal run and feedback state.
- [~] Preserve retained evidence across all lifecycle states. The layout and
  transcript projection do this substantially, but there is no combined
  acceptance test for every state.

## Implementation principles

- The server remains authoritative for run, retry, compaction, and terminal
  lifecycle state.
- Clients must not infer retry deadlines from provider errors or maintain a
  divergent retry policy.
- Snapshot and ordered-event projections must produce the same visible state.
- Replayed events must not duplicate user feedback.
- Lifecycle status belongs in fixed shell chrome; it must not replace retained
  transcript or activity evidence.
- All values crossing process or network boundaries must be bounded and
  validated on both sides.

## Phase 1: expose authoritative provider retry state

### Core and protocol

- [x] Define renderer-neutral retry state with an attempt/count and authoritative
  retry deadline.
- [x] Add ordered lifecycle events for retry scheduled and retry resumed or
  recovered.
- [x] Add active retry state to `protocol.SessionSnapshot` for reconnect and
  resynchronization.
- [x] Translate droid lifecycle events such as `attempt.retry_scheduled` and
  `attempt.started` in `internal/session/events.go`.
- [x] Clear retry state when the retry resumes, the run settles, or cancellation
  becomes authoritative.
- [x] Keep retry timing bounded by the active retry policy and reject invalid or
  unrelated wire payload fields.
- [x] Preserve retry state through daemon/client projections without exposing
  concrete droid types.

### Tests

- [x] Validate retry event payloads and field exclusivity.
- [x] Test droid-to-session retry lifecycle translation.
- [x] Test snapshot reconstruction while a retry is pending or recovering after
  restart.
- [~] Test retry resumption, cancellation, terminal settlement, replay, and
  reconnect. Resumption, settlement clearing, client forwarding, and snapshot
  resynchronization are covered; combined cancellation and replay scenarios
  remain.
- [ ] Verify provider-supplied delays remain bounded by the active retry policy.

## Phase 2: present retry countdowns in the TUI

### State and rendering

- [x] Store active retry state in `appState`.
- [x] Apply snapshot and event updates idempotently.
- [x] Render a concise countdown such as `Retry 2 in 4s…` in the fixed turn
  status slot.
- [x] Derive remaining time from the authoritative deadline.
- [x] Tick only while a retry deadline is active and use the existing bounded
  frame cadence.
- [x] Clear countdown state when retry resumes or the run settles.
- [x] Give `Stopping…` precedence after cancellation.
- [x] Keep terminal title/progress in running state while waiting to retry.
- [x] Preserve transcript scrolling, selection, and activity expansion while the
  countdown changes; retry updates only replace fixed status state.

### Tests

- [x] Test countdown ceiling-rounding and overdue clamping to zero.
- [x] Verify live events and reconnect snapshots produce identical presentation.
- [x] Verify cancellation overrides retry presentation.
- [ ] Verify the countdown does not replace retained transcript content in a
  rendered long-transcript acceptance test.
- [ ] Verify countdown updates do not cause unbounded repainting.

## Phase 3: make compaction lifecycle reconnect-safe

Implemented state:

- Automatic compactions receive stable `compact_...` identities. Active identity
  and forced semantics are persisted atomically with `compaction.started` before
  provider work begins.
- Recovery treats a persisted compaction as mandatory work and reuses its
  identity. Completion or failure emits one matching terminal lifecycle event
  and atomically clears active state.
- Active compaction is projected through droid, session, daemon, and protocol
  snapshots. The session protocol is version 30.
- Compaction lifecycle payloads are bounded and validated at session and wire
  boundaries, including identity exclusivity and rejection of unrelated
  assistant content.
- Outcomes remain transient toast feedback keyed by compaction identity. The TUI
  retains a bounded 16-identity deduplication window rather than adding recent
  outcomes to retained session details.

### Core and protocol

- [x] Define authoritative active compaction snapshot state and identified
  completed/failed lifecycle events.
- [x] Reconstruct pending automatic compaction after daemon restart or client
  reconnect.
- [x] Preserve ordering between successful compaction and its context update by
  committing both events with the checkpoint and runtime replacement.
- [x] Give every lifecycle event a stable identity so replay can be deduplicated.
- [x] Keep the latest outcome as deduplicated transient feedback rather than
  retained session-detail state.

### TUI and tests

- [x] Apply snapshot compaction state to the fixed turn-status slot.
- [x] Test snapshot restoration while compaction is pending.
- [~] Test completed and failed event replay and deduplication. Completed replay,
  stale older outcomes, and failed rendering are covered; direct failed-event
  replay coverage remains.
- [x] Test context pressure refresh after successful compaction.
- [x] Reject stale same-stream snapshots before they can regress run,
  compaction, retry, transcript, or usage state.
- [ ] Test the complete manual flow: request, pending state, result, snapshot
  replacement, and repaint.

## Phase 4: harden cancellation presentation

- [ ] Ignore repeated Escape requests while `runStopping` is true.
- [ ] Add direct TUI tests proving one `Run.Abort` or `Session.Abort` request is
  issued.
- [ ] Define visible abort timeout/failure feedback while waiting for
  authoritative settlement.
- [ ] Test cancellation during provider retry, compaction, event-stream
  reconnect, and tool execution.
- [ ] Verify late events cannot replace `Stopping…`.
- [ ] Verify aborted prose remains visible with terminal styling.
- [ ] Verify completed tools retain success and unresolved tools become visibly
  not-run or aborted.

## Phase 5: close presentation coverage gaps

- [ ] Verify context pressure below 80% uses muted styling.
- [ ] Verify context pressure from 80% through 90% uses warning styling.
- [ ] Verify context pressure above 90% uses danger styling.
- [ ] Verify a live context update repaints the header percentage and style.
- [ ] Verify exact context usage and capacity remain available in session
  details.
- [ ] Add one long-transcript acceptance test covering retrying, compacting,
  stopping, and terminal-state transitions without losing scrolling, selection,
  or retained evidence.

## Validation gates

For each phase, run focused package tests first. Before marking the roadmap item
complete, run:

```sh
gofmt -l .
go build ./...
go vet ./...
go test ./...
go test -race ./...
```

The race suite is required because this work changes session lifecycle,
cancellation, event delivery, and timer-driven UI state.

## Completion criteria

- [ ] `CORE-RUN-003` provides deterministic terminal cancellation behavior used
  by the TUI.
- [ ] `CORE-RUN-004` exposes bounded retry countdown and recovery state.
- [x] `CORE-RUN-005` exposes reconnect-safe automatic compaction lifecycle
  state; broader roadmap completion remains tracked in `docs/roadmap/core.md`.
- [x] Snapshot attachment and ordered live events converge on identical retry
  and automatic-compaction presentation state.
- [ ] Retry, compaction, and cancellation status never obscures retained
  transcript evidence.
- [~] Focused tests, the full Go suite, vet, and build pass. Targeted retry race
  tests pass; the full TUI race suite still reports an unrelated existing race
  in attachment preview rendering.
- [ ] Mark `TUI-TRANSCRIPT-003` complete in the roadmap.
