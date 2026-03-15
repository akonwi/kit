/**
 * Centralized color tokens for the pi-kit shell.
 *
 * Palette inspired by the Stone desktop app dark theme.
 * All shell components should reference theme tokens
 * instead of hardcoding color strings.
 */

// ── Color palette ────────────────────────────────────────────────────

const black = "#0a0a0a";
const nearBlack = "#171717";
const darkGray = "#262626";
const midGray = "#404040";
const gray = "#a1a1a1";
const lightGray = "#d4d4d4";
const offWhite = "#fafafa";
const white = "white";
const transparent = "transparent";

const blue = "#6cb6ff";
const green = "#7ee787";
const red = "#ff6467";
const amber = "#ffb86a";
const purple = "#8a6bbd";

// ── Theme ────────────────────────────────────────────────────────────

export const theme = {
  // Backgrounds
  bg: black,
  bgSurface: nearBlack,
  bgMuted: darkGray,
  bgAccent: midGray,
  bgTransparent: transparent,

  // Borders
  borderDefault: darkGray,
  borderFocused: gray,
  borderAccent: blue,
  borderDebug: purple,
  borderStatus: darkGray,

  // Text
  textPrimary: offWhite,
  textSecondary: lightGray,
  textMuted: gray,
  textPlaceholder: midGray,
  textDebug: lightGray,

  // Semantic (message roles)
  userText: blue,
  userBorder: blue,
  assistantText: offWhite,
  toolText: green,
  errorText: red,
  warningText: amber,
  debugLabel: purple,

  // Cursor
  cursor: offWhite,

  // Picker
  pickerBg: nearBlack,
  pickerBorder: darkGray,
  pickerFocusedBg: offWhite,
  pickerFocusedText: black,
  pickerItemText: offWhite,
  pickerScrollThumb: gray,
  pickerScrollTrack: darkGray,

  // Scrollbar
  scrollbarFg: midGray,
  scrollbarBg: darkGray,

  // Spinner / panel
  panelText: gray,
};
