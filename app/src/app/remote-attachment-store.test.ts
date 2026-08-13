import { describe, expect, test } from "bun:test";
import { crc32, deflateSync } from "node:zlib";
import {
	MAX_REMOTE_ATTACHMENTS,
	MAX_REMOTE_TEXT_ATTACHMENT_BYTES,
	RemoteAttachmentStore,
} from "./remote-attachment-store";

function pngBytes(): Buffer {
	const chunk = (type: string, data = Buffer.alloc(0)) => {
		const output = Buffer.alloc(12 + data.length);
		output.writeUInt32BE(data.length, 0);
		output.write(type, 4, "ascii");
		data.copy(output, 8);
		output.writeUInt32BE(
			crc32(output.subarray(4, 8 + data.length)),
			8 + data.length,
		);
		return output;
	};
	const header = Buffer.alloc(13);
	header.writeUInt32BE(1, 0);
	header.writeUInt32BE(1, 4);
	header[8] = 8;
	header[9] = 6;
	return Buffer.concat([
		Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
		chunk("IHDR", header),
		chunk("IDAT", deflateSync(Buffer.from([0, 0, 0, 0, 0]))),
		chunk("IEND"),
	]);
}

describe("RemoteAttachmentStore", () => {
	test("stores UTF-8 files as one-shot prompt text", async () => {
		const store = new RemoteAttachmentStore();
		const metadata = await store.add(
			new File(["const answer = 42;"], "../example.ts", {
				type: "text/typescript",
			}),
		);
		expect(metadata).toMatchObject({
			filename: "example.ts",
			mimeType: "text/typescript",
			kind: "text",
		});

		expect(
			Buffer.from(store.download(metadata.id)?.bytes ?? []).toString(),
		).toBe("const answer = 42;");
		const claim = store.claim([metadata.id]);
		expect(claim.parts).toEqual([
			{
				type: "text",
				text: '<uploaded_file filename="example.ts" mime_type="text/typescript">\nconst answer = 42;\n</uploaded_file>',
			},
		]);
		expect(() => store.claim([metadata.id])).toThrow(
			`Attachment is unavailable: ${metadata.id}`,
		);
		claim.release();
		const retry = store.claim([metadata.id]);
		retry.commit();
		expect(store.download(metadata.id)).toBeNull();
		expect(() => store.claim([metadata.id])).toThrow(
			`Attachment is unavailable: ${metadata.id}`,
		);
		store.dispose();
	});

	test("stores verified images as base64 image parts", async () => {
		const store = new RemoteAttachmentStore();
		const bytes = pngBytes();
		const metadata = await store.add(
			new File([new Uint8Array(bytes)], "pixel.png", { type: "image/png" }),
		);
		const claim = store.claim([metadata.id]);
		expect(metadata.kind).toBe("image");
		expect(claim.parts).toEqual([
			{
				type: "image",
				data: bytes.toString("base64"),
				mimeType: "image/png",
				filename: "pixel.png",
				attachmentId: metadata.id,
			},
		]);
		claim.commit();
		expect(store.download(metadata.id)?.metadata.id).toBe(metadata.id);
		expect(store.remove(metadata.id)).toBe(true);
		expect(store.download(metadata.id)).toBeNull();
		store.dispose();
	});

	test("rejects oversized text and unsupported binary files", async () => {
		const store = new RemoteAttachmentStore();
		await expect(
			store.add(
				new File(
					[new Uint8Array(MAX_REMOTE_TEXT_ATTACHMENT_BYTES + 1)],
					"large.txt",
					{
						type: "text/plain",
					},
				),
			),
		).rejects.toEqual(expect.objectContaining({ status: 413 }));
		await expect(
			store.add(
				new File([new Uint8Array([0, 1, 2])], "archive.zip", {
					type: "application/zip",
				}),
			),
		).rejects.toEqual(expect.objectContaining({ status: 415 }));
		await expect(
			store.add(
				new File(
					[new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10])],
					"truncated.png",
					{ type: "image/png" },
				),
			),
		).rejects.toEqual(expect.objectContaining({ status: 415 }));
		store.dispose();
	});

	test("reserves capacity before concurrent file reads finish", async () => {
		let releaseReads: (() => void) | undefined;
		const readsReleased = new Promise<void>((resolve) => {
			releaseReads = resolve;
		});
		class DelayedFile extends File {
			override async arrayBuffer(): Promise<ArrayBuffer> {
				await readsReleased;
				return super.arrayBuffer();
			}
		}
		const store = new RemoteAttachmentStore();
		const uploads = Array.from(
			{ length: MAX_REMOTE_ATTACHMENTS + 1 },
			(_, index) => store.add(new DelayedFile(["x"], `${index}.txt`)),
		);
		await expect(uploads.at(-1)).rejects.toEqual(
			expect.objectContaining({ status: 429 }),
		);
		releaseReads?.();
		await Promise.all(uploads.slice(0, -1));
		store.dispose();
	});

	test("counts retained images until they are explicitly released", async () => {
		const store = new RemoteAttachmentStore();
		let firstId = "";
		for (let index = 0; index < MAX_REMOTE_ATTACHMENTS; index += 1) {
			const attachment = await store.add(
				new File([new Uint8Array(pngBytes())], `${index}.png`, {
					type: "image/png",
				}),
			);
			if (index === 0) firstId = attachment.id;
			store.claim([attachment.id]).commit();
		}
		await expect(
			store.add(
				new File([new Uint8Array(pngBytes())], "overflow.png", {
					type: "image/png",
				}),
			),
		).rejects.toEqual(expect.objectContaining({ status: 429 }));
		expect(store.remove(firstId)).toBe(true);
		await expect(
			store.add(
				new File([new Uint8Array(pngBytes())], "replacement.png", {
					type: "image/png",
				}),
			),
		).resolves.toEqual(expect.objectContaining({ kind: "image" }));
		store.dispose();
	});

	test("enforces aggregate prompt limits before claiming", async () => {
		const store = new RemoteAttachmentStore();
		const content = "x".repeat(600 * 1024);
		const first = await store.add(new File([content], "first.txt"));
		const second = await store.add(new File([content], "second.txt"));
		expect(() => store.claim([first.id, second.id])).toThrow(
			"Prompt text attachments exceed the 1 MiB aggregate limit",
		);
		store.remove(first.id);
		store.remove(second.id);
		store.dispose();
	});

	test("validates claims atomically", async () => {
		const store = new RemoteAttachmentStore();
		const first = await store.add(new File(["one"], "one.txt"));
		const second = await store.add(new File(["two"], "two.txt"));
		expect(() => store.claim([first.id, "missing"])).toThrow(
			"Attachment is unavailable: missing",
		);
		const claim = store.claim([first.id, second.id]);
		claim.commit();
		store.dispose();
	});
});
