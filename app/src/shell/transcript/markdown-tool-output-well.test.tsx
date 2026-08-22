import { afterEach, expect, test } from "bun:test";
import { createMockMouse } from "@opentui/core/testing";
import { testRender } from "@opentui/solid";
import { MarkdownToolOutputWell } from "./markdown-tool-output-well";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

async function waitForFrame(text: string): Promise<string> {
	let frame = "";
	for (let attempt = 0; attempt < 20; attempt += 1) {
		await Bun.sleep(10);
		await testSetup?.renderOnce();
		frame = testSetup?.captureCharFrame() ?? "";
		if (frame.includes(text)) break;
	}
	return frame;
}

test("renders tool output as markdown inside the contained result well", async () => {
	testSetup = await testRender(
		() => <MarkdownToolOutputWell content={"# Result\n\n- first\n- second"} />,
		{ width: 50, height: 12 },
	);

	const frame = await waitForFrame("Result");
	expect(frame).toContain("Result");
	expect(frame).toContain("first");
	expect(frame).toContain("second");
	expect(frame).not.toContain("# Result");
});

test("keeps long rendered markdown reachable through overflow scrolling", async () => {
	const rows = Array.from(
		{ length: 20 },
		(_, index) => `| row ${index + 1} | value ${index + 1} |`,
	);
	const table = ["| Name | Value |", "| --- | --- |", ...rows].join("\n");
	testSetup = await testRender(
		() => <MarkdownToolOutputWell content={table} />,
		{
			width: 50,
			height: 18,
		},
	);

	let frame = await waitForFrame("row 1");
	expect(frame).not.toContain("row 20");
	const mouse = createMockMouse(testSetup.renderer);
	for (let index = 0; index < 30; index += 1) {
		await mouse.scroll(10, 5, "down");
	}
	await Bun.sleep(0);
	await testSetup.renderOnce();
	frame = testSetup.captureCharFrame();
	expect(frame).toContain("row 20");
});

test("measures markdown constructs that render taller than their source", async () => {
	const table = [
		"| Name | Value |",
		"| --- | --- |",
		"| alpha | one |",
		"| beta | two |",
	].join("\n");
	testSetup = await testRender(
		() => <MarkdownToolOutputWell content={table} />,
		{
			width: 50,
			height: 14,
		},
	);

	const frame = await waitForFrame("beta");
	expect(frame).toContain("alpha");
	expect(frame).toContain("beta");
	expect(frame).toContain("two");
});
