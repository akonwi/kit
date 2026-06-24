import { createEffect, createSignal, Show } from "solid-js";
import type {
	OverlayComponentProps,
	OverlaySurfaceProps,
} from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { Dialog } from "../../shell/Dialog";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";
import type { ScratchpadController } from "./controller";

export const SCRATCHPAD_MIN_WIDTH = 120;
export const SCRATCHPAD_FRACTION = 0.3;
export const SCRATCHPAD_MIN_COLS = 30;

export type ScratchpadSurfaceMode = "panel" | "dialog";

type TextareaRef = {
	plainText: string;
	cursorOffset: number;
	setText: (value: string) => void;
};

type ScratchpadContentProps = {
	controller: ScratchpadController;
	mode: ScratchpadSurfaceMode;
	active?: boolean;
	onClose: () => void;
	onEditingChange?: (editing: boolean) => void;
};

function ScratchpadContent(props: ScratchpadContentProps) {
	let textareaRef: TextareaRef | undefined;

	const active = () => props.active !== false;
	const draft = () => props.controller.draft();
	const scope = () => (props.mode === "dialog" ? "modal" : "panel");

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
		scope: scope(),
		when: active,
		diagnosticsWhen: active,
		commands: {
			"scratchpad.close": close,
		},
	}));

	return (
		<box
			width="100%"
			height="100%"
			flexDirection="column"
			backgroundColor={props.mode === "dialog" ? theme.bgSurface : theme.bg}
		>
			<Show when={props.mode === "panel"}>
				<box
					flexShrink={0}
					paddingX={1}
					border={["bottom"]}
					borderColor={theme.borderDefault}
				>
					<text fg={theme.textPrimary}>Scratchpad</text>
				</box>
			</Show>

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
				backgroundColor={props.mode === "dialog" ? theme.bgSurface : theme.bg}
				focusedBackgroundColor={
					props.mode === "dialog" ? theme.bgSurface : theme.bg
				}
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

			<box flexShrink={0}>
				<KeymapHintBar
					group="scratchpad"
					borderless={props.mode === "dialog"}
				/>
			</box>
		</box>
	);
}

export type ScratchpadPanelProps = {
	controller: ScratchpadController;
	active?: boolean;
	onClose: () => void;
};

export function ScratchpadPanel(props: ScratchpadPanelProps) {
	const [editing, setEditing] = createSignal(false);
	return (
		<box
			width="100%"
			height="100%"
			border={["left"]}
			borderColor={editing() ? theme.borderAccent : theme.borderDefault}
		>
			<ScratchpadContent
				controller={props.controller}
				mode="panel"
				active={props.active}
				onClose={props.onClose}
				onEditingChange={setEditing}
			/>
		</box>
	);
}

export type ScratchpadDialogProps = OverlayComponentProps<void> & {
	controller: ScratchpadController;
	surfaceProps?: OverlaySurfaceProps;
};

export function ScratchpadDialog(props: ScratchpadDialogProps) {
	return (
		<Dialog.Root
			width="70%"
			maxWidth={90}
			minWidth={48}
			height="70%"
			surfaceProps={props.surfaceProps}
		>
			<Dialog.Header>
				<Dialog.Title>Scratchpad</Dialog.Title>
			</Dialog.Header>
			<Dialog.Body>
				<ScratchpadContent
					controller={props.controller}
					mode="dialog"
					active={props.active}
					onClose={() => props.done(undefined)}
				/>
			</Dialog.Body>
		</Dialog.Root>
	);
}
