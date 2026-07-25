import { describe, expect, test } from "bun:test";
import type { InternalPluginAPI } from "../../plugins";
import { createAttachmentsController } from "../../shell/attachments-controller";
import { PagerFeedbackAttachment } from "./attachment";
import { PagerPlugin } from "./index";
import type { PagerDraftSnapshot } from "./pager-controller";

const snapshot: PagerDraftSnapshot = {
	sourceId: "turn:1",
	title: "Response",
	sections: [{ title: "Response", sectionTitle: "", body: "Body" }],
	currentIndex: 0,
	notes: new Map([[0, "Feedback text"]]),
};

function createKit(
	attachments: ReturnType<typeof createAttachmentsController>,
): InternalPluginAPI {
	return {
		attachments,
		session: {
			get: () => ({ id: "session-1" }),
		},
		on: () => () => {},
		registerCommand: () => () => {},
	} as unknown as InternalPluginAPI;
}

function staleAttachment(onConsumed: () => void): PagerFeedbackAttachment {
	return new PagerFeedbackAttachment(
		"pager-feedback",
		"Feedback text",
		1,
		snapshot,
		{
			onDetach: (reason) => {
				if (reason === "consumed") onConsumed();
			},
			onOpen: () => {},
		},
	);
}

describe("PagerPlugin attachment reloads", () => {
	test("does not resurrect a pending attachment when the plugin reloads", () => {
		const attachments = createAttachmentsController();
		let consumed = false;
		const pending = staleAttachment(() => {
			consumed = true;
		});
		attachments.attach(pending);
		attachments.detach(pending.id, "pending");

		PagerPlugin(createKit(attachments));
		pending.onDetach("consumed");

		expect(consumed).toBe(true);
		expect(attachments.attachments()).toHaveLength(0);
	});

	test("rebinds a pending attachment when failed submission restores it", () => {
		const attachments = createAttachmentsController();
		const pending = staleAttachment(() => {});
		attachments.attach(pending);
		attachments.detach(pending.id, "pending");
		PagerPlugin(createKit(attachments));

		attachments.attach(pending);

		const rebound = attachments.attachments()[0];
		expect(rebound).toBeInstanceOf(PagerFeedbackAttachment);
		expect(rebound).not.toBe(pending);
		if (!(rebound instanceof PagerFeedbackAttachment)) {
			throw new Error("Expected rebound pager attachment");
		}
		expect(rebound.snapshot.notes.get(0)).toBe("Feedback text");
	});
});
