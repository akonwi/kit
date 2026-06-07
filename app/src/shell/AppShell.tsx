import { useRenderer } from "@opentui/solid";
import { createSignal, For, onCleanup, Show } from "solid-js";
import {
	getOverlaySurfaceProps,
	getToastStackZIndex,
	type OverlayEntry,
} from "../app/overlay-ui";
import type { Command, CommandRegistry } from "../features/commands";
import { createKeybindingDiagnosticReporter } from "../keymap/diagnostics";
import { getKeybindingCommand } from "../keymap/registry";
import { KeymapLayerProvider, useKeymapLayer } from "../keymap/useKeymapLayer";
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
import type { OpenOverlay } from "./transcript/types";

const STATUS_BAR_HEIGHT = 1;

export type AppShellProps = {
	settings: Settings;
	state: AppState;
	runtime: AgentRuntime;
	commands: CommandRegistry;
	controller: ComposerController;
	attachments: AttachmentsController;
	footer: FooterStatusController;
	header: HeaderStatusController;
	overlays: () => OverlayEntry[];
	openOverlay: OpenOverlay;
	dismissToast: (id: number) => void;
	onTranscriptViewportChange: (viewport: {
		width: number;
		height: number;
	}) => void;
	showToast: (toast: ToastInput) => void;
};

type AppShellContentProps = Omit<AppShellProps, "settings" | "showToast"> & {
	showToast: (toast: ToastInput) => void;
};

function commandKeybindingGroup(command: Command): string {
	if (command.category) return command.category;
	const dot = command.name.indexOf(".");
	return dot > 0 ? command.name.slice(0, dot) : "Commands";
}

function AppShellContent(props: AppShellContentProps) {
	const [headerHeight, setHeaderHeight] = createSignal(1);
	const [dockHeight, setDockHeight] = createSignal(3);
	const [composerMode, setComposerMode] =
		createSignal<ComposerInputMode>("normal");
	const [commandRegistryVersion, setCommandRegistryVersion] = createSignal(0);
	const renderer = useRenderer();
	let transcriptRef: { width: number; height: number } | undefined;

	onCleanup(
		props.commands.subscribe(() => {
			setCommandRegistryVersion((version) => version + 1);
		}),
	);

	useKeymapLayer(() => {
		commandRegistryVersion();
		const bindableCommands = props.commands
			.getAll()
			.filter((command) => !getKeybindingCommand(command.name));
		return {
			scope: "app",
			when: () => props.overlays().length === 0,
			commandMetadata: Object.fromEntries(
				bindableCommands.map((command) => [
					command.name,
					{
						defaultKeys: [],
						desc: command.description,
						group: commandKeybindingGroup(command),
						hint: false,
					},
				]),
			),
			commands: {
				"command-palette.open": () => {
					props.controller.openCommandPalette();
				},
			},
			generatedCommands: Object.fromEntries(
				bindableCommands.map((command) => [
					command.name,
					() => {
						if (props.controller.picker.visible) return false;
						if (props.controller.commandPalette.visible) return false;
						void props.controller.runCommand(command, "");
					},
				]),
			),
		};
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
				<Transcript
					runtime={props.runtime}
					showToast={props.showToast}
					openOverlay={props.openOverlay}
				/>
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
					runtime={props.runtime}
					status={props.footer}
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

export function AppShell(props: AppShellProps) {
	const [settings, setSettings] = createSignal(props.settings);
	const reportKeybindingDiagnostic = createKeybindingDiagnosticReporter(
		props.showToast,
	);

	onCleanup(
		props.runtime.subscribe("settings.changed", (event) => {
			setSettings(event.settings);
		}),
	);

	return (
		<KeymapLayerProvider
			keybindings={() => settings().keybindings}
			onDiagnostic={reportKeybindingDiagnostic}
		>
			<AppShellContent
				state={props.state}
				runtime={props.runtime}
				commands={props.commands}
				controller={props.controller}
				attachments={props.attachments}
				footer={props.footer}
				header={props.header}
				overlays={props.overlays}
				openOverlay={props.openOverlay}
				dismissToast={props.dismissToast}
				onTranscriptViewportChange={props.onTranscriptViewportChange}
				showToast={props.showToast}
			/>
		</KeymapLayerProvider>
	);
}
