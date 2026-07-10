import { type Accessor, createSignal } from "solid-js";
import type { MessagePart } from "../messages/parts";
import { EventBus } from "../runtime/event-bus";

export type AttachmentDetachReason = "removed" | "pending" | "consumed";

export interface Attachment {
	id: string;
	type: string;
	icon: string;
	summary: string;
	toMessagePart(): MessagePart;
	toPromptText(): string;
	onDetach?(reason: AttachmentDetachReason): void;
}

export type AttachmentEventMap = {
	attached: { attachment: Attachment };
	detached: { id: string };
};

export type AttachmentEvent =
	| { type: "attached"; attachment: Attachment }
	| { type: "detached"; id: string };

export interface AttachmentsController {
	attach(attachment: Attachment): void;
	detach(id: string, reason?: AttachmentDetachReason): void;
	attachments: Accessor<Attachment[]>;
	subscribe(listener: (event: AttachmentEvent) => void): () => void;
}

export function createAttachmentsController(): AttachmentsController {
	const [attachments, setAttachments] = createSignal<Attachment[]>([]);
	const bus = new EventBus<AttachmentEventMap>();

	return {
		attach(attachment) {
			setAttachments((prev) => {
				const next = prev.filter((item) => item.id !== attachment.id);
				next.push(attachment);
				return next;
			});
			bus.publish("attached", { attachment });
		},
		detach(id, reason = "removed") {
			let removed: Attachment | undefined;
			setAttachments((prev) => {
				removed = prev.find((item) => item.id === id);
				return removed ? prev.filter((item) => item.id !== id) : prev;
			});
			if (!removed) return;
			removed.onDetach?.(reason);
			bus.publish("detached", { id });
		},
		attachments,
		subscribe(listener) {
			return bus.subscribe(listener);
		},
	};
}
