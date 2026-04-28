import { useRenderer } from "@opentui/solid";
import { createSignal, For, Show } from "solid-js";
import {
	getOverlaySurfaceProps,
	getToastStackZIndex,
	type OverlayEntry,
} from "../app/overlay-ui";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { AppState } from "../state/app-state";
import type { AttachmentsController } from "./attachments-controller";
import { BottomStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import type { ComposerController } from "./composer-controller";
import { HeaderBar } from "./HeaderBar";
import { InlinePicker } from "./InlinePicker";
import { Modal } from "./Modal";
import { PendingSlot } from "./PendingSlot";
import { copySelection } from "./selection";
import { ToastStack } from "./ToastStack";
import { Transcript } from "./Transcript";
import { theme } from "./theme";
import { TranscriptPane } from "./transcript/index";

const STATUS_BAR_HEIGHT = 1;
const EXPERIMENTAL_TRANSCRIPT = process.env.EXPERIMENTAL === "true";

export type AppShellProps = {
	state: AppState;
	runtime: AgentRuntime;
	controller: ComposerController;
	attachments: AttachmentsController;
	overlays: () => OverlayEntry[];
	dismissToast: (id: number) => void;
	showToast: (toast: {
		title: string;
		lines: string[];
		variant: "info" | "warning" | "error";
	}) => void;
};

export function AppShell(props: AppShellProps) {
	const [headerHeight, setHeaderHeight] = createSignal(1);
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
			<HeaderBar
				sessionName={props.state.sessionMeta.sessionName}
				status={props.state.footerStatus}
				onHeightChange={setHeaderHeight}
			/>

			<Show
				when={EXPERIMENTAL_TRANSCRIPT}
				fallback={
					<TranscriptPane
						runtime={props.runtime}
						turns={props.state.turns}
						showToast={props.showToast}
					/>
				}
			>
				<Transcript
					runtime={props.runtime}
					turns={props.state.turns}
					showToast={props.showToast}
				/>
			</Show>

			<box flexShrink={0} flexDirection="column" gap={0}>
				<PendingSlot
					runtime={props.runtime}
					pendingMessages={props.state.pendingMessages}
				/>
				<ComposerDock
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

			<Modal palette={props.controller.palette} />
			<Show when={props.overlays().length > 0}>
				<For each={props.overlays()}>
					{(entry, index) =>
						entry.component({
							done: (result: unknown) => entry.resolve(result),
							surfaceProps: getOverlaySurfaceProps(index()),
						})
					}
				</For>
			</Show>

			<ToastStack
				toasts={props.state.toasts}
				top={headerHeight()}
				zIndex={getToastStackZIndex(props.overlays().length)}
				onDismiss={props.dismissToast}
			/>
		</box>
	);
}
