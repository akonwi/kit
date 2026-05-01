import type { KeyEvent, PasteEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import { createEffect, createSignal } from "solid-js";
import type { AttachmentsController } from "./attachments-controller";
import type { ComposerController, TextareaHandle } from "./composer-controller";
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
	const palette = props.controller.palette;
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

	useKeyboard((e: KeyEvent) => {
		if (props.locked) return;

		// Ctrl+C — clear composer if it has content, otherwise quit
		if (e.ctrl && e.name === "c") {
			e.preventDefault();
			if (palette.visible) {
				palette.clear();
				return;
			}
			const text = props.controller.getTextareaText();
			if (text.trim()) {
				props.controller.setTextareaText("");
				syncComposerText();
				return;
			}
			// Empty composer — quit the app
			props.controller.quit();
			return;
		}

		// Escape — abort agent when composer is empty and agent is working
		if (
			e.name === "escape" &&
			!palette.visible &&
			!props.controller.getTextareaText().trim() &&
			props.controller.isStreaming()
		) {
			e.preventDefault();
			props.controller.abort();
			return;
		}

		// Enter in empty composer while streaming with queued follow-ups — promote to steering
		if (
			(e.name === "return" || e.name === "enter") &&
			!palette.visible &&
			!props.controller.getTextareaText().trim() &&
			props.controller.isStreaming() &&
			props.controller.getPendingMessageCount() > 0
		) {
			e.preventDefault();
			props.controller.promotePendingFollowUpsToSteering();
			return;
		}

		// Up arrow in empty composer — restore queued follow-ups first, then recall last user message
		if (
			e.name === "up" &&
			!palette.visible &&
			!props.controller.getTextareaText().trim()
		) {
			e.preventDefault();
			if (!props.controller.restorePendingMessages()) {
				props.controller.recallLastUserMessage();
			}
			syncComposerText();
			return;
		}

		// Non-filterable palette navigation
		if (!palette.visible) return;
		if (palette.isFilterable || palette.isInputMode) return;

		if (e.name === "up") {
			e.preventDefault();
			palette.moveUp();
			return;
		}
		if (e.name === "down") {
			e.preventDefault();
			palette.moveDown();
			return;
		}
		if (e.name === "escape") {
			e.preventDefault();
			palette.pop();
			return;
		}
		if (e.name === "return") {
			e.preventDefault();
			palette.selectCurrent();
			return;
		}

		if (e.ctrl && e.name) {
			const key = `ctrl+${e.name}`;
			if (palette.handleKeyBinding(key)) {
				e.preventDefault();
				return;
			}
		}

		e.preventDefault();
	});

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
							{attachment.icon} {attachment.summary}
						</text>
						<text
							fg={theme.textMuted}
							onMouseUp={() => props.attachments.detach(attachment.id)}
						>
							×
						</text>
					</box>
				))}
			</box>
			<MessageComposer
				ref={(value) => {
					props.controller.setTextarea(value as TextareaHandle | undefined);
				}}
				placeholder={placeholder()}
				focused={!palette.visible && !props.locked}
				showCursor={!palette.visible && !props.locked}
				borderColor={composerBorderColor()}
				keyBindings={
					palette.visible || props.locked
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
