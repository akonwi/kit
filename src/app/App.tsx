import { createSignal, onCleanup } from "solid-js";
import {
	BUILT_IN_COMMANDS,
	type CommandRegistry,
	createCommandRegistry,
} from "../features/commands";
import type { GuidedQuestionsController } from "../features/guided-questions";
import type { PagerController } from "../features/pager";
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
	pager: PagerController;
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
	void pluginManager.initialize();
	onCleanup(() => {
		void pluginManager.dispose();
	});

	const controller = createComposerController({
		runtime: props.runtime,
		guidedQuestions: props.guidedQuestions,
		pager: props.pager,
		commands,
		fileIndex: app.fileIndex,
		threadIndex: app.threadIndex,
	});

	props.runtime.subscribe((event) => {
		if (event.type === "session_changed") {
			props.updateTerminalTitle((event.session as Session).name, process.cwd());
		}
		// Auto-open the pager when a long structured response arrives.
		if (event.type === "turn_complete" && !props.pager.active) {
			props.pager.tryActivate(props.runtime.getMessages());
		}
	});

	return (
		<AppShell
			state={app.state}
			controller={controller}
			guidedQuestions={props.guidedQuestions}
			pager={props.pager}
			overlays={overlays}
			dismissToast={app.dismissToast}
		/>
	);
}
