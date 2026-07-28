import { describe, expect, test } from "bun:test";
import type { SubagentDefinition } from "./discovery";
import { mergeItems, relativeTime, statusLabel } from "./presentation";
import type { ActiveSubagentConversationState } from "./state";

function agent(name: string): SubagentDefinition {
	return {
		name,
		description: `${name} description`,
		source: "kit-user",
		instructions: "",
	};
}

function conversation(
	agentName: string,
	status: ActiveSubagentConversationState["status"],
): ActiveSubagentConversationState {
	return {
		agentName,
		subagentConversationId: `conv-${agentName}`,
		status,
		lastActivityAt: new Date().toISOString(),
	} as ActiveSubagentConversationState;
}

describe("subagents presentation", () => {
	test("mergeItems sorts by status rank then name", () => {
		const items = mergeItems(
			[agent("zulu"), agent("alpha"), agent("mike")],
			[conversation("zulu", "running"), conversation("mike", "failed")],
		);

		expect(items.map((item) => item.name)).toEqual(["zulu", "mike", "alpha"]);
		expect(items[0]?.status).toBe("running");
		expect(items[2]?.status).toBe("inactive");
	});

	test("mergeItems keeps conversations without a matching definition", () => {
		const items = mergeItems([], [conversation("ghost", "idle")]);
		expect(items).toHaveLength(1);
		expect(items[0]?.name).toBe("ghost");
		expect(items[0]?.description).toBe(
			"Previously active sub-agent conversation",
		);
	});

	test("statusLabel maps internal states to user-facing labels", () => {
		expect(statusLabel("idle")).toBe("completed");
		expect(statusLabel("inactive")).toBe("available");
		expect(statusLabel("running")).toBe("running");
	});

	test("relativeTime handles missing and invalid timestamps", () => {
		expect(relativeTime(undefined)).toBe("");
		expect(relativeTime("not-a-date")).toBe("");
		expect(relativeTime(new Date().toISOString())).toBe("just now");
	});
});
