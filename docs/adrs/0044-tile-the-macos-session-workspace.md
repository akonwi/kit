# 0044: Tile the macOS session workspace

## Status

Accepted

## Context

[ADR 0013](0013-native-macos-client.md) defines the macOS application as a
native Swift protocol client. Its workspace needs to show several sessions,
conversations, review surfaces, terminals, and web pages side by side.
Terminals and web pages are not Kit UI. They are platform views, and the
workspace has to host them next to Kit's own surfaces.

Three approaches were prototyped against the same Paper design ("Kit App"):

- **Host the Kit TUI in a native shell.** Each window embeds libghostty running
  the unmodified TUI. This reuses the TUI, but every surface inherits
  terminal-cell rendering and TUI layout. Native controls, text selection,
  accessibility, and drag and drop stop at the terminal boundary.
- **Build the client with gpui.** gpui has no stable release. Embedding
  terminal and web views depends on unmerged upstream work. Controls would be
  drawn by gpui rather than AppKit, and Kit has no Rust session client.
- **Build it with SwiftUI and AppKit.** Stock SwiftUI reproduced the design
  closely. libghostty and `WKWebView` work as ordinary AppKit views inside
  tiles, and SwiftUI overlays draw above them. The prototype is kept as a
  reference in [`apps/native-spike`](../../apps/native-spike/README.md).

## Decision

The macOS workspace is built with SwiftUI and narrow AppKit bridges in
`apps/macos`, under ADR 0013's protocol-client boundary. Kit's own surfaces are
drawn natively; the application does not host the TUI.

### Session navigation

A window lists its open sessions as session tabs, either in a sidebar or in a
strip at the top. A toggle switches between the two. Selecting a session tab
replaces the workspace below it with that session's workspace. Top tabs scroll
when they overflow, with an edge fade and a count of hidden sessions. A session
tab shows a spinner while the session runs, a warning marker while it waits for
the user, and a check when a turn finished while the user was elsewhere. A
waiting or finished tab keeps a full-strength title until the user opens it.

Native macOS window tabs are not used for session navigation.

### Tiled workspace

Each session's workspace is a split tree. A leaf is a **tile** that holds a tab
group of panes. Splits are horizontal or vertical, and each has a ratio that
the user adjusts by dragging the gap between tiles. There is no limit on the
number of tiles beyond what the window can usefully show.

- New panes open as a tab in the focused tile. Opening a pane whose identity is
  already open focuses that pane instead of opening another.
- Dragging a tab onto another tile's center or tab strip moves it into that
  tile. Dropping it near an edge splits the target tile on that side and moves
  the tab into the new tile. Closing the last tab of a tile removes the tile,
  and its sibling takes its place.
- The focused tile can be zoomed to fill the workspace, and split ratios can be
  equalized. Opening, splitting, or moving panes ends the zoom.
- Only the focused tile's selected tab has an accent underline. Other tiles
  keep a neutral underline on their selected tab.

Each session keeps its own arrangement while the application runs. There are no
saved or named layouts.

### Panes

Pane kinds are an exhaustive, renderer-neutral set of descriptors:

| Kind | Identity | Notes |
| --- | --- | --- |
| Agent | the session | The main conversation. One per session; not closable. |
| Subagent conversation | durable conversation identity | A conversation with its own composer. |
| Diff | the session | Review surface; comments are added to the Agent composer. |
| File | path | Read-only file view. |
| Scratchpad | the session | The session scratchpad. |
| Terminal | pane instance | A libghostty terminal. Only offered with a local daemon. |
| Browser | pane instance | A web browser behind the browser engine interface. |

A conversation pane owns its composer, activity card, queued-message tray, and
interaction dock. The composer addresses that pane's conversation. The model is
shown only in the composer's model picker.

### Native views

Terminal and browser panes are AppKit views that SwiftUI hosts. A view registry
owns each view for the lifetime of its pane. When a tab switches, moves
between tiles, or a tile zooms, the view moves to the newest SwiftUI host.
If its current host goes away while the pane lives, it returns to a surviving
host. Hosted views are never recreated for those changes.

A focus bridge moves AppKit's first responder to the native view of the focused
tile. SwiftUI focus state covers Kit's own text inputs. Overlays such as the
command palette take focus above native views and return it when dismissed.

### Terminals

Terminal panes embed libghostty, built at a pinned Ghostty commit and bundled
with the application. A terminal runs the user's login shell in the session's
working directory on the client machine. Since workspace operations belong to
the server host, terminal panes are offered only when the server is the local
daemon. The application owns the terminal process. The process ends when its
pane closes or the application quits, and the terminal is not visible to the
agent. Terminal colors and fonts come from the active Kit theme; the user's
Ghostty configuration is not read.

### Browsers

Browser panes host a web engine behind an application-owned browser engine
interface. The pane, its address field and navigation controls, focus
bridging, and view retention depend only on that interface: a view to host
plus navigation, history, title, URL, loading-state, and lifecycle operations
and events. Engine types do not leak past it, so another engine, such as
Chromium through CEF, can be added without changing the workspace.

The initial engine is `WKWebView` with the default persistent website data
store. Cookies and site data last across launches and are separate from
Safari. The browser is for the user; the agent does not drive it. Kit's own
interface never uses web content.

### Ownership

Session tabs, tile trees, split ratios, focus, zoom, selected tabs, and native
views are client-local presentation state. They are not written to session
records or shared settings. The server stays authoritative for sessions,
conversations, subagents, and interactions.

## Consequences

### Positive

- Kit's own surfaces use native controls, text selection, accessibility, menus,
  and drag and drop, with no terminal or renderer boundary inside the workspace.
- Terminals and browsers sit beside Kit surfaces without a second UI toolkit or
  runtime.
- The workspace builds on the existing Swift session client and generated
  protocol types.

### Negative

- Kit's transcript, composer, and workspace exist natively in both the TUI and
  the macOS application, and their parity must be tracked.
- Bundling libghostty adds a Zig build, a pinned upstream dependency, and its
  binary size to the macOS release pipeline. The Go build is unaffected and
  stays CGO-free.
- Moving AppKit views between SwiftUI hosts and bridging focus are
  application-owned code. SwiftUI changes can break them.

## Related

- [ADR 0013: Native macOS client](0013-native-macos-client.md)
- [ADR 0018: Retain native workspace panes in full-width tabs](0018-retained-native-workspace-shell.md)
- [Paper "Kit App" design](https://app.paper.design/file/01M4MCMMAEY5AWN7E5J0PK6X1K)
- [Outstanding work](https://github.com/akonwi/kit/issues?q=is%3Aopen+label%3Amacos)
