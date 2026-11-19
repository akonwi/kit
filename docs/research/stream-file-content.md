# Streaming file content to clients: research

This is an exploration, not a decision. It was checked against Kit at `31175399`
(session protocol 51, unreleased). The proposal it led to is
[ADR 0074](../adrs/0074-download-file-content-to-clients.md).

## Summary

- **A file read is one bounded JSON preview, and nothing can read past it.**
  `readWorkspaceFile` and the text result of `readHostFile` return at most
  1,000,000 bytes and 5,000 lines, marked `truncated` with `byte_limit` or
  `line_limit`. No range or continuation operation exists (ADR 0019 defers
  one), so the rest of a long file is unreachable from any client.
- **For source code, the line cap applies first.** Kit's own Go source
  averages about 34 bytes per line, so the 5,000-line cap cuts at about
  170 KB, long before the 1 MB byte cap.
- **The line cap has a quadratic cost today.** `readPreview`
  (`internal/workspace/service.go`) finds each line boundary with
  `strings.IndexByte(string(data[idx:]), '\n')`, which copies the rest of the
  buffer for every line. A line-limited read of a 0.5–4 MB file took
  1.0–2.0 s of server CPU and allocated about 4.4 GB. With `bytes.IndexByte`
  it took about 2 ms and 21 MB. Workspace and host reads share this function.
  The fix landed separately, in `6e38fa09`
  (`fix(workspace): read line-limited previews in linear time`).
- **Attachments already stream raw bytes.** `readAttachment` is a
  `httpapi.HandleRaw` route. It sends identity and the SHA-256 in `X-Kit-*`
  and `ETag` headers and copies the body with `io.Copy`. The catalog records
  its response media types but not its response headers, so the OpenAPI
  document doesn't describe those headers.

## Measurements

These came from a throwaway Go test that called `workspace.Service.Read` and
`codehighlight.Default` directly on generated `.go` files in a temp directory.
No server, daemon, or Kit home was involved.

| File size | Returned | Lines | Truncation | Read (today) | Read (fixed) | Allocated (today → fixed) | Highlight | JSON with highlight |
| ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: | ---: |
| 100 KB | 100 KB | 2,978 | none | 0.4 ms | 0.3 ms | 8 MB → 8 MB | 60 ms | 207 KB |
| 500 KB | 172 KB | 5,000 | line_limit | 982 ms | 1.5 ms | 2.1 GB → 17 MB | 80–130 ms | 348 KB |
| 1 MB | 172 KB | 5,000 | line_limit | 2,030 ms | 2.4 ms | 4.4 GB → 21 MB | 90–160 ms | 348 KB |
| 4 MB | 172 KB | 5,000 | line_limit | 1,509 ms | 2.9 ms | 4.4 GB → 21 MB | 80–100 ms | 348 KB |

Reading the file isn't the expensive part. Once the quadratic loop is fixed,
highlighting is the main server cost, and the highlighter stops at
`MaxSourceBytes` (512 KiB) and `DefaultHighlightTimeout` (750 ms) anyway.

## What exists

| Surface | Bounds | Paging | Highlight |
| --- | --- | --- | --- |
| Workspace file (`readWorkspaceFile`, ADR 0019) | 1,000,000 B / 5,000 lines | none | optional (ADR 0062) |
| Host file (`readHostFile`, ADR 0050) | same text bounds; images ≤16 MiB, complete | none | optional |
| Directory (`listWorkspaceDirectory`) | 100–200 per page, 10,000 entries, 8 MiB observation | cursors, `stale_cursor` | n/a |
| Diff (`readFileDiff`) | 1 MiB / 100,000 lines per side, 512 KiB response | line and hunk pages | optional, per page |
| Attachment (`readAttachment`) | 16 MiB images, 1 MiB text | none; raw body | n/a |

Other relevant facts:

- **Admission.** Workspace and host reads share per-session gates (4 active,
  16 pending) and a daemon-wide gate. A read holds an active slot from
  admission until its result is built.
- **Consistency.** A workspace read derives a `file_` revision from size,
  mtime, device, inode, and ctime. It fails `stale_file` if the revision
  changes during the read or the path no longer resolves to the same file.
  A host read has no revision. It fails `unavailable` if size, mtime, or
  identity change.
- **Push streams.** ADR 0035 requires every server-push response to be SSE.
  A response that carries one resource's bytes isn't a push stream; the raw
  attachment route sets the precedent.
- **HTTP.** The server sets `IdleTimeout` only. SSE streams use a per-write
  deadline (`streamWriteTimeout`, 10 s). The Go client buffers ordinary
  responses. The Swift transport already reads `URLSession.AsyncBytes` for
  streams and attachments.

## Clients

- **macOS.** The workspace file pane renders the whole preview in
  `CodeEditSourceEditor`, which has its own Tree-sitter highlighter. The
  footer shows "Preview truncated". `FilePreviewCache` keeps at most 8
  previews and 8 MB. Host file tabs (`ExternalFilePane`) show the same
  truncation footer. Annotations anchor to the `file_` revision and line
  numbers, and the server re-reads lines through `ReadLineRange`.
- **iOS.** `FileReader` reads workspace and host files whole and keeps the
  `truncated` flag. It has its own highlighter.
- **Web.** `FileView` asks for `highlight: true` and renders every line into
  the DOM with `<For>`, with no virtualization. A file much larger than
  5,000 lines needs a virtualized line list first.
- **TUI.** `file_pane` reads through `session.ReadWorkspaceFile`, draws only
  visible rows, and prints "Preview truncated (line limit)".

## The macOS editor with large files

Workspace file tabs show files in `AnnotatedFileEditor`, which creates its
`TextViewController` with the document's text. Upstream CodeEditTextView
lays out lines when the text view is created, before it is in a scroll
view, while its frame is the document's height. Every line then counts as
visible and gets its own subview, so opening is quadratic in the line count:
in a Debug build, generated Go source took 0.9 s at 256 KiB, 11 s at 1 MiB,
and 71 s at 2 MiB. Previews are capped at 5,000 lines, which keeps this
under about half a second; whole files are not.

The vendored copy skips that layout, and AppKit lays out the visible lines
once the view is shown. With that change, the annotated editor shows 2 MiB
in about 0.7 s and 8 MiB in about 1.3 s, and highlighting stalls the main
thread for at most about 0.25 s. `ReadOnlyFileEditor`, used for host files,
creates its controller empty and sets the text afterwards, so it never hit
the quadratic path: 8 MiB in about 1.2 s.

## Shapes considered

| Shape | Strength | Weakness |
| --- | --- | --- |
| Raw UTF-8 on a chunked body (like attachments) | No framing or escaping. Plain `URLSession` and `fetch` streaming. Smallest server change. | The end can't carry a status: browsers and `URLSession` don't expose trailers. |
| SSE `file.chunk` records plus a terminal `file.end` | Matches ADR 0035, typed and described, with status in-band | JSON escaping adds about 5–15% for code. Record bounds. More client code. |
| HTTP `Range` on a raw route | Random access into huge files | Many round trips. Lines don't map to bytes without an index. |
| Length-prefixed binary frames | Raw speed and a final status | A custom decoder in every client |

The user chose raw bytes, then reframed the problem as an ordinary web file
download: the client downloads the file into a local cache. That points to
plain HTTP resource semantics, which `http.ServeContent` implements on an
opened descriptor. `ETag` carries the revision and `Content-Length` the size,
`If-None-Match` answers 304, `If-Match` answers 412, `Range` with `If-Range`
resumes a download, and `HEAD` works as the stat. Go's `ServeMux` answers
`HEAD` for `GET` patterns. A `HEAD` after the download replaces the separate
stat operation considered earlier.

## User decisions

| Question | Decision |
| --- | --- |
| Problem | Round trips; exploratory. Nothing has blocked work yet. |
| Scope | Workspace and host files. Directories stay paginated. Diffs out of scope. |
| Highlighting | Not on the stream. Clients highlight, or show text plain. |
| Shape | A standard HTTP file download: `GET` with `ETag`, `Content-Length`, conditional requests, `Range` resume, and `HEAD` |
| File changes mid-download | Deliver what was read, then mark it changed |
| How the client learns of a change | `HEAD` after the download, comparing the `ETag` |
| Routes | One `GET …/files/content`. The presence of `workspaceId` picks the kind: with it, a workspace file; without it, a host file. The path's form isn't used, because a workspace folder named `~` would collide with home-relative paths. |
| ETag | The metadata revision, as a strong validator. The ADR states its limit: resume and `HEAD` are exactly as reliable as the revision. |
| Content check | None. Any regular file's bytes are served, and the client decides what it can show. |
| Size cap | None on the server. Clients cancel at their own budget. |
| First client | macOS |
| Compatibility | Additive. Existing operations stay. No protocol bump. |
| readPreview fix | Done first, separately (`6e38fa09`) |
| Hard ceiling for files that misreport their size | None. The ADR records why. |

## Pierre's diffs, highlights, and trees

The user pointed to [diffs.com/words](https://diffs.com/words) as the
inspiration for this work. Its source is the Apache-2.0 monorepo
[pierrecomputer/pierre](https://github.com/pierrecomputer/pierre), checked at
`b049b9e`. The packages that matter for files:

- **`@pierre/highlights` 0.5.1.** A highlighter written in WebAssembly Text
  that covers 152 languages. Ard isn't one of them. Its lexers keep
  per-line state instead of building a syntax tree. That allows a
  `StreamTokenizer`, a `TransformStream<Uint8Array, ThemedToken[]>` that
  takes raw UTF-8 byte chunks, buffers a split character, and returns tokens
  for each completed line. The Wasm module is 303 KB (144 KB gzipped).
- **`@pierre/diffs` 1.6.4.** File and diff views. `FileStream` renders a
  `ReadableStream<string>` as it arrives. Completed lines are final; the
  unfinished last line is provisional and can be recalled when more text
  arrives. `FileStream` appends every line to the DOM, with no
  virtualization. `CodeView` and `VirtualizedFile` virtualize per line, and
  their docs say they "should scale to nearly any file … that can fit in
  memory". They support line annotations, selection, and `scrollTo` a line.
- **`@pierre/trees` 1.0.0.** A virtualized file tree. Its guidance is to
  prepare and presort the path list on the server and send it whole, rather
  than have the client build the tree.

### Measured

Bun 1.4 on this Mac, a Go file built from repeated
`internal/workspace/service.go`, `pierreDark` theme, 64 KiB chunks:

| Size | Whole file (`codeToTokens`) | Streamed (`StreamTokenizer`) | First lines | Throughput |
| ---: | ---: | ---: | ---: | ---: |
| 166 KiB | 6–17 ms | 2.5–3.4 ms | 1–1.6 ms | 48–65 MiB/s |
| 1 MiB | 14–16 ms | 9–10 ms | 0.7 ms | ~105 MiB/s |
| 4 MiB | 39–42 ms | 31–33 ms | 0.6 ms | ~125 MiB/s |

For comparison, Kit's server highlighter took 60–160 ms for 100–170 KB (see
[Measurements](#measurements)). It stops at 512 KiB and has a 750 ms timeout,
because Tree-sitter parses the whole file.

### What it teaches

- **Raw bytes are the right wire shape for the web, too.** A browser can pipe
  `fetch(url).body` straight into `StreamTokenizer`, or through
  `TextDecoderStream` into `FileStream`. Framing such as JSON or SSE would
  only have to be stripped off again. That supports ADR 0074's raw download.
- **Highlighting can stream if the highlighter keeps line state.** A
  Tree-sitter parse needs the whole file, which is why server highlighting
  stays off the download. A line-state lexer colors each line as it arrives,
  at about 100 MiB/s. A client that streams should therefore highlight on
  the client.
- **Show the first screen early, then virtualize.** `FileStream` renders as
  bytes arrive but keeps every line in the DOM. `CodeView` virtualizes but
  needs the whole text. A large-file view can stream the first screen, then
  hand the completed download to a virtualized view.
- **Line annotations survive virtualization.** `CodeView`'s line annotations
  correspond to Kit's file comments, which are anchored to a revision and
  line range.
- **Trees prefer a whole, presorted path list.** This argues for the server's
  file index over paginated directory pages for large trees. Directories are
  out of scope here.

### Costs of adopting it in the web client

- **Content Security Policy.** The browser build compiles Wasm from embedded
  bytes. The web client's CSP (`internal/webui/webui.go`) has
  `script-src 'self'` with no `'wasm-unsafe-eval'`, so the Wasm can't load
  until the CSP allows it. ADR 0062 chose server highlighting to avoid exactly
  this. `@pierre/diffs` also injects theme `<style>` elements into shadow
  roots, which `style-src 'self'` may block. That needs a spike in a real
  browser.
- **Ard.** `@pierre/highlights` has no Ard lexer, so Ard files would show
  plain. ADR 0062 lists Ard highlighting as a gain of server highlighting.
- **Framework.** The web client is SolidJS. `@pierre/diffs` ships vanilla
  classes and web components besides React, so it can be mounted from Solid
  without React.
- **Size.** About 144 KB gzipped of Wasm, plus the view code.
