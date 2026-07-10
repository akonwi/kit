import { type Dirent, existsSync, readdirSync, statSync } from "node:fs";
import { mkdir, readdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { getKitPaths } from "../paths";
import { getInstalledRuntimeDir } from "../runtime/runtime-dir";
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

/**
 * Resolve the runtime module backing @akonwi/kit/plugin imports.
 *
 * Compiled-binary installs (Homebrew, GitHub releases) have no
 * node_modules anywhere above the executable, so the build ships a
 * self-contained SDK bundle in the runtime assets directory next to
 * the binary. Dev checkouts fall back to a shim pointing at the
 * repo's typebox dependency.
 */
async function findPluginSdkEntry(outdir: string): Promise<string> {
	// Only trust the installed runtime dir when actually running as the
	// compiled kit binary. In dev, process.execPath is the real bun
	// executable — a stray runtime/ directory next to it must not shadow
	// the repo's typebox dependency.
	const execName = path.basename(process.execPath);
	if (execName === "kit") {
		const runtimeDir = getInstalledRuntimeDir();
		if (runtimeDir) {
			const bundled = path.join(runtimeDir, "kit-plugin-sdk.mjs");
			if (existsSync(bundled)) return bundled;
		}
	}
	return writePluginSdkShim(outdir);
}

type InstallCommand = {
	argv: string[];
	env?: Record<string, string | undefined>;
};

/**
 * Commands to try for installing plugin dependencies, in order.
 *
 * Kit ships as a compiled Bun binary (Homebrew, GitHub releases), so
 * users may have neither bun nor npm installed. Bun's BUN_BE_BUN escape
 * hatch makes the kit executable behave as the plain `bun` CLI, letting
 * kit install plugin dependencies with its own embedded runtime. This
 * also works in dev, where process.execPath is a real bun. PATH lookups
 * remain as fallbacks.
 */
function installCommandCandidates(): InstallCommand[] {
	const candidates: InstallCommand[] = [
		{
			argv: [process.execPath, "install"],
			env: { ...process.env, BUN_BE_BUN: "1" },
		},
	];
	if (Bun.which("bun")) candidates.push({ argv: ["bun", "install"] });
	if (Bun.which("npm")) candidates.push({ argv: ["npm", "install"] });
	return candidates;
}

async function runInstallCommand(
	command: InstallCommand,
	pluginDir: string,
): Promise<{ ok: boolean; details: string }> {
	const proc = Bun.spawn(command.argv, {
		cwd: pluginDir,
		env: command.env,
		stdout: "pipe",
		stderr: "pipe",
	});
	const [exitCode, stdoutText, stderrText] = await Promise.all([
		proc.exited,
		new Response(proc.stdout).text(),
		new Response(proc.stderr).text(),
	]);
	const details = [stderrText.trim(), stdoutText.trim()]
		.filter(Boolean)
		.join("\n");
	return { ok: exitCode === 0, details };
}

async function installPluginDependencies(pluginDir: string): Promise<void> {
	if (!existsSync(path.join(pluginDir, "package.json"))) return;
	const failures: string[] = [];
	for (const command of installCommandCandidates()) {
		try {
			const result = await runInstallCommand(command, pluginDir);
			if (result.ok) return;
			failures.push(`${command.argv.join(" ")}:\n${result.details}`);
		} catch (error) {
			failures.push(`${command.argv.join(" ")}: ${formatError(error)}`);
		}
	}
	throw new Error(
		`Failed to install plugin dependencies in ${pluginDir}${
			failures.length > 0 ? `:\n${failures.join("\n")}` : "."
		}`,
	);
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
						pluginSdkShim ??= await findPluginSdkEntry(outdir);
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
