import type { MessagePart } from "../../messages/parts";
import type {
	Attachment,
	AttachmentDetachReason,
} from "../../shell/attachments-controller";
import { PENCIL } from "../../shell/glyphs";
import type { PagerDraftSnapshot } from "./pager-controller";

export type PagerFeedbackDraftAttachmentOptions = {
	onDetach: (reason: AttachmentDetachReason) => void;
	onOpen: () => void | Promise<void>;
};

export class PagerFeedbackAttachment implements Attachment {
	readonly type = "pager-feedback";
	readonly icon = PENCIL;
	readonly summary: string;

	constructor(
		public readonly id: string,
		public readonly feedback: string,
		noteCount: number,
		public readonly snapshot: PagerDraftSnapshot,
		private readonly draft: PagerFeedbackDraftAttachmentOptions,
	) {
		this.summary = `Pager feedback draft · ${noteCount} note${noteCount === 1 ? "" : "s"}`;
	}

	onDetach(reason: AttachmentDetachReason): void {
		// Pending removal is provisional. The composer reattaches this object if
		// submission fails, so retain the in-memory pager draft until consumption.
		if (reason !== "pending") this.draft.onDetach(reason);
	}

	onOpen(): void | Promise<void> {
		return this.draft.onOpen();
	}

	toMessagePart(): MessagePart {
		return { type: "text", text: this.feedback };
	}

	toPromptText(): string {
		return this.feedback;
	}
}
