import { createEffect, createSignal } from "solid-js";
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
	onEditingChange?: (editing: boolean) => void;
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
		props.onEditingChange?.(props.controller.editing());
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
	showEdgeBorder?: boolean;
	onClose: () => void;
	onFocusRequest?: () => void;
};

export function ScratchpadPanel(props: ScratchpadPanelProps) {
	const [editing, setEditing] = createSignal(false);
	return (
		<box
			width="100%"
			height="100%"
			border={props.showEdgeBorder === false ? false : ["left"]}
			borderColor={editing() ? theme.borderAccent : theme.borderDefault}
			onMouseDown={(event) => {
				if (event.button === 0) props.onFocusRequest?.();
			}}
		>
			<WorkspacePanelLayout
				header={
					<box
						flexShrink={0}
						paddingX={1}
						border={["bottom"]}
						borderColor={theme.borderDefault}
					>
						<text fg={theme.textPrimary}>Scratchpad</text>
					</box>
				}
				footer={<KeymapHintBar group="scratchpad" borderless />}
			>
				<ScratchpadContent
					controller={props.controller}
					active={props.active}
					onClose={props.onClose}
					onEditingChange={setEditing}
				/>
			</WorkspacePanelLayout>
		</box>
	);
}
