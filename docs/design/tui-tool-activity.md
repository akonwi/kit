# TUI tool activity presentation

Status: Implemented design guidance for native transcript activity.

## Intent

Native tool activity should be compact, chronological, and easy to scan. It
should identify what Kit is doing without turning the transcript into a stack of
cards. Recorded tool output stays behind an explicit per-row disclosure and is
bounded when shown.

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
✕ Search            TUI-WORK in backlog
```

The example communicates hierarchy rather than literal chip styling. The
argument summary uses the standard semantic surface token and compact horizontal
padding. The row itself keeps the transcript background and does not gain a
hover background; only the selected row is filled (see
[Recorded output](#recorded-output)).

Only states that need immediate attention or communicate ongoing work receive a
glyph:

- pending and running calls use Kit's shared animated spinner;
- failed calls use the failure cross and danger treatment;
- aborted and not-run calls use the muted circle-slash glyph; and
- successful calls reserve alignment space but render no status glyph.

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

## Recorded output

Clicking a tool row selects it, filling the row with the pressed surface, and
shows or hides its output in a well directly below it. Rows show no disclosure
arrow. Several wells may be open at once; collapsing or switching the open batch
closes its wells and clears the selection.

The well sits on the surface fill, indented under the row, and is bounded:

- It grows to fit its output up to 14 rows, then scrolls vertically.
- Lines keep their shape and never wrap. Output wider than the well scrolls
  sideways, and a horizontal scrollbar takes a row only while it is needed.
- At the well's top or bottom edge, the wheel scrolls the transcript instead.
- While the call is running, output streams in plain and the well follows its
  end unless the reader has scrolled away from it.

Output is presented per tool by `toolpresentation.ShowOutput`, so the renderer
never interprets raw arguments itself:

| Tool kind | Well content |
| --- | --- |
| `read` | recorded file contents, highlighted in the file's language |
| `write` | written content, highlighted in the target's language |
| `edit` | each edit's removed and added text on the diff fills, with `-`/`+` markers, separated per edit |
| `bash` | the command after a muted `$` prompt, a blank row, then its output; a unified diff renders as a diff, and a command that only prints one file (`cat`, `head`, `tail`, `sed -n`) highlights it in that file's language |
| other tools, or a failed `read`, `write`, or `edit` | the recorded result text as plain code |

Empty output reads `Waiting for output…` while running and `No output` once
finished.

When the server did not keep everything, the well says so explicitly in a muted,
right-aligned footer joined with ` · `:

- `n lines` when the output overflows the well;
- `output truncated` when the recorded result was truncated;
- `details omitted` when the result's details were not kept; and
- `content not available` when a `write` or `edit` call's arguments were
  truncated, in which case the well shows the result text instead of the
  content.

The footer is absent when there is nothing to note.

Live and restored activity render through the same transcript tool-call and
result records, so a reconnect or reopened session shows the same rows, wells,
and footers as the live run. Subagent conversations use the same rows and
wells.

## Responsive behavior

At narrow widths, the argument chip moves to a second indented line and
truncates rather than breaking horizontal layout. Path summaries preserve their
informative tail:

```text
  Write 132 lines
    …/0013-native-macos-client.md
```

## Interaction and acceptance notes

- Batch disclosure is available by mouse without adding a keyboard focus marker.
- A primary click on a tool row selects it and toggles its output well; output
  disclosure adds no keyboard shortcut, palette action, or Tab stop.
- File-path chips on `read`, `write`, and `edit` calls open or select the retained
  File tab on a primary click instead of toggling the row's output. Only the hovered chip changes background; the
  action title and surrounding row do not gain hover styling. Chips add no
  keyboard shortcut, palette action, picker, or Tab stop.
- Navigation uses the complete path, not its truncated label. Recorded resolved
  paths take precedence over relative arguments when available. Files open in
  the current associated workspace, not as historical snapshots; unavailable
  or stale targets produce a warning rather than an unrelated file open. Reads
  reveal their known line/range; edits and writes do not invent line anchors.
- Rendering or completing a tool call never opens a tab automatically. Repeated
  activation reuses the file's workspace/path identity without reordering tabs.
- A subagent call's chip may open its retained conversation when that
  conversation exists; this is navigation, separate from the row's output well.
- Batch expansion survives ordinary repaint and responsive narrow/wide changes.
- Unknown tools, malformed arguments, failures, cancellation, and reconnect
  snapshots retain a readable summary fallback.
- Presentation tests cover collapsed and expanded batches, argument chips,
  successful glyph omission, narrow path truncation, Markdown thinking style,
  per-tool well content, bounded scrolling, and footer notes.
