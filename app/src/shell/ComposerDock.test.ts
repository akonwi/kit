import { describe, expect, test } from "bun:test";
import type { MessagePart } from "../messages/parts";
import type { Attachment } from "./attachments-controller";
import { findLatestOpenableAttachment } from "./ComposerDock";

function attachment(
	id: string,
	options: { type?: string; openable?: boolean } = {},
): Attachment {
	return {
		id,
		type: options.type ?? "image",
		icon: "",
		summary: id,
		toMessagePart: () => ({ type: "text", text: id }) satisfies MessagePart,
		toPromptText: () => id,
		...(options.openable ? { onOpen: () => {} } : {}),
	};
}

describe("composer attachments", () => {
	test("finds the latest attachment that can be opened", () => {
		const review = attachment("review", { type: "code-review" });
		const image = attachment("image");
		const pager = attachment("pager", { openable: true });

		expect(findLatestOpenableAttachment([review, image, pager])).toBe(pager);
		expect(findLatestOpenableAttachment([review, image])).toBe(review);
		expect(findLatestOpenableAttachment([image])).toBeNull();
	});
});
