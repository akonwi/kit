import { describe, expect, test } from "bun:test";
import path from "node:path";

async function runMain(args: string[]): Promise<{
	exitCode: number;
	stderr: string;
	stdout: string;
}> {
	const proc = Bun.spawn(
		[process.execPath, path.join(import.meta.dir, "main.tsx"), ...args],
		{
			stdin: "pipe",
			stdout: "pipe",
			stderr: "pipe",
		},
	);
	proc.stdin.end();
	const [exitCode, stdout, stderr] = await Promise.all([
		proc.exited,
		new Response(proc.stdout).text(),
		new Response(proc.stderr).text(),
	]);
	return { exitCode, stdout, stderr };
}

describe("print mode CLI", () => {
	test("rejects options that conflict with print mode", async () => {
		const result = await runMain(["-p", "-v"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("cannot be combined");
	});

	test("rejects an empty prompt", async () => {
		const result = await runMain(["-p"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("Usage: kit -p");
	});

	test("rejects an invalid startup model selector", async () => {
		const result = await runMain(["-p", "--model", "model-1", "hello"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("--model expects <provider>/<model-id>");
	});

	test("rejects conflicting session options", async () => {
		const result = await runMain([
			"-p",
			"--no-session",
			"--session",
			"abc",
			"hello",
		]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain(
			"cannot combine --no-session with --session",
		);
	});

	test("accepts a session selector and reports a missing session", async () => {
		const sessionId = "00000000-0000-4000-8000-000000000000";
		const result = await runMain(["-p", "--session", sessionId, "hello"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain(`Session not found: ${sessionId}`);
	});
});

describe("RPC mode CLI", () => {
	test("rejects conflicting session options", async () => {
		const result = await runMain([
			"--mode",
			"rpc",
			"--no-session",
			"-s",
			"abc",
		]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain(
			"cannot combine --no-session with --session",
		);
	});

	test("rejects an invalid startup model selector", async () => {
		const result = await runMain(["--mode", "rpc", "--model", "model-1"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("--model expects <provider>/<model-id>");
	});

	test("rejects unknown modes", async () => {
		const result = await runMain(["--mode", "other"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("Unknown mode: other");
	});
});
