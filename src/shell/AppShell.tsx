import { useRenderer } from "@opentui/solid";
import { createSignal, For, Show } from "solid-js";
import type { OverlayEntry } from "../app/overlay-ui";
import type { AppState } from "../state/app-state";
import type { AttachmentsController } from "./attachments-controller";
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
	attachments: AttachmentsController;
	overlays: () => OverlayEntry[];
	dismissToast: (id: number) => void;
	showToast: (toast: {
		title: string;
		lines: string[];
		variant: "info" | "error";
	}) => void;
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
			<TranscriptPane turns={props.state.turns} showToast={props.showToast} />

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
					attachments={props.attachments}
					locked={props.overlays().length > 0}
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
				top={1}
				onDismiss={props.dismissToast}
			/>

			<Modal palette={props.controller.palette} />
			<Show when={props.overlays().length > 0}>
				<For each={props.overlays()}>
					{(entry) =>
						entry.component({
							done: (result: unknown) => entry.resolve(result),
						})
					}
				</For>
			</Show>
		</box>
	);
}
