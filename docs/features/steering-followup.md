# Steering & Follow-up Messages

## Overview

pi-kit supports queueing messages for the agent while it's actively processing a turn. This allows you to steer the agent's behavior mid-run or queue follow-up work.

## Concepts

### Steering
A steering message is delivered **immediately after the current tool-call batch finishes**, before the next LLM call. Use it to:
- Correct the agent's direction
- Ask it to pause or wait
- Provide a redirect before it continues

### Follow-up
A follow-up message is delivered **only when the agent is completely idle** (no pending tool calls, no steering messages). Use it to:
- Queue up additional work after the current task finishes
- Ask follow-up questions once the agent stops

## Usage

### Keybindings

| Key | Action |
|-----|--------|
| **Enter** (while streaming) | Queue as steering message |
| **Alt+Enter** | Queue as follow-up message |
| **Alt+Up** | Restore all pending (steering + follow-up) messages back to the composer |

### Slash Commands

| Command | Description |
|---------|-------------|
| `/steer <msg>` | Opens an input prompt; queues the text as a steering message |
| `/followup <msg>` | Opens an input prompt; queues the text as a follow-up message |

### Footer Indicator

When pending messages are queued, the status bar shows `📬N` (e.g. `📬2`) next to the model info. The count drops to zero once messages are delivered.

## API

The runtime exposes these methods:

```ts
runtime.sendFollowUp(text: string): Promise<void>
runtime.sendSteer(text: string): Promise<void>
runtime.clearPendingMessages(): { steering: string[]; followUp: string[] }
runtime.getPendingMessages(): { steering: string[]; followUp: string[] }
runtime.getPendingMessageCount(): number
```

## Session Persistence

Queued steering and follow-up messages are held in memory only. If you want to preserve them across sessions, restore them to the composer with `Alt+Up` before ending the session.

## Design Decision

Enter steers by default, not follow-up. Steering is the more common use case while the agent is mid-turn (corrections, redirects). Follow-up is rarer and more deliberate, hence the modifier key. See [docs/decisions/steering-followup.md](../decisions/steering-followup.md).
