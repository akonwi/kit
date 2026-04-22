import { FileDiff, parsePatchFiles } from "@pierre/diffs";

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

type CodeReviewBrowserFile = {
	id: string;
	path: string;
	prevPath?: string;
	status: string;
	filetype?: string;
	changeCount: number;
	hunkCount: number;
	rawPatch: string;
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

type CodeReviewClientMessage =
	| { type: "ready" }
	| { type: "request_state" }
	| { type: "refresh_diff" };

type CodeReviewServerMessage =
	| { type: "connected"; sessionUrl: string }
	| { type: "state"; state: CodeReviewBrowserState; reason: string };

declare global {
	interface Window {
		__KIT_CODE_REVIEW_BOOTSTRAP__?: {
			theme: BrowserTheme;
		};
	}
}

const bootstrap = window.__KIT_CODE_REVIEW_BOOTSTRAP__;
if (!bootstrap) {
	throw new Error("Missing Kit code review bootstrap data.");
}

const theme = bootstrap.theme;
const root = document.getElementById("app");
if (!root) {
	throw new Error("Missing code review root element.");
}

for (const [key, value] of Object.entries(theme)) {
	document.documentElement.style.setProperty(`--kit-${key}`, value);
}

document.title = "Kit Code Review";

document.body.style.margin = "0";
document.body.style.background = theme.bg;
document.body.style.color = theme.textPrimary;

document.head.insertAdjacentHTML(
	"beforeend",
	`<style>
		:root {
			color-scheme: dark;
			font-family: Inter, ui-sans-serif, system-ui, sans-serif;
		}
		* { box-sizing: border-box; }
		body {
			min-height: 100vh;
			background: radial-gradient(circle at top, color-mix(in srgb, var(--kit-bgAccent) 50%, var(--kit-bg) 50%), var(--kit-bg) 60%);
		}
		button, input, textarea { font: inherit; }
		button {
			border: 1px solid var(--kit-borderDefault);
			background: var(--kit-bgAccent);
			color: var(--kit-textPrimary);
			border-radius: 10px;
			padding: 9px 12px;
			cursor: pointer;
		}
		button:hover {
			border-color: var(--kit-borderAccent);
		}
		button:disabled {
			opacity: 0.6;
			cursor: default;
		}
		#app {
			padding: 24px;
		}
		.layout {
			max-width: 1360px;
			margin: 0 auto;
			display: grid;
			gap: 16px;
		}
		.card {
			background: color-mix(in srgb, var(--kit-bgSurface) 92%, transparent);
			border: 1px solid color-mix(in srgb, var(--kit-borderDefault) 90%, transparent);
			border-radius: 16px;
			box-shadow: 0 18px 48px rgba(0, 0, 0, 0.28);
		}
		.muted {
			color: var(--kit-textMuted);
		}
		.status-pill {
			display: inline-flex;
			align-items: center;
			gap: 8px;
			padding: 6px 10px;
			border-radius: 999px;
			background: color-mix(in srgb, var(--kit-bgMuted) 75%, transparent);
			border: 1px solid var(--kit-borderDefault);
			font-size: 13px;
		}
		.shell {
			display: grid;
			grid-template-columns: minmax(280px, 340px) minmax(0, 1fr);
			gap: 16px;
			min-height: 72vh;
		}
		.sidebar,
		.main {
			min-height: 0;
		}
		.sidebar {
			padding: 14px;
			display: grid;
			gap: 12px;
			align-content: start;
		}
		.sidebar-header,
		.main-header {
			display: flex;
			justify-content: space-between;
			align-items: center;
			gap: 12px;
		}
		.file-list {
			list-style: none;
			margin: 0;
			padding: 0;
			display: grid;
			gap: 8px;
			max-height: calc(72vh - 64px);
			overflow: auto;
		}
		.file-button {
			width: 100%;
			text-align: left;
			background: color-mix(in srgb, var(--kit-bgMuted) 55%, transparent);
			border: 1px solid color-mix(in srgb, var(--kit-borderDefault) 90%, transparent);
			border-radius: 12px;
			padding: 12px;
			display: grid;
			gap: 8px;
		}
		.file-button.selected {
			border-color: var(--kit-borderAccent);
			background: color-mix(in srgb, var(--kit-bgAccent) 35%, var(--kit-bgMuted) 65%);
		}
		.row {
			display: flex;
			justify-content: space-between;
			gap: 10px;
			align-items: center;
		}
		.path {
			display: inline-flex;
			align-items: center;
			gap: 8px;
			min-width: 0;
		}
		.path strong {
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
		}
		.badge {
			display: inline-flex;
			align-items: center;
			justify-content: center;
			min-width: 22px;
			height: 22px;
			padding: 0 8px;
			border-radius: 999px;
			font-size: 12px;
			font-weight: 700;
		}
		.badge.add {
			background: color-mix(in srgb, var(--kit-toolText) 24%, transparent);
			color: var(--kit-toolText);
		}
		.badge.delete {
			background: color-mix(in srgb, var(--kit-errorText) 24%, transparent);
			color: var(--kit-errorText);
		}
		.badge.modify {
			background: color-mix(in srgb, var(--kit-userText) 24%, transparent);
			color: var(--kit-userText);
		}
		.badge.rename {
			background: color-mix(in srgb, var(--kit-warningText) 24%, transparent);
			color: var(--kit-warningText);
		}
		.main {
			padding: 14px;
			display: grid;
			grid-template-rows: auto minmax(0, 1fr);
			gap: 12px;
		}
		.patch-shell {
			min-height: 0;
			overflow: auto;
			border-radius: 12px;
			background: color-mix(in srgb, var(--kit-bg) 72%, var(--kit-bgSurface) 28%);
			border: 1px solid color-mix(in srgb, var(--kit-borderDefault) 90%, transparent);
			padding: 12px;
		}
		.diff-host {
			min-height: 100%;
		}
		.empty {
			padding: 24px;
			border-radius: 12px;
			border: 1px dashed color-mix(in srgb, var(--kit-borderDefault) 90%, transparent);
			color: var(--kit-textMuted);
			background: color-mix(in srgb, var(--kit-bgMuted) 42%, transparent);
		}
		.diff-host diffs-container {
			display: block;
		}
		.diff-host svg[data-icon-sprite] {
			display: none;
		}
		.diff-host pre {
			max-height: none;
			padding: 0;
			margin: 0;
			background: transparent;
		}
		.diff-host table {
			width: 100%;
		}
		@media (max-width: 980px) {
			#app {
				padding: 16px;
			}
			.shell {
				grid-template-columns: 1fr;
			}
			.file-list {
				max-height: 260px;
			}
		}
	</style>`,
);

root.innerHTML = `
	<div class="layout">
		<section class="shell">
			<aside class="card sidebar">
				<div class="sidebar-header">
					<div class="row" style="width:100%;justify-content:space-between;">
						<h2 style="margin:0;font-size:18px;">Code review</h2>
						<div class="status-pill"><span id="connection-dot">●</span><span id="connection-text">Connecting…</span></div>
					</div>
				</div>
				<div class="sidebar-header">
					<h2 style="margin:0;font-size:18px;">Changed files</h2>
					<button id="refresh-button" type="button">Refresh</button>
				</div>
				<ul class="file-list" id="file-list"></ul>
			</aside>
			<section class="card main">
				<div class="main-header">
					<div>
						<h2 id="patch-title" style="margin:0;font-size:18px;">Patch</h2>
						<div class="muted" id="patch-meta"></div>
					</div>
				</div>
				<div class="patch-shell">
					<div class="diff-host" id="diff-host"></div>
				</div>
			</section>
		</section>
	</div>
`;

function requireElement<T extends HTMLElement>(id: string): T {
	const element = document.getElementById(id);
	if (!(element instanceof HTMLElement)) {
		throw new Error(`Missing required element: ${id}`);
	}
	return element as T;
}

const fileList = requireElement<HTMLUListElement>("file-list");
const patchTitle = requireElement<HTMLHeadingElement>("patch-title");
const patchMeta = requireElement<HTMLDivElement>("patch-meta");
const diffHost = requireElement<HTMLDivElement>("diff-host");
const refreshButton = requireElement<HTMLButtonElement>("refresh-button");
const connectionDot = requireElement<HTMLSpanElement>("connection-dot");
const connectionText = requireElement<HTMLSpanElement>("connection-text");

let currentState: CodeReviewBrowserState | null = null;
let selectedFileId: string | null = null;
let diffInstance: FileDiff | null = null;

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

function getSelectedFile(
	state: CodeReviewBrowserState,
): CodeReviewBrowserFile | null {
	return (
		state.review.files.find((file) => file.id === selectedFileId) ??
		state.review.files[0] ??
		null
	);
}

function renderFileList(state: CodeReviewBrowserState): void {
	if (state.review.files.length === 0) {
		fileList.innerHTML = `<li class="empty">No uncommitted diff available.</li>`;
		return;
	}

	fileList.innerHTML = state.review.files
		.map((file) => {
			const selected = file.id === selectedFileId ? " selected" : "";
			const prevPath = file.prevPath
				? `<div class="muted">from ${escapeHtml(file.prevPath)}</div>`
				: "";
			const filetype = file.filetype
				? `<span class="muted">${escapeHtml(file.filetype)}</span>`
				: "";
			return `<li>
				<button class="file-button${selected}" data-file-id="${escapeHtml(file.id)}" type="button">
					<div class="row">
						<span class="path"><span class="badge ${statusClass(file.status)}">${statusLabel(file.status)}</span><strong>${escapeHtml(file.path)}</strong></span>
						<span class="muted">${file.changeCount}</span>
					</div>
					<div class="row"><span class="muted">${file.hunkCount} hunks</span>${filetype}</div>
					${prevPath}
				</button>
			</li>`;
		})
		.join("");
}

function disposeDiff(): void {
	diffInstance?.cleanUp();
	diffInstance = null;
}

function renderEmptyPatch(message: string): void {
	disposeDiff();
	diffHost.innerHTML = `<div class="empty">${message}</div>`;
}

function renderPatch(state: CodeReviewBrowserState): void {
	if (state.review.error) {
		patchTitle.textContent = "Diff unavailable";
		patchMeta.textContent = "";
		renderEmptyPatch(state.review.error);
		return;
	}

	const file = getSelectedFile(state);
	if (!file) {
		patchTitle.textContent = "Patch";
		patchMeta.textContent = "";
		renderEmptyPatch("No current uncommitted changes.");
		return;
	}

	selectedFileId = file.id;
	patchTitle.textContent = file.path;
	patchMeta.textContent = `${file.hunkCount} hunks • ${file.changeCount} changed lines`;

	const fileDiff = (() => {
		try {
			return parsePatchFiles(file.rawPatch, file.id, true).flatMap(
				(parsed) => parsed.files,
			)[0];
		} catch (error) {
			renderEmptyPatch(String(error));
			return null;
		}
	})();

	if (!fileDiff) {
		renderEmptyPatch("Unable to parse this patch.");
		return;
	}

	disposeDiff();
	diffHost.innerHTML = "";
	diffInstance = new FileDiff({
		theme: "pierre-dark",
		themeType: "dark",
		overflow: "wrap",
		diffStyle: "unified",
		disableBackground: true,
		hunkSeparators: "line-info-basic",
	});
	diffInstance.render({
		fileDiff,
		containerWrapper: diffHost,
		forceRender: true,
	});
}

function renderState(state: CodeReviewBrowserState): void {
	currentState = state;
	if (
		!selectedFileId ||
		!state.review.files.some((file) => file.id === selectedFileId)
	) {
		selectedFileId = state.review.files[0]?.id ?? null;
	}
	renderFileList(state);
	renderPatch(state);
}

function send(message: CodeReviewClientMessage): void {
	socket.send(JSON.stringify(message));
}

const url = new URL(window.location.href);
const token = url.searchParams.get("token") ?? "";
const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
const socket = new WebSocket(
	`${protocol}//${window.location.host}/ws?token=${encodeURIComponent(token)}`,
);

socket.addEventListener("open", () => {
	setConnection("Connected", theme.toolText);
	send({ type: "ready" });
});

socket.addEventListener("close", () => {
	setConnection("Disconnected", theme.warningText);
});

socket.addEventListener("error", () => {
	setConnection("Connection error", theme.errorText);
});

socket.addEventListener("message", (event) => {
	const message = JSON.parse(event.data) as CodeReviewServerMessage;
	if (message.type === "connected") {
		return;
	}
	if (message.type === "state") {
		renderState(message.state);
	}
});

refreshButton.addEventListener("click", () => {
	send({ type: "refresh_diff" });
});

fileList.addEventListener("click", (event) => {
	const target = event.target;
	if (!(target instanceof Element)) return;
	const button = target.closest("[data-file-id]");
	if (!(button instanceof HTMLElement)) return;
	selectedFileId = button.dataset.fileId ?? null;
	if (currentState) {
		renderState(currentState);
	}
});

window.addEventListener("beforeunload", () => {
	disposeDiff();
});
