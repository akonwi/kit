import { useRenderer } from "@opentui/solid";
import { createSignal } from "solid-js";
import type { AppState } from "../state/app-state";
import { BottomStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import type { ComposerController } from "./composer-controller";
import { InlinePicker } from "./InlinePicker";
import { PendingSlot } from "./PendingSlot";
import { copySelection } from "./selection";
import { ToastStack } from "./ToastStack";
import { TranscriptPane } from "./TranscriptPane";
import { theme } from "./theme";

const STATUS_BAR_HEIGHT = 1;

export type AppShellProps = {
	state: AppState;
	controller: ComposerController;
	dismissToast: (id: number) => void; // kept for future keyboard dismiss
};

export function AppShell(props: AppShellProps) {
	const [dockHeight, setDockHeight] = createSignal(3);
	const renderer = useRenderer();

	return (
		<box
			width="100%"
			height="100%"
			flexDirection="column"
			backgroundColor={theme.bg}
			onMouseUp={() => copySelection(renderer)}
		>
			<TranscriptPane messages={props.state.messages} />

			<box flexShrink={0} flexDirection="column" gap={0}>
				<PendingSlot panel={props.state.panel} />
				<ComposerDock
					cwd={props.state.footerStatus.cwd}
					sessionName={props.state.sessionMeta.sessionName}
					gitBranch={props.state.footerStatus.gitBranch}
					gitDirty={props.state.footerStatus.gitDirty}
					controller={props.controller}
					onHeightChange={setDockHeight}
				/>
				<BottomStatusBar status={props.state.footerStatus} />
			</box>

			<InlinePicker
				palette={props.controller.palette}
				bottomOffset={dockHeight() + STATUS_BAR_HEIGHT + 2}
			/>

			<ToastStack
				toasts={props.state.toasts}
				bottom={dockHeight() + STATUS_BAR_HEIGHT + 3}
			/>
		</box>
	);
}
