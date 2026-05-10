import { useKeyboard } from "@opentui/solid";
import { For, Match, Show, Switch } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import type { Settings } from "../../settings";
import { Dialog } from "../../shell/Dialog";
import { type Binding, HintBar } from "../../shell/HintBar";
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

const SETTINGS_BINDINGS: { editing: Binding[]; browsing: Binding[] } = {
	editing: [
		{ key: "Enter", action: "save" },
		{ key: "Esc", action: "cancel" },
		{ key: "Click", action: "another tab or row to save" },
	],
	browsing: [
		{ key: "Esc", action: "close" },
		{ key: "↑/↓", action: "move" },
		{ key: "←/→", action: "tabs" },
		{ key: "Enter", action: "edit" },
		{ key: "Space", action: "toggle" },
	],
};

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

	useKeyboard((e) => {
		if (e.name === "escape") {
			e.preventDefault();
			if (settings.editingField()) {
				settings.actions.cancelEdit();
				return;
			}
			props.onClose();
			return;
		}

		const editingField = settings.editingField();
		if (editingField) {
			if (e.name === "return" && isNumericEditField(editingField)) {
				e.preventDefault();
				void settings.actions.commitEdit();
			}
			return;
		}

		if (e.name === "left") {
			e.preventDefault();
			void settings.actions.switchTab(
				nextSettingsTab(settings.activeTab(), -1),
			);
			return;
		}

		if (e.name === "right") {
			e.preventDefault();
			void settings.actions.switchTab(nextSettingsTab(settings.activeTab(), 1));
			return;
		}

		if (e.name === "up") {
			e.preventDefault();
			settings.actions.focusRow(settings.focusedRowIndex() - 1);
			return;
		}

		if (e.name === "down") {
			e.preventDefault();
			settings.actions.focusRow(settings.focusedRowIndex() + 1);
			return;
		}

		if (e.name === "return" || e.name === "space") {
			e.preventDefault();
			void settings.actions.activateRow();
		}
	});

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
					<HintBar
						borderless
						bindings={
							SETTINGS_BINDINGS[
								settings.editingField() ? "editing" : "browsing"
							]
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
