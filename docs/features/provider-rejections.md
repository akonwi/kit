# Provider rejections

When a provider refuses a request, Kit shows what the provider said next to its
own description of the failure:

```text
Provider rejected the request: Upstream request failed: Model is unavailable. (HTTP 400)
```

The first part is Kit's fixed description of the kind of failure, such as
"Provider rejected the request" or "Provider access is not entitled for this
model". After the colon comes the provider's own explanation, and in
parentheses the HTTP status of its response. A failure that did not come from
a provider response, such as a lost connection, shows only Kit's description.

The explanation is the user's guide to recovery. Kit does not retry a rejected
request, because the same request would be rejected the same way; images, for
example, are prepared under each provider's published limits
([ADR 0037](../adrs/0037-prepare-model-images-through-provider-image-policies.md)),
and an upstream with stricter, undocumented limits says so only here. Switching
models, compacting, or changing the prompt are typical next steps.

The terminal client, the macOS app, and print mode (`kit print`) show the same
text. It is stored with the failed reply, so it remains after reloading the
session or restarting the server.

Provider text is untrusted. Kit keeps only the explanation fields of a
provider's error response, not the whole body, and makes them safe to show:
one line of at most 300 bytes without control or bidirectional characters.
It does not otherwise change what the provider said. The full SDK rule is
described under provider errors in [`docs/droids-sdk.md`](../droids-sdk.md).
