# Plugin status footer contributions

## Summary

Formalize an app-owned way for plugins to contribute structured status items to
Kit's shared status footer.

## Why

Footer status should stay consistent with Kit's layout, theme, and interaction
patterns while still allowing plugins to expose useful runtime state.

A small structured contribution API avoids ad hoc footer wiring and keeps
rendering owned by the shell.

## Direction

Provide a plugin registration surface for status footer items.

The shell should render those items from an app-owned registry instead of
accepting arbitrary plugin JSX.

## Scope

Initial target:

- status footer contributions

Out of scope for now:

- header contributions
- arbitrary plugin-rendered chrome
- general plugin capability querying from shell components

## Design constraints

- keep rendering app-owned
- avoid coupling shell components to concrete plugin classes
- avoid arbitrary plugin-owned JSX as the first API
- prefer structured status items over custom rendering
- registrations should clean up with the plugin lifecycle and `/reload`

## Preferred shape

In plugin context, provide a registration API such as:

- `status.registerItem(...)`

Exact API is TBD, but structured items should likely include fields like:

- `id`
- `label`
- `tone` / semantic color
- `priority` / order
- visibility rules

## Suggested rollout

1. Add a small status item registry owned by the app shell.
2. Expose registration through plugin context.
3. Render registered items in the footer using existing theme tokens.
4. Migrate concrete footer status needs onto the registry.
