import { describe, expect, test } from "bun:test";
import { RemoteEventJournal } from "./remote-event-journal";

describe("RemoteEventJournal", () => {
	test("assigns one ordered stream sequence and replays a suffix", () => {
		const journal = new RemoteEventJournal({ streamId: "stream-1" });
		expect(journal.append({ type: "first" })).toEqual({
			type: "first",
			streamId: "stream-1",
			sequence: 1,
		});
		journal.append({ type: "second" });
		journal.append({ type: "third" });

		expect(journal.latestSequence).toBe(3);
		expect(journal.replayAfter(1)).toEqual([
			{ type: "second", streamId: "stream-1", sequence: 2 },
			{ type: "third", streamId: "stream-1", sequence: 3 },
		]);
		expect(journal.replayAfter(3)).toEqual([]);
	});

	test("returns null when count eviction leaves a replay gap", () => {
		const journal = new RemoteEventJournal({
			streamId: "stream-1",
			maxEvents: 2,
		});
		journal.append({ type: "first" });
		journal.append({ type: "second" });
		journal.append({ type: "third" });

		expect(journal.replayAfter(0)).toBeNull();
		expect(journal.replayAfter(1)).toEqual([
			{ type: "second", streamId: "stream-1", sequence: 2 },
			{ type: "third", streamId: "stream-1", sequence: 3 },
		]);
	});

	test("returns null after an oversized event that was not retained", () => {
		const journal = new RemoteEventJournal({
			streamId: "stream-1",
			maxBytes: 100,
		});
		journal.append({ type: "first" });
		journal.append({ type: "large", text: "x".repeat(200) });
		journal.append({ type: "third" });

		expect(journal.replayAfter(1)).toBeNull();
		expect(journal.replayAfter(2)).toEqual([
			{ type: "third", streamId: "stream-1", sequence: 3 },
		]);
	});

	test("normalizes bigint values without creating sequence holes", () => {
		const journal = new RemoteEventJournal({ streamId: "stream-1" });
		expect(journal.append({ type: "usage", tokens: 12n })).toEqual({
			type: "usage",
			tokens: "12",
			streamId: "stream-1",
			sequence: 1,
		});
		const circular: Record<string, unknown> = { type: "circular" };
		circular.self = circular;
		expect(journal.append(circular)).toEqual({
			type: "resync_required",
			reason: "event_serialization_failed",
			streamId: "stream-1",
			sequence: 2,
		});
		expect(journal.replayAfter(1)).toEqual([
			{
				type: "resync_required",
				reason: "event_serialization_failed",
				streamId: "stream-1",
				sequence: 2,
			},
		]);
	});

	test("rejects invalid and future cursors", () => {
		const journal = new RemoteEventJournal({ streamId: "stream-1" });
		journal.append({ type: "first" });
		expect(journal.replayAfter(-1)).toBeNull();
		expect(journal.replayAfter(2)).toBeNull();
		expect(journal.replayAfter(0.5)).toBeNull();
	});
});
