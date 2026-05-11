import { createSignal, Match, onCleanup, Switch } from "solid-js";
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
import type { PluginUI, TranscriptViewport } from "../plugins/types";
import {
	AgentRuntime,
	AuthenticationRequiredError,
} from "../runtime/agent-runtime";
import type { Session } from "../session";
import type { LoadedSettings } from "../settings";
import { AppShell } from "../shell/AppShell";
import { createAttachmentsController } from "../shell/attachments-controller";
import { createComposerController } from "../shell/composer-controller";
import { createAppState } from "../state/app-state";
import type { ToastInput } from "../state/toasts";
import { FilePersistence } from "../storage/file-persistence";
import { AuthGateScreen } from "./AuthGateScreen";
import { FatalScreen } from "./FatalScreen";
import { createCustomOverlayHandler, type OverlayEntry } from "./overlay-ui";

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
	app: ReturnType<typeof createAppState>;
	dispose: () => void;
};

type RootState =
	| ReadyState
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

	const ui: PluginUI = {
		toast,
		custom: openCustomOverlay,
		getTranscriptViewport: () => transcriptViewport(),
	};

	function buildReadyState(): ReadyState {
		const attachments = createAttachmentsController();
		const runtime = new AgentRuntime(props.session, {
			settings: props.settings.settings,
		});
		const persistence = new FilePersistence(runtime);
		const app = createAppState(runtime);
		showToast = app.showToast;
		persistence.onFailure((event) => {
			toast({
				title: "Session save failed",
				lines: [event.error],
				variant: "error",
			});
		});

		const pluginContext = {
			runtime,
			commands,
			settings: props.settings,
			ui,
			attachments,
		};
		let pluginReloadCount = 0;
		let pluginManager: PluginManager | null = null;

		function disposePluginManager(manager: PluginManager | null): void {
			manager?.dispose();
		}

		function showPluginFailures(failures: ExternalPluginFailure[]): void {
			if (failures.length === 0) return;
			const visibleFailures = failures.slice(0, 5).map(formatPluginFailure);
			const remaining = failures.length - visibleFailures.length;
			toast({
				title:
					failures.length === 1
						? "Plugin failed to load"
						: `${failures.length} plugins failed to load`,
				lines:
					remaining > 0
						? [...visibleFailures, `...and ${remaining} more.`]
						: visibleFailures,
				variant: "error",
				persistent: true,
			});
		}

		function createPluginManager(
			failures: ExternalPluginFailure[],
		): PluginManager {
			const external = loadExternalPlugins(runtime.getSession().cwd, {
				reloadId: `${Date.now()}-${pluginReloadCount++}`,
				onFailure: (failure) => failures.push(failure),
			});
			return new PluginManager(
				[...createBuiltInPlugins(pluginContext), ...external.plugins],
				pluginContext,
			);
		}

		function initializePluginManager(): ExternalPluginFailure[] {
			const failures: ExternalPluginFailure[] = [];
			pluginManager = createPluginManager(failures);
			pluginManager.initialize();
			return failures;
		}

		try {
			showPluginFailures(initializePluginManager());
		} catch (error) {
			disposePluginManager(pluginManager);
			persistence.dispose();
			runtime.dispose();
			throw error;
		}

		async function _reload(): Promise<void> {
			disposePluginManager(pluginManager);
			try {
				await runtime.reloadSession();
			} catch (error) {
				try {
					showPluginFailures(initializePluginManager());
				} catch {
					disposePluginManager(pluginManager);
					// Preserve the original reload error below.
				}
				toast({
					title: "Reload failed",
					lines: [error instanceof Error ? error.message : String(error)],
					variant: "error",
				});
				return;
			}

			try {
				showPluginFailures(initializePluginManager());
				toast({
					title: "Session reloaded",
					lines: ["Reloaded session context and plugin state."],
					variant: "info",
				});
			} catch (error) {
				disposePluginManager(pluginManager);
				toast({
					title: "Reload failed",
					lines: [error instanceof Error ? error.message : String(error)],
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
			app,
			dispose,
		};
	}

	function buildRootState(): RootState {
		try {
			return buildReadyState();
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

	const [root, setRoot] = createSignal<RootState>(buildRootState());

	function replaceRootState(next: RootState) {
		const previous = root();
		if (previous.kind === "ready") {
			previous.dispose();
		}
		setRoot(next);
	}

	async function handleAuthenticated(providerName?: string): Promise<boolean> {
		const next = buildRootState();
		if (next.kind === "ready") {
			replaceRootState(next);
			next.app.showToast({
				title: "Login successful",
				lines: [
					providerName ? `Logged in to ${providerName}.` : "Credentials saved.",
				],
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
							overlays={overlays}
							dismissToast={current.app.dismissToast}
							onTranscriptViewportChange={setTranscriptViewport}
							showToast={current.app.showToast}
						/>
					);
				})()}
			</Match>
			<Match when={root().kind === "fatal"}>
				{(() => {
					const current = root();
					return current.kind === "fatal" ? (
						<FatalScreen error={current.error} onQuit={props.quitAndDestroy} />
					) : null;
				})()}
			</Match>
			<Match when={true}>
				<AuthGateScreen
					session={props.session}
					onAuthenticated={handleAuthenticated}
					onQuit={props.quitAndDestroy}
				/>
			</Match>
		</Switch>
	);
}
