# Terminal status

Kit reflects the attached session's state in terminal chrome without consuming
space inside the TUI. The title keeps the session context used by the production
client while using compact monochrome markers instead of emoji:

```text
idle       kit - <session name> - <cwd basename>
running    ⠋ kit - <session name> - <cwd basename>
feedback   ? kit - <session name> - <cwd basename>
```

An unnamed session omits the session-name segment. Status markers remain at the
start so narrow terminal tabs preserve the most important state. While running,
the leading marker cycles through `⠋ ⠹ ⠼ ⠦ ⠇` every 160 ms. Entering or
resuming running starts from the first frame; feedback stays static so motion
always means active work.

## State and precedence

The terminal state follows the attached session rather than daemon-global or
subagent work:

1. **Feedback** has highest precedence. It means the active parent agent turn
   is blocked on a user response. Agent interaction tools must use this state
   when those surfaces are implemented. Authentication and ordinary application
   dialogs do not trigger it.
2. **Running** means the attached session has an active parent turn, including a
   turn restored after reconnecting.
3. **Idle** is used otherwise.

Resolving feedback returns the title to running when the parent turn continues,
or to idle when no turn remains. Session rename, switching, and cwd changes
update the contextual title without losing the current state. Delayed events
from detached sessions cannot update the attached title because title state is
projected only from the active TUI state.

## Ghostty surface progress

On Ghostty, Kit also emits OSC 9;4 surface progress:

- running uses indeterminate progress;
- feedback pauses progress; and
- idle removes progress.

Raw OSC 9;4 output is disabled inside tmux and screen until explicit passthrough
wrapping is supported. Terminal titles remain available there, with animation
slowed to one frame per second to reduce multiplexer status-line redraws.

## Program Status Protocol

Kit also reports its state through the
[Program Status Protocol](https://www.superlogical.com/rex/docs/build/program-status)
(OSC 7501), so terminals and agent inboxes can show what Kit is doing without
reading its title. Kit owns only the root record, with `app=kit`, and reports it
with this precedence:

| Kit state | Record | Message |
| --- | --- | --- |
| signed out, or connecting a provider | `blocked`, `kind=auth` | Connect an AI provider |
| a model's confirmation waits | `blocked`, `kind=permission` | the request's title |
| any other model or plugin request waits | `blocked`, `kind=question` | the request's title |
| a turn or compaction runs | `working` | none |
| the last turn completed | `done` | Agent turn complete |
| the last turn failed or was interrupted | `error` | Agent turn failed |
| the last turn was aborted, or nothing has finished | `idle` | none |

The terminal treats each report as an event, so Kit reports only when the state
changes; repainting, renaming the session, or a duplicate completion event sends
nothing. A turn's outcome is reported once, when this client sees it settle, and
never from a snapshot of earlier turns. Later work supersedes it: after a
compaction, or after answering a request outside a turn, Kit reports `idle`
rather than the earlier outcome again. A shell command typed in the composer is
the user's own work and does not count as working.

Reports are sent whether or not the terminal answered the protocol's feature
query, since terminals that act on them may not answer it. Cooper disables them
inside tmux, GNU Screen, and Zellij, and when `COOPER_PROGRAM_STATUS` is `0`,
`false`, or `off`. On exit, Cooper clears the record, or leaves an unseen `done`
or `error` record for the terminal to dismiss.

## Attention and cleanup

A completed turn emits BEL and a terminal-mediated notification. This remains
separate from the title state, allowing terminals such as Ghostty to retain
their own unfocused-attention marker. A terminal that answered the Program
Status Protocol's feature query notifies from the records Kit reports, so Kit
rings the bell but sends no notification of its own there.

Title, progress, program status, BEL, and notification output go through the
Cooper runtime that drives the terminal client. Control and Unicode formatting characters are
removed from title content. Repeated states are coalesced, and TUI shutdown
always restores the idle title and removes surface progress.
