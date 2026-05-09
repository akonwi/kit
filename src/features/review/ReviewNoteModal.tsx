import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import { createSignal } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { DialogFrame } from "../../shell/DialogFrame";
import { type Binding, HintBar } from "../../shell/HintBar";
import { MessageComposer } from "../../shell/MessageComposer";
import { theme } from "../../shell/theme";

const BINDINGS: Binding[] = [
	{ key: "Enter", action: "save" },
	{ key: "Shift+Enter", action: "newline" },
	{ key: "Esc", action: "cancel" },
];

export type ReviewNoteModalProps = {
	title: string;
	subtitle?: string;
	initialValue?: string;
	placeholder?: string;
	surfaceProps?: OverlaySurfaceProps;
	onClose: (value: string | null) => void;
};

export function ReviewNoteModal(props: ReviewNoteModalProps) {
	const [value, setValue] = createSignal(props.initialValue ?? "");
	let textareaRef:
		| { plainText: string; setText: (next: string) => void }
		| undefined;

	function save() {
		props.onClose(value());
	}

	function cancel() {
		props.onClose(null);
	}

	useKeyboard((event: KeyEvent) => {
		if (event.name === "escape") {
			event.preventDefault();
			cancel();
		}
	});

	return (
		<DialogFrame
			width="72%"
			maxWidth={96}
			minWidth={52}
			surfaceProps={props.surfaceProps}
		>
			<text fg={theme.textPrimary}>{props.title}</text>
			{props.subtitle ? (
				<text fg={theme.textMuted}>{props.subtitle}</text>
			) : null}
			<MessageComposer
				ref={(value) => {
					textareaRef = value as typeof textareaRef;
				}}
				initialValue={props.initialValue ?? ""}
				placeholder={props.placeholder ?? "Type your review note..."}
				maxHeight={10}
				keyBindings={[
					{ name: "return", action: "submit" },
					{ name: "return", shift: true, action: "newline" },
				]}
				onContentChange={() => setValue(textareaRef?.plainText ?? "")}
				onSubmit={save}
			/>
			<HintBar bindings={BINDINGS} />
		</DialogFrame>
	);
}
