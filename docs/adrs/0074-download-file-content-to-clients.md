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
understood by `URLSession`, `fetch`, and Go's HTTP client. Kit already serves
attachment bytes on a raw route with identity in headers.

## Decision

### One download route

`downloadFile` (`GET /v1/sessions/{sessionID}/files/content`) serves the
complete bytes of one file. Whether the request names a workspace selects the
kind of file:

- **Workspace file:** the query has `workspaceId` and `path`. `path` is
  always a canonical workspace path (ADR 0019), including paths such as `~`
  or `~/notes` that name entries in the workspace. Resolution, containment,
  and symbolic link rules are those of `readWorkspaceFile`.
- **Host file:** the query has `path` and no `workspaceId`. `path` is
  absolute, `~`, or `~/…` (ADR 0050), normalized exactly as `readHostFile`
  normalizes it, so both operations name the same file. Other forms,
  including relative paths and `~user`, are `invalid_path`. The access
  policy is that of `readHostFile`.

A parameter counts as present even when it's empty, so `workspaceId=` is
a workspace request with an invalid ID. A repeated `workspaceId` or `path`
is `invalid_request`. A request is validated only as the kind it names. A path that fails one
kind's validation is never retried as the other kind. An absolute path inside
the working directory, sent without `workspaceId`, is a host file. A client
that wants the workspace revision, for example to anchor annotations, sends
the workspace path with `workspaceId`.

The server serves any regular file. It doesn't sniff, classify, or validate
the content, and it doesn't reject binary files. Deciding what can be shown,
such as UTF-8 text or a supported image, is the client's job. Directories,
devices, sockets, and FIFOs fail with `not_file`. The server checks the path's
type before opening it, opens with `O_NONBLOCK` and `O_NOCTTY`, and checks the
type and identity of the opened descriptor before sending anything. These
steps narrow a replacement race but don't remove it, which is also true of
`readHostFile`.

### HTTP semantics

Each file is an ordinary HTTP resource, served from the opened descriptor.

- **`ETag`** is the file's revision in quotes, as a strong validator. For
  workspace files it is the `file_` revision that directory entries, reads,
  and annotations already use. Host files get a `hostfile_` revision derived
  the same way, from the cleaned absolute path, size, nanosecond mtime,
  device, inode, and ctime. The revision is derived from metadata, not
  content, so it is as strong as the guarantee ADR 0019 states. Filesystems such as APFS and ext4 ordinarily update ctime at high
  resolution on every write, so a change almost always changes the
  revision. Two writes that share a timestamp, more likely on filesystems
  with coarse timestamps, can leave it unchanged.
- **`Content-Length`** is the size reported by the opened descriptor: the
  intended length of the transfer, read from offset zero. A file that grows
  during the download is served to its length at open. A file truncated
  during the download ends the body early (see Consistency).
- **`If-None-Match`** answers `304 Not Modified` when the client's cached
  revision is current.
- **`If-Match`** answers `412 Precondition Failed`, before any content, when
  the file no longer has the expected revision.
- **One `Range`** with `If-Range` answers `206 Partial Content` with that
  byte range. Clients send `If-Range` with every `Range`. A stale `If-Range`
  gets the whole file with `200`. A request for more than one range also
  gets the whole file with `200`. An unsatisfiable range gets `416` with
  `Content-Range: bytes */size`.
- **`HEAD`** returns the headers a `GET` would, with no body. It is the cheap
  way to check a file's current revision.
- **`Last-Modified` isn't sent.** Its one-second precision would let
  `If-Modified-Since` miss changes, so clients revalidate with the ETag only.

A file that reports a size of zero, such as many files under `/proc`, is
served as an unknown-length transfer until end of file. Such files are often
generated when read, and their content changes while their metadata doesn't,
so a metadata revision can't validate them. Their responses therefore carry
no `ETag` and `Cache-Control: no-store`, and they support no ranges
(`Accept-Ranges: none`). `If-None-Match` and `If-Range` are ignored, so every
request gets the current content with `200`, and `If-Match` fails with `412`.
`HEAD` can't tell a client whether its copy is current, so the client
downloads again to refresh it.

### Headers

`200`, `206`, and `HEAD` responses carry all of the following, except that
an unknown-length response has no `ETag` and uses `Cache-Control: no-store`.
`304`, `412`, and `416` carry the `X-Kit-*` headers, `Cache-Control`, and
the `ETag` when the file has one.

| Header | Value |
| --- | --- |
| `ETag` | `"file_…"` or `"hostfile_…"`, absent for unknown-length files |
| `X-Kit-Session-ID` | the session |
| `X-Kit-Workspace-ID` | workspace files only: the workspace the file was resolved in |
| `X-Kit-File-Path` | the canonical workspace path, or the cleaned absolute host path with `~` expanded, percent-encoded as UTF-8 |
| `X-Kit-File-Modified-At` | RFC 3339 time with nanoseconds |
| `Cache-Control` | `private, no-cache`, or `no-store` for unknown-length files |
| `Accept-Ranges` | `bytes`, or `none` for a file that reports a size of zero |
| `Content-Type` | `application/octet-stream` |
| `X-Content-Type-Options` | `nosniff` |
| `Content-Security-Policy` | `sandbox; default-src 'none'` |
| `Content-Disposition` | `attachment`, with the file's base name as an RFC 6266 `filename` and an RFC 5987 `filename*` |

The content isn't compressed, so ranges are byte offsets into the file.

The route serves arbitrary bytes, including HTML and SVG, from the same
origin as the browser client (ADR 0059). The last four headers stop a browser
that opens a download URL directly from rendering or running the content
with that origin's authority, as they do for attachments. They don't protect
a document the client itself builds from downloaded bytes, such as a `blob:`
URL. The browser client keeps ADR 0059's rule that it never renders HTML,
SVG, or other active content as a document with its origin.

### Consistency

For a workspace file, the server checks `workspaceId` when it admits the
request. It checks again after the file is resolved and opened, immediately
before it sends headers. A mismatch at either check fails with
`stale_workspace` and sends no content. The revision is taken from the
opened descriptor, and every precondition is evaluated against it. Once
headers are sent, the descriptor keeps serving the file that was admitted,
even if the cwd changes.

A download is not a snapshot. If the file changes after the headers are sent:

- **Replaced or renamed:** the descriptor keeps reading the original file, so
  the body is coherent.
- **Grown:** the body stops at the length at open.
- **Truncated below the length at open:** the body ends short of
  `Content-Length`. The client sees a failed transfer.
- **Written in place:** the body may mix old and new bytes.

After every download of a known-length file, whole or resumed, a client that needs to know whether
its bytes are current sends `HEAD` and compares the `ETag` with the one it
downloaded. If they differ, or `HEAD` fails with `stale_workspace` or
`not_found`, the client keeps what it has, marks it as changed while loading,
and may download again. A resumed download is exactly as reliable as the
revision. A change that leaves the revision unchanged isn't detected by a
resume or by `HEAD`, just as it isn't detected by `readWorkspaceFile` or by
annotations.

### Admission, backpressure, and cancellation

A download moves through admission in this order:

1. It takes a read admission from the per-session and daemon-wide gates
   that reads use, and waits in their bounded queues like a read.
2. It validates the request, resolves and opens the file, and evaluates
   preconditions. Requests that send no body (`HEAD`, `304`, `412`, `416`,
   and errors) respond and release the read admission here.
3. A response with a body tries to take a download slot, first per session
   (at most 2), then for the daemon (at most 8), without waiting. If either
   is full, the request releases whatever it took and fails with
   `capacity_exceeded`, with `scope` naming the first gate that was full.
4. It releases its read admission, then sends headers and body. A slow
   download therefore never holds one of the read slots that previews and
   annotations use.
5. When the body is complete, or the client cancels, or a write fails or
   misses its deadline, it releases the download slots and closes the
   descriptor, exactly once.

The server copies the body in bounded blocks, so its memory per download
doesn't depend on the file's size. Backpressure is TCP's. Every write gets a
fresh 10-second deadline (the stream write timeout of ADR 0035), so a client
that stops reading loses its download rather than holding a slot. The copy
stops when the request's context ends. Cancellation can't interrupt a read
that is blocked in the filesystem, such as on a stalled network mount. That
download keeps its slot and descriptor until the read returns. Kit accepts
this risk to availability: it can exhaust the download slots but not the
read slots. Shutting the daemon down closes every download's descriptor.

The server sets no size cap. The client decides how much to download, and
cancels or doesn't start past its own budget. It learns the size from
`HEAD`, from `Content-Length`, or from a directory entry. A cap isn't needed
to protect the server:

- A body with a known size ends at the size at open, however much the file
  grows.
- Memory per download is bounded. A download holds one of a few download
  slots, and its copy ends when the client cancels or stops reading.
- A file that misreports its size, such as `/proc/kcore`, or a zero-size
  file that never reaches end of file, runs only as long as a client reads
  it. Clients learn the reported size before the body and stop at their
  budget.
- The same caller can already read any such file with a shell command in the
  session.

A client that loses its connection to a known-length download, including to
the write deadline, resumes with `Range` and `If-Range` from the bytes it has. If the file changed in the
meantime, it gets the new revision whole, with `200`.

### Errors and authorization

Failures before the body use the ADR 0034 error body with the codes of the
matching read:

- **Both kinds:** `invalid_request` (malformed query), `invalid_path`,
  `not_found`, `not_file`, `permission_denied`, `limit_exceeded` (request
  bounds), `capacity_exceeded`, `unavailable`, and `internal`.
- **Workspace files only:** `outside_workspace`, `symlink_traversal`, and
  `stale_workspace`.

`binary_file` doesn't apply. `304`, `412`, and `416` are HTTP conditional
responses. They carry no error body, and the catalog declares them as such.

The route requires the same Host, Origin, credential, instance, and
session-protocol checks as every session route. It grants no authority
beyond `readWorkspaceFile` and `readHostFile`. As ADR 0050 decides, any
authenticated caller may read any regular file the server's OS user can
read. Downloads keep that boundary and change only how much of a file one
request returns.

### Contract

`internal/httpapi` catalogs the operation with `HandleRaw`, as
`readAttachment` is catalogued, and declares both kinds' errors together.
The catalog gains three declarations, and the OpenAPI document publishes
them:

- the response headers of an operation;
- the bodiless responses `304`, `412`, and `416`; and
- `206` as an additional success.

With `workspaceId`, the query is validated as `ReadWorkspaceFileInput`;
without it, as `ReadHostFileInput`. Neither takes the highlight flag.
`api.Session` exposes a download as a reader that carries the response
metadata. The Go client validates the metadata headers as it validates
response records.

### Highlighting

The download carries no highlighting. A client with its own highlighter,
such as the macOS or iOS app, colors the downloaded text itself. A
highlighter that keeps per-line state can color lines as the bytes arrive. A
client without a highlighter shows the text plain, or reads a highlighted
preview with `readWorkspaceFile`.

### Compatibility

The route is additive, and the session protocol version doesn't change.
`readWorkspaceFile`, `readHostFile`, and directory listings stay as they are.
An older server answers the new route with an unrouted `404` whose body
doesn't decode as an error body. A client treats that as "not supported" and
falls back to the bounded read.

## Consequences

- Clients can show a whole file, however long, with one request, and
  revalidate a cached copy with a bodiless request.
- An interrupted download of a large file resumes rather than restarts, with
  the revision's guarantee.
- The server's memory and admission per download are bounded whatever the
  file's size. Disk and network cost grow with what clients choose to
  download.
- Clients own size budgets, text decoding, binary detection, and the
  presentation of a copy that changed while it loaded.
- Go's `http.ServeContent` doesn't provide the zero-size, single-range,
  `416`, and header behavior above. The server serves a known-length file
  from a fixed-length section of the descriptor, and an unknown-length file
  by copying until end of file.
- The macOS app adopts the route first. The web client needs a virtualized
  line list before it can show files well past today's preview bounds. The
  TUI and iOS app can keep the bounded read until a need appears.
- Diffs and directory listings keep their bounded, paginated contracts.
