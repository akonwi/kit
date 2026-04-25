# Steering & Follow-up Messages

Kit can queue messages for the agent while it is still working on the current turn.

There are two relevant behaviors in the current composer flow:

- **follow-up** — queue a message so it runs after the agent becomes idle
- **steering promotion** — promote already queued follow-ups so they are delivered before the next model call

Current behavior:

- when the agent is streaming and the composer has text, pressing `Enter` queues that text as a follow-up
- queued follow-ups are shown above the composer while they are pending
- when the composer is empty, the agent is streaming, and queued follow-ups exist, pressing `Enter` promotes those queued follow-ups to steering
- pressing `Up` in an empty composer restores queued follow-ups first; if none exist, it recalls the last user message
- queued follow-ups clear from the visible stack when the next turn begins consuming them

This behavior is currently exposed through the composer flow rather than through dedicated slash commands.

## How to access it

Use the normal composer while the agent is streaming:

- type a message and press `Enter` to queue it as a follow-up
- press `Enter` again in an empty composer to promote queued follow-ups to steering
- press `Up` in an empty composer to restore queued follow-ups back into the composer
