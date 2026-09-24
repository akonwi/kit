import { CHEVRON_RIGHT } from "../../shell/glyphs";
import { theme } from "../../shell/theme";
import { SettingsRowFrame } from "./SettingsRowFrame";
import type { ChoiceSettingsRowData } from "./SettingsTypes";

type ChoiceSettingsRowProps = {
	row: ChoiceSettingsRowData;
	index: number;
};

export function ChoiceSettingsRow(props: ChoiceSettingsRowProps) {
	const value = () =>
		props.row.value.length > 24
			? `${props.row.value.slice(0, 23)}…`
			: props.row.value;

	return (
		<SettingsRowFrame row={props.row} index={props.index}>
			<box flexDirection="row" gap={1} paddingY={1}>
				<text fg={theme.textSecondary}>{value()}</text>
				<text fg={theme.textMuted}>{CHEVRON_RIGHT}</text>
			</box>
		</SettingsRowFrame>
	);
}
