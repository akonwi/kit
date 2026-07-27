# Steering & Follow-up Messages

Kit can queue messages for the agent while it is still working on the current turn.

There are two relevant composer behaviors:

- **follow-up** — queue a message so it runs after the agent becomes idle
- **steering promotion** — promote already queued follow-ups so they are delivered before the next model call

## Current behavior

While the agent is streaming:

- typing a message and pressing `Enter` queues it as a follow-up
- queued follow-ups are shown below the agent status line and directly above the composer while they are pending
- the status bar shows `queued messages: N · ↑ restore` when follow-ups are queued
- when the composer is empty and queued follow-ups exist, pressing `Enter` promotes those queued follow-ups to steering
- pressing `Up` while follow-ups are queued restores them into the composer; if none are queued and the composer is empty, it opens user message history for recall
- queued follow-ups clear from the visible stack when the next turn begins consuming them

## Restoring queued messages

Restoring drains every pending follow-up back into the composer, separated by blank lines and in queue order. If the composer already contains a draft, that draft is preserved after the restored messages. Queued image and code-review attachments are restored too. The combined draft can then be edited or cleared using the normal composer controls.

Press `Up` while follow-ups are queued. If the composer already contains a draft, it is preserved after the restored messages. When there are no queued follow-ups, `Up` keeps its normal composer behavior and opens user message history from an empty composer.

## How to use it

- type a message and press `Enter` while streaming to queue a follow-up
- press `Enter` again in an empty composer to promote queued follow-ups to steering
- press `Up` to restore queued follow-ups back into the composer
