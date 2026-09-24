import { afterEach, describe, expect, test } from "bun:test";
import {
	mkdtempSync,
	realpathSync,
	rmSync,
	symlinkSync,
	writeFileSync,
} from "node:fs";
import os from "node:os";
import path from "node:path";
import {
	fileViewerHintGroup,
	readFileWithinRoot,
	resolveFileWithinRoot,
} from "./FileViewerPanel";

const tempDirectories: string[] = [];

function temporaryDirectory(prefix: string): string {
	const directory = mkdtempSync(path.join(os.tmpdir(), prefix));
	tempDirectories.push(directory);
	return directory;
}

afterEach(() => {
	for (const directory of tempDirectories.splice(0)) {
		rmSync(directory, { force: true, recursive: true });
	}
});

describe("workspace file preview", () => {
	test("shows bindings for the currently focused file surface", () => {
		expect(fileViewerHintGroup(false)).toBe("file-viewer");
		expect(fileViewerHintGroup(true)).toBe("files");
	});

	test("reads regular files inside the repository", () => {
		const root = temporaryDirectory("kit-file-viewer-");
		writeFileSync(path.join(root, "inside.ts"), "export const value = 1;\n");

		expect(resolveFileWithinRoot(root, "inside.ts")).toBe(
			realpathSync(path.join(root, "inside.ts")),
		);
		expect(readFileWithinRoot(root, "inside.ts").content).toContain(
			"export const value",
		);
	});

	test("reads an in-repository symlink through its contained target", () => {
		const root = temporaryDirectory("kit-file-viewer-root-");
		writeFileSync(path.join(root, "target.txt"), "inside");
		symlinkSync("target.txt", path.join(root, "linked.txt"));

		expect(readFileWithinRoot(root, "linked.txt").content).toBe("inside");
	});

	test("rejects symlinks that resolve outside the repository", () => {
		const root = temporaryDirectory("kit-file-viewer-root-");
		const outside = temporaryDirectory("kit-file-viewer-outside-");
		const secret = path.join(outside, "secret.txt");
		writeFileSync(secret, "secret");
		symlinkSync(secret, path.join(root, "linked.txt"));

		expect(resolveFileWithinRoot(root, "linked.txt")).toBeNull();
		expect(readFileWithinRoot(root, "linked.txt").content).toBeNull();
	});

	test("rejects directories", () => {
		const root = temporaryDirectory("kit-file-viewer-directory-");
		expect(readFileWithinRoot(root, ".").content).toBeNull();
	});

	test("rejects binary and oversized previews", () => {
		const root = temporaryDirectory("kit-file-viewer-limits-");
		writeFileSync(path.join(root, "binary.bin"), Buffer.from([1, 0, 2]));
		writeFileSync(path.join(root, "large.txt"), "x".repeat(1_000_001));

		expect(readFileWithinRoot(root, "binary.bin").error).toContain("Binary");
		expect(readFileWithinRoot(root, "large.txt").error).toContain("large");
	});
});
