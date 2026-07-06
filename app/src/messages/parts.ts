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

/**
 * Committed diff a review's comments refer to. When present, line
 * numbers are positions in `git diff <parentSha> <sha>`, not the working
 * tree. Covers both single commits (parent..commit) and branch diffs
 * (merge-base..head); `subject` names the target for humans.
 */
export type CodeReviewCommitRef = {
	sha: string;
	parentSha: string;
	subject: string;
};

export type CodeReviewMessagePart = {
	type: "code-review";
	review: {
		submittedAt: string;
		files: CodeReviewFileComment[];
		commit?: CodeReviewCommitRef;
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
			const commit = part.review.commit;
			// The subject is repo-influenceable text entering instructional
			// prompt context; clamp it so it stays a label, not a payload.
			const subject = commit
				? commit.subject.replace(/\s+/g, " ").trim().slice(0, 120)
				: "";
			const scope = commit
				? [
						`These comments are on the committed diff ${commit.parentSha}..${commit.sha} ("${subject}"),`,
						`not the working tree. Line numbers refer to that diff;`,
						`run \`git diff ${commit.parentSha} ${commit.sha}\` to see exactly what was reviewed.`,
					].join(" ")
				: "";
			const content = scope
				? `${scope}\n\n${fileBlocks.join("\n\n")}`
				: fileBlocks.join("\n\n");
			return renderTemplate("review-feedback", { content }) ?? "";
		}
		case "image":
			return part.filename
				? `Attached image: ${part.filename}`
				: "Attached image";
	}
}
