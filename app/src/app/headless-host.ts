import { randomUUID } from "node:crypto";
import {
	BUILT_IN_COMMANDS,
	type CommandRegistry,
	createCommandRegistry,
} from "../features/commands";
import { createScratchpadController } from "../features/scratchpad/controller";
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
import {
	createFooterStatusController,
	type FooterStatusController,
} from "../shell/footer-status";
import {
	createHeaderStatusController,
	type HeaderStatusController,
} from "../shell/header-status";
import { initTemplates } from "../shell/templates";
import { resolveAndApplyTheme } from "../shell/theme";
import { FilePersistence } from "../storage/file-persistence";
import { createHeadlessPluginUI } from "./headless-plugin-ui";
import type { RemoteInteractionBroker } from "./remote-interaction-broker";

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

export type HeadlessHost = {
	runtime: AgentRuntime;
	commands: CommandRegistry;
	footer: FooterStatusController;
	header: HeaderStatusController;
	scratchpad: ReturnType<typeof createScratchpadController> | null;
	waitForWorkspaceReady: () => Promise<void>;
	reload: (signal?: AbortSignal) => Promise<void>;
	dispose: () => Promise<void>;
};

export async function createHeadlessHost(
	session: Session,
	options: {
		externalPluginHome?: string;
		persistSession?: boolean;
		signal?: AbortSignal;
		interactions?: RemoteInteractionBroker;
		externalPlugins?: boolean;
		remoteChrome?: boolean;
		remotePromptCommands?: boolean;
	} = {},
): Promise<HeadlessHost> {
	let settings = await loadSettings();
	await resolveAndApplyTheme(settings.settings.theme ?? "system");
	initTemplates(session.cwd);
	await refreshModelAvailability();

	const runtime = new AgentRuntime(session, { settings: settings.settings });
	const scratchpad = options.persistSession
		? createScratchpadController(runtime)
		: null;
	const commands = createCommandRegistry(BUILT_IN_COMMANDS);
	const footer = createFooterStatusController();
	const header = createHeaderStatusController();
	const interactions = options.interactions;
	const pluginContext: PluginContext = {
		runtime,
		commands,
		settings,
		ui: createHeadlessPluginUI(interactions),
		attachments: createAttachmentsController(),
		footer,
		header,
		...(interactions
			? {
					openUrl: (url: string, source?: string, signal?: AbortSignal) =>
						interactions.openUrl({ url, source, signal }),
				}
			: {}),
		triggerNotification: () => false,
	};
	let builtInPlugins: PluginManager | null = null;
	let externalPlugins: ExternalPluginManager | null = null;
	let externalPluginAbort: AbortController | null = null;
	const removePluginBarrier = runtime.addToolPreparationBarrier(
		(signal) =>
			externalPlugins?.waitForProjectTransition(signal) ?? Promise.resolve(),
	);

	async function initializePlugins(): Promise<void> {
		const pluginReadiness: Promise<void>[] = [];
		const builtIns = new PluginManager(
			createBuiltInPlugins(pluginContext, {
				headless: true,
				onReady: (ready) => pluginReadiness.push(ready),
				subagentParentStorage: createMemorySubagentParentStorage(),
				subagentStorage: createMemorySubagentSessionStorage(),
				remoteGuidedQuestions: options.interactions,
				remoteChrome: options.remoteChrome,
				remotePromptCommands: options.remotePromptCommands,
			}),
			pluginContext,
		);
		const pluginAbort = new AbortController();
		externalPluginAbort = pluginAbort;
		const abortPlugins = () => pluginAbort.abort();
		if (options.signal?.aborted) abortPlugins();
		else
			options.signal?.addEventListener("abort", abortPlugins, { once: true });
		const external =
			options.externalPlugins !== false
				? new ExternalPluginManager(pluginContext, {
						home: options.externalPluginHome,
						...(options.interactions
							? {}
							: { onFailure: reportExternalPluginFailure }),
						signal: pluginAbort.signal,
					})
				: null;
		builtInPlugins = builtIns;
		externalPlugins = external;
		try {
			builtIns.initialize();
			await Promise.all(pluginReadiness);
			const initializeExternal = external?.initialize();
			if (initializeExternal) {
				if (options.signal) {
					await waitForAbortable(initializeExternal, options.signal);
				} else {
					await initializeExternal;
				}
			}
		} catch (error) {
			externalPlugins = null;
			builtInPlugins = null;
			externalPluginAbort = null;
			pluginAbort.abort();
			await external
				?.dispose({ graceful: options.signal?.aborted !== true })
				.catch(() => {});
			await builtIns.disposeAsync().catch(() => {});
			throw error;
		} finally {
			options.signal?.removeEventListener("abort", abortPlugins);
		}
	}

	async function disposePluginManagers(): Promise<void> {
		const external = externalPlugins;
		const builtIns = builtInPlugins;
		externalPluginAbort?.abort();
		externalPluginAbort = null;
		externalPlugins = null;
		builtInPlugins = null;
		let cleanupError: unknown;
		try {
			await external?.dispose();
		} catch (error) {
			cleanupError = error;
		}
		try {
			await builtIns?.disposeAsync();
		} catch (error) {
			cleanupError ??= error;
		}
		if (cleanupError) throw cleanupError;
	}

	const persistence = options.persistSession
		? new FilePersistence(runtime)
		: null;
	let persistenceFailure: Error | null = null;
	const unsubscribePersistenceFailure = persistence?.onFailure((event) => {
		persistenceFailure = new Error(event.error);
	});
	try {
		await initializePlugins();
	} catch (error) {
		unsubscribePersistenceFailure?.();
		persistence?.dispose();
		scratchpad?.dispose();
		runtime.dispose();
		throw error;
	}

	let disposed = false;
	let activeReload: Promise<void> | null = null;

	function assertReloadActive(signal?: AbortSignal): void {
		if (disposed) throw new Error("Headless host is disposed");
		if (signal?.aborted) {
			throw signal.reason instanceof Error
				? signal.reason
				: new Error("Host reload aborted");
		}
	}

	async function performReload(signal?: AbortSignal): Promise<void> {
		assertReloadActive(signal);
		await disposePluginManagers();
		try {
			assertReloadActive(signal);
			settings = await loadSettings();
			pluginContext.settings = settings;
			await resolveAndApplyTheme(settings.settings.theme ?? "system");
			assertReloadActive(signal);
			runtime.emitSettingsChanged(settings.settings);
			await runtime.reloadSession();
			assertReloadActive(signal);
		} catch (error) {
			if (!disposed) await initializePlugins().catch(() => {});
			throw error;
		}
		await initializePlugins();
		assertReloadActive(signal);
		options.interactions?.toast({
			title: "Session reloaded",
			variant: "info",
		});
	}

	function reload(signal?: AbortSignal): Promise<void> {
		if (activeReload) return activeReload;
		const execution = performReload(signal);
		activeReload = execution;
		const clear = () => {
			if (activeReload === execution) activeReload = null;
		};
		execution.then(clear, clear);
		return execution;
	}

	return {
		runtime,
		commands,
		footer,
		header,
		scratchpad,
		waitForWorkspaceReady: async () => {
			await externalPlugins?.waitForProjectReady();
		},
		reload,
		dispose: async () => {
			if (disposed) return;
			disposed = true;
			runtime.abort();
			await activeReload?.catch(() => {});
			scratchpad?.dispose();
			removePluginBarrier();
			let cleanupError: unknown;
			try {
				await disposePluginManagers();
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
