import { type Dirent, existsSync, readdirSync, statSync } from "node:fs";
import { mkdir, readdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { getKitPaths } from "../paths";
import type { ExternalPluginRegistration } from "./PluginManager";
import type { Plugin } from "./types";

export type ExternalPluginSource = "user" | "project";

export type ExternalPluginFailurePhase = "load" | "initialize";

export type ExternalPluginFailure = {
	source: ExternalPluginSource;
	phase: ExternalPluginFailurePhase;
	pluginName: string;
	filePath: string;
	message: string;
};

export type LoadExternalPluginsResult = {
	plugins: ExternalPluginRegistration[];
	failures: ExternalPluginFailure[];
};

export type LoadExternalPluginsOptions = {
	reloadId: string;
	onFailure?: (failure: ExternalPluginFailure) => void;
	home?: string;
};

type PluginFile = {
	source: ExternalPluginSource;
	filePath: string;
};

function formatError(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}

function pluginNameForFile(
	source: ExternalPluginSource,
	filePath: string,
): string {
	const baseName = path.basename(filePath).replace(/\.ts$/, "");
	return `${source}:${baseName}`;
}

function sanitizeFileName(value: string): string {
	return value.replace(/[^a-zA-Z0-9_.-]+/g, "-");
}

const activePluginCacheReloadIdsByRoot = new Map<string, Set<string>>();
const retainedPluginCacheReloadIdsByRoot = new Map<string, string>();

function getActivePluginCacheReloadIds(cacheRoot: string): Set<string> {
	let active = activePluginCacheReloadIdsByRoot.get(cacheRoot);
	if (!active) {
		active = new Set<string>();
		activePluginCacheReloadIdsByRoot.set(cacheRoot, active);
	}
	return active;
}

async function prunePluginCache(cacheRoot: string): Promise<void> {
	let entries: Dirent[];
	try {
		entries = await readdir(cacheRoot, { withFileTypes: true });
	} catch {
		return;
	}

	await Promise.all(
		entries.map(async (entry) => {
			const activeReloadIds = activePluginCacheReloadIdsByRoot.get(cacheRoot);
			const retainedReloadId =
				retainedPluginCacheReloadIdsByRoot.get(cacheRoot);
			if (
				!entry.isDirectory() ||
				entry.name === retainedReloadId ||
				activeReloadIds?.has(entry.name)
			) {
				return;
			}
			await rm(path.join(cacheRoot, entry.name), {
				force: true,
				recursive: true,
			});
		}),
	);
}

function recordFailure(
	failures: ExternalPluginFailure[],
	failure: ExternalPluginFailure,
	onFailure?: (failure: ExternalPluginFailure) => void,
): void {
	failures.push(failure);
	onFailure?.(failure);
}

function scanPluginDirectory(
	dir: string,
	source: ExternalPluginSource,
): PluginFile[] {
	if (!existsSync(dir)) return [];
	const files: PluginFile[] = [];
	let entries: Dirent[];
	try {
		entries = readdirSync(dir, { withFileTypes: true });
	} catch {
		return [];
	}

	for (const entry of entries) {
		if (!entry.name.endsWith(".ts")) continue;
		const filePath = path.join(dir, entry.name);
		let isFile = entry.isFile();
		if (entry.isSymbolicLink()) {
			try {
				isFile = statSync(filePath).isFile();
			} catch {
				continue;
			}
		}
		if (!isFile) continue;
		files.push({ source, filePath });
	}

	return files.sort((a, b) => a.filePath.localeCompare(b.filePath));
}

function discoverPluginFiles(cwd: string, home?: string): PluginFile[] {
	const kitPaths = getKitPaths(home);
	return [
		...scanPluginDirectory(path.join(kitPaths.kitRoot, "plugins"), "user"),
		...scanPluginDirectory(path.resolve(cwd, ".kit", "plugins"), "project"),
	];
}

async function findTypeboxEntry(): Promise<string | null> {
	const candidates: string[] = [];

	// Source/dev checkout: src/plugins/external.ts -> repo/node_modules/typebox.
	candidates.push(
		path.resolve(import.meta.dirname, "../../node_modules/typebox"),
	);

	let current = path.dirname(process.execPath);
	while (true) {
		candidates.push(path.join(current, "node_modules", "typebox"));
		const parent = path.dirname(current);
		if (parent === current) break;
		current = parent;
	}

	for (const packageRoot of candidates) {
		const packagePath = path.join(packageRoot, "package.json");
		if (!existsSync(packagePath)) continue;
		try {
			const packageJson = JSON.parse(await readFile(packagePath, "utf8")) as {
				module?: string;
			};
			return path.join(packageRoot, packageJson.module ?? "build/index.mjs");
		} catch {
			return path.join(packageRoot, "build/index.mjs");
		}
	}

	return null;
}

async function writePluginSdkShim(outdir: string): Promise<string> {
	const typeboxEntry = await findTypeboxEntry();
	if (!typeboxEntry) {
		throw new Error(
			"Unable to locate Kit's bundled typebox dependency for @akonwi/kit/plugin.",
		);
	}
	const shimPath = path.join(outdir, "kit-plugin-sdk.mjs");
	await writeFile(
		shimPath,
		`export { Type } from ${JSON.stringify(typeboxEntry)};\n`,
		"utf8",
	);
	return shimPath;
}

async function installPluginDependencies(pluginDir: string): Promise<void> {
	if (!existsSync(path.join(pluginDir, "package.json"))) return;
	const pm = Bun.which("bun") ? "bun" : Bun.which("npm") ? "npm" : null;
	if (!pm) {
		throw new Error(
			`Cannot install plugin dependencies in ${pluginDir}: neither bun nor npm found in PATH.`,
		);
	}
	const proc = Bun.spawn([pm, "install"], {
		cwd: pluginDir,
		stdout: "pipe",
		stderr: "pipe",
	});
	const [exitCode, stdoutText, stderrText] = await Promise.all([
		proc.exited,
		new Response(proc.stdout).text(),
		new Response(proc.stderr).text(),
	]);
	if (exitCode !== 0) {
		const details = [stderrText.trim(), stdoutText.trim()]
			.filter(Boolean)
			.join("\n");
		throw new Error(
			`Failed to install plugin dependencies in ${pluginDir}${details ? `:\n${details}` : "."}`,
		);
	}
}

function bundleFailureMessage(result: Bun.BuildOutput): string {
	return (
		result.logs.map((log) => String(log)).join("\n") || "Unknown build error"
	);
}

async function bundlePlugin(
	file: PluginFile,
	options: {
		pluginName: string;
		reloadId: string;
		home?: string;
	},
): Promise<string> {
	const cacheRoot = path.join(
		getKitPaths(options.home).kitRoot,
		"plugin-cache",
	);
	const outdir = path.join(
		cacheRoot,
		sanitizeFileName(options.reloadId),
		sanitizeFileName(options.pluginName),
	);
	await rm(outdir, { force: true, recursive: true });
	await mkdir(outdir, { recursive: true });

	let pluginSdkShim: string | null = null;
	const result = await Bun.build({
		entrypoints: [file.filePath],
		outdir,
		target: "bun",
		format: "esm",
		plugins: [
			{
				name: "kit-plugin-sdk",
				setup(build) {
					build.onResolve({ filter: /^@akonwi\/kit\/plugin$/ }, async () => {
						pluginSdkShim ??= await writePluginSdkShim(outdir);
						return { path: pluginSdkShim };
					});
				},
			},
		],
	});

	if (!result.success) {
		throw new Error(bundleFailureMessage(result));
	}

	const output = result.outputs.find(
		(artifact) => artifact.kind === "entry-point",
	);
	if (!output) {
		throw new Error("Plugin build did not produce an entry point.");
	}
	return output.path;
}

async function loadPluginInitializer(
	file: PluginFile,
	options: {
		pluginName: string;
		reloadId: string;
		home?: string;
	},
): Promise<Plugin> {
	// Bundle external plugins before importing them. This makes user/project
	// plugins behave like complete packages: imports are resolved from the plugin
	// file's directory, so ~/.kit/plugins/package.json and .kit/plugins/package.json
	// can declare dependencies Kit itself does not ship. Importing the bundled
	// artifact also avoids Bun compiled-binary limitations around requiring
	// arbitrary external TypeScript files with cache-busting query strings.
	// Dependencies are installed before this point by loadExternalPlugins.
	const bundledPath = await bundlePlugin(file, options);
	const url = pathToFileURL(bundledPath);
	url.searchParams.set("kitReload", options.reloadId);
	const moduleExports = (await import(url.href)) as { default?: unknown };
	if (typeof moduleExports.default !== "function") {
		throw new Error("Plugin default export must be a function.");
	}
	return moduleExports.default as Plugin;
}

export async function loadExternalPlugins(
	cwd: string,
	options: LoadExternalPluginsOptions,
): Promise<LoadExternalPluginsResult> {
	const plugins: ExternalPluginRegistration[] = [];
	const failures: ExternalPluginFailure[] = [];
	const cacheRoot = path.join(
		getKitPaths(options.home).kitRoot,
		"plugin-cache",
	);
	const reloadCacheId = sanitizeFileName(options.reloadId);
	const activeReloadIds = getActivePluginCacheReloadIds(cacheRoot);
	activeReloadIds.add(reloadCacheId);

	try {
		await rm(path.join(cacheRoot, reloadCacheId), {
			force: true,
			recursive: true,
		});

		const files = discoverPluginFiles(cwd, options.home);

		// Install dependencies once per unique plugin directory before bundling.
		const installedDirs = new Set<string>();
		const failedDirs = new Map<string, string>();
		for (const file of files) {
			const pluginDir = path.dirname(file.filePath);
			if (installedDirs.has(pluginDir) || failedDirs.has(pluginDir)) continue;
			try {
				await installPluginDependencies(pluginDir);
				installedDirs.add(pluginDir);
			} catch (error) {
				failedDirs.set(pluginDir, formatError(error));
			}
		}

		for (const file of files) {
			const pluginName = pluginNameForFile(file.source, file.filePath);
			const installError = failedDirs.get(path.dirname(file.filePath));
			if (installError) {
				recordFailure(
					failures,
					{
						source: file.source,
						phase: "load",
						pluginName,
						filePath: file.filePath,
						message: installError,
					},
					options.onFailure,
				);
				continue;
			}
			let initialize: Plugin;
			try {
				initialize = await loadPluginInitializer(file, {
					pluginName,
					reloadId: options.reloadId,
					home: options.home,
				});
			} catch (error) {
				recordFailure(
					failures,
					{
						source: file.source,
						phase: "load",
						pluginName,
						filePath: file.filePath,
						message: formatError(error),
					},
					options.onFailure,
				);
				continue;
			}

			plugins.push({
				name: pluginName,
				initialize,
				continueOnError: true,
				checkContributionConflicts: true,
				onError: ({ error }) => {
					recordFailure(
						failures,
						{
							source: file.source,
							phase: "initialize",
							pluginName,
							filePath: file.filePath,
							message: formatError(error),
						},
						options.onFailure,
					);
				},
			});
		}

		retainedPluginCacheReloadIdsByRoot.set(cacheRoot, reloadCacheId);
		return { plugins, failures };
	} finally {
		activeReloadIds.delete(reloadCacheId);
		await prunePluginCache(cacheRoot);
		if (activeReloadIds.size === 0) {
			activePluginCacheReloadIdsByRoot.delete(cacheRoot);
		}
	}
}
