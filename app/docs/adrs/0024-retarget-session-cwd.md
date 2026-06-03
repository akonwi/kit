# 0024: Retarget session cwd

## Status

Accepted

## Context

Kit currently treats a session's `cwd` as effectively fixed. Session creation, session lookup, tools, context file discovery, VCS state, project-local plugins, skills, sub-agents, MCP config, review flows, and UI metadata all derive behavior from the session cwd.

ADR 0006 deferred mutable cwd because the runtime and UX did not yet have clear answers for persistence, tool rebuilding, shell semantics, and session identity.

We now want sessions to be able to move across directories. A user may begin a session in one directory, discover that the relevant code lives somewhere else, and intentionally retarget the same session to that new directory. After retargeting, launching Kit from the new directory with no explicit session id should resume that session.

This differs from models where cwd is immutable for the lifetime of a coding-agent session.

## Decision

Kit sessions should support an explicit cwd change operation.

A cwd change updates the session's current working directory while preserving the session identity and transcript. It is not a fork, handoff, or new session. The runtime API should expose this as `changeCwd`.

The session's persisted current `cwd` is the source of truth for:

- default tool execution and relative path resolution
- context file discovery
- VCS status and repository-aware UI
- project-local plugins, prompt commands, skills, sub-agents, and MCP config
- code review and file discovery features
- terminal title, header/footer cwd display, and session explorer metadata
- `kit` with no arguments selecting the most recent session for the launch directory

Retargeting should be persisted as durable session metadata. With JSONL session storage, a cwd change should be represented as an append-only metadata entry rather than by rewriting historical entries. Loading a session reconstructs the latest cwd from the header plus later metadata entries.

Kit should keep enough historical information to explain that the session moved. Historical transcript entries should not be reinterpreted as if they originally occurred in the latest cwd. New relative paths and tool calls after the retarget resolve against the latest cwd.

The active session cwd should be an application/runtime concept, not an assumption that `process.cwd()` is always current. `process.cwd()` is useful for initial launch/session lookup, but after bootstrap app code should prefer the active session cwd from runtime/session state. Explicit session cwd changes should also move the process cwd with `process.chdir()` so shell-backed functionality can recover when an old working directory, such as a deleted git worktree, disappears.

Changing cwd should rebuild or refresh cwd-dependent runtime state atomically enough that the next agent turn and UI render both observe the same target directory. At minimum, retargeting needs to refresh:

- default tool instances
- effective system prompt/context files
- VCS watcher and VCS chrome
- file indexes and file pickers
- project-local extension/discovery surfaces
- process cwd
- terminal title and visible session metadata

A shell command such as `cd other-dir` inside a tool call should not implicitly retarget the session. Tool calls are isolated executions.

Changing cwd should be available through both:

- a user-facing `/cd` Kit command for explicit manual cwd changes
- an agent-usable tool so the agent can adapt the session cwd when work moves to another directory

The agent tool does not require an additional confirmation prompt. Changing cwd is a normal session operation, and the resulting cwd change is visible in session metadata/UI.

Session lookup by cwd should use the latest persisted cwd, not only the cwd in the original session header. Once a session is retargeted to `/new/project`, running Kit from `/new/project` with no arguments should consider that session a session for `/new/project`.

Sessions should be discoverable from their latest cwd. Retargeting does not need to keep the session listed under previous directories for default cwd-based resume. Older directories may still appear in cwd history if Kit later adds a dedicated history view, but that history should not affect default `kit` resume semantics.

## Consequences

### Positive

- A long-running session can follow the user's actual work across directories.
- Users can recover a moved session naturally by launching Kit from the new directory.
- Kit can diverge from coding-agent models where cwd is immutable.
- Existing session identity, transcript, compaction, and handoff history remain intact.

### Trade-offs

- Runtime state becomes more dynamic because many subsystems depend on cwd.
- Session loading and summary indexing must understand cwd metadata entries.
- Historical transcript references may span multiple directories, so UI and prompt context need to avoid pretending all past work happened under the latest cwd.
- Project-local plugins and other cwd-derived capabilities may appear, disappear, or change behavior after retargeting.
- Tests need to cover both startup lookup and in-session retarget behavior.

### Constraints

- Retargeting must not silently occur from ordinary shell `cd` commands.
- The agent retarget tool may change cwd without a separate confirmation prompt.
- Cwd-dependent state should not be cached only from process startup.
- Explicit session cwd changes should update `process.cwd()` after the target directory is validated.
- Default cwd-based resume should use the latest cwd only.
- Session lists and pickers should make cwd moves understandable when relevant.

## Related

- `docs/adrs/0002-storage-paths.md`
- `docs/adrs/0006-mutable-cwd.md`
- `docs/adrs/0020-jsonl-session-storage.md`
- `src/storage/session-storage.ts`
- `src/runtime/agent-runtime.ts`
