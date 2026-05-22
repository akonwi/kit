/**
 * Split markdown text into pages at heading boundaries.
 * When headings are present, every heading starts a new page and content
 * stays attached to the nearest heading above it.
 * Nested headings inherit their immediate parent heading as section context.
 */

export type PagerSection = {
	/** The specific heading for this page (e.g. "### Setup") */
	title: string;
	/** Parent heading shown persistently across pages in this group (e.g. "## Architecture") */
	sectionTitle: string;
	/** Full markdown body for this page */
	body: string;
};

type HeadingChunk = {
	level: number;
	title: string;
	body: string;
};

function parseHeadingChunks(text: string): HeadingChunk[] {
	const normalized = text.replace(/\r\n/g, "\n").trim();
	if (!normalized) return [];

	const chunks: HeadingChunk[] = [];
	const rawChunks = normalized.split(/\n(?=#+\s+)/g);

	for (const raw of rawChunks) {
		const trimmed = raw.trim();
		if (!trimmed) continue;

		const match = trimmed.match(/^(#+)\s+(.*)/);
		if (match) {
			const level = match[1].length;
			const title = match[2].trim();
			chunks.push({ level, title, body: trimmed });
		} else {
			// Content before any heading — treat as level 0
			chunks.push({ level: 0, title: "Introduction", body: trimmed });
		}
	}

	return chunks;
}

export function splitSections(text: string): PagerSection[] {
	const chunks = parseHeadingChunks(text);
	if (chunks.length === 0) return [];

	const hasHeadings = chunks.some((chunk) => chunk.level > 0);
	if (!hasHeadings) {
		return fallbackParagraphSplit(text);
	}

	const headingStack: string[] = [];
	return chunks.map((chunk) => {
		if (chunk.level <= 0) {
			return {
				title: chunk.title,
				sectionTitle: "",
				body: chunk.body,
			};
		}

		headingStack.length = Math.max(0, chunk.level - 1);
		const parentTitle = headingStack[chunk.level - 2] ?? "";
		headingStack[chunk.level - 1] = chunk.title;

		return {
			title: chunk.title,
			sectionTitle: parentTitle,
			body: chunk.body,
		};
	});
}

function fallbackParagraphSplit(text: string): PagerSection[] {
	const paragraphs = text
		.replace(/\r\n/g, "\n")
		.trim()
		.split(/\n\n+/g)
		.map((p) => p.trim())
		.filter(Boolean);

	if (paragraphs.length < 2) {
		return [{ title: "Response", sectionTitle: "", body: text.trim() }];
	}

	return paragraphs.map((body, idx) => ({
		title: body.match(/^[^.!?\n]{4,70}/)?.[0]?.trim() || `Section ${idx + 1}`,
		sectionTitle: "",
		body,
	}));
}
