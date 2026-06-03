import { describe, expect, test } from "bun:test";
import { mkdtemp, realpath, rm } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import path from "node:path";
import { SESSION_VERSION, type Session } from "../session";
import { AgentRuntime, isRetryableProviderErrorMessage } from "./agent-runtime";

describe("AgentRuntime cwd changes", () => {
	test("expands ~ targets to the user home directory and updates the process cwd", async () => {
		const originalCwd = process.cwd();
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
			expect(process.cwd()).toBe(await realpath(tempRoot));
			await runtime.changeCwd("~", "user");
			expect(runtime.getSession().cwd).toBe(homedir());
			expect(process.cwd()).toBe(homedir());
		} finally {
			process.chdir(originalCwd);
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
