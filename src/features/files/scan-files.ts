/**
 * Lazy file scanner that walks a directory tree respecting:
 *   - built-in excludes (.git, node_modules, etc.)
 *   - .gitignore files (hierarchical)
 *   - .pi-ignore files
 *
 * Returns relative paths (files as "path/to/file", directories as "path/to/dir/").
 */

import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import ignore, { type Ignore } from "ignore";

const MAX_FILES = 4000;
const PI_IGNORE_FILE = ".pi-ignore";

/** Directories always excluded regardless of ignore files */
const BUILT_IN_EXCLUDES = new Set([".git", "node_modules", ".pi", ".agents"]);

// ── Ignore file loading ─────────────────────────────────────────────

async function tryReadFile(filePath: string): Promise<string | null> {
	try {
		return await readFile(filePath, "utf8");
	} catch {
		return null;
	}
}

/**
 * Build an ignore filter for a given directory.
 * Merges rules from .gitignore and .pi-ignore found in that dir.
 * Hierarchy is handled by maintaining a chain of filters at the call-site.
 */
async function loadIgnoreForDir(dir: string): Promise<Ignore> {
	const ig = ignore();

	const gitignoreContent = await tryReadFile(path.join(dir, ".gitignore"));
	if (gitignoreContent) {
		ig.add(gitignoreContent);
	}

	const piIgnoreContent = await tryReadFile(path.join(dir, PI_IGNORE_FILE));
	if (piIgnoreContent) {
		ig.add(piIgnoreContent);
	}

	return ig;
}

// ── Scanner ─────────────────────────────────────────────────────────

export type ScanResult = {
	files: string[];
	dirs: string[];
};

/**
 * Scan `cwd` for files and directories, respecting ignore rules.
 * Returns relative paths sorted alphabetically.
 * Files are plain paths, directories end with "/".
 */
export async function scanFiles(cwd: string): Promise<ScanResult> {
	const files: string[] = [];
	const dirs: string[] = [];

	type StackEntry = {
		dir: string;
		/** Accumulated ignore filters from ancestor directories */
		ignoreChain: Ignore[];
	};

	// Build the root-level ignore
	const rootIgnore = await loadIgnoreForDir(cwd);
	const stack: StackEntry[] = [{ dir: cwd, ignoreChain: [rootIgnore] }];

	while (stack.length > 0 && files.length + dirs.length < MAX_FILES) {
		const next = stack.pop();
		if (!next) break;
		const { dir, ignoreChain } = next;

		let rawEntries: Array<{
			name: string;
			isDirectory(): boolean;
			isFile(): boolean;
		}>;
		try {
			rawEntries = await readdir(dir, { withFileTypes: true });
		} catch {
			continue;
		}

		// Collect subdirs to process — we'll push them after processing all entries
		const subdirs: { full: string; relative: string; ignoreChain: Ignore[] }[] =
			[];

		for (const entry of rawEntries) {
			if (files.length + dirs.length >= MAX_FILES) break;

			const name = String(entry.name);
			const full = path.join(dir, name);
			const relative = path.relative(cwd, full);

			if (entry.isDirectory()) {
				// Always skip built-in excludes
				if (BUILT_IN_EXCLUDES.has(name)) continue;

				// Check all ignore layers — use "dir/" format for directory matching
				const relativeForIgnore = `${relative}/`;
				if (ignoreChain.some((ig) => ig.ignores(relativeForIgnore))) continue;

				dirs.push(`${relative}/`);

				// Load ignore file for this subdirectory (may add rules)
				const subIgnore = await loadIgnoreForDir(full);
				subdirs.push({
					full,
					relative,
					ignoreChain: [...ignoreChain, subIgnore],
				});
				continue;
			}

			if (!entry.isFile()) continue;

			// Skip ignore files and .git (gitlink files in submodules) from results
			if (name === ".gitignore" || name === PI_IGNORE_FILE || name === ".git")
				continue;

			// Check ignore rules
			if (ignoreChain.some((ig) => ig.ignores(relative))) continue;

			files.push(relative);
		}

		// Push subdirs (reverse so alphabetical order is maintained in DFS)
		for (let i = subdirs.length - 1; i >= 0; i--) {
			stack.push({
				dir: subdirs[i].full,
				ignoreChain: subdirs[i].ignoreChain,
			});
		}
	}

	files.sort((a, b) => a.localeCompare(b));
	dirs.sort((a, b) => a.localeCompare(b));

	return { files, dirs };
}
