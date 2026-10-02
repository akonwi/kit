# 0037: Prepare model images through provider image policies

## Status

Accepted

## Context

Images reach model context from several independent sources: user
attachments, built-in tools such as `inspect_image`, MCP tool results, and
plugin tool results. Canonical history stores them provider-neutrally, and a
session may replay that history to a different provider or model at any time.

Provider image constraints differ in kind, not only in magnitude:

- **Anthropic Messages** rejects images over 10 MB base64-encoded or 8000 px on
  either side, limits a request to 100 images (200k-context models) or 600
  images (other models) and 32 MB, and applies a stricter 2000 px per-image
  limit to every image once a request carries more than 20 image blocks,
  including images from earlier turns and inside tool results. Below those hard
  limits it silently downscales to a per-model native size defined by a long
  edge and a 28 px patch budget: 1568 px and 1568 patches on the standard tier,
  2576 px and 4784 patches on the high-resolution tier (Claude 4.7 and later).
- **OpenAI Responses** allows 512 MB and 1,500 images per request but rejects any
  image needing more than 30,000 32 px patches after the selected `detail`
  level's resizing. Resizing depends on model family and `detail`; on some
  families `auto` means `original`, which performs no patch-budget resizing.
- **OpenAI Codex** uses Responses semantics through the ChatGPT backend. Its
  reference client prepares inline images client-side (2048 px and 2,500
  patches for `high`; 6000 px and 10,000 patches for `original` on models that
  advertise original-detail support) and does not send remote image URLs.
- **OpenAI-compatible Chat Completions** carries images only in user content;
  tool messages are text-only.
- **OpenCode Go** forwards each request unchanged to one of several
  unpublished upstream providers for the requested model. Documented vendor
  limits for its models range from JPEG/PNG-only images capped at 5 MB to a
  6 MB limit on the entire request body.

A single persisted image that violates the active model's constraints must not
make every later request fail, and changing models must not turn valid history
into an unrecoverable session. The target model is unknown when an image is
ingested, so ingestion cannot guarantee provider conformance on its own.

Image capability is also consulted outside request translation: the session
layer decides whether a user may attach images and whether image-producing
tools are enabled, and clients render attachment affordances from model
capabilities. Those answers must come from the same source as request
translation, or the system accepts images it cannot send.

Provider prompt caching matches request prefixes byte for byte, so any request
transformation of history must be deterministic.

## Decision

### Providers declare an image policy for every model

Each resolved model exposes an `ImagePolicy` supplied by its owning provider,
alongside the stream, replay validation, and context measurement behavior that
[ADR 0016](0016-pass-resolved-models-to-droids.md) binds to a resolved model.
The policy is declarative data plus pure sizing functions:

```go
// ImagePolicy is a model's contract for image content.
type ImagePolicy struct {
	// Placements lists where images may appear: user input, context
	// messages, and tool results. Empty means the model accepts no images.
	Placements []ImagePlacement
	// Formats lists media types the provider accepts as sent.
	Formats []string
	// Sources lists accepted source kinds: inline data and/or HTTPS URLs.
	Sources []ImageSourceKind

	// Fit returns the dimensions the model processes for a w×h image under
	// the request options the provider will send. Prepared images are
	// downscaled client-side to exactly these dimensions.
	Fit func(w, h int) (int, int)

	// MaxEncodedBytes bounds one image as encoded on the wire.
	MaxEncodedBytes int64
	// MaxImages and MaxRequestImageBytes bound one request.
	MaxImages            int
	MaxRequestImageBytes int64
	// ManyImages, when set, applies a stricter Fit to every image in a
	// request that carries more than Above images.
	ManyImages *ManyImageRule
}
```

`Fit` must yield dimensions that satisfy every hard dimension, patch, and edge
limit of the model under the request options the provider sends. A provider that
selects request-level image options, such as OpenAI `detail`, declares the `Fit`
that corresponds to those options.

Every provider declares a policy explicitly. Provider registration fails when a
model advertises image input without one. Text-only models declare an empty
policy. `AdaptProvider` and test providers declare text-only behavior unless
they supply a policy.

### The runtime prepares images for every request

Droids applies the active model's policy to the selected request messages
after context selection and before provider translation. The same preparation
runs for request dispatch and for replay validation, so a model switch is
assessed against exactly what would be sent.

Preparation produces request-scoped messages and a list of adjustments. It never
mutates canonical history. For each image, in order, the outcome is one of:

- **Unchanged** — the image already conforms and its original bytes are sent.
- **Prepared** — the image is downscaled to `Fit` and/or re-encoded to an
  accepted format.
- **Omitted** — the image cannot be sent to this model and is replaced by a
  text placeholder that states the reason, such as an unsupported placement,
  format, or source, an undecodable image, or a limit that cannot be met.

When an image is downscaled, a short text notice follows it in the same message
or tool result, for example `Image 1 of 1 was resized from 2880×1800 to
1456×910 pixels.` Tools that act on pixel coordinates can map positions back to
the original.

Provider translators receive only conforming images. A non-conforming image at
translation is an internal invariant violation reported as an error, not a user
condition.

Adjustments are reported through runtime diagnostics so clients can surface,
for example, that earlier screenshots were omitted for the active model.

### Request-level limits and retention

Kit sends at most a fixed number of the most recent images per request. The
default retention window is 20 images, bounded further by the policy's
`MaxImages`. Retention treats every image alike: user attachments, context
images, and tool-result images share one window ordered by position in the
request, with no source receiving priority. When the window is exceeded, the
oldest images are omitted in batches of half the window, so a request carries
between half the window plus one and the full window, and the set of omitted
images, with it the request prefix, changes once per batch rather than on
every new image. Omitted images become placeholders; the canonical history
keeps them.

After retention, if the image count exceeds a policy's `ManyImages.Above`
threshold, every image is fitted with the stricter rule. If the prepared images
still exceed `MaxRequestImageBytes`, the oldest images are omitted until the
request fits.

### Current input fails early; history degrades

User-submitted images are prepared against the active model at submission.
Submission fails with an actionable error only when the model accepts no user
images or the image cannot be decoded or converted to an accepted format.
Oversized images are accepted and prepared.

Kit never rejects a request because of an image in history or in a tool
result. At worst the image is omitted with a placeholder that tells the model
why.

### Capability questions use the policy

Whether a session accepts image attachments, whether image-producing tools are
enabled, and the image input capability reported to clients are derived from
the active model's policy placements. No component outside a provider infers
image capability from provider or model API identifiers.

### Image preparation

Image preparation lives in a dedicated package within Droids that owns
decoding, orientation, resampling, encoding, and caching. It is pure Go, so the
release build stays CGO-free.

- JPEG, PNG, GIF, and WebP are decoded. Animated GIFs use the first frame.
- EXIF orientation is applied before resizing, because re-encoding discards
  the metadata providers would otherwise honor.
- Unchanged images keep their original bytes. An image with a non-default EXIF
  orientation is re-encoded upright even when it otherwise conforms, because
  not every provider documents honoring the tag. Resized images are re-encoded
  in the source format when an encoder is available, otherwise as PNG. If the
  result exceeds `MaxEncodedBytes`, the image is re-encoded once as JPEG when it
  has no meaningful transparency; otherwise it is omitted.
- Output is deterministic for a given source, policy, and Kit build.
- Prepared images are cached in memory by source content hash and fitted
  dimensions in a bounded LRU, so history is not decoded on every request.
- Decoding is bounded by the provider-neutral ingestion limits below.

### Ingestion stays provider-neutral

Every ingestion path, including MCP and plugin tool results, validates images
against Kit's provider-neutral model-image limits: supported raster format,
decodable header, and bounded encoded size and pixel count. These limits bound
storage and decode cost; they are not provider limits and are not tightened to
match any one provider.

Kit ingestion produces inline images. Droids never fetches remote images during
preparation. An HTTPS image is passed through unchanged only to a policy that
declares HTTPS sources and is otherwise omitted.

### Context accounting is unchanged

Context measurement follows [ADR 0024](0024-unify-context-compaction.md)
without change. A provider's context measurer is used when it implements one;
otherwise Droids' conservative estimator measures the active context, charging
each image a fixed allowance independent of its dimensions. Measurement applies
to the active context, not to the prepared request, so images that a request
downscales or omits still count at the full allowance and compaction errs
toward running early.

### Provider rejections are reported, not retried

A policy describes documented limits; it cannot guarantee acceptance. Some
upstreams publish no limits, and OpenCode Go may route a model to a different
upstream. When a provider rejects a prepared request, Kit does not retry it or
alter its images. The turn fails through the normal provider error path, and the
provider's rejection reason, safely redacted, is preserved on the failed turn
and shown to the user instead of a generic failure message.

The failed turn leaves the session usable. Because preparation is
deterministic, repeating the same request on the same model is rejected the same
way; the user acts on the reported reason, for example by switching models or
compacting, which replaces earlier images in the active context with textual
descriptions under [ADR 0024](0024-unify-context-compaction.md).

### Reference sources are out of scope

Preparation produces inline image data only. Sending images by file reference,
such as Anthropic's Files API, Codex attachment uploads, or vendor file IDs,
requires credential-scoped file lifecycle management and is not part of this
decision.

## Initial provider policies

Policies live in each provider's code with tests that pin their values to the
cited documentation. This section records their initial shape and sources.

### Anthropic

Source: Claude API vision and vision-coordinates documentation.

- Placements: user, context, tool result. Formats: JPEG, PNG, GIF, WebP.
  Sources: data, HTTPS.
- `Fit`: Anthropic's reference algorithm — the largest aspect-preserving size
  whose 28 px-padded edges fit the tier's edge limit and whose
  `⌈w/28⌉ × ⌈h/28⌉` patch count fits the tier's patch budget. Standard tier:
  1568 px and 1568 patches; high-resolution tier: 2576 px and 4784 patches.
  Preparing to the native size loses no fidelity the model would have used and
  makes returned coordinates map exactly onto the prepared image.
- Tier membership is a reviewed Kit table keyed by model ID: Claude Opus 4.7 and
  later, Sonnet 5, and Fable models are high-resolution. Models absent from the
  table use the standard tier, which is always accepted.
- `MaxEncodedBytes`: 10 MB base64. `MaxImages`: 100 for models with a context
  window of 200k tokens or less, otherwise 600. `MaxRequestImageBytes`: below
  the 32 MB request limit with headroom for text.
- `ManyImages`: above 20 images, fit to 2000 px per side within the tier's
  patch budget.

### OpenAI

Source: OpenAI images and vision guide.

- Placements: user, context, tool result. Formats: JPEG, PNG, WebP, and
  non-animated GIF. Sources: data, HTTPS.
- Kit sends `"detail": "high"` on every image and declares the matching
  `Fit`. `high` gives predictable per-image cost, keeps every family well under
  the 30,000-patch rejection limit, and matches the Codex reference client.
  `original` and `auto` are not sent, because on several families they skip
  patch-budget resizing.
- For patch-based families, `Fit` applies the family's `high` dimension limit
  and patch budget: 2,500 patches with a 2048 px maximum dimension for most
  families, and the family-specific values where the guide differs. Tile-based
  families fit within 2048 × 2048 and then a 768 px shortest side. Families
  absent from Kit's table use 2048 px and 2,500 patches.
- `MaxImages`: 1,500 and `MaxRequestImageBytes` within the 512 MB request
  limit; the retention window is the binding constraint in practice.

### OpenAI Codex

Source: OpenAI images and vision guide and the `openai/codex` reference client.

- Responses semantics as for OpenAI, including `"detail": "high"` on every
  image. Sources: data only.
- `Fit`: 2048 px and 2,500 patches, matching the reference client's `high`
  preparation.

### OpenCode Go

Sources: OpenCode's open-source Zen gateway
(`packages/console/app/src/routes/zen` in `sst/opencode`) and each upstream
vendor's API documentation.

The gateway forwards request bodies unchanged in the wire format of the
endpoint Kit calls. It rejects, rather than converts, a mismatch between that
format and the upstream provider's format, and it neither processes images nor
enforces image limits. Each model is served by one of several weighted
upstream providers chosen per session, with fallback; the upstream is not
published and can change. A model's effective limits are therefore those of an
upstream Kit cannot identify, and its policy is a conservative envelope that
fits every vendor's documented limits:

| Vendor (models) | Formats | Per image | Per request | Placement |
| --- | --- | --- | --- | --- |
| xAI (Grok) | JPEG, PNG | 20 MiB | — | — |
| Z.ai (GLM-5.3-Flash) | JPEG, PNG | 5 MB, 6000 × 6000 px | 150 images | — |
| Alibaba (Qwen) | no GIF; JPEG and PNG only above 4K | 20 MB data URL, 8K | 250 inline images; 6 MB total body on the Anthropic-compatible API | — |
| MiniMax (M3) | JPEG, PNG, GIF, WebP | 10 MB | 64 MB body | tool results allowed |
| DeepSeek (Flash) | JPEG, PNG, GIF, WebP | 32 MiB, 8192 px; 4096 px at 15 or more images | 48 MiB body, 600 images | user messages only |
| Moonshot (Kimi) | JPEG, PNG, GIF, WebP, BMP, HEIC | 4096 × 2160 recommended | — | no URL sources |
| OpenAI (GPT) | as OpenAI | as OpenAI | as OpenAI | as OpenAI |

Xiaomi MiMo, Meta Muse Spark, and unnamed preview models publish no image
limits.

The envelope:

- Formats: JPEG and PNG. Other formats are re-encoded as PNG, using the first
  frame of animations. Sources: data only.
- `Fit`: long edge at most 2048 px. This also satisfies the 4096 px
  many-image limit, so no `ManyImages` rule is needed.
- `MaxEncodedBytes`: 5 MB.
- `MaxRequestImageBytes`: 32 MB, and 4 MB for models Kit reaches over the
  Anthropic Messages wire format whose vendor documents a smaller total body
  limit, such as Qwen's 6 MB.
- Placements: user input for every image-capable model. Context and
  tool-result images are allowed only where both the wire format and the
  vendor document them: MiniMax M3 over Anthropic Messages and OpenAI models
  over Responses. Elsewhere they are omitted with a placeholder stating that
  the model cannot receive images there.
- OpenAI models use the OpenAI policy. Models whose catalog input excludes
  images declare an empty policy.

Per-model exceptions live in a reviewed Kit table keyed by model ID. Models
absent from the table receive the envelope with user-only placement.

## Required properties

- Every provider declares an image policy for every model; text-only behavior
  is declared, not defaulted.
- Request dispatch and replay validation prepare images identically.
- Canonical history is never mutated by preparation.
- Kit never rejects a request or model switch because of a history or
  tool-result image; such an image is prepared or omitted with a reason the
  model can read.
- User-submitted images fail at submission only when they cannot be prepared
  for the active model.
- Retention applies one window to all images regardless of source.
- Provider translators receive only images that satisfy the policy.
- Preparation output is deterministic and cached by content.
- Image capability outside providers is derived only from policies.
- Preparation never fetches remote content.
- Provider rejections are never retried automatically; their redacted reason
  reaches the user, and the session remains usable after the failed turn.
- Context measurement is unaffected by preparation.
- The release build remains CGO-free.

## Consequences

### Positive

- Provider image constraints are explicit, reviewable data with tests tied to
  documentation, and adding a provider requires answering them.
- Oversized and accumulated images degrade gracefully instead of making sessions
  unusable.
- Images are sent at the size the model actually uses, reducing request size,
  latency, and token cost without losing usable fidelity.
- Clients, session gating, and request translation agree on image capability.

### Negative

- Kit owns image decoding, resampling, and encoding, including orientation and
  format edge cases.
- Policies duplicate provider documentation and can drift when providers change
  limits; tests catch only Kit's side of that drift.
- Omitted images reduce model context in long, image-heavy sessions.
- OpenCode Go's conservative envelope sends smaller images, and fewer image
  placements, than some of its upstreams accept.
- Context accounting overestimates image cost, so compaction may run earlier
  than strictly necessary.
- Preparation adds CPU work per request, bounded by caching.

## Related

- [ADR 0004: Model a droid as an autonomous agent runtime](0004-droids-agent-runtime-boundary.md)
- [ADR 0016: Pass resolved models to Droids](0016-pass-resolved-models-to-droids.md)
- [ADR 0024: Unify active-context compaction](0024-unify-context-compaction.md)
