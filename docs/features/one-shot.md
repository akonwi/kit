# One-shot mode

Kit can run a single prompt without starting the terminal UI:

```bash
kit -p "review PR 345"
```

One-shot mode:

- creates an ephemeral session that is not written to Kit's session history
- loads only headless-safe built-in plugins
- skips user plugins, project plugins, prompt-command plugins, and UI-only built-ins
- withholds Kit's user-interaction tools and their prompt guidance
- suppresses terminal completion notifications
- writes Kit-managed final assistant text to stdout
- redirects ordinary plugin logs, diagnostics, and errors to stderr
- exits with a nonzero status when the request fails or is aborted

Piped stdin is prepended to the prompt:

```bash
cat changes.diff | kit -p "review this diff"
```

Sub-agent conversations created during a one-shot run also use in-memory storage. MCP servers that require a new OAuth login must be authenticated through interactive Kit before they can be used in one-shot mode.

User and project plugins are not loaded in one-shot mode, so their tools, commands, policies, and hooks are unavailable. Prefix option-like prompt text with `--`, for example `kit -p -- "--summarize this"`.
