import { afterEach, describe, expect, test } from "bun:test";
import { testRender } from "@opentui/solid";
import { MessageComposer } from "./MessageComposer";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

describe("MessageComposer", () => {
	test("renders the dock variant without a field border", async () => {
		testSetup = await testRender(
			() => (
				<MessageComposer
					variant="dock"
					placeholder="Ask kit"
					focused={false}
					showCursor={false}
				/>
			),
			{ width: 20, height: 5 },
		);
		await testSetup.renderOnce();

		const frame = testSetup.captureCharFrame();
		expect(frame).toContain("Ask kit");
		expect(frame).not.toContain("┌");
		expect(frame).not.toContain("└");
	});
});
