import { createSignal, Match, onCleanup, onMount, Switch } from "solid-js";
import {
	BUILT_IN_COMMANDS,
	type CommandRegistry,
	createCommandRegistry,
} from "../features/commands";
import { createMcpWorkspaceController } from "../features/mcp";
import { createReleasesWorkspaceController } from "../features/releases";
import { createReviewDraftController } from "../features/review/draft-controller";
import { createReviewWorkspaceController } from "../features/review/workspace-controller";
import {
	createMemoryScratchpadStorage,
	createScratchpadController,
} from "../features/scratchpad/controller";
import { createSubagentsWorkspaceController } from "../features/subagents";
import {
	createMemorySubagentParentStorage,
	createMemorySubagentSessionStorage,
} from "../features/subagents/memory-storage";
import { createBuiltInPlugins } from "../plugins/built-ins";
import { ExternalPluginManager } from "../plugins/external";
import { PluginManager } from "../plugins/PluginManager";
import type { InternalPluginUI, TranscriptViewport } from "../plugins/types";
import {
	AgentRuntime,
	AuthenticationRequiredError,
} from "../runtime/agent-runtime";
import {
	hasCachedProviderAuth,
	kitModels,
	refreshModelAvailability,
} from "../runtime/models";
import type { Session } from "../session";
import { type LoadedSettings, loadSettings } from "../settings";
import { AppShell } from "../shell/AppShell";
import { createAttachmentsController } from "../shell/attachments-controller";
import { createComposerController } from "../shell/composer-controller";
import { createFooterStatusController } from "../shell/footer-status";
import { createHeaderStatusController } from "../shell/header-status";
import { initTemplates } from "../shell/templates";
import { registerTerminalTurnStatus } from "../shell/terminal-turn-status";
import { getCurrentThemeConfig, resolveAndApplyTheme } from "../shell/theme";
import { createAppState } from "../state/app-state";
import type { ToastInput } from "../state/toasts";
import { FilePersistence } from "../storage/file-persistence";
import { AuthGateScreen } from "./AuthGateScreen";
import { FatalScreen } from "./FatalScreen";
import {
	applyStartupModel,
	StartupModelAuthenticationRequiredError,
} from "./headless-model";
import { createCustomOverlayHandler, type OverlayEntry } from "./overlay-ui";
import { createPluginUI } from "./plugin-ui";

export type AppProps = {
	settings: LoadedSettings;
	startupModel?: string;
	session: Session;
	updateTerminalTitle: (sessionName: string | undefined, cwd: string) => void;
	setTerminalTurnActive: (active: boolean) => void;
	triggerNotification: (message: string, title?: string) => boolean;
	triggerBell: (isError: boolean) => void;
	copyText: (text: string) => Promise<void>;
	quitAndDestroy: () => void;
	registerDispose?: (dispose: () => void | Promise<void>) => void;
	persistSession: boolean;
};

type ReadyState = {
	kind: "ready";
	runtime: AgentRuntime;
	commands: CommandRegistry;
	controller: ReturnType<typeof createComposerController>;
	attachments: ReturnType<typeof createAttachmentsController>;
	footer: ReturnType<typeof createFooterStatusController>;
	header: ReturnType<typeof createHeaderStatusController>;
	mcpWorkspace: ReturnType<typeof createMcpWorkspaceController>;
	releasesWorkspace: ReturnType<typeof createReleasesWorkspaceController>;
	reviewDrafts: ReturnType<typeof createReviewDraftController>;
	reviewWorkspace: ReturnType<typeof createReviewWorkspaceController>;
	scratchpad: ReturnType<typeof createScratchpadController>;
	subagentsWorkspace: ReturnType<typeof createSubagentsWorkspaceController>;
	app: ReturnType<typeof createAppState>;
	disposeAsync: () => Promise<void>;
};

type RootState =
	| ReadyState
	| { kind: "loading" }
	| { kind: "unauthenticated" }
	| { kind: "fatal"; error: string };

export function App(props: AppProps) {
	const [overlays, setOverlays] = createSignal<OverlayEntry[]>([]);
	const [transcriptViewport, setTranscriptViewport] =
		createSignal<TranscriptViewport | null>(null);
	const openCustomOverlay = createCustomOverlayHandler(setOverlays);
	const commands: CommandRegistry = createCommandRegistry(BUILT_IN_COMMANDS);

	let showToast: ((toast: ToastInput) => void) | null = null;
	const toast = (nextToast: ToastInput) => {
		showToast?.(nextToast);
	};

	const ui: InternalPluginUI = createPluginUI({
		toast,
		custom: openCustomOverlay,
		getTranscriptViewport: () => transcriptViewport(),
		getTheme: getCurrentThemeConfig,
	});

	async function buildReadyState(): Promise<ReadyState> {
		await refreshModelAvailability();
		let currentSettings = props.settings;
		const attachments = createAttachmentsController();
		const footer = createFooterStatusController();
		const header = createHeaderStatusController();
		const mcpWorkspace = createMcpWorkspaceController();
		const runtime = new AgentRuntime(props.session, {
			settings: currentSettings.settings,
		});
		const releasesWorkspace = createReleasesWorkspaceController();
		const reviewDrafts = createReviewDraftController(props.session.id);
		const reviewWorkspace = createReviewWorkspaceController();
		const scratchpad = createScratchpadController(
			runtime,
			props.persistSession ? undefined : createMemoryScratchpadStorage(),
		);
		const subagentsWorkspace = createSubagentsWorkspaceController();
		const memorySubagentParentStorage = props.persistSession
			? undefined
			: createMemorySubagentParentStorage();
		const memorySubagentStorage = props.persistSession
			? undefined
			: createMemorySubagentSessionStorage();
		const persistence = props.persistSession
			? new FilePersistence(runtime)
			: null;
		const app = createAppState(runtime);
		showToast = app.showToast;
		persistence?.onFailure((event) => {
			toast({
				title: "Session save failed",
				subtitle: event.error,
				variant: "error",
			});
		});

		const pluginContext = {
			runtime,
			commands,
			settings: currentSettings,
			ui,
			attachments,
			footer,
			header,
			triggerNotification: props.triggerNotification,
			triggerBell: props.triggerBell,
		};
		let pluginLoadGeneration = 0;
		let builtInReloadGeneration = 0;
		let builtInPluginManager: PluginManager | null = null;
		let externalPluginManager: ExternalPluginManager | null = null;
		let disposed = false;

		function disposeBuiltInPluginManager(): void {
			builtInReloadGeneration++;
			builtInPluginManager?.dispose();
			builtInPluginManager = null;
		}

		async function disposePluginManagers(): Promise<void> {
			pluginLoadGeneration++;
			disposeBuiltInPluginManager();
			const external = externalPluginManager;
			externalPluginManager = null;
			await external?.dispose();
		}

		function initializeBuiltInPlugins(): void {
			builtInPluginManager = new PluginManager(
				createBuiltInPlugins(pluginContext, {
					mcpWorkspace,
					releasesWorkspace,
					subagentsWorkspace,
					subagentParentStorage: memorySubagentParentStorage,
					subagentStorage: memorySubagentStorage,
				}),
				pluginContext,
			);
			builtInPluginManager.initialize();
		}

		async function initializeExternalPlugins(
			generation: number,
		): Promise<void> {
			let manager: ExternalPluginManager | null = null;
			try {
				manager = new ExternalPluginManager(pluginContext);
				externalPluginManager = manager;
				await manager.initialize();
				if (disposed || generation !== pluginLoadGeneration) {
					await manager.dispose();
					return;
				}
			} catch (error) {
				await manager?.dispose();
				if (disposed || generation !== pluginLoadGeneration) return;
				toast({
					title: "Plugin loading failed",
					subtitle: error instanceof Error ? error.message : String(error),
					variant: "error",
					persistent: true,
				});
			}
		}

		function startExternalPluginLoad(): void {
			const generation = ++pluginLoadGeneration;
			setTimeout(() => {
				if (disposed || generation !== pluginLoadGeneration) return;
				void initializeExternalPlugins(generation);
			}, 0);
		}

		function initializePlugins(): void {
			initializeBuiltInPlugins();
			startExternalPluginLoad();
		}

		try {
			await applyStartupModel(runtime, props.startupModel, {
				isKnown: (provider) => kitModels.getProvider(provider) !== undefined,
				isAuthenticated: hasCachedProviderAuth,
			});
			initializePlugins();
		} catch (error) {
			await disposePluginManagers();
			app.dispose();
			releasesWorkspace.dispose();
			scratchpad.dispose();
			persistence?.dispose();
			runtime.dispose();
			throw error;
		}

		async function reloadSettingsAndTheme(): Promise<void> {
			currentSettings = await loadSettings();
			pluginContext.settings = currentSettings;
			await resolveAndApplyTheme(currentSettings.settings.theme ?? "system");
			runtime.emitSettingsChanged(currentSettings.settings);
		}

		async function _reload(): Promise<void> {
			await disposePluginManagers();
			try {
				await reloadSettingsAndTheme();
				await runtime.reloadSession();
			} catch (error) {
				try {
					initializePlugins();
				} catch {
					await disposePluginManagers();
					// Preserve the original reload error below.
				}
				toast({
					title: "Reload failed",
					subtitle: error instanceof Error ? error.message : String(error),
					variant: "error",
				});
				return;
			}

			try {
				initializePlugins();
				toast({
					title: "Session reloaded",
					variant: "info",
				});
			} catch (error) {
				await disposePluginManagers();
				toast({
					title: "Reload failed",
					subtitle: error instanceof Error ? error.message : String(error),
					variant: "error",
				});
			}
		}

		const controller = createComposerController({
			runtime,
			persistSessions: props.persistSession,
			commands,
			fileIndex: app.fileIndex,
			threadIndex: app.threadIndex,
			attachments,
			reviewDrafts,
			reviewWorkspace,
			toast,
			_reload,
			openCustomOverlay,
		});

		const disposeTerminalTurnStatus = registerTerminalTurnStatus(
			runtime,
			props.setTerminalTurnActive,
		);
		let observedSessionId = runtime.getSession().id;
		runtime.subscribe({ prefix: "session.active.changed" }, (event) => {
			if (event.type === "session.active.changed") {
				const switchedSessions = event.session.id !== observedSessionId;
				observedSessionId = event.session.id;
				if (switchedSessions) {
					reviewDrafts.resetForSession(event.session.id);
					attachments.detach("code-review");
					// Overlays belong to the previous active session. Resolve them so
					// stale components unmount after a true session switch.
					for (const overlay of overlays()) overlay.resolve(undefined);
				}
				props.updateTerminalTitle(event.session.name, event.session.cwd);
				return;
			}
			initTemplates(event.cwd);
			disposeBuiltInPluginManager();
			const reloadGeneration = builtInReloadGeneration;
			setTimeout(() => {
				if (disposed || reloadGeneration !== builtInReloadGeneration) return;
				try {
					initializeBuiltInPlugins();
				} catch (error) {
					disposeBuiltInPluginManager();
					toast({
						title: "Plugin reload failed",
						subtitle: error instanceof Error ? error.message : String(error),
						variant: "error",
					});
				}
			}, 0);
		});

		let disposePromise: Promise<void> | null = null;
		const disposeAsync = (): Promise<void> => {
			if (disposePromise) return disposePromise;
			disposed = true;
			disposeTerminalTurnStatus();
			app.dispose();
			releasesWorkspace.dispose();
			scratchpad.dispose();
			persistence?.dispose();
			disposePromise = disposePluginManagers().finally(() => runtime.dispose());
			return disposePromise;
		};
		runtime.onQuit(() => {
			void disposeAsync().finally(() => props.quitAndDestroy());
		});

		return {
			kind: "ready",
			runtime,
			commands,
			controller,
			attachments,
			footer,
			header,
			mcpWorkspace,
			releasesWorkspace,
			reviewDrafts,
			reviewWorkspace,
			scratchpad,
			subagentsWorkspace,
			app,
			disposeAsync,
		};
	}

	async function buildRootState(): Promise<RootState> {
		try {
			return await buildReadyState();
		} catch (error) {
			if (
				error instanceof AuthenticationRequiredError ||
				error instanceof StartupModelAuthenticationRequiredError
			) {
				return { kind: "unauthenticated" };
			}
			return {
				kind: "fatal",
				error: error instanceof Error ? error.message : String(error),
			};
		}
	}

	const [root, setRoot] = createSignal<RootState>({ kind: "loading" });
	const pendingRootBuilds = new Set<Promise<RootState>>();
	let appDisposed = false;

	function buildTrackedRootState(): Promise<RootState> {
		const build = buildRootState();
		pendingRootBuilds.add(build);
		void build.finally(() => pendingRootBuilds.delete(build));
		return build;
	}

	async function disposeAppState() {
		appDisposed = true;
		const current = root();
		if (current.kind === "ready") await current.disposeAsync();
		const pending = await Promise.all([...pendingRootBuilds]);
		await Promise.all(
			pending.map((state) =>
				state.kind === "ready" ? state.disposeAsync() : Promise.resolve(),
			),
		);
	}

	props.registerDispose?.(disposeAppState);

	async function replaceRootState(next: RootState): Promise<void> {
		if (appDisposed) {
			if (next.kind === "ready") await next.disposeAsync();
			return;
		}
		const previous = root();
		if (previous.kind === "ready") await previous.disposeAsync();
		setRoot(next);
	}

	async function handleAuthenticated(providerName?: string): Promise<boolean> {
		const next = await buildTrackedRootState();
		if (appDisposed) {
			if (next.kind === "ready") await next.disposeAsync();
			return false;
		}
		if (next.kind === "ready") {
			await replaceRootState(next);
			next.app.showToast({
				title: "Login successful",
				subtitle: providerName
					? `Logged in to ${providerName}.`
					: "Credentials saved.",
				variant: "info",
			});
			return true;
		}
		if (next.kind === "fatal") {
			await replaceRootState(next);
			return false;
		}
		return false;
	}

	onMount(() => {
		void (async () => {
			const next = await buildTrackedRootState();
			if (appDisposed) {
				if (next.kind === "ready") await next.disposeAsync();
				return;
			}
			await replaceRootState(next);
		})();
	});

	onCleanup(() => {
		void disposeAppState();
		props.registerDispose?.(() => {});
	});

	return (
		<Switch>
			<Match when={root().kind === "ready"}>
				{(() => {
					const current = root();
					if (current.kind !== "ready") return null;
					return (
						<AppShell
							settings={current.runtime.settings}
							state={current.app.state}
							runtime={current.runtime}
							commands={current.commands}
							controller={current.controller}
							attachments={current.attachments}
							copyText={props.copyText}
							footer={current.footer}
							header={current.header}
							mcpWorkspace={current.mcpWorkspace}
							releasesWorkspace={current.releasesWorkspace}
							reviewDrafts={current.reviewDrafts}
							reviewWorkspace={current.reviewWorkspace}
							scratchpad={current.scratchpad}
							subagentsWorkspace={current.subagentsWorkspace}
							overlays={overlays}
							openOverlay={openCustomOverlay}
							dismissToast={current.app.dismissToast}
							onTranscriptViewportChange={setTranscriptViewport}
							showToast={current.app.showToast}
						/>
					);
				})()}
			</Match>
			<Match when={root().kind === "loading"}>
				<box flexGrow={1} alignItems="center" justifyContent="center">
					<text>Loading Kit…</text>
				</box>
			</Match>
			<Match when={root().kind === "fatal"}>
				{(() => {
					const current = root();
					return current.kind === "fatal" ? (
						<FatalScreen error={current.error} onQuit={props.quitAndDestroy} />
					) : null;
				})()}
			</Match>
			<Match when={root().kind === "unauthenticated"}>
				<AuthGateScreen
					session={props.session}
					onAuthenticated={handleAuthenticated}
					onQuit={props.quitAndDestroy}
				/>
			</Match>
		</Switch>
	);
}
