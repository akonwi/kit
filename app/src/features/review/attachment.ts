import type {
	CodeReviewCommitRef,
	CodeReviewFileComment,
	CodeReviewMessagePart,
	MessagePart,
} from "../../messages/parts";
import { messagePartToPromptText } from "../../messages/parts";
import type {
	Attachment,
	AttachmentDetachReason,
} from "../../shell/attachments-controller";
import { PENCIL } from "../../shell/glyphs";

export type CodeReviewSubmission = {
	submittedAt: string;
	files: CodeReviewFileComment[];
	/** Present when the review targets a commit instead of the working tree. */
	commit?: CodeReviewCommitRef;
};

export type CodeReviewDraftAttachmentOptions = {
	repoRoot: string;
	targetKey: string;
	onDetach: (reason: AttachmentDetachReason) => void;
};

export class CodeReviewAttachment implements Attachment {
	readonly type = "code-review";
	readonly icon = PENCIL;
	readonly summary: string;

	constructor(
		public readonly id: string,
		public readonly review: CodeReviewSubmission,
		public readonly draft?: CodeReviewDraftAttachmentOptions,
	) {
		const commentCount = review.files.reduce(
			(sum, file) =>
				sum + (file.fileComment.trim().length > 0 ? 1 : 0) + file.ranges.length,
			0,
		);
		// Shas are stored full-length; shorten for the chip label.
		const scope = review.commit ? ` · ${review.commit.sha.slice(0, 7)}` : "";
		this.summary = `Code review${draft ? " draft" : ""}${scope} · ${commentCount} comment${commentCount === 1 ? "" : "s"} · ${review.files.length} file${review.files.length === 1 ? "" : "s"}`;
	}

	onDetach(reason: AttachmentDetachReason): void {
		// Composer removes attachments immediately while message submission is
		// pending. Keep the editable draft until submission succeeds; a failed
		// submission reattaches this same attachment.
		if (reason !== "pending") this.draft?.onDetach(reason);
	}

	toMessagePart(): MessagePart {
		const part: CodeReviewMessagePart = {
			type: this.type,
			review: this.review,
		};
		return part;
	}

	toPromptText(): string {
		return messagePartToPromptText(this.toMessagePart());
	}
}
