# 0042: Discover project Claude Code commands and record prompt command invocations

## Status

Accepted. Amends [ADR 0010](0010-compose-session-system-prompts.md).

## Context

Prompt commands are Markdown templates that the server discovers for a session
runtime and expands into an ordinary user prompt. Users who also work in Claude
Code keep their reusable prompts as Claude Code custom slash commands. A
project's commands live in its `.claude/commands/` directory, alongside the
project and often committed with it. These files use the same shape as Kit
prompt templates: a Markdown body, optional YAML frontmatter with `description`
and `argument-hint`, and `$ARGUMENTS` and positional `$1`, `$2`, …
placeholders.
Maintaining a second copy of each command for Kit adds friction without adding
value.

Claude Code commands also use features Kit does not support: frontmatter such as
`allowed-tools` and `model`, `` !`command` `` shell execution at invocation
time, and `@path` file inclusion. Executing shell commands from discovered files
at invocation time would be a new trust decision.

Kit uses `AGENTS.md` as its only context convention and does not discover
`CLAUDE.md` (ADR 0010). Commands differ from context: a command has no effect
until the user invokes it.

An expanded prompt command is the model input for its turn, but the template
body is rarely what the user wants to read back in the transcript or recall
from prompt history. The invocation, such as `/review auth module`, is the
user's own input.

## Decision

### Claude Code command discovery

Prompt command discovery also reads a project's Claude Code custom commands,
as the lowest precedence source. For a session runtime, the server discovers
non-recursive `*.md` files from these directories, in precedence order:

1. the resolved Kit home's `prompts/` directory;
2. `<session-cwd>/.agents/prompts/`;
3. `<session-cwd>/.claude/commands/`.

The first definition of a name wins. Built-in palette commands keep their names.
Kit does not read user-level Claude Code commands from `~/.claude/commands/` or
any other Claude Code configuration directory. Subdirectories are not scanned,
so namespaced Claude Code commands are not discovered.

Claude Code commands use the prompt command template format and expansion
unchanged:

- The filename without `.md` is the command name.
- Frontmatter `description` and `argument-hint` are used. All other frontmatter
  keys are ignored.
- `$ARGUMENTS`, `$@`, positional, and slice placeholders expand as for any
  prompt command. Kit's argument parsing applies: quotes group arguments and
  are not part of the expansion, so `$ARGUMENTS` is the parsed arguments joined
  by spaces rather than the raw argument text.
- `` !`command` `` and `@path` syntax is left in the expanded text as literal
  text. Kit never executes commands or reads files while expanding a template.

Claude Code commands share prompt command discovery bounds, the 128-command
session limit, symlink and traversal rules, and reload semantics. Because they
are discovered last, they cannot displace Kit prompt commands from the limit.

Prompt command metadata identifies Claude Code commands with the source value
`claude_project`, alongside `user` and `project`. Clients may use the source to
label or group commands; they present `claude_project` as `claude`.

### Setting

The `readClaudeConfigs` boolean in `$KIT_HOME/settings.json` controls whether
Kit reads Claude Code configuration. It defaults to `true`. When it is `false`,
discovery skips `<session-cwd>/.claude/commands/`. A non-boolean value produces
a settings warning and uses the default. A changed value takes effect at the next
prompt command discovery for a runtime, when the runtime loads or reloads.

The setting governs Claude Code configuration discovery as a whole. Project
commands are the only Claude Code configuration Kit reads. `CLAUDE.md` is never
discovered, whatever the setting (ADR 0010).

### Prompt command invocations in the transcript

A user message submitted from a prompt command records the invocation together
with its expansion. The message content carries a prompt command block with the
command name, its raw arguments, its source, and the expanded text. The block
stores the expansion made at submission, so later changes to the template file
do not change recorded history.

- Model requests, compaction, plugin turn projections, and every other consumer
  of model-facing user text use the expanded text, exactly as for a typed
  prompt. The model does not receive the command name or a framing marker.
- Transcript projections expose the block as a `promptCommand` content kind
  with `name`, `arguments`, `source`, and `text`. Clients present the user
  message as the invocation, `/<name> <arguments>`, and show the expanded text
  on demand.
- Prompt history recall restores the invocation text, so submitting a recalled
  invocation runs the command again with the current template.
- The follow-up queue preview of a queued prompt command is the same invocation
  text.

This applies to every prompt command, whatever its source. User messages from
typed prompts are unchanged.

### Protocol

The new prompt command source value and transcript content kind are session
protocol changes. They are published through the session contract, generated
OpenAPI document, and Swift client together and bump the session protocol
version.

## Consequences

- Claude Code users can run a project's existing custom commands in Kit without
  copying them, and edits to those files apply after reload.
- A Kit prompt with the same name shadows a Claude Code command. Personal
  Claude Code commands and namespaced commands in subdirectories are
  unavailable in Kit.
- Claude Code commands that rely on `` !`command` ``, `@path`, `allowed-tools`,
  or `model` run in Kit without those effects. Their literal syntax reaches the
  model, which may still act on it. Commands whose arguments depend on quotes
  surviving expansion behave differently than in Claude Code.
- Discovery reads a Claude Code directory in the session cwd by default. Users
  who do not want Kit to read Claude Code configuration set
  `readClaudeConfigs` to `false`. Discovery stays confined to the Kit home and
  the session cwd, so isolating `KIT_HOME` remains sufficient for development
  and tests.
- The transcript and prompt history show what the user invoked rather than the
  template body, while the model input is unchanged.
- Older transcripts contain prompt command expansions as plain text and are
  presented as typed prompts.
- Every client and provider path that handles user message content must handle
  the prompt command block. Clients built for an earlier session protocol
  cannot attach to a daemon that implements this decision.
