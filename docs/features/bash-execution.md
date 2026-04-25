# Bash Execution

## What this covers

Kit supports direct user-triggered bash execution from the composer, in addition to normal agent use of the built-in `bash` tool.

## Current user-facing behavior

Users can run bash commands directly from the composer with:

- `!command` — run the command and include its output in model context
- `!!command` — run the command without including its output in model context

These commands render into the transcript as `bashExecution` entries.

## Transcript behavior

Bash executions appear in the transcript immediately when submitted.

Current behavior:

- a pending transcript entry is added as soon as the command is submitted
- the entry shows loading or in-progress state while the command is running
- the pending entry is replaced in place when execution completes
- completed entries include command, output, and exit status information

Pending bash transcript entries are not included in model context.

## Agent tool calls vs direct user execution

There are two related bash paths in Kit:

1. **Agent bash tool use**
   - the agent calls the built-in `bash` tool as part of a normal turn

2. **Direct user bash execution**
   - the user explicitly runs `!command` or `!!command` from the composer
   - the runtime injects a synthetic `bashExecution` transcript message

These share low-level bash execution code, but they represent different user flows.

## Source

- `src/tools/run-bash.ts`
- `src/tools/bash.ts`
- `src/runtime/agent-runtime.ts`
- `src/shell/composer-controller.ts`
- `src/shell/TranscriptPane.tsx`
