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

export type MessagePart = TextMessagePart | CodeReviewMessagePart;

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
			const lines = ["Attached code review:"];
			for (const file of part.review.files) {
				lines.push(`File: ${file.path}`);
				if (file.fileComment.trim()) {
					lines.push(`- File comment: ${file.fileComment.trim()}`);
				}
				for (const range of file.ranges) {
					const lineLabel =
						range.startLine === range.endLine
							? `${range.startLine}`
							: `${range.startLine}-${range.endLine}`;
					lines.push(`- ${range.side} ${lineLabel}: ${range.comment.trim()}`);
				}
			}
			return lines.join("\n");
		}
	}
}
