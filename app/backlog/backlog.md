# Backlog

This file is the source index for the backlog.

Keep this list short and current. If an item needs more detail, link to a dedicated file in this directory.

## Conventions

- `[ ]` not started
- `[x]` done

Delete done items.

## Active items
- [ ] feat: support remote usage. build a way to use kit sessions from web/mobile 
- [ ] idea: explore whether diff/review tools could be enhanced with Ataraxy libs
  - https://github.com/Ataraxy-Labs/sem
  - https://github.com/Ataraxy-Labs/inspect
- [ ] idea: agent-authored diff annotations in review mode — an `annotate` tool
  (path + side + line range + note, validated against current hunks) feeding a
  session-persisted store, rendered through the existing
  `DiffLineAnnotation`/`ReviewDiffAnnotationMetadata` pipeline with an
  `author: "agent"` style; lazy staleness (drop ranges no longer in the diff),
  dismiss key, hide-all toggle
