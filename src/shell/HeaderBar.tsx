import { createSignal, onCleanup, Show } from "solid-js";
import { codeReviewStatus } from "../features/code-review/state";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { FooterStatusState } from "../state/app-state";
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
	status: FooterStatusState;
	onHeightChange?: (height: number) => void;
	runtime: AgentRuntime;
};

export function HeaderBar(props: HeaderBarProps) {
	const [contexStats, setContextStats] = createSignal(
		props.runtime.contextStats,
	);
	const unsubscribeTurns = props.runtime.subscribe(
		"agent.turn.completed",
		(_) => setContextStats(props.runtime.contextStats),
	);

	const contextUsage = () => contexStats()?.percent ?? 0;
	const formattedContextUsage = () =>
		contexStats() ? `${contextUsage()}%` : "–";

	const bell = () => (props.status.bellsEnabled ? "🔔" : "🔕");
	const speech = () => (props.status.speechEnabled ? "🗣" : "🤫");
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
		(e) => setAgentInfo(e),
	);

	onCleanup(() => {
		unsubscribeTurns();
		unsubscribeAgentInfo();
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
					<text fg={progressColor(contextUsage())}>
						{formattedContextUsage()}
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
