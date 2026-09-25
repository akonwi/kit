# 0024: Unify active-context compaction

## Status

Accepted

## Context

A droid has immutable diagnostic history and a replaceable active context. The
history preserves accepted input, provider responses, tool calls and results,
boundaries, attachments, metadata, and lifecycle records. The active context is
the validated representation supplied to the next provider request.

Compaction must reduce active context without rewriting history. Automatic
pressure, provider-confirmed overflow, explicit compaction, and target-model
adaptation must share one state machine. The retained tail must remain
replayable when a target model has a smaller effective budget or different
provider requirements.

A provider-native replay is not a suitable summary request. It can carry
thinking, signatures, opaque provider metadata, and tool output, or cause the
summary model to continue an old tool interaction. Summary generation therefore
uses an explicit textual projection and a separate previous-summary input.

## Decision

Droids uses one compactor for automatic compaction, overflow recovery, explicit
settled-context compaction, and target-model adaptation. Existing trigger,
operation-receipt, recovery, and Store-commit paths call this same compactor.

### Selection and safe boundaries

The compactor targets an approximately 20,000-token estimated suffix. This is a
built-in target, not a hard minimum, user setting, or universal context limit.
Smaller models, current-target fit, or replay requirements may advance the
cutoff at a complete boundary and retain a shorter suffix. `Force` may summarize
a non-empty prefix even when the active context is shorter than the target.

A cutoff may occur inside a user turn, including between completed model cycles.
It must never split an assistant tool-call message from its complete tool-result
batch. Prefix and suffix selection, and later incremental source chunks, keep
that assistant/tool-result group together. The retained suffix is copied into
the replacement context unchanged. During execution, newly admitted input and
the latest tool exchange not yet consumed by a model response are protected;
compaction fails rather than summarizing away their evidence if they cannot fit.
Settled adaptation may use a summary-only replacement when replay requires it.

### Textual summary requests

Every summary step is a fresh request with one textual user input, not native
conversation replay. The default prompt requests plain text under these
headings:

```text
## Goal
## Constraints & Preferences
## Progress
## Key Decisions
## Next Steps
## Critical Context
```

The summary projection retains user and assistant text, tool names and
arguments, tool error/success status, and boundary/attachment descriptions. It
omits thinking, opaque provider or message metadata, and tool output only from
summary input. Canonical history and the unchanged retained suffix keep their
original representations.

A prior checkpoint summary is supplied intact through a separate
previous-summary update input. It is not folded into provider-native replay.
Repeated compaction folds that summary with the messages retained by the prior
checkpoint that now enter the selected prefix, then with the new source prefix.

### Bounded incremental summarization

If the prepared textual prefix fits the summary model's actual metadata input
and context limits, the compactor makes one summary request. Otherwise it:

1. partitions the prefix into ordered chunks of complete source messages at safe
   boundaries;
2. sends sequential fresh requests, each folding the previous summary with the
   next serialized complete source-message chunk; and
3. keeps intermediate summaries as operation state and installs only the final
   summary with the unchanged suffix.

Every request must fit the model's actual limits. If even one complete source
message cannot fit with the previous summary and prompt, compaction fails
without a checkpoint. It never splits or truncates user text, assistant text,
or source data. Candidate selection reserves summary headroom before generation.
If the generated summary requires a shorter suffix, its measured size guides the
next cutoff and its content is carried forward rather than regenerating the
same prefix. Paid candidate attempts are bounded. All observed summary-response
usage is accounted through the existing usage rules, even when a later chunk or
final commit fails.

### Validation and durable replacement

A summary must be non-empty, complete textual output. The canonical
`StopReasonStop` result is required; `StopReasonLength`, empty output, tool
calls, or other incomplete output rejects the operation. The existing
conservative estimator bounds each prepared request. The replacement must also
pass current-target and requested-target replay validation.

On success, active context becomes the final summary plus the unchanged suffix.
Diagnostic history remains immutable. The final checkpoint, context update,
completion events, and applicable receipt use the existing atomic Store commit;
the final context is not used before that commit. The commit checks both source
context and the identity of the request configuration used for validation. A
concurrent reconfiguration invalidates the prepared replacement instead of
installing a checkpoint against obsolete budgets. Failure leaves the old active
context and checkpoint in place. Summary usage, ambiguous-commit
reconciliation, restart recovery, and target validation retain their existing
semantics.

## Compatibility and scope

This decision keeps the existing compaction prompt/model overrides and APIs. It
adds no settings, public summary schema, wire/protocol values, storage version,
or post-turn scheduling. File-tracking metadata and summary retry promises are
outside this contract. The droid's configured model does not change as a side
effect of compaction.

## Consequences

All compaction triggers have one safe selection and summary path. A small model
or an oversized single source message can produce a typed compaction failure
with no checkpoint rather than truncating source material. The approximately
20,000-token suffix remains a heuristic: target fit, replay requirements, and
forced compaction may produce a shorter suffix.

Incremental requests avoid arbitrary global caps while preserving complete
source-message and assistant/tool-result units. Only the final summary becomes
active context, while every observed summary response remains represented in
cumulative usage.

## Related

- [ADR 0004: Model a droid as an autonomous agent runtime](0004-droids-agent-runtime-boundary.md)
- [ADR 0005: Fork settled droid conversations semantically](0005-droids-semantic-forking.md)
- [ADR 0006: Make droids authoritative for session conversation data](0006-droids-as-session-data-authority.md)
- [ADR 0010: Compose session-scoped system prompts from Kit-owned guidance](0010-compose-session-system-prompts.md)
- [ADR 0015: Configure model-specific context windows in user settings](0015-model-specific-context-windows.md)
- [ADR 0016: Pass resolved models to Droids](0016-pass-resolved-models-to-droids.md)
- [Droids API and SDK specification](../droids-sdk.md)
