import { describe, expect, test } from "bun:test";
import { registerTerminalTurnStatus } from "./terminal-turn-status";

describe("terminal turn status", () => {
	test("tracks turn identity and clears status when disposed", () => {
		type Handler = (event: unknown) => void;
		const handlers = new Map<string, Handler>();
		const removed: string[] = [];
		let streaming = false;
		const fakeRuntime = {
			getStatus: () => ({ isStreaming: streaming }),
			subscribe(type: string, handler: Handler) {
				handlers.set(type, handler);
				return () => {
					removed.push(type);
					handlers.delete(type);
				};
			},
		};
		const runtime = fakeRuntime as unknown as Parameters<
			typeof registerTerminalTurnStatus
		>[0];
		const states: boolean[] = [];
		const dispose = registerTerminalTurnStatus(runtime, (active) => {
			states.push(active);
		});

		streaming = true;
		handlers.get("agent.turn.started")?.({ turn: { id: "turn-1" } });
		// Runtime still reports streaming during finalization, but matching the
		// completed turn must clear its terminal state.
		handlers.get("agent.turn.completed")?.({ turn: { id: "turn-1" } });
		handlers.get("agent.turn.started")?.({ turn: { id: "turn-2" } });
		// A delayed completion for an older turn must not clear the new turn.
		handlers.get("agent.turn.completed")?.({ turn: { id: "turn-1" } });
		streaming = false;
		handlers.get("agent.run.failed")?.({});
		dispose();

		expect(states).toEqual([true, false, true, false, false]);
		expect(removed).toEqual([
			"agent.turn.started",
			"agent.turn.completed",
			"agent.retry.failed",
			"agent.run.failed",
		]);
	});
});
