import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { randomUUID } from "node:crypto";
import { existsSync } from "node:fs";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { homedir as originalHomedir, tmpdir } from "node:os";
import path from "node:path";
import { SESSION_VERSION, type Session, type Turn } from "../session/types";

const originalHome = process.env.HOME;
const originalCwd = process.cwd();
let mockedHomeDir = originalHomedir();

mock.module("node:os", () => ({
	homedir: () => mockedHomeDir,
	tmpdir,
}));

const storage = await import("./session-storage");
const subagentStorage = await import("./subagent-session-storage");
const sidecars = await import("./session-sidecars");

let tempRoot = "";
let homeDir = "";
let projectDir = "";

function userMessage(turnId: string, text: string) {
	return {
		role: "user" as const,
		content: text,
		timestamp: Date.now(),
		turnId,
	};
}

function assistantMessage(
	turnId: string,
	text: string,
	options?: {
		synthetic?: {
			kind: "compaction-summary" | "handoff-summary";
			sourceSessionName?: string;
		};
	},
) {
	return {
		role: "assistant" as const,
		content: [{ type: "text" as const, text }],
		api: "anthropic-messages",
		provider: "anthropic",
		model: "claude-sonnet-4-5",
		usage: {
			input: 1,
			output: 1,
			cacheRead: 0,
			cacheWrite: 0,
			totalTokens: 2,
			cost: {
				input: 0,
				output: 0,
				cacheRead: 0,
				cacheWrite: 0,
				total: 0,
			},
		},
		stopReason: "stop" as const,
		timestamp: Date.now(),
		turnId,
		...(options?.synthetic ? { synthetic: options.synthetic } : {}),
	};
}

describe("session storage", () => {
	beforeEach(async () => {
		tempRoot = await mkdtemp(path.join(tmpdir(), "kit-session-test-"));
		homeDir = path.join(tempRoot, "home");
		projectDir = path.join(tempRoot, "project");
		await mkdir(homeDir, { recursive: true });
		await mkdir(projectDir, { recursive: true });
		process.env.HOME = homeDir;
		mockedHomeDir = homeDir;
		process.chdir(projectDir);
	});

	afterEach(async () => {
		process.chdir(originalCwd);
		if (originalHome === undefined) delete process.env.HOME;
		else process.env.HOME = originalHome;
		if (tempRoot) await rm(tempRoot, { recursive: true, force: true });
	});

	test("stores sub-agent sessions in a nested directory outside normal listings", async () => {
		const parent = await storage.createSession(projectDir, "claude-sonnet-4-5");
		await storage.writeSession(parent);
		const subagentId = randomUUID();
		await subagentStorage.createSubagentSession({
			id: subagentId,
			ownerSessionId: parent.id,
			cwd: projectDir,
			agentName: "scout",
			description: "Fast reconnaissance",
			model: "claude-sonnet-4-5",
			thinkingLevel: "medium",
			source: "agent",
		});

		const filePath = path.join(
			homeDir,
			".kit",
			"sessions",
			"subagents",
			`${subagentId}.jsonl`,
		);
		expect(existsSync(filePath)).toBe(true);
		expect((await storage.listAllSessions()).map((item) => item.id)).toEqual([
			parent.id,
		]);
		expect(
			await subagentStorage.readSubagentSessionHeader(subagentId),
		).toMatchObject({
			kind: "subagent",
			ownerSessionId: parent.id,
			agentName: "scout",
		});

		const appended = await subagentStorage.appendSubagentSessionEntries(
			subagentId,
			[
				{
					type: "subagent_prompt",
					timestamp: new Date().toISOString(),
					agentName: "scout",
					subagentConversationId: subagentId,
					source: "agent",
					prompt: "inspect auth",
				},
			],
		);
		expect(appended[0]?.parentId).toBeNull();

		await storage.deleteSession(parent.id);
		expect(existsSync(filePath)).toBe(false);
	});

	test("deleteSession removes scratchpad sidecar", async () => {
		const session = await storage.createSession(projectDir);
		const scratchpadFile = sidecars.scratchpadPath(session.id);
		await mkdir(path.dirname(scratchpadFile), { recursive: true });
		await writeFile(scratchpadFile, "private notes", "utf8");

		expect(existsSync(scratchpadFile)).toBe(true);
		await storage.deleteSession(session.id);
		expect(existsSync(scratchpadFile)).toBe(false);
	});

	test("persists turns as JSONL message entries and reconstructs them", async () => {
		const session = await storage.createSession(
			projectDir,
			"claude-sonnet-4-5",
		);
		const turn1: Turn = {
			id: "turn-1",
			messages: [
				userMessage("turn-1", "hello"),
				assistantMessage("turn-1", "hi"),
			],
		};
		const turn2: Turn = {
			id: "turn-2",
			messages: [
				userMessage("turn-2", "review this"),
				assistantMessage("turn-2", "done"),
			],
		};

		await storage.appendTurn(session, turn1);
		await storage.appendTurn(session, turn2);

		const restored = await storage.readSession(session.id);
		expect(restored?.turns.map((turn) => turn.id)).toEqual([
			"turn-1",
			"turn-2",
		]);
		expect(restored?.turns[0]?.messages).toHaveLength(2);
		expect(restored?.turns[1]?.messages).toHaveLength(2);

		const raw = await readFile(
			path.join(homeDir, ".kit", "sessions", `${session.id}.jsonl`),
			"utf8",
		);
		const lines = raw
			.trim()
			.split("\n")
			.map((line) => JSON.parse(line) as { type: string; turnId?: string });
		expect(lines[0]?.type).toBe("session");
		expect(lines.slice(1).map((line) => line.type)).toEqual([
			"message",
			"message",
			"message",
			"message",
		]);
		expect(lines.slice(1).map((line) => line.turnId)).toEqual([
			"turn-1",
			"turn-1",
			"turn-2",
			"turn-2",
		]);
	});

	test("persists cwd changes as latest session cwd", async () => {
		const session = await storage.createSession(projectDir);
		const nextDir = path.join(tempRoot, "other-project");
		await mkdir(nextDir, { recursive: true });
		const moved = {
			...session,
			cwd: nextDir,
			updatedAt: new Date(Date.now() + 1000).toISOString(),
		};

		await storage.appendCwdChange(moved, session.cwd, "user");

		const restored = await storage.readSession(session.id);
		expect(restored?.cwd).toBe(nextDir);
		expect(await storage.listSessionsForCwd(projectDir)).toEqual([]);
		expect(
			(await storage.listSessionsForCwd(nextDir)).map((s) => s.id),
		).toEqual([session.id]);

		const entries = await storage.readSessionEntries(session.id);
		expect(entries.at(-1)).toMatchObject({
			type: "cwd_change",
			cwd: nextDir,
			previousCwd: projectDir,
			source: "user",
		});
	});

	test("updateSession accepts and persists cwd changes", async () => {
		const session = await storage.createSession(projectDir);
		const nextDir = path.join(tempRoot, "updated-project");
		await mkdir(nextDir, { recursive: true });

		const updated = await storage.updateSession(session, { cwd: nextDir });

		expect(updated.cwd).toBe(nextDir);
		expect((await storage.readSession(session.id))?.cwd).toBe(nextDir);
	});

	test("updateSession preserves cwd metadata when writing turns", async () => {
		const session = await storage.createSession(projectDir);
		const nextDir = path.join(tempRoot, "updated-with-turns-project");
		await mkdir(nextDir, { recursive: true });
		const turn: Turn = {
			id: "turn-cwd-update",
			messages: [userMessage("turn-cwd-update", "move and remember this")],
		};

		await storage.updateSession(session, { cwd: nextDir, turns: [turn] });

		const raw = await readFile(
			path.join(homeDir, ".kit", "sessions", `${session.id}.jsonl`),
			"utf8",
		);
		const lines = raw
			.trim()
			.split("\n")
			.map((line) => JSON.parse(line) as { type: string; cwd?: string });
		expect(lines[0]).toMatchObject({ type: "session", cwd: projectDir });
		expect(lines.at(-1)).toMatchObject({ type: "cwd_change", cwd: nextDir });
		expect((await storage.readSession(session.id))?.cwd).toBe(nextDir);
		expect((await storage.readSession(session.id))?.turns).toHaveLength(1);
	});

	test("appendCwdChange records metadata when state starts uncached", async () => {
		const nextDir = path.join(tempRoot, "uncached-project");
		await mkdir(nextDir, { recursive: true });
		const timestamp = new Date().toISOString();
		const session: Session = {
			id: randomUUID(),
			version: SESSION_VERSION,
			cwd: nextDir,
			createdAt: timestamp,
			updatedAt: timestamp,
			turns: [],
		};

		await storage.appendCwdChange(session, projectDir, "agent");

		const entries = await storage.readSessionEntries(session.id);
		expect(entries.map((entry) => entry.type)).toEqual(["cwd_change"]);
		expect(entries[0]).toMatchObject({
			type: "cwd_change",
			cwd: nextDir,
			previousCwd: projectDir,
			source: "agent",
		});
		const raw = await readFile(
			path.join(homeDir, ".kit", "sessions", `${session.id}.jsonl`),
			"utf8",
		);
		expect(JSON.parse(raw.split("\n")[0] ?? "{}")).toMatchObject({
			type: "session",
			cwd: projectDir,
		});
		expect((await storage.readSession(session.id))?.cwd).toBe(nextDir);
	});

	test("appends messages incrementally and reconstructs their turn", async () => {
		const session = await storage.createSession(
			projectDir,
			"claude-sonnet-4-5",
		);
		await storage.appendMessage(
			session,
			"turn-incremental",
			userMessage("turn-incremental", "hello"),
		);
		await storage.appendMessage(
			session,
			"turn-incremental",
			assistantMessage("turn-incremental", "hi"),
		);

		const restored = await storage.readSession(session.id);
		expect(restored?.turns).toHaveLength(1);
		expect(restored?.turns[0]?.id).toBe("turn-incremental");
		expect(restored?.turns[0]?.messages.map((message) => message.role)).toEqual(
			["user", "assistant"],
		);
	});

	test("serializes concurrent appends into a single parent chain", async () => {
		const session = await storage.createSession(
			projectDir,
			"claude-sonnet-4-5",
		);
		await Promise.all([
			storage.appendSessionEntries(session, [
				{
					type: "session_info",
					timestamp: new Date().toISOString(),
					name: "first",
				},
			]),
			storage.appendSessionEntries(session, [
				{
					type: "model_change",
					timestamp: new Date().toISOString(),
					modelId: "second-model",
				},
			]),
		]);

		const entries = await storage.readSessionEntries(session.id);
		expect(entries).toHaveLength(2);
		expect(entries[0]?.parentId).toBeNull();
		expect(entries[1]?.parentId).toBe(entries[0]?.id);
	});

	test("reconstructs latest compaction as a synthetic summary plus kept turns", async () => {
		const session = await storage.createSession(
			projectDir,
			"claude-sonnet-4-5",
		);
		const turn1: Turn = {
			id: "turn-1",
			messages: [
				userMessage("turn-1", "earlier work"),
				assistantMessage("turn-1", "earlier answer"),
			],
		};
		const turn2: Turn = {
			id: "turn-2",
			messages: [
				userMessage("turn-2", "keep this"),
				assistantMessage("turn-2", "kept answer"),
			],
		};

		await storage.appendTurn(session, turn1);
		await storage.appendTurn(session, turn2);
		await storage.appendCompaction({
			session,
			summaryMessage: assistantMessage("summary", "compacted summary", {
				synthetic: { kind: "compaction-summary" },
			}),
			firstKeptTurnId: "turn-2",
			compactedTurnCount: 1,
			keptTurnCount: 1,
			tokensBefore: 123,
		});

		const restored = await storage.readSession(session.id);
		expect(restored?.turns).toHaveLength(2);
		expect(restored?.turns[0]?.messages[0]?.synthetic?.kind).toBe(
			"compaction-summary",
		);
		expect(restored?.turns[1]?.id).toBe("turn-2");
		expect(restored?.turns[1]?.messages[0]?.role).toBe("user");
	});

	test("requires a persisted kept-turn boundary before appending compaction", async () => {
		const session = await storage.createSession(
			projectDir,
			"claude-sonnet-4-5",
		);
		await expect(
			storage.appendCompaction({
				session,
				summaryMessage: assistantMessage("summary", "compacted summary", {
					synthetic: { kind: "compaction-summary" },
				}),
				firstKeptTurnId: "missing-turn",
				compactedTurnCount: 1,
				keptTurnCount: 1,
				tokensBefore: 123,
			}),
		).rejects.toThrow(/Compaction boundary could not be resolved/);
	});

	test("replays compact sub-agent delegation turns from persisted entries", async () => {
		const session = await storage.createSession(
			projectDir,
			"claude-sonnet-4-5",
		);
		await storage.appendSessionEntries(session, [
			{
				type: "subagent_started",
				timestamp: new Date().toISOString(),
				agentName: "scout",
				subagentConversationId: "conv-1",
				source: "agent",
				description: "Fast reconnaissance",
			},
			{
				type: "subagent_prompt",
				timestamp: new Date().toISOString(),
				agentName: "scout",
				subagentConversationId: "conv-1",
				source: "agent",
				prompt: "find auth entry points",
			},
			{
				type: "subagent_message_completed",
				timestamp: new Date().toISOString(),
				agentName: "scout",
				subagentConversationId: "conv-1",
				messageId: "msg-1",
				message: assistantMessage("delegated", "auth is in src/auth/index.ts"),
			},
		]);

		const entries = await storage.readSessionEntries(session.id);
		expect(entries.map((entry) => entry.type)).toEqual([
			"subagent_started",
			"subagent_prompt",
			"subagent_message_completed",
		]);

		const restored = await storage.readSession(session.id);
		expect(restored?.turns).toHaveLength(1);
		expect(restored?.turns[0]?.messages.map((message) => message.role)).toEqual(
			["assistant", "toolResult"],
		);
		expect(restored?.turns[0]?.messages[0]).toMatchObject({
			role: "assistant",
			stopReason: "toolUse",
			synthetic: {
				kind: "subagent-delegation",
				subagentName: "scout",
				subagentDescription: "Fast reconnaissance",
				subagentPrompt: "find auth entry points",
			},
			content: [
				{
					type: "toolCall",
					name: "subagent",
					arguments: {
						action: "run",
						agent: "scout",
						message: "find auth entry points",
					},
				},
			],
		});
	});

	test("replays delegated failures and aborts without marking them successful", async () => {
		const session = await storage.createSession(
			projectDir,
			"claude-sonnet-4-5",
		);
		await storage.appendSessionEntries(session, [
			{
				type: "subagent_started",
				timestamp: new Date().toISOString(),
				agentName: "scout",
				subagentConversationId: "conv-1",
				source: "agent",
			},
			{
				type: "subagent_prompt",
				timestamp: new Date().toISOString(),
				agentName: "scout",
				subagentConversationId: "conv-1",
				source: "agent",
				prompt: "first run",
			},
			{
				type: "subagent_failed",
				timestamp: new Date().toISOString(),
				agentName: "scout",
				subagentConversationId: "conv-1",
				error: "boom",
			},
			{
				type: "subagent_prompt",
				timestamp: new Date().toISOString(),
				agentName: "scout",
				subagentConversationId: "conv-1",
				source: "agent",
				prompt: "second run",
			},
			{
				type: "subagent_aborted",
				timestamp: new Date().toISOString(),
				agentName: "scout",
				subagentConversationId: "conv-1",
				reason: "Dismissed",
			},
		]);

		const restored = await storage.readSession(session.id);
		expect(restored?.turns).toHaveLength(2);
		expect(restored?.turns[0]?.messages[0]).toMatchObject({
			role: "assistant",
			stopReason: "toolUse",
		});
		expect(restored?.turns[0]?.messages[1]).toMatchObject({
			role: "toolResult",
			toolName: "subagent",
			isError: true,
			content: [{ type: "text", text: "boom" }],
		});
		expect(restored?.turns[1]?.messages[0]).toMatchObject({
			role: "assistant",
			stopReason: "aborted",
		});
	});

	test("skips synthetic delegation replay when a real subagent tool call exists", async () => {
		const session = await storage.createSession(
			projectDir,
			"claude-sonnet-4-5",
		);
		const turn: Turn = {
			id: "turn-1",
			messages: [
				{
					role: "assistant",
					content: [
						{
							type: "toolCall",
							id: "call-1",
							name: "subagent",
							arguments: {
								action: "run",
								agent: "scout",
								message: "find auth entry points",
							},
						},
					],
					api: "anthropic-messages",
					provider: "anthropic",
					model: "claude-sonnet-4-5",
					usage: {
						input: 1,
						output: 1,
						cacheRead: 0,
						cacheWrite: 0,
						totalTokens: 2,
						cost: {
							input: 0,
							output: 0,
							cacheRead: 0,
							cacheWrite: 0,
							total: 0,
						},
					},
					stopReason: "toolUse",
					timestamp: Date.now(),
					turnId: "turn-1",
				},
				{
					role: "toolResult",
					toolCallId: "call-1",
					toolName: "subagent",
					content: [],
					isError: false,
					timestamp: Date.now(),
					turnId: "turn-1",
				},
			],
		};
		await storage.appendTurn(session, turn);
		await storage.appendSessionEntries(session, [
			{
				type: "subagent_started",
				timestamp: new Date().toISOString(),
				agentName: "scout",
				subagentConversationId: "conv-1",
				source: "agent",
			},
			{
				type: "subagent_prompt",
				timestamp: new Date().toISOString(),
				agentName: "scout",
				subagentConversationId: "conv-1",
				source: "agent",
				prompt: "find auth entry points",
			},
		]);

		const restored = await storage.readSession(session.id);
		expect(restored?.turns).toHaveLength(1);
		expect(restored?.turns[0]?.id).toBe("turn-1");
	});

	test("migrates legacy .json sessions to .jsonl on load", async () => {
		const legacy: Session = {
			id: "legacy-session",
			version: 1 as Session["version"],
			cwd: projectDir,
			name: "Legacy",
			model: "claude-sonnet-4-5",
			thinkingLevel: "medium",
			createdAt: new Date().toISOString(),
			updatedAt: new Date().toISOString(),
			turns: [
				{
					id: "turn-1",
					messages: [
						userMessage("turn-1", "legacy user"),
						assistantMessage("turn-1", "legacy assistant"),
					],
				},
			],
		};
		const legacyPath = path.join(
			homeDir,
			".kit",
			"sessions",
			"legacy-session.json",
		);
		await mkdir(path.dirname(legacyPath), { recursive: true });
		await writeFile(legacyPath, JSON.stringify(legacy, null, 2));

		const restored = await storage.readSession("legacy-session");
		expect(restored?.id).toBe("legacy-session");
		expect(restored?.version).toBe(2);
		expect(
			existsSync(
				path.join(homeDir, ".kit", "sessions", "legacy-session.jsonl"),
			),
		).toBe(true);
		expect(existsSync(legacyPath)).toBe(false);
	});
});
