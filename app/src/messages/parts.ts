import { renderTemplate } from "../shell/templates";

export type TextMessagePart = {
	type: "text";
	text: string;
};

export type CodeReviewCommentRange = {
	side: "additions" | "deletions";
	startLine: number;
	endLine: number;
	comment: string;
};

export type CodeReviewFileComment = {
	path: string;
	fileComment: string;
	ranges: CodeReviewCommentRange[];
};

export type CodeReviewMessagePart = {
	type: "code-review";
	review: {
		submittedAt: string;
		files: CodeReviewFileComment[];
	};
};

export type ImageMessagePart = {
	type: "image";
	data: string;
	mimeType: string;
	filename?: string;
	sourcePath?: string;
};

export type MessagePart =
	| TextMessagePart
	| CodeReviewMessagePart
	| ImageMessagePart;

export type UserMultipartMessage = {
	role: "user";
	content: MessagePart[];
	timestamp: number;
};

export function messagePartToPromptText(part: MessagePart): string {
	switch (part.type) {
		case "text":
			return part.text;
		case "code-review": {
			const fileBlocks: string[] = [];
			for (const file of part.review.files) {
				const notes: string[] = [];
				if (file.fileComment.trim()) {
					notes.push(`- File comment: ${file.fileComment.trim()}`);
				}
				for (const range of file.ranges) {
					const lineLabel =
						range.startLine === range.endLine
							? `${range.startLine}`
							: `${range.startLine}-${range.endLine}`;
					notes.push(`- ${range.side} ${lineLabel}: ${range.comment.trim()}`);
				}
				if (notes.length === 0) continue;
				fileBlocks.push(`File: ${file.path}\n${notes.join("\n")}`);
			}
			if (fileBlocks.length === 0) return "";
			return (
				renderTemplate("review-feedback", {
					content: fileBlocks.join("\n\n"),
				}) ?? ""
			);
		}
		case "image":
			return part.filename
				? `Attached image: ${part.filename}`
				: "Attached image";
	}
}
