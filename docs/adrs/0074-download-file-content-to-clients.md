# 0074: Download file content to clients

## Status

Proposed. Extends the workspace file contract of
[ADR 0019](0019-expose-session-workspace-files.md) and the host file reads of
[ADR 0050](0050-view-files-outside-the-working-directory.md). Research:
[`docs/research/stream-file-content.md`](../research/stream-file-content.md).

## Context

A file read returns one JSON preview of at most 1,000,000 bytes and 5,000
lines. That suits a quick look and server highlighting, but no operation
returns the rest of a longer file. For source code the line cap applies
first, at about 170 KB.

A client that shows a file wants what a web browser does with a URL: download
the bytes once, keep them in a local cache, check cheaply whether the cache is
still current, and resume a download that was cut off. HTTP already defines
all of this. Validators, conditional requests, ranges, and `HEAD` are
implemented by Go's `http.ServeContent`, `URLSession`, and `fetch`. Kit
already serves attachment bytes on a raw route with identity in headers.

## Decision

### Two download routes

The session API serves the complete bytes of a file on two `GET` routes:

- `downloadWorkspaceFile`:
  `GET /v1/sessions/{sessionID}/workspace/files/content?workspaceId=…&path=…`
  serves a workspace file. `path` is a canonical workspace path (ADR 0019).
  Resolution, containment, and symbolic link rules are those of
  `readWorkspaceFile`.
- `downloadHostFile`: `GET /v1/sessions/{sessionID}/host-files/content?path=…`
  serves a file on the server's machine. `path` follows ADR 0050: absolute,
  `~`, or `~/…`. The access policy is that of `readHostFile`.

The server serves any regular file. It doesn't sniff, classify, or validate
the content, and it doesn't reject binary files. Deciding what can be shown,
such as UTF-8 text or a supported image, is the client's job. Directories,
devices, sockets, and FIFOs fail with `not_file` before they're opened, as
they do for reads.

Each route is an ordinary HTTP resource. The server serves it from the opened
descriptor with standard HTTP semantics:

- `ETag` is a strong validator: the file's revision in quotes. For workspace
  files it is the `file_` revision that directory entries, reads, and
  annotations already use. Host files get a `hostfile_` revision derived the
  same way, from the cleaned absolute path, size, high-resolution mtime,
  device, inode, and ctime.
- `Content-Length` is the file's size when it was opened, and the body is
  exactly that many bytes. A file that grows during the download is served
  to its length at open.
- `If-None-Match` answers `304 Not Modified` when the client's cached
  revision is current.
- `If-Match` answers `412 Precondition Failed` when the file no longer has
  the expected revision, before any content is sent.
- `Range` and `If-Range` answer `206 Partial Content`, so a client can resume
  an interrupted download of the same revision. A stale `If-Range` gets the
  whole file with `200`. An unsatisfiable range gets `416`. Clients send
  `If-Range` with every `Range` request, so a resumed download never joins
  bytes from two revisions.
- `HEAD` returns the same headers with no body. It is the cheap way to check
  a file's current revision.
- `Last-Modified` isn't sent: one-second precision would make
  `If-Modified-Since` miss changes. Clients revalidate with the ETag only.

A file that reports a size of zero is served with chunked transfer until
end of file, without `Content-Length` or range support (`Accept-Ranges:
none`). Some host files, such as those under `/proc`, report zero whatever
their content.

Every success, `304`, and `412` carries:

| Header | Value |
| --- | --- |
| `ETag` | `"file_…"` or `"hostfile_…"` |
| `X-Kit-Session-ID` | the session |
| `X-Kit-Workspace-ID` | workspace route only: the workspace the file was resolved in |
| `X-Kit-File-Path` | the canonical workspace path, or the cleaned absolute host path with `~` expanded, percent-encoded as UTF-8 |
| `X-Kit-File-Modified-At` | RFC 3339 time with nanoseconds |
| `Content-Type` | `application/octet-stream` |
| `Cache-Control` | `private, no-cache` |
| `X-Content-Type-Options` | `nosniff` |
| `Content-Security-Policy` | `sandbox; default-src 'none'` |
| `Content-Disposition` | `attachment`, with the file's base name as an RFC 6266 `filename` and an RFC 5987 `filename*` |

The content isn't compressed, so ranges are byte offsets into the file.

The routes serve arbitrary bytes, including HTML and SVG, from the same
origin as the browser client (ADR 0059). The last four headers stop a
browser that opens a download URL directly from rendering or running the
content with that origin's authority, as they do for attachments.

### Consistency

The workspace ID is checked when the request is admitted. A mismatch fails
with `stale_workspace` before any content is sent. The revision is taken from
the opened descriptor, and every precondition is evaluated against it.

A download is not a snapshot. If the file changes after the headers are sent:

- **Replaced or renamed:** the descriptor keeps reading the original file,
  so the body is coherent.
- **Grown:** the body stops at the length at open.
- **Truncated below the length at open:** the body ends short of
  `Content-Length`. The client sees a failed transfer and retries.
- **Written in place:** the body may mix old and new bytes.

After a complete download, a client that needs to know whether the bytes
are current sends `HEAD` and compares the `ETag` with the one it
downloaded. If they differ, or `HEAD` fails with `stale_workspace` or
`not_found`, the client keeps what it downloaded, marks it as changed while
loading, and may download again. The revision has the limits ADR 0019
states: an in-place write that changes no observable metadata isn't
detected.

### Admission, backpressure, and cancellation

A request is first admitted through the per-session and daemon-wide gates
that reads use. Requests that send no body (`HEAD`, `304`, `412`, `416`, and
pre-body errors) release that admission when they respond.

A response with a body also holds a download slot until the body is
written: at most 2 per session and 8 for the daemon. When no slot is free
the request fails with `capacity_exceeded`, with the same `scope` detail
reads use. A slow download therefore can't hold one of the four read slots
that previews and annotations use.

The server reads and writes in bounded blocks, so its memory per download
doesn't depend on the file's size. Backpressure is TCP's. Each write has a
10-second deadline (the stream write timeout of ADR 0035), so a stalled
client ends its download rather than holding a slot. The client cancels by
closing the request, which ends the read.

The server sets no size cap. The client decides how much to download and
cancels or doesn't start past its own budget. It learns the size from
`HEAD`, from `Content-Length`, or from a directory entry. A cap isn't needed
to protect the server:

- A body with a known size ends at the size at open, however the file
  grows.
- Memory per download is bounded. A download holds one of a few download
  slots and ends when the client cancels or stalls past the write deadline.
- A file that misreports its size, such as `/proc/kcore`, or a zero-size
  file that never reaches end of file, runs only as long as a client reads
  it. Clients learn the reported size before the body and stop at their
  budget.
- The same caller can already read any such file with a shell command in
  the session.

A client that loses its connection, including to the write deadline,
resumes with `Range` and `If-Range` from the bytes it has. If the file
changed in the meantime it receives the new revision whole, with `200`.

### Errors and authorization

Failures before the body use the ADR 0034 error body with the codes of the
matching read: `invalid_path`, `not_found`, `not_file`, `permission_denied`,
`outside_workspace` and `symlink_traversal` (workspace only),
`stale_workspace` (workspace only), `limit_exceeded` for request bounds,
`capacity_exceeded`, `unavailable`, and `internal`. `binary_file` doesn't
apply. `304`, `412`, and `416` carry no error body: they are HTTP
conditional responses, and the catalog declares them as such.

The routes require the same Host, Origin, credential, instance, and
session-protocol checks as every session route. They grant no authority
beyond `readWorkspaceFile` and `readHostFile`. As ADR 0050 decides, any
authenticated caller may read any regular file the server's OS user can
read. Downloads keep that boundary and change only how much of a file one
request returns.

### Contract

`internal/httpapi` catalogs both operations with `HandleRaw`, as
`readAttachment` is catalogued. The catalog gains three declarations, and
the OpenAPI document publishes them:

- the response headers of an operation;
- the bodiless responses `304`, `412`, and `416`; and
- `206` as an additional success.

The query parameters reuse the request validation of
`ReadWorkspaceFileInput` and `ReadHostFileInput` without the highlight flag.
`api.Session` exposes the downloads as readers that carry the response
metadata, and the Go client validates the metadata headers as it validates
response records.

### Highlighting

The download carries no highlighting. A client with its own highlighter,
such as the macOS or iOS app, colors the downloaded text itself. A client
without one shows the text plain or reads a highlighted preview with
`readWorkspaceFile`.

### Compatibility

The routes are additive, and the session protocol version doesn't change.
`readWorkspaceFile`, `readHostFile`, and directory listings stay as they
are. An older server answers the new route with an unrouted `404` whose body
doesn't decode as an error body. A client treats that as "not supported" and
falls back to the bounded read.

## Consequences

- Clients can show a whole file, however long, with one request, and revalidate
  a cached copy with a bodiless request.
- An interrupted download of a large file resumes rather than restarts.
- The server's memory and admission per download are bounded, whatever the
  file's size. Disk and network cost grow with what clients choose to
  download.
- Clients own size budgets, text decoding, binary detection, and the
  presentation of a copy that changed while it loaded.
- The macOS app adopts the routes first. The web client needs a virtualized
  line list before it can show files well past today's preview bounds. The
  TUI and iOS app can keep the bounded read until a need appears.
- Diffs and directory listings keep their bounded, paginated contracts.
