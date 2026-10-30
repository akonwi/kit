# 0055: Let the model save session attachments to files

## Status

Accepted. Builds on the session attachments of
[ADR 0051](0051-let-attached-clients-host-session-plugins.md) and the
server-side paths of [ADR 0050](0050-view-files-outside-the-working-directory.md).

## Context

Tools give the model images, such as a `browser__screenshot` of a page. The
model receives each image inline and can reason about it, but it often needs
to use the image somewhere else: commit it to a repository for documentation,
a pull request, or a bug report; hand it to another program, such as an image
tool or an upload; pass it to another session or a subagent; compare it with a
later capture; or look at it again later in the session.

Images a plugin tool returns, from a process or from an attached client, are
already kept as session attachments: private, validated bytes on the server,
owned by the session and removed with it. The model needs a way to name such
an attachment and turn it into an ordinary file. Those bytes are on the server
even when an attached client captured them, and the model's file tools run on
the server, so the file belongs on the server too.

Writing every image to a file automatically would leave copies the model never
uses, in the working directory (often a repository) or in a separate directory
with its own lifetime. Exposing the attachment store's own files would let the
model alter bytes the store owns and other sessions may share.

## Decision

### Tool results name their attachments

When a plugin tool's image is kept as a session attachment, the tool result
the model receives follows the image with one text line that names it:

```text
Attachment attachment_0123…: image/png, 1600×1000, 312 KiB. save_attachment copies it to a file.
```

The line gives the attachment ID, media type, dimensions for an image, and
size. It is ordinary tool result text, kept in history and shown with the call
by clients. When the store refuses an image, the line says that the image was
not kept and cannot be copied. With no attachment store, the image stands
alone.

### `save_attachment` copies an attachment to a file

`save_attachment` takes an `attachment` ID, a destination `path`, and an
optional `overwrite`. It is offered to every session with an attachment store
and to its subagents. A subagent's tool reads its owning session's
attachments, where the images client-hosted tools return to it are kept, and
resolves paths against the subagent's working directory.

- The attachment must be owned by the session. A fork owns its source's
  attachments, so references in the history it inherited still work.
- The path is on the server's machine. A relative path resolves against the
  session's working directory; `~` and `~/…` name the server user's home.
  Missing parent directories are created.
- The tool refuses a path within the Kit home, which holds attachment storage,
  credentials, and other state Kit manages, including through symbolic links
  in the path's existing directories.
- The destination must not exist unless `overwrite` is set, and then it must
  be a regular file. A symbolic link or anything else is refused.
- The bytes are staged beside the destination, checked against the
  attachment's recorded size and SHA-256, and published with a rename, so no
  partial file is ever visible. Without `overwrite`, publication never
  replaces a file created meanwhile. Saved files have mode 0644.

The result names the attachment, its media type and size, and the absolute
path written. Attachments are already bounded by their sources (at most
16 MiB), so the tool adds no size limit of its own.

Once saved, the file is the model's to use: `inspect_image` looks at it
again, other tools read it, and another session or subagent on the same server
can be given its path.

### Contract

The reference line and the tool are model-facing and live entirely on the
server. Neither changes the session API, the session protocol, or a client.

## Consequences

- An image a tool returned can be reused at any later point in the session
  for as long as its attachment exists.
- A saved file may contain whatever the image showed, including page content
  marked as untrusted or sensitive values on screen. The model decides where
  it goes, as with any file it writes.
- Images an MCP tool returns are not kept as attachments and have no
  reference.
