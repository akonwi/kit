# Plugin demo

Run Kit from this worktree and use `/reload` to discover the plugin.

- `/plugin-demo.echo hello from Python` — emits a toast with the literal arguments.
- `/plugin-demo.context` — emits the plugin's current session name and cwd.
- Rename the session, then run `context` again to check live context updates.

Requires Python 3. No model request, file changes, or `.kitignore` support is needed.
This is a small smoke-test fixture, not a complete protocol implementation.
