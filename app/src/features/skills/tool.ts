/**
 * load_skill tool — allows the model to activate a skill by name.
 *
 * Returns the SKILL.md content so the model can follow the skill's instructions.
 */

import { readFileSync } from "node:fs";
import { type Static, Type } from "@earendil-works/pi-ai";
import type { ToolDefinition, ToolResult } from "../../plugins";
import type { Skill } from "./discovery";

const parameters = Type.Object({
	name: Type.String({ description: "Name of the skill to load" }),
});

export function createActivateSkillTool(
	getSkills: () => Skill[],
): ToolDefinition<typeof parameters, Record<string, unknown>> {
	return {
		name: "activate_skill",
		label: "Activate Skill",
		description:
			"Activate a skill by name to get specialized instructions for a task.",
		promptSnippet:
			"Activate a skill to get detailed, task-specific instructions.",
		promptGuidelines: [
			"Call this tool when the user's task matches a skill's description.",
			"The tool returns the skill's full instructions — follow them for the task.",
		],
		parameters,
		async execute(
			_toolCallId: string,
			input: Static<typeof parameters>,
			_signal: AbortSignal | undefined,
			_onUpdate: unknown,
		): Promise<ToolResult<Record<string, unknown>>> {
			const skills = getSkills();
			const skill = skills.find((s) => s.name === input.name);

			if (!skill) {
				const available = skills.map((s) => s.name).join(", ");
				return {
					content: [
						{
							type: "text" as const,
							text: `Skill "${input.name}" not found. Available skills: ${available || "none"}`,
						},
					],
					details: { error: "not_found", available: skills.map((s) => s.name) },
				};
			}

			try {
				const content = readFileSync(skill.filePath, "utf8");
				return {
					content: [{ type: "text" as const, text: content }],
					details: {
						name: skill.name,
						description: skill.description,
						filePath: skill.filePath,
						baseDir: skill.baseDir,
					},
				};
			} catch (err) {
				return {
					content: [
						{
							type: "text" as const,
							text: `Failed to read skill file: ${err instanceof Error ? err.message : String(err)}`,
						},
					],
					details: { error: "read_failed", filePath: skill.filePath },
				};
			}
		},
	};
}
