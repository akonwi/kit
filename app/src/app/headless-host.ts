import { randomUUID } from "node:crypto";
import { BUILT_IN_COMMANDS, createCommandRegistry } from "../features/commands";
import {
	createMemorySubagentParentStorage,
	createMemorySubagentSessionStorage,
} from "../features/subagents/memory-storage";
import { createBuiltInPlugins } from "../plugins/built-ins";
import { PluginManager } from "../plugins/PluginManager";
import type { PluginContext } from "../plugins/types";
import { AgentRuntime } from "../runtime/agent-runtime";
import { refreshModelAvailability } from "../runtime/models";
import { SESSION_VERSION, type Session } from "../session";
import { loadSettings } from "../settings";
import { createAttachmentsController } from "../shell/attachments-controller";
import { createFooterStatusController } from "../shell/footer-status";
import { createHeaderStatusController } from "../shell/header-status";
import { initTemplates } from "../shell/templates";
import { resolveAndApplyTheme } from "../shell/theme";
import { FilePersistence } from "../storage/file-persistence";
import { createHeadlessPluginUI } from "./headless-plugin-ui";

export function createEphemeralSession(cwd: string): Session {
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

export function takeOverStdout(): {
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

export type HeadlessHost = {
	runtime: AgentRuntime;
	dispose: () => Promise<void>;
};

export async function createHeadlessHost(
	session: Session,
	options: { persistSession?: boolean } = {},
): Promise<HeadlessHost> {
	const settings = await loadSettings();
	await resolveAndApplyTheme(settings.settings.theme ?? "system");
	initTemplates(session.cwd);
	await refreshModelAvailability();

	const runtime = new AgentRuntime(session, { settings: settings.settings });
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
	const builtInPlugins = new PluginManager(
		createBuiltInPlugins(pluginContext, {
			headless: true,
			onReady: (ready) => pluginReadiness.push(ready),
			subagentParentStorage: createMemorySubagentParentStorage(),
			subagentStorage: createMemorySubagentSessionStorage(),
		}),
		pluginContext,
	);
	const persistence = options.persistSession
		? new FilePersistence(runtime)
		: null;
	let persistenceFailure: Error | null = null;
	const unsubscribePersistenceFailure = persistence?.onFailure((event) => {
		persistenceFailure = new Error(event.error);
	});
	try {
		builtInPlugins.initialize();
		await Promise.all(pluginReadiness);
	} catch (error) {
		unsubscribePersistenceFailure?.();
		persistence?.dispose();
		await builtInPlugins.disposeAsync();
		runtime.dispose();
		throw error;
	}

	let disposed = false;
	return {
		runtime,
		dispose: async () => {
			if (disposed) return;
			disposed = true;
			runtime.abort();
			let cleanupError: unknown;
			try {
				await builtInPlugins.disposeAsync();
			} catch (error) {
				cleanupError = error;
			}
			if (persistence) {
				try {
					await persistence.flush();
					if (!cleanupError && persistenceFailure) {
						cleanupError = persistenceFailure;
					}
				} catch (error) {
					cleanupError ??= error;
				} finally {
					unsubscribePersistenceFailure?.();
					persistence.dispose();
				}
			}
			runtime.dispose();
			if (cleanupError) throw cleanupError;
		},
	};
}
