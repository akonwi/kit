import { RGBA } from "@opentui/core";
import type { SyntaxPalette, ThemeTokens } from "./types";

// ── Color palette (dark) ─────────────────────────────────────────────

const black = "#0B0B0B";
const nearBlack = "#111111";
const darkGray = "#141414";
const midGray = "#2A2A2A";
const gray = "#A3A3A3";
const lightGray = "#D4D4D4";
const offWhite = "#F4F4F4";
const transparent = "transparent";

const blue = "#2563EB";
const brightBlue = "#60A5FA";
const green = "#86EFAC";
const red = "#F87171";
const amber = "#FDBA74";
const purple = "#A78BFA";
const cyan = "#67E8F9";
const rose = "#FDA4AF";

// ── Theme tokens ─────────────────────────────────────────────────────

export const KIT_TOKENS: ThemeTokens = {
	// Backgrounds
	bg: black,
	bgSurface: nearBlack,
	bgMuted: darkGray,
	bgAccent: midGray,
	bgTransparent: transparent,
	modalBackdrop: RGBA.fromInts(11, 11, 11, 180),

	// Borders
	borderDefault: midGray,
	borderFocused: gray,
	borderAccent: blue,
	borderDebug: purple,
	borderStatus: midGray,
	composerBashBorder: green,
	composerBashExcludedBorder: green,

	// Text
	textPrimary: offWhite,
	textSecondary: lightGray,
	textMuted: gray,
	textPlaceholder: "#525252",
	textDebug: lightGray,

	// Semantic (message roles)
	userText: brightBlue,
	userTextFocused: blue,
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
	pickerBorder: midGray,
	pickerFocusedBg: offWhite,
	pickerFocusedText: black,
	pickerItemText: offWhite,
	pickerScrollThumb: gray,
	pickerScrollTrack: midGray,

	// Scrollbar
	scrollbarFg: midGray,
	scrollbarBg: darkGray,

	// Spinner / panel
	panelText: gray,

	// Progress bar
	progressNormal: blue,
	progressWarning: "#F97316",
	progressCritical: "#DC2626",

	// Toggle
	toggleOn: "#172554",

	// Diff
	diffAddedBg: "#0B2B16",
	diffRemovedBg: "#3B0B0B",
	diffAddedContentBg: "#081F10",
	diffRemovedContentBg: "#2A0808",
	diffAddedLineNumberBg: "#061A0D",
	diffRemovedLineNumberBg: "#1F0606",
	diffCursorBg: "#404040",
	diffCursorGutterBg: midGray,
	diffCursorAddedBg: "#163D22",
	diffCursorRemovedBg: "#4D1212",
};

// ── Syntax palette ───────────────────────────────────────────────────

export const KIT_SYNTAX_PALETTE: SyntaxPalette = {
	text: offWhite,
	heading: brightBlue,
	bold: offWhite,
	italic: amber,
	link: brightBlue,
	list: brightBlue,
	quote: amber,
	codeInline: green,
	codeBlock: offWhite,
	strikethrough: gray,
	conceal: "#525252",
	comment: gray,
	string: green,
	escape: purple,
	number: amber,
	keyword: purple,
	keywordType: amber,
	function: brightBlue,
	operator: offWhite,
	variable: offWhite,
	member: brightBlue,
	builtin: red,
	type: amber,
	punctuation: gray,
	tag: red,
	tagAttribute: purple,
	tagDelimiter: gray,
	attribute: amber,
	label: brightBlue,
};
