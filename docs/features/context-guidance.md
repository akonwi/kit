# Session context guidance

Kit composes each session's system prompt on the server. Context guidance is configuration for the session runtime; it is not copied into conversation history.

## `AGENTS.md` discovery

`AGENTS.md` is the only automatically discovered context filename. Kit does not load `CLAUDE.md`, casing variants, override files, sibling trees, or descendant files below the session working directory.

For each session Kit loads, in order:

1. `AGENTS.md` in the resolved Kit home;
2. when the cwd is inside a Git worktree, `AGENTS.md` in each directory from the worktree root through the cwd; or
3. outside a Git worktree, only `AGENTS.md` in the cwd.

The global location honors `KIT_HOME`. During v2 development the default Kit home is `~/.kit-v2`, so the default global file is `~/.kit-v2/AGENTS.md`.

A nested file applies automatically only when the session cwd is inside that file's directory. Repository guidance can point the model to other files that should be read on demand.

## Limits and diagnostics

Discovery is bounded to 64 existing candidates, 128 KiB per file, and 256 KiB of aggregate context. Kit omits a whole file rather than presenting a truncated fragment. When the aggregate limit is reached, the most local project guidance takes priority.

Unreadable, non-regular, invalid UTF-8, oversized, and over-budget candidates produce structured reload diagnostics without preventing an otherwise valid session from opening. Clients receive them in the reload result; the native TUI reserves toast feedback for operation errors.

## When changes take effect

Kit reads context when an authoritative session runtime is first loaded, including after daemon restart. Attaching another client to an already loaded runtime does not reread files, and Kit does not watch context files.

To apply edits to a loaded session, open the command palette with `Ctrl+P` or `/` from an empty composer and run **reload**. Reload remains available while a turn or direct bash command is active.

Reload atomically refreshes the system prompt, tool contributions, skills, and prompt commands while preserving conversation history, pending boundaries, and the current event stream. A provider request already in flight keeps the configuration it captured; the next provider request uses the refreshed configuration, whether it belongs to the current turn or the next one. If reload cannot build or validate the replacement configuration, the prior runtime remains active.

Changing the session cwd with `change_cwd` or `/cd <path>` immediately retargets relative filesystem tools but does not implicitly reload this guidance. The native TUI shows a warning suggesting **reload** when configuration should be refreshed from the destination. See [Session working directory](./session-cwd.md).

## Built-in customization guidance

Every normal session advertises the embedded `kit-customization` skill and includes the `activate_skill` tool. The skill directs the model to inspect the running Kit version's documentation and source, prefer supported user-editable surfaces, and avoid inventing settings or paths from another version. It has no user-owned filesystem location and cannot be shadowed by a user or project skill.

Kit also discovers user-global and project-local skills and prompt commands. See [Skills](./skills.md) and [Prompt commands](./prompt-commands.md) for locations, precedence, file formats, limits, and reload behavior.
