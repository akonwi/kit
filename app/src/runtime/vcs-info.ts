export type VcsInfo = {
	branch: string | null;
	dirty: boolean;
};

/**
 * Get current git branch and dirty state for a given directory.
 * Uses the same status plumbing as `git status` so linked worktrees are
 * resolved against their own HEAD/ref state.
 */
export function getVcsInfo(cwd: string): VcsInfo {
	try {
		const result = spawnSync("git", ["status", "--porcelain=2", "--branch"], {
			cwd,
			encoding: "utf-8",
		});
		if (result.error || result.status !== 0) {
			return emptyVcsInfo();
		}

		let branch: string | null = null;
		let detachedOid: string | null = null;
		let dirty = false;

		for (const line of result.stdout.split(/\r?\n/)) {
			if (!line) continue;
			if (line.startsWith("# branch.head ")) {
				const head = line.slice("# branch.head ".length).trim();
				branch = head === "(detached)" ? "detached" : head;
				continue;
			}
			if (line.startsWith("# branch.oid ")) {
				detachedOid = line.slice("# branch.oid ".length).trim();
				continue;
			}
			if (!line.startsWith("#")) {
				dirty = true;
			}
		}

		if (branch === "detached" && detachedOid && detachedOid !== "(initial)") {
			branch = `detached@${detachedOid.slice(0, 7)}`;
		}

		return { branch, dirty };
	} catch {
		return emptyVcsInfo();
	}
}

export function emptyVcsInfo(): VcsInfo {
	return { branch: null, dirty: false };
}

function spawnSync(
	cmd: string,
	args: string[],
	options: { cwd: string; encoding: string },
) {
	const { spawnSync } = require("node:child_process");
	return spawnSync(cmd, args, options);
}
