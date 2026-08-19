# Experiment: browser TUI via ghostty-web

Status: experiment on `experiment/web-tui-ghostty`. This is paired with the wterm DOM experiment on `experiment/web-tui-wterm`.

## Goal

Expose Kit's existing OpenTUI interface in a browser without rebuilding it in the semantic Solid SPA, using ghostty-web's Ghostty WASM core and Canvas renderer.

## Architecture

Run:

```bash
kit --web-tui
kit --web-tui --model provider/model-id
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

The WebSocket protocol uses raw binary terminal bytes in both directions and small JSON controls. The `init` control carries an explicit protocol version, validated before a socket can replace the active client. Version-aware mismatches close with code 4002 and reload automatically; legacy pre-version clients close without entering a reconnect loop and require one manual reload. A validated newer browser tab supersedes the old tab with close code 4001.

## What works

- The complete real `AppShell`, including dialogs, pickers, workspace panes, review UI, plugin chrome, and themes
- One authoritative session/runtime owner
- Kit-owned browser keyboard normalization for Escape, Ctrl combinations, navigation, function keys, Linux Alt prefixes, and macOS Option/composition
- Browser-owned copy/paste shortcuts with explicit Canvas-selection copying, browser-routed whole-message Markdown copying, bracketed paste, Unicode preservation, and bounded input frames
- Browser-owned in-page notifications, opt-in Web Notifications, and user-gesture-unlocked bell audio without host `/dev/tty` or `afplay` effects
- Optional startup model selection through the standard `--model <provider>/<model-id>` selector
- Kit-owned SGR mouse encoding for click, release, drag, all-motion, and wheel events, with Shift-selection bypass and CSS-space DPR-safe coordinates
- Focus/bracketed-paste/synchronized-output/alternate-screen mode negotiation
- Resize and reconnect with full repaint
- Canvas selection, links, scrollback, titles, and a hidden textarea for browser input
- Shared web access policy with Host allowlisting, browser Origin validation, timing-safe Basic auth, and same-origin assets
- Canonical `--public-url` support for hosted Host/Origin validation and CSP WebSocket sources without trusting forwarded headers
- Route-specific CSP; only the terminal document gains `'wasm-unsafe-eval'`
- Existing semantic SPA remains the default web mode

## Validation

- `bun run typecheck`
- `bun run check`
- `bun test`: 743 passing
- Production `bun run build`
- `script/smoke-web-tui.ts` against both source and compiled modes:
  - health/document/assets
  - real alternate-screen frames
  - keyboard repaint
  - resize reflow
  - suspend/resume reconnect repaint
  - orderly SIGINT/SIGTERM shutdown, exact `130`/`143` exit codes, and immediate server-port release
- Automated Playwright coverage against the compiled binary: Chromium and Firefox on Linux, plus WebKit on macOS (18 tests total):
  - real ghostty-web/WASM startup with first-party CSP and asset checks
  - meaningful Canvas frames with exact custom-theme pixels and no hardcoded dark background
  - browser CSS variables and light `color-scheme`
  - exact Escape, Ctrl+C, navigation, platform Alt/Option, SGR left/right click/release/move/wheel, and resize WebSocket frames
  - Canvas selection bypass and clipboard content, bracketed Unicode paste chunking, synthetic IME completion, and DPR 1/2 coordinate parity
  - focused and hidden browser notifications, explicit permission prompting, denied-permission focus restoration, and bell controls
  - forced WebSocket reconnect and page reload with verified full repaint and no duplicate Canvas or textarea state
  - protocol-version rejection before active-client promotion so stale browser bundles cannot silently drop controls or evict a compatible client
  - no failed requests, browser console errors, or page errors
  - isolated temporary HOME/workspace and orderly process teardown
- `bun run test:web-tui-browser` builds the binary and runs all installed browser projects locally; `.github/workflows/web-tui-browser.yml` runs the platform matrix for relevant pull requests and `main`, and the release workflow gates publication on it.

## Desktop input compatibility

The browser TUI targets macOS and Linux desktops. Linux Alt+printable keys use the conventional Escape prefix. macOS Option-produced characters are sent as text without an Alt prefix; dead keys and IME completion use the composition path. Cmd+C/Cmd+V on macOS and Ctrl+Shift+C/Ctrl+V on Linux remain browser-owned. Canvas selections are copied explicitly because they are not DOM selections. Shift+mouse remains local selection rather than SGR input.

Input uses deterministic legacy terminal sequences. This covers Kit's control-letter, navigation, function-key, mouse, paste, and selection workflows, but cannot represent every modified key, key release, or browser-reserved shortcut. Cmd/Ctrl shortcuts owned by browser chrome remain unavailable. Kitty keyboard mode stays disabled until Kit can track negotiated Kitty flags and encode them consistently.

Playwright's macOS WebKit build is not the installed Safari application, and synthetic composition does not replace native OS IME validation. Actual Safari, macOS dead-key/IME, and Linux desktop IME behavior remain a short manual release checklist. Windows is not in the supported browser-TUI matrix. The semantic web app remains the supported mobile and touch interface.

## Startup model

`--model <provider>/<model-id>` uses the same exact provider/model selector and model-adaptation path as print, RPC, and semantic web modes. Browser-TUI startup applies the selection before plugins and the composer become ready. If the requested known provider is not authenticated, Kit remains at the authentication gate so the user can log into the correct provider and retry; unknown providers or models produce the normal fatal startup diagnostic.

## Browser notifications

Browser-TUI notification and bell effects are explicit host capabilities. Local terminal mode retains BEL, terminal notifications, and the macOS error sound; browser-TUI mode sends bounded controls only to the active initialized browser and drops them while disconnected. This prevents the server workstation from ringing or displaying completion notifications for a remote browser session.

Every notification appears briefly in browser-owned chrome while the page is focused. Web Notifications are created only while hidden/unfocused and only after the user explicitly selects **Enable notifications**; Kit never prompts automatically. Bell audio is local to the browser and remains silent until a pointer or keyboard gesture unlocks Web Audio. Permission denial or policy failure leaves the in-page indicator available and restores terminal focus.

## Hosting boundary

Loopback remains the safe default and an explicit `--host` controls other bindings. Kit does not infer public reachability or own deployment-specific TLS, identity, network ACL, ingress, rate-limit, resource-limit, or egress policy. Hosted deployments can provide a canonical external origin with `--public-url https://kit.example.com`; this configures browser Host/Origin boundaries without implicitly trusting `Forwarded` or `X-Forwarded-*` headers.

Application-level Origin validation remains active for cross-site WebSocket protection, with Host validation as defense-in-depth. Optional Basic Auth is confidential only behind HTTPS. A hosted Kit process is single-principal and must be isolated from other principals by the hosting environment.

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
- The browser TUI is desktop-focused; the semantic web app owns mobile/touch, native uploads, and mobile-specific layout.
- Actual Safari and native macOS/Linux IME remain manual compatibility checks; browser-reserved shortcuts are documented limitations.
- Browser input currently uses deterministic legacy key sequences. Kitty keyboard mode remains disabled until the adapter tracks Kitty protocol flags.
- ghostty-web 0.4.0 is unofficial and young.

## Recommendation

The remote-stream architecture is viable and removes almost all presentation duplication. Keep the semantic SPA as Kit's accessible/mobile/browser-native interface, and consider a browser TUI as an optional desktop parity surface.

Use ghostty-web for Kit's browser TUI. It provides the strongest rendering fidelity and its Canvas output preserves Kit's full custom-theme system. Keep the semantic SPA available where accessibility and browser-native content matter more than terminal parity. Before promotion, retain the Kit-owned input and theme adapters, pursue a supported dynamic selection/cursor API upstream, and address the remaining lifecycle and security gaps above.
