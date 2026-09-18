# 0012: Route model-user interactions through the session

## Status

Accepted

## Context

A model sometimes needs one focused decision or several structured clarifications
before it can continue safely. Asking for those answers in ordinary chat loses
type information, makes constrained choices harder to answer, and cannot provide
a consistent cancellation result.

An interaction may outlive any one client attachment. Multiple clients may be
attached to the same session, and the session continues running when a client
detaches. Interaction ownership therefore cannot reside in a TUI widget or a
client connection.

The user often needs the preceding transcript to answer well. A modal that hides
or disables that context encourages the model to repeat its explanation and
makes the interaction harder to complete in a terminal.

## Decision

### Model tools

R1 exposes these built-in tools to the primary agent of an interactive session:

- `confirm_from_user` requests one boolean decision;
- `input_from_user` requests one short freeform answer;
- `select_from_user` requests one choice from a bounded list; and
- `guided_questions` requests a structured questionnaire whose questions are
  presented one at a time.

The tools are not exposed to subagents in R1. A subagent communicates a need for
user input to its parent through the normal supervision mailbox.

Kit adds concise tool-selection guidance to the primary agent system prompt only
when the tools are available. The guidance prefers confirmation for one boolean
decision, input for one freeform value, selection for one known choice, and
guided questions for multiple missing inputs. A guided questionnaire may contain
one question for compatibility, but the model guidance prefers it when two or
more answers are needed.

Print-only execution does not expose these tools or their prompt guidance. An
interactive daemon session retains an invoked request while no capable client is
attached so a later attachment can answer it.

### Canonical requests and results

Every request has a stable opaque identity, its originating session, run and
tool-call identities, a creation order, a kind, and a bounded kind-specific
payload. The session rejects malformed, oversized, or unsupported requests
before publishing them to clients.

The model-facing result details are:

```text
confirm   { confirmed: boolean }
input     { value: string | null, cancelled: boolean }
select    { value: string | null, label: string | null, cancelled: boolean }
guided    { cancelled: boolean, answers: object, answeredCount: integer,
            totalQuestions: integer, completed?: boolean }
```

Cancelling confirmation preserves the established model-facing
`{ confirmed: false }` shape. Internally, Kit retains whether the resolution was
an explicit negative answer, user cancellation, run abort, unavailable
interaction, or shutdown. Other cancelled interactions return their explicit
cancelled shape. Cancelling guided questions discards partial answers.

`select_from_user` options have a display label, a stable model-facing value,
and optional short detail. Values must be unique. Transport option identities
are opaque and are mapped back to the server-owned option rather than trusted as
model-facing values.

Guided questions have unique bounded identifiers and one of `text`, `select`,
`multiselect`, or `boolean` as their kind. Questions are required by default.
Required questions must be answered before advancing or completing; optional
questions have an explicit skip operation. Select and multiselect answers must
reference declared options. Unknown question or answer identifiers are invalid.
Labels such as `Other` and `Skip` have no implicit behavior. Support for an
open-ended other value requires a future explicit schema field.

The server defines and enforces limits for pending requests per session, total
request bytes, title and detail bytes, answer bytes, option counts and sizes, and
guided question counts. Those limits are represented in server capabilities
where clients need them and are tested at both sides of the protocol boundary.

### Session ownership and settlement

The authoritative session owns pending interactions, their deterministic FIFO
order, validation, and settlement. Interaction tools execute sequentially so a
single model tool batch cannot create an ambiguous set of simultaneously active
questions.

A pending request is projected to every capable client attached to that session.
The first valid response settles it atomically. Invalid, duplicate, stale, and
late responses fail without settling that or another request. Resolution is
published to every client so all presentations dismiss the request. A snapshot
or reconnect replay includes all pending requests in their canonical order.

Detaching a client or switching the session shown by one client does not cancel
a request. Aborting the originating run, deleting the session, or shutting down
the server cancels it and unblocks the tool. Pending requests need not survive a
server restart in R1; restart follows the existing interrupted-run recovery
rules rather than fabricating an answer.

### Native TUI presentation

The native TUI presents the oldest pending interaction in a dock spanning the
primary transcript column. The dock temporarily replaces the composer and its
pending-status row without adding a modal backdrop. It:

- leaves the transcript visible, selectable, and mouse-scrollable;
- owns keyboard focus while active;
- preserves the composer draft, cursor, attachments, and queued follow-ups;
- uses a measured, bounded share of terminal height and windows long content;
- shows queue position when more than one request is pending;
- provides visible keyboard and mouse actions for submission and cancellation;
- notifies the user when a new answer is needed without repeated alerts; and
- restores the composer state exactly after resolution or cancellation.

The terminal reports `feedback` only while the attached session's active parent
turn is blocked on an interaction. It returns to the appropriate running or idle
state after settlement.

## Consequences

### Positive

- The model receives typed, canonical answers without parsing conversational
  prose.
- Users retain the transcript context needed to make a decision.
- Interaction lifetime follows the session rather than a particular renderer or
  connection.
- Local and future remote clients resolve the same request under one validation
  and first-winner rule.
- Cancellation and malformed-input behavior are deterministic and testable.

### Trade-offs

- The server needs a bounded pending-interaction broker and protocol projection.
- A model run may remain blocked while no capable client is attached.
- Sequential interaction tools can hold the remainder of a tool batch until the
  user responds.
- Pending interaction recovery across server restart is outside R1 and tracked
  by `CORE-INT-003` in the [core backlog](../../backlog/core.md).

## Related

- [0001: Native Go architecture](./0001-native-go-architecture.md)
- [0004: Define the droids agent runtime boundary](./0004-droids-agent-runtime-boundary.md)
- [0011: Use SSE for session event delivery](./0011-use-sse-for-session-events.md)
- [`../../backlog/core.md`](../../backlog/core.md)
- [`../../backlog/tui.md`](../../backlog/tui.md)
