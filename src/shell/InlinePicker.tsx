import { Show } from "solid-js";
import type { PickerManager } from "../state/picker-manager";
import { KeymapHintBar } from "./KeymapHintBar";
import { Picker } from "./Picker";
import { theme } from "./theme";

export type InlinePickerProps = {
	picker: PickerManager;
	bottomOffset: number;
};

export function InlinePicker(props: InlinePickerProps) {
	const snapshot = () => props.picker.current();

	return (
		<Show when={snapshot().visible}>
			<box
				position="absolute"
				bottom={props.bottomOffset}
				left={0}
				width="100%"
				zIndex={100}
				backgroundColor={theme.pickerBg}
				paddingX={1}
				flexDirection="column"
			>
				<Picker.Root
					picker={props.picker}
					commandNamespace="picker"
					selectHint="insert"
				>
					<Picker.Header />
					<Picker.Body />
					<Picker.Footer paddingBottom={1}>
						<KeymapHintBar borderless group="picker" />
					</Picker.Footer>
				</Picker.Root>
			</box>
		</Show>
	);
}
