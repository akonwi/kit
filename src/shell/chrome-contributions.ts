export type ChromeContributionSide = "left" | "right";

export type ChromeTextStyle = {
	fg?: string;
	bg?: string;
	bold?: boolean;
	dim?: boolean;
	italic?: boolean;
	underline?: boolean;
	strikethrough?: boolean;
};

export type ChromeTextSegment = {
	text: string;
	style?: ChromeTextStyle;
};

export type ChromeTextContent =
	| string
	| ChromeTextSegment
	| readonly (string | ChromeTextSegment)[];

export type ChromeContribution = {
	id: string;
	content: ChromeTextSegment[];
	plainText: string;
	side: ChromeContributionSide;
	onClick?: () => void | Promise<void>;
};

export type ChromeContributionInput = {
	id: string;
	content: ChromeTextContent;
	side?: ChromeContributionSide;
	onClick?: () => void | Promise<void>;
};

function sanitizeText(text: string): string {
	let sanitized = "";
	let replacedControlCharacter = false;
	for (const char of text) {
		const code = char.charCodeAt(0);
		if (code < 32 || code === 127) {
			if (!replacedControlCharacter) sanitized += " ";
			replacedControlCharacter = true;
			continue;
		}
		sanitized += char;
		replacedControlCharacter = false;
	}
	return sanitized;
}

function sanitizeStyle(
	style: ChromeTextStyle | undefined,
): ChromeTextStyle | undefined {
	if (!style) return undefined;
	const next: ChromeTextStyle = {};
	if (typeof style.fg === "string" && style.fg) next.fg = style.fg;
	if (typeof style.bg === "string" && style.bg) next.bg = style.bg;
	if (style.bold) next.bold = true;
	if (style.dim) next.dim = true;
	if (style.italic) next.italic = true;
	if (style.underline) next.underline = true;
	if (style.strikethrough) next.strikethrough = true;
	return Object.keys(next).length > 0 ? next : undefined;
}

function toSegment(input: string | ChromeTextSegment): ChromeTextSegment {
	if (typeof input === "string") return { text: sanitizeText(input) };
	return {
		text: sanitizeText(input.text),
		style: sanitizeStyle(input.style),
	};
}

function trimBoundaryWhitespace(
	segments: ChromeTextSegment[],
): ChromeTextSegment[] {
	const next = segments
		.map((segment) => ({ ...segment }))
		.filter((segment) => segment.text.length > 0);
	const firstTextIndex = next.findIndex(
		(segment) => segment.text.trim().length > 0,
	);
	if (firstTextIndex < 0) return [];
	for (let i = 0; i < firstTextIndex; i += 1) next[i].text = "";
	next[firstTextIndex].text = next[firstTextIndex].text.replace(/^\s+/, "");

	let lastTextIndex = next.length - 1;
	while (lastTextIndex >= 0 && next[lastTextIndex].text.trim().length === 0) {
		next[lastTextIndex].text = "";
		lastTextIndex -= 1;
	}
	if (lastTextIndex >= 0) {
		next[lastTextIndex].text = next[lastTextIndex].text.replace(/\s+$/, "");
	}

	return next.filter((segment) => segment.text.length > 0);
}

export function normalizeChromeTextContent(
	content: ChromeTextContent,
): ChromeTextSegment[] {
	const segments = Array.isArray(content)
		? (content as readonly (string | ChromeTextSegment)[]).map(toSegment)
		: [toSegment(content as string | ChromeTextSegment)];
	return trimBoundaryWhitespace(segments);
}

export function getPlainChromeText(
	content: readonly ChromeTextSegment[],
): string {
	return content.map((segment) => segment.text).join("");
}

export function createChromeTextContent(text: string): ChromeTextSegment[] {
	return normalizeChromeTextContent(text);
}

export function createChromeContributionsController() {
	let contributions: ChromeContribution[] = [];
	const hiddenContributions = new Map<string, Set<symbol>>();
	const listeners = new Set<() => void>();

	function notify() {
		for (const listener of listeners) listener();
	}

	function setContribution(input: ChromeContributionInput) {
		const content = normalizeChromeTextContent(input.content);
		const plainText = getPlainChromeText(content);
		if (!plainText.trim()) {
			clearContribution(input.id);
			return;
		}
		const next = {
			id: input.id,
			content,
			plainText,
			side: input.side ?? "right",
			onClick: input.onClick,
		};
		const existingIndex = contributions.findIndex(
			(contribution) => contribution.id === input.id,
		);

		if (existingIndex >= 0) {
			contributions = [
				...contributions.slice(0, existingIndex),
				next,
				...contributions.slice(existingIndex + 1),
			];
		} else {
			contributions = [...contributions, next];
		}

		notify();
	}

	function clearContribution(id: string) {
		const next = contributions.filter((contribution) => contribution.id !== id);
		if (next.length === contributions.length) return;
		contributions = next;
		notify();
	}

	function clearNamespace(namespace: string) {
		const prefix = `${namespace}:`;
		const next = contributions.filter(
			(contribution) => !contribution.id.startsWith(prefix),
		);
		if (next.length === contributions.length) return;
		contributions = next;
		notify();
	}

	function hideContribution(id: string): () => void {
		const token = Symbol(id);
		const tokens = hiddenContributions.get(id) ?? new Set<symbol>();
		tokens.add(token);
		hiddenContributions.set(id, tokens);
		notify();
		return () => {
			const current = hiddenContributions.get(id);
			if (!current?.delete(token)) return;
			if (current.size === 0) hiddenContributions.delete(id);
			notify();
		};
	}

	function isHidden(id: string): boolean {
		return (hiddenContributions.get(id)?.size ?? 0) > 0;
	}

	function getContributions(
		side?: ChromeContributionSide,
	): ChromeContribution[] {
		const visible = contributions.filter(
			(contribution) => !isHidden(contribution.id),
		);
		return side
			? visible.filter((contribution) => contribution.side === side)
			: visible;
	}

	function subscribe(listener: () => void): () => void {
		listeners.add(listener);
		return () => listeners.delete(listener);
	}

	return {
		setContribution,
		clearContribution,
		clearNamespace,
		hideContribution,
		isHidden,
		getContributions,
		subscribe,
	};
}

export type ChromeContributionsController = ReturnType<
	typeof createChromeContributionsController
>;
