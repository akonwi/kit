# Native macOS design direction

This is the agreed presentation direction for Kit's macOS client, established
for the [native client](../../apps/macos/README.md). It extends
Kit's [design language](../../.agents/skills/design/SKILL.md) for desktop use.
The client architecture remains defined by [ADR 0013](../adrs/0013-native-macos-client.md).

## Mica identity with macOS behavior

Build the interface with SwiftUI and narrow AppKit bridges. Mica provides the
surface hierarchy, semantic colors, restrained emphasis, and spacing vocabulary;
it does not require embedding `mica.css`, custom web elements, or a web view.

Use native window chrome, menus, text editing, accessibility, and window tabs.
Let the application retain its own visual identity inside those conventions.
Neither a pixel-for-pixel web translation nor an entirely system-styled content
area is the goal.

The visual language is quiet, functional, and themeable:

- Use fine dividers, clear typography, and surface contrast to establish hierarchy.
- Reserve accent color for meaningful emphasis and selection. Keep focus-border
  and accent roles distinct: a theme may deliberately give them different colors.
- Use system typography; reserve monospaced text for code and useful technical
  metadata. Avoid uppercase labels when ordinary text is clearer.
- Soften interactive surface boundaries without making every region a floating
  card. Workspace boundaries and split dividers remain structural.
- Keep transcript and composer aligned to one readable content column, including
  when a large window offers much more width than the text needs.

The native client uses these baseline dimensions:

| Surface | Corner radius |
| --- | --- |
| Compact controls | 6 pt |
| Messages and code blocks | 8 pt |
| Composer | 10 pt |
| Command palette and file picker | 12 pt |

The base spacing vocabulary is 4/8/12/16/24/40 pt, with 14 pt transcript text.
These are implementation starting points, not requirements to override native
window-control geometry.

## Shell and session navigation

The unified native toolbar owns session identity and workspace actions. Do not
add a second application header for development controls or repeat the session
title in stacked headers.

Sessions use native macOS window tabs, with native closing, reordering, and
detaching into a separate window. Cmd+T opens the launcher in a new tab.
Commands remain available through the command palette and keyboard shortcuts.

Session tabs and workspace tabs are different scopes. Code review, scratchpad,
files, and subagent inspection belong to a session's workspace; they do not
become global session tabs. Switching native tabs retains each session view's
local draft and workspace state.

When there is no saved layout, show a session launcher instead of opening the
latest session. Connect in the background and present recent sessions with their
working directories and update times. Search, arrow keys, and Return support
opening a session without a pointer. The launcher does not show the transcript,
composer, or workspace panes until a session is explicitly chosen.

Relaunch restores only intentionally opened sessions, preserving tab groups,
tab order, the selected tab within each group, and detached windows.
Closing a session removes it from restoration intent. Explicitly reopening it joins
the source window's session group; it does not resurrect a closed group. Quitting
keeps the open layout for the next launch. Draft persistence across close/relaunch is
tracked separately in the [backlog](../../backlog/macos.md).

## Composer

The composer starts with one text line and grows with content. It has no internal
scroll container or arbitrary height cap. The send/stop control stays a fixed
size at the bottom right. Do not display a redundant Cmd+Return label next to it;
the shortcut remains supported.

The placeholder and typed text must use the same font and text origin. Preserve
native selection, insertion, multiline editing, and keyboard behavior. Measure
using the editor's actual text layout instead of approximating its height with a
separate SwiftUI text view.

Very long drafts still need a production layout policy that preserves access to
all content and actions without reintroducing an internal composer scrollbar.

## Workspaces and transient interfaces

Agent, files, subagent conversations, and other opened surfaces are peer workspace
tabs within each native session tab. Agent is first and cannot be closed. The
workspace strip is omitted when Agent is the only surface. Opened panes retain
identity and local state across selection and movement.

The default is one full-width tab group. Tab context menus and the group menu
provide explicit split-right, move-to-other-group, join, and close actions. At
most two groups are shown, with a native draggable divider; narrowing a window
does not automatically rearrange the layout. Closing the last tab in a group
removes that group. The focused group's selected tab uses the accent underline;
the other group's selected tab uses a neutral underline. File tabs use a document
icon and subagent tabs use Phosphor's Robot icon. Close buttons appear inside the
tab on hover or keyboard focus without reserving a gap between tabs.

One composer and interaction dock span beneath the workspace, always addressing
the main Kit session. No recipient label or bottom workspace divider is needed.
File paths occupy a compact header directly below the tab strip, with a bottom
separator above the editor. Prose and labels use proportional text; code, paths,
commands, and technical output use the configured monospace font.

Tool summaries and output use flat transcript surfaces, indentation, and subtle
rules rather than nested filled cards. Preserve the rounded composer, tinted user
messages, and comfortable message spacing. Pickers use rounded outer surfaces,
flat rows, proportional titles, and monospace paths.

Review notes attach to the composer. Scratchpad drafts survive workspace tab
changes. Reopening an individual file or subagent focuses its existing pane.

Command and file pickers should take keyboard focus when opened, support
navigation and dismissal, and keep background controls from accepting input.
They share the softened surface treatment rather than inheriting unrelated
sheet styling. Closing a picker should return focus to the appropriate editor.

## Appearance and themes

Appearance is a choice of **System**, **Light**, or **Dark**. Independently select
a theme for Light and another for Dark. System follows macOS and activates the
corresponding assignment; forced modes use that assignment consistently.
Settings shows both previews so the pair can be assessed without repeatedly
changing the whole application's appearance.

Support Kit's existing `tokens` and `syntaxPalette` JSON structure. Import copies
theme data into application preferences, validates the entire batch before
changing preferences, and leaves source files untouched. Do not scan legacy
Kit state implicitly.

Infer an imported theme's initial assignment from its opaque `tokens.bg` color,
not its filename. Themes without a usable opaque background use the currently
active mode as their initial assignment. Preserve the imported definition even
when some roles do not yet have native consumers.

Apply the active appearance to native window chrome as well as content. Toolbar
backgrounds use the theme's surface color; native controls retain system drawing
and appropriate light/dark contrast. Themes must not leave dark content beneath
light controls with unreadable icons.

## File annotations

Hover a source line to reveal a gutter + control. Click it to annotate that line,
or drag up or down across the gutter to select up to 200 lines, with a live range
highlight; releasing opens the input. Source-text selection and Annotate selection
remain available as a keyboard-accessible alternative.
The focused input belongs beneath the selected range with composer styling.
Highlight the anchored source range in the gutter; omit line labels inside the
comment itself. Enter saves, Cmd+Enter inserts a newline, and Escape
cancels. Saved comments use proportional text with Edit/Delete on hover; source
and its unchanged line numbers remain monospace. Do not repeat filenames inline.

A shared strip above the parent-session composer shows filename:range and a short
comment preview on every workspace tab. Activating a chip reveals the anchored
note in its retained file tab and briefly highlights it. Closing a tab leaves
its server-owned notes intact. A bounded overflow list handles many notes.
Annotations are composition inputs, including when the text draft is empty.
Accepted messages show expandable immutable annotation groups containing the
filename/range, comment, and captured source.

Stale notes remain visible with File changed and cannot be sent. Activation shows
the frozen evidence and Select new range/Delete actions. Replacing an anchor
preserves the comment and creates a new server ID before deleting the old note.
Never silently relocate a note to newer code or add a permanent annotation pane.
