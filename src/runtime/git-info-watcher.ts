import {
	existsSync,
	type FSWatcher,
	readFileSync,
	statSync,
	watch,
} from "node:fs";
import { dirname, join, resolve } from "node:path";
import { type GitInfo, getGitInfo } from "./git-info";

type GitPaths = {
	repoDir: string;
	commonGitDir: string;
	headPath: string;
};

const WATCH_DEBOUNCE_MS = 300;
const POLL_INTERVAL_MS = 5_000;

/**
 * Find git metadata paths by walking up from cwd.
 * Handles both regular git repos (.git is a directory) and worktrees (.git is a file).
 */
function findGitPaths(cwd: string): GitPaths | null {
	let dir = cwd;
	while (true) {
		const gitPath = join(dir, ".git");
		if (existsSync(gitPath)) {
			try {
				const stat = statSync(gitPath);
				if (stat.isFile()) {
					const content = readFileSync(gitPath, "utf8").trim();
					if (content.startsWith("gitdir: ")) {
						const gitDir = resolve(dir, content.slice(8).trim());
						const headPath = join(gitDir, "HEAD");
						if (!existsSync(headPath)) return null;
						const commonDirPath = join(gitDir, "commondir");
						const commonGitDir = existsSync(commonDirPath)
							? resolve(gitDir, readFileSync(commonDirPath, "utf8").trim())
							: gitDir;
						return { repoDir: dir, commonGitDir, headPath };
					}
				} else if (stat.isDirectory()) {
					const headPath = join(gitPath, "HEAD");
					if (!existsSync(headPath)) return null;
					return { repoDir: dir, commonGitDir: gitPath, headPath };
				}
			} catch {
				return null;
			}
		}

		const parent = dirname(dir);
		if (parent === dir) return null;
		dir = parent;
	}
}

export class GitInfoWatcher {
	private readonly cwd: string;
	private readonly onChange: (info: GitInfo) => void;
	private readonly gitPaths: GitPaths | null;
	private readonly watchers: FSWatcher[] = [];
	private refreshTimer: ReturnType<typeof setTimeout> | null = null;
	private pollTimer: ReturnType<typeof setInterval> | null = null;
	private refreshInFlight = false;
	private refreshPending = false;
	private disposed = false;
	private lastInfo: GitInfo;

	constructor(cwd: string, onChange: (info: GitInfo) => void) {
		this.cwd = cwd;
		this.onChange = onChange;
		this.gitPaths = findGitPaths(cwd);
		this.lastInfo = getGitInfo(cwd);
		this.setupWatchers();
	}

	getCurrent(): GitInfo {
		return this.lastInfo;
	}

	dispose(): void {
		this.disposed = true;
		if (this.refreshTimer) {
			clearTimeout(this.refreshTimer);
			this.refreshTimer = null;
		}
		if (this.pollTimer) {
			clearInterval(this.pollTimer);
			this.pollTimer = null;
		}
		for (const watcher of this.watchers) watcher.close();
		this.watchers.length = 0;
	}

	private setupWatchers(): void {
		if (!this.gitPaths) return;

		this.watchPath(
			dirname(this.gitPaths.headPath),
			(filename) => !filename || filename === "HEAD",
		);
		this.watchPath(this.gitPaths.commonGitDir, (filename) => {
			if (!filename) return true;
			return (
				filename === "HEAD" ||
				filename === "index" ||
				filename === "packed-refs" ||
				filename === "refs" ||
				filename === "reftable"
			);
		});

		const reftableDir = join(this.gitPaths.commonGitDir, "reftable");
		if (existsSync(reftableDir)) {
			this.watchPath(reftableDir);
		}

		this.watchPath(
			this.gitPaths.repoDir,
			(filename) => !filename || !filename.startsWith(".git"),
			{ recursive: true },
		);

		this.pollTimer = setInterval(() => {
			this.scheduleRefresh();
		}, POLL_INTERVAL_MS);
	}

	private watchPath(
		path: string,
		shouldRefresh: (filename: string | null) => boolean = () => true,
		options?: { recursive?: boolean },
	): void {
		try {
			const watcher = watch(path, options ?? {}, (_eventType, filename) => {
				const nextFilename = typeof filename === "string" ? filename : null;
				if (!shouldRefresh(nextFilename)) return;
				this.scheduleRefresh();
			});
			this.watchers.push(watcher);
		} catch {
			// Best-effort only.
		}
	}

	private scheduleRefresh(): void {
		if (this.disposed) return;
		if (this.refreshTimer) clearTimeout(this.refreshTimer);
		this.refreshTimer = setTimeout(() => {
			this.refreshTimer = null;
			void this.refresh();
		}, WATCH_DEBOUNCE_MS);
	}

	private async refresh(): Promise<void> {
		if (this.disposed) return;
		if (this.refreshInFlight) {
			this.refreshPending = true;
			return;
		}

		this.refreshInFlight = true;
		try {
			const nextInfo = getGitInfo(this.cwd);
			if (this.disposed) return;
			if (
				nextInfo.branch !== this.lastInfo.branch ||
				nextInfo.dirty !== this.lastInfo.dirty
			) {
				this.lastInfo = nextInfo;
				this.onChange(nextInfo);
				return;
			}
			this.lastInfo = nextInfo;
		} finally {
			this.refreshInFlight = false;
			if (this.refreshPending && !this.disposed) {
				this.refreshPending = false;
				this.scheduleRefresh();
			}
		}
	}
}
