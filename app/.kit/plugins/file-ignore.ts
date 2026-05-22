/**
 * Project plugin: manage .kitignore entries from Kit slash commands.
 *
 * Ported from the global Pi `ignore.ts` extension, adjusted for Kit's
 * `.kitignore` file scanner and public PluginAPI command surface.
 */

import { appendFile, readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import type { CommandContext, PluginAPI } from "@akonwi/kit/plugin";

const KIT_IGNORE_FILE = ".kitignore";

function normalizeRelativePath(value: string): string {
	return value
		.replace(/\\/g, "/")
		.replace(/^\.\//, "")
		.replace(/^\/+|\/+$/g, "");
}

function isWithinDir(child: string, parent: string): boolean {
	const rel = path.relative(parent, child);
	return rel === "" || (!rel.startsWith("..") && !path.isAbsolute(rel));
}

async function findNearestIgnoreFile(
	baseDir: string,
	anchorDir: string,
): Promise<string> {
	let current = anchorDir;
	while (isWithinDir(current, baseDir)) {
		const candidate = path.join(current, KIT_IGNORE_FILE);
		try {
			const info = await stat(candidate);
			if (info.isFile()) return candidate;
		} catch {
			// Continue upward until the session root.
		}
		if (current === baseDir) break;
		const parent = path.dirname(current);
		if (parent === current) break;
		current = parent;
	}
	return path.join(baseDir, KIT_IGNORE_FILE);
}

function normalizeIgnoreEntry(raw: string): string {
	const trimmed = raw.trim();
	if (!trimmed || trimmed.startsWith("#")) return "";
	const directoryOnly = trimmed.endsWith("/");
	const normalized = normalizeRelativePath(
		directoryOnly ? trimmed.slice(0, -1) : trimmed,
	);
	return directoryOnly ? `${normalized}/` : normalized;
}

async function appendIgnoreEntry(
	baseDir: string,
	targetPath: string,
	isDirectory: boolean,
): Promise<{
	ignoreFile: string;
	entry: string;
	created: boolean;
	duplicate: boolean;
}> {
	const anchorDir = isDirectory ? targetPath : path.dirname(targetPath);
	const ignoreFile = await findNearestIgnoreFile(baseDir, anchorDir);
	const ignoreDir = path.dirname(ignoreFile);
	const relative = normalizeRelativePath(path.relative(ignoreDir, targetPath));
	const entry = isDirectory ? `${relative}/` : relative;

	let existing = "";
	let created = false;
	try {
		existing = await readFile(ignoreFile, "utf8");
	} catch {
		created = true;
	}

	const existingEntries = new Set(
		existing.split(/\r?\n/g).map(normalizeIgnoreEntry).filter(Boolean),
	);

	if (existingEntries.has(entry)) {
		return { ignoreFile, entry, created: false, duplicate: true };
	}

	const needsLeadingNewline = existing.length > 0 && !existing.endsWith("\n");
	await appendFile(
		ignoreFile,
		`${needsLeadingNewline ? "\n" : ""}${entry}\n`,
		"utf8",
	);
	return { ignoreFile, entry, created, duplicate: false };
}

async function listIgnoreEntriesInFile(
	ignoreFile: string,
): Promise<{ ignoreFile: string; entry: string }[]> {
	try {
		const content = await readFile(ignoreFile, "utf8");
		return content
			.split(/\r?\n/g)
			.map(normalizeIgnoreEntry)
			.filter(Boolean)
			.map((entry) => ({ ignoreFile, entry }));
	} catch {
		return [];
	}
}

async function removeIgnoreEntryFromFile(
	ignoreFile: string,
	entry: string,
): Promise<boolean> {
	let content: string;
	try {
		content = await readFile(ignoreFile, "utf8");
	} catch {
		return false;
	}
	const lines = content.split(/\r?\n/g);
	const kept = lines.filter((line) => normalizeIgnoreEntry(line) !== entry);
	if (kept.length === lines.length) return false;
	await writeFile(
		ignoreFile,
		`${kept.join("\n").replace(/\n+$/g, "")}\n`,
		"utf8",
	);
	return true;
}

async function removeIgnoreEntryByPath(
	baseDir: string,
	targetPath: string,
): Promise<{ ignoreFile?: string; entry?: string; removed: boolean }> {
	let anchorDir = path.dirname(targetPath);
	try {
		const info = await stat(targetPath);
		anchorDir = info.isDirectory() ? targetPath : path.dirname(targetPath);
	} catch {
		// The path may already be ignored or deleted; search from its parent.
	}

	const searchDirs: string[] = [];
	let current = anchorDir;
	while (isWithinDir(current, baseDir)) {
		searchDirs.push(current);
		if (current === baseDir) break;
		const parent = path.dirname(current);
		if (parent === current) break;
		current = parent;
	}

	for (const dir of searchDirs) {
		const ignoreFile = path.join(dir, KIT_IGNORE_FILE);
		const ignoreDir = path.dirname(ignoreFile);
		const relative = normalizeRelativePath(
			path.relative(ignoreDir, targetPath),
		);
		const candidates = Array.from(
			new Set([relative, `${relative}/`].filter(Boolean)),
		);
		const entries = await listIgnoreEntriesInFile(ignoreFile);
		for (const candidate of candidates) {
			if (!entries.some((item) => item.entry === candidate)) continue;
			const removed = await removeIgnoreEntryFromFile(ignoreFile, candidate);
			if (removed) return { ignoreFile, entry: candidate, removed: true };
		}
	}

	return { removed: false };
}

function toast(
	ctx: CommandContext,
	title: string,
	variant: Parameters<CommandContext["ui"]["toast"]>[0]["variant"],
	lines: string[] = [],
): void {
	ctx.ui.toast({
		title,
		variant,
		...(lines.length > 0 ? { subtitle: lines.join("\n") } : {}),
	});
}

export default function FileIgnorePlugin(kit: PluginAPI): void {
	kit.registerCommand(
		"files:ignore",
		{
			description: "Add a file or directory to .kitignore",
			argName: "path",
		},
		async (ctx) => {
			const raw = ctx.args.trim();
			if (!raw) {
				toast(ctx, "Usage: /files:ignore <path>", "warning");
				return;
			}

			const cleaned = raw.replace(/^@/, "").trim().replace(/\/$/, "");
			const absolute = path.resolve(ctx.system.cwd, cleaned);

			if (!isWithinDir(absolute, ctx.system.cwd)) {
				toast(
					ctx,
					"Path must be inside the current session directory",
					"warning",
				);
				return;
			}

			let info: Awaited<ReturnType<typeof stat>>;
			try {
				info = await stat(absolute);
			} catch {
				toast(ctx, `Path not found: ${cleaned}`, "warning");
				return;
			}

			if (!info.isFile() && !info.isDirectory()) {
				toast(ctx, "Only files and directories can be ignored", "warning");
				return;
			}

			const result = await appendIgnoreEntry(
				ctx.system.cwd,
				absolute,
				info.isDirectory(),
			);
			if (result.duplicate) {
				toast(ctx, "Already ignored", "info", [result.entry]);
				return;
			}

			const location =
				path.relative(ctx.system.cwd, result.ignoreFile) || KIT_IGNORE_FILE;
			toast(
				ctx,
				`${result.created ? "Created" : "Updated"} ${location}`,
				"info",
				[result.entry, "Run /reload if file suggestions were already scanned."],
			);
		},
	);

	kit.registerCommand(
		"files:unignore",
		{
			description: "Remove a file or directory from .kitignore",
			argName: "path",
		},
		async (ctx) => {
			const raw = ctx.args.trim();
			if (!raw) {
				toast(ctx, "Usage: /files:unignore <path>", "warning");
				return;
			}

			const cleaned = raw.replace(/^@/, "").trim().replace(/\/$/, "");
			const absolute = path.resolve(ctx.system.cwd, cleaned);

			if (!isWithinDir(absolute, ctx.system.cwd)) {
				toast(
					ctx,
					"Path must be inside the current session directory",
					"warning",
				);
				return;
			}

			const result = await removeIgnoreEntryByPath(ctx.system.cwd, absolute);
			if (!result.removed || !result.ignoreFile || !result.entry) {
				toast(ctx, `No ignore entry found for: ${cleaned}`, "warning");
				return;
			}

			const location =
				path.relative(ctx.system.cwd, result.ignoreFile) || KIT_IGNORE_FILE;
			toast(ctx, `Removed ${result.entry} from ${location}`, "info", [
				"Run /reload if file suggestions were already scanned.",
			]);
		},
	);
}
