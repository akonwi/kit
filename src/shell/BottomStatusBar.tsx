import { createSignal, onCleanup, Show } from "solid-js";
import type { FooterStatusState } from "../state/app-state";
import { theme } from "./theme";
import { AgentRuntime } from "../runtime/agent-runtime";

export type BottomStatusBarProps = {
  runtime: AgentRuntime
	status: FooterStatusState;
	cwd: string
};

export function BottomStatusBar(props: BottomStatusBarProps) {
	const pending = () =>
		props.status.pendingMessages > 0 ? `📬${props.status.pendingMessages}` : "";
	const [vcs, setVcs] = createSignal(props.runtime.vcsInfo)
	const unsubscribeVcs = props.runtime.subscribe("vcs.updated", e => setVcs(e))
	const branch = () => vcs().branch
	const location = () =>
		branch() != null
			? `${props.cwd} (${branch()}${vcs().dirty ? " ●" : " ○"})`
			: props.cwd;

  onCleanup(() => {
    unsubscribeVcs()
	})

	return (
		<box
			flexShrink={0}
			border
			borderColor={theme.borderStatus}
			paddingX={1}
			width="100%"
			flexDirection="row"
			justifyContent="space-between"
		>
			<Show when={pending()} fallback={<text />}>
				<text fg={theme.textMuted}>{pending()}</text>
			</Show>
			<text fg={theme.textMuted}>{location()}</text>
		</box>
	);
}
