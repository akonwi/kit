import { describe, expect, test } from "bun:test";
import { isRetryableProviderErrorMessage } from "./agent-runtime";

describe("retryable provider errors", () => {
	test("treats websocket abnormal closures as retryable", () => {
		expect(
			isRetryableProviderErrorMessage("WebSocket closed 1006 Connection ended"),
		).toBe(true);
	});

	test("does not treat ordinary model errors as retryable", () => {
		expect(isRetryableProviderErrorMessage("invalid API key")).toBe(false);
	});
});
