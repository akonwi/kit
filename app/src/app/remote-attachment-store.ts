import { isUtf8 } from "node:buffer";
import { randomUUID } from "node:crypto";
import path from "node:path";
import { crc32, inflateSync } from "node:zlib";
import type { MessagePart } from "../messages/parts";

export const MAX_REMOTE_ATTACHMENT_BYTES = 10 * 1024 * 1024;
export const MAX_REMOTE_TEXT_ATTACHMENT_BYTES = 1024 * 1024;
export const MAX_REMOTE_ATTACHMENT_TOTAL_BYTES = 50 * 1024 * 1024;
export const MAX_REMOTE_ATTACHMENTS = 32;
export const MAX_REMOTE_ATTACHMENTS_PER_PROMPT = 8;
export const MAX_REMOTE_PROMPT_ATTACHMENT_BYTES = 20 * 1024 * 1024;
export const MAX_REMOTE_PROMPT_TEXT_BYTES = 1024 * 1024;
const MIN_ACCEPTED_ATTACHMENT_QUOTA_BYTES = 64 * 1024;
const MAX_DECODED_IMAGE_BYTES = 48 * 1024 * 1024;
const MAX_DECODED_IMAGE_PIXELS = 12_000_000;

const IMAGE_MIME_TYPES = new Set([
	"image/gif",
	"image/jpeg",
	"image/png",
	"image/webp",
]);

const MIME_TYPES_BY_EXTENSION: Record<string, string> = {
	".css": "text/css",
	".csv": "text/csv",
	".gif": "image/gif",
	".go": "text/plain",
	".html": "text/html",
	".jpeg": "image/jpeg",
	".jpg": "image/jpeg",
	".js": "text/javascript",
	".json": "application/json",
	".jsx": "text/javascript",
	".md": "text/markdown",
	".png": "image/png",
	".py": "text/x-python",
	".rb": "text/plain",
	".rs": "text/plain",
	".sh": "text/x-shellscript",
	".svg": "image/svg+xml",
	".ts": "text/typescript",
	".tsx": "text/typescript",
	".txt": "text/plain",
	".webp": "image/webp",
	".xml": "application/xml",
	".yaml": "application/yaml",
	".yml": "application/yaml",
};

export type RemoteAttachmentMetadata = {
	id: string;
	filename: string;
	mimeType: string;
	size: number;
	kind: "image" | "text";
	createdAt: string;
};

export class RemoteAttachmentError extends Error {
	constructor(
		message: string,
		readonly status: number,
	) {
		super(message);
	}
}

type StoredAttachment = {
	metadata: RemoteAttachmentMetadata;
	part: MessagePart;
	bytes: Uint8Array<ArrayBuffer>;
	quotaBytes: number;
};

export type RemoteAttachmentDownload = {
	metadata: RemoteAttachmentMetadata;
	bytes: Uint8Array<ArrayBuffer>;
};

export type RemoteAttachmentClaim = {
	parts: MessagePart[];
	commit(): void;
	release(): void;
};

function cleanFilename(filename: string): string {
	const name = [...path.basename(filename)]
		.filter((character) => {
			const code = character.charCodeAt(0);
			return code > 31 && code !== 127;
		})
		.join("")
		.trim();
	return (name || "attachment").slice(0, 255);
}

function declaredMimeType(file: File, filename: string): string {
	const declared = file.type.split(";", 1)[0]?.trim().toLowerCase();
	if (declared) return declared;
	return (
		MIME_TYPES_BY_EXTENSION[path.extname(filename).toLowerCase()] ??
		"application/octet-stream"
	);
}

function validImageDimensions(width: number, height: number): boolean {
	return (
		width > 0 &&
		height > 0 &&
		width <= 8192 &&
		height <= 8192 &&
		width * height <= MAX_DECODED_IMAGE_PIXELS
	);
}

function isPng(bytes: Buffer): boolean {
	if (
		bytes.length < 57 ||
		!bytes.subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]))
	) {
		return false;
	}
	let offset = 8;
	let expectedInflatedBytes = 0;
	const imageData: Buffer[] = [];
	while (offset + 12 <= bytes.length) {
		const length = bytes.readUInt32BE(offset);
		const type = bytes.subarray(offset + 4, offset + 8).toString("ascii");
		const next = offset + 12 + length;
		if (next > bytes.length) return false;
		const chunkBytes = bytes.subarray(offset + 4, offset + 8 + length);
		if (crc32(chunkBytes) !== bytes.readUInt32BE(offset + 8 + length)) {
			return false;
		}
		if (offset === 8) {
			if (type !== "IHDR" || length !== 13) return false;
			const width = bytes.readUInt32BE(offset + 8);
			const height = bytes.readUInt32BE(offset + 12);
			const bitDepth = bytes[offset + 16];
			const colorType = bytes[offset + 17];
			const channels =
				colorType === 0
					? 1
					: colorType === 2
						? 3
						: colorType === 3
							? 1
							: colorType === 4
								? 2
								: colorType === 6
									? 4
									: 0;
			const validBitDepth =
				(colorType === 0 && [1, 2, 4, 8, 16].includes(bitDepth)) ||
				(colorType === 2 && [8, 16].includes(bitDepth)) ||
				(colorType === 3 && [1, 2, 4, 8].includes(bitDepth)) ||
				([4, 6].includes(colorType) && [8, 16].includes(bitDepth));
			if (
				!validImageDimensions(width, height) ||
				!validBitDepth ||
				channels === 0 ||
				bytes[offset + 18] !== 0 ||
				bytes[offset + 19] !== 0 ||
				bytes[offset + 20] !== 0
			) {
				return false;
			}
			expectedInflatedBytes =
				(Math.ceil((width * channels * bitDepth) / 8) + 1) * height;
			if (expectedInflatedBytes > MAX_DECODED_IMAGE_BYTES) return false;
		}
		if (type === "IDAT") {
			imageData.push(bytes.subarray(offset + 8, offset + 8 + length));
		}
		if (type === "IEND") {
			if (
				length !== 0 ||
				next !== bytes.length ||
				expectedInflatedBytes === 0 ||
				imageData.length === 0
			) {
				return false;
			}
			try {
				return (
					inflateSync(Buffer.concat(imageData), {
						maxOutputLength: expectedInflatedBytes + 1,
					}).length === expectedInflatedBytes
				);
			} catch {
				return false;
			}
		}
		offset = next;
	}
	return false;
}

function hasJpegEntropyData(bytes: Buffer, start: number): boolean {
	let offset = start;
	let sawData = false;
	while (offset < bytes.length - 2) {
		if (bytes[offset] !== 0xff) {
			sawData = true;
			offset += 1;
			continue;
		}
		const marker = bytes[offset + 1];
		if (marker === 0x00) {
			sawData = true;
			offset += 2;
			continue;
		}
		if (marker !== undefined && marker >= 0xd0 && marker <= 0xd7) {
			offset += 2;
			continue;
		}
		return false;
	}
	return sawData;
}

function isJpeg(bytes: Buffer): boolean {
	if (
		bytes.length < 16 ||
		bytes[0] !== 0xff ||
		bytes[1] !== 0xd8 ||
		bytes.at(-2) !== 0xff ||
		bytes.at(-1) !== 0xd9
	) {
		return false;
	}
	let offset = 2;
	let dimensionsValid = false;
	while (offset + 3 < bytes.length - 2) {
		if (bytes[offset] !== 0xff) return false;
		while (bytes[offset] === 0xff) offset += 1;
		const marker = bytes[offset];
		offset += 1;
		if (marker === undefined) return false;
		if (marker === 0x01 || (marker >= 0xd0 && marker <= 0xd8)) continue;
		if (offset + 2 > bytes.length) return false;
		const length = bytes.readUInt16BE(offset);
		if (length < 2 || offset + length > bytes.length - 2) return false;
		if (marker === 0xda) {
			return (
				dimensionsValid &&
				length >= 6 &&
				hasJpegEntropyData(bytes, offset + length)
			);
		}
		if (marker === 0xc0 || marker === 0xc1) {
			if (length < 7) return false;
			dimensionsValid = validImageDimensions(
				bytes.readUInt16BE(offset + 5),
				bytes.readUInt16BE(offset + 3),
			);
		} else if (
			marker >= 0xc0 &&
			marker <= 0xcf &&
			![0xc4, 0xc8, 0xcc].includes(marker)
		) {
			return false;
		}
		offset += length;
	}
	return false;
}

function skipGifSubBlocks(bytes: Buffer, start: number): number {
	let offset = start;
	while (offset < bytes.length) {
		const length = bytes[offset];
		if (length === undefined) return -1;
		offset += 1;
		if (length === 0) return offset;
		if (offset + length > bytes.length) return -1;
		offset += length;
	}
	return -1;
}

function isGif(bytes: Buffer): boolean {
	const signature = bytes.subarray(0, 6).toString("ascii");
	if (
		bytes.length < 14 ||
		(signature !== "GIF87a" && signature !== "GIF89a") ||
		!validImageDimensions(bytes.readUInt16LE(6), bytes.readUInt16LE(8))
	) {
		return false;
	}
	let offset = 13;
	if ((bytes[10] & 0x80) !== 0) {
		offset += 3 * 2 ** ((bytes[10] & 0x07) + 1);
	}
	let sawImage = false;
	let totalFramePixels = 0;
	while (offset < bytes.length) {
		const marker = bytes[offset];
		offset += 1;
		if (marker === 0x3b) return sawImage && offset === bytes.length;
		if (marker === 0x21) {
			if (offset >= bytes.length) return false;
			offset = skipGifSubBlocks(bytes, offset + 1);
			if (offset < 0) return false;
			continue;
		}
		if (marker !== 0x2c || offset + 9 > bytes.length) return false;
		const frameWidth = bytes.readUInt16LE(offset + 4);
		const frameHeight = bytes.readUInt16LE(offset + 6);
		if (!validImageDimensions(frameWidth, frameHeight)) return false;
		totalFramePixels += frameWidth * frameHeight;
		if (totalFramePixels > MAX_DECODED_IMAGE_PIXELS) return false;
		sawImage = true;
		const packed = bytes[offset + 8];
		offset += 9;
		if ((packed & 0x80) !== 0) offset += 3 * 2 ** ((packed & 0x07) + 1);
		if (offset >= bytes.length) return false;
		offset = skipGifSubBlocks(bytes, offset + 1);
		if (offset < 0) return false;
	}
	return false;
}

function readUInt24LE(bytes: Buffer, offset: number): number {
	return bytes[offset] | (bytes[offset + 1] << 8) | (bytes[offset + 2] << 16);
}

function webpDimensions(
	bytes: Buffer,
	type: string,
	offset: number,
): [number, number] | null {
	if (type === "VP8X" && offset + 10 <= bytes.length) {
		return [
			readUInt24LE(bytes, offset + 4) + 1,
			readUInt24LE(bytes, offset + 7) + 1,
		];
	}
	if (
		type === "VP8 " &&
		offset + 10 <= bytes.length &&
		bytes
			.subarray(offset + 3, offset + 6)
			.equals(Buffer.from([0x9d, 0x01, 0x2a]))
	) {
		return [
			bytes.readUInt16LE(offset + 6) & 0x3fff,
			bytes.readUInt16LE(offset + 8) & 0x3fff,
		];
	}
	if (type === "VP8L" && offset + 5 <= bytes.length && bytes[offset] === 0x2f) {
		const packed = bytes.readUInt32LE(offset + 1);
		return [(packed & 0x3fff) + 1, ((packed >> 14) & 0x3fff) + 1];
	}
	return null;
}

function isWebp(bytes: Buffer): boolean {
	if (
		bytes.length < 20 ||
		bytes.subarray(0, 4).toString("ascii") !== "RIFF" ||
		bytes.readUInt32LE(4) + 8 !== bytes.length ||
		bytes.subarray(8, 12).toString("ascii") !== "WEBP"
	) {
		return false;
	}
	let offset = 12;
	let dimensionsValid = false;
	let sawImageData = false;
	while (offset + 8 <= bytes.length) {
		const type = bytes.subarray(offset, offset + 4).toString("ascii");
		const length = bytes.readUInt32LE(offset + 4);
		const dataOffset = offset + 8;
		const next = dataOffset + length + (length % 2);
		if (next > bytes.length) return false;
		const dimensions = webpDimensions(bytes, type, dataOffset);
		if (dimensions) {
			if (!validImageDimensions(...dimensions)) return false;
			dimensionsValid = true;
			if (type === "VP8 " || type === "VP8L") sawImageData = true;
		}
		offset = next;
	}
	return offset === bytes.length && dimensionsValid && sawImageData;
}

function isImage(bytes: Buffer, mimeType: string): boolean {
	switch (mimeType) {
		case "image/png":
			return isPng(bytes);
		case "image/jpeg":
			return isJpeg(bytes);
		case "image/gif":
			return isGif(bytes);
		case "image/webp":
			return isWebp(bytes);
		default:
			return false;
	}
}

function textPrompt(filename: string, mimeType: string, text: string): string {
	return [
		`<uploaded_file filename=${JSON.stringify(filename)} mime_type=${JSON.stringify(mimeType)}>`,
		text,
		"</uploaded_file>",
	].join("\n");
}

export class RemoteAttachmentStore {
	private readonly available = new Map<string, StoredAttachment>();
	private readonly claimed = new Map<string, StoredAttachment>();
	private readonly retained = new Map<string, StoredAttachment>();
	private totalBytes = 0;
	private reservedBytes = 0;
	private reservedUploads = 0;
	private disposed = false;

	async add(file: File): Promise<RemoteAttachmentMetadata> {
		if (this.disposed)
			throw new RemoteAttachmentError("Attachment store is closed", 503);
		if (file.size === 0)
			throw new RemoteAttachmentError("Attachment must not be empty", 400);
		if (file.size > MAX_REMOTE_ATTACHMENT_BYTES) {
			throw new RemoteAttachmentError(
				"Attachment exceeds the 10 MiB limit",
				413,
			);
		}
		if (
			this.available.size +
				this.claimed.size +
				this.retained.size +
				this.reservedUploads >=
			MAX_REMOTE_ATTACHMENTS
		) {
			throw new RemoteAttachmentError("Attachment count limit reached", 429);
		}
		const quotaBytes = Math.max(file.size, MIN_ACCEPTED_ATTACHMENT_QUOTA_BYTES);
		if (
			this.totalBytes + this.reservedBytes + quotaBytes >
			MAX_REMOTE_ATTACHMENT_TOTAL_BYTES
		) {
			throw new RemoteAttachmentError("Attachment storage limit reached", 429);
		}

		this.reservedUploads += 1;
		this.reservedBytes += quotaBytes;
		try {
			const filename = cleanFilename(file.name);
			const mimeType = declaredMimeType(file, filename);
			const arrayBuffer = await file.arrayBuffer();
			const rawBytes = new Uint8Array(arrayBuffer);
			const bytes = Buffer.from(arrayBuffer);
			if (this.disposed) {
				throw new RemoteAttachmentError("Attachment store is closed", 503);
			}
			const id = randomUUID();
			let kind: RemoteAttachmentMetadata["kind"];
			let part: MessagePart;
			let textContent: string | undefined;
			if (IMAGE_MIME_TYPES.has(mimeType)) {
				if (!isImage(bytes, mimeType)) {
					throw new RemoteAttachmentError(
						"Attachment does not match its image type",
						415,
					);
				}
				kind = "image";
				part = {
					type: "image",
					data: bytes.toString("base64"),
					mimeType,
					filename,
					attachmentId: id,
				};
			} else {
				if (file.size > MAX_REMOTE_TEXT_ATTACHMENT_BYTES) {
					throw new RemoteAttachmentError(
						"Text attachment exceeds the 1 MiB limit",
						413,
					);
				}
				if (!isUtf8(bytes) || bytes.includes(0)) {
					throw new RemoteAttachmentError(
						"Unsupported binary attachment type",
						415,
					);
				}
				kind = "text";
				textContent = bytes.toString("utf8");
				part = {
					type: "text",
					text: textPrompt(filename, mimeType, textContent),
				};
			}

			const metadata: RemoteAttachmentMetadata = {
				id,
				filename,
				mimeType,
				size: file.size,
				kind,
				createdAt: new Date().toISOString(),
			};
			this.available.set(metadata.id, {
				metadata,
				part,
				bytes: rawBytes,
				quotaBytes,
			});
			this.totalBytes += quotaBytes;
			return metadata;
		} finally {
			this.reservedUploads -= 1;
			this.reservedBytes -= quotaBytes;
		}
	}

	remove(id: string): boolean {
		const available = this.available.get(id);
		const attachment = available ?? this.retained.get(id);
		if (!attachment) return false;
		this.available.delete(id);
		this.retained.delete(id);
		if (available) this.totalBytes -= attachment.quotaBytes;
		return true;
	}

	download(id: string): RemoteAttachmentDownload | null {
		const attachment =
			this.available.get(id) ?? this.claimed.get(id) ?? this.retained.get(id);
		if (!attachment) return null;
		return { metadata: attachment.metadata, bytes: attachment.bytes };
	}

	claim(ids: string[]): RemoteAttachmentClaim {
		if (this.disposed) throw new Error("Attachment store is closed");
		if (ids.length > MAX_REMOTE_ATTACHMENTS_PER_PROMPT) {
			throw new Error(
				`A prompt may reference at most ${MAX_REMOTE_ATTACHMENTS_PER_PROMPT} attachments`,
			);
		}
		if (new Set(ids).size !== ids.length) {
			throw new Error("attachmentIds must not contain duplicates");
		}
		const attachments = ids.map((id) => {
			const attachment = this.available.get(id);
			if (!attachment) throw new Error(`Attachment is unavailable: ${id}`);
			return attachment;
		});
		const totalBytes = attachments.reduce(
			(total, attachment) => total + attachment.metadata.size,
			0,
		);
		const textBytes = attachments.reduce(
			(total, attachment) =>
				total +
				(attachment.metadata.kind === "text" ? attachment.metadata.size : 0),
			0,
		);
		if (totalBytes > MAX_REMOTE_PROMPT_ATTACHMENT_BYTES) {
			throw new Error("Prompt attachments exceed the 20 MiB aggregate limit");
		}
		if (textBytes > MAX_REMOTE_PROMPT_TEXT_BYTES) {
			throw new Error(
				"Prompt text attachments exceed the 1 MiB aggregate limit",
			);
		}
		for (const attachment of attachments) {
			this.available.delete(attachment.metadata.id);
			this.claimed.set(attachment.metadata.id, attachment);
		}

		let settled = false;
		return {
			parts: attachments.map((attachment) => attachment.part),
			commit: () => {
				if (settled) return;
				settled = true;
				for (const attachment of attachments) {
					this.claimed.delete(attachment.metadata.id);
					if (!this.disposed && attachment.metadata.kind === "image") {
						this.retained.set(attachment.metadata.id, attachment);
					}
				}
			},
			release: () => {
				if (settled) return;
				settled = true;
				for (const attachment of attachments) {
					this.claimed.delete(attachment.metadata.id);
					if (!this.disposed) {
						this.available.set(attachment.metadata.id, attachment);
					}
				}
			},
		};
	}

	dispose(): void {
		if (this.disposed) return;
		this.disposed = true;
		this.available.clear();
		this.claimed.clear();
		this.retained.clear();
		this.totalBytes = 0;
	}
}
