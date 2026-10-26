# Markdown files

A Markdown file opens rendered in its file tab, in the terminal client and the
macOS app, as Markdown in the transcript is rendered, with the same headings,
lists, tables, code blocks, and [Mermaid diagrams](mermaid.md). Files named
`.md`, `.markdown`, `.mdown`, `.mdwn`, `.mkd`, `.mkdn`, or `.mkdown` are
Markdown. This holds for workspace files and for files outside the working
directory: files on the session's server, and in the macOS app, files on the
Mac ([ADR 0050](../adrs/0050-view-files-outside-the-working-directory.md)).

## Preview or source

Each Markdown tab has a toggle in its header between the rendered preview and
its source.

- **Terminal:** the toggle names the current view, `Preview` or `Source`;
  click it to switch. A tab keeps its view while it is open; tabs are not kept
  between attachments. A file opened at a line, such as from a tool row, shows
  its source at that line.
- **macOS:** the toggle is a pair, **Preview** and **Source**. **Tile › Show
  Markdown Source** (⇧⌘M) and the tab's context menu switch the selected tab.
  A tab keeps its view, including when windows are restored at launch.

Rendered text is selectable. The rendered view builds only what is on screen,
so long files scroll smoothly. It shows what the file preview loaded, within
the preview's bounds of 1,000,000 bytes and 5,000 lines; a truncated preview
says so in the footer. In the terminal client, a file too complex to parse
within the parser's budget for files shows its source, with
`Too large to render; showing source` in the footer. In the macOS app, a file
over 1 MiB, or with quotes and lists nested more than 64 deep, says it is too
large or too deeply nested to render and offers **Show Source**.

A file that changes on disk shows its changes as its source does: a workspace
file reloads when its tab is shown after the session has worked, and every
file reloads on **Refresh** (macOS) or each time its tab is shown (terminal).

## Annotations

Annotations are anchored to source lines. The preview works on whole blocks:
a heading, paragraph, code block, table, quote, rule, or list item covers the
source lines it came from, including a code block's fences and a setext
heading's underline. Workspace files take
annotations in both views; host files take none.

- Hovering a block shows a **+** in its margin that annotates the block's
  source lines. In the terminal client, dragging down the margin takes in the
  blocks the drag crosses. An annotation covers at most 200 lines, so a longer
  block offers no **+**; annotate it from the source.
- A saved annotation shows under the block holding its last line, and the
  margin beside annotated blocks is marked. A new annotation is written under
  its block.
- Revealing an annotation, such as from a composer chip, scrolls the preview
  to it; in a file too large to render, the source shows it. In the terminal
  client, a stale annotation opens the source at its line, and the toggle is
  hidden while an annotation is being written.
- In the macOS app, choosing a new range for an annotation whose file changed
  works in either view: **+** on a block, or a selection in the source.

The source keeps its line gutter and, in the macOS app, **Annotate
selection** for any range of lines.

## Links and images

A link names a file on the same machine as the Markdown file it is in.

- In the macOS app, a relative link opens the file it names, resolved against
  the Markdown file’s directory, as a tab in its tile. On the server, a path in
  the working directory opens as its workspace file and any other as a server
  file; in a file on this Mac, links open files on this Mac. A `#fragment` is
  ignored. Web links open in a browser tab, as transcript links do, and
  ⌘-click opens them in the default browser.
- In the terminal client, links show their labels, and web links open as
  transcript links do. File links are not yet followed.

Images show as their alt text, as in the transcript, in both clients; they are
not loaded.
