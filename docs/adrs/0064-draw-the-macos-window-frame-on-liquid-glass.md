# 0064: Draw the macOS window frame on Liquid Glass

## Status

Accepted. Applies to the macOS app of
[ADR 0013](0013-native-macos-client.md). The iOS client of
[ADR 0056](0056-build-a-native-ios-client.md) uses the system's glass
controls and is unaffected.

## Context

macOS 26 draws its own windows, sidebars, and toolbars on Liquid Glass, a
material that blurs and refracts what lies behind it. Kit for Mac hides the
system title bar and draws its own chrome in the theme's colours: the session
sidebar or top tab strip, the session bar, the gaps between tiles, and the
Settings section list. Without a decision Kit is the one opaque window among
glass ones. Glass brings its own tensions:

- Untinted glass takes its hue from the desktop, so the chrome no longer
  carries the user's theme, and over a busy desktop it can wash out the
  session bar's muted metadata.
- The system sidebar is a floating panel inset from the window edge, which
  does not fit Kit's flush frame around tiles.
- Tiles hold transcripts, terminals, diffs, and editors. Content under glass
  loses contrast, and terminal colours are chosen for an opaque background.

The app supports macOS 15, where glass does not exist.

## Decision

### The frame is one glass sheet

On macOS 26 and later, the window frame is drawn as a single Liquid Glass
sheet. The frame is the region around the tiles: the session sidebar or top
tab strip, the session bar, and the gaps between tiles. In the Settings
window the section list is glass too. The desktop shows through.

- Tiles, dialogs, the composer, and every content surface stay opaque.
- The layout, spacing, and tokens are unchanged. Glass needs no new theme
  tokens.
- An inactive window keeps the same look, and the system dims its glass.

### The theme's tint by default, the system's glass on request

By default, **Tint with theme** lays the theme's frame colour over the glass
at 50% (the raised colour in Settings), so the frame keeps the theme's hue,
such as Flexoki's cream, while the desktop still shows through. At 50% the
frame plainly reads as glass and chrome text stays readable over typical
desktops.

With Tint with theme off, the frame is the system's standard glass with
nothing over it, the same material as system sidebars. It looks native beside
other apps and picks up any system-wide glass adjustment, but its hue comes
from the desktop rather than the theme.

Neither look holds muted chrome text at 4.5:1 over every desktop. Untinted
glass is weakest for a light theme over a dark desktop, where the frame turns
a cool mid-grey. A 50% tint keeps the frame lighter there. Anyone who finds
the glass frame hard to read turns it off or turns on Reduce Transparency.

The system's `clear` glass variant is not used: it leaves muted text nearly
invisible in dark mode. macOS offers no public API for the Clear or Tinted
choice in System Settings › Appearance, so Kit does not try to follow it
beyond what the standard glass does on its own.

### Two switches

Settings › Appearance has a **Window glass** switch beneath Appearance mode,
stored as the app preference `windowGlass` and on by default. Indented
beneath it, **Tint with theme** is stored as `windowGlassThemeTint` and on
by default; it is dimmed while Window glass is off or Reduce Transparency is
on. Both apply to every session window and the Settings window. The frame
is opaque, exactly as on macOS 15, when:

- Window glass is off,
- Reduce Transparency is on in System Settings, or
- the Mac runs macOS 15, where neither switch is shown.

While the frame is glass its window is non-opaque with a clear background;
when glass turns off, the window gets its own background back.

## Consequences

- Kit's chrome is glass on macOS 26 and keeps the theme's colour by
  default, and people who want the system's own glass get it with one
  switch. macOS 15 is unchanged.
- Glass costs some compositing work, and the frame's colour can no longer
  be read off a screenshot exactly. People who want the flat frame turn
  Window glass off.
- Muted chrome text is not guaranteed 4.5:1 over every desktop while glass
  is on. Window glass off and Reduce Transparency are the accessible paths.
- Future chrome added to the frame inherits the glass, so it must draw its
  background with the shared window backdrop and keep the frame's text
  tokens.
