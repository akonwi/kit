import { afterEach, describe, expect, test } from "bun:test";
import {
	chmod,
	mkdir,
	mkdtemp,
	readFile,
	rm,
	utimes,
	writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import {
	getLocalServerPaths,
	readLocalServerRegistration,
} from "./local-server-files";

const homes: string[] = [];

async function runServerCommand(
	home: string,
	action: "run" | "start" | "status" | "stop" | "restart",
): Promise<{ exitCode: number; stdout: string; stderr: string }> {
	const process = Bun.spawn(
		[
			globalThis.process.execPath,
			path.join(import.meta.dir, "main.tsx"),
			"server",
			action,
		],
		{
			env: { ...globalThis.process.env, HOME: home },
			stdin: "ignore",
			stdout: "pipe",
			stderr: "pipe",
		},
	);
	const [exitCode, stdout, stderr] = await Promise.all([
		process.exited,
		new Response(process.stdout).text(),
		new Response(process.stderr).text(),
	]);
	return { exitCode, stdout, stderr };
}

afterEach(async () => {
	for (const home of homes.splice(0)) {
		await runServerCommand(home, "stop").catch(() => {});
		await rm(home, { recursive: true, force: true });
	}
});

describe("local server service", () => {
	test("serializes concurrent starts and enforces one daemon owner", async () => {
		const home = await mkdtemp(path.join(tmpdir(), "kit-server-service-"));
		homes.push(home);
		const [first, second] = await Promise.all([
			runServerCommand(home, "start"),
			runServerCommand(home, "start"),
		]);
		expect(first.exitCode).toBe(0);
		expect(second.exitCode).toBe(0);
		const firstUrl = first.stdout.match(/http:\/\/127\.0\.0\.1:\d+/)?.[0];
		const secondUrl = second.stdout.match(/http:\/\/127\.0\.0\.1:\d+/)?.[0];
		expect(firstUrl).toBeDefined();
		expect(secondUrl).toBe(firstUrl);

		const competingOwner = await runServerCommand(home, "run");
		expect(competingOwner.exitCode).toBe(1);
		expect(competingOwner.stderr).toContain("internal command");

		const status = await runServerCommand(home, "status");
		expect(status.exitCode).toBe(0);
		expect(status.stdout).toContain(firstUrl ?? "missing-url");
	}, 30_000);

	test("serializes concurrent reclamation of a dead owner", async () => {
		const home = await mkdtemp(path.join(tmpdir(), "kit-server-service-"));
		homes.push(home);
		const paths = getLocalServerPaths(path.join(home, ".kit"));
		await mkdir(paths.runDir, { recursive: true });
		await writeFile(
			paths.ownerLockPath,
			JSON.stringify({ pid: 2_147_483_647, token: "dead-owner" }),
		);

		const [first, second] = await Promise.all([
			runServerCommand(home, "start"),
			runServerCommand(home, "start"),
		]);
		expect(first.exitCode).toBe(0);
		expect(second.exitCode).toBe(0);
		const registration = await readLocalServerRegistration(paths);
		expect(first.stdout).toContain(registration?.url ?? "missing-url");
		expect(second.stdout).toContain(registration?.url ?? "missing-url");

		await runServerCommand(home, "stop");
		await writeFile(paths.ownerLockPath, "{partial", { mode: 0o600 });
		const recovered = await runServerCommand(home, "start");
		expect(recovered.exitCode).toBe(0);
	}, 30_000);

	test("does not replace a live daemon when its credential is temporarily unavailable", async () => {
		const home = await mkdtemp(path.join(tmpdir(), "kit-server-service-"));
		homes.push(home);
		const started = await runServerCommand(home, "start");
		expect(started.exitCode).toBe(0);
		const paths = getLocalServerPaths(path.join(home, ".kit"));
		const registration = await readLocalServerRegistration(paths);
		const password = await readFile(paths.passwordPath, "utf8");
		await rm(paths.passwordPath);
		await utimes(paths.ownerLockPath, new Date(0), new Date(0));

		const secondStart = await runServerCommand(home, "start");
		expect(secondStart.exitCode).not.toBe(0);
		expect(secondStart.stderr).toContain("running but unhealthy");
		expect(await readLocalServerRegistration(paths)).toEqual(registration);

		await writeFile(paths.passwordPath, password, { mode: 0o600 });
		await chmod(paths.passwordPath, 0o600);
		const stopped = await runServerCommand(home, "stop");
		expect(stopped.exitCode).toBe(0);
	}, 30_000);
});
