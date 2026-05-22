import type { PasteEvent } from "@opentui/core";
import { createEffect, createSignal } from "solid-js";
import { useKeymapLayer } from "../keymap/useKeymapLayer";
import type { AttachmentsController } from "./attachments-controller";
import type { ComposerController, TextareaHandle } from "./composer-controller";
import { TIMES } from "./glyphs";
import { MessageComposer } from "./MessageComposer";
import { theme } from "./theme";

export type ComposerDockProps = {
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
	useKeymapLayer(() => ({
		scope: "composer",
		when: shellInputAvailable,
		commands: {
			"composer.clear-or-quit": () => {
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
			"composer.abort": () => {
				if (picker.visible) return false;
				if (props.controller.getTextareaText().trim()) return false;
				if (!props.controller.isStreaming()) return false;
				props.controller.abort();
			},
			"composer.steer": () => {
				if (picker.visible) return false;
				if (props.controller.getTextareaText().trim()) return false;
				if (!props.controller.isStreaming()) return false;
				if (props.controller.getPendingMessageCount() <= 0) return false;
				props.controller.promotePendingFollowUpsToSteering();
			},
		},
	}));

	useKeymapLayer(() => ({
		scope: "composer",
		precedence: "contextual",
		when: shellInputAvailable,
		commands: {
			"composer.bash-history-older": () => {
				if (picker.visible) return false;
				if (!props.controller.getTextareaText().startsWith("!")) return false;
				if (!props.controller.showBashHistoryPicker(syncComposerText))
					return false;
			},
			"composer.bash-history-newer": () => {
				if (picker.visible) return false;
				if (!props.controller.getTextareaText().startsWith("!")) return false;
				if (!props.controller.showBashHistoryPicker(syncComposerText))
					return false;
			},
		},
	}));

	useKeymapLayer(() => ({
		scope: "composer",
		precedence: "fallback",
		when: shellInputAvailable,
		commands: {
			"composer.restore-or-recall": () => {
				if (picker.visible) return false;
				if (props.controller.getTextareaText().trim()) return false;
				if (!props.controller.restorePendingMessages()) {
					props.controller.recallLastUserMessage();
				}
				syncComposerText();
			},
		},
	}));

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
