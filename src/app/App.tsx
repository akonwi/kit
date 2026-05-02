import { createSignal, Match, onCleanup, Switch } from "solid-js";
import {
	BUILT_IN_COMMANDS,
	type CommandRegistry,
	createCommandRegistry,
} from "../features/commands";
import {
	BUILT_IN_PLUGIN_CLASSES,
	PluginManager,
	type PluginUI,
} from "../plugins";
import type { TranscriptViewport } from "../plugins/types";
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

	let showToast:
		| ((toast: {
				title: string;
				lines: string[];
				variant: "info" | "warning" | "error";
		  }) => void)
		| null = null;

	const ui: PluginUI = {
		notify: (message, variant = "info") => {
			showToast?.({
				title: message,
				lines: [],
				variant,
			});
		},
		custom: openCustomOverlay,
		getTranscriptViewport: () => transcriptViewport(),
	};

	function buildReadyState(): ReadyState {
		const attachments = createAttachmentsController();
		const runtime = new AgentRuntime(props.session, {
			settings: props.settings.settings,
		});
		const app = createAppState(runtime);
		showToast = app.showToast;

		const pluginManager = new PluginManager(BUILT_IN_PLUGIN_CLASSES, {
			runtime,
			commands,
			settings: props.settings,
			ui,
			attachments,
		});

		try {
			pluginManager.initialize();
		} catch (error) {
			pluginManager.dispose();
			runtime.dispose();
			throw error;
		}

		async function _reload(): Promise<void> {
			pluginManager.dispose();
			try {
				await runtime.reloadSession();
			} catch (error) {
				pluginManager.initialize();
				showToast?.({
					title: "Reload failed",
					lines: [error instanceof Error ? error.message : String(error)],
					variant: "error",
				});
				return;
			}

			try {
				pluginManager.initialize();
				showToast?.({
					title: "Session reloaded",
					lines: ["Reloaded session context and plugin state."],
					variant: "info",
				});
			} catch (error) {
				pluginManager.dispose();
				showToast?.({
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
			pluginManager.dispose();
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
