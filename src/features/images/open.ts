import { access } from "node:fs/promises";
import type { ImageMessagePart } from "../../messages/parts";

export type OpenImageResult = { ok: true } | { ok: false; message: string };

function getOpenCommand(path: string): string[] {
	if (process.platform === "darwin") return ["open", path];
	if (process.platform === "win32") return ["cmd", "/c", "start", "", path];
	return ["xdg-open", path];
}

export async function openImagePart(
	part: ImageMessagePart,
): Promise<OpenImageResult> {
	if (!part.sourcePath) {
		return { ok: false, message: "This image is not backed by a local file." };
	}

	try {
		await access(part.sourcePath);
	} catch {
		console.warn("[images] source image path no longer exists", {
			sourcePath: part.sourcePath,
		});
		return {
			ok: false,
			message: "The original image file is no longer available.",
		};
	}

	try {
		const [command, ...args] = getOpenCommand(part.sourcePath);
		const proc = Bun.spawn({
			cmd: [command, ...args],
			stdin: "ignore",
			stdout: "ignore",
			stderr: "ignore",
		});
		void proc.exited.catch((error) => {
			console.warn("[images] failed to open image", {
				sourcePath: part.sourcePath,
				error,
			});
		});
		return { ok: true };
	} catch (error) {
		console.warn("[images] failed to spawn image opener", {
			sourcePath: part.sourcePath,
			error,
		});
		return { ok: false, message: "Failed to launch the system image viewer." };
	}
}
