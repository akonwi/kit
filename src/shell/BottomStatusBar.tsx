import { createSignal, onCleanup, Show } from "solid-js";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { FooterStatusState } from "../state/app-state";
import { theme } from "./theme";

export type BottomStatusBarProps = {
	runtime: AgentRuntime;
	cwd: string;
};

export function BottomStatusBar(props: BottomStatusBarProps) {
	const [pendingMessageCount, setPendingMessageCount] = createSignal(
		props.runtime.getPendingMessageCount(),
	);
	const unsubscribePendingMessageCount = props.runtime.subscribe(
		"runtime.pending.changed",
		(e) => setPendingMessageCount(e.count),
	);

	const pending = () =>
		pendingMessageCount() > 0 ? `📬${pendingMessageCount()}` : "";
	const [vcs, setVcs] = createSignal(props.runtime.vcsInfo);
	const unsubscribeVcs = props.runtime.subscribe("vcs.updated", (e) =>
		setVcs(e),
	);
	const branch = () => vcs().branch;
	const location = () =>
		branch() != null
			? `${props.cwd} (${branch()}${vcs().dirty ? " ●" : " ○"})`
			: props.cwd;

	onCleanup(() => {
		unsubscribePendingMessageCount();
		unsubscribeVcs();
	});

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
