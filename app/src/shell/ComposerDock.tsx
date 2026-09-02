import type { PasteEvent } from "@opentui/core";
import { createEffect, createSignal, onCleanup } from "solid-js";
import { useKeymapLayer } from "../keymap/useKeymapLayer";
import type {
	Attachment,
	AttachmentsController,
} from "./attachments-controller";
import type { ComposerController, TextareaHandle } from "./composer-controller";
import { CHEVRON_RIGHT, TIMES } from "./glyphs";
import { MessageComposer } from "./MessageComposer";
import { theme } from "./theme";

export type ComposerDockProps = {
	controller: ComposerController;
	attachments: AttachmentsController;
	locked?: boolean;
	inputFocused?: boolean;
	onHeightChange?: (height: number) => void;
	onModeChange?: (mode: ComposerInputMode) => void;
	onOpenAttachment?: (attachment: Attachment) => void;
	onFocusRequest?: () => void;
};

export type ComposerInputMode = "normal" | "bash" | "bash-excluded";

export function getComposerUpAction(
	pendingMessageCount: number,
	composerText: string,
): "restore" | "recall" | "native" {
	if (pendingMessageCount > 0) return "restore";
	return composerText.trim() ? "native" : "recall";
}

export function findLatestOpenableAttachment(
	attachments: Attachment[],
): Attachment | null {
	return (
		attachments.findLast(
			(attachment) => attachment.type === "code-review" || attachment.onOpen,
		) ?? null
	);
}

function getComposerInputMode(text: string): ComposerInputMode {
	if (text.startsWith("!!")) return "bash-excluded";
	if (text.startsWith("!")) return "bash";
	return "normal";
}

export function ComposerDock(props: ComposerDockProps) {
	let dockRef: { width: number; height: number } | undefined;
	onCleanup(() => props.controller.setTextarea(undefined));
	const picker = props.controller.picker;
	const commandPaletteVisible = () => props.controller.commandPalette.visible;
	const [composerText, setComposerText] = createSignal(
		props.controller.getTextareaText(),
	);
	const composerMode = () => getComposerInputMode(composerText());
	const inputFocused = () => props.inputFocused !== false;
	const composerVisuallyFocused = () =>
		inputFocused() &&
		!props.locked &&
		!picker.visible &&
		!commandPaletteVisible();
	const composerBorderColor = () => {
		if (!composerVisuallyFocused()) return theme.borderDefault;
		return composerMode() === "bash"
			? theme.composerBashBorder
			: composerMode() === "bash-excluded"
				? theme.composerBashExcludedBorder
				: theme.borderFocused;
	};
	const syncComposerText = () =>
		setComposerText(props.controller.getTextareaText());

	createEffect(() => {
		if (!composerVisuallyFocused()) return;
		queueMicrotask(() => {
			if (composerVisuallyFocused()) props.controller.focusTextarea();
		});
	});

	createEffect(() => {
		props.onModeChange?.(composerMode());
	});

	const shellInputAvailable = () =>
		inputFocused() && !props.locked && !commandPaletteVisible();
	useKeymapLayer(() => ({
		scope: "composer",
		when: shellInputAvailable,
		commands: {
			"composer.open-attachment": () => {
				if (picker.visible || !props.onOpenAttachment) return false;
				const attachment = findLatestOpenableAttachment(
					props.attachments.attachments(),
				);
				if (!attachment) return false;
				props.onOpenAttachment(attachment);
			},
			"composer.clear-or-quit": () => {
				if (props.controller.cancelReferenceInteraction()) {
					picker.clear();
					syncComposerText();
					return;
				}
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
				if (
					getComposerUpAction(
						props.controller.getPendingMessageCount(),
						props.controller.getTextareaText(),
					) === "restore"
				) {
					return false;
				}
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
				const action = getComposerUpAction(
					props.controller.getPendingMessageCount(),
					props.controller.getTextareaText(),
				);
				if (action === "restore") {
					if (!props.controller.restorePendingMessages()) return false;
					syncComposerText();
					return;
				}
				if (action === "native") return false;
				if (!props.controller.showUserMessageHistoryPicker(syncComposerText)) {
					return false;
				}
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
			onMouseDown={(event) => {
				if (event.button === 0) props.onFocusRequest?.();
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
						paddingX={1}
						justifyContent="space-between"
						alignItems="center"
					>
						<text
							fg={theme.attachmentText}
							onMouseUp={() => props.onOpenAttachment?.(attachment)}
						>
							{attachment.icon
								? `${attachment.icon} ${attachment.summary}`
								: attachment.summary}
							{attachment.type === "code-review" || attachment.onOpen
								? ` ${CHEVRON_RIGHT}`
								: ""}
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
			<box
				width="100%"
				border={["top"]}
				borderColor={composerBorderColor()}
				paddingX={1}
			>
				<MessageComposer
					variant="dock"
					ref={(value) => {
						props.controller.setTextarea(value as TextareaHandle | undefined);
					}}
					placeholder={placeholder()}
					focused={
						inputFocused() &&
						!picker.visible &&
						!commandPaletteVisible() &&
						!props.locked
					}
					showCursor={
						inputFocused() &&
						!picker.visible &&
						!commandPaletteVisible() &&
						!props.locked
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
		</box>
	);
}
