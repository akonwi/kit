# Feature Documentation

This directory describes user-facing features in `kit`.

Because the app is still being rebuilt on the new standalone runtime, these docs
should still be read with an explicit distinction between:

- **available now** — wired into the current runtime and shell
- **partially implemented** — foundational code exists, but UX wiring is not yet complete
- **planned** — intended feature direction, not currently active

## Current feature docs

- [Context Files](context-files.md) — AGENTS/CLAUDE guidance discovery and system-prompt attachment
- [Slash Commands](slash-commands.md) — current command surface and how commands are modeled
- [File References](file-references.md) — current `@` picker behavior and file reference insertion
- [Thread References](thread-references.md) — current `#` picker behavior and submit-time thread expansion
- [Bash Execution](bash-execution.md) — historical/planned shell-command UX, not currently wired
- [Pager](pager.md) — pager direction and current rebuild status
- [Guided Questions](guided-questions.md) — structured clarification questionnaires in a modal
- [Handoff](handoff.md) — current fork-like linked-session handoff behavior
- [Steering & Follow-up](steering-followup.md) — current queued-message behavior while streaming

## Related

- [Architecture](../architecture/custom-shell.md)
- [Decisions](../decisions/)
