import { describe, expect, test } from "bun:test";
import { mkdtemp, realpath, rm } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import path from "node:path";
import {
	createSession,
	deleteSession,
	SESSION_VERSION,
	type Session,
} from "../session";
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

	test("switchSession changes cwd and emits a cwd event", async () => {
		const originalCwd = process.cwd();
		const firstDir = await mkdtemp(path.join(tmpdir(), "kit-runtime-cwd-a-"));
		const secondDir = await mkdtemp(path.join(tmpdir(), "kit-runtime-cwd-b-"));
		const timestamp = new Date().toISOString();
		const session: Session = {
			id: "session-cwd-switch-test",
			version: SESSION_VERSION,
			cwd: firstDir,
			createdAt: timestamp,
			updatedAt: timestamp,
			turns: [],
		};
		const target = await createSession(secondDir);
		const runtime = new AgentRuntime(session);
		const cwdEvents: string[] = [];
		runtime.subscribe("session.active.changed.cwd", (event) => {
			cwdEvents.push(event.cwd);
		});
		try {
			expect(await runtime.switchSession(target.id)).toBe(true);
			expect(runtime.getSession().id).toBe(target.id);
			expect(process.cwd()).toBe(await realpath(secondDir));
			expect(cwdEvents).toEqual([secondDir]);
		} finally {
			process.chdir(originalCwd);
			runtime.dispose();
			await deleteSession(target.id);
			await rm(firstDir, { recursive: true, force: true });
			await rm(secondDir, { recursive: true, force: true });
		}
	});

	test("switchSession rejects missing cwd before mutating runtime state", async () => {
		const originalCwd = process.cwd();
		const firstDir = await mkdtemp(path.join(tmpdir(), "kit-runtime-cwd-a-"));
		const missingDir = await mkdtemp(
			path.join(tmpdir(), "kit-runtime-cwd-missing-"),
		);
		const timestamp = new Date().toISOString();
		const session: Session = {
			id: "session-cwd-invalid-switch-test",
			version: SESSION_VERSION,
			cwd: firstDir,
			createdAt: timestamp,
			updatedAt: timestamp,
			turns: [],
		};
		const target = await createSession(missingDir);
		await rm(missingDir, { recursive: true, force: true });
		const runtime = new AgentRuntime(session);
		try {
			await expect(runtime.switchSession(target.id)).rejects.toThrow(
				"Session working directory does not exist",
			);
			expect(runtime.getSession().id).toBe(session.id);
			expect(process.cwd()).toBe(await realpath(firstDir));
		} finally {
			process.chdir(originalCwd);
			runtime.dispose();
			await deleteSession(target.id);
			await rm(firstDir, { recursive: true, force: true });
		}
	});
});

describe("AgentRuntime scratchpad context", () => {
	test("includes non-empty scratchpad content as read-only context", async () => {
		const originalCwd = process.cwd();
		const tempRoot = await mkdtemp(
			path.join(tmpdir(), "kit-runtime-scratchpad-"),
		);
		const timestamp = new Date().toISOString();
		const runtime = new AgentRuntime({
			id: "session-scratchpad-test",
			version: SESSION_VERSION,
			cwd: tempRoot,
			createdAt: timestamp,
			updatedAt: timestamp,
			turns: [],
		});
		try {
			runtime.setScratchpadContent("Remember to check auth tests.");
			expect(runtime.getContextFiles()).toContainEqual({
				path: "<scratchpad>",
				content:
					"User scratchpad notes. Read-only to the agent; do not modify.\n\nRemember to check auth tests.",
			});
			runtime.setScratchpadContent("   ");
			expect(
				runtime
					.getContextFiles()
					.some((contextFile) => contextFile.path === "<scratchpad>"),
			).toBe(false);
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
