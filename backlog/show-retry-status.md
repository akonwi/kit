# Show provider retry status

## Phase 1: Authoritative retry lifecycle

- [x] Extend `internal/droids.ExecutionSnapshot` with authoritative retry state.
- [x] Populate it from the existing durable retry runtime while an execution is waiting.
- [x] Add the exact retry deadline to `attempt.retry_scheduled` lifecycle data.
- [x] Extend `internal/session.Snapshot` with the active provider retry state.
- [x] Project retry state only when the active droid turn matches `ActiveRunID`.
- [x] Add session retry lifecycle events for scheduled and resumed attempts.
- [x] Map `attempt.retry_scheduled` and retried `attempt.started` droid events into those session events.
- [x] Extend `internal/protocol.SessionSnapshot` and `SessionEvent` with the retry state and event kinds.
- [x] Validate retry counts, RFC3339Nano deadlines, and kind/payload compatibility on the wire.
- [x] Project retry snapshot and lifecycle state through the daemon API.

## Phase 2: Native TUI countdown

- [x] Track retry state in the TUI app state.
- [x] Initialize retry state from the attached session snapshot.
- [x] Update or clear it from retry lifecycle and run-finished events.
- [x] Render a live, deadline-based countdown in the existing pending status slot.
- [x] Use ceiling seconds and clamp overdue deadlines to zero.
- [x] Preserve compact wording such as `Retry 2 in 4s…`.
- [x] Clear retry state defensively when later lifecycle state supersedes it.

## Tests

- [x] Droid snapshot exposes retry count and exact deadline while waiting or recovering that wait.
- [x] Scheduled lifecycle payload carries the same deadline as durable runtime state.
- [x] Session snapshot projects retry state only for the matching active run.
- [x] Session lifecycle projection emits scheduled and resumed retry events.
- [x] Protocol validation rejects malformed retry payloads and accepts past deadlines.
- [x] Daemon/client projection preserves retry snapshot and lifecycle state.
- [x] Reconnect/resync restores a waiting retry from the authoritative snapshot.
- [x] TUI tests cover initialization, countdown rounding, zero clamp, clearing, and precedence.

## Non-goals

- [x] Do not expose provider error text in the status line.
- [x] Do not allow manual retry timing controls.
- [x] Do not persist renderer-owned countdown state.

## Rollout

- [x] Preserve existing behavior when retry state is absent.
- [x] Keep the protocol additive for older clients.
- [x] Run the full Go validation suite.
