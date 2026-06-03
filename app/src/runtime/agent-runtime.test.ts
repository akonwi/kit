import { describe, expect, test } from "bun:test";
import { mkdtemp, rm } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import path from "node:path";
import { SESSION_VERSION, type Session } from "../session";
import { AgentRuntime, isRetryableProviderErrorMessage } from "./agent-runtime";

describe("AgentRuntime cwd changes", () => {
	test("expands ~ targets to the user home directory", async () => {
		const tempRoot = await mkdtemp(path.join(tmpdir(), "kit-runtime-cwd-"));
		const timestamp = new Date().toISOString();
		const session: Session = {
			id: "session-cwd-test",
			version: SESSION_VERSION,
			cwd: tempRoot,
			createdAt: timestamp,
			updatedAt: timestamp,
			turns: [],
		};
		const runtime = new AgentRuntime(session);
		try {
			await runtime.changeCwd("~", "user");
			expect(runtime.getSession().cwd).toBe(homedir());
		} finally {
			runtime.dispose();
			await rm(tempRoot, { recursive: true, force: true });
		}
	});
});

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
