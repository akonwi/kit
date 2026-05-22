import { SettingsRowFrame } from "./SettingsRowFrame";
import type { BooleanSettingsRowData } from "./SettingsTypes";
import { Toggle } from "./Toggle";

type BooleanSettingsRowProps = {
	row: BooleanSettingsRowData;
	index: number;
};

export function BooleanSettingsRow(props: BooleanSettingsRowProps) {
	return (
		<SettingsRowFrame row={props.row} index={props.index}>
			<box paddingY={1}>
				<Toggle
					checked={props.row.checked}
					disabled={props.row.disabled === true}
				/>
			</box>
		</SettingsRowFrame>
	);
}
