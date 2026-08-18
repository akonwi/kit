# Experiment: browser TUI via ghostty-web

Status: experiment on `experiment/web-tui-ghostty`. This is paired with the wterm DOM experiment on `experiment/web-tui-wterm`.

## Goal

Expose Kit's existing OpenTUI interface in a browser without rebuilding it in the semantic Solid SPA, using ghostty-web's Ghostty WASM core and Canvas renderer.

## Architecture

Run:

```bash
kit --web --experimental-tui
```

Normal `kit --web` behavior is unchanged. The experimental mode has one runtime owner:

```text
browser                              server
┌─────────────────────────┐          ┌──────────────────────────────────┐
│ ghostty-web Canvas      │ ws bytes │ WebTuiServer                    │
│ Ghostty VT + key encoder│◄────────►│ WebTuiBridge virtual tty        │
│ fit/input/mouse/paste   │ resize   │ OpenTUI CliRenderer(remote)     │
└─────────────────────────┘          │ real Kit App / AppShell/runtime │
                                     └──────────────────────────────────┘
```

OpenTUI 0.5.1 accepts custom stdin/stdout streams and explicit dimensions. A non-process stdout activates its remote `NativeSpanFeed`, so no PTY, child Kit process, fixture, or second session runtime is needed.

The app starts lazily after the browser initializes its terminal. On disconnect, the bridge suspends OpenTUI. Reattach resumes it, replays terminal setup, reapplies dimensions, and forces a full repaint. This is deterministic and does not require a VT output journal.

The WebSocket protocol uses raw binary terminal bytes in both directions and small JSON `init`/`resize` controls. A newer browser tab supersedes the old tab with close code 4001.

## What works

- The complete real `AppShell`, including dialogs, pickers, workspace panes, review UI, plugin chrome, and themes
- One authoritative session/runtime owner
- Kit-owned browser keyboard normalization for Escape, Ctrl combinations, navigation, and function keys
- Kit-owned SGR mouse encoding for click, release, drag, all-motion, and wheel events
- Focus/bracketed-paste/synchronized-output/alternate-screen mode negotiation
- Resize and reconnect with full repaint
- Canvas selection, links, scrollback, titles, and a hidden textarea for browser input
- Existing Host allowlist, Origin validation, timing-safe Basic auth, and same-origin assets
- Route-specific CSP; only the terminal document gains `'wasm-unsafe-eval'`
- Existing semantic SPA remains the default web mode

## Validation

- `bun run typecheck`
- `bun run check`
- `bun test`: 709 passing
- Production `bun run build`
- `script/smoke-web-tui.ts` against both source and compiled modes:
  - health/document/assets
  - real alternate-screen frames
  - keyboard repaint
  - resize reflow
  - suspend/resume reconnect repaint
  - clean SIGINT shutdown
- Real Chromium against the compiled binary:
  - one Canvas and one focused hidden textarea
  - WebSocket connected without console errors
  - typing accepted
  - page reload reconnected and recreated the Canvas without errors

## Footprint

| Artifact | Bytes |
| --- | ---: |
| Minified ghostty-web client JS | 640,072 |
| Ghostty WASM asset | 423,045 |
| Total before compression | 1,063,117 |
| Existing minified semantic SPA JS | 10,092,169 |
| Clean Kit binary | 93,136,672 |
| Kit binary with experiment | 93,826,144 |
| Binary increase | 689,472 |

The ghostty-web module contains an embedded base64 WASM fallback even when Kit loads the same-origin WASM URL, so this package version duplicates part of the WASM payload in JS. This is the main footprint disadvantage relative to wterm.

## Gaps

- This mode replaces the SPA in the process. Hosting both interfaces on one session still needs ADR 0027's attach/session-host boundary.
- Single active browser terminal only. Multiple independent clients require one renderer and geometry per client over a shared session host.
- Canvas is poor for accessibility and browser-native find compared with DOM. It cannot replace the SPA's semantic message and form structure.
- Mobile gets a hidden textarea and viewport fitting but no touch shortcut bar, native upload flow, or mobile-specific layout.
- Terminal bell and OSC 52 paths that write directly to process stdout do not reach the browser renderer.
- Security policy is mirrored from `WebRpcServer` rather than extracted into one shared implementation.
- `--model` is not accepted in this mode; select the model inside the TUI.
- Complex IME, selection, links, touch gestures, browser-reserved shortcuts, and long-running reconnects need broader browser testing.
- Browser input currently uses deterministic legacy key sequences. Kitty keyboard mode remains disabled until the adapter tracks Kitty protocol flags.
- ghostty-web 0.4.0 is unofficial and young.

## Recommendation

The remote-stream architecture is viable and removes almost all presentation duplication. Keep the semantic SPA as Kit's accessible/mobile/browser-native interface, and consider a browser TUI as an optional desktop parity surface.

Between the two terminal renderers, ghostty-web offers the strongest rendering fidelity, but its Canvas accessibility and duplicated WASM packaging are meaningful costs. The paired wterm experiment is likely the better default browser surface if native selection/find and a much smaller payload matter more than Canvas rendering fidelity.
