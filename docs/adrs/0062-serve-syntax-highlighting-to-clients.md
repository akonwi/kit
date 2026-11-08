# 0062: Serve syntax highlighting to clients

## Status

Proposed. Extends the semantic highlighting service of
[ADR 0021](0021-use-native-tree-sitter-highlighting.md) to protocol clients,
first the browser client of
[ADR 0059](0059-serve-a-browser-client-from-the-kit-server.md).

## Context

Kit's server executable links a curated set of native Tree-sitter grammars
behind `internal/highlight`, which returns sanitized source and
theme-independent semantic spans with bounded work, caching, and a plain-text
fallback (ADR 0021). The terminal client uses it in process.

A protocol client cannot call it. The browser client shows code in reply
fences, and will show workspace files and diffs, with no highlighter of its
own. Running Tree-sitter in the browser would add a WebAssembly runtime and a
grammar download per language, and would require the client's Content
Security Policy to allow WebAssembly compilation.

Highlighting that arrives separately from the text it colors must be kept in
step with that text as it scrolls, changes on disk, or carries line comments.

## Decision

The session API serves the highlighting service's semantic spans. Highlighting
is optional data returned with, or for, text a client already has. It never
changes that text, and every failure leaves the text plain.

### One record

A `Highlight` record describes the colors of one text:

- `language`: the language the server highlighted, after alias and path
  detection.
- `lines`: one entry per line of the text, in order. Each entry is a flat
  array of triples: start, length, and role. Start and length count UTF-16
  code units within the line, the unit JavaScript and Swift strings index.
  Unstyled ranges are omitted; ranges within a line are ordered and do not
  overlap.
- `roles`: the role names the triples refer to, by index, drawn from the
  semantic roles of ADR 0021 (`keyword`, `string`, `comment`, and the rest).

Offsets index the text exactly as the response or request carries it. The
highlighter sanitizes source before parsing (tab expansion, line-ending
normalization, control-character replacement); the server maps spans from the
sanitized text back to the delivered text, and drops a span it cannot map.

The role set is part of the contract. A client draws a role it has no color
for as plain text, so the server may add a role without a protocol change.
Clients resolve roles to their theme's colors at paint time.

### File reads

`readWorkspaceFile` and text results of `readHostFile` accept an optional
`highlight` flag. When it is set and the content is in a supported language,
the response carries a `highlight` for the returned content, computed by the
server from the content it already read. Text and colors arrive in one
response, so they cannot disagree.

The field is absent when the language is unsupported, the content exceeds the
highlighter's bounds, the highlighter is saturated or times out, or the result
fails validation. A client shows the content plain and does not retry for the
same revision.

### Diffs

File diff reads accept the same flag. The server highlights the whole old and
new file once each, so constructs that span lines are colored correctly, and
returns a `Highlight` per side covering only the lines the page returns.

### Code in messages

A `highlightCode` operation accepts a bounded batch of blocks, each a language
label and source, and returns one optional `Highlight` per block, in order. A
client calls it once for the fenced code of a completed message, and once for
the fenced code of a loaded transcript page. A fence still streaming stays
plain until its message completes. Clients cache results by language and
source for the lifetime of the page.

The operation bounds the number of blocks, the bytes per block, and the total
bytes per request, and rejects a request over those bounds with a declared
error. A block the highlighter cannot color returns no `Highlight`.

### Work and bounds

All three uses run through the highlighting service's existing scheduler,
cache, timeouts, and limits. They add no queue, worker pool, or cache of their
own, and a slow parse never delays the text it would color: a file read
returns its content plain rather than waiting past the highlighter's timeout.

### Compatibility

The flag, the response fields, and the operation are additive, so the session
protocol version does not change. A client treats a missing `highlight` the
same whether the server declined, failed, or predates this decision. Clients
that have their own highlighter, such as the macOS and iOS apps, are not
required to use these fields.

## Consequences

- The browser client colors code with the same grammars and queries as the
  terminal client, including Ard, with no grammar downloads and no change to
  its Content Security Policy.
- Colors for files and diffs need no separate request and no client-side
  synchronization with the text.
- Code in a streaming reply is plain until the reply completes, then colors
  in place without changing layout.
- A highlighted file response is larger than its text, roughly two to three
  times for typical source; compressing session API responses would recover
  most of that.
- Highlighting load from remote clients shares the server's bounded
  highlighter with the terminal client; under saturation, clients see plain
  text rather than delay.
- Mapping spans from sanitized to delivered text is part of the service's
  contract and is tested with tabs, CRLF line endings, control characters,
  and non-ASCII text.
