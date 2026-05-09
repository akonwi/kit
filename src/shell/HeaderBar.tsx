import { createSignal, onCleanup } from "solid-js";
import type { AgentRuntime } from "../runtime/agent-runtime";
import { CIRCLE_EMPTY, CIRCLE_FILLED } from "./glyphs";
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

	onCleanup(() => {
		unsubscribeTurns();
		unsubscribeSettings();
		unsubscribeAgentInfo();
		unsubscribeSessionChange();
		unsubscribeCompactionCompleted();
	});

	return (
		<ScreenHeader
			left={
				<text fg={theme.textMuted}>
					{props.sessionName || "Unnamed session"}
				</text>
			}
			right={
				<box flexDirection="row" gap={1}>
					<text fg={theme.textMuted}>
						{agentInfo().model?.name ?? "model?"} ({agentInfo().thinkingLevel})
					</text>
					<text fg={theme.textMuted}>
						{bell()} {speech()}
					</text>
				</box>
			}
			progress={contextUsage()}
			progressColor={progressColor(contextUsage())}
			onHeightChange={props.onHeightChange}
		/>
	);
}
