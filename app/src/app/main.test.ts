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

	test("uses new as a session selector instead of prompt text", async () => {
		const result = await runMain(["new", "-p"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("Usage: kit -p");
	});

	test("keeps new as prompt text when it follows the print flag", async () => {
		const result = await runMain([
			"-p",
			"new",
			"--session",
			"missing-print-session",
		]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("Session not found: missing-print-session");
		expect(result.stderr).not.toContain("cannot combine with --session");
	});

	test("rejects new combined with an explicit session", async () => {
		const result = await runMain(["new", "-p", "hello", "--session", "abc"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("cannot combine with --session");
	});
});

describe("web mode CLI", () => {
	test("accepts new as a session selector", async () => {
		const result = await runMain(["new", "--web", "--port", "0"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("expects an integer from 1 to 65535");
		expect(result.stderr).not.toContain("positional arguments");
	});

	test("rejects new combined with an explicit session", async () => {
		const result = await runMain(["new", "--web", "--session", "abc"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("cannot combine with --session");
	});

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

	test("rejects an invalid canonical public URL", async () => {
		const results = await Promise.all([
			runMain(["--web", "--public-url", "ftp://kit.example.com"]),
			runMain(["--web", "--public-url", "https://kit.example.com/subpath"]),
		]);
		for (const result of results) {
			expect(result.exitCode).toBe(1);
			expect(result.stderr).toContain("--public-url expects an HTTP(S) origin");
		}
	});

	test("rejects an invalid startup model selector", async () => {
		const result = await runMain(["--web", "--model", "model-1"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("--model expects <provider>/<model-id>");
	});

	test("accepts a startup model selector for the browser TUI", async () => {
		const result = await runMain([
			"--web-tui",
			"--model",
			"openai/gpt-5.5",
			"--port",
			"0",
		]);
		expect(result.exitCode).toBe(1);
		expect(result.stderr).toContain(
			"--port expects an integer from 1 to 65535",
		);
		expect(result.stderr).not.toContain("does not support --model");
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
			runMain(["--rpc", "--web-tui"]),
			runMain(["--web", "--web-tui"]),
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
	test("accepts new as a session selector", async () => {
		const result = await runMain(["new", "--rpc", "--model", "invalid"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("--model expects <provider>/<model-id>");
		expect(result.stderr).not.toContain("positional arguments");
	});

	test("rejects new combined with an explicit session", async () => {
		const result = await runMain(["new", "--rpc", "--session", "abc"]);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("cannot combine with --session");
	});

	test("rejects web-only options", async () => {
		const results = await Promise.all([
			runMain(["--rpc", "--port", "4782"]),
			runMain(["--rpc", "--auth", "user:password"]),
			runMain(["--rpc", "--public-url", "https://kit.example.com"]),
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
