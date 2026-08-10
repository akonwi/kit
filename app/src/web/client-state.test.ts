import { describe, expect, test } from "bun:test";
import { parseRemoteChromeSnapshot } from "./chrome-state";
import {
	type ClientState,
	createClientState,
	hydrateMessageReference,
	ProtocolRebaseRequired,
	ProtocolSyncError,
	reduceClientRecord,
} from "./client-state";

const assistantMessage = (text: string) => ({
	role: "assistant",
	content: [{ type: "text", text }],
	messageId: "message-assistant",
	turnId: "turn-1",
});

const textDelta = (delta: string) => ({
	type: "agent.message.updated",
	messageId: "message-assistant",
	update: {
		kind: "content.delta",
		contentType: "text",
		contentIndex: 0,
		delta,
	},
});

describe("web client state", () => {
	test("rejects unsafe declarative chrome URLs", () => {
		expect(
			parseRemoteChromeSnapshot({
				header: {
					contributions: [
						{
							id: "bad.link",
							content: [{ text: "bad" }],
							plainText: "bad",
							side: "right",
							action: { type: "open-url", url: "javascript:alert(1)" },
							clickable: false,
						},
					],
					hiddenBuiltinIds: [],
				},
				footer: { contributions: [], hiddenBuiltinIds: [] },
			}),
		).toBeNull();
	});
	test("replaces state from a snapshot and enters live mode", () => {
		let state = createClientState();
		state = reduceClientRecord(state, {
			type: "sync",
			mode: "snapshot",
			streamId: "stream-1",
			sequence: 4,
			state: { sessionId: "session-1", isStreaming: false },
			messages: [
				{
					role: "user",
					content: "hello",
					messageId: "message-user",
					turnId: "turn-1",
				},
			],
			messageOffset: 3,
			totalMessageCount: 4,
			pendingInteractions: [{ id: "confirm-1", kind: "confirm" }],
			pendingInteractionGeneration: 1,
		});
		expect(state).toMatchObject({
			phase: "synchronizing",
			streamId: "stream-1",
			sequence: 4,
			messageKeys: ["message-user"],
			messageOffset: 3,
			pendingInteractionGeneration: 1,
			totalMessageCount: 4,
		});
		state = reduceClientRecord(state, {
			type: "sync_complete",
			mode: "snapshot",
			streamId: "stream-1",
			sequence: 4,
		});
		expect(state.phase).toBe("live");
	});

	test("validates chrome snapshots and live contribution changes", () => {
		let state = reduceClientRecord(createClientState(), {
			type: "sync",
			mode: "snapshot",
			streamId: "stream-1",
			sequence: 0,
			state: { sessionId: "session-1", isStreaming: false },
			messages: [],
			pendingInteractions: [],
			pendingInteractionGeneration: 0,
			chrome: {
				header: {
					contributions: [
						{
							id: "speech.status",
							content: [
								{ text: "speech ", style: { fgToken: "textMuted" } },
								{ text: "on", style: { fgToken: "toolText", bold: true } },
							],
							plainText: "speech on",
							side: "right",
							clickable: true,
						},
					],
					hiddenBuiltinIds: ["kit.header.title"],
				},
				footer: { contributions: [], hiddenBuiltinIds: [] },
			},
		});
		expect(state.chrome.header).toMatchObject({
			contributions: [
				expect.objectContaining({ id: "speech.status", clickable: true }),
			],
			hiddenBuiltinIds: ["kit.header.title"],
		});

		state = reduceClientRecord(state, {
			type: "shell.chrome.changed",
			streamId: "stream-1",
			sequence: 1,
			chrome: {
				header: { contributions: [], hiddenBuiltinIds: [] },
				footer: {
					contributions: [
						{
							id: "plugin.footer",
							content: [{ text: "ready" }],
							plainText: "ready",
							side: "left",
							action: {
								type: "open-url",
								url: "https://example.com/status",
							},
							clickable: false,
						},
					],
					hiddenBuiltinIds: [],
				},
			},
		});
		expect(state.chrome.footer.contributions[0]).toMatchObject({
			plainText: "ready",
			action: { type: "open-url", url: "https://example.com/status" },
		});
		expect(() =>
			reduceClientRecord(state, {
				type: "shell.chrome.changed",
				streamId: "stream-1",
				sequence: 2,
				chrome: {
					header: {
						contributions: [
							{
								id: "bad",
								content: [{ text: "bad", style: { fgToken: "unknown" } }],
								plainText: "bad",
								side: "left",
								clickable: false,
							},
						],
						hiddenBuiltinIds: [],
					},
					footer: { contributions: [], hiddenBuiltinIds: [] },
				},
			}),
		).toThrow("Invalid chrome contribution state");
	});

	test("applies ordered replay, ignores duplicates, and rejects gaps", () => {
		let state: ClientState = {
			...createClientState(),
			phase: "disconnected",
			streamId: "stream-1",
			sequence: 2,
		};
		state = reduceClientRecord(state, {
			type: "sync",
			mode: "replay",
			streamId: "stream-1",
			sequence: 2,
			targetSequence: 3,
		});
		state = reduceClientRecord(state, {
			type: "agent.start",
			streamId: "stream-1",
			sequence: 3,
		});
		expect(state.sequence).toBe(3);
		expect(
			reduceClientRecord(state, {
				type: "agent.start",
				streamId: "stream-1",
				sequence: 3,
			}),
		).toBe(state);
		expect(() =>
			reduceClientRecord(state, {
				type: "agent.settled",
				streamId: "stream-1",
				sequence: 5,
			}),
		).toThrow(ProtocolSyncError);
	});

	test("requires a snapshot when another client changes the session", () => {
		const state = {
			...createClientState(),
			phase: "live" as const,
			streamId: "stream-1",
			serverState: { sessionId: "session-1" },
		};
		expect(() =>
			reduceClientRecord(state, {
				type: "state_changed",
				state: { sessionId: "session-2" },
				streamId: "stream-1",
				sequence: 1,
			}),
		).toThrow("The active session changed");
	});

	test("continues an identified assistant message from a streaming snapshot", () => {
		let state = reduceClientRecord(createClientState(), {
			type: "sync",
			mode: "snapshot",
			streamId: "stream-1",
			sequence: 2,
			state: {
				sessionId: "session-1",
				isStreaming: true,
				pendingMessageCount: 1,
			},
			messages: [assistantMessage("hel")],
			messageOffset: 0,
			totalMessageCount: 1,
			pendingInteractions: [],
			pendingInteractionGeneration: 0,
		});
		state = reduceClientRecord(state, {
			...textDelta("lo"),
			streamId: "stream-1",
			sequence: 3,
		});
		expect(state.messages[0]).toEqual(assistantMessage("hello"));
		expect(state.queuedMessageCount).toBe(1);
	});

	test("buffers live deltas until an identified snapshot reference is hydrated", () => {
		const reference = {
			type: "message_reference",
			role: "assistant",
			messageId: "message-assistant",
			turnId: "turn-1",
			messageIndex: 0,
			token: "token-1",
		};
		let state = reduceClientRecord(createClientState(), {
			type: "sync",
			mode: "snapshot",
			streamId: "stream-1",
			sequence: 2,
			state: { sessionId: "session-1", isStreaming: true },
			messages: [reference],
			messageOffset: 0,
			totalMessageCount: 1,
			pendingInteractions: [],
			pendingInteractionGeneration: 0,
		});
		state = reduceClientRecord(state, {
			...textDelta("lo"),
			streamId: "stream-1",
			sequence: 3,
		});
		state = hydrateMessageReference(
			state,
			0,
			reference,
			assistantMessage("hel"),
		);
		expect(state.messages[0]).toEqual(assistantMessage("hello"));
		expect(state.pendingActiveMessageDeltas).toEqual([]);
	});

	test("reduces semantic message, tool, and interaction events", () => {
		let state: ClientState = {
			...createClientState(),
			phase: "live",
			streamId: "stream-1",
		};
		const apply = (record: Record<string, unknown>) => {
			state = reduceClientRecord(state, {
				...record,
				streamId: "stream-1",
				sequence: state.sequence + 1,
			});
		};
		apply({
			type: "agent.message.started",
			turnId: "turn-1",
			messageId: "message-assistant",
			message: assistantMessage(""),
		});
		apply(textDelta("hello"));
		apply({
			type: "agent.tool.started",
			turnId: "turn-1",
			toolCallId: "tool-1",
			toolName: "read",
		});
		apply({
			type: "agent.tool.ended",
			turnId: "turn-1",
			toolCallId: "tool-1",
			result: "done",
		});
		apply({
			type: "ui_request",
			generation: 1,
			request: { id: "request-1", kind: "confirm", payload: {} },
		});
		apply({
			type: "scratchpad.changed",
			sessionId: "session-1",
			content: "shared notes",
		});

		expect(state.messages).toEqual([assistantMessage("hello")]);
		expect(state.messageKeys).toEqual(["message-assistant"]);
		expect(state.tools[0]).toMatchObject({
			id: "tool-1",
			status: "complete",
		});
		expect(state.pendingInteractions).toHaveLength(1);
		expect(state.pendingInteractionGeneration).toBe(1);
		expect(state.serverState.scratchpad).toEqual({
			sessionId: "session-1",
			content: "shared notes",
		});

		apply({
			type: "ui_resolved",
			generation: 2,
			requestId: "request-1",
		});
		expect(state.pendingInteractions).toEqual([]);
		expect(state.pendingInteractionGeneration).toBe(2);
	});

	test("accepts an authoritative pending interaction snapshot", () => {
		const state: ClientState = {
			...createClientState(),
			phase: "live",
			streamId: "stream-1",
		};
		const next = reduceClientRecord(state, {
			type: "ui_snapshot",
			generation: 7,
			requests: [
				{ id: "request-1", kind: "confirm", payload: {} },
				{ id: "request-2", kind: "input", payload: {} },
			],
		});
		expect(next.pendingInteractionGeneration).toBe(7);
		expect(next.pendingInteractions).toHaveLength(2);
		expect(next.totalPendingInteractionCount).toBe(2);
	});

	test("rejects gaps in the pending interaction generation", () => {
		const state: ClientState = {
			...createClientState(),
			phase: "live",
			streamId: "stream-1",
			pendingInteractionGeneration: 3,
		};
		expect(() =>
			reduceClientRecord(state, {
				type: "ui_request",
				generation: 5,
				request: { id: "request-1", kind: "confirm", payload: {} },
				streamId: "stream-1",
				sequence: 1,
			}),
		).toThrow("Interaction generation mismatch");
	});

	test("requires a fresh snapshot when the server replaces the transcript", () => {
		const state: ClientState = {
			...createClientState(),
			phase: "live",
			streamId: "stream-1",
		};
		const replaceTranscript = () =>
			reduceClientRecord(state, {
				type: "session.transcript.replaced",
				reason: "compaction",
				streamId: "stream-1",
				sequence: 1,
			});
		expect(replaceTranscript).toThrow(ProtocolRebaseRequired);
		expect(replaceTranscript).toThrow("The transcript changed");
	});

	test("deduplicates semantic lifecycle and commit events by message id", () => {
		let state: ClientState = {
			...createClientState(),
			phase: "live",
			streamId: "stream-1",
		};
		const records = [
			{
				type: "user.message.created",
				messageId: "message-user",
				message: {
					role: "user",
					content: "hello",
					messageId: "message-user",
					turnId: "turn-1",
				},
			},
			{
				type: "session.message.appended",
				messageId: "message-user",
				message: {
					role: "user",
					content: "hello",
					messageId: "message-user",
					turnId: "turn-1",
				},
			},
		];
		for (const record of records) {
			state = reduceClientRecord(state, {
				...record,
				streamId: "stream-1",
				sequence: state.sequence + 1,
			});
		}
		expect(state.messages).toHaveLength(1);
		expect(state.messageKeys).toEqual(["message-user"]);
		expect(state.totalMessageCount).toBe(1);
	});
});
