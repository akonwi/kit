# Reasoning effort history

Kit's existing thinking selector changes effort without rewriting the original
request-level `reasoning.effort` for supported public OpenAI GPT-6 standard,
single-agent Responses models: `gpt-6-astra`, `gpt-6-sol`, and `gpt-6-luna`.
Support is an explicit model capability, restricted to the public OpenAI
endpoint; a custom provider ID does not disable it. Custom gateway URLs,
Codex, other models/providers, and unimplemented provider execution modes do not
inherit this capability. Their existing effort behavior is unchanged.

## Request and durable state

Droids owns three distinct values for each active context:

- **Baseline:** the original optional request-level effort, including omission.
- **Requested:** the latest accepted selector value.
- **Effective:** the effort selected for the captured provider request.

An explicit selection before the first request becomes the baseline. Later
selections are persisted immediately and coalesce until dispatch. Before the
next new user message, Kit inserts one input item:

```json
{"type":"configuration_update","reasoning":{"effort":"high"}}
```

The request-level effort stays unchanged. A low → high → low conversation
therefore retains both updates at their original positions; returning to low
does not erase the high interval. Selecting the current effort is a no-op, and
selecting high then low before dispatch produces no update when low is already
effective. Kit never emits adjacent updates.

Updates wait for a new user-message boundary. A selection during a response
does not mutate its captured request and does not change intervening tool-result
continuations. Temperature compatibility uses **effective** effort, not the
baseline or a provider response's reported request-level effort.

The runtime persists message-ID anchors, dispatch position, and pending intent.
Immutable history records preserve dispatched baselines and updates separately
from user-visible messages. Restart validates and restores this state; forks
copy the exact prefix and pending selection without sharing mutable state.
`Spawn` restores durable intent rather than applying stale bootstrap settings.
Session owners explicitly reconcile their authoritative configuration after
opening. Model replacement activates its new epoch only after session metadata
commits and the old runtime shuts down; failed activation quarantines the runtime.

## Assessment and estimates

Configuration updates are separate provider input items, so replay assessment and
token estimates include them. Providers may implement `RequestReplayValidator` to
assess the exact request projection; the public Responses provider validates the
projected history and serialized input, while providers without it keep receiving
canonical messages. Dispatch, resume, context assessment, and compaction pressure
all assess the request Kit would send now: dispatched updates are included,
pending undispatched selections are excluded, and compacted candidate contexts or
other target models are assessed as fresh epochs without updates. Estimates add
each projected update item to the request byte estimate.

## Fresh contexts and compatibility

A model switch or Kit summary compaction establishes a fresh compatible
baseline. Old history remains durable, but old update items are not injected into
the new active context. Cross-model context preparation does not change the
active droid's configuration; replacement activation establishes the target epoch.
Kit's compaction is an independent text-summary request, not OpenAI automatic
compaction/truncation, `/responses/compact`, or `compaction_trigger`.

Older conversations without effort metadata retain their transcript and bootstrap
from their current optional setting. This can cause a one-time cache miss; Kit
does not force compaction or invent an earlier setting. An omitted baseline stays
omitted even after explicit updates. The selector requires an explicit supported
value; standalone callers cannot change an established explicit effective effort
back to an unspecified default within the same epoch. No invented `default` wire
value or automatic compaction is used to implement that transition.

Anthropic's managed-effort path was inspected and remains separate: it uses
`output_config` on synthetic system messages, adaptive thinking, and its own beta
headers. Those messages and settings are not OpenAI configuration updates.
OpenAI SDK v3.66.0 provides the typed configuration-update input union used by
the public Responses serializer. SDK retries remain disabled; Kit owns retries.

## Verification

Coverage includes exact update items and replay positions, baseline omission,
first explicit selection, coalescing/no-ops, pending restart/fork, post-dispatch
crash replay, immutable
in-flight requests, tool continuations, temperature, explicit and in-turn overflow
summary compaction, catalog-refresh capability preservation, legacy
bootstrap, cancellation rollback, corrupt-state rejection, capability gates,
SQLite-backed session reopen/model replacement without transcript loss, and
request-aware replay assessment plus update-inclusive token estimates.

Source: [OpenAI reasoning guide — Change reasoning mid-conversation](https://developers.openai.com/api/docs/guides/reasoning#change-reasoning-mid-conversation).
