import { readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import type { AgentTool } from "../runtime/agent";
import { Type } from "../runtime/agent";

// Legacy single-edit shape the model sometimes sends instead of the canonical {edits:[...]} shape.
type LegacyEditParams = {
	path: string;
	oldText: string;
	newText: string;
	edits?: never;
};
type CanonicalEditParams = {
	path: string;
	edits: { oldText: string; newText: string }[];
};
type EditParams = CanonicalEditParams | LegacyEditParams;

const editSchema = Type.Object({
	path: Type.String({
		description: "Path to the file to edit (relative or absolute)",
	}),
	edits: Type.Array(
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
		},
	),
});

export function createEditTool(cwd: string): AgentTool<typeof editSchema> {
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
			try {
				const abs = resolve(cwd, params.path);
				const original = await readFile(abs, "utf8");

				// Validate all edits against original before applying any
				const errors: string[] = [];
				for (let i = 0; i < params.edits.length; i++) {
					const { oldText } = params.edits[i];
					const count = original.split(oldText).length - 1;
					if (count === 0) {
						errors.push(`edits[${i}]: oldText not found in file`);
					} else if (count > 1) {
						errors.push(
							`edits[${i}]: oldText matches ${count} locations — must be unique`,
						);
					}
				}

				if (errors.length > 0) {
					return {
						content: [{ type: "text", text: `Error:\n${errors.join("\n")}` }],
						details: { path: abs, applied: 0, errors },
					};
				}

				// Check for overlaps: no oldText should contain another
				for (let i = 0; i < params.edits.length; i++) {
					for (let j = 0; j < params.edits.length; j++) {
						if (i === j) continue;
						if (params.edits[i].oldText.includes(params.edits[j].oldText)) {
							errors.push(
								`edits[${i}] and edits[${j}] overlap — merge them into one edit`,
							);
						}
					}
				}

				if (errors.length > 0) {
					return {
						content: [{ type: "text", text: `Error:\n${errors.join("\n")}` }],
						details: { path: abs, applied: 0, errors },
					};
				}

				// Apply all edits to the original (not incrementally)
				let result = original;
				for (const { oldText, newText } of params.edits) {
					result = result.replace(oldText, newText);
				}

				await writeFile(abs, result, "utf8");

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
				const msg = err instanceof Error ? err.message : String(err);
				return {
					content: [{ type: "text", text: `Error: ${msg}` }],
					details: { path: params.path, applied: 0, errors: [msg] },
				};
			}
		},
	};
}
