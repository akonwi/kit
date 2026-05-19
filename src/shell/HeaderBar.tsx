import { createSignal, onCleanup } from "solid-js";
import type { AgentRuntime } from "../runtime/agent-runtime";
import { CIRCLE_EMPTY, CIRCLE_FILLED, MIDDLE_DOT } from "./glyphs";
import type { HeaderStatusController } from "./header-status";
import { ScreenHeader } from "./ScreenHeader";
import { theme } from "./theme";

function progressColor(pct: number): string {
	if (pct > 90) return theme.progressCritical;
	if (pct >= 80) return theme.progressWarning;
	return theme.progressNormal;
}

export type HeaderBarProps = {
	sessionName: string | undefined;
	onHeightChange?: (height: number) => void;
	runtime: AgentRuntime;
	header: HeaderStatusController;
};

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

	const [settings, setSettings] = createSignal(props.runtime.settings);
	const unsubscribeSettings = props.runtime.subscribe("settings.changed", (e) =>
		setSettings(e.settings),
	);
	const bell = () =>
		settings().bells ? `${CIRCLE_FILLED} bell` : `${CIRCLE_EMPTY} bell`;
	const speech = () => {
		const value = settings().speech;
		const on =
			(typeof value === "boolean" && value) ||
			(typeof value === "object" && "enabled" in value && value.enabled);
		return on ? `${CIRCLE_FILLED} speech` : `${CIRCLE_EMPTY} speech`;
	};
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
	const contributionLabels = (side: "left" | "right") =>
		headerContributions()
			.filter((contribution) => contribution.side === side)
			.map((contribution) => contribution.label);
	const leftText = () =>
		[
			props.sessionName || "Unnamed session",
			...contributionLabels("left"),
		].join(` ${MIDDLE_DOT} `);
	const rightText = () =>
		[
			`${agentInfo().model?.name ?? "model?"} (${agentInfo().thinkingLevel})`,
			`${bell()} ${speech()}`,
			...contributionLabels("right"),
		].join(` ${MIDDLE_DOT} `);

	onCleanup(() => {
		unsubscribeTurns();
		unsubscribeSettings();
		unsubscribeAgentInfo();
		unsubscribeSessionChange();
		unsubscribeCompactionCompleted();
		unsubscribeHeader();
	});

	return (
		<ScreenHeader
			left={<text fg={theme.textMuted}>{leftText()}</text>}
			right={<text fg={theme.textMuted}>{rightText()}</text>}
			progress={contextUsage()}
			progressColor={progressColor(contextUsage())}
			onHeightChange={props.onHeightChange}
		/>
	);
}
