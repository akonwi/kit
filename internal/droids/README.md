# Kit-local droids

This package is Kit's private agent core. It was seeded from
[`github.com/akonwi/droids`](https://github.com/akonwi/droids) commit
`69dc707` (`feat(provider): add OpenAI Codex support`).

It is intentionally copied into `internal/droids` rather than linked as a Go
module dependency while the native Kit rewrite is evolving. Kit may change its
API and behavior without coordinating an external droids release. There is no
automatic upstream synchronization contract; any future sync must be an
explicit, reviewed migration.

The package keeps droids' three visible layers:

```text
Storage      persistence seam
Droid/loop   bounded agent and tool loop
Providers    model routing and wire translation
```

Kit clients must not import this package. Only server-side runtime and adapter
packages may use it; stored records, wire messages, and renderer models remain
Kit-owned projections.
