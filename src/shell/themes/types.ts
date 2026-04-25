import type { RGBA } from "@opentui/core";

/** All color tokens consumed by shell components. */
export type ThemeTokens = {
	// Backgrounds
	bg: string;
	bgSurface: string;
	bgMuted: string;
	bgAccent: string;
	bgTransparent: string;
	modalBackdrop: RGBA;

	// Borders
	borderDefault: string;
	borderFocused: string;
	borderAccent: string;
	borderDebug: string;
	borderStatus: string;

	// Text
	textPrimary: string;
	textSecondary: string;
	textMuted: string;
	textPlaceholder: string;
	textDebug: string;

	// Semantic (message roles)
	userText: string;
	userBorder: string;
	assistantText: string;
	toolText: string;
	reviewText: string;
	errorText: string;
	warningText: string;
	debugLabel: string;

	// Secondary
	metaText: string;
	attachmentText: string;

	// Cursor
	cursor: string;

	// Picker
	pickerBg: string;
	pickerBorder: string;
	pickerFocusedBg: string;
	pickerFocusedText: string;
	pickerItemText: string;
	pickerScrollThumb: string;
	pickerScrollTrack: string;

	// Scrollbar
	scrollbarFg: string;
	scrollbarBg: string;

	// Spinner / panel
	panelText: string;

	// Progress bar
	progressNormal: string;
	progressWarning: string;
	progressCritical: string;

	// Toggle
	toggleOn: string;

	// Diff
	diffAddedBg: string;
	diffRemovedBg: string;
	diffAddedContentBg: string;
	diffRemovedContentBg: string;
	diffAddedLineNumberBg: string;
	diffRemovedLineNumberBg: string;
};

/** Named color slots for syntax highlighting rules. */
export type SyntaxPalette = {
	text: string;
	heading: string;
	bold: string;
	italic: string;
	link: string;
	list: string;
	quote: string;
	codeInline: string;
	codeBlock: string;
	strikethrough: string;
	conceal: string;
	comment: string;
	string: string;
	escape: string;
	number: string;
	keyword: string;
	keywordType: string;
	function: string;
	operator: string;
	variable: string;
	member: string;
	builtin: string;
	type: string;
	punctuation: string;
	tag: string;
	tagAttribute: string;
	tagDelimiter: string;
	attribute: string;
	label: string;
};

/**
 * A partial theme definition for overrides.
 * User themes and the system theme provide partial overrides
 * that get merged with kit defaults.
 */
export type ThemeDefinition = {
	tokens?: Partial<Omit<ThemeTokens, "modalBackdrop">>;
	syntaxPalette?: Partial<SyntaxPalette>;
};
