import path from "node:path";
import { pathToFileURL } from "node:url";
import {
	BoxRenderable,
	CodeRenderable,
	type ColorInput,
	createMarkdownCodeBlockRenderer,
	RGBA,
	StyledText,
	TextRenderable,
} from "@opentui/core";
import {
	hasMermaidPreviewHandler,
	requestMermaidPreview,
} from "../features/mermaid-preview/requests";
import { getInstalledRuntimeDir } from "../runtime/runtime-dir";
import { CHEVRON_RIGHT } from "./glyphs";
import { syntaxStyle, theme } from "./theme";

const MAX_MERMAID_SOURCE_LENGTH = 8_000;
const MAX_MERMAID_SOURCE_LINES = 24;
const MAX_MERMAID_LINE_LENGTH = 512;
const MAX_MERMAID_RELATIONSHIPS = 20;
const MAX_MERMAID_NODE_DEFINITIONS = 24;
const MAX_VISUAL_MERMAID_SOURCE_LENGTH = 64_000;
const MAX_VISUAL_MERMAID_RELATIONSHIPS = 500;
const MAX_VISUAL_MERMAID_NODE_DEFINITIONS = 300;
const MAX_MERMAID_PENDING_JOBS = 8;
const MAX_MERMAID_PENDING_BYTES = 32_000;
const MAX_MERMAID_CAPACITY_WAITERS = 8;
const MAX_MERMAID_WAITING_BYTES = 32_000;
const MERMAID_RENDER_TIMEOUT_MS = 750;
const MERMAID_CACHE_SIZE = 100;
const textEncoder = new TextEncoder();

type MermaidListener = (output: string | null) => void;
type MermaidWorkerResult = { ok: true; output: string | null } | { ok: false };
type MermaidJob = {
	source: string;
	listeners: Set<MermaidListener>;
	cancelActive?: () => void;
};

const mermaidCache = new Map<string, string | null>();
const mermaidJobs = new Map<string, MermaidJob>();
const mermaidQueue: MermaidJob[] = [];
const mermaidCapacityWaiters = new Map<() => void, number>();
let activeMermaidJob: MermaidJob | null = null;

function cacheMermaid(source: string, output: string | null): void {
	mermaidCache.delete(source);
	mermaidCache.set(source, output);
	if (mermaidCache.size <= MERMAID_CACHE_SIZE) return;
	const oldest = mermaidCache.keys().next().value;
	if (typeof oldest === "string") mermaidCache.delete(oldest);
}

function mermaidWorkerUrl(): URL {
	const runtimeDir = getInstalledRuntimeDir();
	return runtimeDir
		? pathToFileURL(path.join(runtimeDir, "mermaid-worker.js"))
		: new URL("./mermaid-worker.ts", import.meta.url);
}

function wakeMermaidCapacityWaiter(): void {
	const waiter = mermaidCapacityWaiters.keys().next().value;
	if (!waiter) return;
	mermaidCapacityWaiters.delete(waiter);
	queueMicrotask(waiter);
}

function finishMermaidJob(
	job: MermaidJob,
	output: string | null,
	cacheResult: boolean,
): void {
	if (cacheResult) cacheMermaid(job.source, output);
	mermaidJobs.delete(job.source);
	activeMermaidJob = null;
	job.cancelActive = undefined;
	for (const listener of job.listeners) listener(output);
	processMermaidQueue();
	wakeMermaidCapacityWaiter();
}

function processMermaidQueue(): void {
	if (activeMermaidJob || mermaidQueue.length === 0) return;
	const job = mermaidQueue.shift();
	if (!job) return;
	if (job.listeners.size === 0) {
		mermaidJobs.delete(job.source);
		processMermaidQueue();
		return;
	}
	activeMermaidJob = job;

	let worker: Worker;
	try {
		worker = new Worker(mermaidWorkerUrl(), { type: "module" });
	} catch {
		finishMermaidJob(job, null, false);
		return;
	}
	let settled = false;
	const finish = (output: string | null, cacheResult: boolean) => {
		if (settled) return;
		settled = true;
		clearTimeout(timeout);
		worker.terminate();
		finishMermaidJob(job, output, cacheResult);
	};
	const timeout = setTimeout(
		() => finish(null, false),
		MERMAID_RENDER_TIMEOUT_MS,
	);
	job.cancelActive = () => finish(null, false);
	worker.onmessage = (event: MessageEvent<MermaidWorkerResult>) => {
		const result = event.data;
		finish(result.ok ? result.output : null, result.ok);
	};
	worker.onerror = () => finish(null, false);
	worker.postMessage(job.source);
}

function pendingMermaidBytes(): number {
	let bytes = 0;
	for (const source of mermaidJobs.keys()) {
		bytes += textEncoder.encode(source).byteLength;
	}
	return bytes;
}

function waitingMermaidBytes(): number {
	let bytes = 0;
	for (const sourceLength of mermaidCapacityWaiters.values()) {
		bytes += sourceLength;
	}
	return bytes;
}

function requestMermaid(source: string, listener: MermaidListener): () => void {
	const sourceBytes = textEncoder.encode(source).byteLength;
	let cancelled = false;
	let detachFromJob = () => {};

	const attempt = () => {
		mermaidCapacityWaiters.delete(attempt);
		if (cancelled) return;
		if (mermaidCache.has(source)) {
			const output = mermaidCache.get(source) ?? null;
			queueMicrotask(() => {
				if (!cancelled) listener(output);
			});
			wakeMermaidCapacityWaiter();
			return;
		}

		let job = mermaidJobs.get(source);
		if (!job) {
			if (
				mermaidJobs.size >= MAX_MERMAID_PENDING_JOBS ||
				pendingMermaidBytes() + sourceBytes > MAX_MERMAID_PENDING_BYTES
			) {
				if (
					mermaidCapacityWaiters.size < MAX_MERMAID_CAPACITY_WAITERS &&
					waitingMermaidBytes() + sourceBytes <= MAX_MERMAID_WAITING_BYTES
				) {
					mermaidCapacityWaiters.set(attempt, sourceBytes);
				}
				return;
			}
			job = { source, listeners: new Set([listener]) };
			mermaidJobs.set(source, job);
			mermaidQueue.push(job);
			processMermaidQueue();
		} else {
			job.listeners.add(listener);
			wakeMermaidCapacityWaiter();
		}

		const subscribedJob = job;
		detachFromJob = () => {
			subscribedJob.listeners.delete(listener);
			if (subscribedJob.listeners.size > 0) return;
			if (subscribedJob === activeMermaidJob) {
				subscribedJob.cancelActive?.();
				return;
			}
			const queuedIndex = mermaidQueue.indexOf(subscribedJob);
			if (queuedIndex >= 0) mermaidQueue.splice(queuedIndex, 1);
			mermaidJobs.delete(subscribedJob.source);
			wakeMermaidCapacityWaiter();
		};
	};

	attempt();
	return () => {
		cancelled = true;
		mermaidCapacityWaiters.delete(attempt);
		detachFromJob();
	};
}

function hasClosingFence(raw: string): boolean {
	const opening = raw.match(/^ {0,3}(`{3,}|~{3,})[^\n]*(?:\n|$)/);
	if (!opening) return true;

	const marker = opening[1];
	const markerCharacter = marker[0];
	const closingFence = new RegExp(
		`^ {0,3}${markerCharacter === "`" ? "`" : "~"}{${marker.length},}[ \\t]*$`,
		"m",
	);
	return closingFence.test(raw.slice(opening[0].length));
}

type MermaidInlineDecision =
	| { kind: "render" }
	| { kind: "source" }
	| { kind: "preview"; reason: string };

function inspectMermaid(source: string, raw: string): MermaidInlineDecision {
	if (!hasClosingFence(raw)) return { kind: "source" };
	const lines = source.split("\n");
	const nonEmptyLineCount = lines.filter(
		(line) => line.trim().length > 0,
	).length;
	const relationshipCount =
		source.match(/(?:--+|==+|-\.|\.-|->|<-)/g)?.length ?? 0;
	const nodeDefinitionCount =
		source.match(/\b[a-zA-Z][\w-]*\s*(?:\[|\{|\(|>|\[\[)/g)?.length ?? 0;
	const sourceBytes = textEncoder.encode(source).byteLength;
	const visualPreviewAllowed =
		sourceBytes <= MAX_VISUAL_MERMAID_SOURCE_LENGTH &&
		relationshipCount <= MAX_VISUAL_MERMAID_RELATIONSHIPS &&
		nodeDefinitionCount <= MAX_VISUAL_MERMAID_NODE_DEFINITIONS;

	if (sourceBytes > MAX_MERMAID_SOURCE_LENGTH) {
		return visualPreviewAllowed
			? { kind: "preview", reason: "source is too large for inline rendering" }
			: { kind: "source" };
	}
	if (nonEmptyLineCount > MAX_MERMAID_SOURCE_LINES) {
		return visualPreviewAllowed
			? { kind: "preview", reason: `${nonEmptyLineCount} source lines` }
			: { kind: "source" };
	}
	if (lines.some((line) => line.length > MAX_MERMAID_LINE_LENGTH)) {
		return visualPreviewAllowed
			? { kind: "preview", reason: "a source line is too long" }
			: { kind: "source" };
	}
	if (relationshipCount > MAX_MERMAID_RELATIONSHIPS) {
		return visualPreviewAllowed
			? { kind: "preview", reason: `${relationshipCount} relationships` }
			: { kind: "source" };
	}
	if (nodeDefinitionCount > MAX_MERMAID_NODE_DEFINITIONS) {
		return visualPreviewAllowed
			? { kind: "preview", reason: `${nodeDefinitionCount} node definitions` }
			: { kind: "source" };
	}
	return { kind: "render" };
}

export function canRenderMermaid(source: string, raw: string): boolean {
	return inspectMermaid(source, raw).kind === "render";
}

function styledMermaidText(content: string): StyledText {
	return new StyledText([
		{
			__isChunk: true,
			text: content,
			fg: RGBA.fromHex(theme.textSecondary),
		},
	]);
}

function styleMermaidSource(fallback: CodeRenderable): CodeRenderable {
	fallback.filetype = undefined;
	fallback.initialStyledText = styledMermaidText(fallback.content);
	fallback.fg = theme.textSecondary;
	fallback.conceal = false;
	fallback.wrapMode = "none";
	fallback.width = "100%";
	return fallback;
}

class MermaidRenderable extends BoxRenderable {
	private cancelRender: () => void;

	constructor(fallback: CodeRenderable, source: string) {
		super(fallback.ctx, {
			id: `${fallback.id}-mermaid`,
			width: "100%",
			flexDirection: "column",
		});
		styleMermaidSource(fallback);
		this.add(fallback);
		this.cancelRender = requestMermaid(source, (output) => {
			if (output === null || this.isDestroyed) return;
			fallback.initialStyledText = styledMermaidText(output);
			fallback.content = output;
		});
	}

	protected override destroySelf(): void {
		this.cancelRender();
		super.destroySelf();
	}
}

class MermaidPreviewFallbackRenderable extends BoxRenderable {
	constructor(
		fallback: CodeRenderable,
		source: string,
		reason: string,
		onOpen: (source: string) => void,
	) {
		super(fallback.ctx, {
			id: `${fallback.id}-mermaid-preview`,
			width: "100%",
			flexDirection: "column",
		});
		const action = new BoxRenderable(fallback.ctx, {
			id: `${fallback.id}-mermaid-preview-action`,
			height: 1,
			paddingX: 1,
			focusable: true,
			onMouseUp(event) {
				if (event.button !== 0) return;
				event.preventDefault();
				event.stopPropagation();
				this.blur();
				onOpen(source);
			},
			onKeyDown: (key) => {
				if (key.name === "return" || key.name === "space") onOpen(source);
			},
		});
		action.add(
			new TextRenderable(fallback.ctx, {
				content: `${CHEVRON_RIGHT} Open diagram · ${reason}`,
				fg: theme.metaText,
				wrapMode: "none",
			}),
		);
		this.add(action);
		this.add(fallback);
	}
}

export type KitMarkdownProps = {
	content: string;
	fg?: ColorInput;
	conceal?: boolean;
	streaming?: boolean;
	onOpenMermaid?: (source: string) => void;
};

export function KitMarkdown(props: KitMarkdownProps) {
	const openPreview =
		props.onOpenMermaid ??
		(hasMermaidPreviewHandler() ? requestMermaidPreview : undefined);
	const renderMarkdownNode = createMarkdownCodeBlockRenderer({
		mermaid: (token, context) => {
			const decision = inspectMermaid(token.text, token.raw);
			const defaultFallback = context.defaultRender();
			if (decision.kind === "render") {
				if (!(defaultFallback instanceof CodeRenderable))
					return defaultFallback;
				return new MermaidRenderable(defaultFallback, token.text);
			}
			const fallback =
				defaultFallback instanceof CodeRenderable
					? styleMermaidSource(defaultFallback)
					: defaultFallback;
			if (decision.kind === "source") return fallback;
			if (!openPreview || !(fallback instanceof CodeRenderable))
				return fallback;
			return new MermaidPreviewFallbackRenderable(
				fallback,
				token.text,
				decision.reason,
				openPreview,
			);
		},
	});

	return (
		<markdown
			content={props.content}
			syntaxStyle={syntaxStyle()}
			conceal={props.conceal ?? true}
			streaming={props.streaming}
			fg={props.fg}
			renderNode={renderMarkdownNode}
		/>
	);
}
