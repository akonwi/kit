import { describe, expect, test } from "bun:test";
import { createAttachmentsController } from "../../shell/attachments-controller";
import { PagerFeedbackAttachment } from "./attachment";

const snapshot = {
	sourceId: "turn:1",
	title: "Response",
	sections: [{ title: "Response", sectionTitle: "", body: "Body" }],
	currentIndex: 0,
	notes: new Map([[0, "Feedback text"]]),
};

describe("pager feedback draft attachment", () => {
	test("retains drafts while pending and clears them after consumption", () => {
		const attachments = createAttachmentsController();
		const reasons: string[] = [];
		const attachment = new PagerFeedbackAttachment(
			"pager-feedback",
			"Feedback text",
			2,
			snapshot,
			{
				onDetach: (reason) => reasons.push(reason),
				onOpen: () => {},
			},
		);
		attachments.attach(attachment);

		expect(attachment.summary).toBe("Pager feedback draft · 2 notes");
		expect(attachment.toMessagePart()).toEqual({
			type: "text",
			text: "Feedback text",
		});

		attachments.detach(attachment.id, "pending");
		expect(reasons).toEqual([]);
		attachment.onDetach("consumed");
		expect(reasons).toEqual(["consumed"]);
	});

	test("opens the retained pager draft", async () => {
		let opened = false;
		const attachment = new PagerFeedbackAttachment(
			"pager-feedback",
			"Feedback text",
			1,
			snapshot,
			{
				onDetach: () => {},
				onOpen: () => {
					opened = true;
				},
			},
		);

		await attachment.onOpen();
		expect(opened).toBe(true);
	});
});
