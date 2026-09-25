import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { clearModelAvailabilityCache } from "../runtime/models";
import { SHOW_IMAGE_TOOL_NAME } from "../tools";
import { createEphemeralSession, createHeadlessHost } from "./headless-host";

const tempDirs: string[] = [];

afterEach(async () => {
	clearModelAvailabilityCache();
	await Promise.all(
		tempDirs.splice(0).map((dir) => rm(dir, { force: true, recursive: true })),
	);
});

async function createPlugin(root: string, id: string): Promise<string> {
	await mkdir(root, { recursive: true });
	const shutdownMarker = path.join(root, "shutdown");
	const scriptPath = path.join(root, "plugin.ts");
	await writeFile(
		scriptPath,
		`import { createInterface } from "node:readline";
const marker = ${JSON.stringify(shutdownMarker)};
const lines = createInterface({ input: process.stdin });
for await (const line of lines) {
  if (!line.trim()) continue;
  const message = JSON.parse(line);
  if (message.method === "initialize") {
    console.log(JSON.stringify({ jsonrpc: "2.0", id: message.id, result: { protocolVersion: 1 } }));
    console.log(JSON.stringify({
      jsonrpc: "2.0",
      id: "register-tool",
      method: "kit/tools/register",
      params: {
        id: "probe",
        description: "Headless plugin probe",
        inputSchema: { type: "object", additionalProperties: false },
      },
    }));
  } else if (message.method === "shutdown") {
    await Bun.write(marker, "stopped");
    console.log(JSON.stringify({ jsonrpc: "2.0", id: message.id, result: null }));
    setTimeout(() => process.exit(0), 10);
    break;
  }
}
`,
	);
	await writeFile(
		path.join(root, "plugin.json"),
		JSON.stringify({
			manifestVersion: 1,
			id,
			transport: {
				type: "stdio",
				command: process.execPath,
				args: [scriptPath],
			},
		}),
	);
	return shutdownMarker;
}

describe("createHeadlessHost", () => {
	test("aborts startup and terminates a plugin that never initializes", async () => {
		const root = await mkdtemp(path.join(tmpdir(), "kit-headless-abort-"));
		tempDirs.push(root);
		const home = path.join(root, "home");
		const cwd = path.join(root, "project");
		const pluginRoot = path.join(home, ".kit", "plugins", "hung-probe");
		const pidPath = path.join(root, "plugin.pid");
		const scriptPath = path.join(pluginRoot, "plugin.ts");
		await mkdir(pluginRoot, { recursive: true });
		await mkdir(cwd, { recursive: true });
		await writeFile(
			scriptPath,
			`await Bun.write(${JSON.stringify(pidPath)}, String(process.pid));\nawait new Promise(() => {});\n`,
		);
		await writeFile(
			path.join(pluginRoot, "plugin.json"),
			JSON.stringify({
				manifestVersion: 1,
				id: "hung-probe",
				transport: {
					type: "stdio",
					command: process.execPath,
					args: [scriptPath],
				},
			}),
		);

		const originalCwd = process.cwd();
		const controller = new AbortController();
		const startup = createHeadlessHost(createEphemeralSession(cwd), {
			externalPluginHome: home,
			signal: controller.signal,
		});
		for (
			let attempt = 0;
			attempt < 100 && !(await Bun.file(pidPath).exists());
			attempt++
		) {
			await Bun.sleep(10);
		}
		controller.abort();
		try {
			await expect(startup).rejects.toThrow("Headless startup aborted");
		} finally {
			process.chdir(originalCwd);
		}

		const pid = Number(await readFile(pidPath, "utf8"));
		await Bun.sleep(50);
		expect(() => process.kill(pid, 0)).toThrow();
	});

	test("loads and disposes user and project external plugins", async () => {
		const root = await mkdtemp(path.join(tmpdir(), "kit-headless-host-"));
		tempDirs.push(root);
		const home = path.join(root, "home");
		const cwd = path.join(root, "project");
		await mkdir(cwd, { recursive: true });
		const userShutdown = await createPlugin(
			path.join(home, ".kit", "plugins", "user-probe"),
			"user-probe",
		);
		const projectShutdown = await createPlugin(
			path.join(cwd, ".kit", "plugins", "project-probe"),
			"project-probe",
		);

		const originalCwd = process.cwd();
		const host = await createHeadlessHost(createEphemeralSession(cwd), {
			externalPluginHome: home,
		});
		try {
			const toolNames = host.runtime.getTools().map((tool) => tool.name);
			expect(toolNames).toEqual(
				expect.arrayContaining(["user-probe__probe", "project-probe__probe"]),
			);
			expect(toolNames).not.toContain(SHOW_IMAGE_TOOL_NAME);
		} finally {
			await host.dispose();
			process.chdir(originalCwd);
		}
		expect(await Bun.file(userShutdown).text()).toBe("stopped");
		expect(await Bun.file(projectShutdown).text()).toBe("stopped");
	});
});
