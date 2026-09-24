import { afterEach, expect, test } from "bun:test";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createShowImageTool } from "./show-image";

const PNG_1X1 = Buffer.from(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4AWP4z8DwHwAFAAH/e+m+7wAAAABJRU5ErkJggg==",
	"base64",
);

const temporaryDirectories: string[] = [];

afterEach(async () => {
	await Promise.all(
		temporaryDirectories.splice(0).map((path) => rm(path, { recursive: true })),
	);
});

async function temporaryDirectory(): Promise<string> {
	const path = await mkdtemp(join(tmpdir(), "kit-show-image-"));
	temporaryDirectories.push(path);
	return path;
}

test("show_image returns validated image content for the transcript", async () => {
	const cwd = await temporaryDirectory();
	await writeFile(join(cwd, "pixel.png"), PNG_1X1);
	const tool = createShowImageTool(cwd);

	const result = await tool.execute(
		"call-1",
		{ path: "pixel.png", caption: "A red pixel" },
		undefined,
	);

	expect(result.content).toEqual([
		{ type: "text", text: "Displayed image: pixel.png — A red pixel" },
		{ type: "image", data: PNG_1X1.toString("base64"), mimeType: "image/png" },
	]);
	expect(result.details).toMatchObject({
		presentation: "transcript-image",
		filename: "pixel.png",
		caption: "A red pixel",
		mimeType: "image/png",
		width: 1,
		height: 1,
		bytes: PNG_1X1.byteLength,
	});
});

test("show_image rejects malformed image files", async () => {
	const cwd = await temporaryDirectory();
	await writeFile(join(cwd, "not-an-image.png"), "not an image");
	const tool = createShowImageTool(cwd);

	expect(
		tool.execute("call-1", { path: "not-an-image.png" }, undefined),
	).rejects.toThrow();
});
