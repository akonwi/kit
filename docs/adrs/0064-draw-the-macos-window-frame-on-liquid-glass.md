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
glass ones, and system glass alone does not fit it:

- Untinted glass over a busy desktop is loud and washes out the session bar's
  muted metadata.
- The system sidebar is a grey floating panel that ignores the Kit palette
  and the user's theme.
- Tiles hold transcripts, terminals, diffs, and editors. Content under glass
  loses contrast, and terminal colours are chosen for an opaque background.

The app supports macOS 15, where glass does not exist.

## Decision

### The frame is one tinted glass sheet

On macOS 26 and later, the window frame is drawn as a single Liquid Glass
sheet under the theme's own frame colour at 85% opacity. The frame is the
region around the tiles: the session sidebar or top tab strip, the session
bar, and the gaps between tiles. In the Settings window the section list is
glass under the theme's raised colour at the same opacity. The desktop shows
softly through, and the frame keeps the theme's hue.

- Tiles, dialogs, the composer, and every content surface stay opaque.
- The layout, spacing, and tokens are unchanged. Glass needs no new theme
  tokens; each theme's frame colour is the tint.
- An inactive window keeps the same tint, and the system dims its glass.

### The tint keeps text legible

85% is the least tint at which chrome text stays readable over any desktop.
Measured with Kit's default tokens over an unblurred backdrop of the
opposite luminance (white behind a dark frame, black behind a light one):

| Tint | Muted text, dark | Muted text, light | Accent |
|---|---|---|---|
| 0.85 | 4.6:1 | 4.9:1 | ≥ 3.2:1 |
| 0.80 | 3.8:1 | 4.3:1 | — |
| 0.70 | 2.7:1 | 3.3:1 | — |

Blur only lowers the backdrop's contrast, so real desktops do better than
these figures.

### One switch, on by default

Settings › Appearance has a **Window glass** switch beneath Appearance mode,
stored as the app preference `windowGlass` and on by default. It applies to
every session window and the Settings window. The frame is opaque, exactly
as on macOS 15, when:

- the switch is off,
- Reduce Transparency is on in System Settings, or
- the Mac runs macOS 15, where the switch is not shown.

While the frame is glass its window is non-opaque with a clear background;
when glass turns off, the window gets its own background back.

## Consequences

- Kit looks native on macOS 26 without giving up its palette, and macOS 15
  is unchanged.
- Glass costs some compositing work, and the theme's frame colour can no
  longer be read off a screenshot exactly. People who want the flat frame
  turn the switch off.
- Future chrome added to the frame inherits the glass, so its text must keep
  the frame's text tokens and the 85% tint.
