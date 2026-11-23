# Session working directory

A Kit session owns an explicit working directory used as the scope for relative filesystem operations. It is independent of Kit's process working directory and of every other concurrently loaded session.

## Changing directories

The model can call the synchronous sequential `change_cwd` tool. The native command palette also provides:

```text
/cd <path>
```

Relative targets resolve from the session's current cwd. Absolute paths are accepted, `~` resolves to the user's home directory, and the destination must already exist as a directory.

A successful change and its idempotency receipt are persisted before the cwd is published to the loaded runtime, so retrying the same relative mutation cannot move twice after an ambiguous transport failure or interrupted tool call. Kit does not call `chdir` on its own process. The next relative coding-tool or direct composer-bash execution snapshots the new cwd when execution begins; a command already running continues in the directory where it started. Absolute tool paths are unchanged.

A user-initiated `/cd` also appends an idempotent durable cwd boundary to the droid, so the next model request knows that relative filesystem scope moved and that project configuration has not been reloaded. A model-initiated `change_cwd` already observes its own tool result and does not receive a duplicate boundary.

The native TUI updates its cwd and Git/location footer and shows a normal warning toast:

```text
Working directory changed
Now /path/to/destination · run /reload to refresh agent context
```

## Context remains explicit

Changing cwd is filesystem navigation. It does not rebuild the droid, replace the session event stream, or rediscover:

- `AGENTS.md` guidance;
- project skills under `.agents/skills/`; or
- project prompt commands under `.agents/prompts/`.

Run **reload** from the command palette after moving when those agent-configuration snapshots should be refreshed from the new cwd. Moving away from a working directory that no longer exists is the exception: it reloads them automatically (see below). Reload may run during an active turn: a provider request already in flight keeps its captured configuration, and the next provider request uses the refreshed prompt and tools. `change_cwd` remains safe as an in-run sequential tool because it changes only the synchronized filesystem scope.

## When the working directory is missing

A session keeps working when its directory is deleted, for example a worktree removed after its work merged ([ADR 0075](../adrs/0075-keep-sessions-usable-without-their-working-directory.md)). It opens and shows its transcript, and the model can keep working:

- Only user-level `AGENTS.md`, skills, prompt commands, agents, and MCP servers load. MCP stdio servers without a configured `cwd` start in your home directory.
- File panes show the workspace as unavailable.
- Shell commands, and tools given relative paths, fail with `the working directory <path> does not exist` and suggest changing it. Absolute paths still work, and a relative write never recreates the removed directory.
- `/cd` or `change_cwd` to an existing directory moves the session and reloads its project context, skills, and prompt commands from there. If the directory is restored, `/cd` to it or `/reload` picks its project configuration up again.
