import { resolve } from "node:path";
import type { AgentTool } from "../runtime/agent";
import { Type } from "../runtime/agent";
import { defaultFileOperations, type FileOperations } from "./file-operations";

// Legacy single-edit shape the model sometimes sends instead of the canonical {edits:[...]} shape.
type LegacyEditParams = {
	path: string;
	oldText: string;
	newText: string;
	edits?: never;
};
export type ExactEdit = { oldText: string; newText: string };

type CanonicalEditParams = {
	path: string;
	edits: ExactEdit[];
};
type EditParams = CanonicalEditParams | LegacyEditParams;

export const exactEditsSchema = Type.Array(
	Type.Object({
		oldText: Type.String({
			description:
				"Exact text for one targeted replacement. Must be unique in the file and must not overlap with other edits in the same call.",
		}),
		newText: Type.String({
			description: "Replacement text for this targeted edit.",
		}),
	}),
	{
		description:
			"One or more targeted replacements. Each edit is matched against the original file, not incrementally. Merge nearby changes into one edit instead of overlapping edits.",
		minItems: 1,
	},
);

const editSchema = Type.Object({
	path: Type.String({
		description: "Path to the file to edit (relative or absolute)",
	}),
	edits: exactEditsSchema,
});

type EditRange = ExactEdit & { index: number; start: number; end: number };

function findMatchOffsets(content: string, search: string): number[] {
	if (search.length === 0) return [];
	const offsets: number[] = [];
	let from = 0;
	while (from <= content.length - search.length) {
		const index = content.indexOf(search, from);
		if (index < 0) break;
		offsets.push(index);
		from = index + 1;
	}
	return offsets;
}

export function applyExactEdits(
	original: string,
	edits: ExactEdit[],
): { content: string; errors: string[] } {
	const errors: string[] = [];
	const ranges: EditRange[] = [];
	for (let i = 0; i < edits.length; i++) {
		const edit = edits[i];
		if (edit.oldText.length === 0) {
			errors.push(`edits[${i}]: oldText must not be empty`);
			continue;
		}
		const offsets = findMatchOffsets(original, edit.oldText);
		if (offsets.length === 0) {
			errors.push(`edits[${i}]: oldText not found in file`);
		} else if (offsets.length > 1) {
			errors.push(
				`edits[${i}]: oldText matches ${offsets.length} locations — must be unique`,
			);
		} else {
			const start = offsets[0];
			ranges.push({
				...edit,
				index: i,
				start,
				end: start + edit.oldText.length,
			});
		}
	}

	for (let i = 0; i < ranges.length; i++) {
		for (let j = i + 1; j < ranges.length; j++) {
			const left = ranges[i];
			const right = ranges[j];
			if (left.start < right.end && right.start < left.end) {
				errors.push(
					`edits[${left.index}] and edits[${right.index}] overlap — merge them into one edit`,
				);
			}
		}
	}

	if (errors.length > 0) return { content: original, errors };
	let content = original;
	for (const range of ranges.sort((a, b) => b.start - a.start)) {
		content =
			content.slice(0, range.start) + range.newText + content.slice(range.end);
	}
	return { content, errors };
}

export function createEditTool(
	cwd: string,
	files: FileOperations = defaultFileOperations,
): AgentTool<typeof editSchema> {
	return {
		name: "edit",
		label: "Edit",
		description:
			"Edit a file using exact text replacements. Each edit's oldText must match exactly and be unique. Multiple edits are applied to the original file simultaneously — do not include overlapping edits.",
		parameters: editSchema,

		async execute(_id, rawParams, _signal) {
			// Accept legacy single-edit shape {path, oldText, newText} in addition to
			// the canonical {path, edits:[...]} shape, since models sometimes use both.
			const raw = rawParams as unknown as EditParams;
			const legacy = raw as LegacyEditParams;
			const params: CanonicalEditParams =
				typeof legacy.oldText === "string" && !Array.isArray(raw.edits)
					? {
							path: raw.path,
							edits: [{ oldText: legacy.oldText, newText: legacy.newText }],
						}
					: (raw as CanonicalEditParams);
			const abs = resolve(cwd, params.path);
			const errors: string[] = [];
			try {
				await files.mutate(abs, (original) => {
					const result = applyExactEdits(original, params.edits);
					errors.push(...result.errors);
					if (errors.length > 0) throw new Error("Invalid edits");
					return result.content;
				});

				return {
					content: [
						{
							type: "text",
							text: `Applied ${params.edits.length} edit${params.edits.length === 1 ? "" : "s"} to ${params.path}`,
						},
					],
					details: { path: abs, applied: params.edits.length, errors: [] },
				};
			} catch (err) {
				if (errors.length > 0) {
					return {
						content: [{ type: "text", text: `Error:\n${errors.join("\n")}` }],
						details: { path: abs, applied: 0, errors },
					};
				}
				const msg = err instanceof Error ? err.message : String(err);
				return {
					content: [{ type: "text", text: `Error: ${msg}` }],
					details: { path: params.path, applied: 0, errors: [msg] },
				};
			}
		},
	};
}
