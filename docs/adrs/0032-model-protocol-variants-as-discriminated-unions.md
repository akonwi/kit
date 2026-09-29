# 0032: Model protocol variant records as discriminated unions

## Status

Accepted

## Context

Several protocol records are variants selected by a `kind` field, notably
`SessionEvent`, `TranscriptContent`, and `SubagentLiveEvent`. Each kind carries
a different set of fields. When such a record is modelled as one struct holding
the union of all fields, the type system cannot say which fields a kind
carries. Every producer and consumer then re-derives that mapping, and
validation consists largely of rules forbidding fields that do not belong to a
kind.

ADR 0031 publishes the session API as an OpenAPI contract from which clients
generate their types. A contract that describes these records as one object
with dozens of optional fields gives generated clients no more safety than the
Go struct gives Go consumers.

## Decision

Kit models protocol variant records as discriminated unions in both Go and the
published contract.

### Wire layout

- A variant record is one flat JSON object. `kind` is the discriminator. Common
  fields and variant fields are siblings; there is no nested payload object.
- Field names and value encodings are unchanged by adopting this model. A
  record that is valid under the current protocol serializes to the same bytes.
  Changing required fields, identity fields, or kind vocabularies is a protocol
  change decided separately.

### Go representation

- A union is a struct containing its common fields and a `Payload` whose type
  is a sealed interface. Each kind has one payload type holding only that
  kind's fields.
- The union implements `MarshalJSON` and `UnmarshalJSON`. Decoding reads `kind`,
  then strictly decodes the common fields and the matching payload type from
  the same object, rejecting unknown fields and unknown kinds.
- Consumers use type switches over payload types. A test verifies that every
  declared kind has exactly one payload type and round-trips.
- An unrecognized kind is a decoding error in every client. Adding a kind is a
  protocol change.
- `Validate` checks common identity, each payload's own invariants, and
  cross-record invariants such as batch ordering. Rules that only forbid fields
  from other kinds disappear because payload types cannot carry them.
- Runtime and persistence types keep their own representations. The server
  projects them explicitly into protocol unions.

### Contract representation

- A union schema is a `oneOf` with a `kind` discriminator and an explicit
  mapping from every kind value to one variant schema.
- Each variant is a named component schema that the union references through
  its discriminator mapping. Anonymous variants are not used because generated
  clients cannot name them.
- Each variant schema is complete: the common fields, the variant fields,
  `kind` constrained to one value, and `additionalProperties: false`. Variants
  do not use `allOf` composition.
- A field is `required` when every valid record of that kind carries it on the
  wire. A field whose zero value the current protocol omits stays optional and
  declares its default.
- Where a container accepts only some kinds, the contract declares a narrower
  union listing only those variants.

### `SessionEvent`

Common fields are `streamId`, `sequence`, `sessionId`, and `kind`. Kinds fall
into three identity scopes. The table uses protocol-41 kind names; ADR 0033
defines their replacements, and Go payload types are named after the ADR 0033
concepts so that renaming wire values does not rename Go types.

| Scope | `turnId`, `runId` | Kinds |
|---|---|---|
| Run | Required and equal | `run.started`, `run.finished`, `message.user`, `assistant.started`, `assistant.text.delta`, `assistant.thinking.delta`, `assistant.completed`, `tool.planned`, `tool.started`, `tool.updated`, `tool.completed`, `compaction.started`, `compaction.completed`, `compaction.failed`, `provider.retry.scheduled`, `provider.retry.started`, `context.updated`, `usage.updated` |
| Session | Present as empty strings | `session.renamed`, `session.cwd.changed`, `scratchpad.changed`, `subagent.changed`, `peer_query.changed`, `annotation.created`, `annotation.updated`, `annotation.deleted`, `annotation.submitted` |
| Interaction | Present for run-owned requests; absent for plugin-owned requests | `interaction.requested`, `interaction.resolved` |

Tool result content on `tool.updated` and `tool.completed` uses the tool-result
content union. `interactionResolution` is a declared enum.

### `TranscriptContent`

Variants are `text`, `thinking`, `toolCall`, `image`, `file`, and
`annotations`. The contract declares role-specific unions for user, context,
assistant, and tool-result content. Containers whose role is fixed by the
schema reference the narrower union; `TranscriptMessage` references the full
content union, and its role-dependent restriction remains a semantic
validation rule.

### `SubagentLiveEvent`

A union requires a closed kind vocabulary. `SubagentLiveEvent` forwards the agent
core's open set of lifecycle kinds alongside its message and tool kinds, so it
remains a single record with an open `kind` until the protocol defines a closed
vocabulary for child events. At that point it adopts this model. Its `kind` is
not an enum while the vocabulary is open.

## Consequences

### Positive

- Go consumers and generated clients receive exactly the fields each kind
  carries, removing per-client re-validation of kind/field combinations.
- Validation code shrinks to semantic invariants.
- Adding a kind is an explicit, reviewable change to the union, its mapping,
  and the published contract.

### Negative

- Go consumers of these records change from field access on one struct to type
  switches.
- Custom JSON encoding must preserve strict decoding and byte-identical output
  and is covered by round-trip tests against recorded wire fixtures.
- `SubagentLiveEvent` remains loosely typed until the protocol closes its kind
  vocabulary.
