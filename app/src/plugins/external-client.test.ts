import { afterEach, describe, expect, test } from "bun:test";
import {
	chmod,
	mkdtemp,
	readFile,
	realpath,
	rm,
	writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { createCommandRegistry } from "../features/commands";
import type { AgentTool } from "../runtime/agent";
import type { ToolApprovalHandler } from "../runtime/agent-runtime";
import { createChromeContributionsController } from "../shell/chrome-contributions";
import type { ExternalPluginFailure, ExternalPluginManifest } from "./external";
import { ExternalPluginClient } from "./external-client";
import type { PluginContext } from "./types";

const tempDirs: string[] = [];

async function makeTempDir(): Promise<string> {
	const dir = await mkdtemp(path.join(tmpdir(), "kit-plugin-client-test-"));
	tempDirs.push(dir);
	return dir;
}

async function waitFor(
	predicate: () => boolean,
	timeoutMs = 2_000,
): Promise<void> {
	const deadline = Date.now() + timeoutMs;
	while (!predicate()) {
		if (Date.now() >= deadline)
			throw new Error("Timed out waiting for plugin state");
		await Bun.sleep(5);
	}
}

afterEach(async () => {
	await Promise.all(
		tempDirs.splice(0).map((dir) => rm(dir, { force: true, recursive: true })),
	);
});

describe("ExternalPluginClient", () => {
	test("initializes a dependency-free Python plugin and bridges owned contributions", async () => {
		const root = await makeTempDir();
		const canonicalRoot = await realpath(root);
		const statePath = path.join(root, "state.jsonl");
		const scriptPath = path.join(root, "plugin.py");
		await writeFile(
			scriptPath,
			`#!/usr/bin/env python3
import json, os, sys
state_path = ${JSON.stringify(statePath)}
def record(value):
    with open(state_path, "a", encoding="utf-8") as f:
        f.write(json.dumps(value) + "\\n")
def send(value):
    sys.stdout.write(json.dumps(value) + "\\n")
    sys.stdout.flush()
for line in sys.stdin:
    message = json.loads(line)
    method = message.get("method")
    if method == "initialize":
        record({"cwd": os.getcwd(), "initialize": message["params"]})
        send({"jsonrpc": "2.0", "id": message["id"], "result": {"protocolVersion": 1}})
        send({"jsonrpc": "2.0", "id": "p-confirm", "method": "kit/ui/confirm", "params": {"title": "Continue?"}})
        send({"jsonrpc": "2.0", "id": "p-command", "method": "kit/commands/register", "params": {"id": "toggle", "description": "Toggle speech"}})
        send({"jsonrpc": "2.0", "id": "p-tool", "method": "kit/tools/register", "params": {"id": "speak_text", "description": "Speak text", "inputSchema": {"type": "object", "properties": {"text": {"type": "string"}}, "required": ["text"], "additionalProperties": False}}})
        send({"jsonrpc": "2.0", "id": "p-header", "method": "kit/header/set", "params": {"id": "status", "content": [{"text": "speech", "style": {"fg": "toolText", "bold": True}}], "action": {"type": "open-url", "url": "https://example.com/status"}}})
        send({"jsonrpc": "2.0", "id": "p-footer", "method": "kit/footer/set", "params": {"id": "status", "content": [{"text": "ready", "style": {"fg": "metaText"}}], "side": "left", "clickable": True}})
        send({"jsonrpc": "2.0", "id": "p-prompt", "method": "kit/system-prompt/set", "params": {"text": "Always speak the final answer."}})
        send({"jsonrpc": "2.0", "id": "p-open", "method": "kit/system/open-url", "params": {"url": "https://example.com/plugin"}})
    elif method == "kit/commands/execute":
        record({"command": message["params"]})
        send({"jsonrpc": "2.0", "id": message["id"], "result": None})
    elif method == "kit/tools/execute":
        record({"tool": message["params"]})
        send({"jsonrpc": "2.0", "id": message["id"], "result": {"content": [{"type": "text", "text": "spoken"}], "details": None, "terminate": False}})
    elif method == "kit/footer/click":
        record({"footerClick": message["params"]})
        send({"jsonrpc": "2.0", "id": message["id"], "result": None})
    elif method == "shutdown":
        record({"shutdown": True})
        send({"jsonrpc": "2.0", "id": message["id"], "result": None})
        break
    elif method and method.startswith("kit/events/"):
        record({"event": method, "params": message.get("params")})
    elif message.get("id") == "p-confirm":
        record({"confirm": message.get("result")})
`,
		);
		await chmod(scriptPath, 0o755);

		const commands = createCommandRegistry();
		const tools: AgentTool[] = [];
		const header = createChromeContributionsController();
		const footer = createChromeContributionsController();
		let systemPrompt = "";
		const openedUrls: Array<{ url: string; source?: string }> = [];
		const session = {
			id: "session-1",
			name: "Protocol test",
			cwd: root,
		};
		const runtime = {
			vcsInfo: { root, branch: "main", dirty: true },
			getSession: () => session,
			getTools: () => [...tools],
			addTool: (tool: AgentTool) => {
				tools.push(tool);
				return () => tools.splice(tools.indexOf(tool), 1);
			},
			addToolApprovalHandler: () => () => {},
			getAllSubagents: () => [],
			addPluginSubagent: () => () => {},
			createSystemPromptSlot: () => ({
				set: (text: string) => {
					systemPrompt = text;
				},
				clear: () => {
					systemPrompt = "";
				},
				dispose: () => {
					systemPrompt = "";
				},
			}),
			submitMessage: async () => {},
		};
		const context = {
			runtime,
			commands,
			header,
			footer,
			settings: { settings: {}, paths: {} },
			ui: {
				text: (text: string, style?: object) => ({
					__kitText: true,
					text,
					style,
				}),
				theme: () => ({ name: "test", tokens: {}, syntaxPalette: {} }),
				toast: () => {},
				select: async () => undefined,
				input: async () => undefined,
				confirm: async () => undefined,
				custom: async () => undefined,
				getTranscriptViewport: () => null,
			},
			attachments: {},
			openUrl: async (url: string, source?: string) => {
				openedUrls.push({ url, source });
			},
			triggerNotification: () => false,
		} as unknown as PluginContext;
		const manifest: ExternalPluginManifest = {
			source: "project",
			installationName: "speech",
			root,
			manifestPath: path.join(root, "plugin.json"),
			manifest: {
				manifestVersion: 1,
				id: "speech",
				transport: { type: "stdio", command: "./plugin.py" },
			},
		};
		const failures: ExternalPluginFailure[] = [];
		const client = new ExternalPluginClient({
			manifest,
			context,
			onFailure: (failure) => failures.push(failure),
		});

		await client.start();
		await waitFor(
			() =>
				commands.getAll().length === 1 &&
				tools.length === 1 &&
				header.getContributions().length === 1 &&
				footer.getContributions().length === 1 &&
				systemPrompt.length > 0 &&
				openedUrls.length === 1,
		);
		expect(commands.getAll()[0]).toMatchObject({
			name: "speech.toggle",
			displayName: "toggle",
			executeTransportNeutral: expect.any(Function),
		});
		expect(tools[0]?.name).toBe("speech__speak_text");
		expect(header.getContributions()[0]).toMatchObject({
			id: "speech.status",
			content: [{ text: "speech", style: { fgToken: "toolText", bold: true } }],
			action: { type: "open-url", url: "https://example.com/status" },
		});
		expect(footer.getContributions()[0]).toMatchObject({
			id: "speech.status",
			content: [{ text: "ready", style: { fgToken: "metaText" } }],
			side: "left",
		});
		expect(typeof footer.getContributions()[0]?.onClick).toBe("function");
		expect(systemPrompt).toBe("Always speak the final answer.");
		expect(openedUrls).toEqual([
			{ url: "https://example.com/plugin", source: "speech" },
		]);

		await footer.getContributions()[0]?.onClick?.();
		await commands.getAll()[0]?.execute({ args: "now" } as never);
		const toolResult = await tools[0]?.execute(
			"call-1",
			{ text: "hello" },
			undefined,
			undefined,
		);
		expect(toolResult?.content).toEqual([{ type: "text", text: "spoken" }]);
		client.notify("kit/events/project.changed", {
			cwd: root,
			git: { root, branch: "main", dirty: false },
		});
		await client.stop();

		expect(failures).toEqual([]);
		expect(commands.getAll()).toEqual([]);
		expect(tools).toEqual([]);
		expect(header.getContributions()).toEqual([]);
		expect(footer.getContributions()).toEqual([]);
		expect(systemPrompt).toBe("");
		const records = (await readFile(statePath, "utf8"))
			.trim()
			.split("\n")
			.map((line) => JSON.parse(line));
		expect(records[0]).toMatchObject({
			cwd: canonicalRoot,
			initialize: {
				protocolVersion: 1,
				context: {
					project: { cwd: root, git: { root, branch: "main", dirty: true } },
					session: { id: "session-1", name: "Protocol test" },
				},
			},
		});
		expect(records).toContainEqual({ footerClick: { id: "status" } });
		expect(records).toContainEqual({ command: { id: "toggle", args: "now" } });
		expect(records).toContainEqual({
			tool: {
				id: "speak_text",
				toolCallId: "call-1",
				input: { text: "hello" },
			},
		});
		expect(records).toContainEqual({
			event: "kit/events/project.changed",
			params: { cwd: root, git: { root, branch: "main", dirty: false } },
		});
		expect(records).toContainEqual({ confirm: false });
		expect(records).toContainEqual({ shutdown: true });
	});

	test("composes plugin tool interception with nested remote confirmation", async () => {
		const root = path.resolve(import.meta.dir, "fixtures");
		const commands = createCommandRegistry();
		const header = createChromeContributionsController();
		const footer = createChromeContributionsController();
		let approvalHandler: ToolApprovalHandler | undefined;
		let approvalDisposed = false;
		let cancellationCount = 0;
		let nestedRequestRegistered = false;
		const confirmations: Array<Record<string, unknown>> = [];
		const decisions = [false, true];
		const runtime = {
			vcsInfo: null,
			getSession: () => ({ id: "session-1", name: null, cwd: root }),
			getTools: () => [],
			addTool: () => () => {},
			addToolApprovalHandler: (handler: ToolApprovalHandler) => {
				approvalHandler = handler;
				return () => {
					approvalDisposed = true;
					approvalHandler = undefined;
				};
			},
			getAllSubagents: () => [],
			addPluginSubagent: () => () => {},
			createSystemPromptSlot: () => ({
				set: () => {},
				clear: () => {},
				dispose: () => {},
			}),
			submitMessage: async () => {},
		};
		const context = {
			runtime,
			commands,
			header,
			footer,
			settings: { settings: {}, paths: {} },
			ui: {
				text: (text: string, style?: object) => ({
					__kitText: true,
					text,
					style,
				}),
				theme: () => ({ name: "test", tokens: {}, syntaxPalette: {} }),
				toast: (input: Record<string, unknown>) => {
					if (input.title === "Nested request registered") {
						nestedRequestRegistered = true;
					}
				},
				select: async () => undefined,
				input: async () => undefined,
				confirm: async (input: Record<string, unknown>) => {
					confirmations.push(input);
					if (confirmations.length <= decisions.length) {
						return decisions[confirmations.length - 1];
					}
					return new Promise<boolean>((resolve) => {
						const signal = input.signal;
						if (!(signal instanceof AbortSignal)) return resolve(false);
						if (signal.aborted) {
							cancellationCount += 1;
							return resolve(false);
						}
						signal.addEventListener(
							"abort",
							() => {
								cancellationCount += 1;
								resolve(false);
							},
							{ once: true },
						);
					});
				},
				custom: async () => undefined,
				getTranscriptViewport: () => null,
			},
			attachments: {},
			triggerNotification: () => false,
		} as unknown as PluginContext;
		const manifest: ExternalPluginManifest = {
			source: "project",
			installationName: "tool-approval",
			root,
			manifestPath: path.join(root, "tool-approval.plugin.json"),
			manifest: {
				manifestVersion: 1,
				id: "tool-approval",
				transport: {
					type: "stdio",
					command: "python3",
					args: ["-u", "tool-approval.py"],
				},
			},
		};
		const failures: ExternalPluginFailure[] = [];
		const client = new ExternalPluginClient({
			manifest,
			context,
			onFailure: (failure) => failures.push(failure),
		});

		await client.start();
		await waitFor(() => approvalHandler !== undefined);
		const handler = approvalHandler;
		if (!handler) throw new Error("Expected tool approval handler");
		expect(
			await handler({
				toolCallId: "safe",
				toolName: "bash",
				args: { command: "git status" },
			}),
		).toEqual({ approved: true });
		expect(confirmations).toEqual([]);

		expect(
			await handler({
				toolCallId: "blocked",
				toolName: "bash",
				args: { command: "git commit -m 'test'" },
			}),
		).toEqual({ approved: false, reason: "The user rejected bash." });
		expect(confirmations[0]).toMatchObject({
			title: "Allow bash?",
			confirmLabel: "Allow",
			cancelLabel: "Block",
			defaultValue: false,
		});

		expect(
			await handler({
				toolCallId: "allowed",
				toolName: "bash",
				args: { command: "npm publish" },
			}),
		).toEqual({ approved: true });

		const abortController = new AbortController();
		const cancelled = handler(
			{
				toolCallId: "cancelled",
				toolName: "bash",
				args: { command: "git commit --amend" },
			},
			abortController.signal,
		);
		await waitFor(() => confirmations.length === 3);
		abortController.abort();
		await expect(cancelled).rejects.toThrow("cancelled");
		await waitFor(() => cancellationCount === 1);

		const earlyAbort = new AbortController();
		const cancelledBeforeNestedRequest = handler(
			{
				toolCallId: "cancelled-early",
				toolName: "bash",
				args: { command: "git commit --delay-approval" },
			},
			earlyAbort.signal,
		);
		await Bun.sleep(10);
		earlyAbort.abort();
		await expect(cancelledBeforeNestedRequest).rejects.toThrow("cancelled");
		await Bun.sleep(70);
		expect(confirmations).toHaveLength(3);

		const registeredAbort = new AbortController();
		const cancellationCountBeforeSendRace = cancellationCount;
		const cancelledBeforeNestedSend = handler(
			{
				toolCallId: "cancelled-before-send",
				toolName: "bash",
				args: { command: "git commit --delay-nested-send" },
			},
			registeredAbort.signal,
		);
		await waitFor(() => nestedRequestRegistered);
		registeredAbort.abort();
		await expect(cancelledBeforeNestedSend).rejects.toThrow("cancelled");
		await waitFor(() => cancellationCount > cancellationCountBeforeSendRace);
		expect(confirmations).toHaveLength(4);

		await client.stop();
		expect(approvalDisposed).toBe(true);
		expect(failures).toEqual([]);
	});
});
