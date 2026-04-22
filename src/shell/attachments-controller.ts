import { type Accessor, createSignal } from "solid-js";

export type MessagePart = {
	type: string;
	[key: string]: unknown;
};

export interface Attachment {
	id: string;
	type: string;
	icon: string;
	summary: string;
	toMessagePart(): MessagePart;
}

export type AttachmentEvent =
	| { type: "attached"; attachment: Attachment }
	| { type: "detached"; id: string };

export interface AttachmentsController {
	attach(attachment: Attachment): void;
	detach(id: string): void;
	attachments: Accessor<Attachment[]>;
	subscribe(listener: (event: AttachmentEvent) => void): () => void;
}

export function createAttachmentsController(): AttachmentsController {
	const [attachments, setAttachments] = createSignal<Attachment[]>([]);
	const listeners = new Set<(event: AttachmentEvent) => void>();

	function emit(event: AttachmentEvent): void {
		for (const listener of listeners) {
			listener(event);
		}
	}

	return {
		attach(attachment) {
			setAttachments((prev) => {
				const next = prev.filter((item) => item.id !== attachment.id);
				next.push(attachment);
				return next;
			});
			emit({ type: "attached", attachment });
		},
		detach(id) {
			let removed = false;
			setAttachments((prev) => {
				const next = prev.filter((item) => item.id !== id);
				removed = next.length !== prev.length;
				return next;
			});
			if (!removed) return;
			emit({ type: "detached", id });
		},
		attachments,
		subscribe(listener) {
			listeners.add(listener);
			return () => {
				listeners.delete(listener);
			};
		},
	};
}
