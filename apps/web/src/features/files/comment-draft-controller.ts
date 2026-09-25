import { countDraftNotes, type ReviewDraftState } from "../review/draft";

export type FileCommentDraft = ReviewDraftState & {
	revision: string;
};

export type FileCommentDraftToken = {
	sessionId: string;
	generation: number;
};

export type FileCommentDraftEvent =
	| {
			type: "cleared" | "consumed";
			token: FileCommentDraftToken;
			repoRoot: string;
			path: string;
			state: FileCommentDraft;
	  }
	| { type: "reset"; token: FileCommentDraftToken };

function emptyDraft(): FileCommentDraft {
	return { fileNotes: new Map(), rangeNotes: new Map(), revision: "" };
}

function cloneDraft(state: FileCommentDraft): FileCommentDraft {
	return {
		fileNotes: new Map(state.fileNotes),
		rangeNotes: new Map(state.rangeNotes),
		revision: state.revision,
	};
}

function draftKey(repoRoot: string, path: string): string {
	return `${repoRoot}\0${path}`;
}

export function createFileCommentDraftController(initialSessionId: string) {
	let sessionId = initialSessionId;
	let generation = 0;
	const drafts = new Map<string, FileCommentDraft>();
	const listeners = new Set<(event: FileCommentDraftEvent) => void>();

	function currentToken(): FileCommentDraftToken {
		return { sessionId, generation };
	}

	function accepts(token: FileCommentDraftToken): boolean {
		return token.sessionId === sessionId && token.generation === generation;
	}

	function getDraft(
		token: FileCommentDraftToken,
		repoRoot: string,
		path: string,
	): FileCommentDraft {
		if (!accepts(token)) return emptyDraft();
		const state = drafts.get(draftKey(repoRoot, path));
		return state ? cloneDraft(state) : emptyDraft();
	}

	function saveDraft(
		token: FileCommentDraftToken,
		repoRoot: string,
		path: string,
		state: FileCommentDraft,
	): void {
		if (!accepts(token)) return;
		const key = draftKey(repoRoot, path);
		if (countDraftNotes(state) === 0) drafts.delete(key);
		else drafts.set(key, cloneDraft(state));
	}

	function consumeDraft(
		token: FileCommentDraftToken,
		repoRoot: string,
		path: string,
		submitted: FileCommentDraft,
	): void {
		if (!accepts(token)) return;
		const current = getDraft(token, repoRoot, path);
		for (const [key, value] of submitted.fileNotes) {
			if (current.fileNotes.get(key) === value) current.fileNotes.delete(key);
		}
		for (const [key, value] of submitted.rangeNotes) {
			if (current.rangeNotes.get(key) === value) current.rangeNotes.delete(key);
		}
		if (countDraftNotes(current) === 0) {
			current.revision = "";
			drafts.delete(draftKey(repoRoot, path));
		} else {
			drafts.set(draftKey(repoRoot, path), cloneDraft(current));
		}
		publish({
			type: "consumed",
			token: currentToken(),
			repoRoot,
			path,
			state: current,
		});
	}

	function clearDraft(
		token: FileCommentDraftToken,
		repoRoot: string,
		path: string,
	): void {
		if (!accepts(token)) return;
		drafts.delete(draftKey(repoRoot, path));
		publish({
			type: "cleared",
			token: currentToken(),
			repoRoot,
			path,
			state: emptyDraft(),
		});
	}

	function resetForSession(nextSessionId: string): void {
		sessionId = nextSessionId;
		generation += 1;
		drafts.clear();
		publish({ type: "reset", token: currentToken() });
	}

	function publish(event: FileCommentDraftEvent): void {
		for (const listener of listeners) listener(event);
	}

	function subscribe(listener: (event: FileCommentDraftEvent) => void) {
		listeners.add(listener);
		return () => listeners.delete(listener);
	}

	return {
		currentToken,
		getDraft,
		saveDraft,
		consumeDraft,
		clearDraft,
		resetForSession,
		subscribe,
	};
}

export type FileCommentDraftController = ReturnType<
	typeof createFileCommentDraftController
>;
