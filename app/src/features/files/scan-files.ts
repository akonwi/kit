/**
 * Lazy file scanner that walks a directory tree respecting:
 *   - built-in excludes (.git, node_modules, etc.)
 *   - .gitignore files (hierarchical)
 *   - .kitignore files
 *
 * Returns relative paths (files as "path/to/file", directories as "path/to/dir/").
 */

import { readdir, readFile, stat } from "node:fs/promises";
import path from "node:path";
import ignore, { type Ignore } from "ignore";

const DEFAULT_MAX_ENTRIES = 4000;
const KIT_IGNORE_FILE = ".kitignore";

/** Directories always excluded regardless of ignore files */
const BUILT_IN_EXCLUDES = new Set([".git", "node_modules"]);

// ── Ignore file loading ─────────────────────────────────────────────

async function tryReadFile(
	filePath: string,
	signal?: AbortSignal,
): Promise<string | null> {
	try {
		return await readFile(filePath, { encoding: "utf8", signal });
	} catch {
		signal?.throwIfAborted();
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

async function loadIgnoreForDir(
	dir: string,
	options: ScanFilesOptions,
): Promise<IgnoreScope> {
	options.signal?.throwIfAborted();
	const matcher = ignore();

	if (options.respectGitignore !== false) {
		const gitignoreContent = await tryReadFile(
			path.join(dir, ".gitignore"),
			options.signal,
		);
		if (gitignoreContent) {
			matcher.add(gitignoreContent);
		}
	}

	const kitIgnoreContent = await tryReadFile(
		path.join(dir, KIT_IGNORE_FILE),
		options.signal,
	);
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

export type ScanFilesOptions = {
	/** Maximum combined files and directories returned. */
	maxEntries?: number;
	/** Include .gitignore and .kitignore themselves in the result. */
	includeIgnoreFiles?: boolean;
	/** Apply hierarchical .gitignore rules. Defaults to true. */
	respectGitignore?: boolean;
	/** Cancel an in-progress traversal. */
	signal?: AbortSignal;
};

/**
 * Scan `cwd` for files and directories, respecting ignore rules.
 * Returns relative paths sorted alphabetically.
 * Files are plain paths, directories end with "/".
 */
export async function scanFiles(
	cwd: string,
	options: ScanFilesOptions = {},
): Promise<ScanResult> {
	const files: string[] = [];
	const dirs: string[] = [];
	const maxEntries = options.maxEntries ?? DEFAULT_MAX_ENTRIES;

	type StackEntry = {
		dir: string;
		/** Ignore filters, each scoped to the directory that declared it. */
		ignoreChain: IgnoreScope[];
	};

	// Build the root-level ignore
	options.signal?.throwIfAborted();
	const rootIgnore = await loadIgnoreForDir(cwd, options);
	const stack: StackEntry[] = [{ dir: cwd, ignoreChain: [rootIgnore] }];

	while (stack.length > 0 && files.length + dirs.length < maxEntries) {
		options.signal?.throwIfAborted();
		const next = stack.pop();
		if (!next) break;
		const { dir, ignoreChain } = next;

		let rawEntries: Array<{
			name: string;
			isDirectory(): boolean;
			isFile(): boolean;
			isSymbolicLink(): boolean;
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
			options.signal?.throwIfAborted();
			if (files.length + dirs.length >= maxEntries) break;

			const name = String(entry.name);
			const full = path.join(dir, name);
			const relative = toIgnorePath(path.relative(cwd, full));

			if (entry.isDirectory()) {
				// Always skip built-in excludes
				if (BUILT_IN_EXCLUDES.has(name)) continue;

				if (isIgnored(full, true, ignoreChain)) continue;

				dirs.push(`${relative}/`);

				// Load ignore file for this subdirectory (may add rules)
				options.signal?.throwIfAborted();
				const subIgnore = await loadIgnoreForDir(full, options);
				subdirs.push({
					full,
					relative,
					ignoreChain: [...ignoreChain, subIgnore],
				});
				continue;
			}

			let isFile = entry.isFile();
			if (!isFile && entry.isSymbolicLink()) {
				try {
					// Include symlinked files, but never traverse symlinked directories.
					isFile = (await stat(full)).isFile();
				} catch {
					continue;
				}
			}
			if (!isFile) continue;

			// Never expose gitlink metadata files from submodules.
			if (name === ".git") continue;
			const isIgnoreFile = name === ".gitignore" || name === KIT_IGNORE_FILE;
			if (isIgnoreFile) {
				if (options.includeIgnoreFiles) files.push(relative);
				continue;
			}

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
