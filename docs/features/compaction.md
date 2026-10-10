# Compaction model

By default, Kit uses each session's active model to summarize context during
compaction. To use a dedicated model instead, set `compactionModel` in
`$KIT_HOME/settings.json` to an exact `provider/model-id` selector:

```json
{
  "compactionModel": "anthropic/claude-sonnet-4-6"
}
```

The model must exist in Kit's configured model catalog and have usable provider
credentials. The setting also applies to subagent compactions. Remove the field
to restore the default behavior of using each session or subagent's active
model. A changed setting takes effect when a runtime is next opened or recreated.

A matching `modelOverrides` entry, when present, also applies to the compaction
model's context window.
