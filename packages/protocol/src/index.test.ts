import { describe, expect, test } from "bun:test";
import {
	isRpcCommand,
	parseRpcCommand,
	RPC_BASE_COMMAND_TYPES,
	RPC_PROTOCOL_VERSION,
} from "./index";

describe("RPC protocol contract", () => {
	test("exports the version 2 base command vocabulary", () => {
		expect(RPC_PROTOCOL_VERSION).toBe(2);
		expect(RPC_BASE_COMMAND_TYPES).toEqual([
			"prompt",
			"steer",
			"follow_up",
			"restore_follow_ups",
			"promote_follow_ups",
			"acknowledge_follow_up_mutation",
			"abort",
			"new_session",
			"list_sessions",
			"open_session",
			"change_cwd",
			"get_capabilities",
			"get_state",
			"get_messages",
			"get_last_assistant_text",
			"get_available_models",
			"set_model",
			"get_available_thinking_levels",
			"set_thinking_level",
			"get_scratchpad",
			"update_scratchpad",
		]);
	});

	test("accepts command envelopes including forward-compatible command types", () => {
		expect(isRpcCommand({ id: "request-1", type: "prompt" })).toBe(true);
		expect(isRpcCommand({ id: 1, type: "prompt" })).toBe(true);
		expect(isRpcCommand({ type: "future_command" })).toBe(true);
	});

	test("rejects values without a string command type", () => {
		for (const value of [null, [], "prompt", {}, { type: 1 }]) {
			expect(isRpcCommand(value)).toBe(false);
		}
	});

	test("parses valid commands and rejects invalid envelopes", () => {
		const command = { type: "abort", id: "request-2" };
		expect(parseRpcCommand(command)).toBe(command);
		expect(() => parseRpcCommand({ type: false })).toThrow(
			"RPC command must be an object with a string type",
		);
	});
});
