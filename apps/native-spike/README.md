# Native SwiftUI/AppKit workspace spike

A throwaway prototype for [ADR 0044](../../docs/adrs/0044-tile-the-macos-session-workspace.md).
It checks that SwiftUI and AppKit can realize the Paper "Kit App" design, with
libghostty terminals and WKWebView browsers living inside native tiles. It uses
fixture data only and never connects to a Kit server. It is a reference for the
`apps/macos` port and is not shipped or maintained.

    script/build_and_run.sh          # builds dist/KitNativeSpike.app and opens it

The first build runs `script/build_ghosttykit.sh`, which builds
`Vendor/GhosttyKit.xcframework` at a pinned Ghostty commit. It needs the Zig
version named by Ghostty's `build.zig.zon` and Xcode's Metal Toolchain
(`xcodebuild -downloadComponent MetalToolchain`). The framework is not
committed. Fonts are bundled from `Resources/Fonts`: JetBrains Mono under the
SIL Open Font License and Phosphor under the MIT License, with their license
texts alongside.

Shortcuts: ⌘K/⌘P palette · ⌘T terminal tab · ⇧⌘B browser tab · ⌘D / ⇧⌘D split
· ⇧⌘↩ zoom · ⌃⌘= equalize · ⌘W close tab · ⌘1–9 sessions · ⌘\ side/top tabs ·
⇧⌘I guided question · ⇧⌘T toast. Themes are in the View menu and palette.

Interactive events are appended to `/tmp/nativespike/events.log`.
`SPIKE_THEME`, `SPIKE_TABS=top` and `SPIKE_DEMO=terminal,browser,zoom,move,
switch,question,toasts,fold,palette,drag` preset state for screenshots.
`SPIKE_BACKGROUND=1` launches without taking focus.

## Findings

- Stock SwiftUI matches the Paper design closely: surfaces, themes, tab strips,
  dividers, overlays, toasts, and the interaction dock.
- libghostty and WKWebView are ordinary AppKit views inside tiles. A view
  registry keeps each one alive while tabs switch, move between tiles, or zoom,
  which re-parents the view between SwiftUI hosts. Without the registry these
  moves blanked the terminal.
- The palette and other SwiftUI overlays draw above native views.
- A focus bridge moves AppKit's first responder to the focused native tile.
  SwiftUI `FocusState` covers text inputs.
- Traffic lights are placed in the 44 pt top row by moving AppKit's title bar
  views. Observers place them again after resizes and full-screen transitions.
- Divider dragging works. Dropping tabs works, but targeting is finicky. Tile
  frames must come only from the current session's visible tiles.
- Not built: first-run, empty-session and new-session screens, palette argument
  pages, top-tab overflow, reordering within a tab strip, cross-paragraph
  transcript selection (needs the AppKit transcript table `apps/macos` already
  uses), and the stacked edges of folded toasts.
