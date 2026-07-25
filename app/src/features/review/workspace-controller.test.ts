import { describe, expect, test } from "bun:test";
import { createReviewWorkspaceController } from "./workspace-controller";

describe("review workspace controller", () => {
	test("notifies active subscribers when review is opened", () => {
		const controller = createReviewWorkspaceController();
		let opens = 0;
		const unsubscribe = controller.subscribe(() => {
			opens += 1;
		});

		controller.open();
		unsubscribe();
		controller.open();

		expect(opens).toBe(1);
	});
});
