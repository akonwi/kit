import {
	existsSync,
	readdirSync,
	readFileSync,
	type Stats,
	statSync,
} from "node:fs";
import path from "node:path";
import { getKitPaths } from "../paths";

export type ContextFile = {
	path: string;
	content: string;
};

function loadContextFileFromDir(dir: string): ContextFile | null {
	for (const filename of ["AGENTS.md", "CLAUDE.md"]) {
		const filePath = path.join(dir, filename);
		if (!existsSync(filePath)) continue;
		try {
			return {
				path: filePath,
				content: readFileSync(filePath, "utf8"),
			};
		} catch {
			return null;
		}
	}
	return null;
}

function loadGlobalContextFile(): ContextFile | null {
	const globalContextPath = path.join(getKitPaths().kitRoot, "AGENTS.md");
	if (!existsSync(globalContextPath)) return null;
	try {
		return {
			path: globalContextPath,
			content: readFileSync(globalContextPath, "utf8"),
		};
	} catch {
		return null;
	}
}

function collectAncestorContextFiles(
	startDir: string,
	seenPaths: Set<string>,
): ContextFile[] {
	const ancestors: ContextFile[] = [];
	let currentDir = path.resolve(startDir);
	const root = path.parse(currentDir).root;
	while (true) {
		const contextFile = loadContextFileFromDir(currentDir);
		if (contextFile && !seenPaths.has(contextFile.path)) {
			ancestors.unshift(contextFile);
			seenPaths.add(contextFile.path);
		}
		if (currentDir === root) break;
		const parentDir = path.resolve(currentDir, "..");
		if (parentDir === currentDir) break;
		currentDir = parentDir;
	}
	return ancestors;
}

export function discoverChildContextFiles(
	cwd: string,
	options?: { seenPaths?: Iterable<string> },
): ContextFile[] {
	const seenPaths = new Set(options?.seenPaths ?? []);
	const results: ContextFile[] = [];

	let entries: string[];
	try {
		entries = readdirSync(cwd, { encoding: "utf8" });
	} catch {
		return results;
	}

	for (const entryName of entries) {
		const entryPath = path.join(cwd, entryName);
		let stats: Stats;
		try {
			stats = statSync(entryPath);
		} catch {
			continue;
		}
		if (!stats.isDirectory()) continue;
		const contextFile = loadContextFileFromDir(entryPath);
		if (!contextFile || seenPaths.has(contextFile.path)) continue;
		seenPaths.add(contextFile.path);
		results.push(contextFile);
	}

	results.sort((a, b) => a.path.localeCompare(b.path));
	return results;
}

export function discoverContextFiles(cwd: string): ContextFile[] {
	const contextFiles: ContextFile[] = [];
	const seenPaths = new Set<string>();

	const globalContext = loadGlobalContextFile();
	if (globalContext) {
		contextFiles.push(globalContext);
		seenPaths.add(globalContext.path);
	}

	contextFiles.push(...collectAncestorContextFiles(cwd, seenPaths));
	contextFiles.push(...discoverChildContextFiles(cwd, { seenPaths }));
	return contextFiles;
}

export function buildSystemPrompt(
	basePrompt: string,
	contextFiles: ContextFile[],
): string {
	if (contextFiles.length === 0) return basePrompt;

	const renderedFiles = contextFiles
		.map(
			(file) =>
				`<context-file path=${JSON.stringify(file.path)}>
${file.content}
</context-file>`,
		)
		.join("\n\n");

	return `${basePrompt}

Additional context guidance has been loaded from project context files. Follow it unless it conflicts with higher-priority instructions.

<context-files>
${renderedFiles}
</context-files>`;
}
