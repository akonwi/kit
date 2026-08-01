import { CURRENT_RELEASE_NOTES, CURRENT_VERSION } from "./current-release";
import { checkLatestRelease, type ReleaseUpdate } from "./release-check";

export type ReleasesState = {
	currentVersion: string;
	currentNotes: string;
	latest: ReleaseUpdate | null;
	checking: boolean;
};

export type ReleasesWorkspaceController = {
	getState(): ReleasesState;
	subscribe(listener: (state: ReleasesState) => void): () => void;
	open(): void;
	onOpenRequest(listener: () => void): () => void;
	checkForUpdate(): Promise<void>;
	dispose(): void;
};

export function createReleasesWorkspaceController(options?: {
	currentVersion?: string;
	currentNotes?: string;
	checkLatest?: (
		currentVersion: string,
		signal: AbortSignal,
	) => Promise<ReleaseUpdate | null>;
	checkTimeoutMs?: number;
}): ReleasesWorkspaceController {
	let state: ReleasesState = {
		currentVersion: options?.currentVersion ?? CURRENT_VERSION,
		currentNotes: options?.currentNotes ?? CURRENT_RELEASE_NOTES,
		latest: null,
		checking: false,
	};
	const stateListeners = new Set<(state: ReleasesState) => void>();
	const openListeners = new Set<() => void>();
	let checked = false;
	let disposed = false;
	let pendingCheck: Promise<void> | null = null;
	let pendingAbort: AbortController | null = null;

	function publish(next: ReleasesState): void {
		if (disposed) return;
		state = next;
		for (const listener of [...stateListeners]) listener(state);
	}

	async function runCheck(): Promise<void> {
		publish({ ...state, checking: true });
		const abort = new AbortController();
		pendingAbort = abort;
		const timeout = setTimeout(
			() => abort.abort(),
			options?.checkTimeoutMs ?? 5_000,
		);
		try {
			const latest = await (options?.checkLatest
				? options.checkLatest(state.currentVersion, abort.signal)
				: checkLatestRelease(state.currentVersion, fetch, abort.signal));
			publish({ ...state, latest, checking: false });
		} catch {
			// Update checks are advisory. Offline, rate-limited, and malformed
			// responses must never interrupt startup or produce noisy UI.
			publish({ ...state, checking: false });
		} finally {
			clearTimeout(timeout);
			if (pendingAbort === abort) pendingAbort = null;
		}
	}

	return {
		getState: () => state,
		subscribe(listener) {
			stateListeners.add(listener);
			return () => stateListeners.delete(listener);
		},
		open() {
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
		dispose() {
			if (disposed) return;
			disposed = true;
			pendingAbort?.abort();
			pendingAbort = null;
			stateListeners.clear();
			openListeners.clear();
		},
	};
}
