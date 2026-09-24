import { afterEach, describe, expect, test } from "bun:test";
import { testRender } from "@opentui/solid";
import { ScreenHeader } from "./ScreenHeader";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

describe("ScreenHeader", () => {
	test("renders the strip variant with only a bottom separator", async () => {
		testSetup = await testRender(
			() => (
				<ScreenHeader
					variant="strip"
					left={<text>A very long pager title that must truncate</text>}
					right={<text>1/3</text>}
					progress={50}
				/>
			),
			{ width: 30, height: 4 },
		);
		await testSetup.renderOnce();

		const lines = testSetup.captureCharFrame().split("\n");
		expect(lines[0]).toContain("A very long");
		expect(lines[0]).toContain("1/3");
		expect(lines[0]).not.toContain("┌");
		expect(lines[1]).toContain("─");
	});
});
