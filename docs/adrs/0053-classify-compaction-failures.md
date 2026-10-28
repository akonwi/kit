# 0053: Classify compaction failures

## Status

Accepted.

## Context

Context compaction can fail in many ways: the summary model stops at its output
limit, returns no usable summary, or is stopped by the provider's policy; the
provider request fails; the conversation cannot be reduced to fit the model;
the operation is canceled or its runtime closes; the context or its
configuration changes underneath it; or persistence fails
([ADR 0024](0024-unify-context-compaction.md)).

Clients must be able to tell the user which of these happened, live and after
reconnecting, so the user knows whether to retry, change models, or shorten the
conversation. The underlying error text is not safe to show or send: it can
quote provider responses, which may reflect prompt content or request data.

## Decision

### A closed vocabulary

Every compaction failure has exactly one reason:

| Reason | Meaning |
|---|---|
| `summary_truncated` | The summary stopped at the compaction model's output limit. |
| `summary_invalid` | The compaction model returned no usable summary: empty text, tool calls, or another incomplete stop. |
| `policy_stop` | The provider stopped the summary under its usage policy. |
| `provider_error` | A request to the provider failed: a summary request or a context measurement, including the provider's own timeouts. |
| `not_adaptable` | The conversation cannot be reduced to fit the target and current models, or the compaction model cannot accept the material. |
| `canceled` | The compaction was canceled or timed out, or its runtime closed, before it finished. |
| `conflict` | The compaction was refused as busy, or the context or its configuration changed while it ran. |
| `internal_error` | Any other failure, such as persistence. |

Droids classifies a failure where it occurs and returns it from
`CompactContext` as a `CompactionError`. The compaction's own context decides
cancellation: a failure after it was canceled is `canceled`, whatever a
provider reported, while a provider's timeout during a live compaction is
`provider_error`.

### Where the reason lives

- **Persisted.** Droids records the reason on the durable `compaction.failed`
  event and, for a turn that compaction failed, on the turn's error
  (`compaction_failure`). The turn record survives restarts, so
  `GET …/turns/{turnID}` explains the failure after a reconnect or a server
  restart.
- **Live.** A failed `compaction.completed`, inside or outside a turn,
  carries `compactionFailureReason` and no error text. A failed
  `turn.completed` carries `compactionFailureReason` when compaction failed
  the turn, alongside its usual error message.
- **Turn status.** `TurnInfo` and `PromptOutcome` carry
  `compactionFailureReason` on a failed turn.
- **Operations.** `compactSession`, and `configureSession` when a model change
  needs compaction, answer a failed compaction with HTTP 422 and the error code
  `compaction_failed`, whose details are `{"reason": …}`. A compaction
  refused as busy or conflicting, or canceled by its request or a closing
  session, is not answered as `compaction_failed`; its live event still
  carries the reason.

The server maps an undeclared reason to `internal_error` before it sends it,
and checks every event against the rules clients apply. Clients reject a
reason they do not declare, and a reason on any other event or on a turn that
did not fail. Failures recorded without a reason remain unclassified: a turn
keeps its stored error message, and a live failure projects as
`internal_error`.

### What never crosses

The reason is the only explanation that leaves the server. Provider messages
and HTTP bodies, provider error codes and request IDs, credentials, prompt and
summary content, and Go error text stay in the server log.

### Clients

Clients word each reason themselves. The terminal client and print mode share
one table, and the macOS app keeps its own with the same sentences. A failed
turn reads "Context compaction failed." followed by the reason's sentence. A
failed explicit compaction or model change shows the sentence, and the macOS
app offers Retry only for reasons that might not repeat: not for
`summary_truncated`, `policy_stop`, or `not_adaptable`.

## Consequences

- Users see why compaction failed without provider text reaching any client.
- Session protocol 50 carries the reason. The `compaction_failed` code replaces
  `unprocessable` for these operations, and `compaction.completed` no longer
  carries an error message; both are part of protocol 50's breaking change.
- Adding a reason is a protocol change, because clients decode the vocabulary
  strictly.
- A compaction outside any turn is explained live and in its operation's
  response; snapshots do not keep a history of past compaction failures.
