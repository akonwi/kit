# Bash Execution

Kit supports direct user-triggered bash execution from the composer, in addition to normal agent use of the built-in `bash` tool.

Users can run bash commands directly from the composer with:

- `!command` — run the command and include its output in model context
- `!!command` — run the command without including its output in model context

These commands render into the transcript as `bashExecution` entries.

Current behavior:

- a pending transcript entry is added as soon as the command is submitted
- the entry shows loading or in-progress state while the command is running
- the pending entry is replaced in place when execution completes
- completed entries include the command, output, and exit status
- pending bash transcript entries are not included in model context

Kit also has a separate agent-driven `bash` tool path. The low-level execution code is shared, but direct user bash execution and agent tool use are different flows.

## How to access it

Type a bash command directly into the composer:

- `!ls`
- `!!git status`
