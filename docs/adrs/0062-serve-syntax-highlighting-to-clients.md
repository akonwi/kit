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
- `lines`: one entry per line of the text, in order, split at line feeds; a
  final line feed ends the last line rather than starting another. Each entry
  is a flat array of triples: start, length, and role. Start and length count
  UTF-16 code units within the line, the unit JavaScript and Swift strings
  index. Unstyled ranges are omitted; ranges within a line are ordered and do
  not overlap. The carriage return of a CRLF line ending stays in its line
  and is never styled.
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

The language is detected from the file's path. The field is absent when the
language is unsupported, the content exceeds the highlighter's bounds, the
highlighter is saturated or times out, or the result fails validation. A
client shows the content plain and does not retry for the same revision.

### Diffs

`readFileDiff` accepts the same flag. The server highlights the whole old and
new file once each, so constructs that span lines are colored correctly, and
returns a `highlight` with an optional `old` and `new` `Highlight`, each
covering only the lines the page returns. The old side has one entry per page
line with an old line number (context and deletions), and the new side one
per page line with a new line number (context and additions), each in page
order across the page's hunks; each side lists only the roles its lines use.
A side is absent when it cannot be highlighted or the page has no lines on
it, and the whole field is absent when neither side can be highlighted or the
colors would take the page past the diff response bound.

Diff observations (`observeDiff`, `observeWorkingTree`) return file summaries
without text, so they take no flag.

### Code in messages

A `highlightCode` operation accepts a bounded batch of blocks, each a language
label and source, and returns one entry per block, in order, each with an
optional `Highlight`. A
client calls it once for the fenced code of a completed message, and once for
the fenced code of a loaded transcript page. A fence still streaming stays
plain until its message completes. Clients cache results by language and
source for the lifetime of the page.

The operation bounds a request to 128 blocks, 512 KiB of source per block,
768 KiB of source in total, and 64 bytes per language label, and rejects a
request over those bounds, or over the session API's request body limit, as
`limit_exceeded` with details naming the exceeded bound. A block the
highlighter cannot color, including one with no label, returns no
`Highlight`.

Highlighting code is not session state, so the operation is a server
operation, `POST /v1/highlight`, rather than a session route. Like every API
route it is reached only after the server's Host, Origin, credential,
instance, and session protocol checks.

### Work and bounds

All three uses run through the highlighting service's existing scheduler,
cache, timeouts, and limits. They add no queue, worker pool, or cache of their
own, and a slow parse never delays the text it would color: a file read
returns its content plain rather than waiting past the highlighter's timeout.

Every request shares one deadline, the highlighter's timeout, across all of
its texts: a diff page's two sides, or a batch's blocks. A request highlights
at most as many texts at once as the highlighter has workers, so one request
cannot fill the highlighter's queue. Text in an unsupported language or over
the highlighter's bound never takes a place in its queue, and a diff page is
colored after the server releases the diff capacity it held to read it.

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
- A highlighted file response is larger than its text: for typical source
  the colors add about 0.7 to 0.9 times the text's size, so the response is
  about 1.7 to 1.9 times as large. Compressing session API responses would
  recover most of that.
- Highlighting load from remote clients shares the server's bounded
  highlighter with the terminal client; under saturation, clients see plain
  text rather than delay.
- Mapping spans from sanitized to delivered text is part of the service's
  contract and is tested with tabs, CRLF line endings, control characters,
  and non-ASCII text.
