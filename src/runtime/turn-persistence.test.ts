/**
 * Architectural contract tests for turn assembly and persistence.
 *
 * These tests pin down the invariants we want for user input → turn assembly
 * → persistence. Some of them are expected to FAIL against the current
 * implementation; that's intentional. They form the foundation we'll use to
 * redesign this surface on the `fix/session-turn-persistence-architecture`
 * branch.
 *
 * Layout:
 *
 *   1. Turn assembly contract (KitAgent)
 *   2. Persistence buffer contract (AgentRuntime)
 *   3. Submission shape uniformity (AgentRuntime)
 *   4. Hydration contract (storage + transcript)
 */

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { homedir as originalHomedir, tmpdir } from "node:os";
import path from "node:path";
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type { Session, Turn } from "../session/types";

// ---- Test environment setup -------------------------------------------------

const originalHome = process.env.HOME;
const originalCwd = process.cwd();
let mockedHomeDir = originalHomedir();

mock.module("node:os", () => ({
	homedir: () => mockedHomeDir,
	tmpdir,
}));

// Storage import — we keep references to the real implementations so we can
// toggle failure injection per test without replacing the whole module.
const sessionStorageReal = await import("../session/storage");

type AppendTurnFn = (session: Session, turn: Turn) => Promise<void>;
let appendTurnImpl: AppendTurnFn = sessionStorageReal.appendTurn;

mock.module("../session", async () => {
	const real = await import("../session/storage");
	return {
		...real,
		appendTurn: (session: Session, turn: Turn) => appendTurnImpl(session, turn),
	};
});

const sessionStorage = await import("../session");
const { KitAgent } = await import("./kit-agent");
const { AgentRuntime } = await import("./agent-runtime");
const transcript = await import("../shell/transcript/turns");

let tempRoot = "";
let homeDir = "";
let projectDir = "";

function makeUserMessage(text: string, extra?: Record<string, unknown>) {
	return {
		role: "user" as const,
		content: [{ type: "text" as const, text }],
		timestamp: Date.now(),
		...(extra ?? {}),
	};
}

function makeAssistantMessage(text: string) {
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
			cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
		},
		stopReason: "stop" as const,
		timestamp: Date.now(),
	};
}

beforeEach(async () => {
	tempRoot = await mkdtemp(path.join(tmpdir(), "kit-turn-persistence-test-"));
	homeDir = path.join(tempRoot, "home");
	projectDir = path.join(tempRoot, "project");
	await mkdir(homeDir, { recursive: true });
	await mkdir(projectDir, { recursive: true });
	process.env.HOME = homeDir;
	mockedHomeDir = homeDir;
	process.chdir(projectDir);
	// Reset failure injection between tests.
	appendTurnImpl = sessionStorageReal.appendTurn;
});

afterEach(async () => {
	process.chdir(originalCwd);
	if (originalHome === undefined) delete process.env.HOME;
	else process.env.HOME = originalHome;
	if (tempRoot) await rm(tempRoot, { recursive: true, force: true });
});

// -----------------------------------------------------------------------------
// 1. Turn assembly contract (KitAgent)
// -----------------------------------------------------------------------------

describe("KitAgent — turn assembly contract", () => {
	function drive(agent: InstanceType<typeof KitAgent>, event: unknown) {
		const events = (
			agent as unknown as {
				processPiEvent: (e: unknown) => unknown[];
				emit: (e: unknown) => void;
			}
		).processPiEvent(event);
		for (const e of events) {
			(agent as unknown as { emit: (e: unknown) => void }).emit(e);
		}
	}

	test("a fresh prompt produces a single turn containing user then assistant messages", () => {
		const agent = new KitAgent({});
		const user = makeUserMessage("/review");
		const assistant = makeAssistantMessage("review feedback");

		drive(agent, { type: "agent_start" });
		drive(agent, { type: "turn_start" });
		drive(agent, { type: "message_start", message: user });
		drive(agent, { type: "message_end", message: user });
		drive(agent, { type: "message_start", message: assistant });
		drive(agent, { type: "message_end", message: assistant });
		drive(agent, { type: "turn_end", message: assistant, toolResults: [] });
		drive(agent, { type: "agent_end", messages: [user, assistant] });

		expect(agent.turns).toHaveLength(1);
		expect(agent.turns[0]?.messages.map((m) => m.role)).toEqual([
			"user",
			"assistant",
		]);
	});

	test("after a turn ends, the next prompt creates a new turn (not appending to old)", () => {
		const agent = new KitAgent({});

		const userA = makeUserMessage("first");
		const assistantA = makeAssistantMessage("ack");
		drive(agent, { type: "agent_start" });
		drive(agent, { type: "turn_start" });
		drive(agent, { type: "message_start", message: userA });
		drive(agent, { type: "message_end", message: userA });
		drive(agent, { type: "message_start", message: assistantA });
		drive(agent, { type: "message_end", message: assistantA });
		drive(agent, { type: "turn_end", message: assistantA, toolResults: [] });
		drive(agent, { type: "agent_end", messages: [userA, assistantA] });

		const userB = makeUserMessage("/review");
		const assistantB = makeAssistantMessage("review feedback");
		drive(agent, { type: "agent_start" });
		drive(agent, { type: "turn_start" });
		drive(agent, { type: "message_start", message: userB });
		drive(agent, { type: "message_end", message: userB });
		drive(agent, { type: "message_start", message: assistantB });
		drive(agent, { type: "message_end", message: assistantB });
		drive(agent, { type: "turn_end", message: assistantB, toolResults: [] });
		drive(agent, { type: "agent_end", messages: [userB, assistantB] });

		expect(agent.turns).toHaveLength(2);
		expect(agent.turns[0]?.messages.map((m) => m.role)).toEqual([
			"user",
			"assistant",
		]);
		expect(agent.turns[1]?.messages.map((m) => m.role)).toEqual([
			"user",
			"assistant",
		]);
	});

	test("user message submitted via KitAgent.prompt() is recorded into the active turn before any provider events", async () => {
		const agent = new KitAgent({});
		(agent as unknown as { pi: { prompt: () => Promise<void> } }).pi.prompt =
			async () => {};

		const user = makeUserMessage("/review");
		await agent.prompt(user as unknown as AgentMessage);

		expect(agent.turns).toHaveLength(1);
		expect(agent.turns[0]?.messages.map((m) => m.role)).toEqual(["user"]);
	});
});

// -----------------------------------------------------------------------------
// 2. Persistence buffer contract (AgentRuntime)
// -----------------------------------------------------------------------------

type PersistenceTestRuntime = {
	session: Session;
	pendingAutoCompaction: null;
	persistenceQueue: string[];
	persistenceQueueSet: Set<string>;
	isPersistenceFlushInFlight: boolean;
	emitPersistenceFailure: (error: unknown) => void;
	subscribe: (
		listener: (event: { type: string; turn: Turn | null }) => void,
	) => () => void;
	registerPersistence: () => void;
};

function buildPersistenceRuntime(
	session: Session,
	options?: { onFailure?: (error: unknown) => void },
): {
	runtime: PersistenceTestRuntime;
	emitTurnCompleted: (turn: Turn) => void;
} {
	const runtime = Object.create(
		AgentRuntime.prototype,
	) as PersistenceTestRuntime;
	runtime.session = session;
	runtime.pendingAutoCompaction = null;
	runtime.persistenceQueue = [];
	runtime.persistenceQueueSet = new Set<string>();
	runtime.isPersistenceFlushInFlight = false;
	runtime.emitPersistenceFailure = options?.onFailure ?? (() => {});
	const listeners: Array<(event: { type: string; turn: Turn | null }) => void> =
		[];
	runtime.subscribe = (next) => {
		listeners.push(next);
		return () => {};
	};
	runtime.registerPersistence();
	const listener = listeners[0];
	if (!listener) throw new Error("persistence listener not registered");
	return {
		runtime,
		emitTurnCompleted: (turn: Turn) =>
			listener({ type: "agent.turn.completed", turn }),
	};
}

describe("AgentRuntime — persistence buffer contract", () => {
	test("multiple completed turns persist in the order they were submitted", async () => {
		const baseSession = await sessionStorage.createSession(
			projectDir,
			"claude-sonnet-4-6",
		);
		const turn1: Turn = {
			id: "turn-1",
			messages: [
				{ ...makeUserMessage("first"), turnId: "turn-1" },
				{ ...makeAssistantMessage("a1"), turnId: "turn-1" },
			],
		};
		const turn2: Turn = {
			id: "turn-2",
			messages: [
				{ ...makeUserMessage("/review"), turnId: "turn-2" },
				{ ...makeAssistantMessage("a2"), turnId: "turn-2" },
			],
		};

		const { emitTurnCompleted } = buildPersistenceRuntime({
			...baseSession,
			turns: [turn1, turn2],
		});

		emitTurnCompleted(turn1);
		emitTurnCompleted(turn2);
		await new Promise((resolve) => setTimeout(resolve, 25));

		const restored = await sessionStorage.readSession(baseSession.id);
		expect(restored?.turns.map((t) => t.id)).toEqual(["turn-1", "turn-2"]);
		expect(
			restored?.turns.flatMap((t) => t.messages.map((m) => m.role)),
		).toEqual(["user", "assistant", "user", "assistant"]);
	});

	test("persists every turn from a multi-turn agent run, not only the last one", async () => {
		// Real-world failure mode: a single user submission can produce many
		// turns when the assistant uses tools (each tool-loop iteration is a
		// separate pi turn). The user message lives in the FIRST turn; the
		// final assistant response lives in the LAST. If only the last turn
		// is persisted, the user message disappears from the JSONL.
		const baseSession = await sessionStorage.createSession(
			projectDir,
			"claude-sonnet-4-6",
		);
		const userTurn: Turn = {
			id: "turn-user",
			messages: [
				{
					...makeUserMessage("Please review the auth flow.", {
						synthetic: {
							kind: "prompt-command",
							command: "review",
						},
					}),
					turnId: "turn-user",
				},
				{ ...makeAssistantMessage("using tools"), turnId: "turn-user" },
			],
		};
		const toolTurn1: Turn = {
			id: "turn-tool-1",
			messages: [
				{ ...makeAssistantMessage("reading file"), turnId: "turn-tool-1" },
			],
		};
		const toolTurn2: Turn = {
			id: "turn-tool-2",
			messages: [
				{ ...makeAssistantMessage("final feedback"), turnId: "turn-tool-2" },
			],
		};

		const { emitTurnCompleted } = buildPersistenceRuntime({
			...baseSession,
			turns: [userTurn, toolTurn1, toolTurn2],
		});

		// agent.turn.completed fires once per agent run with the LAST turn.
		emitTurnCompleted(toolTurn2);
		await new Promise((resolve) => setTimeout(resolve, 25));

		const restored = await sessionStorage.readSession(baseSession.id);
		expect(restored?.turns.map((t) => t.id)).toEqual([
			"turn-user",
			"turn-tool-1",
			"turn-tool-2",
		]);
		const userMessages = restored?.turns
			.flatMap((t) => t.messages)
			.filter((m) => m.role === "user");
		expect(userMessages).toHaveLength(1);
	});

	test("if a turn append fails, the buffer keeps it and a later trigger persists it", async () => {
		const baseSession = await sessionStorage.createSession(
			projectDir,
			"claude-sonnet-4-6",
		);
		const turn1: Turn = {
			id: "turn-1",
			messages: [
				{ ...makeUserMessage("first"), turnId: "turn-1" },
				{ ...makeAssistantMessage("a1"), turnId: "turn-1" },
			],
		};
		const turn2: Turn = {
			id: "turn-2",
			messages: [
				{ ...makeUserMessage("/review"), turnId: "turn-2" },
				{ ...makeAssistantMessage("a2"), turnId: "turn-2" },
			],
		};

		let failNextAppend = true;
		appendTurnImpl = async (session, turn) => {
			if (failNextAppend) {
				failNextAppend = false;
				throw new Error("simulated disk failure");
			}
			return sessionStorageReal.appendTurn(session, turn);
		};

		const errors: string[] = [];
		const { emitTurnCompleted } = buildPersistenceRuntime(
			{ ...baseSession, turns: [turn1, turn2] },
			{
				onFailure: (error) => {
					errors.push(error instanceof Error ? error.message : String(error));
				},
			},
		);

		emitTurnCompleted(turn1);
		await new Promise((resolve) => setTimeout(resolve, 25));

		emitTurnCompleted(turn2);
		await new Promise((resolve) => setTimeout(resolve, 25));

		const restored = await sessionStorage.readSession(baseSession.id);
		expect(restored?.turns.map((t) => t.id)).toEqual(["turn-1", "turn-2"]);
		expect(errors).toEqual(["simulated disk failure"]);
	});

	test("persistence failure surfaces a session.persistence.failed event", async () => {
		const baseSession = await sessionStorage.createSession(
			projectDir,
			"claude-sonnet-4-6",
		);
		const turnX: Turn = {
			id: "turn-x",
			messages: [
				{ ...makeUserMessage("/review"), turnId: "turn-x" },
				{ ...makeAssistantMessage("ax"), turnId: "turn-x" },
			],
		};

		appendTurnImpl = async () => {
			throw new Error("boom");
		};

		const failureEvents: string[] = [];
		const { emitTurnCompleted } = buildPersistenceRuntime(
			{ ...baseSession, turns: [turnX] },
			{
				onFailure: (error) => {
					failureEvents.push(
						error instanceof Error ? error.message : String(error),
					);
				},
			},
		);

		emitTurnCompleted(turnX);
		await new Promise((resolve) => setTimeout(resolve, 25));

		expect(failureEvents).toEqual(["boom"]);
	});
});

// -----------------------------------------------------------------------------
// 3. Submission shape uniformity (AgentRuntime)
// -----------------------------------------------------------------------------

type SubmissionTestRuntime = {
	agent: {
		state: { isStreaming: boolean };
		prompt: (message: AgentMessage) => Promise<void>;
		followUp: (message: unknown) => void;
	};
	waitForRecovery: () => Promise<void>;
	syncPendingState: () => void;
	sendFollowUp: (text: string) => void;
	submitPromptCommandMessage: InstanceType<
		typeof AgentRuntime
	>["submitPromptCommandMessage"];
};

describe("AgentRuntime — submission shape uniformity", () => {
	test("submitPromptCommandMessage (idle) sends a structured user message with prompt-command synthetic metadata to KitAgent.prompt", async () => {
		let promptedMessage: AgentMessage | null = null;
		let queuedFollowUp: AgentMessage | string | null = null;

		const runtime = Object.create(
			AgentRuntime.prototype,
		) as SubmissionTestRuntime;
		runtime.agent = {
			state: { isStreaming: false },
			prompt: async (message) => {
				promptedMessage = message;
			},
			followUp: (message) => {
				queuedFollowUp = message as AgentMessage;
			},
		};
		runtime.waitForRecovery = async () => {};
		runtime.syncPendingState = () => {};
		runtime.sendFollowUp = (text) => {
			queuedFollowUp = text;
		};

		await runtime.submitPromptCommandMessage(
			"review",
			"auth flow",
			"Please review the auth flow.",
		);

		expect(queuedFollowUp).toBeNull();
		expect(promptedMessage).toMatchObject({
			role: "user",
			synthetic: {
				kind: "prompt-command",
				command: "review",
				args: "auth flow",
			},
		});
	});

	test("submitPromptCommandMessage (streaming) queues a structured user message preserving prompt-command synthetic metadata", async () => {
		let promptedMessage: AgentMessage | null = null;
		let queuedFollowUp: AgentMessage | string | null = null;

		const runtime = Object.create(
			AgentRuntime.prototype,
		) as SubmissionTestRuntime;
		runtime.agent = {
			state: { isStreaming: true },
			prompt: async (message) => {
				promptedMessage = message;
			},
			followUp: (message) => {
				queuedFollowUp = message as AgentMessage;
			},
		};
		runtime.waitForRecovery = async () => {};
		runtime.syncPendingState = () => {};
		runtime.sendFollowUp = (text) => {
			queuedFollowUp = text;
		};

		await runtime.submitPromptCommandMessage(
			"review",
			"auth flow",
			"Please review the auth flow.",
		);

		expect(promptedMessage).toBeNull();
		expect(queuedFollowUp).toMatchObject({
			role: "user",
			synthetic: {
				kind: "prompt-command",
				command: "review",
				args: "auth flow",
			},
		});
	});
});

// -----------------------------------------------------------------------------
// 4. Hydration contract (storage + transcript)
// -----------------------------------------------------------------------------

describe("Hydration — prompt-command identity round-trip", () => {
	test("a prompt-command user message is restored from JSONL with synthetic metadata intact", async () => {
		const session = await sessionStorage.createSession(
			projectDir,
			"claude-sonnet-4-6",
		);
		const turn: Turn = {
			id: "turn-pc",
			messages: [
				{
					...makeUserMessage("Please review the auth flow.", {
						synthetic: {
							kind: "prompt-command",
							command: "review",
							args: "auth flow",
						},
					}),
					turnId: "turn-pc",
				},
				{ ...makeAssistantMessage("review feedback"), turnId: "turn-pc" },
			],
		};

		await sessionStorage.appendTurn(session, turn);
		const restored = await sessionStorage.readSession(session.id);
		expect(restored?.turns).toHaveLength(1);
		const userMessage = restored?.turns[0]?.messages[0];
		expect(userMessage?.role).toBe("user");
		const synthetic = transcript.extractPromptCommandSynthetic(
			userMessage as never,
		);
		expect(synthetic).toEqual({
			kind: "prompt-command",
			command: "review",
			args: "auth flow",
		});
	});

	test("a prompt-command user message produces a user transcript item after hydration", async () => {
		const session = await sessionStorage.createSession(
			projectDir,
			"claude-sonnet-4-6",
		);
		const turn: Turn = {
			id: "turn-pc",
			messages: [
				{
					...makeUserMessage("Please review the auth flow.", {
						synthetic: {
							kind: "prompt-command",
							command: "review",
							args: "auth flow",
						},
					}),
					turnId: "turn-pc",
				},
				{ ...makeAssistantMessage("review feedback"), turnId: "turn-pc" },
			],
		};
		await sessionStorage.appendTurn(session, turn);

		const restored = await sessionStorage.readSession(session.id);
		const items = transcript.flattenTurnsToTranscriptItems(
			restored?.turns ?? [],
		);
		const userItems = items.filter((item) => item.kind === "user");
		expect(userItems).toHaveLength(1);
	});
});
