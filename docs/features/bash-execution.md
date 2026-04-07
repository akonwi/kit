# Bash Execution

## Status

Not currently wired in the active minimum loop.

## Goal

Support a direct shell-command UX from the composer and render command execution
results in the transcript.

## Current relevant pieces

- the app has a built-in `bash` tool in `src/tools/bash.ts`
- the transcript already knows how to render `bashExecution` entries via
  `BashEntry` in `src/shell/TranscriptPane.tsx`

## Historical / intended UX

Earlier iterations explored composer prefixes such as:

- `!command` — run command and include output in context
- `!!command` — run command without including output in context

That UX is **not currently active** in the rebuilt composer flow.

## Current caveat

There is a difference between:

- the agent calling the `bash` tool as part of a normal turn, and
- the user directly invoking ad-hoc shell execution from the composer

The first exists as part of the runtime/tooling foundation.
The second still needs a deliberate UX decision and wiring work.

## Source

- `src/tools/bash.ts`
- `src/shell/TranscriptPane.tsx`
