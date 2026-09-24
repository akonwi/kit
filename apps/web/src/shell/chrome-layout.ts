import type {
	ChromeContribution,
	ChromeContributionSide,
} from "./chrome-contributions";

export const CHROME_SEPARATOR = " · ";
const REGION_GAP_WIDTH = 1;

export function terminalTextWidth(text: string): number {
	return Bun.stringWidth(text);
}

export function truncateEnd(text: string, maxWidth: number): string {
	if (maxWidth <= 0) return "";
	if (terminalTextWidth(text) <= maxWidth) return text;
	if (maxWidth === 1) return "…";

	let result = "";
	for (const char of text) {
		if (terminalTextWidth(`${result}${char}…`) > maxWidth) break;
		result += char;
	}
	return `${result}…`;
}

export function transcriptContextProgressColumns(input: {
	shellWidth: number;
	transcriptWidth: number;
	percent: number;
}): number {
	const available = Math.max(
		0,
		Math.min(input.shellWidth - 2, input.transcriptWidth - 1),
	);
	const percent = Math.max(0, Math.min(100, input.percent));
	return Math.min(available, Math.round((percent / 100) * available));
}

export function truncateStart(text: string, maxWidth: number): string {
	if (maxWidth <= 0) return "";
	if (terminalTextWidth(text) <= maxWidth) return text;
	if (maxWidth === 1) return "…";

	let result = "";
	for (const char of Array.from(text).reverse()) {
		if (terminalTextWidth(`…${char}${result}`) > maxWidth) break;
		result = `${char}${result}`;
	}
	return `…${result}`;
}

function sideWidth(
	contributions: readonly ChromeContribution[],
	side: ChromeContributionSide,
): number {
	const items = contributions.filter(
		(contribution) => contribution.side === side,
	);
	if (items.length === 0) return 0;
	return (
		items.reduce(
			(total, contribution) =>
				total + terminalTextWidth(contribution.plainText),
			0,
		) +
		(items.length - 1) * terminalTextWidth(CHROME_SEPARATOR)
	);
}

export function chromeLayoutWidth(
	contributions: readonly ChromeContribution[],
): number {
	const leftWidth = sideWidth(contributions, "left");
	const rightWidth = sideWidth(contributions, "right");
	return (
		leftWidth +
		rightWidth +
		(leftWidth > 0 && rightWidth > 0 ? REGION_GAP_WIDTH : 0)
	);
}

export type PackedChromeContributions = {
	visible: ChromeContribution[];
	hidden: ChromeContribution[];
	overflowLabel: string | null;
};

function overflowContribution(label: string): ChromeContribution {
	return {
		id: "ShellChrome:overflow",
		content: [{ text: label }],
		plainText: label,
		side: "right",
	};
}

/**
 * Packs a registration-ordered prefix of standard contributions around
 * privileged shell content. Contributions are included whole or omitted.
 */
export function packChromeContributions(input: {
	availableWidth: number;
	privileged: readonly ChromeContribution[];
	standard: readonly ChromeContribution[];
}): PackedChromeContributions {
	const availableWidth = Math.max(0, input.availableWidth);
	const all = [...input.privileged, ...input.standard];
	if (chromeLayoutWidth(all) <= availableWidth) {
		return {
			visible: [...input.standard],
			hidden: [],
			overflowLabel: null,
		};
	}

	for (let count = input.standard.length; count >= 0; count -= 1) {
		const hiddenCount = input.standard.length - count;
		if (hiddenCount === 0) continue;
		const label = `… +${hiddenCount}`;
		const candidate = [
			...input.privileged,
			...input.standard.slice(0, count),
			overflowContribution(label),
		];
		if (chromeLayoutWidth(candidate) <= availableWidth) {
			return {
				visible: input.standard.slice(0, count),
				hidden: input.standard.slice(count),
				overflowLabel: label,
			};
		}
	}

	return {
		visible: [],
		hidden: [...input.standard],
		overflowLabel:
			chromeLayoutWidth([...input.privileged, overflowContribution("…")]) <=
			availableWidth
				? "…"
				: null,
	};
}
