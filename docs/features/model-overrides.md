# Model overrides

Kit supports per-model overrides in `$KIT_HOME/settings.json` under the
`modelOverrides` key. Overrides are keyed by canonical model selectors in
`provider/model-id` form. The only supported override today is `contextWindow`,
the context budget Kit uses for that model.

## Settings format

```json
{
  "modelOverrides": {
    "openai-codex/gpt-5.6-sol": { "contextWindow": 1000000 }
  }
}
```

Validation is structural only:

- Selector keys must be `provider/model-id` (a `/` that is neither the first
  nor the last character). Malformed keys are dropped on load.
- `contextWindow` must be a positive integer. Non-integer, zero, negative, or
  non-numeric values are dropped on load.
- Kit does not check the value against provider or model capability limits;
  providers may still reject out-of-range values at request time.

## Editing from the settings dialog

Open the command palette and choose `/settings`, then activate the
**Model Context Windows** row. Pick a model from the filterable list — models with
an existing override show a check mark and their current limit — then enter a
positive integer for `contextWindow`. Leave the input blank to clear the override
for that model. Changes are saved immediately and apply to newly opened runtimes or after `/reload`.

In the native `/model` picker, overridden values appear as the model's context
size. Press `Ctrl+O` on a model to edit its context window; submit an empty value
to clear the override.
