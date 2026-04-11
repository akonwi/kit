import { createSignal, onCleanup } from "solid-js";
import {
	BUILT_IN_COMMANDS,
	type CommandRegistry,
	createCommandRegistry,
} from "../features/commands";
import { GUIDED_QUESTIONS_POLICY } from "../features/guided-questions";
import {
	BUILT_IN_PLUGIN_CLASSES,
	PluginManager,
	type PluginUI,
} from "../plugins";
import { AgentRuntime } from "../runtime/agent-runtime";
import type { Session } from "../session";
import type { LoadedSettings } from "../settings";
import { AppShell } from "../shell/AppShell";
import { createComposerController } from "../shell/composer-controller";
import { createAppState } from "../state/app-state";
import { createCustomOverlayHandler, type OverlayEntry } from "./overlay-ui";

export type AppProps = {
	settings: LoadedSettings;
	session: Session;
	updateTerminalTitle: (sessionName: string | undefined, cwd: string) => void;
};

export function App(props: AppProps) {
	const [overlays, setOverlays] = createSignal<OverlayEntry[]>([]);

	// Toast handler - will be connected after app state is created
	let showToast:
		| ((toast: {
				title: string;
				lines: string[];
				variant: "info" | "error";
		  }) => void)
		| null = null;

	// Create UI for plugins
	const ui: PluginUI = {
		notify: (message, variant = "info") => {
			showToast?.({
				title: message,
				lines: [],
				variant: variant === "warning" ? "info" : variant,
			});
		},
		custom: createCustomOverlayHandler(setOverlays),
	};

	// Create command registry
	const commands: CommandRegistry = createCommandRegistry(BUILT_IN_COMMANDS);

	// Create runtime first (plugins need it)
	const runtime = new AgentRuntime(props.session, {
		extraTools: [], // Plugins will add tools via registerTool()
		systemPromptAdditions: [GUIDED_QUESTIONS_POLICY],
	});

	// Create plugin manager and initialize plugins
	const pluginManager = new PluginManager(BUILT_IN_PLUGIN_CLASSES, {
		runtime,
		commands,
		settings: props.settings,
		ui,
	});
	pluginManager.initialize();

	// Create app state (provides showToast implementation)
	const app = createAppState(props.settings, props.session, runtime);
	showToast = app.showToast;

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
			overlays={overlays}
			dismissToast={app.dismissToast}
		/>
	);
}
