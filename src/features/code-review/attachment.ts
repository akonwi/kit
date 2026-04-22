import type {
	Attachment,
	MessagePart,
} from "../../shell/attachments-controller";

type CodeReviewSubmission = {
	submittedAt: string;
	files: Array<{
		path: string;
		fileComment: string;
		ranges: Array<{
			side: "additions" | "deletions";
			startLine: number;
			endLine: number;
			comment: string;
		}>;
	}>;
};

export class CodeReviewAttachment implements Attachment {
	readonly type = "code-review";
	readonly icon = "🧐";
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
		return {
			type: this.type,
			review: this.review,
		};
	}
}

export type { CodeReviewSubmission };
