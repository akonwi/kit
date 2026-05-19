import type { InternalPluginAPI } from "../../plugins/types";
import type { VcsInfo } from "../../runtime/vcs-info";
import { type GitHubPullRequest, getCurrentBranchPullRequest } from "./pr";

const PR_CACHE_TTL_MS = 60_000;

type CachedPrLookup = {
	branch: string | null;
	pullRequest: GitHubPullRequest | null;
	updatedAt: number;
};

export function GitHubPlugin(kit: InternalPluginAPI) {
	let disposed = false;
	let refreshGeneration = 0;
	let cache: CachedPrLookup | null = null;

	function clearBadge() {
		kit.status.setVcsBadge(null);
	}

	function updateBadge(pullRequest: GitHubPullRequest | null) {
		kit.status.setVcsBadge(pullRequest ? `PR #${pullRequest.number}` : null);
	}

	async function refresh(gitInfo: VcsInfo) {
		const branch = gitInfo.branch;
		const generation = ++refreshGeneration;
		const now = Date.now();
		const cached = cache;
		const cacheFresh =
			cached &&
			cached.branch === branch &&
			now - cached.updatedAt <= PR_CACHE_TTL_MS;

		if (cacheFresh) {
			updateBadge(cached.pullRequest);
			return;
		}

		if (branch !== cached?.branch) clearBadge();

		const pullRequest = await getCurrentBranchPullRequest(
			kit.system.cwd,
			branch,
		);
		if (disposed || generation !== refreshGeneration) return;

		cache = { branch, pullRequest, updatedAt: Date.now() };
		updateBadge(pullRequest);
	}

	kit.on("vcs.updated", (event) => {
		void refresh(event);
	});

	const refreshTimer = setInterval(() => {
		void refresh(kit.vcs.get());
	}, PR_CACHE_TTL_MS);

	void refresh(kit.vcs.get());

	return () => {
		disposed = true;
		clearInterval(refreshTimer);
		clearBadge();
	};
}
