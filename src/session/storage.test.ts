import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { existsSync } from "node:fs";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { homedir as originalHomedir, tmpdir } from "node:os";
import path from "node:path";
import type { Session, Turn } from "./types";

const originalHome = process.env.HOME;
const originalCwd = process.cwd();
let mockedHomeDir = originalHomedir();

mock.module("node:os", () => ({
	homedir: () => mockedHomeDir,
	tmpdir,
}));

const storage = await import("./storage");

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
