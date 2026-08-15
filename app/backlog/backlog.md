# Backlog

This file is the source index for the backlog.

Keep this list short and current. If an item needs more detail, link to a dedicated file in this directory.

## Conventions

- `[ ]` not started
- `[x]` done

Delete done items.

## Active items
- [ ] web: implement runtime workspace tabs ([design](./workspace-tabs.md))
- [ ] web: add a session explorer like the TUI
- [ ] cli: harden transport protocols by unifying transport and shared logic across modes
- [ ] cli: support `kit attach` for connecting to remotely served sessions
- [ ] idea: explore whether diff/review tools could be enhanced with Ataraxy libs
  - https://github.com/Ataraxy-Labs/sem
  - https://github.com/Ataraxy-Labs/inspect
- [ ] idea: agent-authored diff annotations in review mode — an `annotate` tool
  (path + side + line range + note, validated against current hunks) feeding a
  session-persisted store, rendered through the existing
  `DiffLineAnnotation`/`ReviewDiffAnnotationMetadata` pipeline with an
  `author: "agent"` style; lazy staleness (drop ranges no longer in the diff),
  dismiss key, hide-all toggle
- [ ] tui: upstream an OpenTUI decoded pre-hit-test mouse-input hook, then remove
  Kit's parallel `StdinParser` workspace fallback
