import { describe, expect, test } from "bun:test";
import type { RpcCommand, RpcConnectionSnapshot } from "@akonwi/kit-protocol";
import { SESSION_VERSION, type Session } from "../session";
import { KitServer, type KitServerOptions } from "./kit-server";

function session(id = "session-1", cwd = "/workspace"): Session {
	return {
		id,
		version: SESSION_VERSION,
		cwd,
		createdAt: "2026-01-01T00:00:00.000Z",
		updatedAt: "2026-01-01T00:00:00.000Z",
		turns: [],
	};
}

function createStorage(initial: Session[] = []) {
	const sessions = new Map(initial.map((value) => [value.id, value]));
	const writes: Session[] = [];
	let nextId = 1;
	const storage: NonNullable<KitServerOptions["storage"]> = {
		createSession: async (cwd) => session(`created-${nextId++}`, cwd),
		listAllSessions: async () =>
			[...sessions.values()].map((value) => ({
				id: value.id,
				cwd: value.cwd,
				createdAt: value.createdAt,
				updatedAt: value.updatedAt,
				messageCount: 0,
			})),
		listSessionsForCwd: async (cwd) =>
			[...sessions.values()]
				.filter((value) => value.cwd === cwd)
				.map((value) => ({
					id: value.id,
					cwd: value.cwd,
					createdAt: value.createdAt,
					updatedAt: value.updatedAt,
					messageCount: 0,
				})),
		readSession: async (id) => sessions.get(id) ?? null,
		writeSession: async (value) => {
			writes.push(value);
			sessions.set(value.id, value);
		},
	};
	return { storage, writes };
}

function createRunningSession() {
	const commands: RpcCommand[] = [];
	const listeners = new Set<(record: unknown) => void>();
	let disposeCount = 0;
	let abortCount = 0;
	const snapshot: RpcConnectionSnapshot = {
		state: { sessionId: "session-1" },
		messages: [],
		messageOffset: 0,
		totalMessageCount: 0,
		pendingInteractions: [],
		pendingInteractionGeneration: 0,
	};
	return {
		commands,
		listeners,
		get abortCount() {
			return abortCount;
		},
		get disposeCount() {
			return disposeCount;
		},
		running: {
			rpc: {
				abortAndWait: async () => {
					abortCount += 1;
				},
				connectClient: (_listener: (record: unknown) => void) => () => {},
				dispose: () => {},
				getConnectionSnapshot: () => snapshot,
				handleCommand: async (
					command: RpcCommand,
					respond: (record: unknown) => Promise<void>,
				) => {
					commands.push(command);
					await respond({
						type: "response",
						command: command.type,
						success: true,
					});
				},
				subscribe: (listener: (record: unknown) => void) => {
					listeners.add(listener);
					return () => listeners.delete(listener);
				},
			},
			dispose: async () => {
				disposeCount += 1;
			},
		},
	};
}

describe("KitServer", () => {
	test("lists and creates persistent sessions without starting a runtime", async () => {
		const existing = session();
		const { storage, writes } = createStorage([existing]);
		let starts = 0;
		const server = new KitServer("/default", {
			storage,
			startSession: async () => {
				starts += 1;
				return createRunningSession().running;
			},
		});

		expect(await server.listSessions("/workspace")).toHaveLength(1);
		const created = await server.createSession();
		expect(created).toMatchObject({ id: "created-1", cwd: "/default" });
		expect(writes.map((value) => value.id)).toEqual(["created-1"]);
		expect(starts).toBe(0);
		await server.dispose();
	});

	test("starts one authoritative runtime for concurrent connections", async () => {
		const stored = session();
		const { storage } = createStorage([stored]);
		const resource = createRunningSession();
		let starts = 0;
		const server = new KitServer(stored.cwd, {
			storage,
			startSession: async () => {
				starts += 1;
				return resource.running;
			},
		});

		const [first, second] = await Promise.all([
			server.connectSession(stored.id),
			server.connectSession(stored.id),
		]);
		expect(starts).toBe(1);
		expect(first).not.toBe(second);
		expect(first.sessionId).toBe(stored.id);
		expect(second.sessionId).toBe(stored.id);

		first.close();
		expect(resource.disposeCount).toBe(0);
		await server.dispose();
		expect(resource.disposeCount).toBe(1);
		expect(() => second.getConnectionSnapshot()).toThrow("closed");
	});

	test("rejects a second in-process runtime until runtime globals are session-scoped", async () => {
		const firstSession = session("session-1", "/workspace/one");
		const secondSession = session("session-2", "/workspace/two");
		const { storage } = createStorage([firstSession, secondSession]);
		const firstResource = createRunningSession();
		let starts = 0;
		const server = new KitServer(firstSession.cwd, {
			storage,
			startSession: async () => {
				starts += 1;
				return firstResource.running;
			},
		});

		await server.connectSession(firstSession.id);
		await expect(server.connectSession(secondSession.id)).rejects.toThrow(
			"session-scoped runtime globals",
		);
		expect(starts).toBe(1);
		await server.dispose();
	});

	test("removes an RPC subscription when initial client synchronization throws", async () => {
		const stored = session();
		const { storage } = createStorage([stored]);
		const resource = createRunningSession();
		resource.running.rpc.connectClient = (listener) => {
			listener({ type: "ui_snapshot" });
			return () => {};
		};
		const server = new KitServer(stored.cwd, {
			storage,
			startSession: async () => resource.running,
		});
		const connection = await server.connectSession(stored.id);

		expect(() =>
			connection.subscribe(() => {
				throw new Error("sync failed");
			}),
		).toThrow("sync failed");
		expect(resource.listeners.size).toBe(0);
		await server.dispose();
	});

	test("keeps a connection bound to its original session", async () => {
		const stored = session();
		const { storage } = createStorage([stored]);
		const resource = createRunningSession();
		const server = new KitServer(stored.cwd, {
			storage,
			startSession: async () => resource.running,
		});
		const connection = await server.connectSession(stored.id);
		const responses: unknown[] = [];

		await connection.handleCommand(
			{ id: "request-1", type: "switch_session", sessionId: "session-2" },
			async (response) => {
				responses.push(response);
			},
		);
		expect(resource.commands).toEqual([]);
		expect(responses).toEqual([
			{
				id: "request-1",
				type: "response",
				command: "switch_session",
				success: false,
				error: "Command is unavailable on a bound session",
			},
		]);

		await connection.handleCommand({ type: "abort" }, async () => {});
		expect(resource.commands).toEqual([{ type: "abort" }]);
		await server.dispose();
	});

	test("waits for session creation that committed during shutdown", async () => {
		const { storage } = createStorage();
		let releaseWrite: (() => void) | undefined;
		const writeReleased = new Promise<void>((resolve) => {
			releaseWrite = resolve;
		});
		let signalWrite: (() => void) | undefined;
		const writeStarted = new Promise<void>((resolve) => {
			signalWrite = resolve;
		});
		storage.writeSession = async () => {
			signalWrite?.();
			await writeReleased;
		};
		const server = new KitServer("/workspace", { storage });
		const creating = server.createSession();
		await writeStarted;
		let disposed = false;
		const disposing = server.dispose().then(() => {
			disposed = true;
		});
		await Promise.resolve();
		expect(disposed).toBe(false);
		releaseWrite?.();

		await expect(creating).resolves.toMatchObject({ id: "created-1" });
		await disposing;
		expect(disposed).toBe(true);
	});

	test("returns the same cleanup promise to concurrent disposal callers", async () => {
		const server = new KitServer("/workspace", {
			storage: createStorage().storage,
		});
		const first = server.dispose();
		const second = server.dispose();
		expect(first).toBe(second);
		await first;
	});

	test("rejects new work and disposes a session that finishes starting during shutdown", async () => {
		const stored = session();
		const { storage } = createStorage([stored]);
		const resource = createRunningSession();
		let signalStart: (() => void) | undefined;
		const started = new Promise<void>((resolve) => {
			signalStart = resolve;
		});
		let finishStart: ((value: typeof resource.running) => void) | undefined;
		const server = new KitServer(stored.cwd, {
			storage,
			startSession: () => {
				signalStart?.();
				return new Promise((resolve) => {
					finishStart = resolve;
				});
			},
		});
		const connecting = server.connectSession(stored.id);
		await started;
		const disposing = server.dispose();
		finishStart?.(resource.running);

		await expect(connecting).rejects.toThrow("disposed");
		await disposing;
		expect(resource.disposeCount).toBe(1);
		await expect(server.createSession()).rejects.toThrow("disposed");
	});

	test("treats an abort-aware startup rejection as normal shutdown", async () => {
		const stored = session();
		const { storage } = createStorage([stored]);
		let signalStart: (() => void) | undefined;
		const started = new Promise<void>((resolve) => {
			signalStart = resolve;
		});
		const server = new KitServer(stored.cwd, {
			storage,
			startSession: async (_session, signal) => {
				signalStart?.();
				return await new Promise((_resolve, reject) => {
					signal.addEventListener("abort", () => reject(signal.reason), {
						once: true,
					});
				});
			},
		});
		const connecting = server.connectSession(stored.id);
		const connectionResult = Promise.allSettled([connecting]);
		await started;

		await expect(server.dispose()).resolves.toBeUndefined();
		expect(await connectionResult).toEqual([
			expect.objectContaining({
				status: "rejected",
				reason: expect.objectContaining({
					message: expect.stringContaining("disposed"),
				}),
			}),
		]);
	});

	test("reports cleanup failures from a session that finishes starting during shutdown", async () => {
		const stored = session();
		const { storage } = createStorage([stored]);
		const resource = createRunningSession();
		resource.running.dispose = async () => {
			throw new Error("startup cleanup failed");
		};
		let signalStart: (() => void) | undefined;
		const started = new Promise<void>((resolve) => {
			signalStart = resolve;
		});
		let finishStart: ((value: typeof resource.running) => void) | undefined;
		const server = new KitServer(stored.cwd, {
			storage,
			startSession: () => {
				signalStart?.();
				return new Promise((resolve) => {
					finishStart = resolve;
				});
			},
		});
		const connecting = server.connectSession(stored.id);
		await started;
		const disposing = server.dispose();
		const settled = Promise.allSettled([connecting, disposing]);
		finishStart?.(resource.running);

		const results = await settled;
		expect(results).toEqual([
			expect.objectContaining({
				status: "rejected",
				reason: expect.objectContaining({ message: "startup cleanup failed" }),
			}),
			expect.objectContaining({
				status: "rejected",
				reason: expect.objectContaining({ message: "startup cleanup failed" }),
			}),
		]);
	});
});
