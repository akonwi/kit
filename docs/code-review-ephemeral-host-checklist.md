# Code review ephemeral host checklist

Status: active working checklist

## Goal

Treat `/code-review` as an ephemeral, per-process browser surface for now, and improve:

- launch reliability
- recovery from port collisions
- browser reconnect behavior
- TUI footer visibility
- TUI error feedback

Not in scope for this checklist:

- shared broker across Kit processes
- cross-process session switching
- tab-count tracking
- making `AgentRuntime` explicitly aware of code-review internals

## Agreed implementation constraints

- Recover from port collisions by searching for the next available port.
- Track only whether a browser client is connected, not number of tabs.
- Keep code-review status in plugin/feature state, not runtime state.
- Prefer a footer indicator for TUI visibility.
- Extra debugging UI is optional and deferred.

## Checklist

### 1. Host lifecycle and status model
- [x] Define plugin-owned host status model
  - server state: `idle | starting | ready | error`
  - port
  - browser connected: boolean
  - launch in flight: boolean
  - last error
- [x] Expose `getStatus()` and `subscribeStatus()` from the browser host
- [x] Emit status changes on startup, ready, disconnect, and failure

### 2. Port collision recovery
- [x] Keep deterministic preferred port selection
- [x] On `EADDRINUSE`, scan forward for next available port
- [x] Bound the search window and surface failure clearly if exhausted

### 3. Browser host readiness
- [x] Expose lightweight `/health` endpoint
- [x] Expose `/state` snapshot endpoint
- [x] Wait for host readiness before opening browser
- [x] Surface readiness timeout as a TUI error

### 4. Browser connection resilience
- [x] Retry websocket connection with backoff
- [x] Fall back to `/state` snapshot fetch when websocket is unavailable
- [x] Keep refresh working in degraded mode
- [x] Show clear browser connection states
  - `Connecting…`
  - `Waiting for Kit…`
  - `Connected`
  - `Reconnecting…`
  - `Snapshot only`
  - `Connection error`

### 5. TUI visibility and feedback
- [x] Add compact code-review indicator to footer
  - `review off`
  - `review starting`
  - `review ready :PORT`
  - `review connected :PORT`
  - `review error`
- [x] Emit TUI errors for meaningful startup/host failures
- [x] Avoid noisy connect/disconnect spam

### 6. Plugin integration
- [x] Keep code-review status in plugin state
- [x] Subscribe plugin to browser-host status changes
- [x] Feed footer rendering from plugin-owned status
- [x] Keep runtime interaction limited to existing error/info emission

## Implementation order

1. Host lifecycle and status model
2. Port collision recovery
3. Browser readiness checks
4. Browser reconnect + snapshot fallback
5. Footer status indicator
6. TUI error feedback polish

## Notes

- This checklist is intentionally for the ephemeral per-process approach.
- If `/code-review` stops being ephemeral, revisit the architecture in favor of a shared broker/service.
