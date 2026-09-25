import { describe, expect, test } from "bun:test";
import { createAttachmentsController } from "../../shell/attachments-controller";
import { CodeReviewAttachment } from "./attachment";

describe("code review draft attachment", () => {
	test("hides while pending and consumes only after submission succeeds", () => {
		const attachments = createAttachmentsController();
		const reasons: string[] = [];
		const attachment = new CodeReviewAttachment(
			"code-review",
			{
				submittedAt: new Date(0).toISOString(),
				files: [
					{
						path: "src/test.ts",
						fileComment: "Check this file",
						ranges: [],
					},
				],
			},
			{
				repoRoot: "/repo",
				targetKey: "working",
				onDetach: (reason) => reasons.push(reason),
			},
		);
		attachments.attach(attachment);

		expect(attachments.attachments()[0]?.summary).toContain(
			"Code review draft",
		);
		attachments.detach("code-review", "pending");
		expect(attachments.attachments()).toHaveLength(0);
		expect(reasons).toEqual([]);
		attachment.onDetach("consumed");
		expect(reasons).toEqual(["consumed"]);
	});

	test("labels full-file feedback with working-tree line locations", () => {
		const attachment = new CodeReviewAttachment(
			"file-review:/repo:src/test.ts",
			{
				submittedAt: new Date(0).toISOString(),
				source: "file",
				files: [
					{
						path: "src/test.ts",
						fileComment: "",
						ranges: [
							{
								side: "additions",
								startLine: 2,
								endLine: 4,
								comment: "Simplify this block",
							},
						],
					},
				],
			},
		);

		expect(attachment.summary).toContain("File feedback");
		expect(attachment.toPromptText()).toContain(
			"- Lines 2-4: Simplify this block",
		);
		expect(attachment.toPromptText()).not.toContain("additions 2-4");
	});
});
