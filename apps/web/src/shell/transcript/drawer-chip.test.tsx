import { afterEach, describe, expect, test } from "bun:test";
import { createMockMouse } from "@opentui/core/testing";
import { testRender } from "@opentui/solid";
import type { ToolCall, ToolResultMessage } from "../../runtime/agent";
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

	test("opens a subagent pane when its tool name is clicked", async () => {
		const opened: string[] = [];
		let activityOpens = 0;
		let available = true;
		const subagentCall = {
			type: "toolCall",
			id: "subagent-1",
			name: "subagent",
			arguments: {
				action: "run",
				agent: "code-reviewer",
				message: "Inspect the changes",
			},
		} as ToolCall;
		const result = {
			role: "toolResult",
			toolCallId: subagentCall.id,
			toolName: subagentCall.name,
			content: [],
			isError: false,
			timestamp: 1,
		} as ToolResultMessage;
		const readCall = toolCall("read", 2);
		const readResult = {
			...result,
			toolCallId: readCall.id,
			toolName: readCall.name,
		};

		testSetup = await testRender(
			() => (
				<DrawerChip
					toolCalls={[readCall, subagentCall]}
					toolResults={
						new Map([
							[readCall.id, readResult],
							[subagentCall.id, result],
						])
					}
					onActivate={() => activityOpens++}
					onOpenSubagent={(agentName) => {
						if (!available) return false;
						opened.push(agentName);
						return true;
					}}
				/>
			),
			{ width: 40, height: 3 },
		);
		await testSetup.renderOnce();
		const frame = testSetup.captureCharFrame();
		const row = frame
			.split("\n")
			.findIndex((line) => line.includes("code-reviewer"));
		const column = frame.split("\n")[row]?.indexOf("code-reviewer") ?? -1;
		expect(frame).toContain("code-reviewer · read");
		expect(row).toBeGreaterThanOrEqual(0);
		expect(column).toBeGreaterThanOrEqual(0);

		const mouse = createMockMouse(testSetup.renderer);
		await mouse.pressDown(column, row);
		await mouse.release(column, row);

		expect(opened).toEqual(["code-reviewer"]);
		expect(activityOpens).toBe(0);

		available = false;
		await mouse.pressDown(column, row);
		await mouse.release(column, row);
		expect(opened).toEqual(["code-reviewer"]);
		expect(activityOpens).toBe(1);
	});
});
