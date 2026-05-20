import { useBindings, useKeymap } from "@opentui/keymap/solid";
import { useRenderer } from "@opentui/solid";
import { createSignal, For, onCleanup, Show } from "solid-js";
import {
	getOverlaySurfaceProps,
	getToastStackZIndex,
	type OverlayEntry,
} from "../app/overlay-ui";
import {
	createConfiguredBindings,
	type KitBindingDefinition,
	withKitKeyAliases,
} from "../keymap/bindings";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { Settings } from "../settings";
import type { AppState } from "../state/app-state";
import type { ToastInput } from "../state/toasts";
import type { AttachmentsController } from "./attachments-controller";
import { BottomStatusBar } from "./BottomStatusBar";
import { CommandPalette } from "./CommandPalette";
import { ComposerDock, type ComposerInputMode } from "./ComposerDock";
import type { ComposerController } from "./composer-controller";
import type { FooterStatusController } from "./footer-status";
import { HeaderBar } from "./HeaderBar";
import type { HeaderStatusController } from "./header-status";
import { InlinePicker } from "./InlinePicker";
import { PendingSlot } from "./PendingSlot";
import { copySelection } from "./selection";
import { ToastStack } from "./ToastStack";
import { theme } from "./theme";
import { Transcript } from "./transcript";

const STATUS_BAR_HEIGHT = 1;

const APP_SHELL_BINDINGS = [
	{
		cmd: "command-palette.open",
		key: "ctrl+p",
		desc: "Open command palette",
		group: "App",
	},
] as const satisfies readonly KitBindingDefinition[];

export type AppShellProps = {
	settings: Settings;
	state: AppState;
	runtime: AgentRuntime;
	controller: ComposerController;
	attachments: AttachmentsController;
	footer: FooterStatusController;
	header: HeaderStatusController;
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
	const [settings, setSettings] = createSignal(props.settings);
	const renderer = useRenderer();
	const keymap = useKeymap();
	let transcriptRef: { width: number; height: number } | undefined;

	onCleanup(
		props.runtime.subscribe("settings.changed", (event) => {
			setSettings(event.settings);
		}),
	);

	useBindings(() =>
		withKitKeyAliases({
			priority: 100,
			commands: [
				{
					name: "command-palette.open",
					desc: "Open command palette",
					group: "App",
					run: () => {
						if (props.overlays().length > 0) return false;
						props.controller.openCommandPalette();
					},
				},
			],
			bindings: createConfiguredBindings(
				keymap,
				APP_SHELL_BINDINGS,
				settings().keybindings,
			),
		}),
	);

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
				header={props.header}
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
					settings={settings}
					controller={props.controller}
					attachments={props.attachments}
					locked={props.overlays().length > 0}
					onHeightChange={setDockHeight}
					onModeChange={setComposerMode}
				/>
				<BottomStatusBar
					runtime={props.runtime}
					status={props.footer}
					composerMode={composerMode()}
				/>
			</box>

			<InlinePicker
				settings={settings}
				picker={props.controller.picker}
				bottomOffset={dockHeight() + STATUS_BAR_HEIGHT + 2}
			/>

			{/* Composer picker only serves @/# references */}
			<CommandPalette
				settings={settings}
				picker={props.controller.commandPalette}
			/>
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
