import { type Accessor, createSignal } from "solid-js";
import type { MessagePart } from "../messages/parts";
import { EventBus } from "../runtime/event-bus";

export interface Attachment {
	id: string;
	type: string;
	icon: string;
	summary: string;
	toMessagePart(): MessagePart;
	toPromptText(): string;
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
	detach(id: string): void;
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
		detach(id) {
			let removed = false;
			setAttachments((prev) => {
				const next = prev.filter((item) => item.id !== id);
				removed = next.length !== prev.length;
				return next;
			});
			if (!removed) return;
			bus.publish("detached", { id });
		},
		attachments,
		subscribe(listener) {
			return bus.subscribe(listener);
		},
	};
}
