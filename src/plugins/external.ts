import { type Dirent, existsSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
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

function loadPluginInitializer(file: PluginFile, reloadId: string): Plugin {
	const moduleExports = require(
		`${file.filePath}?kitReload=${encodeURIComponent(reloadId)}`,
	) as { default?: unknown };
	if (typeof moduleExports.default !== "function") {
		throw new Error("Plugin default export must be a function.");
	}
	return moduleExports.default as Plugin;
}

export function loadExternalPlugins(
	cwd: string,
	options: LoadExternalPluginsOptions,
): LoadExternalPluginsResult {
	const plugins: ExternalPluginRegistration[] = [];
	const failures: ExternalPluginFailure[] = [];

	for (const file of discoverPluginFiles(cwd, options.home)) {
		const pluginName = pluginNameForFile(file.source, file.filePath);
		let initialize: Plugin;
		try {
			initialize = loadPluginInitializer(file, options.reloadId);
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

	return { plugins, failures };
}
