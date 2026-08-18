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
│ Ghostty VT renderer     │◄────────►│ WebTuiBridge virtual tty        │
│ Kit input/mouse adapter │ resize   │ OpenTUI CliRenderer(remote)     │
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
- `bun test`: 714 passing
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

## Theme fidelity

Custom themes are supported because this mode renders the real reactive OpenTUI shell. Theme tokens and syntax palettes resolve server-side exactly as in a local terminal, then OpenTUI emits truecolor VT cells for ghostty-web to render.

A controlled light-theme test verified all three stages:

| Stage | Result |
| --- | --- |
| `~/.kit/themes/{name}.json` resolution | Custom token values loaded and merged over the system theme |
| OpenTUI VT output | Exact custom background `48;2;253;246;227` and foreground `38;2;18;52;86` sequences observed |
| ghostty-web Canvas | 315,903 of 319,950 sampled pixels used the exact custom background |
| `/theme` live preview | Canvas switched from system dark to the custom light palette without reconnecting |
| Preview dismissal | Canvas restored the exact system background |
| Browser-owned chrome | Page background, status overlay, and `color-scheme` update from live theme controls |

This is materially more capable than the semantic SPA's light/dark mapping. Shell colors, overlays, diffs, Markdown, code syntax, and plugin-exposed theme tokens all use the actual selected theme.

Resolved theme changes are also sent to the browser as bounded control messages. The client updates CSS variables for the page background, foreground, status overlay, and browser `color-scheme`. A controlled light-theme run verified the exact expected CSS values and no residual dark pixels in the connected Canvas.

Remaining browser-owned colors:

- ghostty-web's Canvas selection background and terminal fallback cursor are initialized from the startup palette.
- ghostty-web 0.4.0 exposes a renderer-level `setTheme`, but its public `Terminal` API warns that theme changes after `open()` are not fully supported.
- Before the first resolved theme event, loading chrome uses the safe default dark palette.

These do not affect connected OpenTUI cells because they are painted with truecolor values. Full selection/fallback-cursor synchronization should wait for a supported ghostty-web terminal API rather than reaching into its private renderer.

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

Use ghostty-web for Kit's browser TUI. It provides the strongest rendering fidelity and its Canvas output preserves Kit's full custom-theme system. Keep the semantic SPA available where accessibility and browser-native content matter more than terminal parity. Before promotion, retain the Kit-owned input and theme adapters, pursue a supported dynamic selection/cursor API upstream, and address the remaining lifecycle and security gaps above.
