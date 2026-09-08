---
name: vaxis-ui
description: Build terminal UIs with the vaxis/ui Go package — a Flutter-inspired widget, layout, and painting framework for terminal apps. Use when building or modifying any TUI component using vaxis, or debugging widget rendering, focus, events, or scrolling issues.
---

# vaxis/ui — Terminal UI Framework

`go.rockorager.dev/vaxis/ui` is a Flutter-inspired widget framework for terminal applications.
Most apps start with `ui.Run(rootWidget)`.

## Core concepts

### Widget types

Every widget is a struct implementing one of:

- **StatelessWidget** — `Build(BuildContext) Widget` — pure function of config
- **StatefulWidget** — `CreateState() State` — creates persistent state
- **RenderObjectWidget** — `CreateRenderObject` / `UpdateRenderObject` — custom layout/paint

Widgets are **immutable descriptions**. State is where mutation lives.

### State lifecycle

```go
type MyWidget struct{ /* config */ }
func (w MyWidget) CreateState() ui.State { return &myState{} }

type myState struct {
    ui.StateBase       // embed this
    count int
}

// Optional lifecycle hooks:
func (s *myState) InitState()              { /* runs once after mount */ }
func (s *myState) Dispose()                { /* cleanup on unmount */ }
func (s *myState) DidUpdateWidget(old Widget) { /* config changed */ }

func (s *myState) Build(ctx ui.BuildContext) ui.Widget {
    // return the widget tree
}
```

- `s.SetState(func() { s.count++ })` — mutate state and trigger rebuild
- `s.Context()` — access BuildContext outside of Build
- `s.Widget()` — current widget config (always read this, never cache the widget)

### Async updates from goroutines

```go
rt := ctx.Runtime() // or s.Context().Runtime()
go func() {
    result := doWork()
    rt.Dispatch(func() {
        s.SetState(func() { s.data = result })
    })
}()
```

`Runtime.Dispatch(fn)` posts `fn` to the UI event loop thread. Always use it
for state updates from goroutines — never call `SetState` directly from a
background goroutine.

### Post-layout callbacks via `TickFrame`

`Dispatch` runs **before** the next frame's layout. If you need code to run
**after** layout (e.g. to read scroll metrics or rendered sizes), implement
the `frameTicker` interface:

```go
// Called after every frame's build+layout cycle
func (s *myState) TickFrame(now time.Time) bool {
    // return true to keep ticking, false to stop
    return false
}
```

Use this for:
- Scrolling to a position that depends on content height
- Reading `ScrollMetrics()` after content changes
- Any operation that needs valid layout measurements

## Layout widgets

| Widget | Purpose |
|---|---|
| `ui.Flex{Axis, Children, MainAxisAlignment, CrossAxisAlignment, MainAxisSize}` | Flexbox layout |
| `ui.Row(children...)` | Horizontal flex shortcut |
| `ui.Column(children...)` | Vertical flex shortcut |
| `ui.Expanded(child)` | Fill remaining flex space (tight, flex=1) |
| `ui.ExpandedWidget{Flex: n, Child: w}` | Fill with custom flex factor |
| `ui.Flexible(child)` | Loose flex factor of 1 |
| `ui.Padding(insets, child)` | Add spacing around child |
| `ui.Center(child)` | Center on both axes |
| `ui.Align{Alignment, Child}` | Position child within parent |
| `ui.SizedBox{Width, Height, Child}` | Force dimensions |
| `ui.ConstrainedBox{Constraints, Child}` | Min/max size bounds |
| `ui.Stack{Alignment, Children}` | Paint children on top of each other |
| `ui.Positioned{Left, Top, Child}` | Offset inside a Stack |

### Flex enums

- **Axis**: `ui.Horizontal`, `ui.Vertical`
- **MainAxisSize**: `ui.MainAxisSizeMax` (default), `ui.MainAxisSizeMin`
- **MainAxisAlignment**: `ui.MainAxisStart`, `ui.MainAxisEnd`, `ui.MainAxisCenter`, `ui.MainAxisSpaceBetween`, `ui.MainAxisSpaceAround`, `ui.MainAxisSpaceEvenly`
- **CrossAxisAlignment**: `ui.CrossAxisCenter` (default), `ui.CrossAxisStart`, `ui.CrossAxisEnd`, `ui.CrossAxisStretch`

### Insets helpers

```go
ui.All(1)              // equal on all sides
ui.Symmetric(h, v)     // horizontal, vertical
ui.Insets{Top: 1, Left: 2, Bottom: 1, Right: 2}
```

## Content widgets

| Widget | Purpose |
|---|---|
| `ui.Text{Value, Style, SoftWrap, MaxLines, Overflow, Align}` | Plain text |
| `ui.RichText{Spans: []TextSpan{...}, SoftWrap}` | Styled text with mixed formatting |
| `ui.Button{Label, OnPressed}` | Clickable button |
| `ui.Divider{Axis, Style}` | Horizontal/vertical line separator |
| `ui.TextField{Value, Placeholder, OnChanged, OnSubmitted}` | Single-line input |
| `ui.TextArea{Value, Placeholder, MinHeight, SoftWrap, OnChanged}` | Multi-line input |
| `ui.Checkbox{Checked, Label, OnChanged}` | Toggle checkbox |
| `ui.Radio[T]{Value, GroupValue, Label, OnChanged}` | Radio button |
| `ui.ProgressBar{Value, GradientStart, GradientEnd}` | 0–1 progress bar |

### TextSpan styling

```go
ui.TextSpan{
    Text:  "bold text",
    Style: ui.Style{
        Foreground: theme.Primary,
        Background: theme.Surface,
        Attribute:  ui.AttrBold | ui.AttrItalic,
    },
}
```

## Decoration and borders

```go
ui.DecoratedBox(
    ui.Decoration{
        Style:  ui.Style{Background: theme.Surface},
        Border: ui.BorderLine(theme.Border),  // all four sides
    },
    child,
)

// Selective borders
ui.Border{
    Style: ui.Style{Foreground: theme.Border},
    Top: true, Bottom: true, Left: false, Right: false,
}
```

**Important**: `DecoratedBox` draws borders **inside** the box bounds.
Content needs padding to avoid overlapping with border characters.
Use `Padding(Symmetric(1, 1), child)` to inset content by 1 cell on all sides.

## Scrolling

### ScrollView — simple single-child scroll

```go
ui.Scrollbar{
    Child: ui.ScrollView{
        Controller: &ui.ScrollController{},
        Child:      content,
    },
}
```

### CustomScrollView — sliver-based, supports follow-output

```go
ui.Scrollbar{
    Child: ui.CustomScrollView{
        Controller:   scrollCtrl,
        FollowOutput: true,  // stay at bottom as content grows
        Slivers:      slivers,
    },
}
```

- `ui.SliverToBox{Child: w}` — adapt a regular widget into a sliver
- `ui.SliverPinnedHeader{Child: w}` — sticky header
- `ui.SliverListBuilder{Count, Builder, ItemExtent, Overscan}` — lazy list
- `ui.SliverFillRemaining{Child: w}` — fill viewport remainder

### ScrollController

```go
ctrl := &ui.ScrollController{}
ctrl.ScrollToEnd()         // scroll to bottom
ctrl.ScrollToOffset(row)   // scroll to specific row
ctrl.ScrollMetrics()       // get current offset, max, viewport size
```

**Timing**: `ScrollToEnd()` requires valid layout metrics. It won't work
during `InitState` or `Build` because layout hasn't run yet. Use `TickFrame`
(the `frameTicker` interface) to scroll after layout:

```go
func (s *myState) TickFrame(_ time.Time) bool {
    if s.needsScroll {
        if s.scroll.ScrollToEnd() {
            s.needsScroll = false
        }
    }
    return s.needsScroll
}
```

## Overlay and modal

```go
ui.Overlay{
    Child:   mainContent,
    Entries: []ui.OverlayEntry{
        {Modal: true, Child: dialogWidget},
    },
}
```

- `Modal: true` — traps focus and blocks interaction with content below
- `ui.Dialog{Title, Child, Width, Actions, OnDismiss}` — built-in modal dialog
- `ui.CommandPalette{Items, OnDismiss}` — fuzzy command picker

## Theme

```go
theme := ui.MustDepend[ui.Theme](ctx) // access in Build

// Key semantic colors:
theme.Foreground      // default text
theme.Background      // app background
theme.MutedForeground // secondary/dimmed text
theme.Border          // border lines (subtle, close to background)
theme.Primary         // accent/brand color
theme.Surface         // panel/control fill
theme.Success         // green
theme.Warning         // yellow/orange
theme.Danger          // red
theme.DangerText      // red for text
theme.AccentText      // accent colored text
```

`ui.Run(root)` with no theme options auto-detects colors from the terminal
via OSC 10/11 queries. Use `ui.WithThemeSet(ui.DefaultThemeSet())` to force
a built-in theme instead.

Override theme for a subtree:
```go
ui.Provider[ui.Theme]{Value: customTheme, Child: subtree}
```

## Events, shortcuts, and actions

### Intent-based key handling

```go
// 1. Define an intent
type myIntent struct{}
func (myIntent) IntentType() ui.IntentType { return "my.action" }

// 2. Bind keys to intents
ui.Shortcuts{
    Bindings: map[string]ui.Intent{"Ctrl+x": myIntent{}},
    Child: ...
}

// 3. Handle intents
ui.Actions{
    Bindings: map[ui.IntentType]ui.ActionFunc{
        "my.action": func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
            // handle it
            return ui.EventHandled
        },
    },
    Child: ...
}
```

### Event dispatch order

Events flow: **capture** (root → target parent) → **target** → **bubble** (target parent → root).
First `EventHandled` stops dispatch.

### Critical: the app root wraps your tree

`ui.Run` wraps your root in app-level `Actions` + `Shortcuts` that handle:
- `Tab` → `"vaxis.next-focus"` (focus next)
- `Shift+Tab` → `"vaxis.previous-focus"` (focus previous)
- `Escape` → `"vaxis.dismiss"`

**These keys cannot be rebound by inner `Shortcuts`** because the outer
`Shortcuts` matches them first during capture phase.

**Fix**: hijack the upstream *intent* in your own `Actions`, not the key:

```go
ui.Actions{
    Bindings: map[ui.IntentType]ui.ActionFunc{
        "vaxis.next-focus": func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
            // your Tab behavior
            return ui.EventHandled
        },
    },
    Child: ...
}
```

### Pitfall: Actions returning EventIgnored still blocks outer handlers

An inner `Actions` handler that returns `EventIgnored` is still considered
"found" — `ctx.Invoke` stops walking and won't try outer `Actions`. If you
need conditional handling, **register the binding conditionally** instead of
returning `EventIgnored`:

```go
bindings := map[ui.IntentType]ui.ActionFunc{...}
if shouldHandle {
    bindings["vaxis.dismiss"] = myHandler
}
ui.Actions{Bindings: bindings, Child: ...}
```

## Focus

### `ui.Focus(node, child)` — makes child a focus target

```go
var node ui.FocusNode
ui.Focus(&node, myWidget)
```

### `ui.FocusScope` — scopes focus traversal

```go
ui.FocusScope{
    Trap:         true,  // Tab/Shift+Tab stays inside
    AutoFocus:    true,  // grab focus on mount
    ReclaimFocus: true,  // re-grab if focus leaves (use sparingly)
    Child:        ...,
}
```

**Don't put `ReclaimFocus: true` on sibling scopes** — they'll fight for
focus every frame.

## Widget reconciliation pitfall

**Do not change the outermost widget type** returned by a stateful's `Build`
between frames when nested inside render-object widgets (Column, Row, Padding, etc.).

❌ Breaks:
```go
if loading { return ui.Center(spinner) }
return ui.Column(items...)
```

✅ Works — wrap in a stable outer type:
```go
return ui.Column(func() []ui.Widget {
    if loading { return []ui.Widget{ui.Center(spinner)} }
    return items
}()...)
```

The reconciler unmounts/mounts correctly at the element level, but ancestor
render objects don't re-discover the new render-object child, so the old tree
keeps painting. This is an upstream bug.

## Animation

```go
func (s *myState) InitState() {
    s.anim = s.NewAnimation(ui.AnimationOptions{
        Duration: 300 * time.Millisecond,
        Curve:    ui.EaseInOut,
    })
}

// In Build:
value := s.anim.Value()  // 0.0 → 1.0
s.anim.Forward()         // start
s.anim.Stop()            // pause
s.anim.Reset()           // back to 0
```

## Table

```go
ui.Table{
    Columns:   []ui.TableColumn{ui.IntrinsicColumn(), ui.FlexColumn(1), ui.FixedColumn(8)},
    ColumnGap: 2,
    Rows: []ui.TableRow{
        {Children: []ui.Widget{header1, header2, header3}},
        {Children: []ui.Widget{cell1, cell2, cell3}},
    },
}
```

## Selection and copy

```go
ui.SelectionArea{Child: selectable_content}
// Users can drag-select, double-click words, triple-click lines, Ctrl+C to copy
ui.SelectionContainer{Disabled: true, Child: not_selectable}
```

## Common patterns

### Bordered box with content (like a status bar)

Build the border manually with explicit rows to avoid the
DecoratedBox border-content overlap issue and to allow partial
coloring of individual border segments:

```go
borderStyle := ui.Style{Foreground: theme.Border}
ui.Flex{
    Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch,
    Children: []ui.Widget{
        // Top border: ┌───────┐
        ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
            ui.Text{Value: "┌", Style: borderStyle},
            ui.Expanded(ui.Divider{Style: borderStyle}),
            ui.Text{Value: "┐", Style: borderStyle},
        }},
        // Content with side borders
        ui.DecoratedBox(
            ui.Decoration{Border: ui.Border{Style: borderStyle, Left: true, Right: true}},
            ui.Padding(ui.Symmetric(1, 0), content),
        ),
        // Bottom border: └───────┘
        ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
            ui.Text{Value: "└", Style: borderStyle},
            ui.Expanded(ui.Divider{Style: borderStyle}),
            ui.Text{Value: "┘", Style: borderStyle},
        }},
    },
}
```

This gives you explicit control over each border segment (e.g. coloring
part of the top border for a progress indicator using flex-proportioned
dividers).

### Sticky-bottom scrollable list

```go
ui.CustomScrollView{
    FollowOutput: true,  // stays at bottom as content grows
    Slivers: []ui.Widget{
        ui.SliverToBox{Child: item1},
        ui.SliverToBox{Child: item2},
    },
}
```
