# 0013: Resilient code review browser connection

## Status
Accepted

## Context

The `/code-review` browser flow can fail if the local host is not ready yet, if the preferred session port is already in use, or if the websocket drops while the browser UI is open.

Previously:

- the host tried exactly one deterministic port per session
- launch opened the browser immediately after starting the host
- the browser UI relied entirely on a websocket connection for state
- reconnect logic only retried the websocket after a disconnect

That made startup and reconnection fragile.

## Decision

Make the code review browser connection path resilient in both the host and the browser client.

### Host

- keep a session-based preferred port, but retry a small sequence of nearby ports when the preferred port is unavailable
- expose lightweight `/health` and `/state` endpoints alongside the websocket endpoint
- wait for the local host health check to succeed before opening the browser

### Browser client

- continue using the websocket as the primary live-update channel
- retry websocket connection with backoff
- when the websocket connection is unavailable, fetch `/state` as a fallback so the review UI can still render the latest snapshot
- show connection status that distinguishes normal connection from degraded snapshot-only recovery

## Consequences

- `/code-review` becomes more tolerant of port conflicts and startup timing races
- the browser UI can still load review state even while live websocket updates are reconnecting
- the actual browser URL may use a nearby fallback port instead of the session's first-choice port

## Related

- `docs/features/code-review-web.md`
