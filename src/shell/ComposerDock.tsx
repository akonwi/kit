import type { PasteEvent } from "@opentui/core";
import { useBindings, useKeymap } from "@opentui/keymap/solid";
import type { Accessor } from "solid-js";
import { createEffect, createSignal } from "solid-js";
import {
	createConfiguredBindings,
	type KitBindingDefinition,
	withKitKeyAliases,
} from "../keymap/bindings";
import type { Settings } from "../settings";
import type { AttachmentsController } from "./attachments-controller";
import type { ComposerController, TextareaHandle } from "./composer-controller";
import { TIMES } from "./glyphs";
import { MessageComposer } from "./MessageComposer";
import { theme } from "./theme";

const COMPOSER_CORE_BINDINGS = [
	{
		cmd: "composer.clear-or-quit",
		key: "ctrl+c",
		desc: "Clear input or quit",
		group: "Composer",
	},
	{
		cmd: "composer.abort",
		key: "escape",
		desc: "Abort response",
		group: "Composer",
	},
	{
		cmd: "composer.steer",
		key: "return",
		desc: "Steer with queued follow-ups",
		group: "Composer",
	},
] as const satisfies readonly KitBindingDefinition[];

const COMPOSER_BASH_HISTORY_BINDINGS = [
	{
		cmd: "composer.bash-history-older",
		key: "up",
		desc: "Recall previous bash command",
		group: "Composer",
	},
	{
		cmd: "composer.bash-history-newer",
		key: "down",
		desc: "Recall next bash command",
		group: "Composer",
	},
] as const satisfies readonly KitBindingDefinition[];

const COMPOSER_RECALL_BINDINGS = [
	{
		cmd: "composer.restore-or-recall",
		key: "up",
		desc: "Restore queued follow-ups or recall previous message",
		group: "Composer",
	},
] as const satisfies readonly KitBindingDefinition[];

export type ComposerDockProps = {
	settings: Accessor<Settings>;
	controller: ComposerController;
	attachments: AttachmentsController;
	locked?: boolean;
	onHeightChange?: (height: number) => void;
	onModeChange?: (mode: ComposerInputMode) => void;
};

export type ComposerInputMode = "normal" | "bash" | "bash-excluded";

function getComposerInputMode(text: string): ComposerInputMode {
	if (text.startsWith("!!")) return "bash-excluded";
	if (text.startsWith("!")) return "bash";
	return "normal";
}

export function ComposerDock(props: ComposerDockProps) {
	let dockRef: { width: number; height: number } | undefined;
	const picker = props.controller.picker;
	const commandPaletteVisible = () => props.controller.commandPalette.visible;
	const [composerText, setComposerText] = createSignal(
		props.controller.getTextareaText(),
	);
	const keymap = useKeymap();
	const composerMode = () => getComposerInputMode(composerText());
	const composerBorderColor = () =>
		composerMode() === "bash"
			? theme.composerBashBorder
			: composerMode() === "bash-excluded"
				? theme.composerBashExcludedBorder
				: theme.borderFocused;
	const syncComposerText = () =>
		setComposerText(props.controller.getTextareaText());

	createEffect(() => {
		props.onModeChange?.(composerMode());
	});

	const shellInputAvailable = () => !props.locked && !commandPaletteVisible();

	useBindings(() =>
		withKitKeyAliases({
			priority: 90,
			commands: [
				{
					name: "composer.clear-or-quit",
					desc: "Clear input or quit",
					group: "Composer",
					run: () => {
						if (!shellInputAvailable()) return false;
						if (picker.visible) {
							picker.clear();
							return;
						}
						const text = props.controller.getTextareaText();
						if (text.trim()) {
							props.controller.setTextareaText("");
							syncComposerText();
							return;
						}
						props.controller.quit();
					},
				},
				{
					name: "composer.abort",
					desc: "Abort response",
					group: "Composer",
					run: () => {
						if (!shellInputAvailable()) return false;
						if (picker.visible) return false;
						if (props.controller.getTextareaText().trim()) return false;
						if (!props.controller.isStreaming()) return false;
						props.controller.abort();
					},
				},
				{
					name: "composer.steer",
					desc: "Steer with queued follow-ups",
					group: "Composer",
					run: () => {
						if (!shellInputAvailable()) return false;
						if (picker.visible) return false;
						if (props.controller.getTextareaText().trim()) return false;
						if (!props.controller.isStreaming()) return false;
						if (props.controller.getPendingMessageCount() <= 0) return false;
						props.controller.promotePendingFollowUpsToSteering();
					},
				},
			],
			bindings: createConfiguredBindings(
				keymap,
				COMPOSER_CORE_BINDINGS,
				props.settings().keybindings,
			),
		}),
	);

	useBindings(() =>
		withKitKeyAliases({
			priority: 80,
			commands: [
				{
					name: "composer.bash-history-older",
					desc: "Recall previous bash command",
					group: "Composer",
					run: () => {
						if (!shellInputAvailable() || picker.visible) return false;
						if (!props.controller.getTextareaText().startsWith("!"))
							return false;
						if (!props.controller.navigateBashHistory("older")) return false;
						syncComposerText();
					},
				},
				{
					name: "composer.bash-history-newer",
					desc: "Recall next bash command",
					group: "Composer",
					run: () => {
						if (!shellInputAvailable() || picker.visible) return false;
						if (!props.controller.getTextareaText().startsWith("!"))
							return false;
						if (!props.controller.navigateBashHistory("newer")) return false;
						syncComposerText();
					},
				},
			],
			bindings: createConfiguredBindings(
				keymap,
				COMPOSER_BASH_HISTORY_BINDINGS,
				props.settings().keybindings,
			),
		}),
	);

	useBindings(() =>
		withKitKeyAliases({
			priority: 70,
			commands: [
				{
					name: "composer.restore-or-recall",
					desc: "Restore queued follow-ups or recall previous message",
					group: "Composer",
					run: () => {
						if (!shellInputAvailable() || picker.visible) return false;
						if (props.controller.getTextareaText().trim()) return false;
						if (!props.controller.restorePendingMessages()) {
							props.controller.recallLastUserMessage();
						}
						syncComposerText();
					},
				},
			],
			bindings: createConfiguredBindings(
				keymap,
				COMPOSER_RECALL_BINDINGS,
				props.settings().keybindings,
			),
		}),
	);

	const placeholder = () => "Ask kit to do something...";

	return (
		<box
			flexShrink={0}
			flexDirection="column"
			gap={0}
			ref={(value) => {
				dockRef = value;
			}}
			onSizeChange={() => {
				if (dockRef) props.onHeightChange?.(dockRef.height);
			}}
		>
			<box width="100%" flexDirection="column" gap={0}>
				{props.attachments.attachments().map((attachment) => (
					<box
						width="100%"
						flexDirection="row"
						paddingLeft={1}
						paddingRight={1}
						paddingBottom={1}
						justifyContent="space-between"
						alignItems="center"
					>
						<text fg={theme.attachmentText}>
							{attachment.icon
								? `${attachment.icon} ${attachment.summary}`
								: attachment.summary}
						</text>
						<text
							fg={theme.textMuted}
							onMouseUp={() => props.attachments.detach(attachment.id)}
						>
							{TIMES}
						</text>
					</box>
				))}
			</box>
			<MessageComposer
				ref={(value) => {
					props.controller.setTextarea(value as TextareaHandle | undefined);
				}}
				placeholder={placeholder()}
				focused={!picker.visible && !commandPaletteVisible() && !props.locked}
				showCursor={
					!picker.visible && !commandPaletteVisible() && !props.locked
				}
				borderColor={composerBorderColor()}
				keyBindings={
					picker.visible || commandPaletteVisible() || props.locked
						? []
						: [
								{ name: "return", action: "submit" },
								{ name: "linefeed", action: "submit" },
								{ name: "return", shift: true, action: "newline" },
							]
				}
				onContentChange={() => {
					props.controller.handleTextChange();
					syncComposerText();
				}}
				onPaste={(event: PasteEvent) => {
					console.log("[composer-dock] textarea onPaste fired", {
						mimeType: event.metadata?.mimeType,
						kind: event.metadata?.kind,
						byteLength: event.bytes.length,
					});
					void props.controller.handlePaste(event).finally(syncComposerText);
				}}
				onSubmit={() => {
					void props.controller.handleSubmit().finally(syncComposerText);
				}}
			/>
		</box>
	);
}
