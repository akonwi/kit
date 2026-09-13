# Attachments and transcript images implementation plan

Status: accepted direction; implementation in progress.

## Goals

- Stage validated image and text attachments from pasted local paths without
  losing composer state across submission, queueing, reconnect, or restart.
- Keep attachment bytes authoritative and durable in the Go server while
  snapshots and events remain bounded.
- Render user image attachments and explicit `show_image` results directly in
  the transcript.
- Open persisted image bytes in the operating system's default application.

## Product decisions

### Composer input

The native TUI does not provide an `/attach` command. It recognizes attachments
from a bracketed paste containing absolute paths, `file://` URLs, or multiple
newline-separated paths. Terminal drag-and-drop is supported when the terminal
represents the dropped files as pasted paths, including names containing spaces
quoted or backslash-escaped on a single space-separated line.

Path recognition is conservative: every non-empty pasted entry must resolve to
a supported regular file or the paste remains ordinary composer text. Binary
image clipboard payloads are not supported because Vaxis bracketed paste only
provides text.

Staged attachments appear above the composer as removable rows. Image-only
submission is valid. Failed admission restores the same staged attachment
identities; queued prompts retain them durably.

### Transcript presentation

Sent user images and successful, explicitly typed `show_image` results both
render image pixels inline. Images are not collapsible. A preview is replaced
by a persistent `preview unavailable` error row only when decoding or rendering
fails.

Previews reserve bounded geometry while decoding, with a maximum of 72 columns
and 12 terminal rows. Metadata includes the filename, dimensions when known,
and the optional `show_image` caption. Ordinary paths in prose and unrelated
tool image results are never promoted.

Clicking a transcript image opens its persisted bytes in the system default
application. Kit does not provide a retained image workspace pane.

### Initial renderer

The initial renderer uses normal `vaxis/ui` cell painting. Each terminal cell
represents two vertically adjacent pixels using Unicode upper/lower half-block
characters and truecolor foreground/background styles. Decoding and scaling run
asynchronously, and rendered cells are cached by attachment identity and target
size.

This deliberately avoids a Vaxis fork, a duplicate `ui.Run`, and the unrelated
`vxfw` widget framework. Native Kitty and Sixel placement remains backlog work
pending an upstream retained-image paint API in `vaxis/ui`. Unicode half-block
rendering is the supported baseline until then.

## Authoritative attachment model

Protocol projections carry opaque, session-scoped attachment IDs and bounded
metadata, never base64 data or local source paths. The server owns:

- atomic private byte storage under `KIT_HOME/attachments`;
- lifecycle and blob metadata without duplicating canonical droid message data;
- staged, queued/claimed, consumed, and unreferenced lifecycle transitions;
- validated reads for attached clients;
- cleanup on removal and session deletion after canonical droid references are
  accounted for.

Message and tool-result associations are derived from canonical droid
`FileInput`/`FileContent` records as required by ADR 0006; Kit does not maintain
a parallel transcript association table.

Prompt admission accepts text plus ordered attachment IDs. The server validates
ownership, lifecycle, prompt limits, and provider image capability before
resolving images into provider-facing content. Text attachments are converted
to bounded labeled text input. Attachment-only prompts are allowed.

Initial limits follow the stricter legacy remote contract:

- 8 attachments and 20 MiB per prompt;
- 10 MiB per image;
- 1 MiB per text attachment and aggregate attached text;
- 8192 by 8192 dimensions and 12 megapixels;
- structurally validated PNG, JPEG, GIF, and WebP;
- sanitized basename-only filenames.

## `show_image`

`show_image` accepts a local path and an optional caption of at most 200
characters. Relative paths resolve against session cwd. The tool requires a
stable regular PNG, JPEG, GIF, or WebP file, with limits of 16 MiB and 24
megapixels.

Success persists the bytes before returning model-facing image content and
typed presentation details containing the attachment ID, filename, media type,
dimensions, and caption. Only this explicit presentation marker creates a
promoted tool image in the transcript.

## External opening

The TUI retrieves authoritative bytes, writes a private file with a sanitized
name and suitable extension under a managed cache, and invokes `open` on macOS
or `xdg-open` on Linux. Materialized files are retained long enough for the
external application to read them and are cleaned by age. Retrieval, write, and
launch failures are shown to the user.

## Delivery sequence

1. Add and test the bounded Unicode half-block raster renderer.
2. Add the attachment protocol records, server-owned storage, validation, and
   authenticated/session-bound retrieval.
3. Replace string-only prompt and follow-up records with structured prompt
   input carrying ordered attachment IDs.
4. Add validated `show_image` results backed by durable attachments.
5. Add conservative path-paste staging and removable composer rows.
6. Project user and `show_image` attachment IDs into transcript records and
   render their previews asynchronously.
7. Add external-open materialization and lifecycle cleanup.
8. Prove identity, cleanup, failure, reconnect, and restart behavior in protocol,
   core integration, and deterministic TUI presentation tests.

## Deferred work

- Upstream retained image paint operations for `vaxis/ui`.
- Native Kitty/Sixel rendering and clipping after that API is available.
- Binary clipboard image ingestion if Vaxis gains a bounded typed clipboard
  payload contract.
