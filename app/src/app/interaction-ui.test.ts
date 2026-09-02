import { describe, expect, test } from "bun:test";
import {
	createInteractionHandler,
	type InteractionEntry,
} from "./interaction-ui";

describe("interaction UI queue", () => {
	function setup() {
		let entries: InteractionEntry[] = [];
		const open = createInteractionHandler(
			() => entries,
			(next) => {
				entries = next;
			},
		);
		return { open, entries: () => entries };
	}

	test("queues interactions in request order and removes resolved entries", async () => {
		const queue = setup();
		const first = queue.open<string>(() => undefined as never, {
			abortValue: "cancelled",
		});
		const second = queue.open<string>(() => undefined as never, {
			abortValue: "cancelled",
		});

		expect(queue.entries()).toHaveLength(2);
		const [firstEntry, secondEntry] = queue.entries();
		firstEntry?.resolve("first answer");
		expect(await first).toBe("first answer");
		expect(queue.entries().map((entry) => entry.id)).toEqual([secondEntry?.id]);

		secondEntry?.resolve("second answer");
		expect(await second).toBe("second answer");
		expect(queue.entries()).toEqual([]);
	});

	test("cancels an interaction with its typed fallback", async () => {
		const queue = setup();
		const result = queue.open<boolean>(() => undefined as never, {
			abortValue: false,
		});

		queue.entries()[0]?.cancel();

		expect(await result).toBe(false);
		expect(queue.entries()).toEqual([]);
	});

	test("aborts a queued interaction before it is rendered", async () => {
		const queue = setup();
		const abortController = new AbortController();
		const result = queue.open<boolean>(() => undefined as never, {
			signal: abortController.signal,
			abortValue: false,
		});

		expect(queue.entries()).toHaveLength(1);
		abortController.abort();

		expect(await result).toBe(false);
		expect(queue.entries()).toEqual([]);
	});
});
