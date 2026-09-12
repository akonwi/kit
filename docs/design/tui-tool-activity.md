# TUI tool activity presentation

Status: Implemented design guidance for native transcript activity.

## Intent

Native tool activity should be compact, chronological, and easy to scan. It
should identify what Kit is doing without turning the transcript into a stack of
cards or exposing verbose tool output inline.

The macOS specimen remains a reference for grouping and information hierarchy,
not for desktop card, pill, corner-radius, or icon styling.

## Transcript structure

Tool calls are grouped only when they are contiguous in the recorded transcript.
Assistant prose between two groups keeps them as separate batches, even when all
entries belong to one turn. Calls within a batch remain in recorded source order;
concurrent start or finish order does not reorder them.

A collapsed batch shows its call count and failure count when applicable:

```text
▸ 5 tool calls · 1 failed
```

Expanding a batch inserts its content directly into the transcript's natural
flow. It does not create an independently scrolling nested viewport. The batch
row is mouse-activated and has no keyboard focus target, row-level hover fill,
or raised chip background.

## Expanded activity flow

Expanded content has no artificial blank rows between thinking, assistant prose,
activity sections, or tool calls. Semantic Markdown may still create structure
where the recorded content requests it.

Full thinking is rendered as muted, italic Markdown. Do not add a `Thinking`
label. Markdown semantics remain active, so headings and explicit emphasis can
add their own weight.

Assistant prose remains normal transcript Markdown in chronological position.

## Tool rows

At normal widths, each tool call uses one compact row containing a status
column, human-readable action title, and surfaced argument-summary chip:

```text
  Run command       cat · echo · grep → head
✕ Search            TUI-WORK in docs/roadmap
```

The example communicates hierarchy rather than literal chip styling. The
argument summary uses the standard semantic surface token and compact horizontal
padding. The row itself keeps the transcript background and does not gain a
hover background.

Only states that need immediate attention or communicate ongoing work receive a
glyph:

- pending and running calls use Kit's shared animated spinner;
- failed calls use the failure cross and danger treatment;
- aborted and not-run calls use the muted circle-slash glyph; and
- successful calls reserve alignment space but render no status glyph.

Tool rows do not show disclosure arrows and do not expand to output details.
Recorded result output, commands, diffs, file contents, and generic detail wells
are intentionally omitted from this presentation. Keep the underlying typed
presentation and enrichment path available for future tool-specific work, but do
not expose it until an individual tool design is intentionally added.

A tool row contains:

1. pending, running, succeeded, failed, or aborted state;
2. a human-readable action title; and
3. a concise typed argument summary.

Raw tool names and serialized arguments are fallback evidence, not the primary
label. Malformed or unfamiliar calls use a normalized tool name and bounded
argument summary.

## Presentation vocabulary

Current action and summary rules:

| Tool kind | Action title | Argument summary |
| --- | --- | --- |
| `bash` | Run command | concise command or pipeline |
| `change_cwd` | Change directory | target path |
| `read` | Read file | path and requested range |
| `write` | Write _n_ lines | target path |
| `edit` | Edit _n_ sections | target path |
| `grep` | Search | pattern and scope |
| `find` / `glob` | Find files | pattern and scope |
| `ls` | List directory | target path |
| `activate_skill` | Load skill | skill name |
| `subagent` | Start, message, wait for, cancel, or inspect agent | agent name |

Pending or running batches retain live status without causing completed rows to
reorder. The collapsed batch keeps its failure count visible.

## Responsive behavior

At narrow widths, the argument chip moves to a second indented line and
truncates rather than breaking horizontal layout. Path summaries preserve their
informative tail:

```text
  Write 132 lines
    …/0012-native-macos-client.md
```

## Interaction and acceptance notes

- Batch disclosure is available by mouse without adding a keyboard focus marker.
- Tool rows have no output disclosure interaction.
- A subagent call may still open its retained conversation when that conversation
  exists; this is navigation, not inline output expansion.
- Batch expansion survives ordinary repaint and responsive narrow/wide changes.
- Unknown tools, malformed arguments, failures, cancellation, and reconnect
  snapshots retain a readable summary fallback.
- Presentation tests cover collapsed and expanded batches, argument chips,
  successful glyph omission, narrow path truncation, Markdown thinking style,
  and absence of inline output.
