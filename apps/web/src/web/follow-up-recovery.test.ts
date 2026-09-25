import { describe, expect, test } from "bun:test";
import {
	clearPendingRestore,
	readPendingRestore,
	writePendingRestore,
} from "./controller";

function memoryStorage() {
	const values = new Map<string, string>();
	return {
		get length() {
			return values.size;
		},
		key: (index: number) => [...values.keys()][index] ?? null,
		getItem: (key: string) => values.get(key) ?? null,
		setItem: (key: string, value: string) => values.set(key, value),
		removeItem: (key: string) => values.delete(key),
	};
}

describe("queued follow-up recovery storage", () => {
	test("persists claiming, restored, and applied recovery states", () => {
		const storage = memoryStorage();
		const claiming = {
			operationId: "operation-1",
			streamId: "stream-1",
			sessionId: "session-1",
			generation: 4,
			status: "claiming" as const,
		};
		expect(writePendingRestore("client-a", claiming, storage)).toBeTrue();
		expect(readPendingRestore("client-a", storage, "operation-1")).toEqual(
			claiming,
		);
		expect(readPendingRestore("client-b", storage)).toBeNull();

		const restored = {
			...claiming,
			status: "restored" as const,
			messages: ["first", "second"],
		};
		writePendingRestore("client-a", restored, storage);
		expect(readPendingRestore("client-a", storage)).toEqual(restored);

		const applied = { ...claiming, status: "applied" as const };
		writePendingRestore("client-a", applied, storage);
		expect(readPendingRestore("client-a", storage)).toEqual(applied);
		clearPendingRestore("client-a", "operation-1", storage);
		expect(readPendingRestore("client-a", storage)).toBeNull();
	});

	test("keeps concurrent operation records independent", () => {
		const storage = memoryStorage();
		const first = {
			operationId: "operation-1",
			streamId: "stream-1",
			sessionId: "session-1",
			generation: 1,
			status: "claiming" as const,
		};
		const second = { ...first, operationId: "operation-2", generation: 2 };
		writePendingRestore("client-a", first, storage);
		writePendingRestore("client-a", second, storage);

		expect(readPendingRestore("client-a", storage, "operation-1")).toEqual(
			first,
		);
		expect(readPendingRestore("client-a", storage, "operation-2")).toEqual(
			second,
		);
		clearPendingRestore("client-a", "operation-1", storage);
		expect(readPendingRestore("client-a", storage, "operation-2")).toEqual(
			second,
		);
	});
});
