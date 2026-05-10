# Tool call approval configuration

Status: deferred

## Goal

Support user-configured approval prompts before matching tool calls execute.

This is enforcement, not prompt guidance: the model may request any tool, but Kit decides whether a matching call needs user approval before execution.

## Config locations

Use Kit-specific settings files:

- Global: `~/.kit/settings.json`
- Project-local: `.kit/settings.json`

Do **not** use `AGENTS.md` for this. Context files are prompt guidance, not structured enforcement policy.

Do **not** use `.agents/settings.json`. The project-local `.agents/` directory is reserved for general agent compatibility surfaces such as MCP, skills, and prompts. Kit-specific project settings should live under `.kit/`.

## Precedence

Project settings take precedence over global settings for this feature.

For `toolApprovals`, use replacement semantics:

1. If project `.kit/settings.json` defines `toolApprovals`, use that list.
2. Otherwise use global `~/.kit/settings.json` `toolApprovals`.
3. If neither defines it, no tool calls require approval.

This intentionally allows project config to be looser than global config.

## Config shape

The config only specifies calls that require approval. Non-matching calls are allowed.

```json
{
  "toolApprovals": [
    { "tool": "bash" },
    {
      "tool": "write",
      "args": {
        "path": { "glob": "**/.env*" }
      }
    },
    {
      "tool": "bash",
      "args": {
        "command": { "matches": "\\b(rm|sudo|chmod|chown|git push)\\b" }
      }
    },
    {
      "tool": "mcp",
      "args": {
        "tool": { "glob": "github.*" }
      }
    }
  ]
}
```

No `allow`/`deny` actions are needed in v1. A rule match means "ask for approval".

## Matching model

A rule matches when:

- `tool` matches the tool call name
- every configured arg matcher matches the validated tool arguments

Multiple arg matchers in one rule are ANDed. Multiple rules are ORed.

Initial matcher types:

```ts
type ToolApprovalRule = {
  tool: string;
  args?: Record<string, ArgMatcher>;
};

type ArgMatcher =
  | string
  | {
      equals?: unknown;
      contains?: string;
      matches?: string;
      glob?: string;
      exists?: boolean;
    };
```

Notes:

- string matcher shorthand means exact equality
- `matches` is a regular expression string
- `glob` is useful for paths and namespaced tool identifiers
- missing arguments fail unless the matcher is `{ "exists": false }`
- nested argument paths can be supported with dot-path keys if needed

## Approval UI

Add a `ToolApprovalDialog` using the existing `Dialog.*` components.

Suggested layout:

- Header title: `Approve tool call?`
- Header meta: tool name, e.g. `bash`
- Body:
  - concise tool call summary
  - tool-specific argument preview when possible:
    - `bash`: command preview
    - `write`/`edit`: target path and concise change summary
    - `mcp`: requested MCP tool name and parsed args if practical
- Footer hint bar:
  - `←/→ choose`
  - `Enter confirm`
  - `Esc deny`

The dialog should behave like an AlertDialog: it blocks the pending call until the user approves or denies.

## Runtime integration

Use the Pi/Kit before-tool-call hook path:

- evaluate approval rules in a `beforeToolCall` hook
- if no rule matches, return `undefined` and let the tool run
- if a rule matches, open the approval dialog and await the result
- approval returns `undefined`
- denial returns `{ block: true, reason: "Tool call denied by user." }`

Implementation likely needs an `AgentRuntime` API for registering/composing before-tool-call hooks so UI-driven features can install approval behavior without reaching into `KitAgent` internals.

## Implementation outline

1. Add project settings discovery for `.kit/settings.json`.
2. Extend settings types/sanitization with `toolApprovals`.
3. Add a rule matcher module with tests.
4. Add a tool approval controller/dialog using `Dialog.*` and `HintBar`.
5. Wire the controller into the runtime before-tool-call path.
6. Add docs for the settings shape and precedence.
