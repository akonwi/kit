import { describe, expect, test } from "bun:test";
import { createEditScratchpadTool } from "./edit-scratchpad";
import type { FileOperations } from "./file-operations";

describe("edit_scratchpad tool", () => {
	test("edits the active session scratchpad without accepting a path", async () => {
		let activePath = "/sessions/parent.scratchpad.md";
		const contents = new Map([
			["/sessions/parent.scratchpad.md", "parent notes"],
			["/sessions/child.scratchpad.md", "child notes"],
		]);
		const files: FileOperations = {
			read: async (filePath) => contents.get(filePath) ?? "",
			write: async (filePath, content) => {
				contents.set(filePath, content);
			},
			mutate: async (filePath, update) => {
				const current = contents.get(filePath) ?? "";
				const content = update(current);
				contents.set(filePath, content);
				return { updated: content !== current, content };
			},
			mutateOrCreate: async (filePath, update) => {
				const current = contents.get(filePath) ?? "";
				const content = update(current);
				contents.set(filePath, content);
				return { updated: content !== current, content };
			},
		};
		const tool = createEditScratchpadTool(() => activePath, files);

		activePath = "/sessions/child.scratchpad.md";
		const result = await tool.execute("edit-child", {
			edits: [{ oldText: "child notes", newText: "updated child notes" }],
		});

		expect(result.details).toEqual({ applied: 1, errors: [] });
		expect(contents.get("/sessions/parent.scratchpad.md")).toBe("parent notes");
		expect(contents.get("/sessions/child.scratchpad.md")).toBe(
			"updated child notes",
		);
	});

	test("creates and initializes a missing scratchpad", async () => {
		let content: string | undefined;
		const files: FileOperations = {
			read: async () => content ?? "",
			write: async (_filePath, next) => {
				content = next;
			},
			mutate: async (_filePath, update) => {
				if (content === undefined) {
					throw Object.assign(new Error("missing"), { code: "ENOENT" });
				}
				const next = update(content);
				const updated = next !== content;
				content = next;
				return { updated, content };
			},
			mutateOrCreate: async (_filePath, update) => {
				const current = content ?? "";
				const next = update(current);
				const updated = next !== current;
				content = next;
				return { updated, content: next };
			},
		};
		const tool = createEditScratchpadTool(() => "/sessions/new.md", files);

		const result = await tool.execute("initialize", {
			edits: [{ oldText: "", newText: "initial notes" }],
		});

		expect(result.details).toEqual({ applied: 1, errors: [] });
		expect(content).toBe("initial notes");

		const rejected = await tool.execute("reinitialize", {
			edits: [{ oldText: "", newText: "replacement" }],
		});
		expect(rejected.details.applied).toBe(0);
		expect(content).toBe("initial notes");
	});

	test("returns the same validation errors as the standard edit tool", async () => {
		const files: FileOperations = {
			read: async () => "repeated repeated",
			write: async () => {},
			mutate: async (_filePath, update) => {
				const content = update("repeated repeated");
				return { updated: false, content };
			},
			mutateOrCreate: async (_filePath, update) => {
				const content = update("repeated repeated");
				return { updated: false, content };
			},
		};
		const tool = createEditScratchpadTool(() => "/sessions/active.md", files);

		const result = await tool.execute("invalid-edit", {
			edits: [{ oldText: "repeated", newText: "changed" }],
		});

		expect(result.details.applied).toBe(0);
		expect(result.content[0]).toEqual({
			type: "text",
			text: "Error:\nedits[0]: oldText matches 2 locations — must be unique",
		});
	});
});
