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
	composerBashBorder: string;
	composerBashExcludedBorder: string;

	// Text
	textPrimary: string;
	textSecondary: string;
	textMuted: string;
	textPlaceholder: string;
	textDebug: string;

	// Semantic (message roles)
	userText: string;
	userTextFocused: string;
	userBorder: string;
	assistantText: string;
	toolText: string;
	reviewText: string;
	errorText: string;
	warningText: string;
	subagentText: string;
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
	diffCursorBg: string;
	diffCursorGutterBg: string;
	diffCursorAddedBg: string;
	diffCursorRemovedBg: string;
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

/** Color tokens safe to expose outside the shell. */
export type ThemeColorTokens = Omit<ThemeTokens, "modalBackdrop">;

/** A fully resolved theme with all tokens filled in (except modalBackdrop). */
export type ResolvedTheme = {
	tokens: ThemeColorTokens;
	syntaxPalette: SyntaxPalette;
};

/** Public resolved theme config exposed to plugins. */
export type ThemeConfig = ResolvedTheme & { name: string };

/**
 * A partial theme definition for overrides.
 * User themes provide partial overrides
 * that get merged on top of the system theme.
 */
export type ThemeDefinition = {
	tokens?: Partial<ThemeColorTokens>;
	syntaxPalette?: Partial<SyntaxPalette>;
};
