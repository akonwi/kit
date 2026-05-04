import type {
	CodeReviewFileComment,
	CodeReviewMessagePart,
	MessagePart,
} from "../../messages/parts";
import { messagePartToPromptText } from "../../messages/parts";
import type { Attachment } from "../../shell/attachments-controller";

export type CodeReviewSubmission = {
	submittedAt: string;
	files: CodeReviewFileComment[];
};

export class CodeReviewAttachment implements Attachment {
	readonly type = "code-review";
	readonly icon = "◌";
	readonly summary: string;

	constructor(
		public readonly id: string,
		public readonly review: CodeReviewSubmission,
	) {
		const commentCount = review.files.reduce(
			(sum, file) =>
				sum + (file.fileComment.trim().length > 0 ? 1 : 0) + file.ranges.length,
			0,
		);
		this.summary = `Code review · ${commentCount} comment${commentCount === 1 ? "" : "s"} · ${review.files.length} file${review.files.length === 1 ? "" : "s"}`;
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
