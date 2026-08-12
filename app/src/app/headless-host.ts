import { randomUUID } from "node:crypto";
import { BUILT_IN_COMMANDS, createCommandRegistry } from "../features/commands";
import {
	createMemorySubagentParentStorage,
	createMemorySubagentSessionStorage,
} from "../features/subagents/memory-storage";
import { createBuiltInPlugins } from "../plugins/built-ins";
import {
	type ExternalPluginFailure,
	ExternalPluginManager,
} from "../plugins/external";
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
	waitForPlugins: () => Promise<void>;
};

async function waitForAbortable(
	promise: Promise<void>,
	signal: AbortSignal,
): Promise<void> {
	if (signal.aborted) throw new Error("Headless startup aborted");
	let rejectAbort: ((error: Error) => void) | null = null;
	const aborted = new Promise<never>((_resolve, reject) => {
		rejectAbort = reject;
	});
	const onAbort = () => rejectAbort?.(new Error("Headless startup aborted"));
	signal.addEventListener("abort", onAbort, { once: true });
	try {
		await Promise.race([promise, aborted]);
	} finally {
		signal.removeEventListener("abort", onAbort);
	}
}

function reportExternalPluginFailure(failure: ExternalPluginFailure): void {
	const plugin = failure.pluginId ? ` ${failure.pluginId}` : "";
	const processExit = [
		failure.exitCode != null ? `exit ${failure.exitCode}` : undefined,
		failure.exitSignal ? `signal ${failure.exitSignal}` : undefined,
	]
		.filter(Boolean)
		.join(", ");
	const details = [
		`${failure.phase}: ${failure.message}`,
		failure.manifestPath,
		failure.otherManifestPath,
		processExit || undefined,
		failure.stderr ? `stderr: ${failure.stderr}` : undefined,
	].filter((value): value is string => Boolean(value));
	console.error(`Plugin${plugin} failed: ${details.join(" · ")}`);
}

export async function createHeadlessHost(
	session: Session,
	options: {
		externalPluginHome?: string;
		persistSession?: boolean;
		signal?: AbortSignal;
	} = {},
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
	const pluginAbort = new AbortController();
	const abortPlugins = () => pluginAbort.abort();
	if (options.signal?.aborted) abortPlugins();
	else options.signal?.addEventListener("abort", abortPlugins, { once: true });
	const externalPlugins = new ExternalPluginManager(pluginContext, {
		home: options.externalPluginHome,
		onFailure: reportExternalPluginFailure,
		signal: pluginAbort.signal,
	});
	const removePluginBarrier = runtime.addToolPreparationBarrier((signal) =>
		externalPlugins.waitForProjectTransition(signal),
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
		const initializeExternalPlugins = externalPlugins.initialize();
		if (options.signal) {
			await waitForAbortable(initializeExternalPlugins, options.signal);
		} else {
			await initializeExternalPlugins;
		}
	} catch (error) {
		unsubscribePersistenceFailure?.();
		persistence?.dispose();
		pluginAbort.abort();
		options.signal?.removeEventListener("abort", abortPlugins);
		await externalPlugins.dispose({ graceful: !options.signal?.aborted });
		removePluginBarrier();
		await builtInPlugins.disposeAsync();
		runtime.dispose();
		throw error;
	}

	let disposed = false;
	return {
		runtime,
		waitForPlugins: () => externalPlugins.waitForProjectTransition(),
		dispose: async () => {
			if (disposed) return;
			disposed = true;
			runtime.abort();
			pluginAbort.abort();
			options.signal?.removeEventListener("abort", abortPlugins);
			let cleanupError: unknown;
			try {
				await externalPlugins.dispose();
			} catch (error) {
				cleanupError = error;
			}
			removePluginBarrier();
			try {
				await builtInPlugins.disposeAsync();
			} catch (error) {
				cleanupError ??= error;
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
