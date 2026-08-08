import path from "node:path";
import { pathToFileURL } from "node:url";
import { getInstalledRuntimeDir } from "../../runtime/runtime-dir";
import type {
	MermaidPreviewWorkerRequest,
	MermaidPreviewWorkerResult,
} from "./mermaid-preview-worker";
import type { MermaidPreviewColors, MermaidPreviewImage } from "./render";

const MERMAID_PREVIEW_TIMEOUT_MS = 5_000;
const MERMAID_PREVIEW_CACHE_SIZE = 10;

const previewCache = new Map<string, MermaidPreviewImage>();

function previewWorkerUrl(): URL {
	const runtimeDir = getInstalledRuntimeDir();
	return runtimeDir
		? pathToFileURL(path.join(runtimeDir, "mermaid-preview-worker.js"))
		: new URL("./mermaid-preview-worker.ts", import.meta.url);
}

function cacheKey(source: string, colors: MermaidPreviewColors): string {
	return `${JSON.stringify(colors)}\0${source}`;
}

function cachePreview(key: string, image: MermaidPreviewImage): void {
	previewCache.delete(key);
	previewCache.set(key, image);
	if (previewCache.size <= MERMAID_PREVIEW_CACHE_SIZE) return;
	const oldest = previewCache.keys().next().value;
	if (typeof oldest === "string") previewCache.delete(oldest);
}

export function loadMermaidPreview(
	source: string,
	colors: MermaidPreviewColors,
	signal?: AbortSignal,
): Promise<MermaidPreviewImage> {
	const key = cacheKey(source, colors);
	const cached = previewCache.get(key);
	if (cached) {
		previewCache.delete(key);
		previewCache.set(key, cached);
		return Promise.resolve(cached);
	}

	return new Promise((resolve, reject) => {
		if (signal?.aborted) {
			reject(signal.reason);
			return;
		}

		let worker: Worker;
		try {
			worker = new Worker(previewWorkerUrl(), { type: "module" });
		} catch (error) {
			reject(error);
			return;
		}

		let settled = false;
		const cleanup = () => {
			clearTimeout(timeout);
			signal?.removeEventListener("abort", onAbort);
			worker.terminate();
		};
		const fail = (error: unknown) => {
			if (settled) return;
			settled = true;
			cleanup();
			reject(error);
		};
		const succeed = (image: MermaidPreviewImage) => {
			if (settled) return;
			settled = true;
			cleanup();
			cachePreview(key, image);
			resolve(image);
		};
		const onAbort = () => fail(signal?.reason ?? new Error("Aborted"));
		const timeout = setTimeout(
			() => fail(new Error("Mermaid visual preview timed out")),
			MERMAID_PREVIEW_TIMEOUT_MS,
		);

		signal?.addEventListener("abort", onAbort, { once: true });
		worker.onmessage = (event: MessageEvent<MermaidPreviewWorkerResult>) => {
			const result = event.data;
			if (!result.ok) {
				fail(new Error(result.error));
				return;
			}
			succeed({
				png: result.png,
				width: result.width,
				height: result.height,
			});
		};
		worker.onerror = (event) => fail(new Error(event.message));
		worker.postMessage({
			source,
			colors,
		} satisfies MermaidPreviewWorkerRequest);
	});
}
