import { describe, expect, test } from "bun:test";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { applyExactEdits, createEditTool } from "./edit";
import { defaultFileOperations, type FileOperations } from "./file-operations";
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
			mutateOrCreate: async (_filePath, update) => {
				const next = update(content);
				const updated = next !== content;
				content = next;
				return { updated, content };
			},
		},
	};
}

describe("exact edits", () => {
	test("applies replacements to original ranges rather than inserted text", () => {
		expect(
			applyExactEdits("A B", [
				{ oldText: "A", newText: "B" },
				{ oldText: "B", newText: "C" },
			]),
		).toEqual({ content: "B C", errors: [] });
	});

	test("rejects empty searches and overlapping matches", () => {
		expect(applyExactEdits("content", [{ oldText: "", newText: "x" }])).toEqual(
			{
				content: "content",
				errors: ["edits[0]: oldText must not be empty"],
			},
		);
		expect(applyExactEdits("aaa", [{ oldText: "aa", newText: "x" }])).toEqual({
			content: "aaa",
			errors: ["edits[0]: oldText matches 2 locations — must be unique"],
		});
		expect(
			applyExactEdits("abc", [
				{ oldText: "ab", newText: "x" },
				{ oldText: "bc", newText: "y" },
			]),
		).toEqual({
			content: "abc",
			errors: ["edits[0] and edits[1] overlap — merge them into one edit"],
		});
	});
});

describe("standard file tool operations", () => {
	test("atomically creates missing files and mutates files that already exist", async () => {
		const directory = await mkdtemp(
			path.join(tmpdir(), "kit-file-operations-"),
		);
		const filePath = path.join(directory, "scratchpad.md");
		try {
			await defaultFileOperations.mutateOrCreate(filePath, () => "created");
			expect(await readFile(filePath, "utf8")).toBe("created");

			await writeFile(filePath, "existing", "utf8");
			await defaultFileOperations.mutateOrCreate(
				filePath,
				(current) => `${current} updated`,
			);
			expect(await readFile(filePath, "utf8")).toBe("existing updated");
		} finally {
			await rm(directory, { recursive: true, force: true });
		}
	});

	test("publishes only one concurrent missing-file initialization", async () => {
		const directory = await mkdtemp(path.join(tmpdir(), "kit-file-race-"));
		const filePath = path.join(directory, "scratchpad.md");
		try {
			const initialize = (value: string) =>
				defaultFileOperations.mutateOrCreate(filePath, (current) => {
					if (current !== "") throw new Error("already initialized");
					return value;
				});
			const results = await Promise.allSettled([
				initialize("first"),
				initialize("second"),
			]);

			expect(
				results.filter((result) => result.status === "fulfilled"),
			).toHaveLength(1);
			expect(
				results.filter((result) => result.status === "rejected"),
			).toHaveLength(1);
			expect(["first", "second"]).toContain(await readFile(filePath, "utf8"));
		} finally {
			await rm(directory, { recursive: true, force: true });
		}
	});

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
