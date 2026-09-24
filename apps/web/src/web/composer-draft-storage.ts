import { mergeQueuedFollowUpsIntoDraft } from "./composer-draft";

const COMPOSER_DRAFT_KEY_PREFIX = "kit.composer.draft.";
const COMPOSER_DRAFT_OWNER_KEY_PREFIX = "kit.composer.draft-owner.";

type DraftStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;

type StoredComposerDraft = {
	text: string;
	restoreOperationId?: string;
};

function draftKey(scopeId: string, sessionId: string): string {
	return `${COMPOSER_DRAFT_KEY_PREFIX}${encodeURIComponent(scopeId)}.${encodeURIComponent(sessionId)}`;
}

function ownerKey(sessionId: string): string {
	return `${COMPOSER_DRAFT_OWNER_KEY_PREFIX}${encodeURIComponent(sessionId)}`;
}

function readStoredDraft(
	scopeId: string,
	sessionId: string,
	storage: DraftStorage,
): StoredComposerDraft {
	try {
		const value = storage.getItem(draftKey(scopeId, sessionId));
		if (!value) return { text: "" };
		const parsed: unknown = JSON.parse(value);
		if (
			typeof parsed !== "object" ||
			parsed === null ||
			!("text" in parsed) ||
			typeof parsed.text !== "string"
		) {
			return { text: "" };
		}
		return {
			text: parsed.text,
			...("restoreOperationId" in parsed &&
			typeof parsed.restoreOperationId === "string"
				? { restoreOperationId: parsed.restoreOperationId }
				: {}),
		};
	} catch {
		return { text: "" };
	}
}

function readCanonicalDraft(
	scopeId: string,
	sessionId: string,
	storage: DraftStorage,
): StoredComposerDraft {
	try {
		const owner = storage.getItem(ownerKey(sessionId));
		return readStoredDraft(owner || scopeId, sessionId, storage);
	} catch {
		return readStoredDraft(scopeId, sessionId, storage);
	}
}

export function readComposerDraft(
	scopeId: string,
	sessionId: string,
	storage: DraftStorage = localStorage,
): string {
	const draft = readCanonicalDraft(scopeId, sessionId, storage);
	if (!draft.text) return "";
	try {
		storage.setItem(draftKey(scopeId, sessionId), JSON.stringify(draft));
		storage.setItem(ownerKey(sessionId), scopeId);
	} catch {
		// The canonical draft is still readable when migration is unavailable.
	}
	return draft.text;
}

export function writeComposerDraft(
	scopeId: string,
	sessionId: string,
	text: string,
	storage: DraftStorage = localStorage,
): boolean {
	try {
		if (!text) {
			storage.removeItem(draftKey(scopeId, sessionId));
			if (storage.getItem(ownerKey(sessionId)) === scopeId) {
				storage.removeItem(ownerKey(sessionId));
			}
			return true;
		}
		const current = readCanonicalDraft(scopeId, sessionId, storage);
		storage.setItem(
			draftKey(scopeId, sessionId),
			JSON.stringify({
				text,
				...(current.restoreOperationId
					? { restoreOperationId: current.restoreOperationId }
					: {}),
			}),
		);
		storage.setItem(ownerKey(sessionId), scopeId);
		return true;
	} catch {
		return false;
	}
}

export function applyRestoredComposerDraft(
	scopeId: string,
	sessionId: string,
	operationId: string,
	messages: readonly string[],
	currentDraft: string,
	storage: DraftStorage = localStorage,
): string | null {
	try {
		const stored = readCanonicalDraft(scopeId, sessionId, storage);
		if (stored.restoreOperationId === operationId) return stored.text;
		const restored = mergeQueuedFollowUpsIntoDraft(messages, currentDraft);
		storage.setItem(
			draftKey(scopeId, sessionId),
			JSON.stringify({ text: restored, restoreOperationId: operationId }),
		);
		storage.setItem(ownerKey(sessionId), scopeId);
		return restored;
	} catch {
		return null;
	}
}
