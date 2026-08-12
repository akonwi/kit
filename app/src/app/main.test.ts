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
});

describe("web mode CLI", () => {
	test("rejects ephemeral sessions", async () => {
		const result = await runMain(["--mode", "web", "--no-session"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("does not support --no-session");
	});

	test("rejects an invalid port", async () => {
		const result = await runMain(["--mode", "web", "--port", "70000"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("expects an integer from 1 to 65535");
	});

	test("rejects invalid Basic auth credentials", async () => {
		const results = await Promise.all([
			runMain(["--mode", "web", "--auth", "missing-separator"]),
			runMain(["--mode", "web", "--auth", ":password"]),
			runMain(["--mode", "web", "--auth", "username:"]),
		]);
		for (const result of results) {
			expect(result.exitCode).toBe(1);
			expect(result.stdout).toBe("");
			expect(result.stderr).toContain("--auth expects <username>:<password>");
		}
	});

	test("rejects an invalid startup model selector", async () => {
		const result = await runMain(["--mode", "web", "--model", "model-1"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("--model expects <provider>/<model-id>");
	});
});

describe("mode selection", () => {
	test("rejects print and RPC mode together", async () => {
		const result = await runMain(["--print", "--rpc", "hello"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("mutually exclusive");
	});

	test("rejects non-web uses of --mode", async () => {
		const result = await runMain(["--mode", "rpc"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("use --rpc");
	});

	test("rejects combining RPC and web mode selectors", async () => {
		const result = await runMain(["--rpc", "--mode", "web"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("cannot be combined");
	});
});

describe("RPC mode CLI", () => {
	test("rejects web-only options", async () => {
		const results = await Promise.all([
			runMain(["--rpc", "--port", "4782"]),
			runMain(["--rpc", "--auth", "user:password"]),
		]);
		for (const result of results) {
			expect(result.exitCode).toBe(1);
			expect(result.stdout).toBe("");
			expect(result.stderr).toContain("require --mode web");
		}
	});

	test("rejects conflicting session options", async () => {
		const result = await runMain(["--rpc", "--no-session", "-s", "abc"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain(
			"cannot combine --no-session with --session",
		);
	});

	test("rejects an invalid startup model selector", async () => {
		const result = await runMain(["--rpc", "--model", "model-1"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("--model expects <provider>/<model-id>");
	});
});
