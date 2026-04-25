import { RGBA } from "@opentui/core";
import type { SyntaxPalette, ThemeTokens } from "./types";

// ── Color palette ────────────────────────────────────────────────────

const black = "#0a0a0a";
const nearBlack = "#171717";
const darkGray = "#262626";
const midGray = "#404040";
const gray = "#a1a1a1";
const lightGray = "#d4d4d4";
const offWhite = "#fafafa";
const transparent = "transparent";

const blue = "#6cb6ff";
const green = "#7ee787";
const red = "#ff6467";
const amber = "#ffb86a";
const purple = "#8a6bbd";
const cyan = "#7dcfff";
const rose = "#e78a9a";

// ── Theme tokens ─────────────────────────────────────────────────────

export const KIT_TOKENS: ThemeTokens = {
	// Backgrounds
	bg: black,
	bgSurface: nearBlack,
	bgMuted: darkGray,
	bgAccent: midGray,
	bgTransparent: transparent,
	modalBackdrop: RGBA.fromInts(10, 10, 10, 180),

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
	reviewText: purple,
	errorText: red,
	warningText: amber,
	debugLabel: purple,

	// Secondary
	metaText: cyan,
	attachmentText: rose,

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

	// Progress bar
	progressNormal: "#5599dd",
	progressWarning: "#dd8833",
	progressCritical: "#dd3333",

	// Toggle
	toggleOn: "#567fab",

	// Diff
	diffAddedBg: "#16351f",
	diffRemovedBg: "#3a1f24",
	diffAddedContentBg: "#0f2917",
	diffRemovedContentBg: "#291217",
	diffAddedLineNumberBg: "#102717",
	diffRemovedLineNumberBg: "#2a1519",
};

// ── Syntax palette ───────────────────────────────────────────────────

export const KIT_SYNTAX_PALETTE: SyntaxPalette = {
	text: offWhite,
	heading: blue,
	bold: offWhite,
	italic: amber,
	link: blue,
	list: blue,
	quote: amber,
	codeInline: green,
	codeBlock: offWhite,
	strikethrough: gray,
	conceal: midGray,
	comment: gray,
	string: green,
	escape: purple,
	number: amber,
	keyword: purple,
	keywordType: amber,
	function: blue,
	operator: offWhite,
	variable: offWhite,
	member: blue,
	builtin: red,
	type: amber,
	punctuation: gray,
	tag: red,
	tagAttribute: purple,
	tagDelimiter: gray,
	attribute: amber,
	label: blue,
};
