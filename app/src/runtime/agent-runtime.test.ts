import { describe, expect, test } from "bun:test";
import { mkdtemp, realpath, rm } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import path from "node:path";
import {
	createSession,
	deleteSession,
	listAllSessions,
	SESSION_VERSION,
	type Session,
	writeSession,
} from "../session";
import { scratchpadPath } from "../storage/session-sidecars";
import type { AgentTool } from "./agent";
import { AgentRuntime, isRetryableProviderErrorMessage } from "./agent-runtime";

const blockedTool = { name: "blocked" } as AgentTool;
const customTool = { name: "custom" } as AgentTool;

function runtimeSession(id: string): Session {
	const timestamp = new Date().toISOString();
	return {
		id,
		version: SESSION_VERSION,
		cwd: process.cwd(),
		createdAt: timestamp,
		updatedAt: timestamp,
		turns: [],
	};
}

describe("AgentRuntime tool exclusions", () => {
	test("excludes configured tools while retaining other tools", () => {
		const runtime = new AgentRuntime(runtimeSession("excluded-tools-test"), {
			extraTools: [blockedTool, customTool],
			disableGitWatcher: true,
			excludedToolNames: [blockedTool.name],
		});
		try {
			const toolNames = runtime.getTools().map((tool) => tool.name);
			expect(toolNames).not.toContain(blockedTool.name);
			expect(toolNames).toContain(customTool.name);
		} finally {
			runtime.dispose();
		}
	});

	test("keeps tools when they are not excluded", () => {
		const runtime = new AgentRuntime(runtimeSession("included-tools-test"), {
			disableGitWatcher: true,
			extraTools: [blockedTool],
		});
		try {
			expect(runtime.getTools().map((tool) => tool.name)).toContain(
				blockedTool.name,
			);
		} finally {
			runtime.dispose();
		}
	});
});

describe("AgentRuntime manual compaction", () => {
	test("keeps TUI compaction non-throwing and exposes strict RPC failures", async () => {
		const runtime = new AgentRuntime(runtimeSession("strict-compaction-test"), {
			disableGitWatcher: true,
		});
		const failures: string[] = [];
		const unsubscribe = runtime.subscribe(
			"session.compaction.failed.manual",
			(event) => failures.push(event.error),
		);
		try {
			let strictFailure = "";
			try {
				await runtime.compactOrThrow();
			} catch (error) {
				strictFailure = error instanceof Error ? error.message : String(error);
			}
			expect(strictFailure.length).toBeGreaterThan(0);
			await expect(runtime.compact()).resolves.toBeUndefined();
			expect(failures).toEqual([strictFailure, strictFailure]);
		} finally {
			unsubscribe();
			runtime.dispose();
		}
	});
});

describe("AgentRuntime session creation persistence", () => {
	test("persists new sessions only when requested", async () => {
		const runtime = new AgentRuntime(
			runtimeSession("new-session-policy-test"),
			{
				disableGitWatcher: true,
			},
		);
		let ephemeralId = "";
		let persistentId = "";
		try {
			await runtime.newSession(undefined, { persist: false });
			ephemeralId = runtime.getSession().id;
			await runtime.newSession(undefined, { persist: true });
			persistentId = runtime.getSession().id;
			const listedIds = new Set(
				(await listAllSessions()).map((session) => session.id),
			);
			expect(listedIds.has(ephemeralId)).toBe(false);
			expect(listedIds.has(persistentId)).toBe(true);
		} finally {
			runtime.dispose();
			if (ephemeralId) await deleteSession(ephemeralId);
			if (persistentId) await deleteSession(persistentId);
		}
	});

	test("applies the configured default model to new sessions", async () => {
		const selector = "test-provider/configured-default";
		const runtime = new AgentRuntime(
			runtimeSession("new-session-default-model-test"),
			{
				disableGitWatcher: true,
				settings: { defaultModel: selector },
			},
		);
		const currentModel = runtime.getCurrentModel();
		expect(currentModel).toBeDefined();
		if (!currentModel) {
			runtime.dispose();
			return;
		}
		const configuredModel = {
			...currentModel,
			provider: "test-provider",
			id: "configured-default",
		};
		runtime.getAvailableModels = () => [configuredModel];
		const modelEvents: string[] = [];
		const unsubscribe = runtime.subscribe("agent.model.changed", (event) => {
			modelEvents.push(`${event.model?.provider}/${event.model?.id}`);
		});

		try {
			await runtime.newSession(undefined, { persist: false });
			expect(runtime.getCurrentModel()).toBe(configuredModel);
			expect(runtime.getSession().model).toBe(configuredModel.id);
			expect(modelEvents).toContain(selector);
		} finally {
			unsubscribe();
			runtime.dispose();
		}
	});

	test("does not persist ephemeral handoff sessions", async () => {
		const session = runtimeSession("handoff-session-policy-test");
		session.turns = [{ id: "turn-1", messages: [] }];
		const runtime = new AgentRuntime(session, { disableGitWatcher: true });
		let childId = "";
		try {
			const child = await runtime.handoffSession(undefined, { persist: false });
			childId = child.id;
			const listedIds = new Set(
				(await listAllSessions()).map((entry) => entry.id),
			);
			expect(listedIds.has(child.id)).toBe(false);
		} finally {
			runtime.dispose();
			if (childId) await deleteSession(childId);
		}
	});
});

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
		const runtime = new AgentRuntime(session, { disableGitWatcher: true });
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

	test("switchSessionById requires an exact id, changes cwd, and emits a cwd event", async () => {
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
		await writeSession(target);
		const runtime = new AgentRuntime(session, { disableGitWatcher: true });
		const cwdEvents: string[] = [];
		runtime.subscribe("session.active.changed.cwd", (event) => {
			cwdEvents.push(event.cwd);
		});
		try {
			expect(await runtime.switchSessionById(target.id.slice(0, 8))).toBe(
				false,
			);
			expect(runtime.getSession().id).toBe(session.id);
			expect(await runtime.switchSessionById(target.id)).toBe(true);
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
		const runtime = new AgentRuntime(session, { disableGitWatcher: true });
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
	test("exposes the scratchpad as an editable context file", async () => {
		const originalCwd = process.cwd();
		const tempRoot = await mkdtemp(
			path.join(tmpdir(), "kit-runtime-scratchpad-"),
		);
		const timestamp = new Date().toISOString();
		const runtime = new AgentRuntime(
			{
				id: "session-scratchpad-test",
				version: SESSION_VERSION,
				cwd: tempRoot,
				createdAt: timestamp,
				updatedAt: timestamp,
				turns: [],
			},
			{ disableGitWatcher: true },
		);
		try {
			const filePath = scratchpadPath("session-scratchpad-test");
			expect(
				runtime.getTools().some((tool) => tool.name === "edit_scratchpad"),
			).toBe(false);
			runtime.setScratchpadContent("Remember to check auth tests.");
			expect(
				runtime.getTools().some((tool) => tool.name === "edit_scratchpad"),
			).toBe(true);
			expect(runtime.getContextFiles()).toContainEqual({
				path: filePath,
				content: "Remember to check auth tests.",
			});
			runtime.setScratchpadContent("");
			expect(runtime.getContextFiles()).toContainEqual({
				path: filePath,
				content: "",
			});
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
