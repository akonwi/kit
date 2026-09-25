import type { ReviewDraftState } from "./draft";
import { countDraftNotes } from "./draft";
import type { ReviewTarget } from "./model";

export type ReviewDraftToken = {
	sessionId: string;
	generation: number;
};

export type ReviewDraftEvent =
	| {
			type: "cleared";
			token: ReviewDraftToken;
			repoRoot: string;
			targetKey: string;
	  }
	| {
			type: "consumed";
			token: ReviewDraftToken;
			repoRoot: string;
			targetKey: string;
			state: ReviewDraftState;
	  }
	| { type: "reset"; token: ReviewDraftToken };

type RepoDraftWorkspace = {
	lastTarget: ReviewTarget;
	drafts: Map<string, ReviewDraftState>;
};

function emptyDraftState(): ReviewDraftState {
	return { fileNotes: new Map(), rangeNotes: new Map() };
}

function cloneDraftState(state: ReviewDraftState): ReviewDraftState {
	return {
		fileNotes: new Map(state.fileNotes),
		rangeNotes: new Map(state.rangeNotes),
	};
}

export function reviewTargetKey(target: ReviewTarget): string {
	switch (target.kind) {
		case "working":
			return "working";
		case "commit":
			return `commit:${target.sha}`;
		case "branch":
			return `branch:${target.base}:${target.mergeBase}:${target.head}`;
	}
}

export function createReviewDraftController(initialSessionId: string) {
	let sessionId = initialSessionId;
	let generation = 0;
	const repos = new Map<string, RepoDraftWorkspace>();
	const listeners = new Set<(event: ReviewDraftEvent) => void>();

	function publish(event: ReviewDraftEvent): void {
		for (const listener of listeners) listener(event);
	}

	function currentToken(): ReviewDraftToken {
		return { sessionId, generation };
	}

	function accepts(token: ReviewDraftToken): boolean {
		return token.sessionId === sessionId && token.generation === generation;
	}

	function resetForSession(nextSessionId: string): void {
		sessionId = nextSessionId;
		generation += 1;
		repos.clear();
		publish({ type: "reset", token: currentToken() });
	}

	function workspace(repoRoot: string): RepoDraftWorkspace {
		let value = repos.get(repoRoot);
		if (!value) {
			value = {
				lastTarget: { kind: "working" },
				drafts: new Map(),
			};
			repos.set(repoRoot, value);
		}
		return value;
	}

	function getDraft(
		token: ReviewDraftToken,
		repoRoot: string,
		target: ReviewTarget,
	): ReviewDraftState {
		if (!accepts(token)) return emptyDraftState();
		const state = repos.get(repoRoot)?.drafts.get(reviewTargetKey(target));
		return state ? cloneDraftState(state) : emptyDraftState();
	}

	function saveDraft(
		token: ReviewDraftToken,
		repoRoot: string,
		target: ReviewTarget,
		state: ReviewDraftState,
	): void {
		if (!accepts(token)) return;
		const drafts = workspace(repoRoot).drafts;
		const key = reviewTargetKey(target);
		if (countDraftNotes(state) === 0) drafts.delete(key);
		else drafts.set(key, cloneDraftState(state));
	}

	function clearDraft(
		token: ReviewDraftToken,
		repoRoot: string,
		target: ReviewTarget,
	): void {
		if (!accepts(token)) return;
		const key = reviewTargetKey(target);
		repos.get(repoRoot)?.drafts.delete(key);
		publish({
			type: "cleared",
			token: currentToken(),
			repoRoot,
			targetKey: key,
		});
	}

	function consumeDraft(
		token: ReviewDraftToken,
		repoRoot: string,
		target: ReviewTarget,
		submitted: ReviewDraftState,
	): void {
		if (!accepts(token)) return;
		const key = reviewTargetKey(target);
		const current = repos.get(repoRoot)?.drafts.get(key) ?? emptyDraftState();
		const remaining = cloneDraftState(current);
		for (const [noteKey, value] of submitted.fileNotes) {
			if (remaining.fileNotes.get(noteKey) === value) {
				remaining.fileNotes.delete(noteKey);
			}
		}
		for (const [noteKey, value] of submitted.rangeNotes) {
			if (remaining.rangeNotes.get(noteKey) === value) {
				remaining.rangeNotes.delete(noteKey);
			}
		}
		const drafts = workspace(repoRoot).drafts;
		if (countDraftNotes(remaining) === 0) drafts.delete(key);
		else drafts.set(key, cloneDraftState(remaining));
		publish({
			type: "consumed",
			token: currentToken(),
			repoRoot,
			targetKey: key,
			state: cloneDraftState(remaining),
		});
	}

	function getLastTarget(
		token: ReviewDraftToken,
		repoRoot: string,
	): ReviewTarget {
		if (!accepts(token)) return { kind: "working" };
		return repos.get(repoRoot)?.lastTarget ?? { kind: "working" };
	}

	function setLastTarget(
		token: ReviewDraftToken,
		repoRoot: string,
		target: ReviewTarget,
	): void {
		if (!accepts(token)) return;
		workspace(repoRoot).lastTarget = target;
	}

	function countDraftForKey(
		token: ReviewDraftToken,
		repoRoot: string,
		key: string,
	): number {
		if (!accepts(token)) return 0;
		const state = repos.get(repoRoot)?.drafts.get(key);
		return state ? countDraftNotes(state) : 0;
	}

	function subscribe(listener: (event: ReviewDraftEvent) => void): () => void {
		listeners.add(listener);
		return () => listeners.delete(listener);
	}

	function countDraftsExcept(
		token: ReviewDraftToken,
		repoRoot: string,
		target: ReviewTarget,
	): number {
		if (!accepts(token)) return 0;
		const excludedKey = reviewTargetKey(target);
		let count = 0;
		for (const [key, state] of repos.get(repoRoot)?.drafts ?? []) {
			if (key !== excludedKey) count += countDraftNotes(state);
		}
		return count;
	}

	return {
		currentToken,
		resetForSession,
		getDraft,
		saveDraft,
		clearDraft,
		consumeDraft,
		getLastTarget,
		setLastTarget,
		countDraftForKey,
		countDraftsExcept,
		subscribe,
	};
}

export type ReviewDraftController = ReturnType<
	typeof createReviewDraftController
>;
