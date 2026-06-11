# Steering & Follow-up Messages

Kit can queue messages for the agent while it is still working on the current turn.

There are two relevant composer behaviors:

- **follow-up** — queue a message so it runs after the agent becomes idle
- **steering promotion** — promote already queued follow-ups so they are delivered before the next model call

## Current behavior

While the agent is streaming:

- typing a message and pressing `Enter` queues it as a follow-up
- queued follow-ups are shown below the agent status line and directly above the composer while they are pending
- the status bar shows `queued messages: N · Alt+Q edit · ↑ restore` when follow-ups are queued
- when the composer is empty and queued follow-ups exist, pressing `Enter` promotes those queued follow-ups to steering
- pressing `Up` in an empty composer restores queued follow-ups back into the composer; if none exist, it opens user message history for recall
- pressing `Alt+Q` opens the queued follow-up editor
- running `edit-queue` from the command palette opens the same editor
- queued follow-ups clear from the visible stack when the next turn begins consuming them

## Queue editor

The queue editor lets pending follow-ups be changed before the agent consumes them.

Open it with `Alt+Q` or from the command palette.

Inside the editor:

| Key | Action |
| --- | --- |
| `↑` / `↓` | Select a queued follow-up |
| `Enter` | Edit the selected follow-up |
| `d` | Delete the selected follow-up |
| `c` | Clear queued follow-ups; confirms when multiple are queued |
| `Esc` | Close the editor, or cancel the current edit/confirmation |

While editing a queued follow-up:

| Key | Action |
| --- | --- |
| `Enter` | Save the edit |
| `Shift+Enter` | Insert a newline |
| `Esc` | Cancel editing |

If an edit is saved as empty text, the queued follow-up is removed.

## How to use it

- type a message and press `Enter` while streaming to queue a follow-up
- press `Enter` again in an empty composer to promote queued follow-ups to steering
- press `Up` in an empty composer to restore queued follow-ups back into the composer
- press `Alt+Q` or run `edit-queue` from the command palette to edit, delete, or clear queued follow-ups
