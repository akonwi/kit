import { describe, expect, test } from "bun:test";
import type { MessagePart } from "../messages/parts";
import type { Attachment } from "./attachments-controller";
import {
	findLatestOpenableAttachment,
	getComposerUpAction,
} from "./ComposerDock";

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

describe("composer Up behavior", () => {
	test("restores queued messages before native navigation or recall", () => {
		expect(getComposerUpAction(2, "current draft")).toBe("restore");
		expect(getComposerUpAction(0, "current draft")).toBe("native");
		expect(getComposerUpAction(0, "")).toBe("recall");
	});
});

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
