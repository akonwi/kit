import { createSignal, onCleanup } from "solid-js";
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
import { AgentRuntime } from "../runtime/agent-runtime";
import type { Session } from "../session";
import type { LoadedSettings } from "../settings";
import { AppShell } from "../shell/AppShell";
import { createAttachmentsController } from "../shell/attachments-controller";
import { createComposerController } from "../shell/composer-controller";
import { createAppState } from "../state/app-state";
import { createCustomOverlayHandler, type OverlayEntry } from "./overlay-ui";

export type AppProps = {
	settings: LoadedSettings;
	session: Session;
	updateTerminalTitle: (sessionName: string | undefined, cwd: string) => void;
	quitAndDestroy: () => void;
};

export function App(props: AppProps) {
	const [overlays, setOverlays] = createSignal<OverlayEntry[]>([]);

	// Toast handler - will be connected after app state is created
	let showToast:
		| ((toast: {
				title: string;
				lines: string[];
				variant: "info" | "warning" | "error";
		  }) => void)
		| null = null;

	const openCustomOverlay = createCustomOverlayHandler(setOverlays);

	// Create UI for plugins
	const ui: PluginUI = {
		notify: (message, variant = "info") => {
			showToast?.({
				title: message,
				lines: [],
				variant,
			});
		},
		custom: openCustomOverlay,
	};

	// Create command registry
	const commands: CommandRegistry = createCommandRegistry(BUILT_IN_COMMANDS);
	const attachments = createAttachmentsController();

	// Create runtime first (plugins need it).
	// Plugins add their own tools and system prompt additions in initialize().
	const runtime = new AgentRuntime(props.session, {
		settings: props.settings.settings,
	});

	// Create plugin manager and initialize plugins
	const pluginManager = new PluginManager(BUILT_IN_PLUGIN_CLASSES, {
		runtime,
		commands,
		settings: props.settings,
		ui,
		attachments,
	});
	pluginManager.initialize();

	// Create app state (provides showToast implementation)
	const app = createAppState(props.settings, props.session, runtime);
	showToast = app.showToast;

	runtime.onQuit(() => {
		pluginManager.dispose();
		runtime.dispose();
		props.quitAndDestroy();
	});

	onCleanup(() => {
		pluginManager.dispose();
		runtime.dispose();
	});

	// Create composer controller
	const controller = createComposerController({
		runtime,
		commands,
		fileIndex: app.fileIndex,
		threadIndex: app.threadIndex,
		attachments,
		openCustomOverlay,
	});

	// Update terminal title on session change
	runtime.subscribe((event) => {
		if (event.type === "session_changed") {
			props.updateTerminalTitle((event.session as Session).name, process.cwd());
		}
	});

	return (
		<AppShell
			state={app.state}
			controller={controller}
			attachments={attachments}
			overlays={overlays}
			dismissToast={app.dismissToast}
			showToast={app.showToast}
		/>
	);
}
