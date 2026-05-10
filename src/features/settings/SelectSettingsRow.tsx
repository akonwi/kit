import { Show } from "solid-js";
import { theme } from "../../shell/theme";
import { useSettingsContext } from "./SettingsContext";
import { SettingsRowFrame } from "./SettingsRowFrame";
import type { SelectSettingsRowData } from "./SettingsTypes";

type SelectSettingsRowProps = {
	row: SelectSettingsRowData;
	index: number;
};

export function SelectSettingsRow(props: SelectSettingsRowProps) {
	const settings = useSettingsContext();
	const disabled = () => props.row.disabled === true;
	const focused = () => settings.isRowFocused(props.index);
	const editing = () => settings.isEditing(props.row.id);
	const display = () => props.row.value || props.row.placeholder || "";

	return (
		<SettingsRowFrame row={props.row} index={props.index}>
			<box
				minWidth={settings.selectMinWidth(props.row.id)}
				border
				borderColor={
					editing()
						? theme.borderAccent
						: focused()
							? theme.borderFocused
							: theme.borderDefault
				}
				backgroundColor={theme.bgTransparent}
				paddingX={editing() ? 0 : 1}
			>
				<Show
					when={editing()}
					fallback={
						<text fg={disabled() ? theme.textMuted : theme.textSecondary}>
							{display()}
						</text>
					}
				>
					<select
						focused
						height={settings.selectHeight(props.row.id)}
						showDescription={settings.showSelectDescription(props.row.id)}
						options={settings.selectOptions(props.row.id)}
						selectedIndex={settings.selectSelectedIndex(props.row.id)}
						selectedBackgroundColor={theme.pickerFocusedBg}
						selectedTextColor={theme.pickerFocusedText}
						onChange={(index, option) => {
							settings.setSelectDraft(props.row.id, index, option?.value);
						}}
						onSelect={(index, option) => {
							settings.commitSelect(props.row.id, index, option?.value);
						}}
					/>
				</Show>
			</box>
		</SettingsRowFrame>
	);
}
