import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import { createSignal } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { Dialog } from "../../shell/Dialog";
import { type Binding, HintBar } from "../../shell/HintBar";
import { MessageComposer } from "../../shell/MessageComposer";

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
		<Dialog.Root
			width="72%"
			maxWidth={96}
			minWidth={52}
			surfaceProps={props.surfaceProps}
		>
			<Dialog.Header>
				<Dialog.Title>{props.title}</Dialog.Title>
				{props.subtitle ? <Dialog.Meta>{props.subtitle}</Dialog.Meta> : null}
			</Dialog.Header>
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
			<Dialog.Footer>
				<HintBar bindings={BINDINGS} />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
