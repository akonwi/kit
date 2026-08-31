import { afterEach, describe, expect, test } from "bun:test";
import { mkdtemp, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { SESSION_VERSION, type Session } from "../session";
import { KitServer, type KitServerOptions } from "./kit-server";
import { LocalKitServer } from "./local-kit-server";
import {
	ensureLocalServerPassword,
	getLocalServerPaths,
	LOCAL_SERVER_USERNAME,
	readLocalServerRegistration,
} from "./local-server-files";
import { getLocalServerStatus } from "./local-server-service";

const temporaryDirectories: string[] = [];

afterEach(async () => {
	await Promise.all(
		temporaryDirectories
			.splice(0)
			.map((directory) => rm(directory, { recursive: true, force: true })),
	);
});

function createSession(id: string, cwd: string): Session {
	return {
		id,
		version: SESSION_VERSION,
		cwd,
		createdAt: "2026-01-01T00:00:00.000Z",
		updatedAt: "2026-01-01T00:00:00.000Z",
		turns: [],
	};
}

function createStorage() {
	const sessions = new Map<string, Session>();
	let nextId = 1;
	const storage: NonNullable<KitServerOptions["storage"]> = {
		createSession: async (cwd) => createSession(`session-${nextId++}`, cwd),
		listAllSessions: async () =>
			[...sessions.values()].map((session) => ({
				id: session.id,
				cwd: session.cwd,
				createdAt: session.createdAt,
				updatedAt: session.updatedAt,
				messageCount: 0,
			})),
		listSessionsForCwd: async (cwd) =>
			[...sessions.values()]
				.filter((session) => session.cwd === cwd)
				.map((session) => ({
					id: session.id,
					cwd: session.cwd,
					createdAt: session.createdAt,
					updatedAt: session.updatedAt,
					messageCount: 0,
				})),
		readSession: async (id) => sessions.get(id) ?? null,
		writeSession: async (session) => {
			sessions.set(session.id, session);
		},
	};
	return storage;
}

function authHeader(password: string): string {
	return `Basic ${Buffer.from(`${LOCAL_SERVER_USERNAME}:${password}`, "utf8").toString("base64")}`;
}

async function createServer() {
	const directory = await mkdtemp(path.join(tmpdir(), "kit-local-server-"));
	temporaryDirectories.push(directory);
	const paths = getLocalServerPaths(directory);
	const password = await ensureLocalServerPassword(paths);
	const server = new LocalKitServer(
		new KitServer("/default", { storage: createStorage() }),
		{ kitVersion: "1.2.3", password, paths },
	);
	const address = await server.start();
	return { address, password, paths, server };
}

describe("LocalKitServer", () => {
	test("registers an authenticated loopback control plane", async () => {
		const { address, password, paths, server } = await createServer();
		const registration = await readLocalServerRegistration(paths);
		expect(registration).toMatchObject({
			url: address.url,
			instanceId: address.instanceId,
			kitVersion: "1.2.3",
			protocolVersion: 1,
		});
		expect((await stat(paths.runDir)).mode & 0o777).toBe(0o700);
		expect((await stat(paths.passwordPath)).mode & 0o777).toBe(0o600);
		expect((await stat(paths.registryPath)).mode & 0o777).toBe(0o600);

		const unauthorized = await fetch(`${address.url}/api/health`);
		expect(unauthorized.status).toBe(401);
		const authorized = await fetch(`${address.url}/api/health`, {
			headers: { authorization: authHeader(password) },
		});
		expect(await authorized.json()).toMatchObject({
			ok: true,
			instanceId: address.instanceId,
			kitVersion: "1.2.3",
			protocolVersion: 1,
		});
		expect(await getLocalServerStatus("1.2.3", paths)).toMatchObject({
			state: "running",
		});
		expect(await getLocalServerStatus("2.0.0", paths)).toMatchObject({
			state: "incompatible",
		});
		await server.stop();
		expect(await readLocalServerRegistration(paths)).toBeNull();
	});

	test("lists and creates sessions through authenticated HTTP", async () => {
		const { address, password, server } = await createServer();
		const headers = {
			authorization: authHeader(password),
			"content-type": "application/json",
		};
		const created = await fetch(`${address.url}/api/sessions`, {
			method: "POST",
			headers,
			body: JSON.stringify({ cwd: "/workspace" }),
		});
		expect(created.status).toBe(201);
		expect(await created.json()).toMatchObject({
			session: { id: "session-1", cwd: "/workspace" },
		});

		const listed = await fetch(
			`${address.url}/api/sessions?cwd=${encodeURIComponent("/workspace")}`,
			{ headers },
		);
		expect(await listed.json()).toEqual({
			sessions: [
				expect.objectContaining({ id: "session-1", cwd: "/workspace" }),
			],
		});
		await server.stop();
	});

	test("stops only when shutdown targets the registered instance", async () => {
		const { address, password, paths, server } = await createServer();
		const rejected = await fetch(`${address.url}/api/shutdown`, {
			method: "POST",
			headers: {
				authorization: authHeader(password),
				"x-kit-instance-id": "stale-instance",
			},
		});
		expect(rejected.status).toBe(409);
		expect(await getLocalServerStatus("1.2.3", paths)).toMatchObject({
			state: "running",
		});
		const response = await fetch(`${address.url}/api/shutdown`, {
			method: "POST",
			headers: {
				authorization: authHeader(password),
				"x-kit-instance-id": address.instanceId,
			},
		});
		expect(response.status).toBe(200);
		await server.waitForStop();
		expect(await readLocalServerRegistration(paths)).toBeNull();
	});
});
