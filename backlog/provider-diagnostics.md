# Provider diagnostics and catalog freshness

- Preserve an actionable, safely redacted provider rejection reason. OpenCode Go
  returned HTTP 400 with `Upstream request failed: Model is unavailable.` for
  `grok-4.5`, but the saved transcript only retained `Provider rejected the request`.
- Reconcile OpenCode Go model availability with its live model list so removals
  after an embedded catalog release do not leave unavailable models selectable.
  Deprecated entries are filtered, but embedded metadata alone cannot guarantee
  current availability or account entitlement.
