export type KitRelease = {
	version: string;
	tag: string;
	url: string;
	notes: string;
	publishedAt?: string;
};

export type ReleaseUpdate = KitRelease;

export type ReleasePage = {
	releases: KitRelease[];
	hasMore: boolean;
};

type GithubRelease = {
	tag_name?: unknown;
	body?: unknown;
	html_url?: unknown;
	published_at?: unknown;
	draft?: unknown;
	prerelease?: unknown;
};

type Fetcher = (
	input: string | URL | Request,
	init?: RequestInit,
) => Promise<Response>;

const RELEASES_API_URL = "https://api.github.com/repos/akonwi/kit/releases";
export const RELEASE_PAGE_SIZE = 3;
const NEXT_LINK = /<[^>]+>;\s*rel="next"/;
const STABLE_VERSION = /^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const INSTALLED_VERSION =
	/^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/;

function withoutV(version: string): string {
	return version.startsWith("v") ? version.slice(1) : version;
}

function releaseRequestInit(
	currentVersion: string,
	signal?: AbortSignal,
): RequestInit {
	return {
		signal,
		headers: {
			Accept: "application/vnd.github+json",
			"User-Agent": `akonwi-kit/${currentVersion}`,
			"X-GitHub-Api-Version": "2022-11-28",
		},
	};
}

function parseRelease(value: unknown): KitRelease | null {
	if (typeof value !== "object" || value === null) return null;
	const release = value as GithubRelease;
	if (
		typeof release.tag_name !== "string" ||
		!STABLE_VERSION.test(release.tag_name) ||
		release.draft === true ||
		release.prerelease === true
	) {
		return null;
	}
	const sourceTag = release.tag_name;
	const version = withoutV(sourceTag);
	const tag = `v${version}`;
	return {
		version,
		tag,
		url:
			typeof release.html_url === "string" && release.html_url.length > 0
				? release.html_url
				: `https://github.com/akonwi/kit/releases/tag/${encodeURIComponent(sourceTag)}`,
		notes: typeof release.body === "string" ? release.body : "",
		...(typeof release.published_at === "string"
			? { publishedAt: release.published_at }
			: {}),
	};
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
		`${RELEASES_API_URL}/latest`,
		releaseRequestInit(currentVersion, signal),
	);
	if (!response.ok) {
		throw new Error(`GitHub release check failed (${response.status})`);
	}
	const release = parseRelease(await response.json());
	if (!release) {
		throw new Error("GitHub release response did not include a stable tag");
	}
	return isNewerVersion(release.tag, currentVersion) ? release : null;
}

export async function fetchReleasePage(
	currentVersion: string,
	page: number,
	fetchImpl: Fetcher = fetch,
	signal?: AbortSignal,
): Promise<ReleasePage> {
	if (!Number.isInteger(page) || page < 1) {
		throw new Error("Release page must be a positive integer");
	}
	const response = await fetchImpl(
		`${RELEASES_API_URL}?per_page=${RELEASE_PAGE_SIZE}&page=${page}`,
		releaseRequestInit(currentVersion, signal),
	);
	if (!response.ok) {
		throw new Error(`GitHub releases request failed (${response.status})`);
	}
	const payload = await response.json();
	if (!Array.isArray(payload)) {
		throw new Error("GitHub releases response was not a list");
	}
	return {
		releases: payload
			.map(parseRelease)
			.filter((release): release is KitRelease => release !== null),
		hasMore: NEXT_LINK.test(response.headers.get("link") ?? ""),
	};
}
