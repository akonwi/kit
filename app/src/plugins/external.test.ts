import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { discoverExternalPluginManifests } from "./external";

const tempDirs: string[] = [];

async function makeTempDir(): Promise<string> {
	const dir = await mkdtemp(path.join(tmpdir(), "kit-plugins-test-"));
	tempDirs.push(dir);
	return dir;
}

async function writeManifest(
	directory: string,
	id: string,
	overrides: Record<string, unknown> = {},
): Promise<string> {
	await mkdir(directory, { recursive: true });
	const manifestPath = path.join(directory, "plugin.json");
	await writeFile(
		manifestPath,
		JSON.stringify({
			manifestVersion: 1,
			id,
			transport: { type: "stdio", command: "python3", args: ["plugin.py"] },
			...overrides,
		}),
	);
	return manifestPath;
}

afterEach(async () => {
	await Promise.all(
		tempDirs.splice(0).map((dir) => rm(dir, { force: true, recursive: true })),
	);
});

describe("external plugin manifest discovery", () => {
	test("discovers user before project and sorts installation directories", async () => {
		const home = await makeTempDir();
		const cwd = await makeTempDir();
		await writeManifest(path.join(home, ".kit/plugins/z-user"), "zeta");
		await writeManifest(path.join(home, ".kit/plugins/a-user"), "alpha");
		await writeManifest(path.join(cwd, ".kit/plugins/b-project"), "project-b");
		await writeManifest(path.join(cwd, ".kit/plugins/a-project"), "project-a");
		await writeFile(
			path.join(home, ".kit/plugins/legacy.ts"),
			"export default {};",
		);

		const result = discoverExternalPluginManifests(cwd, { home });
		expect(result.failures).toEqual([]);
		expect(
			result.manifests.map(
				(manifest) =>
					`${manifest.source}:${manifest.installationName}:${manifest.manifest.id}`,
			),
		).toEqual([
			"user:a-user:alpha",
			"user:z-user:zeta",
			"project:a-project:project-a",
			"project:b-project:project-b",
		]);
	});

	test("follows installation-directory symlinks but not nested manifests", async () => {
		const home = await makeTempDir();
		const cwd = await makeTempDir();
		const standalone = path.join(home, "standalone-plugin");
		await writeManifest(standalone, "linked");
		await mkdir(path.join(cwd, ".kit/plugins"), { recursive: true });
		await symlink(standalone, path.join(cwd, ".kit/plugins/linked-install"));
		await writeManifest(
			path.join(cwd, ".kit/plugins/outer/nested"),
			"too-deep",
		);

		const result = discoverExternalPluginManifests(cwd, { home });
		expect(result.manifests.map((manifest) => manifest.manifest.id)).toEqual([
			"linked",
		]);
	});

	test("reports invalid manifests without reserving command domains", async () => {
		const home = await makeTempDir();
		const cwd = await makeTempDir();
		const invalidDir = path.join(home, ".kit/plugins/invalid");
		await mkdir(invalidDir, { recursive: true });
		await writeFile(path.join(invalidDir, "plugin.json"), "not json");
		await writeManifest(path.join(cwd, ".kit/plugins/help"), "help");

		const result = discoverExternalPluginManifests(cwd, { home });
		expect(result.manifests.map((manifest) => manifest.manifest.id)).toEqual([
			"help",
		]);
		expect(result.failures.map((failure) => failure.phase)).toEqual([
			"manifest",
		]);
	});

	test("keeps the first duplicate id and reports both manifest paths", async () => {
		const home = await makeTempDir();
		const cwd = await makeTempDir();
		const firstPath = await writeManifest(
			path.join(home, ".kit/plugins/first"),
			"speech",
		);
		const secondPath = await writeManifest(
			path.join(cwd, ".kit/plugins/second"),
			"speech",
		);

		const result = discoverExternalPluginManifests(cwd, { home });
		expect(result.manifests).toHaveLength(1);
		expect(result.manifests[0]?.manifestPath).toBe(firstPath);
		expect(result.failures).toEqual([
			expect.objectContaining({
				phase: "duplicate",
				pluginId: "speech",
				manifestPath: secondPath,
				otherManifestPath: firstPath,
			}),
		]);
	});

	test("uses existing user ownership when rotating project manifests", async () => {
		const home = await makeTempDir();
		const cwd = await makeTempDir();
		await writeManifest(path.join(home, ".kit/plugins/user"), "owned");
		await writeManifest(path.join(cwd, ".kit/plugins/project"), "owned");
		const user = discoverExternalPluginManifests(cwd, {
			home,
			includeProject: false,
		}).manifests;

		const project = discoverExternalPluginManifests(cwd, {
			home,
			includeUser: false,
			existingManifests: user,
		});
		expect(project.manifests).toEqual([]);
		expect(project.failures[0]).toMatchObject({
			phase: "duplicate",
			pluginId: "owned",
			otherManifestPath: user[0]?.manifestPath,
		});
	});
});
