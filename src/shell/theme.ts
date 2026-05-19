/**
 * Centralized color tokens for the kit shell.
 *
 * `theme` is a Solid reactive store — components that read
 * `theme.bg` etc. in JSX will re-render when the theme changes.
 * `syntaxStyle` is a Solid signal accessor — call `syntaxStyle()`.
 */

import {
	RGBA,
	SyntaxStyle,
	type TerminalColors,
	type ThemeTokenStyle,
} from "@opentui/core";
import { createSignal } from "solid-js";
import { createStore, reconcile } from "solid-js/store";
import { loadUserTheme } from "./themes/loader";
import { buildDefaultTheme, buildSystemTheme, parseHex } from "./themes/system";
import type {
	ResolvedTheme,
	SyntaxPalette,
	ThemeConfig,
	ThemeTokens,
} from "./themes/types";

// ── Reactive theme store ────────────────────────────────────────────

function modalBackdropFromBg(bg: string): RGBA {
	const [r, g, b] = parseHex(bg);
	return RGBA.fromInts(r, g, b, 180);
}

const initialTheme = buildDefaultTheme();

let currentThemeConfig: ThemeConfig = {
	name: "system",
	tokens: { ...initialTheme.tokens },
	syntaxPalette: { ...initialTheme.syntaxPalette },
};

const [theme, setTheme] = createStore<ThemeTokens>({
	...initialTheme.tokens,
	modalBackdrop: modalBackdropFromBg(initialTheme.tokens.bg),
});
export { theme };

// ── Syntax style ────────────────────────────────────────────────────

function rgba(hex: string): RGBA {
	return RGBA.fromHex(hex);
}

function buildSyntaxStyle(p: SyntaxPalette): SyntaxStyle {
	const rules: ThemeTokenStyle[] = [
		{ scope: ["default"], style: { foreground: rgba(p.text) } },

		// ── Markdown ────────────────────────────────────────────────
		{
			scope: [
				"markup.heading",
				"markup.heading.1",
				"markup.heading.2",
				"markup.heading.3",
				"markup.heading.4",
				"markup.heading.5",
				"markup.heading.6",
			],
			style: { foreground: rgba(p.heading), bold: true },
		},
		{
			scope: ["markup.bold", "markup.strong"],
			style: { foreground: rgba(p.bold), bold: true },
		},
		{
			scope: ["markup.italic"],
			style: { foreground: rgba(p.italic), italic: true },
		},
		{ scope: ["markup.list"], style: { foreground: rgba(p.list) } },
		{
			scope: ["markup.quote"],
			style: { foreground: rgba(p.quote), italic: true },
		},
		{
			scope: ["markup.raw.block"],
			style: { foreground: rgba(p.codeBlock) },
		},
		{
			scope: ["markup.raw", "markup.raw.inline"],
			style: { foreground: rgba(p.codeInline) },
		},
		{
			scope: ["markup.link"],
			style: { foreground: rgba(p.link), underline: true },
		},
		{
			scope: ["markup.link.label"],
			style: { foreground: rgba(p.link), underline: true },
		},
		{
			scope: ["markup.link.url"],
			style: { foreground: rgba(p.link), underline: true },
		},
		{
			scope: ["markup.strikethrough"],
			style: { foreground: rgba(p.strikethrough) },
		},
		{ scope: ["conceal"], style: { foreground: rgba(p.conceal) } },
		{ scope: ["spell", "nospell"], style: { foreground: rgba(p.text) } },

		// ── Code / tree-sitter scopes ───────────────────────────────
		{
			scope: ["comment", "comment.documentation"],
			style: { foreground: rgba(p.comment), italic: true },
		},
		{
			scope: ["string", "symbol", "character", "character.special"],
			style: { foreground: rgba(p.string) },
		},
		{
			scope: ["string.escape", "string.regexp"],
			style: { foreground: rgba(p.escape) },
		},
		{
			scope: ["number", "boolean", "float", "constant"],
			style: { foreground: rgba(p.number) },
		},
		{
			scope: [
				"keyword",
				"keyword.import",
				"keyword.export",
				"keyword.directive",
				"keyword.modifier",
				"keyword.exception",
			],
			style: { foreground: rgba(p.keyword), italic: true },
		},
		{
			scope: [
				"keyword.return",
				"keyword.conditional",
				"keyword.repeat",
				"keyword.coroutine",
			],
			style: { foreground: rgba(p.keyword), italic: true },
		},
		{
			scope: ["keyword.type"],
			style: {
				foreground: rgba(p.keywordType),
				bold: true,
				italic: true,
			},
		},
		{
			scope: ["keyword.function", "function.method"],
			style: { foreground: rgba(p.function) },
		},
		{
			scope: [
				"operator",
				"keyword.operator",
				"punctuation.delimiter",
				"keyword.conditional.ternary",
			],
			style: { foreground: rgba(p.operator) },
		},
		{
			scope: [
				"variable",
				"variable.parameter",
				"function.method.call",
				"function.call",
				"property",
				"field",
				"parameter",
			],
			style: { foreground: rgba(p.variable) },
		},
		{
			scope: ["variable.member", "function", "constructor"],
			style: { foreground: rgba(p.member) },
		},
		{
			scope: [
				"variable.builtin",
				"type.builtin",
				"function.builtin",
				"module.builtin",
				"constant.builtin",
				"variable.super",
			],
			style: { foreground: rgba(p.builtin) },
		},
		{
			scope: ["type", "module", "namespace", "class", "type.definition"],
			style: { foreground: rgba(p.type) },
		},
		{
			scope: ["punctuation", "punctuation.bracket", "punctuation.special"],
			style: { foreground: rgba(p.punctuation) },
		},
		{ scope: ["tag"], style: { foreground: rgba(p.tag) } },
		{
			scope: ["tag.attribute"],
			style: { foreground: rgba(p.tagAttribute) },
		},
		{
			scope: ["tag.delimiter"],
			style: { foreground: rgba(p.tagDelimiter) },
		},
		{
			scope: ["attribute", "annotation"],
			style: { foreground: rgba(p.attribute) },
		},
		{ scope: ["label"], style: { foreground: rgba(p.label) } },
	];

	return SyntaxStyle.fromTheme(rules);
}

const [syntaxStyle, setSyntaxStyle] = createSignal<SyntaxStyle>(
	buildSyntaxStyle(initialTheme.syntaxPalette),
);
export { syntaxStyle };

// ── Theme resolution ────────────────────────────────────────────────

type PaletteSource = {
	getPalette(options?: { timeout?: number }): Promise<TerminalColors>;
};

let cachedRenderer: PaletteSource | null = null;
let cachedSystemTheme: ResolvedTheme | null = null;
let cachedSystemThemePromise: Promise<ResolvedTheme> | null = null;

function cloneResolvedTheme(resolved: ResolvedTheme): ResolvedTheme {
	return {
		tokens: { ...resolved.tokens },
		syntaxPalette: { ...resolved.syntaxPalette },
	};
}

export function getCurrentThemeConfig(): ThemeConfig {
	return {
		name: currentThemeConfig.name,
		tokens: { ...currentThemeConfig.tokens },
		syntaxPalette: { ...currentThemeConfig.syntaxPalette },
	};
}

async function resolveSystemThemeBase(): Promise<ResolvedTheme> {
	if (cachedSystemTheme) return cloneResolvedTheme(cachedSystemTheme);

	cachedSystemThemePromise ??= (async () => {
		if (cachedRenderer) {
			try {
				const termColors = await cachedRenderer.getPalette({ timeout: 2000 });
				return buildSystemTheme(termColors);
			} catch {
				// Terminal doesn't support palette queries — use xterm defaults
				return buildDefaultTheme();
			}
		}

		return buildDefaultTheme();
	})();

	try {
		cachedSystemTheme = await cachedSystemThemePromise;
		return cloneResolvedTheme(cachedSystemTheme);
	} finally {
		cachedSystemThemePromise = null;
	}
}

/**
 * Resolve a theme by name and apply it to the reactive theme store.
 *
 * The system theme is always resolved first as the base. User themes
 * from ~/.kit/themes/ are layered on top as partial overrides.
 *
 * On first call, pass the renderer so the system theme can query
 * the terminal palette. Subsequent calls reuse the cached renderer.
 */
export async function resolveAndApplyTheme(
	themeName: string,
	renderer?: PaletteSource,
): Promise<void> {
	if (renderer && renderer !== cachedRenderer) {
		cachedRenderer = renderer;
		cachedSystemTheme = null;
		cachedSystemThemePromise = null;
	}

	// Always resolve the system theme as the base
	let resolved = await resolveSystemThemeBase();

	// Layer user theme overrides on top of the system theme
	if (themeName !== "system") {
		const userDef = await loadUserTheme(themeName);
		if (userDef) {
			if (userDef.tokens) {
				resolved = {
					...resolved,
					tokens: { ...resolved.tokens, ...userDef.tokens },
				};
			}
			if (userDef.syntaxPalette) {
				resolved = {
					...resolved,
					syntaxPalette: {
						...resolved.syntaxPalette,
						...userDef.syntaxPalette,
					},
				};
			}
		}
	}

	currentThemeConfig = {
		name: themeName,
		tokens: { ...resolved.tokens },
		syntaxPalette: { ...resolved.syntaxPalette },
	};

	// Apply to reactive stores
	setTheme(
		reconcile({
			...resolved.tokens,
			modalBackdrop: modalBackdropFromBg(resolved.tokens.bg),
		}),
	);
	setSyntaxStyle(buildSyntaxStyle(resolved.syntaxPalette));
}
