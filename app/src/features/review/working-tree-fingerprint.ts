import { execFile } from "node:child_process";
import { lstat } from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

function statusPaths(output: string): string[] {
	const paths = new Set<string>();
	for (const entry of output.split("\0")) {
		if (!entry) continue;
		// Porcelain v1 prefixes primary records with XY + space. Rename records
		// include a second NUL-delimited path without that prefix; retaining both
		// is harmless and ensures either side changing invalidates the fingerprint.
		const filePath = /^[ MADRCU?!]{2} /.test(entry) ? entry.slice(3) : entry;
		if (filePath) paths.add(filePath);
	}
	return [...paths].sort();
}

export async function getWorkingTreeFingerprint(
	repoRoot: string,
): Promise<string> {
	const { stdout } = await execFileAsync(
		"git",
		["status", "--porcelain=v1", "-z", "--untracked-files=all"],
		{
			cwd: repoRoot,
			encoding: "utf8",
			maxBuffer: 16 * 1024 * 1024,
		},
	);
	const paths = statusPaths(stdout);
	const metadata = await Promise.all(
		paths.map(async (filePath) => {
			try {
				const value = await lstat(path.join(repoRoot, filePath));
				return `${filePath}:${value.mtimeMs}:${value.ctimeMs}:${value.size}`;
			} catch {
				return `${filePath}:missing`;
			}
		}),
	);
	return `${stdout}\n${metadata.join("\n")}`;
}
