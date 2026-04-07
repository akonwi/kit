/**
 * Centralized color tokens for the kit shell.
 *
 * Palette inspired by the Stone desktop app dark theme.
 * All shell components should reference theme tokens
 * instead of hardcoding color strings.
 */

import { SyntaxStyle } from "@opentui/core";

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

// ── Syntax style for markdown / code rendering ───────────────────────

import { RGBA, type ThemeTokenStyle } from "@opentui/core";

function rgba(hex: string): RGBA {
  return RGBA.fromHex(hex);
}

const themeRules: ThemeTokenStyle[] = [
  // Default text
  { scope: ["default"], style: { foreground: rgba(offWhite) } },

  // ── Markdown ────────────────────────────────────────────────────
  { scope: ["markup.heading", "markup.heading.1", "markup.heading.2", "markup.heading.3", "markup.heading.4", "markup.heading.5", "markup.heading.6"], style: { foreground: rgba(blue), bold: true } },
  { scope: ["markup.bold", "markup.strong"], style: { foreground: rgba(offWhite), bold: true } },
  { scope: ["markup.italic"], style: { foreground: rgba(amber), italic: true } },
  { scope: ["markup.list"], style: { foreground: rgba(blue) } },
  { scope: ["markup.quote"], style: { foreground: rgba(amber), italic: true } },
  { scope: ["markup.raw.block"], style: { foreground: rgba(offWhite) } },
  { scope: ["markup.raw", "markup.raw.inline"], style: { foreground: rgba(green) } },
  { scope: ["markup.link"], style: { foreground: rgba(blue), underline: true } },
  { scope: ["markup.link.label"], style: { foreground: rgba(blue), underline: true } },
  { scope: ["markup.link.url"], style: { foreground: rgba(blue), underline: true } },
  { scope: ["markup.strikethrough"], style: { foreground: rgba(gray) } },
  { scope: ["conceal"], style: { foreground: rgba(midGray) } },
  { scope: ["spell", "nospell"], style: { foreground: rgba(offWhite) } },

  // ── Code / tree-sitter scopes ───────────────────────────────────
  { scope: ["comment", "comment.documentation"], style: { foreground: rgba(gray), italic: true } },
  { scope: ["string", "symbol", "character", "character.special"], style: { foreground: rgba(green) } },
  { scope: ["string.escape", "string.regexp"], style: { foreground: rgba(purple) } },
  { scope: ["number", "boolean", "float", "constant"], style: { foreground: rgba(amber) } },
  { scope: ["keyword", "keyword.import", "keyword.export", "keyword.directive", "keyword.modifier", "keyword.exception"], style: { foreground: rgba(purple), italic: true } },
  { scope: ["keyword.return", "keyword.conditional", "keyword.repeat", "keyword.coroutine"], style: { foreground: rgba(purple), italic: true } },
  { scope: ["keyword.type"], style: { foreground: rgba(amber), bold: true, italic: true } },
  { scope: ["keyword.function", "function.method"], style: { foreground: rgba(blue) } },
  { scope: ["operator", "keyword.operator", "punctuation.delimiter", "keyword.conditional.ternary"], style: { foreground: rgba(offWhite) } },
  { scope: ["variable", "variable.parameter", "function.method.call", "function.call", "property", "field", "parameter"], style: { foreground: rgba(offWhite) } },
  { scope: ["variable.member", "function", "constructor"], style: { foreground: rgba(blue) } },
  { scope: ["variable.builtin", "type.builtin", "function.builtin", "module.builtin", "constant.builtin", "variable.super"], style: { foreground: rgba(red) } },
  { scope: ["type", "module", "namespace", "class", "type.definition"], style: { foreground: rgba(amber) } },
  { scope: ["punctuation", "punctuation.bracket", "punctuation.special"], style: { foreground: rgba(gray) } },
  { scope: ["tag"], style: { foreground: rgba(red) } },
  { scope: ["tag.attribute"], style: { foreground: rgba(purple) } },
  { scope: ["tag.delimiter"], style: { foreground: rgba(gray) } },
  { scope: ["attribute", "annotation"], style: { foreground: rgba(amber) } },
  { scope: ["label"], style: { foreground: rgba(blue) } },
];

/** Shared SyntaxStyle instance for markdown/code components. */
export const syntaxStyle: SyntaxStyle = SyntaxStyle.fromTheme(themeRules);
