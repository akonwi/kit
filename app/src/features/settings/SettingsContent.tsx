import { useBindings } from "@opentui/keymap/solid";
import { For, Match, Show, Switch } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { withKitKeyAliases } from "../../keymap/bindings";
import type { Settings } from "../../settings";
import { Dialog } from "../../shell/Dialog";
import type { Binding } from "../../shell/HintBar";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";
import type { SpeechVoiceDiscovery } from "../notifications/voices";
import { BooleanSettingsRow } from "./BooleanSettingsRow";
import { InputSettingsRow } from "./InputSettingsRow";
import { SelectSettingsRow } from "./SelectSettingsRow";
import {
	nextSettingsTab,
	SettingsProvider,
	useSettingsContext,
} from "./SettingsContext";
import { SettingsTabs } from "./SettingsTabs";
import type { EditableField } from "./SettingsTypes";

type SettingsContentProps = {
	initialSettings: Settings;
	speechVoices: SpeechVoiceDiscovery;
	userThemes: string[];
	onSave: (settings: Settings) => Promise<void>;
	onClose: () => void;
	surfaceProps?: OverlaySurfaceProps;
};

const SETTINGS_EDITING_SUFFIX_BINDINGS: Binding[] = [
	{ key: "Click", action: "another tab or row to save" },
];

function isNumericEditField(
	field: EditableField,
): field is Exclude<
	EditableField,
	"theme" | "diffs.view" | "speech.voice" | null
> {
	return (
		field === "speech.maxChars" ||
		field === "retry.maxRetries" ||
		field === "retry.baseDelayMs" ||
		field === "retry.maxDelayMs"
	);
}

function SettingsDialog(props: {
	onClose: () => void;
	surfaceProps?: OverlaySurfaceProps;
}) {
	const settings = useSettingsContext();

	const numericEditing = () => {
		const editingField = settings.editingField();
		return editingField ? isNumericEditField(editingField) : false;
	};

	useBindings(() =>
		withKitKeyAliases({
			enabled: () => !settings.editingField(),
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
					name: "settings.tab-previous",
					desc: "Switch to previous settings tab",
					group: "settings",
					hint: "tabs",
					run: () =>
						void settings.actions.switchTab(
							nextSettingsTab(settings.activeTab(), -1),
						),
				},
				{
					name: "settings.tab-next",
					desc: "Switch to next settings tab",
					group: "settings",
					hint: "tabs",
					run: () =>
						void settings.actions.switchTab(
							nextSettingsTab(settings.activeTab(), 1),
						),
				},
				{
					name: "settings.edit-row",
					desc: "Edit focused setting",
					group: "settings",
					hint: "edit",
					run: () => void settings.actions.activateRow(),
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
					key: "left",
					cmd: "settings.tab-previous",
					desc: "Switch to previous settings tab",
					group: "settings",
				},
				{
					key: "right",
					cmd: "settings.tab-next",
					desc: "Switch to next settings tab",
					group: "settings",
				},
				{
					key: "return",
					cmd: "settings.edit-row",
					desc: "Edit focused setting",
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

	useBindings(() =>
		withKitKeyAliases({
			enabled: () => Boolean(settings.editingField()),
			priority: 200,
			commands: [
				{
					name: "settings.cancel-edit",
					desc: "Cancel editing setting",
					group: "settings",
					hint: "cancel",
					run: settings.actions.cancelEdit,
				},
			],
			bindings: [
				{
					key: "escape",
					cmd: "settings.cancel-edit",
					desc: "Cancel editing setting",
					group: "settings",
				},
			],
		}),
	);

	useBindings(() =>
		withKitKeyAliases({
			enabled: numericEditing,
			priority: 200,
			commands: [
				{
					name: "settings.commit-edit",
					desc: "Save edited setting",
					group: "settings",
					hint: "save",
					run: () => void settings.actions.commitEdit(),
				},
			],
			bindings: [
				{
					key: "return",
					cmd: "settings.commit-edit",
					desc: "Save edited setting",
					group: "settings",
				},
			],
		}),
	);

	return (
		<Dialog.Root
			width="74%"
			maxWidth={104}
			minWidth={64}
			height="70%"
			surfaceProps={props.surfaceProps}
		>
			<Dialog.Header>
				<Dialog.Title>Settings</Dialog.Title>
				<Dialog.Meta>~/.kit/settings.json</Dialog.Meta>
			</Dialog.Header>

			<Dialog.Body>
				<SettingsTabs />

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
									<Match when={row.kind === "input" ? row : undefined}>
										{(inputRow) => (
											<InputSettingsRow row={inputRow()} index={index()} />
										)}
									</Match>
									<Match when={row.kind === "select" ? row : undefined}>
										{(selectRow) => (
											<SelectSettingsRow row={selectRow()} index={index()} />
										)}
									</Match>
								</Switch>
							)}
						</For>
					</box>
				</scrollbox>

				<Show when={settings.error()}>
					<box border borderColor={theme.errorText} paddingX={1}>
						<text fg={theme.errorText}>{settings.error()}</text>
					</box>
				</Show>
			</Dialog.Body>

			<Dialog.Footer>
				<box>
					<KeymapHintBar
						borderless
						group="settings"
						suffixBindings={
							settings.editingField() ? SETTINGS_EDITING_SUFFIX_BINDINGS : []
						}
					/>
				</box>
			</Dialog.Footer>
		</Dialog.Root>
	);
}

export function SettingsContent(props: SettingsContentProps) {
	return (
		<SettingsProvider
			initialSettings={props.initialSettings}
			speechVoices={props.speechVoices}
			userThemes={props.userThemes}
			onSave={props.onSave}
		>
			<SettingsDialog
				onClose={props.onClose}
				surfaceProps={props.surfaceProps}
			/>
		</SettingsProvider>
	);
}
