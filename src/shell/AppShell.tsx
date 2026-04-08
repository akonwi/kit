import { useRenderer } from "@opentui/solid";
import { createSignal } from "solid-js";
import type { AppState } from "../state/app-state";
import { BottomStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import type { ComposerController } from "./composer-controller";
import { InlinePicker } from "./InlinePicker";
import { Modal } from "./Modal";
import { PendingSlot } from "./PendingSlot";
import { copySelection } from "./selection";
import { ToastStack } from "./ToastStack";
import { TranscriptPane } from "./TranscriptPane";
import { theme } from "./theme";

const STATUS_BAR_HEIGHT = 1;

export type AppShellProps = {
	state: AppState;
	controller: ComposerController;
	dismissToast: (id: number) => void;
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
			<TranscriptPane turns={props.state.turns} />

			<box flexShrink={0} flexDirection="column" gap={0}>
				<PendingSlot
					panel={props.state.panel}
					pendingMessages={props.state.pendingMessages}
				/>
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
				onDismiss={props.dismissToast}
			/>

			<Modal palette={props.controller.palette} />
		</box>
	);
}
