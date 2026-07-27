import type { PasteEvent } from "@opentui/core";
import { Show } from "solid-js";
import { theme } from "./theme";

type TextareaAction = "submit" | "newline";
type KeyBinding = { name: string; shift?: boolean; action: TextareaAction };

// todo: this seems like it should be an opentui type
export type TextareaRef = {
	plainText: string;
	cursorOffset: number;
	setText: (v: string) => void;
};

export type MessageComposerProps = {
	ref?: (el: TextareaRef | undefined) => void;
	variant?: "field" | "dock";
	initialValue?: string;
	placeholder?: string;
	focused?: boolean;
	showCursor?: boolean;
	maxHeight?: number;
	borderColor?: string;
	backgroundColor?: string;
	focusedBackgroundColor?: string;
	keyBindings?: KeyBinding[];
	onContentChange?: () => void;
	onPaste?: (event: PasteEvent) => void;
	onSubmit?: () => void;
};

function ComposerTextarea(input: { composerProps: MessageComposerProps }) {
	const props = input.composerProps;
	return (
		<textarea
			ref={(value: unknown) => {
				const ref = value as TextareaRef | undefined;
				if (ref && props.initialValue !== undefined) {
					ref.setText(props.initialValue);
					ref.cursorOffset = props.initialValue.length;
				}
				props.ref?.(ref);
			}}
			minHeight={1}
			maxHeight={props.maxHeight ?? 10}
			placeholder={props.placeholder ?? ""}
			placeholderColor={theme.textPlaceholder}
			backgroundColor={props.backgroundColor ?? theme.bg}
			focusedBackgroundColor={props.focusedBackgroundColor ?? theme.bg}
			textColor={theme.textPrimary}
			focusedTextColor={theme.textPrimary}
			cursorColor={theme.cursor}
			showCursor={props.showCursor ?? true}
			wrapMode="word"
			overflow="scroll"
			focused={props.focused ?? true}
			keyBindings={props.keyBindings ?? []}
			onContentChange={() => props.onContentChange?.()}
			onPaste={props.onPaste}
			onSubmit={() => props.onSubmit?.()}
		/>
	);
}

/**
 * Themed textarea with field and shell-dock presentation variants.
 */
export function MessageComposer(props: MessageComposerProps) {
	return (
		<Show
			when={props.variant === "dock"}
			fallback={
				<box
					width="100%"
					border
					borderColor={props.borderColor ?? theme.borderFocused}
					paddingLeft={1}
					paddingRight={1}
					paddingBottom={1}
					flexDirection="column"
					gap={0}
				>
					<ComposerTextarea composerProps={props} />
				</box>
			}
		>
			<box width="100%" flexDirection="column" gap={0}>
				<ComposerTextarea composerProps={props} />
			</box>
		</Show>
	);
}
