# Code review browser connection retry

## Context

The `/code-review` browser prototype can fail to connect if the local host is not ready yet, if the preferred session port is already in use, or if the websocket drops while the browser UI is open.

Previously:

- the host tried exactly one deterministic port per session
- launch opened the browser immediately after starting the host
- the browser UI relied entirely on a websocket connection for state
- reconnect logic only retried the websocket after a disconnect

That made startup and reconnection fragile.

## Decision

Make the code review browser connection path resilient in both the host and the browser client.

### Host

- Keep a session-based preferred port, but retry a small sequence of nearby ports when the preferred port is unavailable.
- Expose lightweight `/health` and `/state` endpoints alongside the websocket endpoint.
- Wait for the local host health check to succeed before opening the browser.

### Browser client

- Continue using the websocket as the primary live-update channel.
- Retry websocket connection with backoff.
- When websocket connection is unavailable, fetch `/state` as a fallback so the review UI can still render the latest snapshot.
- Show connection status that distinguishes normal connection from degraded snapshot-only recovery.

## Consequences

- `/code-review` becomes more tolerant of port conflicts and startup timing races.
- The browser UI can still load review state even while live websocket updates are reconnecting.
- The actual browser URL may use a nearby fallback port instead of the session's first-choice port.
