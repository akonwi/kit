// Manual authenticated end-to-end verification for `kit -p`. Keep this matrix
// aligned with the lifecycle and output guarantees in app/src/app/print-mode.ts.
import { existsSync } from "node:fs";
import {
	mkdir,
	mkdtemp,
	readdir,
	readFile,
	realpath,
	rm,
	writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import {
	createSession,
	deleteSession,
	SESSIONS_DIR,
	writeSession,
} from "../src/session";

const repoRoot = path.resolve(import.meta.dir, "../..");
const pluginRoot = path.join(
	repoRoot,
	".kit",
	"plugins",
	"headless-print-mode-smoke",
);
const pluginManifestPath = path.join(pluginRoot, "plugin.json");
const pluginScriptPath = path.join(pluginRoot, "plugin.py");
const subagentPath = path.join(
	repoRoot,
	".kit",
	"agents",
	"headless-print-mode-smoke.md",
);
const tempDir = await mkdtemp(path.join(tmpdir(), "kit-print-mode-smoke-"));
const readFixture = path.join(tempDir, "read-fixture.txt");
const pluginMarker = path.join(tempDir, "external-plugin-loaded");
const appPreload = path.join(
	repoRoot,
	"node_modules/@opentui/solid/scripts/preload.js",
);
const appMain = path.join(repoRoot, "app/src/app/main.tsx");

for (const fixturePath of [pluginRoot, subagentPath]) {
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

async function runPrintMode(
	prompt: string,
	options: {
		model?: string;
		noSession?: boolean;
		sessionId?: string;
		stdin?: string;
		cwd?: string;
	} = {},
): Promise<RunResult> {
	const modelArgs = options.model ? ["--model", options.model] : [];
	const sessionArgs = options.sessionId ? ["--session", options.sessionId] : [];
	const noSession = options.noSession ?? options.sessionId === undefined;
	const noSessionArgs = noSession ? ["--no-session"] : [];
	const proc = Bun.spawn(
		[
			process.execPath,
			`--preload=${appPreload}`,
			appMain,
			"-p",
			...modelArgs,
			...sessionArgs,
			...noSessionArgs,
			prompt,
		],
		{
			cwd: options.cwd ?? repoRoot,
			env: process.env,
			stdin: options.stdin === undefined ? "ignore" : new Blob([options.stdin]),
			stdout: "pipe",
			stderr: "pipe",
		},
	);
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
	options?: Parameters<typeof runPrintMode>[1],
): Promise<void> {
	const result = await runPrintMode(prompt, options);
	if (result.exitCode !== 0 || result.stdout !== expected) {
		throw new Error(
			`${name} failed: exit=${result.exitCode}, expected=${JSON.stringify(expected)}, actual=${JSON.stringify(result.stdout)}\nstderr:\n${result.stderr}`,
		);
	}
	console.log(`PASS ${name}: ${expected}`);
}

async function testSignalHandling(): Promise<void> {
	const proc = Bun.spawn(
		[
			process.execPath,
			`--preload=${appPreload}`,
			appMain,
			"-p",
			"--no-session",
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

const pluginManifest = {
	manifestVersion: 1,
	id: "print-smoke",
	transport: {
		type: "stdio",
		command: "python3",
		args: ["-u", "plugin.py"],
	},
};
const pluginSource = `import json
import pathlib
import sys

marker = pathlib.Path(${JSON.stringify(pluginMarker)})

def send(message):
    print(json.dumps(message), flush=True)

for line in sys.stdin:
    if not line.strip():
        continue
    message = json.loads(line)
    method = message.get("method")
    if method == "initialize":
        marker.write_text("loaded")
        send({"jsonrpc": "2.0", "id": message["id"], "result": {"protocolVersion": 1}})
        send({
            "jsonrpc": "2.0",
            "id": "register-probe",
            "method": "kit/tools/register",
            "params": {
                "id": "probe",
                "description": "Return the print-mode plugin smoke-test token.",
                "inputSchema": {"type": "object", "additionalProperties": False},
            },
        })
    elif method == "kit/tools/execute":
        send({
            "jsonrpc": "2.0",
            "id": message["id"],
            "result": {"content": [{"type": "text", "text": "PLUGIN_TOOL_OK"}]},
        })
    elif method == "shutdown":
        send({"jsonrpc": "2.0", "id": message["id"], "result": None})
        break
`;
let smokeSessionId: string | null = null;

const subagentSource = `---
name: headless-print-mode-smoke
description: Smoke-test subagent
---
Follow the caller's request and return only the exact token it asks for.
`;

try {
	await writeFile(readFixture, "READ_TOOL_OK\n");
	await mkdir(pluginRoot, { recursive: true });
	await mkdir(path.dirname(subagentPath), { recursive: true });
	await writeFile(pluginManifestPath, JSON.stringify(pluginManifest));
	await writeFile(pluginScriptPath, pluginSource);
	await writeFile(subagentPath, subagentSource);

	const defaultCwd = await realpath(tempDir);
	const defaultSession = await createSession(defaultCwd);
	await writeSession(defaultSession);
	await expectExact(
		"default persistent session",
		"DEFAULT_SESSION_OK",
		"Do not call tools. Reply with exactly DEFAULT_SESSION_OK and nothing else.",
		{ noSession: false, cwd: defaultCwd },
	);
	const defaultSessionFile = path.join(
		SESSIONS_DIR,
		`${defaultSession.id}.jsonl`,
	);
	const defaultSessionContent = await readFile(defaultSessionFile, "utf8");
	if (!defaultSessionContent.includes("DEFAULT_SESSION_OK")) {
		throw new Error(
			"Print mode did not resume and persist its default session.",
		);
	}
	await deleteSession(defaultSession.id);
	console.log("PASS default session resumed and persisted");

	const sessionsRoot = SESSIONS_DIR;
	const sessionsBefore = await filesBelow(sessionsRoot);
	await expectExact(
		"plain prompt",
		"PLAIN_OK",
		"Do not call tools. Reply with exactly PLAIN_OK and nothing else.",
	);
	const explicitModel = process.env.KIT_PRINT_MODE_SMOKE_MODEL;
	if (explicitModel) {
		await expectExact(
			"explicit model",
			"MODEL_OK",
			"Do not call tools. Reply with exactly MODEL_OK and nothing else.",
			{ model: explicitModel },
		);
	} else {
		console.log(
			"SKIP explicit model: set KIT_PRINT_MODE_SMOKE_MODEL=<provider>/<model-id>",
		);
	}
	const smokeSession = await createSession(tempDir);
	smokeSessionId = smokeSession.id;
	await writeSession(smokeSession);
	await expectExact(
		"persistent session",
		"SESSION_OK",
		"Do not call tools. Reply with exactly SESSION_OK and nothing else.",
		{ sessionId: smokeSession.id },
	);
	const persistedSession = await readFile(
		path.join(SESSIONS_DIR, `${smokeSession.id}.jsonl`),
		"utf8",
	);
	if (!persistedSession.includes("SESSION_OK")) {
		throw new Error("Print mode did not persist the selected session.");
	}
	await deleteSession(smokeSession.id);
	smokeSessionId = null;
	console.log("PASS selected session persisted");

	if (!existsSync(pluginMarker)) {
		throw new Error("Print mode did not load the project plugin.");
	}
	console.log("PASS external plugin loaded");
	await expectExact(
		"external plugin tool",
		"PLUGIN_TOOL_OK",
		"Call the print-smoke__probe tool. After it returns, reply with exactly PLUGIN_TOOL_OK and nothing else.",
	);

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
		"Use the subagent tool with agent 'headless-print-mode-smoke' and ask it to return exactly SUBAGENT_INNER_OK. After it finishes, reply with exactly SUBAGENT_OK and nothing else.",
	);
	await testSignalHandling();

	const sessionsAfter = await filesBelow(sessionsRoot);
	const newSessions = [...sessionsAfter].filter(
		(filePath) => !sessionsBefore.has(filePath),
	);
	if (newSessions.length > 0) {
		throw new Error(
			`Print mode persisted sessions:\n${newSessions.join("\n")}`,
		);
	}
	console.log("PASS --no-session storage");
	console.log("Print mode smoke test passed.");
} finally {
	if (smokeSessionId) await deleteSession(smokeSessionId);
	await rm(pluginRoot, { force: true, recursive: true });
	await rm(subagentPath, { force: true });
	await rm(tempDir, { force: true, recursive: true });
}
