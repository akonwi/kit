# 0075: Keep sessions usable without their working directory

## Status

Accepted. Amends the cwd-change rule of
[ADR 0010](0010-compose-session-system-prompts.md) and the stdio working
directory of [ADR 0028](0028-own-mcp-transport-construction.md).

## Context

A session's working directory is mutable state, not part of its identity. A
session often outlives the directory it was created in: a Git worktree is
removed after its work merges, a temporary checkout is cleaned up, or a
volume is unmounted. The session's transcript, scratchpad, subagents, and
settings don't depend on that directory, and the model or the person can
move the session to another directory at any time.

Building a runtime still discovers project configuration from the cwd:
`AGENTS.md` context, skills, prompt commands, agents, and MCP configuration.
Coding tools, direct shell commands, and MCP stdio servers also run relative
to it.

## Decision

### A missing cwd omits only project discovery

When a runtime bundle is built while the session cwd is unavailable (missing,
not a directory, or not accessible), Kit builds it from user-level
configuration alone: the Kit home's
`AGENTS.md`, skills, prompt commands, agents, and MCP servers. Project
discovery for the cwd is skipped rather than failing, and the bundle records
one `session.cwd_unavailable` prompt diagnostic naming the cwd and why it is
unavailable. Every session
operation that loads a runtime (snapshots, attach, prompts, reload, forks,
plugin and client-plugin routes) behaves as for any other session.

The session's published cwd is kept as it is. The workspace reports it as
`unavailable` (ADR 0019), and clients show that through their existing
workspace surfaces. Kit doesn't add a system-prompt note about the missing
directory; tools report it when they need it.

### Tools fail per call

A tool that needs the cwd fails that one call with an error that names the
directory and says what to do:

- `bash` and direct shell commands fail before starting a process:
  `the working directory <cwd> does not exist; change the working directory`.
- `read`, `write`, `edit`, `ls`, `grep`, `find`, `show_image`,
  `inspect_image`, and `save_attachment` refuse relative paths, including the
  implicit cwd of `ls`, `grep`, and `find`:
  `the working directory <cwd> does not exist; change the working directory or use an absolute path`.

Absolute paths work as usual. Refusing relative writes means a session
never silently recreates a directory that was removed on purpose. A relative
write creates its parent directories through a handle on the cwd, so a cwd
removed during the call fails the write rather than being recreated.

### MCP stdio servers start in the home directory

A stdio server with no configured `cwd` starts in the session cwd when it
exists and in the user's home directory otherwise. The directory is chosen
each time a server process starts, so a server started after the cwd returns
starts there again. A configured `cwd` is used as written.

### Moving away from a missing cwd rediscovers the project

`change_cwd` and `/cd` accept a session whose current cwd is missing; the
target must still exist. When the session's runtime bundle was built while
its cwd was unavailable, a successful change to an existing directory also
reloads the runtime (ADR 0010's reload), so project context, skills, prompt
commands, agents, and MCP configuration come from the new directory. This
also applies when the target is the same directory after it has been
restored.

The change's result tells the actor that the configuration was reloaded: the
model's `change_cwd` result says so, and a user's cwd boundary says that
context was reloaded from the new directory. A replay of the same cwd
mutation reports the same outcome while the runtime stays loaded. A reload that cannot run, such
as during another runtime transition, leaves the change applied and the
result unchanged; an explicit reload rediscovers later. Moving between
existing directories keeps ADR 0010's rule that a cwd change doesn't reload.

A cwd that comes back without a cwd change is picked up by the next reload or
runtime load.

## Consequences

- A session whose worktree was removed opens, shows its transcript, and
  keeps working after daemon restarts.
- Project-specific configuration is absent until the session moves to an
  existing directory or its directory returns and the runtime reloads.
- The session API and protocol don't change. Clients keep using the
  workspace `unavailable` state they already show.
