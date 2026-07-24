import { randomUUID } from "node:crypto";
import { BUILT_IN_COMMANDS, createCommandRegistry } from "../features/commands";
import {
	createMemorySubagentParentStorage,
	createMemorySubagentSessionStorage,
} from "../features/subagents/memory-storage";
import { createBuiltInPlugins } from "../plugins/built-ins";
import { PluginManager } from "../plugins/PluginManager";
import type { PluginContext } from "../plugins/types";
import type { AgentMessage } from "../runtime/agent";
import {
	AgentRuntime,
	AuthenticationRequiredError,
} from "../runtime/agent-runtime";
import { SESSION_VERSION, type Session } from "../session";
import { loadSettings } from "../settings";
import { createAttachmentsController } from "../shell/attachments-controller";
import { createFooterStatusController } from "../shell/footer-status";
import { createHeaderStatusController } from "../shell/header-status";
import { initTemplates } from "../shell/templates";
import { resolveAndApplyTheme } from "../shell/theme";
import { createHeadlessPluginUI } from "./headless-plugin-ui";

function createEphemeralSession(cwd: string): Session {
	const timestamp = new Date().toISOString();
	return {
		id: randomUUID(),
		version: SESSION_VERSION,
		cwd,
		createdAt: timestamp,
		updatedAt: timestamp,
		turns: [],
	};
}

function assistantText(message: AgentMessage | undefined): string {
	if (message?.role !== "assistant") return "";
	return message.content
		.filter(
			(block): block is { type: "text"; text: string } => block.type === "text",
		)
		.map((block) => block.text)
		.join("\n");
}

// One-shot CLI execution is intentionally limited to one run per process. These
// process-global patches are not safe for concurrent or nested runOneShot calls;
// use subprocess isolation before introducing either execution model.
function takeOverStdout(): {
	restore: () => void;
	write: (text: string) => Promise<void>;
} {
	const originalWrite = process.stdout.write;
	const originalConsoleDebug = console.debug;
	const originalConsoleInfo = console.info;
	const originalConsoleLog = console.log;
	const rawWrite = originalWrite.bind(process.stdout);
	process.stdout.write = ((
		chunk: string | Uint8Array,
		encodingOrCallback?: BufferEncoding | ((error?: Error | null) => void),
		callback?: (error?: Error | null) => void,
	): boolean => {
		if (typeof encodingOrCallback === "function") {
			return process.stderr.write(chunk, encodingOrCallback);
		}
		return process.stderr.write(chunk, encodingOrCallback, callback);
	}) as typeof process.stdout.write;
	console.debug = (...args) => console.error(...args);
	console.info = (...args) => console.error(...args);
	console.log = (...args) => console.error(...args);

	return {
		restore: () => {
			process.stdout.write = originalWrite;
			console.debug = originalConsoleDebug;
			console.info = originalConsoleInfo;
			console.log = originalConsoleLog;
		},
		write: (text) =>
			new Promise<void>((resolve, reject) => {
				rawWrite(text, (error) => {
					if (error) reject(error);
					else resolve();
				});
			}),
	};
}

// Run `bun run smoke:one-shot` from the repository root after changing this
// lifecycle or its plugin, output, signal, tool, or persistence behavior.
export async function runOneShot(prompt: string, cwd: string): Promise<number> {
	const stdout = takeOverStdout();
	let runtime: AgentRuntime | null = null;
	let builtInPlugins: PluginManager | null = null;
	let signalExitCode: number | null = null;
	let forcedExitTimer: ReturnType<typeof setTimeout> | null = null;
	const handleSignal = (signal: "SIGINT" | "SIGTERM") => {
		const exitCode = signal === "SIGINT" ? 130 : 143;
		if (signalExitCode !== null) process.exit(exitCode);
		signalExitCode = exitCode;
		runtime?.abort();
		forcedExitTimer = setTimeout(() => process.exit(exitCode), 5_000);
	};
	const handleSigint = () => handleSignal("SIGINT");
	const handleSigterm = () => handleSignal("SIGTERM");
	process.on("SIGINT", handleSigint);
	process.on("SIGTERM", handleSigterm);
	try {
		const settings = await loadSettings();
		await resolveAndApplyTheme(settings.settings.theme ?? "system");
		initTemplates(cwd);

		runtime = new AgentRuntime(createEphemeralSession(cwd), {
			settings: settings.settings,
		});
		const pluginContext: PluginContext = {
			runtime,
			commands: createCommandRegistry(BUILT_IN_COMMANDS),
			settings,
			ui: createHeadlessPluginUI(),
			attachments: createAttachmentsController(),
			footer: createFooterStatusController(),
			header: createHeaderStatusController(),
			triggerNotification: () => false,
		};

		const pluginReadiness: Promise<void>[] = [];
		builtInPlugins = new PluginManager(
			createBuiltInPlugins(pluginContext, {
				headless: true,
				onReady: (ready) => pluginReadiness.push(ready),
				subagentParentStorage: createMemorySubagentParentStorage(),
				subagentStorage: createMemorySubagentSessionStorage(),
			}),
			pluginContext,
		);
		builtInPlugins.initialize();
		await Promise.all(pluginReadiness);

		if (signalExitCode !== null) return signalExitCode;
		await runtime.submitUserMessage(prompt);
		if (signalExitCode !== null) return signalExitCode;
		const lastMessage = runtime.getMessages().at(-1);
		if (lastMessage?.role !== "assistant") {
			console.error("Kit completed without an assistant response.");
			return 1;
		}
		if (
			lastMessage.stopReason === "error" ||
			lastMessage.stopReason === "aborted"
		) {
			console.error(
				lastMessage.errorMessage ?? `Request ${lastMessage.stopReason}.`,
			);
			return 1;
		}
		const text = assistantText(lastMessage);
		if (!text.trim()) {
			console.error("Kit completed without assistant text.");
			return 1;
		}
		await stdout.write(`${text}\n`);
		return signalExitCode ?? 0;
	} catch (error) {
		if (signalExitCode !== null) return signalExitCode;
		if (error instanceof AuthenticationRequiredError) {
			console.error(
				"Kit is not authenticated with an available model provider.",
			);
		} else {
			console.error(error instanceof Error ? error.message : String(error));
		}
		return 1;
	} finally {
		process.off("SIGINT", handleSigint);
		process.off("SIGTERM", handleSigterm);
		try {
			await builtInPlugins?.disposeAsync();
		} catch (error) {
			console.error(
				`Built-in plugin cleanup failed: ${error instanceof Error ? error.message : String(error)}`,
			);
		}
		try {
			runtime?.dispose();
		} catch (error) {
			console.error(
				`Runtime cleanup failed: ${error instanceof Error ? error.message : String(error)}`,
			);
		} finally {
			if (forcedExitTimer) clearTimeout(forcedExitTimer);
			stdout.restore();
		}
	}
}
