# 0007: Buffer assistant transcript rendering

## Status

Accepted

## Context

Kit's session protocol carries incremental assistant text so clients can
observe active execution and assemble completed messages. The transcript must
remain a stable conversation record: users should not begin reading an
unfinished answer, Markdown should not repeatedly reflow, and partial output
must not look committed before the assistant message has settled.

## Decision

Kit transports assistant text deltas, but clients do not render
pending assistant text as transcript prose. A client buffers the deltas by
message identity and reveals the assembled assistant message atomically when it
observes that message's completion.

The following activity may remain live while an assistant message is pending:

- thinking summaries;
- planned and running tool calls;
- tool progress and terminal results; and
- fixed-slot run, retry, compaction, and abort status.

Pending assistant text is not shown in the transcript or Activity prose. A
pending assistant message with no thinking or tool activity has no transcript
row; the fixed run-status slot provides feedback instead. Once complete, its
prose is rendered normally as Markdown. If a client cannot reconstruct a
completed message coherently, it resynchronizes from the authoritative droid
snapshot rather than displaying a known-partial answer.

This is a renderer policy. Droids streams provider-neutral text events, and
Kit's session protocol carries bounded text deltas. Neither droids nor the
server suppresses data based on TUI presentation policy.

## Consequences

### Positive

- The transcript remains a stable, durable conversation record.
- Users do not read text that may still change or end in failure.
- Markdown parsing and layout do not repeat for every text delta.
- Thinking and tool execution still communicate useful live progress.
- Other clients retain the underlying stream and can implement the same
  completion boundary without coupling it to the native renderer.

### Trade-offs

- Assistant prose appears later than the first provider text token.
- Clients retain bounded in-progress text until message completion.
- Completion and resynchronization paths must not accidentally promote an
  incomplete buffered message into transcript prose.

## Related

- [0001: Native Go application architecture](./0001-native-go-architecture.md)
- [0006: Make droids authoritative for session conversation data](./0006-droids-as-session-data-authority.md)
- [`../droids-sdk.md`](../droids-sdk.md)
