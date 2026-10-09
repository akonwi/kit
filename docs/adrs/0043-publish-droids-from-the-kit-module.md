# 0043: Publish droids from the Kit module

## Status

Accepted

## Context

ADR 0002 made droids private while Kit's native runtime and the agent API were
evolving together. The autonomous runtime boundary specified by ADR 0004 and
the public API described in `docs/droids-sdk.md` are now sufficiently explicit
for other Go applications to embed the runtime.

Keeping the implementation below Go's `internal` boundary prevents those
applications from using it. Moving the source back to the standalone droids
repository would restore coordinated cross-repository releases and would split
Kit's runtime changes from their primary integration and test suite.

## Decision

Publish the agent runtime from this repository at
`github.com/akonwi/kit/droids`. The public package family includes:

- `droids` for the runtime, domain types, providers, tools, and in-memory Store;
- `droids/sqlitestore` for the CGO-free SQLite Store;
- `droids/droidstest` for reusable Store test support;
- `droids/mcp` for MCP namespace tools and transport contracts;
- `droids/anthropicoauth` and `droids/openaicodex` for provider OAuth protocol
  helpers.

Packages below `droids/internal` remain private implementation details. Public
droids packages must not import packages below Kit's top-level `internal`
directory. In particular, domain identifier validation belongs in droids, and
the SQLite Store owns any compatibility decoding needed by its historical
migrations rather than importing Kit presentation packages.

Droids remains part of the `github.com/akonwi/kit` Go module rather than
becoming a nested module. Its compatibility and releases therefore follow the
Kit module's semantic version. While the module is pre-1.0, consumers should
pin a version and review release notes for API changes.

Kit's maintained clients continue to consume Kit's session protocol and client
projections. Publishing droids does not make runtime types part of those client
contracts or permit clients to bypass the authoritative server.

The source remains a deliberate fork of `github.com/akonwi/droids` from the
commit recorded in `droids/README.md`. There is no automatic synchronization
contract with the standalone repository.

## Consequences

Positive:

- other Go projects can embed Kit's tested agent runtime directly;
- Kit and droids still evolve atomically in one repository and test suite;
- the public boundary prevents accidental dependencies on Kit internals;
- storage, MCP transport, and credential persistence remain explicit adapter
  boundaries.

Trade-offs:

- Kit releases now publish a supported external Go API in addition to the
  executable;
- droids compatibility follows Kit's release cadence and pre-1.0 versioning;
- importing the root package includes its provider SDK dependency footprint;
- future package moves require normal public API compatibility consideration.

## Related

- [0001: Native Go application architecture](./0001-native-go-architecture.md)
- [0002: Internalize the droids agent core during the rewrite](./0002-internalize-agent-core.md)
- [0004: Model a droid as an autonomous agent runtime](./0004-droids-agent-runtime-boundary.md)
- [`../droids-sdk.md`](../droids-sdk.md)
- [`../../droids/README.md`](../../droids/README.md)
