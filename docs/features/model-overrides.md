# Model overrides

Kit supports per-model overrides in the server's `$KIT_HOME/settings.json`
under the `modelOverrides` key, one of the [server settings](server-settings.md)
clients change through the session API. Overrides are keyed by canonical model selectors in
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

Through the API, an override must name a model in the server's catalog, and
its `contextWindow` is at most 100,000,000.

## Editing from the terminal client

In the `/model` picker, overridden values appear as the model's context size.
Press `Ctrl+O` on a model to edit its context window on the attached server;
submit an empty value to clear the override. Changes are saved immediately and
apply to newly opened runtimes or after `/reload`.
