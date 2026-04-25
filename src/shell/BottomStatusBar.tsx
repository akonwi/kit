import { Show } from "solid-js";
import type { FooterStatusState } from "../state/app-state";
import { theme } from "./theme";

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
			<Show when={pending()} fallback={<text />}>
				<text fg={theme.textMuted}>{pending()}</text>
			</Show>
			<text fg={theme.textMuted}>{location()}</text>
		</box>
	);
}
