# 0039: Bound transcript responses by pagination

## Status

Accepted

## Context

Session snapshots, message pages, transcript pages, and subagent transcript
pages carry durable transcript messages. ADR 0013 and ADR 0038 bound these
responses by complete turns and message counts. A message's size is determined
by what the model and tools produced, so an additional encoded-size bound on
these responses cannot be satisfied by choosing a smaller page: it can only
reject or alter durable history.

## Decision

Responses that carry durable transcript messages are bounded by pagination,
not by encoded size:

- Clients buffer ordinary (non-stream) responses without a total size cap.
  The local daemon is authenticated, non-transcript responses are bounded by
  the server where they are produced, and transcript responses are bounded by
  pagination. Clients still bound request bodies and each record of a server
  push stream.
- Durable transcript projections carry complete content, including complete
  tool-call arguments. `argumentsTruncated` is set only on content derived from
  live events.
- Transcript validation checks structure and identity, not the encoded size of
  messages, content blocks, details, or pages.

Live session events stay bounded per event, per page, and in retained bytes
(ADR 0011, ADR 0035); a turn's complete content is available from the
transcript once it is durable. Inputs remain bounded where they are accepted,
for example prompts, attachments, and plugin tool results.

## Consequences

Clients render durable history exactly as stored and need no truncated-history
state or oversized-page recovery. A page that contains exceptionally large
messages is correspondingly large, and transfer and decoding cost grow with
that content. Message counts and complete-turn boundaries remain the only page
bounds.
