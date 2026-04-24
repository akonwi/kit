import type { PasteEvent } from "@opentui/core";
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
	initialValue?: string;
	placeholder?: string;
	focused?: boolean;
	showCursor?: boolean;
	maxHeight?: number;
	keyBindings?: KeyBinding[];
	onContentChange?: () => void;
	onPaste?: (event: PasteEvent) => void;
	onSubmit?: () => void;
};

/**
 * Themed textarea in a bordered box.
 * Shared input field used by the main composer and pager note area.
 */
export function MessageComposer(props: MessageComposerProps) {
	return (
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
				ref={(value: unknown) => {
					const ref = value as TextareaRef | undefined;
					if (ref && props.initialValue) {
						ref.setText(props.initialValue);
						ref.cursorOffset = props.initialValue.length;
					}
					props.ref?.(ref);
				}}
				minHeight={1}
				maxHeight={props.maxHeight ?? 10}
				placeholder={props.placeholder ?? ""}
				placeholderColor={theme.textPlaceholder}
				backgroundColor={theme.bg}
				focusedBackgroundColor={theme.bg}
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
		</box>
	);
}
