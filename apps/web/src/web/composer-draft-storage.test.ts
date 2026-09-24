import { describe, expect, test } from "bun:test";
import {
	applyRestoredComposerDraft,
	readComposerDraft,
	writeComposerDraft,
} from "./composer-draft-storage";

function memoryStorage() {
	const values = new Map<string, string>();
	return {
		getItem: (key: string) => values.get(key) ?? null,
		setItem: (key: string, value: string) => values.set(key, value),
		removeItem: (key: string) => values.delete(key),
	};
}

describe("browser composer draft storage", () => {
	test("shares the canonical session draft across browser scopes", () => {
		const storage = memoryStorage();
		expect(
			writeComposerDraft("tab-a", "session/a", "first", storage),
		).toBeTrue();
		expect(readComposerDraft("tab-b", "session/a", storage)).toBe("first");
		expect(
			writeComposerDraft("tab-b", "session/a", "other tab", storage),
		).toBeTrue();
		expect(readComposerDraft("tab-a", "session/a", storage)).toBe("other tab");
		writeComposerDraft("tab-a", "session/b", "second", storage);
		expect(readComposerDraft("tab-b", "session/b", storage)).toBe("second");
	});

	test("applies one restore operation idempotently", () => {
		const storage = memoryStorage();
		writeComposerDraft("tab-a", "session-1", "existing", storage);
		expect(
			applyRestoredComposerDraft(
				"tab-a",
				"session-1",
				"operation-1",
				["first", "second"],
				"existing",
				storage,
			),
		).toBe("first\n\nsecond\n\nexisting");
		expect(
			applyRestoredComposerDraft(
				"tab-a",
				"session-1",
				"operation-1",
				["first", "second"],
				"first\n\nsecond\n\nexisting",
				storage,
			),
		).toBe("first\n\nsecond\n\nexisting");
	});

	test("removes empty drafts and tolerates unavailable storage", () => {
		const storage = memoryStorage();
		writeComposerDraft("tab-a", "session-1", "draft", storage);
		expect(writeComposerDraft("tab-a", "session-1", "", storage)).toBeTrue();
		expect(readComposerDraft("tab-a", "session-1", storage)).toBe("");
		const unavailable = {
			getItem: () => {
				throw new Error("blocked");
			},
			setItem: () => {
				throw new Error("blocked");
			},
			removeItem: () => {
				throw new Error("blocked");
			},
		};
		expect(readComposerDraft("tab-a", "session-1", unavailable)).toBe("");
		expect(
			writeComposerDraft("tab-a", "session-1", "draft", unavailable),
		).toBeFalse();
	});
});
