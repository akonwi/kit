/** @jsxImportSource solid-js */
import {
	type DiffLineAnnotation,
	FileDiff,
	type FileDiffMetadata,
	parsePatchFiles,
	type SelectedLineRange,
	type ThemeTypes,
} from "@pierre/diffs";
import { createEffect, createSignal, type JSX, onCleanup } from "solid-js";
import { readBrowserTheme } from "./browser-theme";
import type { RemoteReviewFile } from "./remote-services";

export type ReviewDiffLayout = "unified" | "split";
export type ReviewDiffOverflow = "scroll" | "wrap";
export type ReviewLineRange = SelectedLineRange;

export type ReviewNote = {
	id: string;
	range: ReviewLineRange;
	comment: string;
};

export type ReviewNoteDraft = {
	range: ReviewLineRange;
	noteId?: string;
	initialComment: string;
};

type ReviewAnnotation = {
	kind: "note" | "composer";
	note: ReviewNote | null;
	draft: ReviewNoteDraft | null;
};

const SELECTION_CSS = `
:host {
  --diffs-selection-color-override: var(--kit-attachment-text);
}
`;

function themeType(): ThemeTypes {
	return readBrowserTheme();
}

/**
 * The web client deliberately blocks inline styles in its CSP. Pierre emits
 * generated grid placement and token styles as style attributes/elements, so
 * re-apply them through the CSSOM instead of weakening the page policy.
 */
function applyPierreDynamicStyles(container: HTMLElement): void {
	const root = container.shadowRoot;
	if (!root) return;
	for (const element of Array.from(
		root.querySelectorAll<HTMLElement>("[style]"),
	)) {
		const cssText = element.getAttribute("style");
		if (!cssText) continue;
		// Parsing the original attribute happens under CSP and leaves the CSSOM
		// declaration empty. Reassigning through the DOM API applies it safely.
		element.style.cssText = "";
		element.style.cssText = cssText;
	}
	const generatedSheets: CSSStyleSheet[] = [];
	for (const style of Array.from(root.querySelectorAll("style"))) {
		const sheet = new CSSStyleSheet();
		sheet.replaceSync(style.textContent ?? "");
		generatedSheets.push(sheet);
	}
	const coreSheet = root.adoptedStyleSheets[0];
	root.adoptedStyleSheets = [
		...(coreSheet ? [coreSheet] : []),
		...generatedSheets,
	];
}

function rangeSide(range: ReviewLineRange): "additions" | "deletions" {
	return range.endSide ?? range.side ?? "additions";
}

function rangeEnd(range: ReviewLineRange): number {
	if ((range.endSide ?? range.side) === range.side) {
		return Math.max(range.start, range.end);
	}
	return range.end;
}

function annotationPlacement(
	metadata: ReviewAnnotation,
): DiffLineAnnotation<ReviewAnnotation> {
	const range = metadata.note?.range ?? metadata.draft?.range;
	if (!range) throw new Error("Review annotation is missing its line range");
	return {
		side: rangeSide(range),
		lineNumber: rangeEnd(range),
		metadata,
	};
}

function button(label: string, onClick: () => void): HTMLButtonElement {
	const element = document.createElement("button");
	element.type = "button";
	element.textContent = label;
	element.addEventListener("click", (event) => {
		event.stopPropagation();
		onClick();
	});
	return element;
}

function deleteButton(onClick: () => void): HTMLButtonElement {
	const element = button("", onClick);
	element.className = "kit-review-note-delete";
	element.setAttribute("aria-label", "Delete note");
	element.title = "Delete note";
	const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
	svg.setAttribute("viewBox", "0 0 24 24");
	svg.setAttribute("fill", "none");
	svg.setAttribute("stroke", "currentColor");
	svg.setAttribute("stroke-width", "2");
	svg.setAttribute("stroke-linecap", "round");
	svg.setAttribute("stroke-linejoin", "round");
	svg.setAttribute("aria-hidden", "true");
	for (const data of [
		"M3 6h18",
		"M8 6V4h8v2",
		"M19 6l-1 14H6L5 6",
		"M10 11v6",
		"M14 11v6",
	]) {
		const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
		path.setAttribute("d", data);
		svg.append(path);
	}
	element.append(svg);
	return element;
}

function findFileDiff(file: RemoteReviewFile): FileDiffMetadata {
	const parsed = parsePatchFiles(file.file.rawPatch, file.file.id);
	const files = parsed.flatMap((patch) => patch.files);
	const match = files.find(
		(candidate) =>
			candidate.name === file.file.path ||
			candidate.prevName === file.file.prevPath,
	);
	if (!match) throw new Error("Could not render this file diff");
	return match;
}

export function PierreDiff(props: {
	file: RemoteReviewFile;
	layout: ReviewDiffLayout;
	overflow: ReviewDiffOverflow;
	notes: ReviewNote[];
	draft: ReviewNoteDraft | null;
	onSelectRange(range: ReviewLineRange | null): void;
	onSaveNote(draft: ReviewNoteDraft, comment: string): void;
	onEditNote(note: ReviewNote): void;
	onDraftChange(comment: string): void;
	onDeleteNote(noteId: string): void;
	onCancelNote(): void;
}): JSX.Element {
	let host: HTMLDivElement | undefined;
	const [activeRenderer, setActiveRenderer] =
		createSignal<FileDiff<ReviewAnnotation>>();

	createEffect(() => {
		const file = props.file;
		const layout = props.layout;
		const overflow = props.overflow;
		if (!host) return;
		host.replaceChildren();
		const renderer = new FileDiff<ReviewAnnotation>({
			disableFileHeader: true,
			disableVirtualizationBuffers: true,
			diffStyle: layout,
			enableLineSelection: true,
			lineHoverHighlight: "number",
			onLineSelectionEnd: props.onSelectRange,
			overflow,
			themeType: themeType(),
			unsafeCSS: SELECTION_CSS,
			renderAnnotation: (annotation) => {
				const metadata = annotation.metadata;
				const wrapper = document.createElement("div");
				wrapper.className = "kit-review-annotation";
				const note = metadata.note;
				if (metadata.kind === "note" && note) {
					const body = document.createElement("div");
					body.className = "kit-review-note-body";
					const edit = button("", () => props.onEditNote(note));
					edit.className = "kit-review-note-edit";
					edit.setAttribute("aria-label", "Edit note");
					const comment = document.createElement("p");
					comment.className = "kit-review-note-comment";
					comment.textContent = note.comment;
					edit.append(comment);
					body.append(
						edit,
						deleteButton(() => props.onDeleteNote(note.id)),
					);
					wrapper.append(body);
					return wrapper;
				}

				const noteDraft = metadata.draft;
				if (!noteDraft) return;
				const composer = document.createElement("div");
				composer.className = "kit-review-composer";
				const textarea = document.createElement("textarea");
				textarea.value = noteDraft.initialComment;
				textarea.placeholder = "Leave a review note…";
				textarea.setAttribute("aria-label", "Review note");
				const actions = document.createElement("div");
				actions.className = "kit-review-composer-actions";
				const cancel = button("Cancel", props.onCancelNote);
				const save = button(noteDraft.noteId ? "Save" : "Add", () =>
					props.onSaveNote(noteDraft, textarea.value),
				);
				save.dataset.primary = "";
				const updateSaveState = () => {
					save.disabled = textarea.value.trim().length === 0;
				};
				textarea.addEventListener("input", () => {
					updateSaveState();
					props.onDraftChange(textarea.value);
				});
				textarea.addEventListener("keydown", (event) => {
					if (event.key === "Escape") {
						event.preventDefault();
						event.stopPropagation();
						props.onCancelNote();
					} else if (
						event.key === "Enter" &&
						(event.metaKey || event.ctrlKey) &&
						!save.disabled
					) {
						event.preventDefault();
						props.onSaveNote(noteDraft, textarea.value);
					}
				});
				updateSaveState();
				actions.append(cancel, save);
				composer.append(textarea, actions);
				wrapper.append(composer);
				queueMicrotask(() => {
					textarea.focus({ preventScroll: true });
					wrapper.scrollIntoView({ block: "nearest" });
				});
				return wrapper;
			},
		});
		let observer: MutationObserver | undefined;
		try {
			renderer.render({
				fileDiff: findFileDiff(file),
				containerWrapper: host,
			});
			setActiveRenderer(renderer);
			const container = host.querySelector<HTMLElement>("diffs-container");
			if (container) {
				applyPierreDynamicStyles(container);
				const root = container.shadowRoot;
				if (root) {
					let queued = false;
					observer = new MutationObserver(() => {
						if (queued) return;
						queued = true;
						queueMicrotask(() => {
							queued = false;
							applyPierreDynamicStyles(container);
						});
					});
					observer.observe(root, {
						childList: true,
						characterData: true,
						subtree: true,
					});
				}
			}
		} catch (cause) {
			host.textContent = cause instanceof Error ? cause.message : String(cause);
		}
		onCleanup(() => {
			observer?.disconnect();
			if (activeRenderer() === renderer) setActiveRenderer(undefined);
			renderer.cleanUp();
		});
	});

	createEffect(() => {
		const renderer = activeRenderer();
		const notes = props.notes;
		const draft = props.draft;
		if (!renderer) return;
		const lineAnnotations: DiffLineAnnotation<ReviewAnnotation>[] = notes
			.filter((note) => note.id !== draft?.noteId)
			.map((note) => annotationPlacement({ kind: "note", note, draft: null }));
		if (draft) {
			lineAnnotations.push(
				annotationPlacement({ kind: "composer", note: null, draft }),
			);
		}
		renderer.setLineAnnotations(lineAnnotations);
		renderer.rerender();
		renderer.setSelectedLines(draft?.range ?? null);
	});

	return <div ref={host} class="pierre-diff-host" />;
}
