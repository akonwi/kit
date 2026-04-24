import { useKeyboard } from "@opentui/solid";
import { createMemo, createSignal, For, Show } from "solid-js";
import { resolveSpeechSettings, type Settings } from "../../settings";
import { theme } from "../../shell/theme";

type SettingsTabId = "general" | "notifications";
type EditableField = "speech.maxChars" | "speech.voice" | null;

type SettingsContentProps = {
	initialSettings: Settings;
	onSave: (settings: Settings) => Promise<void>;
	onClose: () => void;
};

type SettingsRow =
	| {
			id: "guidedQuestions" | "sessionNaming" | "pager" | "bells" | "speech";
			kind: "boolean";
			label: string;
			help: string;
			checked: boolean;
			disabled?: boolean;
	  }
	| {
			id: "speech.maxChars" | "speech.voice";
			kind: "input";
			label: string;
			help: string;
			value: string;
			placeholder?: string;
			disabled?: boolean;
	  };

const TABS: Array<{ id: SettingsTabId; label: string; description: string }> = [
	{
		id: "general",
		label: "General",
		description: "Core assistant behavior and session automation.",
	},
	{
		id: "notifications",
		label: "Notifications",
		description: "Bell and speech feedback after turns complete.",
	},
];

function cloneSettings(settings: Settings): Settings {
	return {
		...settings,
		speech:
			typeof settings.speech === "object" && settings.speech !== null
				? { ...settings.speech }
				: settings.speech,
	};
}

type ToggleProps = {
	checked: boolean;
	disabled: boolean;
};

function Toggle(props: ToggleProps) {
	const trackBackground = props.disabled
		? theme.bgMuted
		: props.checked
			? "#567fab"
			: theme.bgAccent;
	const knobBackground = props.disabled ? theme.textMuted : theme.textSecondary;

	return (
		<box
			width={4}
			height={1}
			backgroundColor={trackBackground}
			flexDirection="row"
			justifyContent={props.checked ? "flex-end" : "flex-start"}
			alignItems="center"
		>
			<box width={2} height={1} backgroundColor={knobBackground} />
		</box>
	);
}

export function SettingsContent(props: SettingsContentProps) {
	const [settings, setSettings] = createSignal<Settings>(
		cloneSettings(props.initialSettings),
	);
	const [activeTab, setActiveTab] = createSignal<SettingsTabId>("general");
	const [focusedRowIndex, setFocusedRowIndex] = createSignal(0);
	const [editingField, setEditingField] = createSignal<EditableField>(null);
	const [error, setError] = createSignal<string | null>(null);
	const [maxCharsDraft, setMaxCharsDraft] = createSignal(
		String(resolveSpeechSettings(props.initialSettings.speech).maxChars),
	);
	const [voiceDraft, setVoiceDraft] = createSignal(
		resolveSpeechSettings(props.initialSettings.speech).voice ?? "",
	);

	const rows = createMemo<SettingsRow[]>(() => {
		const currentSettings = settings();
		const speech = resolveSpeechSettings(currentSettings.speech);
		if (activeTab() === "general") {
			return [
				{
					id: "guidedQuestions",
					kind: "boolean",
					label: "Guided Questions",
					help: "Let the agent open structured questionnaires when it needs several answers.",
					checked: currentSettings.guidedQuestions !== false,
				},
				{
					id: "sessionNaming",
					kind: "boolean",
					label: "Auto-name Sessions",
					help: "Generate a session title automatically after the first couple of turns.",
					checked: currentSettings.sessionNaming !== false,
				},
				{
					id: "pager",
					kind: "boolean",
					label: "Auto-open Pager",
					help: "Open pager automatically for substantial assistant responses.",
					checked: currentSettings.pager !== false,
				},
			];
		}

		return [
			{
				id: "bells",
				kind: "boolean",
				label: "Bells",
				help: "Play a terminal bell when a turn completes.",
				checked: currentSettings.bells !== false,
			},
			{
				id: "speech",
				kind: "boolean",
				label: "Speech",
				help: "Speak assistant responses aloud.",
				checked: speech.enabled,
			},
			{
				id: "speech.maxChars",
				kind: "input",
				label: "Speech Max Chars",
				help: "Maximum response length to read aloud.",
				value: String(speech.maxChars),
				disabled: !speech.enabled,
			},
			{
				id: "speech.voice",
				kind: "input",
				label: "Voice",
				help: "Optional voice override.",
				value: speech.voice ?? "",
				placeholder: "system default",
				disabled: !speech.enabled,
			},
		];
	});

	function syncDrafts(nextSettings: Settings) {
		const speech = resolveSpeechSettings(nextSettings.speech);
		setMaxCharsDraft(String(speech.maxChars));
		setVoiceDraft(speech.voice ?? "");
	}

	async function persist(nextSettings: Settings): Promise<boolean> {
		try {
			await props.onSave(nextSettings);
			const cloned = cloneSettings(nextSettings);
			setSettings(cloned);
			syncDrafts(cloned);
			setError(null);
			return true;
		} catch (cause) {
			const message =
				cause instanceof Error
					? cause.message
					: String(cause || "Unknown error");
			setError(`Failed to save settings: ${message}`);
			return false;
		}
	}

	async function updateSettings(mutator: (current: Settings) => Settings) {
		const nextSettings = mutator(cloneSettings(settings()));
		await persist(nextSettings);
	}

	async function toggleBoolean(
		rowId: Extract<SettingsRow, { kind: "boolean" }>["id"],
	) {
		await updateSettings((current) => {
			switch (rowId) {
				case "guidedQuestions":
					return {
						...current,
						guidedQuestions: current.guidedQuestions === false,
					};
				case "sessionNaming":
					return { ...current, sessionNaming: current.sessionNaming === false };
				case "pager":
					return { ...current, pager: current.pager === false };
				case "bells":
					return { ...current, bells: current.bells === false };
				case "speech": {
					const speech = resolveSpeechSettings(current.speech);
					return {
						...current,
						speech: {
							enabled: !speech.enabled,
							maxChars: speech.maxChars,
							...(speech.voice ? { voice: speech.voice } : {}),
						},
					};
				}
			}
		});
	}

	async function commitEdit(field = editingField()): Promise<boolean> {
		if (!field) return true;
		if (field === "speech.maxChars") {
			const draft = maxCharsDraft().trim();
			if (draft.length === 0) {
				syncDrafts(settings());
				setEditingField(null);
				return true;
			}
			const parsed = Number.parseInt(draft, 10);
			if (!Number.isFinite(parsed) || parsed <= 0) {
				syncDrafts(settings());
				setEditingField(null);
				return true;
			}
			const currentSpeech = resolveSpeechSettings(settings().speech);
			const ok = await persist({
				...cloneSettings(settings()),
				speech: {
					enabled: currentSpeech.enabled,
					maxChars: parsed,
					...(currentSpeech.voice ? { voice: currentSpeech.voice } : {}),
				},
			});
			if (ok) setEditingField(null);
			return ok;
		}

		const currentSpeech = resolveSpeechSettings(settings().speech);
		const voice = voiceDraft().trim();
		const ok = await persist({
			...cloneSettings(settings()),
			speech: {
				enabled: currentSpeech.enabled,
				maxChars: currentSpeech.maxChars,
				...(voice ? { voice } : {}),
			},
		});
		if (ok) setEditingField(null);
		return ok;
	}

	function cancelEdit() {
		syncDrafts(settings());
		setEditingField(null);
	}

	function focusRow(index: number) {
		const currentRows = rows();
		if (currentRows.length === 0) {
			setFocusedRowIndex(0);
			return;
		}
		const bounded = Math.max(0, Math.min(index, currentRows.length - 1));
		setFocusedRowIndex(bounded);
	}

	async function activateRow(index = focusedRowIndex()) {
		const row = rows()[index];
		if (!row || row.disabled) return;
		if (row.kind === "boolean") {
			await toggleBoolean(row.id);
			return;
		}
		setError(null);
		setEditingField(row.id);
	}

	async function runAfterPendingEdit(action: () => void | Promise<void>) {
		if (editingField()) {
			const ok = await commitEdit();
			if (!ok) return;
		}
		await action();
	}

	async function switchTab(nextTab: SettingsTabId) {
		await runAfterPendingEdit(async () => {
			setActiveTab(nextTab);
			setFocusedRowIndex(0);
			setError(null);
		});
	}

	const activeTabMeta = createMemo(
		() => TABS.find((tab) => tab.id === activeTab()) ?? TABS[0],
	);

	function renderInputValue(row: Extract<SettingsRow, { kind: "input" }>) {
		const disabled = row.disabled === true;
		const editing = editingField() === row.id;
		const value = row.id === "speech.maxChars" ? maxCharsDraft() : voiceDraft();
		const display = row.value || row.placeholder || "";
		const minWidth = row.id === "speech.maxChars" ? 8 : 24;
		return (
			<box
				minWidth={minWidth}
				border
				borderColor={theme.borderDefault}
				backgroundColor={theme.bgSurface}
				paddingX={1}
			>
				<Show
					when={editing}
					fallback={
						<text fg={disabled ? theme.textMuted : theme.textSecondary}>
							{display}
						</text>
					}
				>
					<input
						focused
						width="100%"
						value={value}
						placeholder={row.placeholder}
						placeholderColor={theme.textPlaceholder}
						backgroundColor={theme.bgSurface}
						focusedBackgroundColor={theme.bgSurface}
						textColor={theme.textPrimary}
						focusedTextColor={theme.textPrimary}
						cursorColor={theme.cursor}
						onInput={(nextValue: string) => {
							if (row.id === "speech.maxChars") {
								if (/^\d*$/.test(nextValue)) setMaxCharsDraft(nextValue);
								return;
							}
							setVoiceDraft(nextValue);
						}}
					/>
				</Show>
			</box>
		);
	}

	useKeyboard((e) => {
		if (e.name === "escape") {
			e.preventDefault();
			if (editingField()) {
				cancelEdit();
				return;
			}
			props.onClose();
			return;
		}

		if (editingField()) {
			if (e.name === "return") {
				e.preventDefault();
				void commitEdit();
			}
			return;
		}

		if (e.name === "left") {
			e.preventDefault();
			const currentIndex = TABS.findIndex((tab) => tab.id === activeTab());
			const nextIndex = currentIndex <= 0 ? TABS.length - 1 : currentIndex - 1;
			void switchTab(TABS[nextIndex]?.id ?? "general");
			return;
		}

		if (e.name === "right") {
			e.preventDefault();
			const currentIndex = TABS.findIndex((tab) => tab.id === activeTab());
			const nextIndex = currentIndex >= TABS.length - 1 ? 0 : currentIndex + 1;
			void switchTab(TABS[nextIndex]?.id ?? "notifications");
			return;
		}

		if (e.name === "up") {
			e.preventDefault();
			focusRow(focusedRowIndex() - 1);
			return;
		}

		if (e.name === "down") {
			e.preventDefault();
			focusRow(focusedRowIndex() + 1);
			return;
		}

		if (e.name === "return" || e.name === "space") {
			e.preventDefault();
			void activateRow();
		}
	});

	return (
		<box
			position="absolute"
			left={0}
			top={0}
			bottom={0}
			width="100%"
			justifyContent="center"
			alignItems="center"
			zIndex={1150}
			backgroundColor={theme.modalBackdrop}
		>
			<box
				width="74%"
				maxWidth={104}
				minWidth={64}
				border
				borderStyle="double"
				borderColor={theme.borderFocused}
				backgroundColor={theme.bgSurface}
				padding={1}
				flexDirection="column"
				gap={1}
			>
				<box flexDirection="column" gap={0}>
					<box flexDirection="row" justifyContent="space-between">
						<text fg={theme.textPrimary}>Settings</text>
						<text fg={theme.textMuted}>~/.kit/settings.json</text>
					</box>
					<text fg={theme.textMuted}>{activeTabMeta().description}</text>
				</box>

				<box flexDirection="row" gap={1}>
					<For each={TABS}>
						{(tab) => {
							const selected = () => tab.id === activeTab();
							return (
								<box
									paddingX={2}
									border
									borderColor={
										selected() ? theme.borderAccent : theme.borderDefault
									}
									backgroundColor={selected() ? theme.bgMuted : theme.bgSurface}
									onMouseUp={() => {
										void switchTab(tab.id);
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

				<box border borderColor={theme.borderDefault} flexDirection="column">
					<For each={rows()}>
						{(row, index) => {
							const focused = () => index() === focusedRowIndex();
							const disabled = () => row.disabled === true;
							return (
								<box
									flexDirection="row"
									justifyContent="space-between"
									alignItems="center"
									gap={2}
									paddingX={1}
									paddingY={0}
									borderColor={
										focused() ? theme.borderAccent : theme.borderDefault
									}
									backgroundColor={focused() ? theme.bgMuted : theme.bgSurface}
									onMouseUp={() => {
										void runAfterPendingEdit(async () => {
											focusRow(index());
											if (row.kind === "boolean") {
												if (!disabled()) await toggleBoolean(row.id);
												return;
											}
											if (!disabled()) {
												setError(null);
												setEditingField(row.id);
											}
										});
									}}
								>
									<box flexDirection="column" flexGrow={1} gap={0}>
										<text fg={disabled() ? theme.textMuted : theme.textPrimary}>
											{focused() ? "› " : "  "}
											{row.label}
										</text>
										<text fg={theme.textMuted}>{row.help}</text>
									</box>

									<box flexShrink={0} justifyContent="center">
										{row.kind === "boolean" ? (
											<Toggle checked={row.checked} disabled={disabled()} />
										) : (
											renderInputValue(row)
										)}
									</box>
								</box>
							);
						}}
					</For>
				</box>

				<Show when={error()}>
					<box border borderColor={theme.errorText} paddingX={1}>
						<text fg={theme.errorText}>{error()}</text>
					</box>
				</Show>

				<box border borderColor={theme.borderDefault} paddingX={1}>
					<text fg={theme.textMuted}>
						{editingField()
							? "Enter save · Esc cancel · Click another tab or row to save"
							: "Esc close · ↑/↓ move · ←/→ tabs · Enter edit · Space toggle · Click supported"}
					</text>
				</box>
			</box>
		</box>
	);
}
