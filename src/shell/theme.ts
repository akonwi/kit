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
import { KIT_SYNTAX_PALETTE, KIT_TOKENS } from "./themes/kit";
import { loadUserTheme } from "./themes/loader";
import { buildSystemTheme } from "./themes/system";
import type { SyntaxPalette, ThemeTokens } from "./themes/types";

// ── Reactive theme store ────────────────────────────────────────────

const [theme, setTheme] = createStore<ThemeTokens>({ ...KIT_TOKENS });
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
	buildSyntaxStyle(KIT_SYNTAX_PALETTE),
);
export { syntaxStyle };

// ── Theme resolution ────────────────────────────────────────────────

type PaletteSource = {
	getPalette(options?: { timeout?: number }): Promise<TerminalColors>;
};

let cachedRenderer: PaletteSource | null = null;

/**
 * Resolve a theme by name and apply it to the reactive theme store.
 * On first call, pass the renderer so the system theme can query
 * the terminal palette. Subsequent calls reuse the cached renderer.
 */
export async function resolveAndApplyTheme(
	themeName: string,
	renderer?: PaletteSource,
): Promise<void> {
	if (renderer) cachedRenderer = renderer;

	let tokenOverrides: Partial<ThemeTokens> = {};
	let syntaxOverrides: Partial<SyntaxPalette> = {};

	if (themeName === "system" && cachedRenderer) {
		try {
			const termColors = await cachedRenderer.getPalette({ timeout: 2000 });
			const systemDef = buildSystemTheme(termColors);
			tokenOverrides = systemDef.tokens ?? {};
			syntaxOverrides = systemDef.syntaxPalette ?? {};
		} catch {
			// Terminal doesn't support palette queries — keep kit defaults
		}
	} else if (themeName !== "kit") {
		const userDef = await loadUserTheme(themeName);
		if (userDef) {
			tokenOverrides = userDef.tokens ?? {};
			syntaxOverrides = userDef.syntaxPalette ?? {};
		}
	}

	// Update reactive store — components re-render automatically
	setTheme(reconcile({ ...KIT_TOKENS, ...tokenOverrides }));

	// Rebuild syntax style signal
	const mergedPalette = { ...KIT_SYNTAX_PALETTE, ...syntaxOverrides };
	setSyntaxStyle(buildSyntaxStyle(mergedPalette));
}
