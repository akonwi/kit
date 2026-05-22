import { afterEach, describe, expect, test } from "bun:test";
import { existsSync, utimesSync } from "node:fs";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { loadExternalPlugins } from "./external";
import type { PluginAPI, ThemeConfig } from "./types";

const tempDirs: string[] = [];

async function makeTempDir(): Promise<string> {
	const dir = await mkdtemp(path.join(tmpdir(), "kit-plugins-test-"));
	tempDirs.push(dir);
	return dir;
}

afterEach(async () => {
	await Promise.all(
		tempDirs.splice(0).map((dir) => rm(dir, { force: true, recursive: true })),
	);
});

function createMockKit(logs: string[]): PluginAPI {
	return {
		logger: { log: (...args) => logs.push(args.join(" ")) },
		ui: {
			text: (text, style) => ({ __kitText: true, text, style }),
			theme: () => ({}) as ThemeConfig,
			toast: () => {},
			select: async () => undefined,
			input: async () => undefined,
			confirm: async () => false,
		},
		session: {
			get: () => ({}) as ReturnType<PluginAPI["session"]["get"]>,
			getMessages: () => [],
			setName: async () => {},
			submitMessage: async () => {},
			submitPromptCommandMessage: async () => {},
		},
		settings: {
			get: () => ({}),
			update: async () => {},
		},
		model: { getCurrent: () => undefined },
		footer: { set: () => {}, clear: () => {}, hide: () => () => {} },
		header: { set: () => {}, clear: () => {}, hide: () => () => {} },
		system: {
			cwd: process.cwd(),
			open: async () => {},
		},
		on: () => () => {},
		registerCommand: () => () => {},
		registerTool: () => () => {},
		onToolCall: () => () => {},
		addSystemPrompt: () => () => {},
		addDebugSection: () => () => {},
	};
}

describe("external plugin loading", () => {
	test("loads user and project .kit plugins but ignores .agents plugins", async () => {
		const home = await makeTempDir();
		const cwd = await makeTempDir();
		await mkdir(path.join(home, ".kit", "plugins"), { recursive: true });
		await mkdir(path.join(cwd, ".kit", "plugins"), { recursive: true });
		await mkdir(path.join(cwd, ".agents", "plugins"), { recursive: true });
		await writeFile(
			path.join(home, ".kit", "plugins", "user.ts"),
			[
				'import type { PluginAPI } from "@akonwi/kit/plugin";',
				"export default function UserPlugin(kit: PluginAPI) { kit.logger.log('user') }",
				"",
			].join("\n"),
		);
		await writeFile(
			path.join(cwd, ".kit", "plugins", "project.ts"),
			[
				'import type { PluginAPI } from "@akonwi/kit/plugin";',
				"export default function ProjectPlugin(kit: PluginAPI) { kit.logger.log('project') }",
				"",
			].join("\n"),
		);
		await writeFile(
			path.join(cwd, ".agents", "plugins", "ignored.ts"),
			"export default function IgnoredPlugin(kit) { kit.logger.log('ignored') }\n",
		);

		const result = await loadExternalPlugins(cwd, {
			reloadId: "initial",
			home,
		});
		expect(result.failures).toEqual([]);
		expect(result.plugins.map((plugin) => plugin.name)).toEqual([
			"user:user",
			"project:project",
		]);

		const logs: string[] = [];
		const kit = createMockKit(logs);
		for (const plugin of result.plugins) plugin.initialize(kit);
		expect(logs).toEqual(["user", "project"]);
	});

	test("loads plugin dependencies from the plugin directory", async () => {
		const home = await makeTempDir();
		const cwd = await makeTempDir();
		const pluginsDir = path.join(home, ".kit", "plugins");
		const depDir = path.join(pluginsDir, "node_modules", "plugin-local-dep");
		await mkdir(depDir, { recursive: true });
		await writeFile(
			path.join(depDir, "package.json"),
			JSON.stringify({
				name: "plugin-local-dep",
				version: "1.0.0",
				type: "module",
			}),
		);
		await writeFile(
			path.join(depDir, "index.js"),
			"export function message() { return 'from plugin dependency' }\n",
		);
		await writeFile(
			path.join(pluginsDir, "uses-dep.ts"),
			[
				'import { Type } from "@akonwi/kit/plugin";',
				'import { message } from "plugin-local-dep";',
				"export default function UsesDepPlugin(kit) { kit.logger.log(message() + ' ' + typeof Type.Object) }",
				"",
			].join("\n"),
		);

		const result = await loadExternalPlugins(cwd, { reloadId: "deps", home });
		expect(result.failures).toEqual([]);
		expect(result.plugins.map((plugin) => plugin.name)).toEqual([
			"user:uses-dep",
		]);

		const logs: string[] = [];
		result.plugins[0]?.initialize(createMockKit(logs));
		expect(logs).toEqual(["from plugin dependency function"]);
	});

	test("prunes stale plugin cache dirs after loading", async () => {
		const home = await makeTempDir();
		const cwd = await makeTempDir();
		const pluginsDir = path.join(cwd, ".kit", "plugins");
		await mkdir(pluginsDir, { recursive: true });
		await writeFile(
			path.join(pluginsDir, "cached.ts"),
			"export default function CachedPlugin(kit) { kit.logger.log('cached') }\n",
		);

		await loadExternalPlugins(cwd, { reloadId: "cache-one", home });
		const cacheRoot = path.join(home, ".kit", "plugin-cache");
		expect(existsSync(path.join(cacheRoot, "cache-one"))).toBe(true);

		await loadExternalPlugins(cwd, { reloadId: "cache-two", home });
		expect(existsSync(path.join(cacheRoot, "cache-one"))).toBe(false);
		expect(existsSync(path.join(cacheRoot, "cache-two"))).toBe(true);
	});

	test("reports load failures and reloads changed plugin files", async () => {
		const home = await makeTempDir();
		const cwd = await makeTempDir();
		const pluginsDir = path.join(cwd, ".kit", "plugins");
		const pluginPath = path.join(pluginsDir, "dynamic.ts");
		await mkdir(pluginsDir, { recursive: true });
		await writeFile(
			path.join(pluginsDir, "bad.ts"),
			"export const notDefault = true;\n",
		);
		await writeFile(
			pluginPath,
			"export default function DynamicPlugin(kit) { kit.logger.log('one') }\n",
		);

		const first = await loadExternalPlugins(cwd, { reloadId: "one", home });
		expect(first.failures).toHaveLength(1);
		expect(first.failures[0]?.phase).toBe("load");
		expect(first.plugins.map((plugin) => plugin.name)).toEqual([
			"project:dynamic",
		]);

		const firstLogs: string[] = [];
		first.plugins[0]?.initialize(createMockKit(firstLogs));
		expect(firstLogs).toEqual(["one"]);

		await writeFile(
			pluginPath,
			"export default function DynamicPlugin(kit) { kit.logger.log('two') }\n",
		);
		const future = new Date(Date.now() + 2000);
		utimesSync(pluginPath, future, future);
		const second = await loadExternalPlugins(cwd, { reloadId: "two", home });
		const secondLogs: string[] = [];
		second.plugins[0]?.initialize(createMockKit(secondLogs));
		expect(secondLogs).toEqual(["two"]);
	});
});
