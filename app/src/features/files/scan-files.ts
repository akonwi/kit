/**
 * Lazy file scanner that walks a directory tree respecting:
 *   - built-in excludes (.git, node_modules, etc.)
 *   - .gitignore files (hierarchical)
 *   - .kitignore files
 *
 * Returns relative paths (files as "path/to/file", directories as "path/to/dir/").
 */

import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import ignore, { type Ignore } from "ignore";

const MAX_FILES = 4000;
const KIT_IGNORE_FILE = ".kitignore";

/** Directories always excluded regardless of ignore files */
const BUILT_IN_EXCLUDES = new Set([".git", "node_modules"]);

// ── Ignore file loading ─────────────────────────────────────────────

async function tryReadFile(filePath: string): Promise<string | null> {
	try {
		return await readFile(filePath, "utf8");
	} catch {
		return null;
	}
}

/**
 * Build an ignore filter for a given directory. Rules from .gitignore are
 * applied first, then .kitignore, so .kitignore may override .gitignore in
 * the same directory. Descendant scopes are applied after ancestor scopes.
 */
type IgnoreScope = {
	dir: string;
	matcher: Ignore;
};

async function loadIgnoreForDir(dir: string): Promise<IgnoreScope> {
	const matcher = ignore();

	const gitignoreContent = await tryReadFile(path.join(dir, ".gitignore"));
	if (gitignoreContent) {
		matcher.add(gitignoreContent);
	}

	const kitIgnoreContent = await tryReadFile(path.join(dir, KIT_IGNORE_FILE));
	if (kitIgnoreContent) {
		matcher.add(kitIgnoreContent);
	}

	return { dir, matcher };
}

function toIgnorePath(value: string): string {
	return value.split(path.sep).join("/");
}

function isIgnored(
	fullPath: string,
	isDirectory: boolean,
	ignoreChain: IgnoreScope[],
): boolean {
	let ignored = false;
	for (const scope of ignoreChain) {
		const scopedPath = toIgnorePath(path.relative(scope.dir, fullPath));
		if (!scopedPath || scopedPath === ".." || scopedPath.startsWith("../")) {
			continue;
		}
		const result = scope.matcher.test(
			isDirectory ? `${scopedPath}/` : scopedPath,
		);
		if (result.ignored) ignored = true;
		else if (result.unignored) ignored = false;
	}
	return ignored;
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
		/** Ignore filters, each scoped to the directory that declared it. */
		ignoreChain: IgnoreScope[];
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
		rawEntries.sort((a, b) => String(a.name).localeCompare(String(b.name)));

		// Collect subdirs to process — we'll push them after processing all entries
		const subdirs: {
			full: string;
			relative: string;
			ignoreChain: IgnoreScope[];
		}[] = [];

		for (const entry of rawEntries) {
			if (files.length + dirs.length >= MAX_FILES) break;

			const name = String(entry.name);
			const full = path.join(dir, name);
			const relative = toIgnorePath(path.relative(cwd, full));

			if (entry.isDirectory()) {
				// Always skip built-in excludes
				if (BUILT_IN_EXCLUDES.has(name)) continue;

				if (isIgnored(full, true, ignoreChain)) continue;

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
			if (name === ".gitignore" || name === KIT_IGNORE_FILE || name === ".git")
				continue;

			// Check ignore rules
			if (isIgnored(full, false, ignoreChain)) continue;

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
