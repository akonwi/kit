# 0050: View files outside the working directory

## Status

Accepted. Extends the workspace file preview of the session API and the
connection model of [ADR 0045](0045-connect-clients-to-local-ssh-and-network-servers.md).

## Context

Workspace reads are confined to the session's working directory: paths are
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

### The server reads host files for any session

`readHostFile` (`POST /v1/sessions/{sessionID}/host-files/read`) reads one
file anywhere on the server's machine. The path is absolute, or `~` or `~/…`
for the server user's home directory, which only the server can resolve. The
response is the `HostFileRead` union:

- `text`: a UTF-8 preview with the workspace preview bounds (1,000,000 bytes
  and 5,000 lines, truncated past either).
- `image`: a complete PNG, JPEG, GIF, or WebP file of at most 16 MiB and
  24,000,000 pixels, validated as `show_image` validates images, carried as
  base64 in the response.

Anything else fails with `binary_file`. Images larger than the bounds fail
with `limit_exceeded`. Directories, devices, sockets, and FIFOs fail with
`not_file` before they are opened, so opening one cannot block or have side
effects. The path is cleaned as text, then symbolic links are followed. A
text file's size is what the file reports, which for some files, such as
those under `/proc`, is not the length of their content. A file that changes
while it is read fails with `unavailable` and may be retried.

Reads are view-only and keep no state. They share the per-session and daemon
admission of workspace reads. An image also holds one of a small number of
daemon-wide image admissions from reading it until its response is written.

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
- An older server, without the operation, cannot open host files. Clients
  report that the server must be updated, and workspace files are unaffected.
- Formats that the server does not validate, such as HEIC or PDF, are not
  shown from the server. Supporting them means adding a validated kind.
