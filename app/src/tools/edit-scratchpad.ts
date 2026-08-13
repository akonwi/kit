import type { AgentTool } from "../runtime/agent";
import { Type } from "../runtime/agent";
import { applyExactEdits, exactEditsSchema } from "./edit";
import { defaultFileOperations, type FileOperations } from "./file-operations";

function applyScratchpadEdits(
	original: string,
	edits: Parameters<typeof applyExactEdits>[1],
) {
	if (edits.some((edit) => edit.oldText === "")) {
		if (original === "" && edits.length === 1 && edits[0].oldText === "") {
			return { content: edits[0].newText, errors: [] };
		}
		return {
			content: original,
			errors: edits.flatMap((edit, index) =>
				edit.oldText === ""
					? [
							`edits[${index}]: oldText may be empty only when initializing an empty scratchpad`,
						]
					: [],
			),
		};
	}
	return applyExactEdits(original, edits);
}

export const EDIT_SCRATCHPAD_TOOL_NAME = "edit_scratchpad";

const parameters = Type.Object({
	edits: exactEditsSchema,
});

export function createEditScratchpadTool(
	getScratchpadPath: () => string,
	files: FileOperations = defaultFileOperations,
): AgentTool<typeof parameters> {
	return {
		name: EDIT_SCRATCHPAD_TOOL_NAME,
		label: "Edit scratchpad",
		description:
			"Edit the active session scratchpad using exact text replacements. The scratchpad is selected automatically; never provide or reuse a session path. Each edit's oldText must match exactly and be unique. To initialize an empty scratchpad, send one edit with an empty oldText.",
		parameters,
		async execute(_id, params, _signal) {
			const filePath = getScratchpadPath();
			const errors: string[] = [];
			const apply = (original: string) => {
				const result = applyScratchpadEdits(original, params.edits);
				errors.push(...result.errors);
				if (errors.length > 0) throw new Error("Invalid edits");
				return result.content;
			};
			try {
				await files.mutateOrCreate(filePath, apply);
				return {
					content: [
						{
							type: "text",
							text: `Applied ${params.edits.length} edit${params.edits.length === 1 ? "" : "s"} to the active session scratchpad`,
						},
					],
					details: { applied: params.edits.length, errors: [] },
				};
			} catch (error) {
				const messages =
					errors.length > 0
						? errors
						: [error instanceof Error ? error.message : String(error)];
				return {
					content: [{ type: "text", text: `Error:\n${messages.join("\n")}` }],
					details: { applied: 0, errors: messages },
				};
			}
		},
	};
}
