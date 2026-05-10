import { For } from "solid-js";
import { theme } from "../../shell/theme";
import { useSettingsContext } from "./SettingsContext";
import { TABS } from "./SettingsTypes";

export function SettingsTabs() {
	const settings = useSettingsContext();

	return (
		<box flexShrink={0} flexDirection="row" gap={1}>
			<For each={TABS}>
				{(tab) => {
					const selected = () => tab.id === settings.activeTab();
					return (
						<box
							paddingX={2}
							border
							borderColor={
								selected() ? theme.borderAccent : theme.borderDefault
							}
							onMouseUp={() => {
								void settings.actions.switchTab(tab.id);
							}}
						>
							<text fg={selected() ? theme.textPrimary : theme.textMuted}>
								{tab.label}
							</text>
						</box>
					);
				}}
			</For>
		</box>
	);
}
