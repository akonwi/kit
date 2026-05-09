import { createSignal, onCleanup, Show } from "solid-js";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { ComposerInputMode } from "./ComposerDock";
import { GLYPH_ACTIVE, GLYPH_INACTIVE } from "./glyphs";
import { theme } from "./theme";

export type BottomStatusBarProps = {
	runtime: AgentRuntime;
	cwd: string;
	composerMode: ComposerInputMode;
};

export function BottomStatusBar(props: BottomStatusBarProps) {
	const [pendingMessageCount, setPendingMessageCount] = createSignal(
		props.runtime.getPendingMessageCount(),
	);
	const unsubscribePendingMessageCount = props.runtime.subscribe(
		"chat.message-queue.changed",
		(e) => setPendingMessageCount(e.count),
	);

	const pending = () =>
		pendingMessageCount() > 0 ? `queue:${pendingMessageCount()}` : "";
	const composerModeLabel = () => {
		switch (props.composerMode) {
			case "bash":
				return "bash command · result will be added to context";
			case "bash-excluded":
				return "bash command · result excluded from context";
			default:
				return "";
		}
	};
	const leftText = () => composerModeLabel() || pending();
	const leftColor = () =>
		props.composerMode === "bash"
			? theme.composerBashBorder
			: props.composerMode === "bash-excluded"
				? theme.composerBashExcludedBorder
				: theme.textMuted;
	const [vcs, setVcs] = createSignal(props.runtime.vcsInfo);
	const unsubscribeVcs = props.runtime.subscribe("vcs.updated", (e) =>
		setVcs(e),
	);
	const branch = () => vcs().branch;
	const location = () =>
		branch() != null
			? `${props.cwd} (${branch()}${vcs().dirty ? ` ${GLYPH_ACTIVE}` : ` ${GLYPH_INACTIVE}`})`
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
			<Show when={leftText()} fallback={<text />}>
				<text fg={leftColor()}>{leftText()}</text>
			</Show>
			<text fg={theme.textMuted}>{location()}</text>
		</box>
	);
}
