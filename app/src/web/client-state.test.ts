import { describe, expect, test } from "bun:test";
import {
	type ClientState,
	createClientState,
	hydrateMessageReference,
	ProtocolSyncError,
	reduceClientRecord,
} from "./client-state";

describe("web client state", () => {
	test("replaces state from a snapshot and enters live mode", () => {
		let state = createClientState();
		state = reduceClientRecord(state, {
			type: "sync",
			mode: "snapshot",
			streamId: "stream-1",
			sequence: 4,
			state: { sessionId: "session-1", isStreaming: false },
			messages: [{ role: "user", content: "hello" }],
			messageOffset: 3,
			totalMessageCount: 4,
			pendingInteractions: [{ id: "confirm-1", kind: "confirm" }],
		});
		expect(state).toMatchObject({
			phase: "synchronizing",
			streamId: "stream-1",
			sequence: 4,
			messageOffset: 3,
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
			type: "agent_start",
			streamId: "stream-1",
			sequence: 3,
		});
		expect(state.sequence).toBe(3);
		expect(
			reduceClientRecord(state, {
				type: "agent_start",
				streamId: "stream-1",
				sequence: 3,
			}),
		).toBe(state);
		expect(() =>
			reduceClientRecord(state, {
				type: "agent_settled",
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

	test("continues an assistant message present in a streaming snapshot", () => {
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
			messages: [
				{ role: "assistant", content: [{ type: "text", text: "hel" }] },
			],
			messageOffset: 0,
			totalMessageCount: 1,
			pendingInteractions: [],
		});
		state = reduceClientRecord(state, {
			type: "message_update",
			assistantMessageEvent: {
				type: "text_delta",
				contentIndex: 0,
				delta: "lo",
			},
			streamId: "stream-1",
			sequence: 3,
		});
		expect(state.messages[0]).toEqual({
			role: "assistant",
			content: [{ type: "text", text: "hello" }],
		});
		expect(state.queuedMessageCount).toBe(1);
	});

	test("buffers live deltas until an active snapshot reference is hydrated", () => {
		const reference = {
			type: "message_reference",
			role: "assistant",
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
		});
		state = reduceClientRecord(state, {
			type: "message_update",
			assistantMessageEvent: {
				type: "text_delta",
				contentIndex: 0,
				delta: "lo",
			},
			streamId: "stream-1",
			sequence: 3,
		});
		state = hydrateMessageReference(state, 0, reference, {
			role: "assistant",
			content: [{ type: "text", text: "hel" }],
		});
		expect(state.messages[0]).toEqual({
			role: "assistant",
			content: [{ type: "text", text: "hello" }],
		});
		expect(state.pendingActiveMessageDeltas).toEqual([]);
	});

	test("reduces streaming messages, tools, and interactions", () => {
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
			type: "message_start",
			message: { role: "assistant", content: [{ type: "text", text: "" }] },
		});
		apply({
			type: "message_update",
			assistantMessageEvent: {
				type: "text_delta",
				contentIndex: 0,
				delta: "hello",
			},
		});
		apply({
			type: "tool_execution_start",
			toolCallId: "tool-1",
			toolName: "read",
		});
		apply({
			type: "tool_execution_end",
			toolCallId: "tool-1",
			result: "done",
		});
		apply({
			type: "ui_request",
			request: { id: "request-1", kind: "confirm", payload: {} },
		});

		expect(state.messages).toEqual([
			{ role: "assistant", content: [{ type: "text", text: "hello" }] },
		]);
		expect(state.tools[0]).toMatchObject({
			id: "tool-1",
			status: "complete",
		});
		expect(state.pendingInteractions).toHaveLength(1);

		apply({ type: "ui_resolved", requestId: "request-1" });
		expect(state.pendingInteractions).toEqual([]);
	});
});
