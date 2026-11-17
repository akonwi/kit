# 0070: Comment on selected text

## Status

Accepted

## Context

A person reading a reply, a tool's output, a file, a diff or the scratchpad
often wants to answer one part of it: "this timeout is the same bug", "use the
shared helper here". Doing that by copying the words into the composer and
formatting a quote around them makes the person compose a message about the
words instead of commenting on them, and the model can't tell quoted words
from the person's own.

Kit already has two kinds of comment that point at something:

- Line annotations on a file or a working-tree diff are server records
  pinned to a revision and a range
  ([ADR 0022](./0022-model-draft-annotations-as-session-inputs.md)). They go
  stale when the source changes, and every attached client sees them.
- Comments on a browser element or a drawn diagram are drafts in one
  client's composer, sent as blocks appended to the message
  ([ADR 0046](./0046-draw-mermaid-diagrams-with-kits-own-renderer.md#annotating-a-diagram)).
  They need no session protocol, because what they point at can't change.

Selected text falls into both. Lines of a file or of a diff are
revision-pinned sources that already have server annotations. A reply, a
tool's output and the scratchpad are text that a person reads once and
answers in their next message.

## Decision

A person who selects text in a client can comment on it in place. The client
offers **Comment** next to the selection and in its context menu; it opens a
comment box at the selection, like a diagram's. Terminals don't offer it: a
terminal's selection belongs to the program running in it.

### Where a comment goes

- **Lines of a source with server annotations.** A selection in a file tab,
  in any diff, or in a file's Markdown preview where it maps to source
  lines becomes a line annotation over the lines it touches
  (ADR 0022). It is stored, shown and sent like one made from the gutter.
- **Everything else is a text comment.** A selection in a reply, a person's
  own message, a tool's output or the scratchpad becomes a text comment: a
  draft in the client's composer that holds the comment, the selected words,
  and where they came from.

Text comments are client drafts, like browser and diagram annotations. What
they point at is either immutable (a reply, a tool's output) or is read once
and answered (the scratchpad), so it can't go stale before the message is
sent, and keeping them in the client needs no change to the session
protocol. A comment that should follow its source as it changes is a line
annotation.

### What the model gets

Each text comment is a block appended to the message after its browser and
diagram blocks, numbered from 1:

```text
<text-annotation n="1">
Comment: This timeout is the same bug.
  It only shows on CI.
[UNTRUSTED_SELECTED_TEXT nonce=…]
From: Tool output · go test ./internal/pair
Text:
--- FAIL: TestPairRetries (30.02s)
[/UNTRUSTED_SELECTED_TEXT nonce=…]
</text-annotation>
```

- The comment's later lines are indented two spaces, so none can pass for a
  line of the block.
- The selected words may come from a reply or a tool, so they and their
  source are fenced as untrusted with a per-block nonce. Only the fence with
  that nonce closes the block, so words that imitate the fence don't end it.
- `From:` names the source as a person reads it: "Assistant reply",
  "Your message", "Tool output · …", "Scratchpad".
- The words are kept to 4 KiB, cut at a character boundary, and say so
  (`Text, cut to 4 KiB:`). A message holds up to 16 text comments.

The terminal, browser and Apple clients read the same blocks, held to the
fixtures in `testdata/text-annotations`. The Apple clients share one reader
and writer in `KitTranscript`. Blocks that are malformed, or are followed by
more text, are left in the message as written.

### How clients show them

- **While writing.** The words a pending text comment points at keep an
  accent wash and underline wherever they are on screen, until the message is
  sent or the comment is removed. Hovering shows the comment; clicking opens
  it to edit or delete.
- **In the composer.** Attachments and every kind of comment (browser,
  diagram, line and text) share one row of chips above the field. A chip
  opens, reveals or removes its comment. Notices about stale or unsynchronized
  line annotations sit under the row.
- **Sent.** A person's message shows its own text, then a closed "N comments"
  disclosure that opens to each comment with the words it answered and their
  source. Recalling the message into the composer brings back only its text.

## Consequences

- Commenting on a reply or a tool's output takes one selection and one
  sentence; the model gets the words, their source and the comment kept apart.
- Text comments live in one client until sent. Another attached client
  doesn't see them, and closing the client loses them, like browser and
  diagram annotations.
- Every client that shows transcripts carries a reader for the block format,
  and a change to it is made in each, held together by the shared fixtures.
- Line annotations made from a selection keep ADR 0022's revision pinning
  and stale handling. A selection in a Markdown preview that doesn't map to
  source lines can't become one and is a text comment instead.
- A comment anchored to a transcript message on the server, visible to every
  client while drafting, would need a session protocol change and is not part
  of this decision.
