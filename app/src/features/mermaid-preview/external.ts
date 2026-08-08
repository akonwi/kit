import { createHash, randomUUID } from "node:crypto";
import { rmSync } from "node:fs";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { openExternal } from "../../shell/open-external";

const PREVIEW_RETENTION_MS = 60_000;
let previewDirectoryPromise: Promise<string> | undefined;
let registeredExitCleanup = false;

async function previewDirectory(): Promise<string> {
	previewDirectoryPromise ??= mkdtemp(
		path.join(tmpdir(), "kit-mermaid-previews-"),
	);
	const directory = await previewDirectoryPromise;
	if (!registeredExitCleanup) {
		registeredExitCleanup = true;
		process.once("exit", () => {
			try {
				rmSync(directory, { recursive: true, force: true });
			} catch {
				// Best-effort cleanup; the system viewer may still hold a file open.
			}
		});
	}
	return directory;
}

export async function openMermaidPreviewExternally(
	source: string,
	png: Uint8Array,
): Promise<void> {
	const digest = createHash("sha256").update(source).digest("hex").slice(0, 16);
	const directory = await previewDirectory();
	const previewPath = path.join(directory, `${digest}-${randomUUID()}.png`);
	await writeFile(previewPath, png, { mode: 0o600 });
	await openExternal(previewPath);
	const cleanup = setTimeout(() => {
		void rm(previewPath, { force: true }).catch(() => {
			// Best-effort cleanup; the system viewer may still hold the file open.
		});
	}, PREVIEW_RETENTION_MS);
	cleanup.unref();
}
