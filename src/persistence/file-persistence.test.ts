import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { homedir as originalHomedir, tmpdir } from "node:os";
import path from "node:path";
import type { AgentRuntimeEvent } from "../runtime/agent-runtime";
import type { Session, Turn } from "../session";

const originalHome = process.env.HOME;
const originalCwd = process.cwd();
let mockedHomeDir = originalHomedir();

mock.module("node:os", () => ({
	homedir: () => mockedHomeDir,
	tmpdir,
}));

const storage = await import("../session/storage");
const { FilePersistence } = await import("./file-persistence");

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
		};
	},
) {
	return {
		role: "assistant" as const,
		content: [{ type: "text" as const, text }],
		api: "anthropic-messages" as const,
		provider: "anthropic" as const,
		model: "claude-sonnet-4-6",
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

function withUpdatedTimestamp(session: Session): Session {
	return {
		...session,
		updatedAt: new Date().toISOString(),
	};
}

class FakeRuntime {
	private readonly listeners = new Set<(event: AgentRuntimeEvent) => void>();

	constructor(private readonly session: Session) {}

	subscribe(listener: (event: AgentRuntimeEvent) => void): () => void {
		this.listeners.add(listener);
		return () => this.listeners.delete(listener);
	}

	getSession(): Session {
		return this.session;
	}

	emit(event: AgentRuntimeEvent): void {
		for (const listener of this.listeners) listener(event);
	}
}

describe("FilePersistence", () => {
	beforeEach(async () => {
		tempRoot = await mkdtemp(path.join(tmpdir(), "kit-persistence-test-"));
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

	test("persists completed turns observed from runtime events", async () => {
		const session = await storage.createSession(
			projectDir,
			"claude-sonnet-4-6",
		);
		const turn: Turn = {
			id: "turn-1",
			messages: [
				userMessage("turn-1", "hello"),
				assistantMessage("turn-1", "hi"),
			],
		};
		const runtime = new FakeRuntime(session);
		const persistence = new FilePersistence(runtime);

		runtime.emit({ type: "agent.turn.completed", turn });
		await persistence.flush();

		const restored = await storage.readSession(session.id);
		expect(restored?.turns.map((candidate) => candidate.id)).toEqual([
			"turn-1",
		]);
		expect(restored?.turns[0]?.messages.map((message) => message.role)).toEqual(
			["user", "assistant"],
		);
		persistence.dispose();
	});

	test("persists metadata changes observed from runtime events", async () => {
		let session = await storage.createSession(
			projectDir,
			"claude-sonnet-4-6",
			"medium",
		);
		const runtime = new FakeRuntime(session);
		const persistence = new FilePersistence(runtime);

		session = withUpdatedTimestamp({ ...session, name: "Named session" });
		runtime.emit({
			type: "session.name.changed",
			session,
			name: session.name,
		});
		session = withUpdatedTimestamp({ ...session, model: "other-model" });
		runtime.emit({
			type: "session.model.changed",
			session,
			modelId: session.model,
		});
		session = withUpdatedTimestamp({ ...session, thinkingLevel: "high" });
		runtime.emit({
			type: "session.thinking_level.changed",
			session,
			thinkingLevel: session.thinkingLevel,
		});
		await persistence.flush();

		const restored = await storage.readSession(session.id);
		expect(restored?.name).toBe("Named session");
		expect(restored?.model).toBe("other-model");
		expect(restored?.thinkingLevel).toBe("high");
		persistence.dispose();
	});

	test("persists compaction by writing kept turns before the compaction entry", async () => {
		const session = await storage.createSession(
			projectDir,
			"claude-sonnet-4-6",
		);
		const keptTurn: Turn = {
			id: "turn-kept",
			messages: [
				userMessage("turn-kept", "keep this"),
				assistantMessage("turn-kept", "kept answer"),
			],
		};
		const summaryMessage = assistantMessage("summary", "summary", {
			synthetic: { kind: "compaction-summary" },
		});
		const runtime = new FakeRuntime(session);
		const persistence = new FilePersistence(runtime);

		runtime.emit({
			type: "session.compaction.completed.auto",
			contextPercent: 91,
			compactedTurnCount: 3,
			keptTurnCount: 1,
			tokensBefore: 123,
			firstKeptTurnId: keptTurn.id,
			keptTurns: [keptTurn],
			summaryMessage,
		});
		await persistence.flush();

		const restored = await storage.readSession(session.id);
		expect(restored?.turns).toHaveLength(2);
		expect(restored?.turns[0]?.messages[0]?.synthetic?.kind).toBe(
			"compaction-summary",
		);
		expect(restored?.turns[1]?.id).toBe("turn-kept");
		persistence.dispose();
	});

	test("persists handoff summaries observed from runtime events", async () => {
		const session = await storage.createSession(
			projectDir,
			"claude-sonnet-4-6",
		);
		const summaryMessage = assistantMessage("summary", "handoff summary", {
			synthetic: { kind: "handoff-summary" },
		});
		const runtime = new FakeRuntime(session);
		const persistence = new FilePersistence(runtime);

		runtime.emit({
			type: "session.handoff_summary.appended",
			session,
			summaryMessage,
		});
		await persistence.flush();

		const restored = await storage.readSession(session.id);
		expect(restored?.turns).toHaveLength(1);
		expect(restored?.turns[0]?.messages[0]?.synthetic?.kind).toBe(
			"handoff-summary",
		);
		persistence.dispose();
	});
});
