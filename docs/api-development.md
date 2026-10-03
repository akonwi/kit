# Developing the Kit session API and client SDKs

Kit has one session protocol and multiple client implementations. A capability is
complete only when the canonical contract, server, maintained clients, generated
artifacts, and compatibility checks agree.

This guide is procedural. The architectural decisions remain authoritative:

- [ADR 0031](adrs/0031-publish-go-defined-openapi-contract.md): Go-defined OpenAPI contract
- [ADR 0032](adrs/0032-model-protocol-variants-as-discriminated-unions.md): closed protocol unions
- [ADR 0034](adrs/0034-use-one-error-body-for-the-session-api.md): declared errors
- [ADR 0035](adrs/0035-describe-server-push-streams-in-the-session-contract.md): SSE streams
- [ADR 0038](adrs/0038-expose-a-typed-go-client-api.md): public stateful Go client

## Ownership

| Concern | Owner |
| --- | --- |
| Wire records, enum values, validation | `api/contract` |
| Operation method, path, parameters, responses, errors, and stream metadata | `internal/httpapi` catalog |
| Committed language-neutral contract | `api/kit-session.openapi.json` |
| Server behavior | `internal/server` and the owning domain package |
| Go transport mechanics | `internal/clienttransport` |
| Public Go behavior and state | `api.Client`, `api.Session`, and public handles |
| Swift generated transport/types | `apps/macos/Sources/Kit/GeneratedSources` |
| Swift semantic behavior and projections | `apps/macos/Sources/Kit/Client` |

There must be one canonical wire record and one production Go client behavior.
Do not add parallel request records, raw HTTP calls in maintained clients, or a
second reconnect/cache implementation.

## Adding a capability

### 1. Decide whether the wire contract changes

A Go convenience method that composes existing operations may require only a
public Go API change. A new route, field, enum value, error, event, or payload
changes the wire contract and must follow the remaining steps.

For wire changes:

1. Add or update records and validation in `api/contract`.
2. Add or update the typed operation in the `internal/httpapi` catalog,
   including every success response and declared error.
3. For SSE, declare record names, payload schemas, resumability, and the encoded
   record bound as required by ADR 0035.
4. Implement the server handler through the catalog. Do not add an unregistered
   route or hand-maintained duplicate path.
5. Decide compatibility explicitly. Additive optional fields and operations are
   normally protocol-compatible. Removing or reinterpreting fields, tightening
   accepted shapes, and changing required values or union behavior are breaking
   and require a `SessionProtocolVersion` bump.

Use stable error codes and closed discriminated unions. Clients must not infer
behavior from error strings or unknown union shapes.

### 2. Update the public Go client

Add the typed transport operation under `internal/clienttransport`, then expose
client-owned behavior through `api.Client`, `api.Session`, or a public handle.
Public capabilities are direct methods rather than optional capability facets.

Keep these ownership rules:

- `api.Client` owns connection compatibility, lifetime, and concurrent sessions.
- `api.Session` owns snapshot caching, mutation serialization, reconciliation,
  replay, resynchronization, and reconnect policy.
- caller cancellation detaches; it never implies turn or bash abort;
- malformed or undeclared responses become `ProtocolError`;
- declared failures become `ServerError` with stable codes;
- unsupported direct capabilities return the public unsupported error.

Test client-owned state and recovery with fake typed transports. Add focused
adapter tests only for mechanics not guaranteed by the shared catalog, such as
headers, redirects, SSE framing, cancellation, and bounded streaming content.
Do not add a duplicate production-server conformance suite.

If a new contract type must be convenient from the parent package, regenerate
and verify aliases:

```sh
go generate ./api
go test ./api/...
```

### 3. Regenerate the OpenAPI document

From the repository root:

```sh
go run ./internal/httpapi/openapi/cmd/emit
go test ./internal/httpapi/openapi
```

Commit `api/kit-session.openapi.json` with the source changes. Review its diff,
not merely the generator result. CI compares it with the released contract and
requires a protocol bump for breaking changes.

### 4. Update the Swift client

Regenerate the committed, tag-filtered Swift transport and types:

```sh
apps/macos/script/generate_openapi.sh
apps/macos/script/generate_openapi.sh --check
```

Generated code is only the wire layer. Update `apps/macos/Sources/Kit/Client`
when the capability needs semantic projection, validation not expressible in
OpenAPI, state caching, retries, replay, or UI-facing behavior. Use the generated
operation and schema types rather than introducing a raw URLSession route.

Add focused Swift tests for the generated-operation integration and handwritten
behavior. Then run:

```sh
cd apps/macos
xcodebuild -scheme Kit -destination 'platform=macOS' \
  -derivedDataPath .build/xcode -skipPackagePluginValidation test
```

The live-server smoke suite is optional and must use isolated state as described
in [`apps/macos/README.md`](../apps/macos/README.md). It is not a substitute for
contract and client-owned behavior tests.

### 5. Update maintained callers and documentation

Use the new direct API in the native TUI, print mode, and macOS client where
applicable. Do not restore imports of retired internal client stacks or add
caller-owned reconnect loops.

Document public lifecycle, cancellation, error, or compatibility behavior when
it changes. Add outstanding or deferred client work to `backlog/`, not to an
ADR or an informal checklist.

The semantic browser client is not maintained and is updated only when work on
it is explicitly requested.

## Definition of done

- [ ] Canonical records and validation are updated in `api/contract`.
- [ ] The typed operation and all declared errors/stream metadata are catalogued.
- [ ] The server implements the catalogued operation.
- [ ] The public Go client exposes the capability at the correct ownership layer.
- [ ] Go state/recovery and necessary transport mechanics have focused tests.
- [ ] Parent-package aliases are regenerated when required.
- [ ] The committed OpenAPI document is regenerated and reviewed.
- [ ] Swift generated sources pass their staleness check.
- [ ] Handwritten Swift behavior/projections and tests are updated when needed.
- [ ] Maintained callers use the public capability without raw transport logic.
- [ ] Compatibility is additive or accompanied by a protocol-version decision.
- [ ] Formatting, build, vet, isolated Go tests, and relevant race tests pass.
- [ ] macOS tests pass for changes affecting the Swift client.
