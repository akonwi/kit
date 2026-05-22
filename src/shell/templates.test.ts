import { describe, expect, test } from "bun:test";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { BUILT_IN_TEMPLATES, initTemplates, renderTemplate } from "./templates";

describe("templates", () => {
	test("renderTemplate uses built-in defaults before initTemplates", () => {
		const result = renderTemplate("review-feedback", {
			content: "FILE_NOTES_HERE",
		});
		expect(result).toContain("FILE_NOTES_HERE");
		expect(result).toContain("Here is my feedback to code");
	});

	test("built-in review-feedback template wraps content", () => {
		const result = renderTemplate("review-feedback", {
			content: "Note about file.ts",
		});
		expect(result).toContain("Note about file.ts");
		expect(result).toContain("Here is my feedback to code");
		expect(result).not.toContain("Please use");
	});

	test("built-in pager-feedback template wraps content", () => {
		const result = renderTemplate("pager-feedback", {
			content: "Section note",
		});
		expect(result).toContain("Section note");
		expect(result).toContain("Here is my feedback");
		expect(result).not.toContain("revision or reply");
	});

	test("initTemplates does not throw for missing project dir", () => {
		initTemplates(undefined);
		// Falls back to built-in; renderTemplate should still work
		const result = renderTemplate("review-feedback", {
			content: "test",
		});
		expect(result).toContain("test");
	});

	test("unknown template returns null", () => {
		const result = renderTemplate("nonexistent", { content: "x" });
		expect(result).toBeNull();
	});

	test("BUILT_IN_TEMPLATES has both expected keys", () => {
		expect(Object.keys(BUILT_IN_TEMPLATES).sort()).toEqual([
			"pager-feedback",
			"review-feedback",
		]);
	});

	test("loads project-level template override from disk", () => {
		const tmp = path.join(import.meta.dir, `.test-templates-${Date.now()}`);
		mkdirSync(path.join(tmp, ".kit", "templates"), { recursive: true });
		writeFileSync(
			path.join(tmp, ".kit", "templates", "review-feedback.md"),
			"custom: {{content}}",
		);

		try {
			initTemplates(tmp);
			const result = renderTemplate("review-feedback", { content: "notes" });
			expect(result).toBe("custom: notes");
			expect(result).not.toContain("Here is my feedback to code");
		} finally {
			rmSync(tmp, { recursive: true, force: true });
		}
	});
});
