# Bash Execution

Run shell commands directly from the composer.

## Triggers

- `!command` — runs the command and adds output to session context (agent sees it)
- `!!command` — runs the command but excludes output from context (fire-and-forget)

## How it works

1. Composer submit detects `!` or `!!` prefix
2. Strips prefix and executes command via `AgentSession.executeBash()`
3. Output is displayed as a `BashEntry` in the transcript
4. For `!`: output is added to session context via `BashExecutionMessage`
5. For `!!`: output is excluded via `excludeFromContext: true`

## BashEntry Display

- Shows command with syntax highlighting
- Prefix indicates result: `✓` (success), `✗` (error), `⊘` (cancelled)
- Collapsible output (expanded by default for user-invoked commands)
- Click to expand/collapse long output

## Source

`src/shell/TranscriptPane.tsx` (BashEntry component)
`src/shell/composer-controller.ts` (prefix detection)
