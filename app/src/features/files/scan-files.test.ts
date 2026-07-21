import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { scanFiles } from "./scan-files";

const tempDirs: string[] = [];

afterEach(async () => {
	await Promise.all(
		tempDirs.splice(0).map((dir) => rm(dir, { recursive: true, force: true })),
	);
});

async function createTempDir(): Promise<string> {
	const dir = await mkdtemp(path.join(tmpdir(), "kit-scan-files-"));
	tempDirs.push(dir);
	return dir;
}

describe("scanFiles", () => {
	test("applies nested gitignore rules relative to their directory", async () => {
		const root = await createTempDir();
		await mkdir(path.join(root, ".build"), { recursive: true });
		await mkdir(path.join(root, "desktop", ".build"), { recursive: true });
		await writeFile(path.join(root, ".build", "root.txt"), "root");
		await writeFile(path.join(root, "desktop", ".gitignore"), "/.build\n");
		await writeFile(path.join(root, "desktop", ".build", "ignored.txt"), "no");
		await writeFile(path.join(root, "desktop", "visible.txt"), "yes");

		const result = await scanFiles(root);

		expect(result.files).toContain(".build/root.txt");
		expect(result.files).toContain("desktop/visible.txt");
		expect(result.files).not.toContain("desktop/.build/ignored.txt");
		expect(result.dirs).not.toContain("desktop/.build/");
	});

	test("applies descendant rules after ancestors without affecting siblings", async () => {
		const root = await createTempDir();
		await mkdir(path.join(root, "a"), { recursive: true });
		await mkdir(path.join(root, "b"), { recursive: true });
		await writeFile(path.join(root, ".gitignore"), "*.log\n");
		await writeFile(
			path.join(root, "a", ".gitignore"),
			"*.log\n!keep.log\n*.tmp\n",
		);
		await writeFile(path.join(root, "a", "keep.log"), "yes");
		await writeFile(path.join(root, "a", "drop.log"), "no");
		await writeFile(path.join(root, "a", "drop.tmp"), "no");
		await writeFile(path.join(root, "b", "keep.log"), "no");
		await writeFile(path.join(root, "b", "visible.tmp"), "yes");

		const result = await scanFiles(root);

		expect(result.files).toContain("a/keep.log");
		expect(result.files).toContain("b/visible.tmp");
		expect(result.files).not.toContain("a/drop.log");
		expect(result.files).not.toContain("a/drop.tmp");
		expect(result.files).not.toContain("b/keep.log");
	});

	test("applies kitignore after gitignore in the same directory", async () => {
		const root = await createTempDir();
		await writeFile(path.join(root, ".gitignore"), "*.txt\n!git-kept.txt\n");
		await writeFile(
			path.join(root, ".kitignore"),
			"!keep.txt\ngit-kept.txt\n*.secret\n",
		);
		await writeFile(path.join(root, "keep.txt"), "yes");
		await writeFile(path.join(root, "drop.txt"), "no");
		await writeFile(path.join(root, "git-kept.txt"), "no");
		await writeFile(path.join(root, "drop.secret"), "no");

		const result = await scanFiles(root);

		expect(result.files).toContain("keep.txt");
		expect(result.files).not.toContain("drop.txt");
		expect(result.files).not.toContain("git-kept.txt");
		expect(result.files).not.toContain("drop.secret");
	});

	test("can include ignore control files even when their rules match them", async () => {
		const root = await createTempDir();
		await writeFile(path.join(root, ".kitignore"), ".*\n");
		await writeFile(path.join(root, ".gitignore"), "ignored\n");

		const result = await scanFiles(root, { includeIgnoreFiles: true });

		expect(result.files).toContain(".kitignore");
		expect(result.files).toContain(".gitignore");
	});

	test("includes file symlinks without traversing directory symlinks", async () => {
		const root = await createTempDir();
		await mkdir(path.join(root, "real-dir"));
		await writeFile(path.join(root, "target.txt"), "target");
		await writeFile(path.join(root, "real-dir", "nested.txt"), "nested");
		await symlink("target.txt", path.join(root, "linked.txt"));
		await symlink("real-dir", path.join(root, "linked-dir"));

		const result = await scanFiles(root);

		expect(result.files).toContain("linked.txt");
		expect(result.files).not.toContain("linked-dir/nested.txt");
	});

	test("supports cancelling a traversal", async () => {
		const root = await createTempDir();
		const controller = new AbortController();
		controller.abort();

		await expect(
			scanFiles(root, { signal: controller.signal }),
		).rejects.toThrow();
	});

	test("traverses a re-included directory only when its contents are unignored", async () => {
		const root = await createTempDir();
		await mkdir(path.join(root, "ignored", "keep"), { recursive: true });
		await mkdir(path.join(root, "ignored", "drop"), { recursive: true });
		await writeFile(path.join(root, ".gitignore"), "ignored/*\n");
		await writeFile(
			path.join(root, "ignored", ".gitignore"),
			"!keep/\n!keep/**\n",
		);
		await writeFile(path.join(root, "ignored", "keep", "visible.txt"), "yes");
		await writeFile(path.join(root, "ignored", "drop", "hidden.txt"), "no");

		const result = await scanFiles(root);

		expect(result.dirs).toContain("ignored/keep/");
		expect(result.files).toContain("ignored/keep/visible.txt");
		expect(result.dirs).not.toContain("ignored/drop/");
		expect(result.files).not.toContain("ignored/drop/hidden.txt");
	});
});
