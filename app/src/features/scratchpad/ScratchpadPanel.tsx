import { createEffect } from "solid-js";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";
import { WorkspacePanelLayout } from "../../shell/WorkspacePanelLayout";
import type { ScratchpadController } from "./controller";

export const SCRATCHPAD_MIN_COLS = 30;

type TextareaRef = {
	plainText: string;
	cursorOffset: number;
	setText: (value: string) => void;
};

type ScratchpadContentProps = {
	controller: ScratchpadController;
	active?: boolean;
	onClose: () => void;
};

function ScratchpadContent(props: ScratchpadContentProps) {
	let textareaRef: TextareaRef | undefined;

	const active = () => props.active !== false;
	const draft = () => props.controller.draft();

	function close(): void {
		props.controller.autosaveDraft();
		props.onClose();
	}

	createEffect(() => {
		if (active() && !props.controller.editing()) props.controller.enterEdit();
	});

	createEffect(() => {
		const next = draft();
		if (textareaRef && textareaRef.plainText !== next) {
			textareaRef.setText(next);
			textareaRef.cursorOffset = next.length;
		}
	});

	useKeymapLayer(() => ({
		scope: "panel",
		when: active,
		diagnosticsWhen: active,
		commands: {
			"scratchpad.close": close,
		},
	}));

	return (
		<textarea
			ref={(value) => {
				textareaRef = value as TextareaRef | undefined;
				try {
					textareaRef?.setText(draft());
					if (textareaRef) textareaRef.cursorOffset = draft().length;
				} catch {
					textareaRef = undefined;
				}
			}}
			flexGrow={1}
			paddingX={1}
			placeholder="Type notes..."
			placeholderColor={theme.textPlaceholder}
			backgroundColor={theme.bg}
			focusedBackgroundColor={theme.bg}
			textColor={theme.textPrimary}
			focusedTextColor={theme.textPrimary}
			cursorColor={theme.cursor}
			showCursor={active()}
			wrapMode="word"
			overflow="scroll"
			focused={active()}
			keyBindings={[{ name: "return", shift: true, action: "newline" }]}
			onContentChange={() =>
				props.controller.setDraft(textareaRef?.plainText ?? "")
			}
		/>
	);
}

export type ScratchpadPanelProps = {
	controller: ScratchpadController;
	active?: boolean;
	onClose: () => void;
	onFocusRequest?: () => void;
};

export function ScratchpadPanel(props: ScratchpadPanelProps) {
	return (
		<box
			width="100%"
			height="100%"
			onMouseDown={(event) => {
				if (event.button === 0) props.onFocusRequest?.();
			}}
		>
			<WorkspacePanelLayout
				footer={<KeymapHintBar group="scratchpad" borderless />}
			>
				<ScratchpadContent
					controller={props.controller}
					active={props.active}
					onClose={props.onClose}
				/>
			</WorkspacePanelLayout>
		</box>
	);
}
