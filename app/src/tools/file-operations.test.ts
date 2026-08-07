import { describe, expect, test } from "bun:test";
import path from "node:path";
import { createEditTool } from "./edit";
import type { FileOperations } from "./file-operations";
import { createReadTool } from "./read";
import { createWriteTool } from "./write";

function memoryFiles(initial: string): {
	files: FileOperations;
	content: () => string;
} {
	let content = initial;
	return {
		content: () => content,
		files: {
			read: async () => content,
			write: async (_filePath, next) => {
				content = next;
			},
			mutate: async (_filePath, update) => {
				const next = update(content);
				const updated = next !== content;
				content = next;
				return { updated, content };
			},
		},
	};
}

describe("standard file tool operations", () => {
	test("routes read, edit, and write through the provided file operations", async () => {
		const memory = memoryFiles("first line\nsecond line");
		const cwd = path.resolve("/tmp/project");
		const read = createReadTool(cwd, memory.files);
		const edit = createEditTool(cwd, memory.files);
		const write = createWriteTool(cwd, memory.files);

		const readResult = await read.execute("read-1", { path: "notes.md" });
		expect(readResult.content).toEqual([
			{ type: "text", text: "first line\nsecond line" },
		]);

		const editResult = await edit.execute("edit-1", {
			path: "notes.md",
			edits: [{ oldText: "second line", newText: "updated line" }],
		});
		expect(editResult.details.applied).toBe(1);
		expect(memory.content()).toBe("first line\nupdated line");

		await write.execute("write-1", {
			path: "notes.md",
			content: "replacement",
		});
		expect(memory.content()).toBe("replacement");
	});
});
