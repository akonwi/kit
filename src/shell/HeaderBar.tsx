import { createSignal, onCleanup, Show } from "solid-js";
import { codeReviewStatus } from "../features/code-review/state";
import type { AgentRuntime } from "../runtime/agent-runtime";
import { ScreenHeader } from "./ScreenHeader";
import { theme } from "./theme";

// TODO: Replace this direct global feature-state import once plugins can expose
// header/footer contributions or plugin state can be queried through
// PluginManager in the component tree.

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
	const bell = () => (settings().bells ? "◉ bell" : "◌ bell");
	const speech = () => {
		const value = settings().speech;
		const on =
			(typeof value === "boolean" && value) ||
			(typeof value === "object" && "enabled" in value && value.enabled);
		return on ? "◉ speech" : "◌ speech";
	};
	const reviewVisible = () =>
		codeReviewStatus.launchInFlight ||
		codeReviewStatus.serverState === "ready" ||
		codeReviewStatus.serverState === "error";
	const reviewLabel = () => {
		if (codeReviewStatus.serverState === "error") return "✗ review";
		if (codeReviewStatus.launchInFlight) return "◌ review…";
		if (codeReviewStatus.clientConnected) {
			return `◉ review${codeReviewStatus.port ? ` :${codeReviewStatus.port}` : ""}`;
		}
		return `◌ review${codeReviewStatus.port ? ` :${codeReviewStatus.port}` : ""}`;
	};
	const reviewColor = () => {
		if (codeReviewStatus.serverState === "error") return theme.errorText;
		if (codeReviewStatus.launchInFlight) return theme.warningText;
		if (codeReviewStatus.clientConnected) return theme.toolText;
		return theme.textMuted;
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
					<Show when={reviewVisible()}>
						<text fg={reviewColor()}>{reviewLabel()}</text>
					</Show>
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
