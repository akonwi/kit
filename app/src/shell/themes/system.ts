import type { TerminalColors } from "@opentui/core";
import type { ResolvedTheme } from "./types";

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

// ── Standard xterm fallback palette ──────────────────────────────────

const XTERM_PALETTE: string[] = [
	"#000000", // 0  black
	"#800000", // 1  red
	"#008000", // 2  green
	"#808000", // 3  yellow
	"#000080", // 4  blue
	"#800080", // 5  magenta
	"#008080", // 6  cyan
	"#C0C0C0", // 7  white
	"#808080", // 8  bright black
	"#FF0000", // 9  bright red
	"#00FF00", // 10 bright green
	"#FFFF00", // 11 bright yellow
	"#0000FF", // 12 bright blue
	"#FF00FF", // 13 bright magenta
	"#00FFFF", // 14 bright cyan
	"#FFFFFF", // 15 bright white
];

const DEFAULT_TERMINAL_COLORS: TerminalColors = {
	palette: XTERM_PALETTE,
	defaultBackground: "#000000",
	defaultForeground: "#C0C0C0",
	cursorColor: "#FFFFFF",
	mouseForeground: "#000000",
	mouseBackground: "#FFFFFF",
	tekForeground: "#000000",
	tekBackground: "#FFFFFF",
	highlightBackground: "#C0C0C0",
	highlightForeground: "#000000",
};

// ── Color utilities ──────────────────────────────────────────────────

export function parseHex(hex: string): [number, number, number] {
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
	return toHex(ar + (br - ar) * t, ag + (bg - ag) * t, ab + (bb - ab) * t);
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

export function buildSystemTheme(colors: TerminalColors): ResolvedTheme {
	const p = (index: number): string | null => colors.palette[index] ?? null;

	const bg = colors.defaultBackground ?? p(ANSI.BLACK) ?? "#000000";
	const fg = colors.defaultForeground ?? p(ANSI.WHITE) ?? "#C0C0C0";
	const isDark = luminance(bg) < 0.5;

	// On dark backgrounds prefer bright ANSI variants for readability;
	// on light backgrounds prefer the dim variants.
	const ansiColor = (
		dim: number,
		bright: number,
		darkFallback: string,
		lightFallback: string,
	): string =>
		(isDark ? (p(bright) ?? p(dim)) : (p(dim) ?? p(bright))) ??
		(isDark ? darkFallback : lightFallback);

	const red = ansiColor(ANSI.RED, ANSI.BRIGHT_RED, "#FF5555", "#CC0000");
	const green = ansiColor(ANSI.GREEN, ANSI.BRIGHT_GREEN, "#55FF55", "#005500");
	const yellow = ansiColor(
		ANSI.YELLOW,
		ANSI.BRIGHT_YELLOW,
		"#FFFF55",
		"#888800",
	);
	const blue = ansiColor(ANSI.BLUE, ANSI.BRIGHT_BLUE, "#5555FF", "#0000CC");
	const magenta = ansiColor(
		ANSI.MAGENTA,
		ANSI.BRIGHT_MAGENTA,
		"#FF55FF",
		"#880088",
	);
	const cyan = ansiColor(ANSI.CYAN, ANSI.BRIGHT_CYAN, "#55FFFF", "#008888");

	const brightWhite = p(ANSI.BRIGHT_WHITE) ?? fg;

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

	// Diff cursor — stronger tint for the active line
	const diffCursorBg = lerp(bg, fg, isDark ? 0.25 : 0.2);
	const diffCursorGutterBg = bgAccent;
	const diffCursorAddedBg = tint(bg, green, 0.2);
	const diffCursorRemovedBg = tint(bg, red, 0.2);

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
			composerBashBorder: green,
			composerBashExcludedBorder: green,

			textPrimary: fg,
			textSecondary,
			textMuted,
			textPlaceholder,
			textDebug: textSecondary,

			userText: blue,
			userTextFocused: tint(bg, blue, 0.55),
			userBorder: blue,
			assistantText: fg,
			toolText: green,
			reviewText: magenta,
			errorText: red,
			warningText: yellow,
			subagentText: magenta,
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
			diffCursorBg,
			diffCursorGutterBg,
			diffCursorAddedBg,
			diffCursorRemovedBg,
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

/**
 * Build a theme from the standard xterm fallback palette.
 * Used as the initial store value before the terminal palette is queried.
 */
export function buildDefaultTheme(): ResolvedTheme {
	return buildSystemTheme(DEFAULT_TERMINAL_COLORS);
}
