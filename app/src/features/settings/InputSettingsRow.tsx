import { Show } from "solid-js";
import { theme } from "../../shell/theme";
import { useSettingsContext } from "./SettingsContext";
import { SettingsRowFrame } from "./SettingsRowFrame";
import type { InputSettingsRowData } from "./SettingsTypes";

type InputSettingsRowProps = {
	row: InputSettingsRowData;
	index: number;
};

export function InputSettingsRow(props: InputSettingsRowProps) {
	const settings = useSettingsContext();
	const disabled = () => props.row.disabled === true;
	const focused = () => settings.isRowFocused(props.index);
	const editing = () => settings.isEditing(props.row.id);
	const display = () => props.row.value || props.row.placeholder || "";

	return (
		<SettingsRowFrame row={props.row} index={props.index}>
			<box
				minWidth={8}
				border
				borderColor={
					editing()
						? theme.borderAccent
						: focused()
							? theme.borderFocused
							: theme.borderDefault
				}
				backgroundColor={theme.bgTransparent}
				paddingX={1}
			>
				<Show
					when={editing()}
					fallback={
						<text fg={disabled() ? theme.textMuted : theme.textSecondary}>
							{display()}
						</text>
					}
				>
					<input
						focused
						width="100%"
						value={settings.inputDraft(props.row.id)}
						placeholder={props.row.placeholder}
						placeholderColor={theme.textPlaceholder}
						backgroundColor={theme.bgTransparent}
						focusedBackgroundColor={theme.bgTransparent}
						textColor={theme.textPrimary}
						focusedTextColor={theme.textPrimary}
						cursorColor={theme.cursor}
						onInput={(nextValue: string) =>
							settings.setInputDraft(props.row.id, nextValue)
						}
					/>
				</Show>
			</box>
		</SettingsRowFrame>
	);
}
