// Manual authenticated end-to-end verification for `kit -p`. Keep this matrix
// aligned with the lifecycle and output guarantees in app/src/app/one-shot.ts.
import { existsSync } from "node:fs";
import { mkdir, mkdtemp, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

const repoRoot = path.resolve(import.meta.dir, "../..");
const probePath = path.join(
	repoRoot,
	".kit",
	"plugins",
	"headless-one-shot-smoke.ts",
);
const subagentPath = path.join(
	repoRoot,
	".kit",
	"agents",
	"headless-one-shot-smoke.md",
);
const tempDir = await mkdtemp(path.join(tmpdir(), "kit-one-shot-smoke-"));
const readFixture = path.join(tempDir, "read-fixture.txt");
const pluginMarker = path.join(tempDir, "external-plugin-loaded");

for (const fixturePath of [probePath, subagentPath]) {
	if (existsSync(fixturePath)) {
		throw new Error(
			`Refusing to overwrite existing smoke fixture: ${fixturePath}`,
		);
	}
}

async function filesBelow(root: string): Promise<Set<string>> {
	if (!existsSync(root)) return new Set();
	const files = new Set<string>();
	async function visit(directory: string): Promise<void> {
		for (const entry of await readdir(directory, { withFileTypes: true })) {
			const filePath = path.join(directory, entry.name);
			if (entry.isDirectory()) await visit(filePath);
			else files.add(filePath);
		}
	}
	await visit(root);
	return files;
}

type RunResult = {
	exitCode: number;
	stderr: string;
	stdout: string;
};

async function runOneShot(
	prompt: string,
	options: { stdin?: string } = {},
): Promise<RunResult> {
	const proc = Bun.spawn([process.execPath, "dev", "-p", prompt], {
		cwd: repoRoot,
		env: process.env,
		stdin: options.stdin === undefined ? "ignore" : new Blob([options.stdin]),
		stdout: "pipe",
		stderr: "pipe",
	});
	const [exitCode, stdout, stderr] = await Promise.all([
		proc.exited,
		new Response(proc.stdout).text(),
		new Response(proc.stderr).text(),
	]);
	return { exitCode, stdout: stdout.trimEnd(), stderr };
}

async function expectExact(
	name: string,
	expected: string,
	prompt: string,
	options?: Parameters<typeof runOneShot>[1],
): Promise<void> {
	const result = await runOneShot(prompt, options);
	if (result.exitCode !== 0 || result.stdout !== expected) {
		throw new Error(
			`${name} failed: exit=${result.exitCode}, expected=${JSON.stringify(expected)}, actual=${JSON.stringify(result.stdout)}\nstderr:\n${result.stderr}`,
		);
	}
	console.log(`PASS ${name}: ${expected}`);
}

async function testSignalHandling(): Promise<void> {
	const preload = path.join(
		repoRoot,
		"app/node_modules/@opentui/solid/scripts/preload.js",
	);
	const main = path.join(repoRoot, "app/src/app/main.tsx");
	const proc = Bun.spawn(
		[
			process.execPath,
			`--preload=${preload}`,
			main,
			"-p",
			"Use the bash tool to execute sleep 30, then reply with exactly LATE.",
		],
		{
			cwd: repoRoot,
			env: process.env,
			stdin: "ignore",
			stdout: "pipe",
			stderr: "pipe",
		},
	);
	const earlyExit = await Promise.race([
		proc.exited.then((exitCode) => exitCode),
		Bun.sleep(500).then(() => null),
	]);
	if (earlyExit !== null) {
		throw new Error(`SIGINT fixture exited early with code ${earlyExit}`);
	}
	proc.kill("SIGINT");
	const exitCode = await proc.exited;
	const [stdout, stderr] = await Promise.all([
		new Response(proc.stdout).text(),
		new Response(proc.stderr).text(),
	]);
	if (exitCode !== 130 || stdout !== "") {
		throw new Error(
			`SIGINT exited ${exitCode} with stdout=${JSON.stringify(stdout)}, expected exit 130 and empty stdout\nstderr:\n${stderr}`,
		);
	}
	console.log("PASS SIGINT: direct process exited 130 with empty stdout");
}

const skippedPluginSource = `import { writeFileSync } from "node:fs";

export default function ExternalPluginProbe() {
  writeFileSync(${JSON.stringify(pluginMarker)}, "loaded");
}
`;
const subagentSource = `---
name: headless-one-shot-smoke
description: Smoke-test subagent
---
Follow the caller's request and return only the exact token it asks for.
`;

try {
	await writeFile(readFixture, "READ_TOOL_OK\n");
	await mkdir(path.dirname(probePath), { recursive: true });
	await mkdir(path.dirname(subagentPath), { recursive: true });
	await writeFile(probePath, skippedPluginSource);
	await writeFile(subagentPath, subagentSource);

	const sessionsRoot = path.join(process.env.HOME ?? "", ".kit", "sessions");
	const sessionsBefore = await filesBelow(sessionsRoot);

	await expectExact(
		"plain prompt",
		"PLAIN_OK",
		"Do not call tools. Reply with exactly PLAIN_OK and nothing else.",
	);
	if (existsSync(pluginMarker)) {
		throw new Error("One-shot mode loaded a project plugin.");
	}
	console.log("PASS external plugins skipped");

	await expectExact(
		"piped stdin",
		"STDIN_OK",
		"Read the piped input and reply with only its response token.",
		{ stdin: "The required response token is STDIN_OK.\n" },
	);
	await expectExact(
		"bash tool",
		"BASH_TOOL_OK",
		"You must call the bash tool to execute printf BASH_TOOL_OK. After the tool returns, reply with exactly BASH_TOOL_OK and nothing else.",
	);
	await expectExact(
		"read tool",
		"READ_TOOL_OK",
		`You must call the read tool on ${readFixture}. After the tool returns, reply with exactly READ_TOOL_OK and nothing else.`,
	);
	await expectExact(
		"subagent",
		"SUBAGENT_OK",
		"Use the subagent tool with agent 'headless-one-shot-smoke' and ask it to return exactly SUBAGENT_INNER_OK. After it finishes, reply with exactly SUBAGENT_OK and nothing else.",
	);
	await testSignalHandling();

	const sessionsAfter = await filesBelow(sessionsRoot);
	const newSessions = [...sessionsAfter].filter(
		(filePath) => !sessionsBefore.has(filePath),
	);
	if (newSessions.length > 0) {
		throw new Error(
			`One-shot mode persisted sessions:\n${newSessions.join("\n")}`,
		);
	}
	console.log("PASS ephemeral session storage");
	console.log("One-shot smoke test passed.");
} finally {
	await rm(probePath, { force: true });
	await rm(subagentPath, { force: true });
	await rm(tempDir, { force: true, recursive: true });
}
