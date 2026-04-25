import type { TerminalColors } from "@opentui/core";
import { KIT_SYNTAX_PALETTE, KIT_TOKENS } from "./kit";
import type { ThemeDefinition } from "./types";

// ── ANSI 16 palette indices ──────────────────────────────────────────

const ANSI = {
	BLACK: 0,
	RED: 1,
	GREEN: 2,
	YELLOW: 3,
	BLUE: 4,
	MAGENTA: 5,
	CYAN: 6,
	WHITE: 7,
	BRIGHT_BLACK: 8,
	BRIGHT_RED: 9,
	BRIGHT_GREEN: 10,
	BRIGHT_YELLOW: 11,
	BRIGHT_BLUE: 12,
	BRIGHT_MAGENTA: 13,
	BRIGHT_CYAN: 14,
	BRIGHT_WHITE: 15,
} as const;

// ── Color utilities ──────────────────────────────────────────────────

function parseHex(hex: string): [number, number, number] {
	const h = hex.replace("#", "");
	return [
		parseInt(h.slice(0, 2), 16),
		parseInt(h.slice(2, 4), 16),
		parseInt(h.slice(4, 6), 16),
	];
}

function toHex(r: number, g: number, b: number): string {
	const clamp = (n: number) => Math.max(0, Math.min(255, Math.round(n)));
	return `#${clamp(r).toString(16).padStart(2, "0")}${clamp(g).toString(16).padStart(2, "0")}${clamp(b).toString(16).padStart(2, "0")}`;
}

function lerp(a: string, b: string, t: number): string {
	const [ar, ag, ab] = parseHex(a);
	const [br, bg, bb] = parseHex(b);
	return toHex(
		ar + (br - ar) * t,
		ag + (bg - ag) * t,
		ab + (bb - ab) * t,
	);
}

function luminance(hex: string): number {
	const [r, g, b] = parseHex(hex);
	return (r * 299 + g * 587 + b * 114) / 1000 / 255;
}

/** Tint a base color toward an accent at a given opacity. */
function tint(base: string, accent: string, opacity: number): string {
	return lerp(base, accent, opacity);
}

// ── System theme builder ─────────────────────────────────────────────

export function buildSystemTheme(colors: TerminalColors): ThemeDefinition {
	const p = (index: number): string | null => colors.palette[index] ?? null;

	const bg = colors.defaultBackground ?? p(ANSI.BLACK) ?? KIT_TOKENS.bg;
	const fg = colors.defaultForeground ?? p(ANSI.WHITE) ?? KIT_TOKENS.textPrimary;

	const red = p(ANSI.RED) ?? KIT_TOKENS.errorText;
	const green = p(ANSI.GREEN) ?? KIT_TOKENS.toolText;
	const yellow = p(ANSI.YELLOW) ?? KIT_TOKENS.warningText;
	const blue = p(ANSI.BLUE) ?? KIT_TOKENS.userText;
	const magenta = p(ANSI.MAGENTA) ?? KIT_TOKENS.reviewText;
	const cyan = p(ANSI.CYAN) ?? KIT_TOKENS.metaText;

	const brightBlack = p(ANSI.BRIGHT_BLACK) ?? lerp(bg, fg, 0.25);
	const brightWhite = p(ANSI.BRIGHT_WHITE) ?? fg;
	const brightBlue = p(ANSI.BRIGHT_BLUE) ?? blue;
	const brightGreen = p(ANSI.BRIGHT_GREEN) ?? green;
	const brightRed = p(ANSI.BRIGHT_RED) ?? red;

	const isDark = luminance(bg) < 0.5;

	// Grayscale ramp interpolated between bg and fg
	const bgSurface = lerp(bg, fg, isDark ? 0.05 : 0.03);
	const bgMuted = lerp(bg, fg, isDark ? 0.1 : 0.07);
	const bgAccent = lerp(bg, fg, isDark ? 0.2 : 0.15);
	const textPlaceholder = lerp(bg, fg, isDark ? 0.25 : 0.3);
	const textMuted = lerp(bg, fg, isDark ? 0.55 : 0.45);
	const textSecondary = lerp(bg, fg, isDark ? 0.8 : 0.7);

	// Diff backgrounds — tint the base bg toward green/red
	const diffAddedBg = tint(bg, green, 0.12);
	const diffRemovedBg = tint(bg, red, 0.12);
	const diffAddedContentBg = tint(bg, green, 0.08);
	const diffRemovedContentBg = tint(bg, red, 0.08);
	const diffAddedLineNumberBg = tint(bg, green, 0.06);
	const diffRemovedLineNumberBg = tint(bg, red, 0.06);

	return {
		tokens: {
			bg,
			bgSurface,
			bgMuted,
			bgAccent,
			bgTransparent: "transparent",

			borderDefault: bgMuted,
			borderFocused: textMuted,
			borderAccent: blue,
			borderDebug: magenta,
			borderStatus: bgMuted,

			textPrimary: fg,
			textSecondary,
			textMuted,
			textPlaceholder,
			textDebug: textSecondary,

			userText: blue,
			userBorder: blue,
			assistantText: fg,
			toolText: green,
			reviewText: magenta,
			errorText: red,
			warningText: yellow,
			debugLabel: magenta,

			metaText: cyan,
			attachmentText: lerp(red, fg, 0.3),

			cursor: fg,

			pickerBg: bgSurface,
			pickerBorder: bgMuted,
			pickerFocusedBg: fg,
			pickerFocusedText: bg,
			pickerItemText: fg,
			pickerScrollThumb: textMuted,
			pickerScrollTrack: bgMuted,

			scrollbarFg: bgAccent,
			scrollbarBg: bgMuted,

			panelText: textMuted,

			progressNormal: blue,
			progressWarning: yellow,
			progressCritical: red,

			toggleOn: lerp(blue, bg, 0.3),

			diffAddedBg,
			diffRemovedBg,
			diffAddedContentBg,
			diffRemovedContentBg,
			diffAddedLineNumberBg,
			diffRemovedLineNumberBg,
		},
		syntaxPalette: {
			text: fg,
			heading: blue,
			bold: brightWhite,
			italic: yellow,
			link: blue,
			list: blue,
			quote: yellow,
			codeInline: green,
			codeBlock: fg,
			strikethrough: textMuted,
			conceal: textPlaceholder,
			comment: textMuted,
			string: green,
			escape: magenta,
			number: yellow,
			keyword: magenta,
			keywordType: yellow,
			function: blue,
			operator: fg,
			variable: fg,
			member: blue,
			builtin: red,
			type: yellow,
			punctuation: textMuted,
			tag: red,
			tagAttribute: magenta,
			tagDelimiter: textMuted,
			attribute: yellow,
			label: blue,
		},
	};
}
