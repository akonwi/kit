export type ReleaseUpdate = {
	version: string;
	tag: string;
	url: string;
};

type GithubRelease = {
	tag_name?: unknown;
};

type Fetcher = (
	input: string | URL | Request,
	init?: RequestInit,
) => Promise<Response>;

const STABLE_VERSION = /^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const INSTALLED_VERSION =
	/^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/;

function withoutV(version: string): string {
	return version.startsWith("v") ? version.slice(1) : version;
}

export function isNewerVersion(candidate: string, current: string): boolean {
	const next = candidate.trim();
	const installed = current.trim();
	if (!STABLE_VERSION.test(next) || !INSTALLED_VERSION.test(installed)) {
		return false;
	}
	try {
		return Bun.semver.order(withoutV(next), withoutV(installed)) > 0;
	} catch {
		return false;
	}
}

export async function checkLatestRelease(
	currentVersion: string,
	fetchImpl: Fetcher = fetch,
	signal?: AbortSignal,
): Promise<ReleaseUpdate | null> {
	const response = await fetchImpl(
		"https://api.github.com/repos/akonwi/kit/releases/latest",
		{
			signal,
			headers: {
				Accept: "application/vnd.github+json",
				"User-Agent": `akonwi-kit/${currentVersion}`,
				"X-GitHub-Api-Version": "2022-11-28",
			},
		},
	);
	if (!response.ok) {
		throw new Error(`GitHub release check failed (${response.status})`);
	}
	const release = (await response.json()) as GithubRelease;
	if (typeof release.tag_name !== "string") {
		throw new Error("GitHub release response did not include a tag");
	}
	const tag = release.tag_name;
	if (!isNewerVersion(tag, currentVersion)) return null;
	const version = tag.startsWith("v") ? tag.slice(1) : tag;
	return {
		version,
		tag,
		url: `https://github.com/akonwi/kit/releases/tag/${encodeURIComponent(tag)}`,
	};
}
