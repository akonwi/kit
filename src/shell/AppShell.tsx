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
	const [headerHeight, setHeaderHeight] = createSignal(1);
	const [dockHeight, setDockHeight] = createSignal(3);
	const renderer = useRenderer();
	let headerRef: { height: number } | undefined;

	return (
		<box
			width="100%"
			height="100%"
			flexDirection="column"
			backgroundColor={theme.bg}
			onMouseUp={() => copySelection(renderer)}
		>
			<box
				flexShrink={0}
				border
				borderColor={theme.borderDefault}
				paddingX={1}
				ref={(value) => {
					headerRef = value;
				}}
				onSizeChange={() => {
					if (headerRef) setHeaderHeight(headerRef.height);
				}}
			>
				<text fg={theme.textMuted}>
					{props.state.sessionMeta.sessionName || "Unnamed session"}
				</text>
			</box>

			<TranscriptPane turns={props.state.turns} showToast={props.showToast} />

			<box flexShrink={0} flexDirection="column" gap={0}>
				<PendingSlot
					panel={props.state.panel}
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

			<ToastStack
				toasts={props.state.toasts}
				top={headerHeight()}
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
