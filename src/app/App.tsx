import { createSignal, Match, onCleanup, onMount, Switch } from "solid-js";
import {
	BUILT_IN_COMMANDS,
	type CommandRegistry,
	createCommandRegistry,
} from "../features/commands";
import { createBuiltInPlugins } from "../plugins/built-ins";
import {
	type ExternalPluginFailure,
	loadExternalPlugins,
} from "../plugins/external";
import { PluginManager } from "../plugins/PluginManager";
import type { InternalPluginUI, TranscriptViewport } from "../plugins/types";
import {
	AgentRuntime,
	AuthenticationRequiredError,
} from "../runtime/agent-runtime";
import type { Session } from "../session";
import { type LoadedSettings, loadSettings } from "../settings";
import { AppShell } from "../shell/AppShell";
import { createAttachmentsController } from "../shell/attachments-controller";
import { createComposerController } from "../shell/composer-controller";
import { createFooterStatusController } from "../shell/footer-status";
import { createHeaderStatusController } from "../shell/header-status";
import { getCurrentThemeConfig, resolveAndApplyTheme } from "../shell/theme";
import { createAppState } from "../state/app-state";
import type { ToastInput } from "../state/toasts";
import { FilePersistence } from "../storage/file-persistence";
import { AuthGateScreen } from "./AuthGateScreen";
import { FatalScreen } from "./FatalScreen";
import { createCustomOverlayHandler, type OverlayEntry } from "./overlay-ui";
import { createPluginUI } from "./plugin-ui";

export type AppProps = {
	settings: LoadedSettings;
	session: Session;
	updateTerminalTitle: (sessionName: string | undefined, cwd: string) => void;
	quitAndDestroy: () => void;
};

type ReadyState = {
	kind: "ready";
	runtime: AgentRuntime;
	controller: ReturnType<typeof createComposerController>;
	attachments: ReturnType<typeof createAttachmentsController>;
	footer: ReturnType<typeof createFooterStatusController>;
	header: ReturnType<typeof createHeaderStatusController>;
	app: ReturnType<typeof createAppState>;
	dispose: () => void;
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
		let currentSettings = props.settings;
		const attachments = createAttachmentsController();
		const footer = createFooterStatusController();
		const header = createHeaderStatusController();
		const runtime = new AgentRuntime(props.session, {
			settings: currentSettings.settings,
		});
		const persistence = new FilePersistence(runtime);
		const app = createAppState(runtime);
		showToast = app.showToast;
		persistence.onFailure((event) => {
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
		};
		let pluginReloadCount = 0;
		let pluginManager: PluginManager | null = null;

		function disposePluginManager(manager: PluginManager | null): void {
			manager?.dispose();
		}

		function showPluginFailures(failures: ExternalPluginFailure[]): void {
			if (failures.length === 0) return;
			const firstFailure = formatPluginFailure(failures[0]);
			const remaining = failures.length - 1;
			toast({
				title:
					failures.length === 1
						? "Plugin failed to load"
						: `${failures.length} plugins failed to load`,
				subtitle:
					remaining > 0
						? `${firstFailure} · ${remaining} more failure${remaining === 1 ? "" : "s"}`
						: firstFailure,
				variant: "error",
				persistent: true,
			});
		}

		async function createPluginManager(
			failures: ExternalPluginFailure[],
		): Promise<PluginManager> {
			const external = await loadExternalPlugins(runtime.getSession().cwd, {
				reloadId: `${Date.now()}-${pluginReloadCount++}`,
				onFailure: (failure) => failures.push(failure),
			});
			return new PluginManager(
				[...createBuiltInPlugins(pluginContext), ...external.plugins],
				pluginContext,
			);
		}

		async function initializePluginManager(): Promise<ExternalPluginFailure[]> {
			const failures: ExternalPluginFailure[] = [];
			pluginManager = await createPluginManager(failures);
			pluginManager.initialize();
			return failures;
		}

		try {
			showPluginFailures(await initializePluginManager());
		} catch (error) {
			disposePluginManager(pluginManager);
			persistence.dispose();
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
			disposePluginManager(pluginManager);
			try {
				await reloadSettingsAndTheme();
				await runtime.reloadSession();
			} catch (error) {
				try {
					showPluginFailures(await initializePluginManager());
				} catch {
					disposePluginManager(pluginManager);
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
				showPluginFailures(await initializePluginManager());
				toast({
					title: "Session reloaded",
					variant: "info",
				});
			} catch (error) {
				disposePluginManager(pluginManager);
				toast({
					title: "Reload failed",
					subtitle: error instanceof Error ? error.message : String(error),
					variant: "error",
				});
			}
		}

		const controller = createComposerController({
			runtime,
			commands,
			fileIndex: app.fileIndex,
			threadIndex: app.threadIndex,
			attachments,
			toast,
			_reload,
			openCustomOverlay,
		});

		runtime.subscribe("session.active.changed", (event) => {
			props.updateTerminalTitle(event.session.name, process.cwd());
		});

		let disposed = false;
		const dispose = () => {
			if (disposed) return;
			disposed = true;
			disposePluginManager(pluginManager);
			persistence.dispose();
			runtime.dispose();
		};

		runtime.onQuit(() => {
			dispose();
			props.quitAndDestroy();
		});

		return {
			kind: "ready",
			runtime,
			controller,
			attachments,
			footer,
			header,
			app,
			dispose,
		};
	}

	async function buildRootState(): Promise<RootState> {
		try {
			return await buildReadyState();
		} catch (error) {
			if (error instanceof AuthenticationRequiredError) {
				return { kind: "unauthenticated" };
			}
			return {
				kind: "fatal",
				error: error instanceof Error ? error.message : String(error),
			};
		}
	}

	function formatPluginFailure(failure: ExternalPluginFailure): string {
		return `${failure.filePath} (${failure.phase}): ${failure.message}`;
	}

	const [root, setRoot] = createSignal<RootState>({ kind: "loading" });

	function replaceRootState(next: RootState) {
		const previous = root();
		if (previous.kind === "ready") {
			previous.dispose();
		}
		setRoot(next);
	}

	async function handleAuthenticated(providerName?: string): Promise<boolean> {
		const next = await buildRootState();
		if (next.kind === "ready") {
			replaceRootState(next);
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
			replaceRootState(next);
			return false;
		}
		return false;
	}

	onMount(() => {
		void (async () => {
			replaceRootState(await buildRootState());
		})();
	});

	onCleanup(() => {
		const current = root();
		if (current.kind === "ready") {
			current.dispose();
		}
	});

	return (
		<Switch>
			<Match when={root().kind === "ready"}>
				{(() => {
					const current = root();
					if (current.kind !== "ready") return null;
					return (
						<AppShell
							state={current.app.state}
							runtime={current.runtime}
							controller={current.controller}
							attachments={current.attachments}
							footer={current.footer}
							header={current.header}
							overlays={overlays}
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
