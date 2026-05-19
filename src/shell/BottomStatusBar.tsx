import { createSignal, onCleanup, Show } from "solid-js";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { ComposerInputMode } from "./ComposerDock";
import type { FooterStatusController } from "./footer-status";
import { MIDDLE_DOT } from "./glyphs";
import { theme } from "./theme";

export type BottomStatusBarProps = {
	runtime: AgentRuntime;
	cwd: string;
	status: FooterStatusController;
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
		pendingMessageCount() > 0
			? `queued messages: ${pendingMessageCount()}`
			: "";
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
	const [vcsContributions, setVcsContributions] = createSignal(
		props.status.getVcsContributions(),
	);
	const unsubscribeVcs = props.runtime.subscribe("vcs.updated", (e) =>
		setVcs(e),
	);
	const unsubscribeStatus = props.status.subscribe(() =>
		setVcsContributions(props.status.getVcsContributions()),
	);
	const branch = () => vcs().branch;
	const vcsLabel = () => {
		const currentBranch = branch();
		if (currentBranch == null) return null;
		return [
			`${currentBranch}${vcs().dirty ? "*" : ""}`,
			...vcsContributions().map((contribution) => contribution.label),
		].join(` ${MIDDLE_DOT} `);
	};
	const location = () => {
		const label = vcsLabel();
		return label ? `${props.cwd} (${label})` : props.cwd;
	};

	onCleanup(() => {
		unsubscribePendingMessageCount();
		unsubscribeVcs();
		unsubscribeStatus();
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
