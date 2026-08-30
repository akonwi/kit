import {
	isRecord,
	RemoteSessionServices,
	type RpcCommandClient,
} from "@akonwi/kit-session-client";

function isRemoteImage(file: File): boolean {
	const mimeType = file.type.split(";", 1)[0]?.trim().toLowerCase();
	if (mimeType) {
		return ["image/gif", "image/jpeg", "image/png", "image/webp"].includes(
			mimeType,
		);
	}
	return /\.(?:gif|jpe?g|png|webp)$/i.test(file.name);
}

export class WebRemoteServices extends RemoteSessionServices {
	constructor(
		rpc: RpcCommandClient,
		// WebKit requires fetch to retain its global receiver.
		private readonly fetcher: typeof fetch = globalThis.fetch.bind(globalThis),
	) {
		super(rpc);
	}

	validateAttachments(
		existing: ReadonlyArray<{ file: File }>,
		files: readonly File[],
	): string | null {
		const limits = this.currentLimits();
		if (existing.length + files.length > limits.maxAttachmentsPerPrompt) {
			return `A prompt supports at most ${limits.maxAttachmentsPerPrompt} attachments`;
		}
		if (files.some((file) => file.size > limits.maxAttachmentBytes)) {
			return "An attachment exceeds the server file-size limit";
		}
		if (
			files.some(
				(file) =>
					!isRemoteImage(file) && file.size > limits.maxTextAttachmentBytes,
			)
		) {
			return "A text attachment exceeds the server size limit";
		}
		const allFiles = [...existing.map(({ file }) => file), ...files];
		const totalBytes = allFiles.reduce((sum, file) => sum + file.size, 0);
		if (totalBytes > limits.maxPromptAttachmentBytes) {
			return "Attachments exceed the server prompt-size limit";
		}
		const totalTextBytes = allFiles
			.filter((file) => !isRemoteImage(file))
			.reduce((sum, file) => sum + file.size, 0);
		return totalTextBytes > limits.maxPromptTextBytes
			? "Text attachments exceed the server prompt-size limit"
			: null;
	}

	async removeAttachment(id: string): Promise<void> {
		const response = await this.fetcher(
			`/api/attachments/${encodeURIComponent(id)}`,
			{ method: "DELETE" },
		);
		if (!response.ok && response.status !== 404) {
			throw new Error(`Attachment removal failed (${response.status})`);
		}
	}

	async uploadAttachment(file: File): Promise<string> {
		const form = new FormData();
		form.append("file", file);
		const response = await this.fetcher("/api/attachments", {
			method: "POST",
			body: form,
		});
		const payload: unknown = await response.json();
		if (!response.ok || !isRecord(payload) || !isRecord(payload.attachment)) {
			throw new Error(
				isRecord(payload) && typeof payload.error === "string"
					? payload.error
					: `Attachment upload failed (${response.status})`,
			);
		}
		if (typeof payload.attachment.id !== "string") {
			throw new Error("Attachment upload returned no id");
		}
		return payload.attachment.id;
	}
}
