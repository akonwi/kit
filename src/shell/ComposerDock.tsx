import type { KeyEvent, PasteEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import type { AttachmentsController } from "./attachments-controller";
import type { ComposerController, TextareaHandle } from "./composer-controller";
import { theme } from "./theme";

export type ComposerDockProps = {
	cwd: string;
	sessionName: string | undefined;
	gitBranch: string | null;
	gitDirty: boolean;
	controller: ComposerController;
	attachments: AttachmentsController;
	locked?: boolean;
	onHeightChange?: (height: number) => void;
};

export function ComposerDock(props: ComposerDockProps) {
	let dockRef: { width: number; height: number } | undefined;
	const palette = props.controller.palette;

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
						<text fg={theme.textMuted}>
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
			<box
				width="100%"
				border
				borderColor={theme.borderFocused}
				paddingLeft={1}
				paddingRight={1}
				paddingBottom={1}
				flexDirection="column"
				gap={0}
			>
				{/* @ts-ignore onPaste supported but not typed */}
				<textarea
					ref={(value) => {
						props.controller.setTextarea(value as TextareaHandle | undefined);
					}}
					minHeight={1}
					placeholder={placeholder()}
					placeholderColor={theme.textPlaceholder}
					backgroundColor={theme.bg}
					focusedBackgroundColor={theme.bg}
					textColor={theme.textPrimary}
					focusedTextColor={theme.textPrimary}
					cursorColor={theme.cursor}
					showCursor={!palette.visible && !props.locked}
					wrapMode="word"
					maxHeight={10}
					overflow="scroll"
					keyBindings={
						palette.visible || props.locked
							? []
							: [
									{ name: "return", action: "submit" },
									{ name: "linefeed", action: "submit" },
									{ name: "return", shift: true, action: "newline" },
								]
					}
					onContentChange={() => props.controller.handleTextChange()}
					onPaste={(event: PasteEvent) => {
						console.log("[composer-dock] textarea onPaste fired", {
							mimeType: event.metadata?.mimeType,
							kind: event.metadata?.kind,
							byteLength: event.bytes.length,
						});
						void props.controller.handlePaste(event);
					}}
					onSubmit={() => props.controller.handleSubmit()}
					focused={!palette.visible && !props.locked}
				/>
			</box>
			<box width="100%" flexDirection="row" justifyContent="space-between">
				<text fg={theme.textMuted}>{props.sessionName || "Unnamed"}</text>
				<text fg={theme.textMuted}>
					{props.cwd}
					{props.gitBranch &&
						` (${props.gitBranch}${props.gitDirty ? " ●" : " ○"})`}
				</text>
			</box>
		</box>
	);
}
