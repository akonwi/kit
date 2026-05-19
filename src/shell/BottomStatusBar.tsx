import { createSignal, onCleanup, Show } from "solid-js";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { ComposerInputMode } from "./ComposerDock";
import type { FooterStatusController } from "./footer-status";
import { MIDDLE_DOT } from "./glyphs";
import { theme } from "./theme";

export type BottomStatusBarProps = {
	runtime: AgentRuntime;
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
				return ["bash command", "result will be added to context"].join(
					` ${MIDDLE_DOT} `,
				);
			case "bash-excluded":
				return ["bash command", "result excluded from context"].join(
					` ${MIDDLE_DOT} `,
				);
			default:
				return "";
		}
	};
	const leftColor = () =>
		props.composerMode === "bash"
			? theme.composerBashBorder
			: props.composerMode === "bash-excluded"
				? theme.composerBashExcludedBorder
				: theme.textMuted;
	const [footerContributions, setFooterContributions] = createSignal(
		props.status.getContributions(),
	);
	const unsubscribeStatus = props.status.subscribe(() =>
		setFooterContributions(props.status.getContributions()),
	);
	const contributionLabels = (side: "left" | "right") =>
		footerContributions()
			.filter((contribution) => contribution.side === side)
			.map((contribution) => contribution.label);
	const leftText = () =>
		[composerModeLabel() || pending(), ...contributionLabels("left")]
			.filter(Boolean)
			.join(` ${MIDDLE_DOT} `);
	const rightText = () => contributionLabels("right").join(` ${MIDDLE_DOT} `);

	onCleanup(() => {
		unsubscribePendingMessageCount();
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
			flexWrap="wrap"
			justifyContent="space-between"
			gap={1}
		>
			<box flexGrow={1} flexShrink={0} maxWidth="100%" overflow="hidden">
				<text fg={leftColor()}>{leftText() || " "}</text>
			</box>
			<Show when={rightText()}>
				<box
					flexShrink={0}
					maxWidth="100%"
					overflow="hidden"
					justifyContent="flex-end"
				>
					<text fg={theme.textMuted}>{rightText()}</text>
				</box>
			</Show>
		</box>
	);
}
