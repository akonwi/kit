import { describe, expect, test } from "bun:test";
import type { SubagentDefinition } from "./discovery";
import { formatSubagentsForPrompt } from "./format";

const agents: SubagentDefinition[] = [
	{
		name: "scout",
		description: "Fast reconnaissance",
		model: "claude-haiku-4-5",
		instructions: "Scout instructions",
		filePath: "/tmp/scout.md",
		baseDir: "/tmp",
		source: "kit-user",
	},
	{
		name: "reviewer",
		description: "Review correctness & risk",
		instructions: "Reviewer instructions",
		filePath: "/tmp/reviewer.md",
		baseDir: "/tmp",
		source: "pi-project",
	},
];

describe("formatSubagentsForPrompt", () => {
	test("returns empty string when no sub-agents are available", () => {
		expect(formatSubagentsForPrompt([])).toBe("");
	});

	test("formats sub-agents as prompt guidance with xml metadata", () => {
		const formatted = formatSubagentsForPrompt(agents);
		expect(formatted).toContain(
			"The following sub-agents are available as named specialists.",
		);
		expect(formatted).toContain(
			"Use the subagent tool to delegate to them when isolated context would help.",
		);
		expect(formatted).toContain("<available_subagents>");
		expect(formatted).toContain("<name>scout</name>");
		expect(formatted).toContain(
			"<description>Review correctness &amp; risk</description>",
		);
		expect(formatted).toContain("<model>claude-haiku-4-5</model>");
		expect(formatted).toContain("<source>pi-project</source>");
		expect(formatted).toContain("</available_subagents>");
	});
});
