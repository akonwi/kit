# Print mode

Kit can run a single prompt without starting the terminal UI:

```bash
kit print "review PR 345"
```

Print mode:

- resumes the most recent session for the current directory by default, creating and persisting one when none exists
- opens and continues an existing session with `--session <long-or-short-id>`
- keeps the main conversation in memory with `--temp` and disposes it when the foreground command exits
- loads headless-safe built-in plugins plus user and project external plugins
- skips prompt-command plugins and UI-only built-ins
- withholds Kit's user-interaction tools and their prompt guidance
- suppresses terminal completion notifications
- writes Kit-managed final assistant text to stdout
- redirects ordinary logs, diagnostics, and errors to stderr
- exits with a nonzero status when the request fails or is aborted

Piped stdin is prepended to the prompt:

```bash
cat changes.diff | kit print "review this diff"
```

External plugins are discovered from `~/.kit/plugins/` and
`<session-cwd>/.kit/plugins/` before the prompt starts. Headless-compatible
contributions such as tools, tool-call interceptors, subagents, system-prompt
slots, and lifecycle events are active. Commands and chrome can register but
have no interactive surface in print mode. Plugin confirm, input, and select
requests cannot display a terminal dialog; they fail or return cancellation.

Sub-agent conversations created during a `--temp` run will also use in-memory
storage when subagent support is restored. MCP servers that require a new OAuth
login must be authenticated through interactive Kit before they can be used in
print mode.

Prefix option-like prompt text with `--`, for example
`kit print -- "--summarize this"`.
