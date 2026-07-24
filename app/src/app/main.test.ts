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
