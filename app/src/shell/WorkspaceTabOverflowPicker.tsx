import type { PickerManager } from "../state/picker-manager";
import { Picker } from "./Picker";
import { theme } from "./theme";

export function WorkspaceTabOverflowPicker(props: {
	picker: PickerManager;
	width: number;
}) {
	return (
		<box
			position="absolute"
			top={3}
			right={1}
			width={props.width}
			height={12}
			zIndex={50}
			backgroundColor={theme.pickerBg}
			padding={1}
			onMouseDown={(event) => {
				event.preventDefault();
				event.stopPropagation();
			}}
		>
			<Picker.Root
				picker={props.picker}
				maxVisible={7}
				commandNamespace="workspace-overflow"
				selectHint="open"
			>
				<Picker.Header />
				<Picker.Body />
			</Picker.Root>
		</box>
	);
}
