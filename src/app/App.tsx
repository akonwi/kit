import { createSignal, onCleanup } from "solid-js";
import {
	BUILT_IN_COMMANDS,
	type CommandRegistry,
	createCommandRegistry,
} from "../features/commands";
import type { GuidedQuestionsController } from "../features/guided-questions";
import {
	BUILT_IN_PLUGIN_CLASSES,
	PluginManager,
	type PluginUI,
} from "../plugins";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { Session } from "../session";
import type { LoadedSettings } from "../settings";
import { AppShell } from "../shell/AppShell";
import { createComposerController } from "../shell/composer-controller";
import { createAppState } from "../state/app-state";
import { createCustomOverlayHandler, type OverlayEntry } from "./overlay-ui";

export type AppProps = {
	settings: LoadedSettings;
	session: Session | null;
	runtime: AgentRuntime;
	guidedQuestions: GuidedQuestionsController;
	updateTerminalTitle: (sessionName: string | undefined, cwd: string) => void;
};

export function App(props: AppProps) {
	const app = createAppState(
		props.settings,
		props.runtime.getSession(),
		props.runtime,
	);

	const [overlays, setOverlays] = createSignal<OverlayEntry[]>([]);

	const ui: PluginUI = {
		notify: (
			message: string,
			variant: "info" | "warning" | "error" = "info",
		) => {
			app.showToast({
				title: message,
				lines: [],
				variant: variant === "warning" ? "info" : variant,
			});
		},
		custom: createCustomOverlayHandler(setOverlays),
	};

	const commands: CommandRegistry = createCommandRegistry(BUILT_IN_COMMANDS);
	const pluginManager = new PluginManager(BUILT_IN_PLUGIN_CLASSES, {
		runtime: props.runtime,
		commands,
		settings: props.settings,
		ui,
	});
	pluginManager.initialize();
	onCleanup(() => {
		pluginManager.dispose();
	});

	const controller = createComposerController({
		runtime: props.runtime,
		guidedQuestions: props.guidedQuestions,
		commands,
		fileIndex: app.fileIndex,
		threadIndex: app.threadIndex,
	});

	props.runtime.subscribe((event) => {
		if (event.type === "session_changed") {
			props.updateTerminalTitle((event.session as Session).name, process.cwd());
		}
	});

	return (
		<AppShell
			state={app.state}
			controller={controller}
			guidedQuestions={props.guidedQuestions}
			overlays={overlays}
			dismissToast={app.dismissToast}
		/>
	);
}
