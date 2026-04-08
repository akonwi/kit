import type { AgentRuntime } from "../runtime/agent-runtime";
import type { Session } from "../session";
import type { LoadedSettings } from "../settings";
import { AppShell } from "../shell/AppShell";
import { createComposerController } from "../shell/composer-controller";
import { createAppState } from "../state/app-state";

export type AppProps = {
	settings: LoadedSettings;
	session: Session | null;
	runtime: AgentRuntime;
	updateTerminalTitle: (sessionName: string | undefined, cwd: string) => void;
};

export function App(props: AppProps) {
	const app = createAppState(
		props.settings,
		props.runtime.getSession(),
		props.runtime,
	);

	const controller = createComposerController({
		runtime: props.runtime,
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
			dismissToast={app.dismissToast}
		/>
	);
}
