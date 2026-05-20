import type { Accessor } from "solid-js";
import { Show } from "solid-js";
import type { KeybindingDiagnostic } from "../keymap/bindings";
import type { Settings } from "../settings";
import type { PickerManager } from "../state/picker-manager";
import { Dialog } from "./Dialog";
import { KeymapHintBar } from "./KeymapHintBar";
import { Picker } from "./Picker";

const MAX_VISIBLE = 12;

export type CommandPaletteProps = {
	settings: Accessor<Settings>;
	onKeybindingDiagnostic?: (diagnostic: KeybindingDiagnostic) => void;
	picker: PickerManager;
};

export function CommandPalette(props: CommandPaletteProps) {
	const snapshot = () => props.picker.current();
	return (
		<Show when={snapshot().visible}>
			<Dialog.Root
				width="80%"
				minWidth={96}
				height={MAX_VISIBLE + 6}
				padding={0}
			>
				<box flexGrow={1} flexDirection="column" paddingX={1}>
					<Picker.Root
						settings={props.settings}
						onKeybindingDiagnostic={props.onKeybindingDiagnostic}
						picker={props.picker}
						maxVisible={MAX_VISIBLE}
						commandNamespace="command-palette"
						includeCompleteBinding
						selectHint="run"
					>
						<Picker.Header />
						<Picker.Body />
						<Picker.Footer>
							<KeymapHintBar borderless group="command-palette" />
						</Picker.Footer>
					</Picker.Root>
				</box>
			</Dialog.Root>
		</Show>
	);
}
