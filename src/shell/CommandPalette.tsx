import { Show } from "solid-js";
import type { PickerManager } from "../state/picker-manager";
import { Dialog } from "./Dialog";
import { type Binding, HintBar } from "./HintBar";
import { Picker } from "./Picker";

const MAX_VISIBLE = 12;

const LIST_BINDINGS: Binding[] = [
	{ key: "↑/↓", action: "move" },
	{ key: "Enter", action: "run" },
	{ key: "Esc", action: "close" },
];

const INPUT_BINDINGS: Binding[] = [
	{ key: "Enter", action: "submit" },
	{ key: "Esc", action: "cancel" },
];

export type CommandPaletteProps = {
	picker: PickerManager;
};

export function CommandPalette(props: CommandPaletteProps) {
	const snapshot = () => props.picker.current();
	const bindings = () =>
		snapshot().mode === "input" ? INPUT_BINDINGS : LIST_BINDINGS;

	return (
		<Show when={snapshot().visible}>
			<Dialog.Root
				width="80%"
				minWidth={96}
				height={MAX_VISIBLE + 6}
				padding={0}
			>
				<box flexGrow={1} flexDirection="column" paddingX={1}>
					<Picker.Root picker={props.picker} maxVisible={MAX_VISIBLE}>
						<Picker.Header />
						<Picker.Body />
						<Picker.Footer>
							<HintBar borderless bindings={bindings()} />
						</Picker.Footer>
					</Picker.Root>
				</box>
			</Dialog.Root>
		</Show>
	);
}
