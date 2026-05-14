import type { KeyEvent } from "@opentui/core";
import { useKeyboard, useRenderer } from "@opentui/solid";
import { createSignal, For, Show } from "solid-js";
import {
	getOverlaySurfaceProps,
	getToastStackZIndex,
	type OverlayEntry,
} from "../app/overlay-ui";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { AppState } from "../state/app-state";
import type { ToastInput } from "../state/toasts";
import type { AttachmentsController } from "./attachments-controller";
import { BottomStatusBar } from "./BottomStatusBar";
import { CommandPalette } from "./CommandPalette";
import { ComposerDock, type ComposerInputMode } from "./ComposerDock";
import type { ComposerController } from "./composer-controller";
import { HeaderBar } from "./HeaderBar";
import { InlinePicker } from "./InlinePicker";
import { PendingSlot } from "./PendingSlot";
import { copySelection } from "./selection";
import { ToastStack } from "./ToastStack";
import { theme } from "./theme";
import { Transcript } from "./transcript";

const STATUS_BAR_HEIGHT = 1;

export type AppShellProps = {
	state: AppState;
	runtime: AgentRuntime;
	controller: ComposerController;
	attachments: AttachmentsController;
	overlays: () => OverlayEntry[];
	dismissToast: (id: number) => void;
	onTranscriptViewportChange: (viewport: {
		width: number;
		height: number;
	}) => void;
	showToast: (toast: ToastInput) => void;
};

export function AppShell(props: AppShellProps) {
	const [headerHeight, setHeaderHeight] = createSignal(1);
	const [dockHeight, setDockHeight] = createSignal(3);
	const [composerMode, setComposerMode] =
		createSignal<ComposerInputMode>("normal");
	const renderer = useRenderer();
	let transcriptRef: { width: number; height: number } | undefined;

	useKeyboard((e: KeyEvent) => {
		if (e.ctrl && e.name === "p") {
			if (props.overlays().length > 0) return;
			e.preventDefault();
			props.controller.openCommandPalette();
		}
	});

	return (
		<box
			width="100%"
			height="100%"
			flexDirection="column"
			backgroundColor={theme.bg}
			onMouseUp={() => copySelection(renderer)}
		>
			<HeaderBar
				runtime={props.runtime}
				sessionName={props.state.sessionMeta.name}
				onHeightChange={setHeaderHeight}
			/>

			<box
				flexGrow={1}
				ref={(value) => {
					transcriptRef = value as typeof transcriptRef;
				}}
				onSizeChange={() => {
					if (!transcriptRef) return;
					props.onTranscriptViewportChange({
						width: transcriptRef.width,
						height: transcriptRef.height,
					});
				}}
			>
				<Transcript runtime={props.runtime} showToast={props.showToast} />
			</box>

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
					onModeChange={setComposerMode}
				/>
				<BottomStatusBar
					cwd={props.state.sessionMeta.cwd}
					runtime={props.runtime}
					composerMode={composerMode()}
				/>
			</box>

			<InlinePicker
				picker={props.controller.picker}
				bottomOffset={dockHeight() + STATUS_BAR_HEIGHT + 2}
			/>

			{/* Composer picker only serves @/# references */}
			<CommandPalette picker={props.controller.commandPalette} />
			<Show when={props.overlays().length > 0}>
				<For each={props.overlays()}>
					{(entry, index) =>
						entry.component({
							done: (result: unknown) => entry.resolve(result),
							surfaceProps: getOverlaySurfaceProps(index()),
							get active() {
								return index() === props.overlays().length - 1;
							},
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
