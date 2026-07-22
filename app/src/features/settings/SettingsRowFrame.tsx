import type { JSX } from "solid-js";
import { theme } from "../../shell/theme";
import { useSettingsContext } from "./SettingsContext";
import type { SettingsRowData } from "./SettingsTypes";

type SettingsRowFrameProps = {
	row: SettingsRowData;
	index: number;
	children: JSX.Element;
};

export function SettingsRowFrame(props: SettingsRowFrameProps) {
	const settings = useSettingsContext();
	const focused = () => settings.isRowFocused(props.index);
	const disabled = () => props.row.disabled === true;
	const help = () =>
		props.row.help.length > 55
			? `${props.row.help.slice(0, 54)}…`
			: props.row.help;

	return (
		<box
			flexDirection="row"
			justifyContent="space-between"
			alignItems="flex-start"
			gap={2}
			minHeight={3}
			paddingX={1}
			backgroundColor={focused() ? theme.bgMuted : theme.bgTransparent}
			onMouseUp={() => {
				settings.actions.focusRow(props.index);
				void settings.actions.activateRow(props.index);
			}}
		>
			<box flexDirection="column" flexGrow={1} gap={0}>
				<text fg={disabled() ? theme.textMuted : theme.textPrimary}>
					{props.row.label}
				</text>
				<text fg={theme.textMuted}>{help()}</text>
			</box>

			<box flexShrink={0}>{props.children}</box>
		</box>
	);
}
