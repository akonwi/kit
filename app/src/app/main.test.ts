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
	test("rejects conflicting session options", async () => {
		const result = await runMain(["--web", "--no-session", "--session", "abc"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain(
			"cannot combine --no-session with --session",
		);
	});

	test("rejects an invalid port", async () => {
		const result = await runMain(["--web", "--port", "70000"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("expects an integer from 1 to 65535");
	});

	test("rejects invalid Basic auth credentials", async () => {
		const results = await Promise.all([
			runMain(["--web", "--auth", "missing-separator"]),
			runMain(["--web", "--auth", ":password"]),
			runMain(["--web", "--auth", "username:"]),
		]);
		for (const result of results) {
			expect(result.exitCode).toBe(1);
			expect(result.stdout).toBe("");
			expect(result.stderr).toContain("--auth expects <username>:<password>");
		}
	});

	test("rejects an invalid startup model selector", async () => {
		const result = await runMain(["--web", "--model", "model-1"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("--model expects <provider>/<model-id>");
	});
});

describe("interactive mode CLI", () => {
	test("rejects conflicting session options", async () => {
		const result = await runMain(["--no-session", "--session", "abc"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain(
			"cannot combine --no-session with --session",
		);
	});
});

describe("mode selection", () => {
	test("rejects every pair of headless modes", async () => {
		const results = await Promise.all([
			runMain(["--print", "--rpc", "hello"]),
			runMain(["--print", "--web", "hello"]),
			runMain(["--rpc", "--web"]),
		]);
		for (const result of results) {
			expect(result.exitCode).toBe(1);
			expect(result.stdout).toBe("");
			expect(result.stderr).toContain("mutually exclusive");
		}
	});

	test("rejects the removed --mode selector", async () => {
		const results = await Promise.all([
			runMain(["--mode", "rpc"]),
			runMain(["--mode", "web"]),
		]);
		for (const result of results) {
			expect(result.exitCode).toBe(1);
			expect(result.stdout).toBe("");
			expect(result.stderr).toContain("no longer supported");
		}
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
			expect(result.stderr).toContain("require --web");
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
