import { FileDiff, parsePatchFiles } from "@pierre/diffs";

type DiffSide = "additions" | "deletions";

type BrowserTheme = {
	bg: string;
	bgSurface: string;
	bgMuted: string;
	bgAccent: string;
	borderDefault: string;
	borderAccent: string;
	textPrimary: string;
	textSecondary: string;
	textMuted: string;
	userText: string;
	toolText: string;
	warningText: string;
	errorText: string;
};

type CodeReviewBrowserHunk = {
	id: string;
	header: string;
	context: string;
	changeCount: number;
	additionStart: number;
	additionCount: number;
	deletionStart: number;
	deletionCount: number;
};

type CodeReviewBrowserFile = {
	id: string;
	path: string;
	prevPath?: string;
	status: string;
	filetype?: string;
	changeCount: number;
	hunkCount: number;
	rawPatch: string;
	hunks: CodeReviewBrowserHunk[];
};

type CodeReviewBrowserState = {
	sessionId: string;
	sessionName: string | null;
	cwd: string;
	model: string | null;
	lastUpdatedAt: string;
	review: {
		files: CodeReviewBrowserFile[];
		totalFileCount: number;
		totalHunkCount: number;
		totalChangeCount: number;
		error: string | null;
	};
};

type RangeCommentSubmission = {
	side: DiffSide;
	startLine: number;
	endLine: number;
	comment: string;
};

type CodeReviewSubmission = {
	submittedAt: string;
	files: Array<{
		path: string;
		fileComment: string;
		ranges: RangeCommentSubmission[];
	}>;
};

type CodeReviewClientMessage =
	| { type: "ready" }
	| { type: "request_state" }
	| { type: "refresh_diff" }
	| { type: "submit_review_state"; review: CodeReviewSubmission };

type CodeReviewServerMessage =
	| { type: "connected"; sessionUrl: string }
	| { type: "state"; state: CodeReviewBrowserState; reason: string }
	| {
			type: "submission_saved";
			submittedAt: string;
			fileCount: number;
			commentCount: number;
	  };

type SelectedRange = {
	path: string;
	side: DiffSide;
	startLine: number;
	endLine: number;
	anchorLine: number;
};

type ReviewDraft = {
	fileComments: Record<string, string>;
	rangeComments: Record<string, string>;
};

type RangeAnnotationMetadata = {
	rangeKey: string;
	path: string;
	side: DiffSide;
	startLine: number;
	endLine: number;
};

declare global {
	interface Window {
		__KIT_CODE_REVIEW_BOOTSTRAP__?: {
			theme: BrowserTheme;
		};
	}
}

const bootstrap = window.__KIT_CODE_REVIEW_BOOTSTRAP__;
if (!bootstrap) throw new Error("Missing Kit code review bootstrap data.");

const theme = bootstrap.theme;
const root = document.getElementById("app");
if (!root) throw new Error("Missing code review root element.");

for (const [key, value] of Object.entries(theme)) {
	document.documentElement.style.setProperty(`--kit-${key}`, value);
}

document.title = "Kit Code Review";
document.body.style.margin = "0";
document.body.style.background = theme.bg;
document.body.style.color = theme.textPrimary;

const styles = [
	":root { color-scheme: dark; font-family: Inter, ui-sans-serif, system-ui, sans-serif; }",
	"* { box-sizing: border-box; }",
	"body { min-height: 100vh; background: radial-gradient(circle at top, color-mix(in srgb, var(--kit-bgAccent) 50%, var(--kit-bg) 50%), var(--kit-bg) 60%); }",
	"button, textarea { font: inherit; }",
	"button { border: 1px solid var(--kit-borderDefault); background: var(--kit-bgAccent); color: var(--kit-textPrimary); border-radius: 10px; padding: 9px 12px; cursor: pointer; }",
	"button:hover { border-color: var(--kit-borderAccent); }",
	"button:disabled { opacity: 0.6; cursor: default; }",
	"textarea { width: 100%; border: 1px solid color-mix(in srgb, var(--kit-borderDefault) 92%, transparent); border-radius: 12px; padding: 12px; background: color-mix(in srgb, var(--kit-bg) 82%, var(--kit-bgSurface) 18%); color: var(--kit-textPrimary); resize: vertical; }",
	"#app { padding: 24px; }",
	".layout { max-width: 1440px; margin: 0 auto; display: grid; gap: 16px; }",
	".card { background: color-mix(in srgb, var(--kit-bgSurface) 92%, transparent); border: 1px solid color-mix(in srgb, var(--kit-borderDefault) 90%, transparent); border-radius: 16px; box-shadow: 0 18px 48px rgba(0, 0, 0, 0.28); }",
	".toolbar { padding: 14px 16px; }",
	".accordion-shell { min-height: 78vh; max-height: 78vh; overflow: auto; padding: 14px; }",
	".muted { color: var(--kit-textMuted); }",
	".row { display: flex; justify-content: space-between; gap: 10px; align-items: center; }",
	".action-row { display: flex; justify-content: space-between; gap: 10px; align-items: center; flex-wrap: wrap; }",
	".header-stack { display: grid; gap: 6px; }",
	".header-stack h2, .header-stack h3 { margin: 0; }",
	".status-pill { display: inline-flex; align-items: center; gap: 8px; padding: 6px 10px; border-radius: 999px; background: color-mix(in srgb, var(--kit-bgMuted) 75%, transparent); border: 1px solid var(--kit-borderDefault); font-size: 13px; }",
	".review-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 10px; }",
	".review-row { border: 1px solid color-mix(in srgb, var(--kit-borderDefault) 90%, transparent); border-radius: 14px; overflow: hidden; background: color-mix(in srgb, var(--kit-bgMuted) 34%, transparent); }",
	".file-button { width: 100%; text-align: left; background: color-mix(in srgb, var(--kit-bgMuted) 55%, transparent); border: 0; border-radius: 0; padding: 12px; display: grid; gap: 8px; }",
	".file-button.selected { background: color-mix(in srgb, var(--kit-bgAccent) 28%, var(--kit-bgMuted) 72%); }",
	".file-body { padding: 12px; display: grid; gap: 12px; border-top: 1px solid color-mix(in srgb, var(--kit-borderDefault) 88%, transparent); }",
	".path { display: inline-flex; align-items: center; gap: 8px; min-width: 0; }",
	".path strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }",
	".badge { display: inline-flex; align-items: center; justify-content: center; min-width: 22px; height: 22px; padding: 0 8px; border-radius: 999px; font-size: 12px; font-weight: 700; }",
	".badge.add { background: color-mix(in srgb, var(--kit-toolText) 24%, transparent); color: var(--kit-toolText); }",
	".badge.delete { background: color-mix(in srgb, var(--kit-errorText) 24%, transparent); color: var(--kit-errorText); }",
	".badge.modify { background: color-mix(in srgb, var(--kit-userText) 24%, transparent); color: var(--kit-userText); }",
	".badge.rename { background: color-mix(in srgb, var(--kit-warningText) 24%, transparent); color: var(--kit-warningText); }",
	".comment-chip { display: inline-flex; align-items: center; gap: 6px; padding: 3px 8px; border-radius: 999px; font-size: 12px; background: color-mix(in srgb, var(--kit-bgAccent) 30%, transparent); color: var(--kit-textSecondary); }",
	".file-comment-card { padding: 12px; border-radius: 12px; background: color-mix(in srgb, var(--kit-bgMuted) 45%, transparent); border: 1px solid color-mix(in srgb, var(--kit-borderDefault) 90%, transparent); display: grid; gap: 8px; }",
	".file-comment-card label, .diff-annotation label { font-size: 13px; color: var(--kit-textSecondary); font-weight: 600; }",
	".patch-shell { min-height: 0; overflow: auto; border-radius: 12px; background: color-mix(in srgb, var(--kit-bg) 72%, var(--kit-bgSurface) 28%); border: 1px solid color-mix(in srgb, var(--kit-borderDefault) 90%, transparent); padding: 12px; }",
	".diff-host { min-height: 100%; }",
	".empty { padding: 24px; border-radius: 12px; border: 1px dashed color-mix(in srgb, var(--kit-borderDefault) 90%, transparent); color: var(--kit-textMuted); background: color-mix(in srgb, var(--kit-bgMuted) 42%, transparent); }",
	".diff-host diffs-container { display: block; }",
	".diff-host svg[data-icon-sprite] { display: none; }",
	".diff-host pre { max-height: none; padding: 0; margin: 0; background: transparent; }",
	".diff-host table { width: 100%; }",
	".diff-annotation { padding: 12px; margin: 10px 12px 14px; border-radius: 12px; background: color-mix(in srgb, var(--kit-bgMuted) 72%, transparent); border: 1px solid color-mix(in srgb, var(--kit-borderAccent) 55%, transparent); display: grid; gap: 8px; }",
	".diff-annotation-header { display: grid; gap: 4px; }",
	".diff-annotation-header strong { font-size: 13px; }",
	".range-comment-card { padding: 10px 12px; margin: 10px 12px 14px; border-radius: 12px; background: color-mix(in srgb, var(--kit-bgMuted) 58%, transparent); border: 1px solid color-mix(in srgb, var(--kit-borderDefault) 92%, transparent); display: grid; gap: 8px; }",
	".range-comment-card button { padding: 6px 10px; font-size: 12px; }",
	".help-text { font-size: 12px; color: var(--kit-textMuted); }",
	".submit-status { font-size: 12px; color: var(--kit-textMuted); }",
	"@media (max-width: 980px) { #app { padding: 16px; } .accordion-shell { min-height: auto; max-height: none; } }",
].join("\n");

document.head.insertAdjacentHTML("beforeend", `<style>${styles}</style>`);

root.innerHTML = `
	<div class="layout">
		<section class="card toolbar">
			<div class="row" style="width:100%;justify-content:space-between;">
				<div class="header-stack">
					<h2 style="margin:0;font-size:18px;">Code review</h2>
					<div class="muted">Select a file row to expand it. Click a line, then Shift-click on the same side to extend a range.</div>
				</div>
				<div class="action-row">
					<div class="status-pill"><span id="connection-dot">●</span><span id="connection-text">Connecting…</span></div>
					<button id="refresh-button" type="button">Refresh</button>
					<button id="submit-button" type="button">Submit</button>
				</div>
			</div>
		</section>
		<section class="card accordion-shell">
			<ul class="review-list" id="review-list"></ul>
		</section>
		<div class="submit-status" id="submit-status"></div>
	</div>
`;

function requireElement<T extends HTMLElement>(id: string): T {
	const element = document.getElementById(id);
	if (!(element instanceof HTMLElement)) {
		throw new Error(`Missing required element: ${id}`);
	}
	return element as T;
}

const reviewList = requireElement<HTMLUListElement>("review-list");
const refreshButton = requireElement<HTMLButtonElement>("refresh-button");
const submitButton = requireElement<HTMLButtonElement>("submit-button");
const submitStatus = requireElement<HTMLDivElement>("submit-status");
const connectionDot = requireElement<HTMLSpanElement>("connection-dot");
const connectionText = requireElement<HTMLSpanElement>("connection-text");

let currentState: CodeReviewBrowserState | null = null;
let selectedFileId: string | null = null;
let selectedRange: SelectedRange | null = null;
let diffInstance: FileDiff<RangeAnnotationMetadata> | null = null;
let draft: ReviewDraft = { fileComments: {}, rangeComments: {} };

function setConnection(status: string, color: string): void {
	connectionText.textContent = status;
	connectionDot.style.color = color;
}

function escapeHtml(text: string): string {
	return text
		.replaceAll("&", "&amp;")
		.replaceAll("<", "&lt;")
		.replaceAll(">", "&gt;")
		.replaceAll('"', "&quot;")
		.replaceAll("'", "&#39;");
}

function statusLabel(status: string): string {
	if (status === "new") return "A";
	if (status === "deleted") return "D";
	if (status === "rename-pure" || status === "rename-changed") return "R";
	return "M";
}

function statusClass(status: string): string {
	if (status === "new") return "add";
	if (status === "deleted") return "delete";
	if (status === "rename-pure" || status === "rename-changed") return "rename";
	return "modify";
}

function fileCommentKey(file: CodeReviewBrowserFile): string {
	return file.path;
}

function rangeKey(
	path: string,
	side: DiffSide,
	startLine: number,
	endLine: number,
): string {
	const start = Math.min(startLine, endLine);
	const end = Math.max(startLine, endLine);
	return `${path}::${side}::${start}-${end}`;
}

function getSelectedRangeKey(): string | null {
	if (!selectedRange) return null;
	return rangeKey(
		selectedRange.path,
		selectedRange.side,
		selectedRange.startLine,
		selectedRange.endLine,
	);
}

function buildSelection(
	path: string,
	side: DiffSide,
	anchorLine: number,
	currentLine: number,
): SelectedRange {
	return {
		path,
		side,
		anchorLine,
		startLine: Math.min(anchorLine, currentLine),
		endLine: Math.max(anchorLine, currentLine),
	};
}

function getStructuredReviewDraft(): CodeReviewSubmission | null {
	if (!currentState) return null;
	const files = currentState.review.files
		.map((file) => {
			const fileComment =
				draft.fileComments[fileCommentKey(file)]?.trim() ?? "";
			const ranges = Object.entries(draft.rangeComments)
				.filter(
					([key, value]) =>
						key.startsWith(`${file.path}::`) && value.trim().length > 0,
				)
				.map(([key, value]) => {
					const [, side, range] = key.split("::");
					const [startLineText, endLineText] = range.split("-");
					return {
						side: side as DiffSide,
						startLine: Number(startLineText),
						endLine: Number(endLineText),
						comment: value.trim(),
					};
				})
				.sort((a, b) => a.startLine - b.startLine || a.endLine - b.endLine);
			if (fileComment.length === 0 && ranges.length === 0) return null;
			return {
				path: file.path,
				fileComment,
				ranges,
			};
		})
		.filter((file): file is NonNullable<typeof file> => file !== null);
	if (files.length === 0) return null;
	return {
		submittedAt: new Date().toISOString(),
		files,
	};
}

function updateSubmitButtonState(): void {
	const review = getStructuredReviewDraft();
	submitButton.disabled = review === null;
	if (review === null) {
		submitStatus.textContent =
			"Add a file or line comment to send review state to Kit.";
		return;
	}
	const commentCount = review.files.reduce(
		(sum, file) =>
			sum + (file.fileComment.length > 0 ? 1 : 0) + file.ranges.length,
		0,
	);
	submitStatus.textContent = `${commentCount} comment${commentCount === 1 ? "" : "s"} ready to send.`;
}

function reconcileDrafts(state: CodeReviewBrowserState): void {
	const nextFileComments: Record<string, string> = {};
	const nextRangeComments: Record<string, string> = {};
	const validPaths = new Set(state.review.files.map((file) => file.path));

	for (const [key, value] of Object.entries(draft.fileComments)) {
		if (validPaths.has(key)) {
			nextFileComments[key] = value;
		}
	}

	for (const [key, value] of Object.entries(draft.rangeComments)) {
		const [path] = key.split("::");
		if (validPaths.has(path)) {
			nextRangeComments[key] = value;
		}
	}

	draft = {
		fileComments: nextFileComments,
		rangeComments: nextRangeComments,
	};

	if (selectedRange && !validPaths.has(selectedRange.path)) {
		selectedRange = null;
	}
}

function expandedDiffHostId(fileId: string): string {
	return `diff-host-${fileId}`;
}

function expandedFileCommentId(fileId: string): string {
	return `file-comment-${fileId}`;
}

function createRangeAnnotationElement(
	metadata: RangeAnnotationMetadata,
): HTMLElement {
	const key = metadata.rangeKey;
	const isSelected = key === getSelectedRangeKey();
	const currentValue = draft.rangeComments[key] ?? "";

	if (!isSelected) {
		const wrapper = document.createElement("div");
		wrapper.className = "range-comment-card";

		const header = document.createElement("div");
		header.className = "row";
		header.innerHTML = `<strong>${escapeHtml(metadata.side)} ${metadata.startLine}${metadata.startLine === metadata.endLine ? "" : `–${metadata.endLine}`}</strong>`;
		const button = document.createElement("button");
		button.type = "button";
		button.textContent = "Edit";
		button.addEventListener("click", () => {
			selectedRange = buildSelection(
				metadata.path,
				metadata.side,
				metadata.startLine,
				metadata.endLine,
			);
			if (currentState) {
				renderState(currentState);
			}
		});
		header.appendChild(button);
		wrapper.appendChild(header);

		const body = document.createElement("div");
		body.textContent = currentValue;
		wrapper.appendChild(body);
		return wrapper;
	}

	const wrapper = document.createElement("div");
	wrapper.className = "diff-annotation";

	const header = document.createElement("div");
	header.className = "diff-annotation-header";
	const title = document.createElement("strong");
	title.textContent = `${metadata.side} ${metadata.startLine}${metadata.startLine === metadata.endLine ? "" : `–${metadata.endLine}`}`;
	header.appendChild(title);
	const help = document.createElement("div");
	help.className = "help-text";
	help.textContent =
		"Selection stays on one diff side only. Shift-click another line on the same side to extend a range.";
	header.appendChild(help);
	wrapper.appendChild(header);

	const label = document.createElement("label");
	label.textContent = "Line comment";
	wrapper.appendChild(label);

	const textarea = document.createElement("textarea");
	textarea.rows = 4;
	textarea.placeholder = "Comment on this selected line or range...";
	textarea.value = currentValue;
	textarea.addEventListener("input", () => {
		draft.rangeComments[key] = textarea.value;
		updateSubmitButtonState();
	});
	wrapper.appendChild(textarea);

	const actions = document.createElement("div");
	actions.className = "action-row";
	const preserve = document.createElement("div");
	preserve.className = "help-text";
	preserve.textContent =
		"Range comments are preserved on refresh by file path and selected line range.";
	actions.appendChild(preserve);
	const clearButton = document.createElement("button");
	clearButton.type = "button";
	clearButton.textContent =
		currentValue.trim().length > 0 ? "Delete comment" : "Clear selection";
	clearButton.addEventListener("click", () => {
		delete draft.rangeComments[key];
		selectedRange = null;
		updateSubmitButtonState();
		if (currentState) {
			renderState(currentState);
		}
	});
	actions.appendChild(clearButton);
	wrapper.appendChild(actions);

	return wrapper;
}

function buildRangeAnnotations(file: CodeReviewBrowserFile): Array<{
	lineNumber: number;
	side: DiffSide;
	metadata: RangeAnnotationMetadata;
}> {
	const entries: Array<{
		lineNumber: number;
		side: DiffSide;
		metadata: RangeAnnotationMetadata;
	}> = [];

	for (const [key, comment] of Object.entries(draft.rangeComments)) {
		if (!key.startsWith(`${file.path}::`) || comment.trim().length === 0) {
			continue;
		}
		const [, side, range] = key.split("::");
		const [startLineText, endLineText] = range.split("-");
		entries.push({
			lineNumber: Number(endLineText),
			side: side as DiffSide,
			metadata: {
				rangeKey: key,
				path: file.path,
				side: side as DiffSide,
				startLine: Number(startLineText),
				endLine: Number(endLineText),
			},
		});
	}

	if (selectedRange && selectedRange.path === file.path) {
		const key = getSelectedRangeKey();
		if (key && !entries.some((entry) => entry.metadata.rangeKey === key)) {
			entries.push({
				lineNumber: selectedRange.endLine,
				side: selectedRange.side,
				metadata: {
					rangeKey: key,
					path: selectedRange.path,
					side: selectedRange.side,
					startLine: selectedRange.startLine,
					endLine: selectedRange.endLine,
				},
			});
		}
	}

	return entries;
}

function renderReviewList(state: CodeReviewBrowserState): void {
	if (state.review.files.length === 0) {
		reviewList.innerHTML = `<li class="empty">No uncommitted diff available.</li>`;
		return;
	}

	const expandedFile =
		state.review.files.find((file) => file.id === selectedFileId) ?? null;

	reviewList.innerHTML = state.review.files
		.map((file) => {
			const expanded = expandedFile?.id === file.id;
			const selected = expanded ? " selected" : "";
			const prevPath = file.prevPath
				? `<div class="muted">from ${escapeHtml(file.prevPath)}</div>`
				: "";
			const fileComment =
				draft.fileComments[fileCommentKey(file)]?.trim() ?? "";
			const rangeCommentCount = Object.entries(draft.rangeComments).filter(
				([key, value]) =>
					key.startsWith(`${file.path}::`) && value.trim().length > 0,
			).length;
			const totalCommentCount =
				(fileComment.length > 0 ? 1 : 0) + rangeCommentCount;
			const commentSummary =
				totalCommentCount > 0
					? `<span class="comment-chip">${totalCommentCount} note${totalCommentCount === 1 ? "" : "s"}</span>`
					: "";
			const body = expanded
				? `<div class="file-body">
					<div class="file-comment-card">
						<label for="${expandedFileCommentId(file.id)}">File comment</label>
						<textarea id="${expandedFileCommentId(file.id)}" rows="4" placeholder="Comment on the whole file...">${escapeHtml(draft.fileComments[fileCommentKey(file)] ?? "")}</textarea>
						<div class="help-text">File-level comments are preserved on refresh by file path.</div>
					</div>
					<div class="patch-shell">
						<div class="diff-host" id="${expandedDiffHostId(file.id)}"></div>
					</div>
				</div>`
				: "";
			return `<li class="review-row">
				<button class="file-button${selected}" data-file-id="${escapeHtml(file.id)}" type="button">
					<div class="row">
						<span class="path"><span class="badge ${statusClass(file.status)}">${statusLabel(file.status)}</span><strong>${escapeHtml(file.path)}</strong></span>
						${commentSummary}
					</div>
					${prevPath}
				</button>
				${body}
			</li>`;
		})
		.join("");
}

function disposeDiff(): void {
	diffInstance?.cleanUp();
	diffInstance = null;
}

function renderEmptyPatch(host: HTMLElement, message: string): void {
	disposeDiff();
	host.innerHTML = `<div class="empty">${escapeHtml(message)}</div>`;
}

function renderExpandedFile(state: CodeReviewBrowserState): void {
	if (state.review.error) {
		submitStatus.textContent = state.review.error;
		disposeDiff();
		return;
	}

	const file =
		state.review.files.find((entry) => entry.id === selectedFileId) ?? null;
	if (!file) {
		disposeDiff();
		return;
	}

	const diffHost = document.getElementById(expandedDiffHostId(file.id));
	const fileCommentInput = document.getElementById(
		expandedFileCommentId(file.id),
	);
	if (
		!(diffHost instanceof HTMLElement) ||
		!(fileCommentInput instanceof HTMLTextAreaElement)
	) {
		return;
	}

	fileCommentInput.addEventListener("input", () => {
		draft.fileComments[fileCommentKey(file)] = fileCommentInput.value;
		updateSubmitButtonState();
	});

	const fileDiff = (() => {
		try {
			return parsePatchFiles(file.rawPatch, file.id, true).flatMap(
				(parsed) => parsed.files,
			)[0];
		} catch (error) {
			renderEmptyPatch(diffHost, String(error));
			return null;
		}
	})();

	if (!fileDiff) {
		renderEmptyPatch(diffHost, "Unable to parse this patch.");
		return;
	}

	const lineAnnotations = buildRangeAnnotations(file);

	disposeDiff();
	diffHost.innerHTML = "";
	diffInstance = new FileDiff<RangeAnnotationMetadata>({
		theme: "pierre-dark",
		themeType: "dark",
		overflow: "wrap",
		diffStyle: "unified",
		disableBackground: true,
		hunkSeparators: "line-info-basic",
		onLineClick: ({ lineNumber, annotationSide, event }) => {
			const sameFile = selectedRange?.path === file.path;
			const sameSide = selectedRange?.side === annotationSide;
			if (event.shiftKey && sameFile && sameSide && selectedRange) {
				selectedRange = buildSelection(
					file.path,
					annotationSide,
					selectedRange.anchorLine,
					lineNumber,
				);
			} else {
				selectedRange = buildSelection(
					file.path,
					annotationSide,
					lineNumber,
					lineNumber,
				);
			}
			const nextState = currentState ?? state;
			renderState(nextState);
		},
		renderAnnotation: (annotation) =>
			createRangeAnnotationElement(annotation.metadata),
	});
	diffInstance.render({
		fileDiff,
		containerWrapper: diffHost,
		forceRender: true,
		lineAnnotations,
	});
	if (selectedRange && selectedRange.path === file.path) {
		diffInstance.setSelectedLines({
			start: selectedRange.startLine,
			end: selectedRange.endLine,
			side: selectedRange.side,
			endSide: selectedRange.side,
		});
	}
}

function renderState(state: CodeReviewBrowserState): void {
	const firstRender = currentState === null;
	currentState = state;
	reconcileDrafts(state);
	if (
		selectedFileId &&
		!state.review.files.some((file) => file.id === selectedFileId)
	) {
		selectedFileId = state.review.files[0]?.id ?? null;
	}
	if (firstRender && selectedFileId === null) {
		selectedFileId = state.review.files[0]?.id ?? null;
	}
	const activeSelectedRange = selectedRange;
	if (activeSelectedRange) {
		const selectedFile = state.review.files.find(
			(file) => file.path === activeSelectedRange.path,
		);
		if (!selectedFile) {
			selectedRange = null;
		}
	}
	renderReviewList(state);
	renderExpandedFile(state);
	updateSubmitButtonState();
}

let socket: WebSocket | null = null;
let reconnectTimer: number | null = null;
let reconnectAttempt = 0;
let snapshotRefreshInFlight = false;

function send(message: CodeReviewClientMessage): void {
	if (!socket || socket.readyState !== WebSocket.OPEN) return;
	socket.send(JSON.stringify(message));
}

const url = new URL(window.location.href);
const sessionId = url.searchParams.get("sessionId") ?? "";
const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
const socketUrl = `${protocol}//${window.location.host}/ws?sessionId=${encodeURIComponent(sessionId)}`;
const stateUrl = `/state?sessionId=${encodeURIComponent(sessionId)}`;
const healthUrl = `/health?sessionId=${encodeURIComponent(sessionId)}`;

async function refreshStateSnapshot(reason: string): Promise<boolean> {
	if (snapshotRefreshInFlight) return false;
	snapshotRefreshInFlight = true;
	try {
		const response = await fetch(stateUrl, { cache: "no-store" });
		if (!response.ok) return false;
		const state = (await response.json()) as CodeReviewBrowserState;
		renderState(state);
		if (!socket || socket.readyState !== WebSocket.OPEN) {
			setConnection(reason, theme.warningText);
		}
		return true;
	} catch {
		return false;
	} finally {
		snapshotRefreshInFlight = false;
	}
}

async function waitForHostReady(): Promise<boolean> {
	try {
		const response = await fetch(healthUrl, { cache: "no-store" });
		return response.ok;
	} catch {
		return false;
	}
}

function scheduleReconnect(): void {
	if (reconnectTimer !== null) return;
	const delay = Math.min(1000 * 2 ** reconnectAttempt, 5000);
	reconnectAttempt += 1;
	setConnection("Reconnecting…", theme.warningText);
	void refreshStateSnapshot("Snapshot only");
	reconnectTimer = window.setTimeout(() => {
		reconnectTimer = null;
		connectSocket();
	}, delay);
}

async function connectSocket(): Promise<void> {
	if (
		socket &&
		(socket.readyState === WebSocket.OPEN ||
			socket.readyState === WebSocket.CONNECTING)
	) {
		return;
	}

	setConnection(
		reconnectAttempt > 0 ? "Reconnecting…" : "Connecting…",
		theme.warningText,
	);
	const hostReady = await waitForHostReady();
	if (!hostReady) {
		setConnection("Waiting for Kit…", theme.warningText);
		void refreshStateSnapshot("Waiting for Kit…");
		scheduleReconnect();
		return;
	}

	try {
		const nextSocket = new WebSocket(socketUrl);
		socket = nextSocket;

		nextSocket.addEventListener("open", () => {
			reconnectAttempt = 0;
			setConnection("Connected", theme.toolText);
			send({ type: "ready" });
		});

		nextSocket.addEventListener("close", () => {
			if (socket === nextSocket) {
				socket = null;
			}
			scheduleReconnect();
		});

		nextSocket.addEventListener("error", () => {
			setConnection("Connection error", theme.errorText);
			void refreshStateSnapshot("Snapshot only");
		});

		nextSocket.addEventListener("message", (event) => {
			try {
				const message = JSON.parse(event.data) as CodeReviewServerMessage;
				if (message.type === "connected") return;
				if (message.type === "state") {
					renderState(message.state);
					return;
				}
				if (message.type === "submission_saved") {
					draft = { fileComments: {}, rangeComments: {} };
					selectedRange = null;
					submitStatus.textContent = `Sent ${message.commentCount} comment${message.commentCount === 1 ? "" : "s"} across ${message.fileCount} file${message.fileCount === 1 ? "" : "s"} at ${message.submittedAt}.`;
					if (currentState) {
						renderState(currentState);
					}
					send({ type: "refresh_diff" });
				}
			} catch {
				setConnection("Bad server response", theme.errorText);
			}
		});
	} catch {
		setConnection("Connection error", theme.errorText);
		void refreshStateSnapshot("Snapshot only");
		scheduleReconnect();
	}
}

void refreshStateSnapshot("Loading review…");
void connectSocket();

refreshButton.addEventListener("click", () => {
	if (socket && socket.readyState === WebSocket.OPEN) {
		send({ type: "refresh_diff" });
		return;
	}
	void refreshStateSnapshot("Snapshot only");
});

submitButton.addEventListener("click", () => {
	const review = getStructuredReviewDraft();
	if (!review) return;
	send({ type: "submit_review_state", review });
});

reviewList.addEventListener("click", (event) => {
	const target = event.target;
	if (!(target instanceof Element)) return;
	const button = target.closest("[data-file-id]");
	if (!(button instanceof HTMLElement)) return;
	const nextFileId = button.dataset.fileId ?? null;
	selectedFileId = selectedFileId === nextFileId ? null : nextFileId;
	selectedRange = null;
	if (currentState) {
		renderState(currentState);
	}
});

window.addEventListener("beforeunload", () => {
	disposeDiff();
});

updateSubmitButtonState();
