import { Show } from "solid-js";
import { codeReviewStatus } from "../features/code-review/state";
import type { FooterStatusState } from "../state/app-state";
import { theme } from "./theme";

// TODO: Replace this direct global feature-state import once plugins can expose
// footer contributions or plugin state can be queried through PluginManager in
// the component tree.

export type BottomStatusBarProps = {
	status: FooterStatusState;
};

export function BottomStatusBar(props: BottomStatusBarProps) {
	const pending = () =>
		props.status.pendingMessages > 0 ? `📬${props.status.pendingMessages}` : "";
	const location = () =>
		props.status.gitBranch
			? `${props.status.cwd} (${props.status.gitBranch}${props.status.gitDirty ? " ●" : " ○"})`
			: props.status.cwd;
	const reviewLabel = () => {
		if (codeReviewStatus.serverState === "error") return "review error";
		if (codeReviewStatus.launchInFlight) return "review starting";
		if (codeReviewStatus.serverState === "ready") {
			if (codeReviewStatus.clientConnected) {
				return `review connected${codeReviewStatus.port ? ` :${codeReviewStatus.port}` : ""}`;
			}
			return `review ready${codeReviewStatus.port ? ` :${codeReviewStatus.port}` : ""}`;
		}
		return "review off";
	};
	const reviewColor = () => {
		if (codeReviewStatus.serverState === "error") return theme.errorText;
		if (codeReviewStatus.launchInFlight) return theme.warningText;
		if (codeReviewStatus.clientConnected) return theme.toolText;
		if (codeReviewStatus.serverState === "ready") return theme.textSecondary;
		return theme.textMuted;
	};

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
			<box flexDirection="row" gap={1}>
				<text fg={reviewColor()}>{reviewLabel()}</text>
				<Show when={pending()}>
					<text fg={theme.textMuted}>{pending()}</text>
				</Show>
			</box>
			<text fg={theme.textMuted}>{location()}</text>
		</box>
	);
}
