# 0010: Compose session-scoped system prompts from Kit-owned guidance

## Status

Accepted

## Context

Every droid model request needs a system prompt that combines Kit's stable
operating instructions with guidance applicable to one session. That guidance
may come from filesystem context, built-in capabilities, skills, and eventually
session-owned plugin processes.

The server is authoritative for sessions and may keep a session running after
clients detach. Prompt composition therefore cannot depend on a TUI, a client's
process cwd, or files read directly by a client. It must also avoid placing
Kit-specific filesystem and feature knowledge inside the provider-neutral
droids runtime.

Context guidance is configuration, not conversation history. It should be
recomputed deliberately when a session runtime is created or reloaded, while an
already executing model cycle must continue with the prompt it started with.

## Decision

### Ownership

Kit's authoritative server owns system-prompt composition. A Kit-owned prompt
builder receives an explicit session identity, session cwd, application paths,
and ordered feature contributions. It discovers guidance and returns the final
prompt plus diagnostics describing its sources.

Droids receives the assembled prompt through `droids.Config.SystemPrompt` and
treats it as opaque configuration. Droids does not discover `AGENTS.md`, inspect
Kit paths, load skills, or understand feature-policy sections. The native TUI,
print client, and future remote clients do not read server-side context files or
assemble prompts.

```text
session supervisor
  prompt builder
    core prompt
    feature guidance
    available skills
    session context files
  droids.Config.SystemPrompt
    provider system/instructions field
```

Prompt composition uses the session record's cwd directly. It never changes or
reads a process-global working directory.

### Minimal core prompt

The literal default core prompt is:

```text
You are Kit, a coding assistant running in the terminal.
You have access to tools to read and modify files, run commands, search code, and more.
Be concise and direct. Prefer surgical edits over full rewrites when practical.
```

The core prompt contains only stable behavior shared by every Kit session. It
does not enumerate optional customization surfaces or contain instructions
about when feature-specific policy should be registered.

### Ordered composition

The effective prompt is assembled from named sections in this order:

1. the core prompt;
2. guidance registered by available built-in features;
3. the available-skill catalog and its activation guidance;
4. stable prompt slots owned by active session plugins, when plugin support is
   available; and
5. session context files.

Empty sections are omitted. Sections are separated by two newlines. Feature and
plugin contributions have stable identities and deterministic order; prompt
ordering must not depend on goroutine completion or map iteration. Removing or
reloading a contributor removes its section atomically.

A feature registers model guidance only while the capability described by that
guidance is available to the droid. This is a composition invariant, not text
included in the model prompt.

### Kit customization as a built-in skill

Kit-specific customization and documentation instructions live in an embedded,
built-in skill named `kit-customization`, not in the core prompt. Its metadata
is advertised in the available-skill catalog. Activating it returns the full
instructions needed to:

- inspect Kit's relevant documentation and source before changing Kit;
- follow documentation cross-references;
- prefer supported user-editable customization surfaces over product-source
  changes when they satisfy the request;
- use the canonical Kit repository as a reference; and
- locate the supported settings, themes, context, skills, prompts, agents, and
  MCP configuration surfaces.

The skill's contents describe only surfaces supported by the running Kit
version. The embedded definition has a reserved stable identity and cannot be
shadowed by a user or project skill. It does not depend on a file under the
user's Kit home.

The `activate_skill` tool is part of every normal Kit session's stable tool
surface. Its registry and model-visible catalog vary by session. Kit discovers
user-global skills from the resolved Kit home's `skills/` directory and project
skills from `<session-cwd>/.agents/skills/`; the embedded reserved skill wins any
name collision, followed by user and then project definitions. Plugin skills may
extend the registry separately.

A filesystem skill is a recursively discovered directory containing `SKILL.md`.
Its YAML frontmatter supplies a matching stable name, required description, and
optional `disable-model-invocation` flag. Discovery is deterministic and bounded,
does not follow escaping skill roots, and reports malformed, unreadable,
duplicate, reserved, and over-limit definitions as structured diagnostics. The
catalog and activation tool are always derived from the same immutable discovery
snapshot.

### `AGENTS.md` context discovery

`AGENTS.md` is Kit's only filesystem context convention. Kit does not discover
or fall back to `CLAUDE.md`.

For a session cwd, Kit first attempts to resolve the enclosing Git worktree
root. It then discovers:

1. the global `AGENTS.md` under the resolved Kit home;
2. inside a Git worktree, `AGENTS.md` in every directory from the worktree root
   through the session cwd, inclusive; and
3. outside a Git worktree, `AGENTS.md` in the session cwd only.

The global file is first. Repository files are ordered from the worktree root
to the session cwd. Kit never searches above the resolved worktree root. Git
worktree discovery is best-effort; inability to resolve one safely falls back
to cwd-only project context. Duplicate resolved paths are included once.

Kit does not scan sibling or descendant directories for additional context. A
nested `AGENTS.md` becomes automatic context when the session cwd is within its
directory tree. Repository-level guidance may reference other nested
`AGENTS.md` files so the model can read them on demand when work enters those
areas.

The global path comes from Kit's application-path resolver and honors its
configured home override. Prompt text does not hardcode the location of Kit's
home.

### Context rendering

When at least one context file is loaded, Kit appends:

```text
Additional context guidance has been loaded from project context files. Follow it unless it conflicts with higher-priority instructions.

<context-files>
<context-file path="/absolute/path/AGENTS.md">
...file contents...
</context-file>
</context-files>
```

Each path is an absolute normalized path encoded safely as an attribute value.
File contents remain trusted instruction text and are not interpreted as
conversation messages. With no loaded context files, the wrapper and preamble
are omitted.

### Bounds and diagnostics

Context discovery and rendering are deterministic and bounded. The
implementation defines and tests limits for individual files, the number of
sources, and aggregate loaded bytes. It does not partially truncate a context
file and present the fragment as complete guidance.

An unreadable, invalid, or oversized candidate is omitted and produces a
structured diagnostic. Aggregate-budget selection preserves the most local
applicable project guidance rather than allowing broader repository-root
guidance to crowd it out. Loaded source paths, omitted source paths, and
warnings are available through server-owned diagnostic state for future TUI,
print, and web presentation.

A context-file diagnostic does not prevent opening the session unless prompt
assembly cannot produce a valid bounded result.

### Session lifecycle and reload

A session's prompt is assembled when its authoritative runtime is first created
or lazily reopened, including after daemon restart. Merely attaching another
client does not mutate or reload an already loaded runtime.

Kit provides a session-scoped explicit reload operation. Reload:

1. requires the session's parent droid to be quiescent;
2. rediscovers context from that session's cwd;
3. refreshes available Kit-owned feature and skill contributions;
4. replaces the complete effective prompt and cwd-bound tool configuration as
   one runtime transition; and
5. leaves droid conversation history and provider replay metadata unchanged.

A busy session rejects reload with a typed error rather than changing guidance
between model cycles. A cwd change uses the same quiescent transition and does
not publish the new cwd unless prompt and tool rebuilding succeeds.

There is no filesystem watcher in the initial implementation. File edits become
effective after explicit reload, a successful cwd change, runtime eviction and
reopen, or daemon restart.

Because `droids.Config.SystemPrompt` is runtime configuration rather than
durable conversation data, Kit may implement a quiescent refresh by closing and
reopening the droid against its existing Store. Any future droids reconfiguration
API must preserve the same quiescent and atomic semantics.

### Persistence and compaction

Filesystem guidance and the effective system prompt are not copied into droid
message history or Kit's session registry. Reopening a runtime reads the current
guidance from its authoritative files.

Droid compaction replaces only conversation context. The effective Kit system
prompt remains separate and is supplied to context measurement and every normal
model request. A compaction summary is not a substitute for `AGENTS.md`
guidance.

## Required properties

The implementation must demonstrate:

- the exact core prompt remains stable under an exact test;
- prompt sections have deterministic identities, ordering, replacement, and
  removal;
- droids and client packages do not import Kit context-discovery
  implementations;
- two concurrently loaded sessions resolve guidance from their own cwd without
  process-global cwd mutation;
- global and repository-root-to-cwd `AGENTS.md` files are composed in the
  specified order;
- sibling and descendant `AGENTS.md` files outside the cwd ancestry are not
  preloaded;
- project discovery never searches above an enclosing Git worktree root and
  falls back to cwd-only context outside a worktree;
- `CLAUDE.md` is never discovered;
- unreadable and over-budget sources produce bounded diagnostics while local
  guidance retains priority;
- context contents are present in the system prompt but absent from droid
  conversation history;
- `activate_skill` is available in every normal Kit session;
- `kit-customization` is embedded, advertised, activatable, and protected from
  name shadowing;
- user-global and project `SKILL.md` definitions are discovered deterministically,
  bounded safely, surfaced in diagnostics, and refreshed atomically on reload;
- attaching a client does not reload an existing runtime;
- explicit reload and cwd changes reject active sessions and update prompt plus
  tools atomically while quiescent;
- reopening after runtime eviction or daemon restart uses current context-file
  contents without changing droid history; and
- compaction requests and normal requests preserve their distinct system-prompt
  purposes.

## Consequences

### Positive

- Every session receives deterministic guidance derived from its own workspace.
- Droids remains provider-neutral and independent of Kit configuration files.
- The default prompt stays small while optional behavior remains discoverable
  through capabilities and skills.
- Kit-specific customization guidance can evolve as one embedded skill without
  increasing every request's core prompt.
- Explicit reload avoids mid-turn prompt mutation and works with detached or
  future multi-client sessions.
- Diagnostics make omitted guidance visible instead of silently changing agent
  behavior.

### Trade-offs

- Prompt composition, source limits, diagnostics, and reload coordination add a
  server-side subsystem.
- Users must reload a session before edited guidance affects a loaded runtime.
- Removing `CLAUDE.md` compatibility is an intentional parity difference.
- A built-in customization skill requires a minimal skill registry and
  activation tool before general user/project skill parity is complete.
- Reopening a droid for prompt changes may replace transient subscriptions and
  require clients to resynchronize from an authoritative snapshot.

## Related

- [0001: Native Go application architecture](./0001-native-go-architecture.md)
- [0004: Model a droid as an autonomous agent runtime](./0004-droids-agent-runtime-boundary.md)
- [0006: Make droids authoritative for session conversation data](./0006-droids-as-session-data-authority.md)
- [`../parity.md`](../parity.md)
- [`../features/context-guidance.md`](../features/context-guidance.md)
- [`../features/skills.md`](../features/skills.md)
