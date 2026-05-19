import type { InternalPluginAPI } from "../../plugins/types";
import type { VcsInfo } from "../../runtime/vcs-info";
import { MIDDLE_DOT } from "../../shell/glyphs";
import {
	type GitHubPullRequest,
	getCurrentBranchPullRequest,
} from "../github/pr";

const PR_CACHE_TTL_MS = 60_000;

type CachedPrLookup = {
	branch: string | null;
	pullRequest: GitHubPullRequest | null;
	updatedAt: number;
};

export function VcsStatusPlugin(kit: InternalPluginAPI) {
	let disposed = false;
	let refreshGeneration = 0;
	let cache: CachedPrLookup | null = null;
	let currentVcsInfo = kit.vcs.get();
	let currentPullRequest: GitHubPullRequest | null = null;

	function formatLocation(
		vcsInfo: VcsInfo,
		pullRequest: GitHubPullRequest | null,
	): string {
		const cwd = kit.system.cwd;
		if (vcsInfo.branch == null) return cwd;
		return `${cwd} (${[
			`${vcsInfo.branch}${vcsInfo.dirty ? "*" : ""}`,
			...(pullRequest ? [`PR #${pullRequest.number}`] : []),
		].join(` ${MIDDLE_DOT} `)})`;
	}

	function updateFooter() {
		kit.footer.set(
			"location",
			formatLocation(currentVcsInfo, currentPullRequest),
			{
				side: "right",
			},
		);
	}

	async function refreshPullRequest(vcsInfo: VcsInfo) {
		const branch = vcsInfo.branch;
		const generation = ++refreshGeneration;
		const now = Date.now();
		const cached = cache;
		const cacheFresh =
			cached &&
			cached.branch === branch &&
			now - cached.updatedAt <= PR_CACHE_TTL_MS;

		if (cacheFresh) {
			currentPullRequest = cached.pullRequest;
			updateFooter();
			return;
		}

		if (branch !== cached?.branch) {
			currentPullRequest = null;
			updateFooter();
		}

		const pullRequest = await getCurrentBranchPullRequest(
			kit.system.cwd,
			branch,
		);
		if (disposed || generation !== refreshGeneration) return;

		cache = { branch, pullRequest, updatedAt: Date.now() };
		currentPullRequest = pullRequest;
		updateFooter();
	}

	function update(vcsInfo: VcsInfo) {
		currentVcsInfo = vcsInfo;
		updateFooter();
		void refreshPullRequest(vcsInfo);
	}

	kit.on("vcs.updated", (event) => update(event));

	const refreshTimer = setInterval(() => {
		void refreshPullRequest(currentVcsInfo);
	}, PR_CACHE_TTL_MS);

	update(currentVcsInfo);

	return () => {
		disposed = true;
		clearInterval(refreshTimer);
		kit.footer.clear("location");
	};
}
