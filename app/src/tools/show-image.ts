import { open } from "node:fs/promises";
import { basename, resolve } from "node:path";
import { type ImageFormat, imageInfo } from "@opentui/core";
import type { AgentTool } from "../runtime/agent";
import { Type } from "../runtime/agent";

export const SHOW_IMAGE_TOOL_NAME = "show_image";

const MAX_IMAGE_BYTES = 16 * 1024 * 1024;
const MAX_IMAGE_PIXELS = 24_000_000;

const parameters = Type.Object({
	path: Type.String({
		description: "Path to the image file (relative to cwd or absolute)",
	}),
	caption: Type.Optional(
		Type.String({
			description: "Short caption shown with the image",
			maxLength: 200,
		}),
	),
});

const MIME_TYPES: Record<ImageFormat, string> = {
	png: "image/png",
	jpeg: "image/jpeg",
	webp: "image/webp",
	gif: "image/gif",
	"raw-rgba": "application/octet-stream",
};

export type ShowImageDetails = {
	presentation: "transcript-image";
	path: string;
	filename: string;
	caption?: string;
	mimeType: string;
	width: number;
	height: number;
	bytes: number;
};

export function createShowImageTool(
	cwd: string,
): AgentTool<typeof parameters, ShowImageDetails> {
	return {
		name: SHOW_IMAGE_TOOL_NAME,
		label: "Show image",
		description:
			"Display a local PNG, JPEG, WebP, or GIF image in the transcript. Use this after creating or capturing an image that the user should see.",
		parameters,
		async execute(_id, params, signal) {
			signal?.throwIfAborted();
			const path = resolve(cwd, params.path);
			const handle = await open(path, "r");
			let bytes: Buffer;
			try {
				const file = await handle.stat();
				if (!file.isFile()) throw new Error(`Not a file: ${path}`);
				if (file.size > MAX_IMAGE_BYTES) {
					throw new Error("Image exceeds the 16 MiB display limit");
				}
				const expectedSize = file.size;
				const allocation = Buffer.allocUnsafe(expectedSize + 1);
				let offset = 0;
				while (offset < allocation.byteLength) {
					signal?.throwIfAborted();
					const { bytesRead } = await handle.read(
						allocation,
						offset,
						allocation.byteLength - offset,
						offset,
					);
					if (bytesRead === 0) break;
					offset += bytesRead;
				}
				if (offset !== expectedSize) {
					throw new Error("Image changed while it was being read");
				}
				bytes = allocation.subarray(0, offset);
			} finally {
				await handle.close();
			}
			signal?.throwIfAborted();
			const info = imageInfo(bytes);
			if (info.format === "raw-rgba") {
				throw new Error("Raw RGBA images are not supported by show_image");
			}
			if (info.width * info.height > MAX_IMAGE_PIXELS) {
				throw new Error("Image exceeds the 24 megapixel display limit");
			}

			const filename = basename(path);
			const mimeType = MIME_TYPES[info.format];
			const caption = params.caption?.trim();
			return {
				content: [
					{
						type: "text",
						text: caption
							? `Displayed image: ${filename} — ${caption}`
							: `Displayed image: ${filename}`,
					},
					{
						type: "image",
						data: bytes.toString("base64"),
						mimeType,
					},
				],
				details: {
					presentation: "transcript-image",
					path,
					filename,
					...(caption ? { caption } : {}),
					mimeType,
					width: info.width,
					height: info.height,
					bytes: bytes.byteLength,
				},
			};
		},
	};
}
