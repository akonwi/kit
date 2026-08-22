import { useBindings } from "@opentui/keymap/solid";
import { For, Match, Show, Switch } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { withKitKeyAliases } from "../../keymap/bindings";
import type { Settings } from "../../settings";
import { Dialog } from "../../shell/Dialog";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";
import { BooleanSettingsRow } from "./BooleanSettingsRow";
import { ChoiceSettingsRow } from "./ChoiceSettingsRow";
import { SettingsProvider, useSettingsContext } from "./SettingsContext";
import type { SettingsModelOption } from "./SettingsTypes";

type SettingsContentProps = {
	initialSettings: Settings;
	modelOptions: SettingsModelOption[];
	onSelectDefaultModel: (
		currentSelector: string | undefined,
	) => Promise<string | null | undefined>;
	onSave: (settings: Settings) => Promise<void>;
	onClose: () => void;
	active?: boolean;
	surfaceProps?: OverlaySurfaceProps;
};

function SettingsDialog(props: {
	onClose: () => void;
	active: boolean;
	surfaceProps?: OverlaySurfaceProps;
}) {
	const settings = useSettingsContext();

	useBindings(() =>
		withKitKeyAliases({
			enabled: () => props.active,
			priority: 200,
			commands: [
				{
					name: "settings.close",
					desc: "Close settings",
					group: "settings",
					hint: "close",
					run: props.onClose,
				},
				{
					name: "settings.row-up",
					desc: "Move to previous setting",
					group: "settings",
					hint: "move",
					run: () => settings.actions.focusRow(settings.focusedRowIndex() - 1),
				},
				{
					name: "settings.row-down",
					desc: "Move to next setting",
					group: "settings",
					hint: "move",
					run: () => settings.actions.focusRow(settings.focusedRowIndex() + 1),
				},
				{
					name: "settings.toggle-row",
					desc: "Toggle focused setting",
					group: "settings",
					hint: "toggle",
					run: () => void settings.actions.activateRow(),
				},
			],
			bindings: [
				{
					key: "escape",
					cmd: "settings.close",
					desc: "Close settings",
					group: "settings",
				},
				{
					key: "up",
					cmd: "settings.row-up",
					desc: "Move to previous setting",
					group: "settings",
				},
				{
					key: "down",
					cmd: "settings.row-down",
					desc: "Move to next setting",
					group: "settings",
				},
				{
					key: "return",
					cmd: "settings.toggle-row",
					desc: "Toggle focused setting",
					group: "settings",
				},
				{
					key: "space",
					cmd: "settings.toggle-row",
					desc: "Toggle focused setting",
					group: "settings",
				},
			],
		}),
	);

	return (
		<Dialog.Root
			width="60%"
			maxWidth={80}
			minWidth={56}
			height="35%"
			padding={0}
			gap={0}
			surfaceProps={props.surfaceProps}
		>
			<Dialog.Header strip>
				<Dialog.Title>Settings</Dialog.Title>
				<Dialog.Meta>~/.kit/settings.json</Dialog.Meta>
			</Dialog.Header>

			<Dialog.Body paddingBottom={1} overflow="hidden">
				<scrollbox flexGrow={1} scrollY>
					<box flexDirection="column" gap={1}>
						<For each={settings.rows()}>
							{(row, index) => (
								<Switch>
									<Match when={row.kind === "boolean" ? row : undefined}>
										{(booleanRow) => (
											<BooleanSettingsRow row={booleanRow()} index={index()} />
										)}
									</Match>
									<Match when={row.kind === "choice" ? row : undefined}>
										{(choiceRow) => (
											<ChoiceSettingsRow row={choiceRow()} index={index()} />
										)}
									</Match>
								</Switch>
							)}
						</For>
					</box>
				</scrollbox>

				<Show when={settings.error()}>
					<box border borderColor={theme.errorText} paddingX={1} marginX={1}>
						<text fg={theme.errorText}>{settings.error()}</text>
					</box>
				</Show>
			</Dialog.Body>

			<Dialog.Footer strip>
				<KeymapHintBar borderless group="settings" />
			</Dialog.Footer>
		</Dialog.Root>
	);
}

export function SettingsContent(props: SettingsContentProps) {
	return (
		<SettingsProvider
			initialSettings={props.initialSettings}
			modelOptions={props.modelOptions}
			onSelectDefaultModel={props.onSelectDefaultModel}
			onSave={props.onSave}
		>
			<SettingsDialog
				onClose={props.onClose}
				active={props.active !== false}
				surfaceProps={props.surfaceProps}
			/>
		</SettingsProvider>
	);
}
