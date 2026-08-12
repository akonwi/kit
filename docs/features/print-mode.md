# Print mode

Kit can run a single prompt without starting the terminal UI:

```bash
kit -p "review PR 345"
```

Print mode:

- creates and persists a new session by default
- opens and continues an existing session with `--session <id-or-path>`
- keeps the main and sub-agent conversations in memory with `--no-session`
- loads headless-safe built-in plugins plus user and project external plugins
- skips prompt-command plugins and UI-only built-ins
- withholds Kit's user-interaction tools and their prompt guidance
- suppresses terminal completion notifications
- writes Kit-managed final assistant text to stdout
- redirects ordinary logs, diagnostics, and errors to stderr
- exits with a nonzero status when the request fails or is aborted

Piped stdin is prepended to the prompt:

```bash
cat changes.diff | kit -p "review this diff"
```

External plugins are discovered from `~/.kit/plugins/` and
`<session-cwd>/.kit/plugins/` before the prompt starts. Headless-compatible
contributions such as tools, tool-call interceptors, subagents, system-prompt
slots, and lifecycle events are active. Commands and chrome can register but
have no interactive surface in print mode. Plugin confirm, input, and select
requests cannot display a terminal dialog; they fail or return cancellation.

Sub-agent conversations created during a `--no-session` run also use in-memory
storage. MCP servers that require a new OAuth login must be authenticated
through interactive Kit before they can be used in print mode.

Prefix option-like prompt text with `--`, for example
`kit -p -- "--summarize this"`.
