import { describe, expect, test } from "bun:test";
import type { MessagePart } from "../messages/parts";
import {
	type Attachment,
	createAttachmentsController,
} from "./attachments-controller";

function attachment(id: string, onDetach?: Attachment["onDetach"]): Attachment {
	return {
		id,
		type: "test",
		icon: "",
		summary: id,
		toMessagePart: () => ({ type: "text", text: id }) satisfies MessagePart,
		toPromptText: () => id,
		onDetach,
	};
}

describe("attachments controller", () => {
	test("reports whether an attachment was removed or consumed", () => {
		const controller = createAttachmentsController();
		const reasons: string[] = [];
		controller.attach(attachment("manual", (reason) => reasons.push(reason)));
		controller.detach("manual");
		controller.attach(attachment("message", (reason) => reasons.push(reason)));
		controller.detach("message", "consumed");

		expect(reasons).toEqual(["removed", "consumed"]);
	});

	test("replacing an attachment does not detach the previous projection", () => {
		const controller = createAttachmentsController();
		let detached = false;
		controller.attach(
			attachment("draft", () => {
				detached = true;
			}),
		);
		controller.attach(attachment("draft"));

		expect(detached).toBe(false);
		expect(controller.attachments()).toHaveLength(1);
	});
});
