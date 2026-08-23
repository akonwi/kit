import { Show } from "solid-js";
import type { PickerManager } from "../state/picker-manager";
import { Dialog } from "./Dialog";
import { KeymapHintBar } from "./KeymapHintBar";
import { Picker } from "./Picker";

export function WorkspaceFileFinderPicker(props: {
	picker: PickerManager;
	availableWidth: number;
}) {
	return (
		<Show when={props.picker.current().visible}>
			<Dialog.Root
				width="80%"
				minWidth={Math.min(72, Math.max(1, props.availableWidth - 2))}
				maxWidth={120}
				height={18}
				padding={0}
			>
				<Picker.Root
					picker={props.picker}
					maxVisible={12}
					commandNamespace="workspace-file-finder"
					selectHint="open"
				>
					<Picker.Header />
					<Picker.Body />
					<Picker.Footer flexDirection="column">
						<KeymapHintBar borderless group="workspace-file-finder" />
					</Picker.Footer>
				</Picker.Root>
			</Dialog.Root>
		</Show>
	);
}
