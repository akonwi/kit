import { createSignal, onCleanup } from "solid-js";
import type { AgentRuntime } from "../runtime/agent-runtime";
import { ChromeContributionLine } from "./ChromeContributionLine";
import {
	type ChromeContribution,
	createChromeTextContent,
} from "./chrome-contributions";
import type { HeaderStatusController } from "./header-status";
import { ScreenHeader } from "./ScreenHeader";
import { theme } from "./theme";

function progressColor(pct: number): string {
	if (pct > 90) return theme.progressCritical;
	if (pct >= 80) return theme.progressWarning;
	return theme.progressNormal;
}

export const HEADER_CONTRIBUTION_IDS = {
	title: "HeaderBar:title",
	model: "HeaderBar:model",
} as const;

export type HeaderBarProps = {
	sessionName: string | undefined;
	onHeightChange?: (height: number) => void;
	runtime: AgentRuntime;
	header: HeaderStatusController;
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

export function HeaderBar(props: HeaderBarProps) {
	const [contextStats, setContextStats] = createSignal(
		props.runtime.contextStats,
	);
	const refreshContextStats = () => setContextStats(props.runtime.contextStats);
	const unsubscribeTurns = props.runtime.subscribe(
		"agent.turn.completed",
		refreshContextStats,
	);
	const unsubscribeSessionChange = props.runtime.subscribe(
		"session.active.changed",
		refreshContextStats,
	);
	const unsubscribeCompactionCompleted = props.runtime.subscribe(
		{ prefix: "session.compaction.completed" },
		refreshContextStats,
	);

	const contextUsage = () => contextStats()?.percent ?? 0;

	const [agentInfo, setAgentInfo] = createSignal(props.runtime.agentInfo);
	const unsubscribeAgentInfo = props.runtime.subscribe(
		"agent.model.changed",
		(e) => {
			setAgentInfo(e);
			refreshContextStats();
		},
	);
	const [headerContributions, setHeaderContributions] = createSignal(
		props.header.getContributions(),
	);
	const unsubscribeHeader = props.header.subscribe(() =>
		setHeaderContributions(props.header.getContributions()),
	);
	const builtInContributions = (): ChromeContribution[] => [
		builtInContribution({
			id: HEADER_CONTRIBUTION_IDS.title,
			label: props.sessionName || "Unnamed session",
			side: "left",
		}),
		builtInContribution({
			id: HEADER_CONTRIBUTION_IDS.model,
			label: `${agentInfo().model?.name ?? "model?"} (${agentInfo().thinkingLevel})`,
			side: "right",
		}),
	];
	const contributions = (side: "left" | "right") => [
		...builtInContributions().filter(
			(contribution) =>
				contribution.side === side && !props.header.isHidden(contribution.id),
		),
		...headerContributions().filter(
			(contribution) => contribution.side === side,
		),
	];

	onCleanup(() => {
		unsubscribeTurns();
		unsubscribeAgentInfo();
		unsubscribeSessionChange();
		unsubscribeCompactionCompleted();
		unsubscribeHeader();
	});

	return (
		<ScreenHeader
			left={
				<ChromeContributionLine
					contributions={contributions("left")}
					fg={theme.textMuted}
				/>
			}
			right={
				<ChromeContributionLine
					contributions={contributions("right")}
					fg={theme.textMuted}
				/>
			}
			progress={contextUsage()}
			progressColor={progressColor(contextUsage())}
			onHeightChange={props.onHeightChange}
		/>
	);
}
