# Session model, thinking, and usage parity plan

## Status

Planned. This file is an implementation checklist, not an accepted architecture
record. Promote durable architectural decisions into an ADR if implementation
changes the ownership boundaries established by ADR 0006.

Related parity entries:

- [`docs/parity.md`](../docs/parity.md): session metadata persistence
- [`docs/parity.md`](../docs/parity.md): context usage and model limits
- [`docs/parity.md`](../docs/parity.md): model selection
- [`docs/parity.md`](../docs/parity.md): thinking-level discovery and persistence
- [`docs/parity.md`](../docs/parity.md): `/model`, `/thinking`, and `/session`

## Current behavior

### Model and thinking

- Session creation resolves and validates an exact provider/model ID.
- Kit persists `model_provider`, `model_id`, and `thinking_level` in its session
  registry.
- Opening a session configures its droid from those persisted values.
- CLI model/thinking flags can select creation values and filter implicit resume.
- The native TUI displays the restored model and thinking level.
- Existing sessions cannot change model or thinking level.
- Restoring a saved thinking level that is no longer supported fails instead of
  clamping it.
- Droids configuration is fixed for the lifetime of an opened `Droid`; Kit has
  no safe model/thinking reconfiguration operation yet.

### Usage

- Providers report input, output, cache-read, cache-write, reasoning, and total
  tokens.
- Droids computes cost using the model active for each provider call.
- Assistant messages and terminal turn records persist usage in each session's
  droid Store.
- `droids.Outcome` returns per-turn usage.
- Kit currently drops that usage when projecting prompt results, events,
  transcripts, and session snapshots.
- The TUI's context percentage is current context pressure, not cumulative
  provider usage.

## Accepted product behavior

### Ownership

```text
kit.db session registry
└── selected model and thinking level

per-session droid Store
├── canonical messages and turns
├── context state
└── cumulative usage and cost

Manager snapshot
└── projects both authorities into the session protocol
```

Kit must not persist a duplicate usage aggregate in its session registry.
Droids owns usage because droids owns provider calls, turns, retries, forks, and
compaction.

### Model changes

- Model and thinking level change as one configuration operation.
- Configuration changes are allowed only while the session is quiescent.
- Reject a change while a parent run, reload, deletion, or direct bash execution
  makes replacement unsafe.
- Resolve and persist exact provider/model IDs; do not persist display names or
  ambiguous aliases.
- If the target model cannot fit the current context, compact automatically and
  then switch.
- Do not switch first and defer overflow discovery until the next prompt.
- Preserve canonical droid history, turn IDs, message IDs, boundaries, and fork
  lineage across the runtime replacement.
- A successful configuration change advances session activity time.

### Thinking levels

Canonical Kit ordering:

```text
off < minimal < low < medium < high < xhigh < max
```

Provider-specific `none` is presented and persisted canonically as `off` where
applicable.

Resolution rules:

1. A saved level that is still supported is restored exactly.
2. A saved level that is no longer supported is clamped without silently
   increasing effort: choose the greatest supported level at or below the saved
   level; if none exists, choose the lowest supported level. Report and persist
   the adjustment.
3. With no saved level, use `medium` when supported; otherwise use the lowest
   supported level, preferring `off` when available. Persist the resolved value.
4. The model/thinking selectors only offer supported combinations.
5. Malformed or explicitly unsupported raw protocol requests are rejected; the
   restore-time clamp is not a substitute for boundary validation.

Examples:

```text
supported: off, low, high      no saved level -> off
supported: low, high           no saved level -> low
supported: high                no saved level -> high
supported: low, medium, high   no saved level -> medium
saved: xhigh; supported: low, high             -> high + warning
saved: low; supported: medium, high            -> medium + warning
```

### Cumulative usage

Expose one cumulative total. Do not expose inherited/newly-incurred breakdowns.

```go
type SessionUsage struct {
    InputTokens      int
    OutputTokens     int
    CacheReadTokens  int
    CacheWriteTokens int
    ReasoningTokens  int
    TotalTokens      int
    CostUSD          float64
}
```

- Aggregate every provider call represented by canonical droid history,
  including multiple model cycles in tool loops and retries when the provider
  reports usage.
- Persist the aggregate in the droid Store and update it atomically with the
  canonical usage-bearing operation.
- A forked or handed-off conversation starts with the source conversation's
  cumulative total at the exact fork point, then adds its own subsequent usage.
- Later work in the source conversation does not change the child total.
- Historical costs remain those computed using the model active at execution
  time; model switching does not reprice old work.
- Expose cumulative usage through the session protocol and a `/session` details
  surface.
- Keep the normal TUI header focused on current context percentage; do not add
  cumulative tokens or cost to persistent header chrome.

## Target configuration flow

```text
/model or /thinking selector
└── Session.Configure
    └── daemon session configuration endpoint
        └── Manager.ConfigureSession
            ├── acquire admission and runtime control locks
            ├── require a quiescent session
            ├── resolve exact target model
            ├── resolve/clamp effective thinking level
            ├── measure context against the target model
            ├── compact current droid context when required
            ├── prepare a replacement droid over the same Store
            ├── atomically persist model + thinking + activity
            ├── swap droid/runtime configuration
            ├── replace the runtime event stream
            └── return applied configuration and warnings
```

The existing session reload transition is the starting pattern: prepare a
replacement while admission/control are excluded, retain the old runtime on a
pre-commit failure, commit Kit-owned registry state, then swap the in-memory
projection. Compaction may durably improve context before the configuration
commit; that remains valid if the later model switch fails.

Crash boundary:

```text
before registry commit   -> persisted model remains old
                            old runtime remains authoritative
                            completed compaction may remain

after registry commit    -> persisted model is new
                            restart opens the new configuration
```

## Implementation checklist

### 1. Separate the parity ledger

- [x] Split the broad model/thinking/usage line into independently verifiable
  entries for initial persistence, mutable configuration, and cumulative usage.
- [x] Keep context pressure and cumulative provider usage described separately.
- [ ] Add final implementation and test references to the relevant parity
  entries as each remaining slice is completed.

### 2. Droids: quiescent model adaptation

- [x] Add a droids-owned operation to measure current context against a target
  model and reasoning configuration.
- [x] Add an explicit quiescent compaction operation suitable for model
  adaptation; Kit must not implement model-context rewriting itself.
- [x] Give model-adaptation compaction a stable operation ID so retries after an
  ambiguous response do not compact twice.
- [x] Require settled/quiescent execution state and reject active execution.
- [x] Preserve canonical history, checkpoint identity, boundaries, lineage, and
  event ordering.

Kit-side integration behavior is deferred to end-to-end validation after the
manager, protocol, and `/compact` command are wired.

### 3. Droids: cumulative usage authority

- [ ] Add a durable cumulative `SessionUsage` aggregate owned by droids.
- [ ] Update it atomically whenever provider usage becomes canonical.
- [ ] Include all model cycles in a turn and usage reported for failed, aborted,
  or retried calls without double counting replayed records.
- [ ] Expose cumulative usage from an authoritative droid snapshot API.
- [ ] Expose per-turn usage from `TurnSnapshot` if protocol/run details need it;
  do not make Kit scan private droid records.
- [ ] Initialize a fork's cumulative usage from the source's total at the exact
  fork point.
- [ ] Define record-version compatibility or an explicit development-database
  reset rule; never silently interpret missing aggregate state as a verified
  zero when historical usage records exist.
- [ ] Keep cumulative usage intact across compaction and runtime replacement.

### 4. Kit repository: atomic configuration persistence

- [ ] Add one repository operation that updates model provider, model ID,
  thinking level, and activity time atomically.
- [ ] Store a client-selected configuration mutation ID and an idempotent receipt
  because compaction and runtime replacement make retries semantically
  significant.
- [ ] Include an expected configuration revision so stale attached clients
  cannot silently overwrite a newer model choice.
- [ ] Increment and return the configuration revision on a committed change.
- [ ] Make activity advancement monotonic.
- [ ] Add migration/schema tests, idempotent replay tests, stale-revision tests,
  and ambiguous-commit reconciliation tests.

### 5. Session manager: configuration transition

- [ ] Introduce a single `ConfigureSession` operation for model and thinking;
  avoid independent setters that can expose invalid intermediate combinations.
- [ ] Follow the established lock order and reject changes while the session is
  active, reloading, deleting, or running direct bash.
- [ ] Resolve the target model through the provider registry and validate exact
  provider/model ownership.
- [ ] Resolve thinking using the accepted restore/default rules.
- [ ] Automatically invoke droids model-adaptation compaction when required.
- [ ] Expose an idempotent manager operation for explicit settled-session
  compaction so `/compact` uses the same droids-owned path.
- [ ] Open and validate a replacement droid against the same authoritative Store
  before committing Kit registry state.
- [ ] Persist configuration and mutation receipt before publishing the runtime
  replacement.
- [ ] Replace the runtime event stream so clients cannot continue from a cursor
  created under the previous configuration.
- [ ] Return the effective model, effective thinking level, configuration
  revision, new stream identity, and any clamp/shutdown warnings.
- [ ] On startup, resolve and persist missing or stale thinking levels before
  opening the runtime; surface an adjustment warning to the attaching client.
- [ ] Ensure restart after every meaningful transition boundary converges on the
  committed registry configuration.

### 6. Protocol and clients

- [ ] Bump the canonical protocol version.
- [ ] Add wire-safe model capability records: exact ID, display name, provider,
  context limits, supported thinking levels, and relevant input capabilities.
- [ ] Add a session configuration command with mutation ID, expected revision,
  target model, and optional target thinking level.
- [ ] Add an explicit session-compaction command carrying a stable operation ID.
- [ ] Return the applied configuration rather than requiring clients to infer
  clamp results.
- [ ] Add cumulative `SessionUsage` to authoritative session snapshots.
- [ ] Add usage updates at durable model-call boundaries so an open session
  details view can update without polling full history.
- [ ] Validate enum values, bounded IDs, non-negative token counts, finite
  non-negative costs, revision monotonicity, and model/thinking compatibility on
  both sides of the transport boundary.
- [ ] Add local-client and daemon conformance tests for success, busy state,
  stale revision, idempotent retry, resynchronization, and cancellation.

### 7. Native TUI

- [ ] Add `/compact` to run explicit settled-session compaction and report
  whether context changed or already fit.
- [ ] Add `/model` to the command palette and open a searchable model selector.
- [ ] Show exact provider/model identity, current selection, authentication
  availability, and context window without overcrowding rows.
- [ ] Add `/thinking` and offer only levels supported by the active model.
- [ ] Make the model and thinking segments in the top-right header compact
  clickable controls with immediate hover feedback; primary-clicking a segment
  invokes the same registered `/model` or `/thinking` command path as keyboard
  activation rather than introducing separate selector state or behavior.
- [ ] Give header hit regions only their visible segment width, prevent click
  propagation into surrounding chrome, and preserve ordinary terminal text
  selection outside those controls.
- [ ] Test model-click, thinking-click, hover, non-primary clicks, and adjacent
  non-control header cells, including width-aware states where either segment is
  hidden.
- [ ] Update model and thinking together when model selection requires a clamp.
- [ ] Show a concise toast/status when Kit adjusts a restored or carried-over
  thinking level.
- [ ] Disable or reject configuration while the session is busy and preserve the
  user's selector state for retry.
- [ ] Apply the returned event-stream identity and authoritative configuration
  atomically; never patch local header text optimistically.
- [ ] Add `/session` details showing current model, thinking level, context
  pressure, cumulative token categories, and cumulative cost.
- [ ] Keep cumulative usage out of persistent header chrome.
- [ ] Add exact presentation and interaction tests following the project UI test
  conventions.

### 8. End-to-end acceptance tests

#### Model and thinking

- [ ] Create, close, and reopen a session with an explicit model/thinking pair.
- [ ] Change model while idle and verify the next provider request uses it.
- [ ] Change thinking while idle and verify the next provider request uses it.
- [ ] Reject changes during active parent work, direct bash, reload, and delete.
- [ ] Restore an unsupported saved level, clamp it, report it, persist it, and
  reopen without repeating the warning.
- [ ] With no saved level, choose medium when available and otherwise the lowest
  supported level/off.
- [ ] Run `/compact`, verify checkpoint/history preservation, reopen, and
  continue the session.
- [ ] Switch to a smaller-context model, compact first, and continue the session.
- [ ] Preserve message/turn identity and canonical history across replacement.
- [ ] Retry the same mutation ID without repeating compaction or changing the
  configuration revision twice.
- [ ] Reject a stale expected revision from a second client.
- [ ] Restart at each transition boundary and recover the committed choice.

#### Usage

- [ ] Aggregate input, output, cache-read, cache-write, reasoning, total tokens,
  and cost across ordinary turns.
- [ ] Aggregate every model cycle in tool loops.
- [ ] Handle provider errors, retries, aborts, and missing provider usage without
  double counting.
- [ ] Preserve totals after daemon restart and session runtime replacement.
- [ ] Preserve totals across compaction.
- [ ] Initialize a fork/handoff with the source cumulative total at its fork
  point and add later child usage to that one total.
- [ ] Prove later parent work does not change the child total.
- [ ] Preserve historical cost across model changes.
- [ ] Project the same authoritative total through snapshot, live update, and
  `/session` details.
- [ ] Keep context percentage behavior independent from cumulative totals.

### 9. Validation

- [ ] `gofmt -l .` prints nothing.
- [ ] `go build ./...`
- [ ] `go vet ./...`
- [ ] `go test ./...`
- [ ] `go test -race ./...` on supported platforms because runtime replacement,
  event-stream replacement, and shared session configuration are
  concurrency-sensitive.
- [ ] Run authenticated provider smoke tests for model and thinking selection.
- [ ] Update `docs/parity.md` statuses only after behavior and verification are
  complete.

## Out of scope for this plan

- Displaying inherited and newly incurred fork usage separately.
- Persistent cumulative usage in Kit's session registry.
- Always-visible cumulative token/cost header chrome.
- Repricing historical turns when model catalog prices change.
- Allowing model/thinking mutation during an active parent turn.
