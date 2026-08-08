import { randomUUID } from "node:crypto";

export const DEFAULT_REMOTE_EVENT_HISTORY_COUNT = 2048;
export const DEFAULT_REMOTE_EVENT_HISTORY_BYTES = 8 * 1024 * 1024;

export type SequencedRemoteEvent = Record<string, unknown> & {
	streamId: string;
	sequence: number;
};

export type RemoteEventJournalOptions = {
	streamId?: string;
	maxEvents?: number;
	maxBytes?: number;
};

type JournalEntry = {
	event: SequencedRemoteEvent;
	bytes: number;
};

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function boundedInteger(value: number | undefined, fallback: number): number {
	return value !== undefined && Number.isFinite(value)
		? Math.max(0, Math.floor(value))
		: fallback;
}

export class RemoteEventJournal {
	readonly streamId: string;
	private readonly maxEvents: number;
	private readonly maxBytes: number;
	private readonly entries: JournalEntry[] = [];
	private retainedBytes = 0;
	private sequence = 0;

	constructor(options: RemoteEventJournalOptions = {}) {
		this.streamId = options.streamId ?? randomUUID();
		this.maxEvents = boundedInteger(
			options.maxEvents,
			DEFAULT_REMOTE_EVENT_HISTORY_COUNT,
		);
		this.maxBytes = boundedInteger(
			options.maxBytes,
			DEFAULT_REMOTE_EVENT_HISTORY_BYTES,
		);
	}

	get latestSequence(): number {
		return this.sequence;
	}

	get retention(): { maxEvents: number; maxBytes: number } {
		return { maxEvents: this.maxEvents, maxBytes: this.maxBytes };
	}

	append(record: unknown): SequencedRemoteEvent {
		const sequence = this.sequence + 1;
		const candidate: SequencedRemoteEvent = {
			...(isRecord(record) ? record : { type: "event", value: record }),
			streamId: this.streamId,
			sequence,
		};
		let event: SequencedRemoteEvent;
		let serialized: string;
		try {
			serialized = JSON.stringify(candidate, (_key, value) =>
				typeof value === "bigint" ? value.toString() : value,
			);
			event = JSON.parse(serialized) as SequencedRemoteEvent;
		} catch {
			event = {
				type: "resync_required",
				reason: "event_serialization_failed",
				streamId: this.streamId,
				sequence,
			};
			serialized = JSON.stringify(event);
			this.entries.length = 0;
			this.retainedBytes = 0;
		}
		this.sequence = sequence;
		const bytes = Buffer.byteLength(serialized, "utf8");
		if (this.maxEvents === 0 || bytes > this.maxBytes) {
			this.entries.length = 0;
			this.retainedBytes = 0;
			return event;
		}

		this.entries.push({ event, bytes });
		this.retainedBytes += bytes;
		while (
			this.entries.length > this.maxEvents ||
			this.retainedBytes > this.maxBytes
		) {
			const removed = this.entries.shift();
			if (removed) this.retainedBytes -= removed.bytes;
		}
		return event;
	}

	replayAfter(sequence: number): SequencedRemoteEvent[] | null {
		if (
			!Number.isSafeInteger(sequence) ||
			sequence < 0 ||
			sequence > this.sequence
		) {
			return null;
		}
		if (sequence === this.sequence) return [];
		const first = this.entries[0]?.event.sequence;
		if (first === undefined || sequence < first - 1) return null;
		return this.entries
			.filter((entry) => entry.event.sequence > sequence)
			.map((entry) => entry.event);
	}
}
