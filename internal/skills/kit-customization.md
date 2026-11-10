---
name: kit-customization
description: >-
  Use when the user asks about Kit itself, its behavior, documentation, or
  supported customization surfaces.
---

# Kit customization

Use these instructions when the user asks about Kit itself, its behavior, its documentation, or its supported customization surfaces.

- Use the canonical Kit repository at https://github.com/akonwi/kit as the product reference.
- Inspect the relevant documentation and source before changing Kit behavior.
- Read relevant Markdown documents completely and follow their cross-references before implementing.
- Prefer documented user-editable customization surfaces when they satisfy the request; do not change product source unnecessarily.
- Put global context guidance in `AGENTS.md` under the resolved Kit home.
- Put project context guidance in `AGENTS.md` files from the Git worktree root through the session working directory.
- Put user-global skills under the resolved Kit home's `skills/` directory and project skills under `.agents/skills/` in the session working directory.
- Put user-global prompt commands under the resolved Kit home's `prompts/` directory and project prompt commands under `.agents/prompts/` in the session working directory.
- Put user-global plugins in a directory with a `plugin.json` manifest under the resolved Kit home's `plugins/` directory, and project plugins under `.kit/plugins/` in the session working directory. Write new plugins to plugin protocol version 2, specified in `docs/plugin-protocol/v2.md`; their manifests declare `"protocolVersion": 2`.
- Write a TypeScript plugin with Kit's helper: copy `sdk/plugin/kit-plugin.ts`, and `sdk/plugin/kit-plugin-testing.ts` for its tests, from the Kit repository at the release tag of the running Kit version (`v` followed by the version `kit version` prints, such as `https://raw.githubusercontent.com/akonwi/kit/v0.58.0/sdk/plugin/kit-plugin.ts`). Do not install a package for it; none is published. Run the plugin with Bun, for example `"transport": { "type": "stdio", "command": "bun", "args": ["plugin.ts"] }`.
- A plugin whose `plugin.json` does not declare `"protocolVersion": 2` speaks protocol version 1, which Kit will remove. When you come across one, tell the user and offer to migrate it following `docs/plugin-protocol/migrating-from-v1.md`; migrate it only if they agree.
- Apply context, skill, prompt-command, or plugin edits to an already loaded idle session with the native command palette's `reload` command.
- Treat the running version's documentation and source as authoritative; do not assume a customization path or setting exists because another version supports it.
- If a requested customization surface is not supported by this Kit version, explain that limitation rather than inventing it.
