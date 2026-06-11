import { createSignal, onCleanup } from "solid-js";
import type { AgentRuntime } from "../runtime/agent-runtime";
import { ChromeContributionLine } from "./ChromeContributionLine";
import type { ComposerInputMode } from "./ComposerDock";
import type { ChromeContribution } from "./chrome-contributions";
import { createChromeTextContent } from "./chrome-contributions";
import type { FooterStatusController } from "./footer-status";
import { ARROW_UP, MIDDLE_DOT } from "./glyphs";
import { theme } from "./theme";

export type BottomStatusBarProps = {
	runtime: AgentRuntime;
	status: FooterStatusController;
	composerMode: ComposerInputMode;
};

function builtInContribution(input: {
	id: string;
	label: string;
	side: "left" | "right";
}): ChromeContribution {
	return {
		id: input.id,
		content: createChromeTextContent(input.label),
		plainText: input.label,
		side: input.side,
	};
}

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
			? `queued messages: ${pendingMessageCount()} ${MIDDLE_DOT} Alt+Q edit ${MIDDLE_DOT} ${ARROW_UP} restore`
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
	const builtInLeftContribution = (): ChromeContribution | null => {
		const label = composerModeLabel() || pending();
		return label
			? builtInContribution({
					id: "BottomStatusBar:status",
					label,
					side: "left",
				})
			: null;
	};
	const leftContributions = () => {
		const builtIn = builtInLeftContribution();
		return [
			...(builtIn ? [builtIn] : []),
			...footerContributions().filter(
				(contribution) => contribution.side === "left",
			),
		];
	};
	const rightContributions = () =>
		footerContributions().filter(
			(contribution) => contribution.side === "right",
		);

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
				<ChromeContributionLine
					contributions={leftContributions()}
					fg={leftColor()}
				/>
			</box>
			<box
				flexShrink={0}
				maxWidth="100%"
				overflow="hidden"
				justifyContent="flex-end"
			>
				<ChromeContributionLine
					contributions={rightContributions()}
					fg={theme.textMuted}
					fallback=""
				/>
			</box>
		</box>
	);
}
