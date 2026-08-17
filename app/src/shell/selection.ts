import type { RGBA, Selection } from "@opentui/core";
import { copyToClipboard } from "./clipboard";

type SelectionColor = string | RGBA | undefined;
type SelectionColorRenderable = {
	isDestroyed: boolean;
	selectionBg: SelectionColor;
	selectionFg: SelectionColor;
};
export type SelectionColorRestore = Map<
	SelectionColorRenderable,
	{ background: SelectionColor; foreground: SelectionColor }
>;

type Renderer = {
	getSelection(): { getSelectedText(): string } | null;
	clearSelection(): void;
};

function supportsSelectionColors(
	value: unknown,
): value is SelectionColorRenderable {
	return (
		typeof value === "object" &&
		value !== null &&
		"isDestroyed" in value &&
		"selectionBg" in value &&
		"selectionFg" in value
	);
}

export function applySelectionColors(
	selection: Selection,
	restore: SelectionColorRestore,
	background: string,
	foreground: string,
): void {
	for (const renderable of selection.selectedRenderables) {
		if (!supportsSelectionColors(renderable) || renderable.isDestroyed)
			continue;
		if (!restore.has(renderable)) {
			restore.set(renderable, {
				background: renderable.selectionBg,
				foreground: renderable.selectionFg,
			});
		}
		renderable.selectionBg = background;
		renderable.selectionFg = foreground;
	}
}

export function restoreSelectionColors(restore: SelectionColorRestore): void {
	for (const [renderable, colors] of restore) {
		if (renderable.isDestroyed) continue;
		renderable.selectionBg = colors.background;
		renderable.selectionFg = colors.foreground;
	}
	restore.clear();
}

export function formatSelectionAsQuote(
	text: string,
	prependNewline = false,
): string {
	const normalized = text.replace(/\r\n?/g, "\n").replace(/\n+$/, "");
	if (!normalized) return "";
	const quote = normalized
		.split("\n")
		.map((line) => `> ${line}`)
		.join("\n");
	return `${prependNewline ? "\n" : ""}${quote}\n\n`;
}

/** Copy a selection in focused modal content that cannot use the shell menu. */
export function copySelection(renderer: Renderer): boolean {
	const text = renderer.getSelection()?.getSelectedText();
	if (!text) return false;

	copyToClipboard(text).catch((error) => {
		console.error("clipboard copy failed:", error);
	});
	renderer.clearSelection();
	return true;
}
