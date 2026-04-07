# Docs

This directory records architectural decisions and feature design choices for `kit`.

## Structure

- `architecture/` — architecture decisions, compatibility contracts, shell model, and system-level design notes
- `features/` — feature-specific design notes as we define new shell behavior and port or redesign functionality
- `decisions/` — smaller focused decisions that do not need a full architecture document

## Current key docs

- `architecture/custom-shell.md` — primary architecture document for the current standalone app
- `decisions/decouple-from-pi.md` — the app is standalone and no longer targets Pi compatibility
- `decisions/storage-paths.md` — `~/.kit` is the single storage root
- `decisions/turn-based-session-model.md` — sessions persist explicit turns with `turnId`-tagged messages
- `decisions/assistant-message-streaming.md` — assistant text is not streamed; only runtime activity may appear live

As we make decisions in chat, capture them here so the design does not live only in conversation history.
