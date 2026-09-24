/**
 * Pager controller — manages section navigation and in-memory feedback drafts
 * for long assistant responses.
 */

import { createMemo, createSignal } from "solid-js";
import type { AgentMessage } from "../../runtime/agent";
import { renderTemplate } from "../../shell/templates";
import { type PagerSection, splitSections } from "./split-sections";

const AUTO_PAGE_MIN_OVERFLOW_ROWS = 8;
const AUTO_PAGE_VIEWPORT_MULTIPLIER = 1.35;
const TRANSCRIPT_HORIZONTAL_CHROME = 4;
const MIN_WRAP_WIDTH = 20;

export type PagerViewport = {
	width: number;
	height: number;
};

export type PagerDraftSnapshot = {
	sourceId: string;
	title: string;
	sections: PagerSection[];
	currentIndex: number;
	notes: Map<number, string>;
};

function extractAssistantText(msg: AgentMessage): string {
	const content: unknown = (msg as { content?: unknown }).content;
	if (typeof content === "string") return content;
	if (!Array.isArray(content)) return "";
	return content
		.map((block) => {
			if (!block || typeof block !== "object") return "";
			const b = block as Record<string, unknown>;
			if (b.type === "text" && typeof b.text === "string") return b.text;
			return "";
		})
		.filter(Boolean)
		.join("\n")
		.trim();
}

function assistantSourceId(message: AgentMessage, index: number): string {
	const tagged = message as AgentMessage & {
		id?: unknown;
		turnId?: unknown;
	};
	if (typeof tagged.id === "string" && tagged.id.length > 0) {
		return `message:${tagged.id}`;
	}
	if (typeof tagged.turnId === "string" && tagged.turnId.length > 0) {
		return `turn:${tagged.turnId}:assistant:${index}`;
	}
	return `assistant:${index}:${message.timestamp}`;
}

function estimateWrappedRows(text: string, viewportWidth: number): number {
	const usableWidth = Math.max(
		MIN_WRAP_WIDTH,
		viewportWidth - TRANSCRIPT_HORIZONTAL_CHROME,
	);
	const normalized = text.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
	return normalized.split("\n").reduce((total, rawLine) => {
		const expandedLine = rawLine.replace(/\t/g, "    ");
		const visualLength = Array.from(expandedLine).length;
		return total + Math.max(1, Math.ceil(visualLength / usableWidth));
	}, 0);
}

function getAutoPageThreshold(viewportHeight: number): number {
	return Math.max(
		viewportHeight + AUTO_PAGE_MIN_OVERFLOW_ROWS,
		Math.ceil(viewportHeight * AUTO_PAGE_VIEWPORT_MULTIPLIER),
	);
}

function shouldAutoPage(text: string, viewport: PagerViewport | null): boolean {
	if (!viewport) return false;
	if (viewport.width <= 0 || viewport.height <= 0) return false;
	const estimatedRows = estimateWrappedRows(text, viewport.width);
	return estimatedRows >= getAutoPageThreshold(viewport.height);
}

export function formatPagerFeedbackMessage(
	sections: PagerSection[],
	notes: Map<number, string>,
): string | null {
	const blocks: string[] = [];

	sections.forEach((section, idx) => {
		const note = notes.get(idx)?.trim();
		if (!note) return;
		const label = section.sectionTitle
			? `${section.sectionTitle}: ${section.title}`
			: section.title;
		blocks.push(`## ${label}\n${note}`);
	});

	if (blocks.length === 0) return null;

	return renderTemplate("pager-feedback", {
		content: blocks.join("\n\n"),
	});
}

export function createPagerController() {
	const [sections, setSections] = createSignal<PagerSection[]>([]);
	const [currentIndex, setCurrentIndex] = createSignal(0);
	const [notes, setNotes] = createSignal<Map<number, string>>(new Map());
	const [active, setActive] = createSignal(false);
	const [title, setTitle] = createSignal("");

	const currentSection = createMemo(() => {
		const s = sections();
		const idx = currentIndex();
		return idx >= 0 && idx < s.length ? s[idx] : null;
	});

	// Resolves the Promise returned by activateWithContent when the pager closes.
	let pendingClose: (() => void) | null = null;

	// The latest paged response remains in memory until its feedback attachment
	// is consumed/removed, a new response replaces it, or the session changes.
	let sourceId: string | null = null;
	let draftGeneration = 0;
	let manualSourceId = 0;

	function activate(
		text: string,
		nextSourceId: string,
		pageTitle?: string,
	): boolean {
		const result = splitSections(text);
		if (result.length === 0) return false;

		const restoringDraft = sourceId === nextSourceId;
		if (!restoringDraft) draftGeneration += 1;
		sourceId = nextSourceId;
		setSections(result);
		setTitle(pageTitle ?? result[0]?.title ?? "");
		setCurrentIndex((index) =>
			restoringDraft ? Math.min(index, result.length - 1) : 0,
		);
		if (!restoringDraft) setNotes(new Map());
		setActive(true);
		return true;
	}

	/**
	 * Activate the pager with arbitrary markdown content.
	 * Returns a Promise that resolves when the user closes the pager —
	 * use this from agent tools so the tool awaits user interaction.
	 */
	function activateWithContent(
		text: string,
		pageTitle?: string,
	): Promise<void> {
		manualSourceId += 1;
		if (!activate(text, `manual:${manualSourceId}`, pageTitle)) {
			return Promise.resolve();
		}

		return new Promise<void>((resolve) => {
			pendingClose = resolve;
		});
	}

	/**
	 * Open the pager for the last assistant message, regardless of size.
	 * Returns true if the pager was activated.
	 */
	function tryActivate(messages: AgentMessage[]): boolean {
		for (let i = messages.length - 1; i >= 0; i--) {
			const msg = messages[i];
			if (msg.role !== "assistant") continue;

			const text = extractAssistantText(msg);
			if (!text) break;

			return activate(text, assistantSourceId(msg, i));
		}
		return false;
	}

	/**
	 * Auto-open the pager when the last assistant response substantially
	 * overflows the visible transcript viewport.
	 */
	function tryAutoActivate(
		messages: AgentMessage[],
		viewport: PagerViewport | null,
	): boolean {
		for (let i = messages.length - 1; i >= 0; i--) {
			const msg = messages[i];
			if (msg.role !== "assistant") continue;

			const text = extractAssistantText(msg);
			if (!text) break;
			if (!shouldAutoPage(text, viewport)) break;

			return activate(text, assistantSourceId(msg, i));
		}
		return false;
	}

	// Scroll delegate — PagerModal binds its scrollbox ref here.
	let scrollDelegate: { scrollBy: (delta: number) => void } | null = null;

	function setScrollDelegate(
		delegate: { scrollBy: (delta: number) => void } | null,
	) {
		scrollDelegate = delegate;
	}

	function scrollUp() {
		scrollDelegate?.scrollBy(-3);
	}

	function scrollDown() {
		scrollDelegate?.scrollBy(3);
	}

	function closeView() {
		setActive(false);
		scrollDelegate = null;
		const resolve = pendingClose;
		pendingClose = null;
		resolve?.();
	}

	function reopen(): boolean {
		if (sections().length === 0) return false;
		setActive(true);
		return true;
	}

	function clearDraft(expectedGeneration?: number): boolean {
		if (
			expectedGeneration !== undefined &&
			expectedGeneration !== draftGeneration
		) {
			return false;
		}
		closeView();
		setSections([]);
		setNotes(new Map());
		setCurrentIndex(0);
		setTitle("");
		sourceId = null;
		draftGeneration += 1;
		return true;
	}

	function nextSection() {
		const max = sections().length - 1;
		if (currentIndex() < max) setCurrentIndex(currentIndex() + 1);
	}

	function prevSection() {
		if (currentIndex() > 0) setCurrentIndex(currentIndex() - 1);
	}

	function setNote(index: number, text: string) {
		setNotes((prev) => {
			const next = new Map(prev);
			const trimmed = text.trim();
			if (trimmed) {
				next.set(index, trimmed);
			} else {
				next.delete(index);
			}
			return next;
		});
	}

	function getNoteCount(): number {
		return Array.from(notes().values()).filter((n) => n.trim().length > 0)
			.length;
	}

	function getFeedbackMessage(): string | null {
		return formatPagerFeedbackMessage(sections(), notes());
	}

	function getDraftSnapshot(): PagerDraftSnapshot | null {
		if (!sourceId) return null;
		return {
			sourceId,
			title: title(),
			sections: sections().map((section) => ({ ...section })),
			currentIndex: currentIndex(),
			notes: new Map(notes()),
		};
	}

	function restoreDraft(snapshot: PagerDraftSnapshot): void {
		closeView();
		sourceId = snapshot.sourceId;
		draftGeneration += 1;
		setTitle(snapshot.title);
		setSections(snapshot.sections.map((section) => ({ ...section })));
		setCurrentIndex(
			Math.min(
				snapshot.currentIndex,
				Math.max(0, snapshot.sections.length - 1),
			),
		);
		setNotes(new Map(snapshot.notes));
	}

	return {
		get active() {
			return active();
		},
		get title() {
			return title();
		},
		get sections() {
			return sections();
		},
		get currentIndex() {
			return currentIndex();
		},
		get currentSection() {
			return currentSection();
		},
		get notes() {
			return notes();
		},
		getNoteCount,
		getFeedbackMessage,
		getDraftSnapshot,
		restoreDraft,
		get draftGeneration() {
			return draftGeneration;
		},
		activateWithContent,
		tryActivate,
		tryAutoActivate,
		closeView,
		reopen,
		clearDraft,
		nextSection,
		prevSection,
		setNote,
		setScrollDelegate,
		scrollUp,
		scrollDown,
	};
}

export type PagerController = ReturnType<typeof createPagerController>;
