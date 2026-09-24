import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { closeSync, openSync } from "node:fs";
import { link, open, readFile, rm, stat } from "node:fs/promises";
import path from "node:path";
import { safeProcessCwd } from "../process-cwd";
import { replaceFileAtomically, withFileLock } from "../storage/atomic-file";
import { KitServer } from "./kit-server";
import { LocalKitServer } from "./local-kit-server";
import {
	ensureLocalServerPassword,
	ensureLocalServerRunDir,
	getLocalServerPaths,
	LOCAL_SERVER_PROTOCOL_VERSION,
	LOCAL_SERVER_USERNAME,
	type LocalServerPaths,
	type LocalServerRegistration,
	readLocalServerPassword,
	readLocalServerRegistration,
	removeLocalServerRegistration,
} from "./local-server-files";

type LocalServerHealth = {
	pid: number;
	instanceId: string;
	kitVersion: string;
	protocolVersion: number;
	startedAt: string;
};

export type LocalServerStatus =
	| { state: "stopped" }
	| { state: "unreachable"; registration: LocalServerRegistration }
	| {
			state: "incompatible";
			registration: LocalServerRegistration;
			health: LocalServerHealth;
	  }
	| {
			state: "running";
			registration: LocalServerRegistration;
			health: LocalServerHealth;
	  };

type OwnerRecord = {
	pid: number;
	token: string;
};

function authorization(password: string): string {
	return `Basic ${Buffer.from(`${LOCAL_SERVER_USERNAME}:${password}`, "utf8").toString("base64")}`;
}

function parseHealth(value: unknown): LocalServerHealth | null {
	if (typeof value !== "object" || value === null || Array.isArray(value)) {
		return null;
	}
	const record = value as Record<string, unknown>;
	if (
		record.ok !== true ||
		typeof record.pid !== "number" ||
		!Number.isSafeInteger(record.pid) ||
		record.pid < 1 ||
		typeof record.instanceId !== "string" ||
		!record.instanceId ||
		typeof record.kitVersion !== "string" ||
		!record.kitVersion ||
		typeof record.protocolVersion !== "number" ||
		!Number.isSafeInteger(record.protocolVersion) ||
		typeof record.startedAt !== "string"
	) {
		return null;
	}
	return record as LocalServerHealth;
}

async function probe(
	registration: LocalServerRegistration,
	password: string,
): Promise<LocalServerHealth | null> {
	try {
		const response = await fetch(`${registration.url}/api/health`, {
			headers: { authorization: authorization(password) },
			signal: AbortSignal.timeout(1_000),
		});
		if (!response.ok) return null;
		const health = parseHealth(await response.json());
		return health?.instanceId === registration.instanceId &&
			health.pid === registration.pid &&
			health.startedAt === registration.startedAt
			? health
			: null;
	} catch {
		return null;
	}
}

async function readOwner(paths: LocalServerPaths): Promise<OwnerRecord | null> {
	try {
		const value: unknown = JSON.parse(
			await readFile(paths.ownerLockPath, "utf8"),
		);
		if (typeof value !== "object" || value === null || Array.isArray(value)) {
			return null;
		}
		const record = value as Record<string, unknown>;
		return typeof record.pid === "number" &&
			Number.isSafeInteger(record.pid) &&
			record.pid > 0 &&
			typeof record.token === "string" &&
			record.token
			? { pid: record.pid, token: record.token }
			: null;
	} catch {
		return null;
	}
}

function processIsAlive(pid: number): boolean {
	try {
		process.kill(pid, 0);
		return true;
	} catch (error) {
		return !(
			error instanceof Error &&
			"code" in error &&
			error.code === "ESRCH"
		);
	}
}

async function ownerIsLocked(paths: LocalServerPaths): Promise<boolean> {
	try {
		await stat(paths.ownerLockPath);
	} catch (error) {
		if (error instanceof Error && "code" in error && error.code === "ENOENT") {
			return false;
		}
		throw error;
	}
	const owner = await readOwner(paths);
	if (owner && processIsAlive(owner.pid)) return true;
	await rm(paths.ownerLockPath, { force: true });
	return false;
}

async function createOwnerClaim(
	paths: LocalServerPaths,
	owner: OwnerRecord,
): Promise<void> {
	const temporaryPath = `${paths.ownerLockPath}.${owner.token}.tmp`;
	try {
		const handle = await open(temporaryPath, "wx", 0o600);
		try {
			await handle.writeFile(`${JSON.stringify(owner)}\n`, "utf8");
			await handle.sync();
		} finally {
			await handle.close();
		}
		await link(temporaryPath, paths.ownerLockPath);
	} finally {
		await rm(temporaryPath, { force: true }).catch(() => {});
	}
}

async function updateOwnerClaim(
	paths: LocalServerPaths,
	token: string,
	pid: number,
): Promise<void> {
	const current = await readOwner(paths);
	if (current?.token !== token) {
		throw new Error("Local server ownership changed during startup");
	}
	await replaceFileAtomically(
		paths.ownerLockPath,
		`${JSON.stringify({ pid, token })}\n`,
		{ mode: 0o600 },
	);
}

async function waitForOwnership(
	paths: LocalServerPaths,
	token: string,
): Promise<boolean> {
	const deadline = Date.now() + 5_000;
	while (Date.now() < deadline) {
		const owner = await readOwner(paths);
		if (owner?.pid === process.pid && owner.token === token) return true;
		await Bun.sleep(10);
	}
	return false;
}

async function releaseOwnership(
	paths: LocalServerPaths,
	token: string,
): Promise<void> {
	const owner = await readOwner(paths);
	if (owner?.token !== token) return;
	await rm(paths.ownerLockPath, { force: true });
}

async function withServiceOperation<T>(
	paths: LocalServerPaths,
	operation: () => Promise<T>,
): Promise<T> {
	await ensureLocalServerRunDir(paths);
	return withFileLock(paths.operationLockPath, operation);
}

export async function getLocalServerStatus(
	kitVersion: string,
	paths = getLocalServerPaths(),
): Promise<LocalServerStatus> {
	const registration = await readLocalServerRegistration(paths);
	if (!registration) return { state: "stopped" };
	const password = await readLocalServerPassword(paths);
	if (!password) return { state: "unreachable", registration };
	const health = await probe(registration, password);
	if (!health) return { state: "unreachable", registration };
	if (
		health.kitVersion !== kitVersion ||
		health.protocolVersion !== LOCAL_SERVER_PROTOCOL_VERSION
	) {
		return { state: "incompatible", registration, health };
	}
	return { state: "running", registration, health };
}

function daemonCommand(): string[] {
	const executable = process.execPath;
	const name = path.basename(executable).toLowerCase();
	if (name === "bun" || name.startsWith("bun-")) {
		const entrypoint = process.argv[1];
		if (!entrypoint) throw new Error("Cannot determine Kit entrypoint");
		return [executable, entrypoint, "server", "run"];
	}
	return [executable, "server", "run"];
}

async function launchDetachedServer(paths: LocalServerPaths): Promise<void> {
	const token = randomUUID();
	await createOwnerClaim(paths, { pid: process.pid, token });
	let log: number | null = null;
	let child: ReturnType<typeof spawn> | null = null;
	try {
		log = openSync(paths.logPath, "a", 0o600);
		const [command, ...args] = daemonCommand();
		child = await new Promise<ReturnType<typeof spawn>>((resolve, reject) => {
			const candidate = spawn(command, args, {
				detached: true,
				env: {
					...process.env,
					KIT_LOCAL_SERVER_OWNER_TOKEN: token,
				},
				stdio: ["ignore", log, log],
				windowsHide: true,
			});
			candidate.once("error", reject);
			candidate.once("spawn", () => resolve(candidate));
		});
		if (child.pid === undefined) {
			throw new Error("Local server process did not report a PID");
		}
		await updateOwnerClaim(paths, token, child.pid);
		child.unref();
	} catch (error) {
		child?.kill();
		await releaseOwnership(paths, token).catch(() => {});
		throw error;
	} finally {
		if (log !== null) closeSync(log);
	}
}

async function waitForServer(
	kitVersion: string,
	paths: LocalServerPaths,
): Promise<LocalServerRegistration> {
	const deadline = Date.now() + 10_000;
	while (Date.now() < deadline) {
		const status = await getLocalServerStatus(kitVersion, paths);
		if (status.state === "running") return status.registration;
		if (status.state === "incompatible") {
			throw new Error(
				`Local server version ${status.health.kitVersion} is incompatible with Kit ${kitVersion}`,
			);
		}
		await Bun.sleep(50);
	}
	throw new Error(
		`Local server did not become ready; inspect ${paths.logPath}`,
	);
}

async function startLocalServerUnlocked(
	kitVersion: string,
	paths: LocalServerPaths,
): Promise<LocalServerRegistration> {
	const status = await getLocalServerStatus(kitVersion, paths);
	if (status.state === "running") return status.registration;
	if (status.state === "incompatible") {
		throw new Error(
			`Local server version ${status.health.kitVersion} is already running; restart it before using Kit ${kitVersion}`,
		);
	}
	if (await ownerIsLocked(paths)) {
		if (status.state === "stopped") {
			return waitForServer(kitVersion, paths);
		}
		throw new Error(
			`The registered local server is running but unhealthy; inspect ${paths.logPath}`,
		);
	}
	await removeLocalServerRegistration(paths);
	await launchDetachedServer(paths);
	return waitForServer(kitVersion, paths);
}

export async function startLocalServer(
	kitVersion: string,
	paths = getLocalServerPaths(),
): Promise<LocalServerRegistration> {
	return withServiceOperation(paths, () =>
		startLocalServerUnlocked(kitVersion, paths),
	);
}

async function stopLocalServerUnlocked(
	paths: LocalServerPaths,
): Promise<boolean> {
	const registration = await readLocalServerRegistration(paths);
	const password = await readLocalServerPassword(paths);
	const owned = await ownerIsLocked(paths);
	if (!registration || !password) {
		if (owned) {
			throw new Error(
				"The local server is starting or its registration is unhealthy",
			);
		}
		if (registration) await removeLocalServerRegistration(paths);
		return false;
	}
	const health = await probe(registration, password);
	if (!health) {
		if (owned) {
			throw new Error(
				"The registered local server is running but did not pass its health check",
			);
		}
		await removeLocalServerRegistration(paths, registration.instanceId);
		return false;
	}
	let response: Response;
	try {
		response = await fetch(`${registration.url}/api/shutdown`, {
			method: "POST",
			headers: {
				authorization: authorization(password),
				"x-kit-instance-id": registration.instanceId,
			},
			signal: AbortSignal.timeout(2_000),
		});
	} catch (error) {
		throw new Error("Local server shutdown request failed", { cause: error });
	}
	if (!response.ok) {
		throw new Error(
			`Local server rejected shutdown with HTTP ${response.status}`,
		);
	}
	const deadline = Date.now() + 5_000;
	while (Date.now() < deadline) {
		const current = await readLocalServerRegistration(paths);
		if (
			current?.instanceId !== registration.instanceId &&
			!(await ownerIsLocked(paths))
		) {
			return true;
		}
		await Bun.sleep(50);
	}
	throw new Error("Local server did not stop");
}

export async function stopLocalServer(
	paths = getLocalServerPaths(),
): Promise<boolean> {
	return withServiceOperation(paths, () => stopLocalServerUnlocked(paths));
}

export async function runLocalServerDaemon(
	kitVersion: string,
	paths = getLocalServerPaths(),
): Promise<number> {
	await ensureLocalServerRunDir(paths);
	const ownerToken = process.env.KIT_LOCAL_SERVER_OWNER_TOKEN;
	if (!ownerToken) {
		console.error(
			"kit server run is an internal command; use kit server start",
		);
		return 1;
	}
	if (!(await waitForOwnership(paths, ownerToken))) {
		console.error("Local server ownership registration did not complete");
		return 1;
	}
	try {
		const existing = await getLocalServerStatus(kitVersion, paths);
		if (existing.state === "running" || existing.state === "incompatible") {
			console.error(
				`Kit local server is already running at ${existing.registration.url}`,
			);
			return 1;
		}
		if (existing.state === "unreachable") {
			await removeLocalServerRegistration(
				paths,
				existing.registration.instanceId,
			);
		}
		const password = await ensureLocalServerPassword(paths);
		const server = new LocalKitServer(new KitServer(safeProcessCwd()), {
			kitVersion,
			password,
			paths,
		});
		const address = await server.start();
		console.log(`Kit local server listening at ${address.url}`);
		let stopping = false;
		const stop = () => {
			if (stopping) return;
			stopping = true;
			void server.stop().catch((error) => {
				console.error(
					`Local server shutdown failed: ${error instanceof Error ? error.message : String(error)}`,
				);
			});
		};
		process.once("SIGINT", stop);
		process.once("SIGTERM", stop);
		try {
			await server.waitForStop();
			return 0;
		} finally {
			process.off("SIGINT", stop);
			process.off("SIGTERM", stop);
		}
	} finally {
		await releaseOwnership(paths, ownerToken);
	}
}

export async function runLocalServerCommand(
	action: string | undefined,
	kitVersion: string,
): Promise<number> {
	const paths = getLocalServerPaths();
	switch (action) {
		case "run":
			return runLocalServerDaemon(kitVersion, paths);
		case "start": {
			const registration = await startLocalServer(kitVersion, paths);
			console.log(`Kit local server running at ${registration.url}`);
			return 0;
		}
		case "stop": {
			const stopped = await stopLocalServer(paths);
			console.log(
				stopped
					? "Kit local server stopped"
					: "Kit local server is not running",
			);
			return 0;
		}
		case "restart": {
			const registration = await withServiceOperation(paths, async () => {
				await stopLocalServerUnlocked(paths);
				return startLocalServerUnlocked(kitVersion, paths);
			});
			console.log(`Kit local server running at ${registration.url}`);
			return 0;
		}
		case "status":
		case undefined: {
			const status = await getLocalServerStatus(kitVersion, paths);
			if (status.state === "running") {
				console.log(
					`Kit local server running at ${status.registration.url} (pid ${status.health.pid})`,
				);
				return 0;
			}
			if (status.state === "incompatible") {
				console.log(
					`Kit local server ${status.health.kitVersion} is incompatible with Kit ${kitVersion}`,
				);
				return 2;
			}
			if (status.state === "unreachable") {
				console.log(
					`Kit local server is registered but unreachable (pid ${status.registration.pid})`,
				);
				return 2;
			}
			console.log("Kit local server is not running");
			return 1;
		}
		default:
			console.error("Usage: kit server [start|status|stop|restart]");
			return 1;
	}
}
