import { CURRENT_RELEASE_NOTES, CURRENT_VERSION } from "./current-release";
import {
	checkLatestRelease,
	fetchReleasePage,
	isNewerVersion,
	type KitRelease,
	type ReleasePage,
	type ReleaseUpdate,
} from "./release-check";

export type ReleaseHistoryStatus =
	| "idle"
	| "loading"
	| "loaded"
	| "unavailable";

export type ReleasesState = {
	currentVersion: string;
	currentNotes: string;
	latest: ReleaseUpdate | null;
	checking: boolean;
	releases: KitRelease[];
	historyStatus: ReleaseHistoryStatus;
	hasMore: boolean;
	loadingMore: boolean;
};

export type ReleasesWorkspaceController = {
	getState(): ReleasesState;
	subscribe(listener: (state: ReleasesState) => void): () => void;
	open(): void;
	onOpenRequest(listener: () => void): () => void;
	checkForUpdate(): Promise<void>;
	loadReleases(): Promise<void>;
	loadMoreReleases(): Promise<void>;
	dispose(): void;
};

function installedRelease(version: string, notes: string): KitRelease {
	const tag = `v${version}`;
	return {
		version,
		tag,
		notes,
		url: `https://github.com/akonwi/kit/releases/tag/${encodeURIComponent(tag)}`,
	};
}

function mergeReleases(
	currentVersion: string,
	currentNotes: string,
	...groups: KitRelease[][]
): KitRelease[] {
	const byTag = new Map<string, KitRelease>();
	for (const release of groups.flat()) byTag.set(release.tag, release);
	const installed = installedRelease(currentVersion, currentNotes);
	const remoteInstalled = byTag.get(installed.tag);
	byTag.set(
		installed.tag,
		remoteInstalled ? { ...remoteInstalled, notes: currentNotes } : installed,
	);
	return [...byTag.values()].sort((left, right) => {
		if (left.publishedAt && right.publishedAt) {
			return right.publishedAt.localeCompare(left.publishedAt);
		}
		if (left.publishedAt) return -1;
		if (right.publishedAt) return 1;
		return 0;
	});
}

function newestUpdate(
	currentVersion: string,
	releases: KitRelease[],
): ReleaseUpdate | null {
	return (
		releases.find((release) =>
			isNewerVersion(release.version, currentVersion),
		) ?? null
	);
}

export function createReleasesWorkspaceController(options?: {
	currentVersion?: string;
	currentNotes?: string;
	checkLatest?: (
		currentVersion: string,
		signal: AbortSignal,
	) => Promise<ReleaseUpdate | null>;
	fetchHistoryPage?: (
		currentVersion: string,
		page: number,
		signal: AbortSignal,
	) => Promise<ReleasePage>;
	checkTimeoutMs?: number;
}): ReleasesWorkspaceController {
	const currentVersion = options?.currentVersion ?? CURRENT_VERSION;
	const currentNotes = options?.currentNotes ?? CURRENT_RELEASE_NOTES;
	let state: ReleasesState = {
		currentVersion,
		currentNotes,
		latest: null,
		checking: false,
		releases: [installedRelease(currentVersion, currentNotes)],
		historyStatus: "idle",
		hasMore: false,
		loadingMore: false,
	};
	const stateListeners = new Set<(state: ReleasesState) => void>();
	const openListeners = new Set<() => void>();
	let checked = false;
	let disposed = false;
	let nextPage = 1;
	let pendingCheck: Promise<void> | null = null;
	let pendingHistory: Promise<void> | null = null;
	const pendingAborts = new Set<AbortController>();

	function publish(next: ReleasesState): void {
		if (disposed) return;
		state = next;
		for (const listener of [...stateListeners]) listener(state);
	}

	async function withTimeout<T>(
		task: (signal: AbortSignal) => Promise<T>,
	): Promise<T> {
		const abort = new AbortController();
		pendingAborts.add(abort);
		const timeout = setTimeout(
			() => abort.abort(),
			options?.checkTimeoutMs ?? 5_000,
		);
		try {
			return await task(abort.signal);
		} finally {
			clearTimeout(timeout);
			pendingAborts.delete(abort);
		}
	}

	async function runCheck(): Promise<void> {
		publish({ ...state, checking: true });
		try {
			const latest = await withTimeout((signal) =>
				options?.checkLatest
					? options.checkLatest(state.currentVersion, signal)
					: checkLatestRelease(state.currentVersion, fetch, signal),
			);
			const releases = latest
				? mergeReleases(
						state.currentVersion,
						state.currentNotes,
						state.releases,
						[latest],
					)
				: state.releases;
			publish({
				...state,
				latest: newestUpdate(state.currentVersion, releases),
				checking: false,
				releases,
			});
		} catch {
			publish({ ...state, checking: false });
		}
	}

	async function runHistoryPage(page: number): Promise<void> {
		const initial = page === 1;
		publish({
			...state,
			historyStatus: initial ? "loading" : state.historyStatus,
			loadingMore: !initial,
		});
		try {
			const result = await withTimeout((signal) =>
				options?.fetchHistoryPage
					? options.fetchHistoryPage(state.currentVersion, page, signal)
					: fetchReleasePage(state.currentVersion, page, fetch, signal),
			);
			const merged = mergeReleases(
				state.currentVersion,
				state.currentNotes,
				state.releases,
				result.releases,
			);
			nextPage = page + 1;
			publish({
				...state,
				latest: newestUpdate(state.currentVersion, merged),
				releases: merged,
				historyStatus: "loaded",
				hasMore: result.hasMore,
				loadingMore: false,
			});
		} catch {
			publish({
				...state,
				historyStatus: initial ? "unavailable" : "loaded",
				loadingMore: false,
			});
		}
	}

	function loadPage(page: number): Promise<void> {
		if (pendingHistory) return pendingHistory;
		pendingHistory = runHistoryPage(page).finally(() => {
			pendingHistory = null;
		});
		return pendingHistory;
	}

	const controller: ReleasesWorkspaceController = {
		getState: () => state,
		subscribe(listener) {
			stateListeners.add(listener);
			return () => stateListeners.delete(listener);
		},
		open() {
			void controller.loadReleases();
			for (const listener of [...openListeners]) listener();
		},
		onOpenRequest(listener) {
			openListeners.add(listener);
			return () => openListeners.delete(listener);
		},
		checkForUpdate() {
			if (disposed) return Promise.resolve();
			if (pendingCheck) return pendingCheck;
			if (checked) return Promise.resolve();
			checked = true;
			pendingCheck = runCheck().finally(() => {
				pendingCheck = null;
			});
			return pendingCheck;
		},
		loadReleases() {
			if (disposed || state.historyStatus === "loaded") {
				return Promise.resolve();
			}
			return loadPage(1);
		},
		loadMoreReleases() {
			if (
				disposed ||
				state.historyStatus !== "loaded" ||
				!state.hasMore ||
				state.loadingMore
			) {
				return Promise.resolve();
			}
			return loadPage(nextPage);
		},
		dispose() {
			if (disposed) return;
			disposed = true;
			for (const abort of pendingAborts) abort.abort();
			pendingAborts.clear();
			stateListeners.clear();
			openListeners.clear();
		},
	};
	return controller;
}
