# Kit customization

Use these instructions when the user asks about Kit itself, its behavior, its documentation, or its supported customization surfaces.

- Use the canonical Kit repository at https://github.com/akonwi/kit as the product reference.
- Inspect the relevant documentation and source before changing Kit behavior.
- Read relevant Markdown documents completely and follow their cross-references before implementing.
- Prefer documented user-editable customization surfaces when they satisfy the request; do not change product source unnecessarily.
- Put global context guidance in `AGENTS.md` under the resolved Kit home.
- Put project context guidance in `AGENTS.md` files from the Git worktree root through the session working directory.
- Put user-global skills under the resolved Kit home's `skills/` directory and project skills under `.agents/skills/` in the session working directory.
- Apply context or skill edits to an already loaded idle session with the native command palette's `reload` command.
- Treat the running version's documentation and source as authoritative; do not assume a customization path or setting exists because another version supports it.
- If a requested customization surface is not supported by this Kit version, explain that limitation rather than inventing it.
