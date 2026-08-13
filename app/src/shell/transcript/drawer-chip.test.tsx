import { afterEach, describe, expect, test } from "bun:test";
import { testRender } from "@opentui/solid";
import type { ToolCall } from "../../runtime/agent";
import { DrawerChip } from "./drawer-chip";

let testSetup: Awaited<ReturnType<typeof testRender>> | undefined;

afterEach(() => {
	testSetup?.renderer.destroy();
	testSetup = undefined;
});

function toolCall(name: string, index: number): ToolCall {
	return {
		type: "toolCall",
		id: `tool-${index}`,
		name,
		arguments: {},
	} as ToolCall;
}

describe("DrawerChip", () => {
	test("keeps a long tool summary on one row", async () => {
		testSetup = await testRender(
			() => (
				<DrawerChip
					toolCalls={[
						toolCall("read", 1),
						toolCall("grep", 2),
						toolCall("edit", 3),
						toolCall("bash", 4),
					]}
					toolResults={new Map()}
				/>
			),
			{ width: 40, height: 3 },
		);
		await testSetup.renderOnce();

		const lines = testSetup.captureCharFrame().split("\n");
		expect(lines[0]).toContain("4 tool calls");
		expect(lines[0]).toContain("read · gre");
		expect(lines[0]).toContain("dit · bash");
		expect(lines[0]).not.toContain("[object Object]");
		expect(lines[1]?.trim()).toBe("");
	});
});
