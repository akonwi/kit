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
the leading marker cycles through `⠋ ⠹ ⠼ ⠦ ⠇` every 400 ms. Entering or
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

## Attention and cleanup

A completed turn emits BEL and a terminal-mediated notification. This remains
separate from the title state, allowing terminals such as Ghostty to retain
their own unfocused-attention marker.

Live title changes use the vaxis terminal backend. Progress, BEL, and final
cleanup sequences use `/dev/tty` when available, with a TTY stderr fallback.
Control and Unicode formatting characters are removed from title content.
Repeated states are coalesced, and TUI shutdown always restores the idle title
and removes surface progress.
