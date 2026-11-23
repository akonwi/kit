# 0050: View files outside the working directory

## Status

Accepted. Extends the workspace files of the session API and the connection
model of [ADR 0045](0045-connect-clients-to-local-ssh-and-network-servers.md).

## Context

Workspace files are confined to the session's working directory: paths are
canonical and relative, and symbolic links may not leave the directory. The
model and its tools routinely name files elsewhere, such as a log under
`/var/log`, a file in another checkout, or a screenshot in the home
directory. A user also needs to look at such files without asking the model
to print them.

A client may be attached to a server on another machine. A path in the
transcript was produced by the model or a tool running on the server, so it
names a file on the server's machine. A user of the macOS app may also want to
look at a file on the Mac itself.

## Decision

### The server serves host files for any session

A client downloads a file anywhere on the server's machine with the file
content route of [ADR 0074](0074-download-file-content-to-clients.md),
naming no workspace. The path is absolute, or `~` or `~/…` for the server
user's home directory, which only the server can resolve. The path is cleaned
as text, then symbolic links are followed. Directories, devices, sockets, and
FIFOs fail with `not_file` before they are opened, so opening one cannot
block or have side effects. A file's size is what the file reports, which for
some files, such as those under `/proc`, is not the length of their content.

Clients classify what they download: UTF-8 text, a PNG, JPEG, GIF, or WebP
image of at most 16 MiB and 24,000,000 pixels, or a file they don't show.
Downloads are view-only and keep no state on the server, and they share the
per-session and daemon admission of other workspace work.

### The access policy is the server user's

Any authenticated caller may read any regular file the server's OS user can
read. All valid credentials have identical authority (ADR 0045), and that
authority already includes running shell commands in a session, so host reads
grant nothing new. There is no deny list: a list that bash bypasses would only
suggest a boundary that does not exist.

### Which machine a path names

- A path shown in the transcript names a file on the server.
- A path typed into Open File names a file on the server. A relative path, or
  an absolute path inside the working directory, opens the workspace file as
  before, with annotations. Any other absolute or `~` path opens a host file.
- The macOS app opens files on the Mac only through **Open File on This
  Mac…**, which chooses the file with the system open panel. It reads the
  file locally with the same type and size bounds and labels the tab with the
  Mac's location. A typed path never silently changes machines.

Host and local file tabs are view-only and cannot be annotated, because
annotations are anchored to workspace file revisions.

## Consequences

- Clients show files from anywhere on the server without a tool call, and
  never read transcript paths from the client's own disk.
- A server without the download route cannot open host files or workspace
  files. Clients report that the server must be updated.
- Formats that clients don't classify, such as HEIC or PDF, are not shown.
  Supporting one means teaching the clients to show it.
