# Plugin chrome and capabilities

## Summary

Generalize the current plugin-shell integration idea beyond the footer.

The real need is an app-owned way for shell components to read plugin-owned
state/capabilities and for plugins to contribute structured status items to
shared shell chrome such as the header and footer.

## Why

Current header/footer integration is inconsistent:

- `HeaderBar` directly imports feature-global state for code review
- shell components cannot query plugin-owned state through the component tree
- footer-specific thinking is too narrow because the same problem already exists
  in the header

## Direction

Introduce an app-owned shell chrome/capabilities surface that is available in
the component tree.

This should support two related needs:

1. **Query plugin-owned state/capabilities**
   - shell components should be able to read plugin-derived state without
     importing feature-global stores
2. **Contribute structured chrome items**
   - plugins should be able to register status items for shared shell chrome
     slots such as header and footer

## Scope

Treat this as a shared shell chrome problem, not a footer-only problem.

Initial target areas:

- header
- footer

Possible later areas:

- composer-adjacent status
- pending area
- other shell widgets/slots

## Design constraints

- keep rendering app-owned
- avoid coupling shell components to concrete plugin classes
- avoid arbitrary plugin-owned JSX as the first API
- prefer structured status items over custom rendering at first

## Preferred shape

### In plugin context

Provide an app-owned registration surface for shell chrome and/or capabilities.

Examples of concepts, not final API:

- `chrome.registerItem(...)`
- `capabilities.set(...)`

### In the component tree

Expose a read-only view/facade that shell components can query.

Examples of concepts, not final API:

- `useChromeSlot("header-right")`
- `useChromeSlot("footer-left")`
- `usePluginCapability("code-review")`

## Structured item bias

Prefer structured contributions first, e.g. items with fields like:

- `id`
- `slot`
- `label`
- `tone` / `color`
- `priority` / `order`
- visibility rules

That keeps layout, theme, and rendering consistent across plugins.

## Suggested rollout

1. expose a read-only plugin chrome/capabilities view in the component tree
2. move code review header state off direct global-store imports
3. support footer contributions through the same registry
4. expand to richer slots only if concrete needs appear
