import { useKeyboard } from "@opentui/solid";
import { createMemo, createSignal, For, Show } from "solid-js";
import {
	resolveRetrySettings,
	resolveSpeechSettings,
	type Settings,
} from "../../settings";
import { theme } from "../../shell/theme";
import type { SpeechVoiceDiscovery } from "../notifications/voices";

type SettingsTabId = "general" | "notifications";
type EditableField =
	| "speech.maxChars"
	| "speech.voice"
	| "retry.maxRetries"
	| "retry.baseDelayMs"
	| "retry.maxDelayMs"
	| null;

type SettingsContentProps = {
	initialSettings: Settings;
	speechVoices: SpeechVoiceDiscovery;
	onSave: (settings: Settings) => Promise<void>;
	onClose: () => void;
};

type SettingsRow =
	| {
			id:
				| "guidedQuestions"
				| "sessionNaming"
				| "pager"
				| "bells"
				| "speech"
				| "retry.enabled";
			kind: "boolean";
			label: string;
			help: string;
			checked: boolean;
			disabled?: boolean;
	  }
	| {
			id:
				| "speech.maxChars"
				| "retry.maxRetries"
				| "retry.baseDelayMs"
				| "retry.maxDelayMs";
			kind: "input";
			label: string;
			help: string;
			value: string;
			placeholder?: string;
			disabled?: boolean;
	  }
	| {
			id: "speech.voice";
			kind: "select";
			label: string;
			help: string;
			value: string;
			placeholder?: string;
			disabled?: boolean;
	  };

const TABS: Array<{ id: SettingsTabId; label: string }> = [
	{
		id: "general",
		label: "General",
	},
	{
		id: "notifications",
		label: "Notifications",
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
	const [retryMaxRetriesDraft, setRetryMaxRetriesDraft] = createSignal(
		String(resolveRetrySettings(props.initialSettings.retry).maxRetries),
	);
	const [retryBaseDelayDraft, setRetryBaseDelayDraft] = createSignal(
		String(resolveRetrySettings(props.initialSettings.retry).baseDelayMs),
	);
	const [retryMaxDelayDraft, setRetryMaxDelayDraft] = createSignal(
		String(resolveRetrySettings(props.initialSettings.retry).maxDelayMs),
	);
	const [voiceDraft, setVoiceDraft] = createSignal(
		resolveSpeechSettings(props.initialSettings.speech).voice ?? "",
	);
	const [voiceSelectedIndex, setVoiceSelectedIndex] = createSignal(0);

	const voiceOptions = createMemo(() => [
		{
			name: "System Default",
			description: "Use the macOS default speech voice",
			value: "",
		},
		...props.speechVoices.voices.map((voice) => ({
			name: voice.name,
			description: voice.locale ?? voice.sample ?? "",
			value: voice.name,
		})),
	]);

	const rows = createMemo<SettingsRow[]>(() => {
		const currentSettings = settings();
		const speech = resolveSpeechSettings(currentSettings.speech);
		const retry = resolveRetrySettings(currentSettings.retry);
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
				{
					id: "retry.enabled",
					kind: "boolean",
					label: "Auto-retry Errors",
					help: "Retry transient provider and server failures with exponential backoff.",
					checked: retry.enabled,
				},
				{
					id: "retry.maxRetries",
					kind: "input",
					label: "Retry Attempts",
					help: "Maximum retry attempts for transient errors.",
					value: String(retry.maxRetries),
					disabled: !retry.enabled,
				},
				{
					id: "retry.baseDelayMs",
					kind: "input",
					label: "Retry Base Delay",
					help: "Base delay in milliseconds. Retries back off exponentially from this value.",
					value: String(retry.baseDelayMs),
					disabled: !retry.enabled,
				},
				{
					id: "retry.maxDelayMs",
					kind: "input",
					label: "Retry Max Delay",
					help: "Maximum server-requested retry delay to honor, in milliseconds.",
					value: String(retry.maxDelayMs),
					disabled: !retry.enabled,
				},
			];
		}

		const voiceHelp = !props.speechVoices.supported
			? props.speechVoices.reason
			: props.speechVoices.voices.length === 0
				? "No macOS voices were discovered."
				: "Choose which macOS voice to use for speech notifications.";

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
				kind: "select",
				label: "Voice",
				help: voiceHelp,
				value: speech.voice ?? "",
				placeholder: "System Default",
				disabled:
					!speech.enabled ||
					!props.speechVoices.supported ||
					props.speechVoices.voices.length === 0,
			},
		];
	});

	function resolveVoiceIndex(value: string): number {
		const index = voiceOptions().findIndex((option) => option.value === value);
		return index >= 0 ? index : 0;
	}

	function syncDrafts(nextSettings: Settings) {
		const speech = resolveSpeechSettings(nextSettings.speech);
		const retry = resolveRetrySettings(nextSettings.retry);
		setMaxCharsDraft(String(speech.maxChars));
		setRetryMaxRetriesDraft(String(retry.maxRetries));
		setRetryBaseDelayDraft(String(retry.baseDelayMs));
		setRetryMaxDelayDraft(String(retry.maxDelayMs));
		setVoiceDraft(speech.voice ?? "");
		setVoiceSelectedIndex(resolveVoiceIndex(speech.voice ?? ""));
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
				case "retry.enabled": {
					const retry = resolveRetrySettings(current.retry);
					return {
						...current,
						retry: {
							enabled: !retry.enabled,
							maxRetries: retry.maxRetries,
							baseDelayMs: retry.baseDelayMs,
							maxDelayMs: retry.maxDelayMs,
						},
					};
				}
			}
		});
	}

	async function commitEdit(field = editingField()): Promise<boolean> {
		if (!field) return true;
		if (
			field === "speech.maxChars" ||
			field === "retry.maxRetries" ||
			field === "retry.baseDelayMs" ||
			field === "retry.maxDelayMs"
		) {
			const draft =
				field === "speech.maxChars"
					? maxCharsDraft().trim()
					: field === "retry.maxRetries"
						? retryMaxRetriesDraft().trim()
						: field === "retry.baseDelayMs"
							? retryBaseDelayDraft().trim()
							: retryMaxDelayDraft().trim();
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
			const currentSettings = cloneSettings(settings());
			if (field === "speech.maxChars") {
				const currentSpeech = resolveSpeechSettings(currentSettings.speech);
				const ok = await persist({
					...currentSettings,
					speech: {
						enabled: currentSpeech.enabled,
						maxChars: parsed,
						...(currentSpeech.voice ? { voice: currentSpeech.voice } : {}),
					},
				});
				if (ok) setEditingField(null);
				return ok;
			}
			const currentRetry = resolveRetrySettings(currentSettings.retry);
			const ok = await persist({
				...currentSettings,
				retry: {
					enabled: currentRetry.enabled,
					maxRetries:
						field === "retry.maxRetries" ? parsed : currentRetry.maxRetries,
					baseDelayMs:
						field === "retry.baseDelayMs" ? parsed : currentRetry.baseDelayMs,
					maxDelayMs:
						field === "retry.maxDelayMs" ? parsed : currentRetry.maxDelayMs,
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
		if (row.kind === "select") {
			setVoiceSelectedIndex(resolveVoiceIndex(voiceDraft()));
		}
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

	function renderInputValue(row: Extract<SettingsRow, { kind: "input" }>) {
		const disabled = row.disabled === true;
		const editing = editingField() === row.id;
		const value =
			row.id === "speech.maxChars"
				? maxCharsDraft()
				: row.id === "retry.maxRetries"
					? retryMaxRetriesDraft()
					: row.id === "retry.baseDelayMs"
						? retryBaseDelayDraft()
						: retryMaxDelayDraft();
		const display = row.value || row.placeholder || "";
		return (
			<box
				minWidth={8}
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
							if (!/^\d*$/.test(nextValue)) return;
							if (row.id === "speech.maxChars") setMaxCharsDraft(nextValue);
							else if (row.id === "retry.maxRetries")
								setRetryMaxRetriesDraft(nextValue);
							else if (row.id === "retry.baseDelayMs")
								setRetryBaseDelayDraft(nextValue);
							else setRetryMaxDelayDraft(nextValue);
						}}
					/>
				</Show>
			</box>
		);
	}

	function renderSelectValue(row: Extract<SettingsRow, { kind: "select" }>) {
		const disabled = row.disabled === true;
		const editing = editingField() === row.id;
		const display = row.value || row.placeholder || "";
		return (
			<box
				minWidth={28}
				border
				borderColor={theme.borderDefault}
				backgroundColor={theme.bgSurface}
				paddingX={editing ? 0 : 1}
			>
				<Show
					when={editing}
					fallback={
						<text fg={disabled ? theme.textMuted : theme.textSecondary}>
							{display}
						</text>
					}
				>
					<select
						focused
						height={Math.min(6, Math.max(2, voiceOptions().length))}
						options={voiceOptions()}
						selectedIndex={voiceSelectedIndex()}
						selectedBackgroundColor={theme.pickerFocusedBg}
						selectedTextColor={theme.pickerFocusedText}
						onChange={(index, option) => {
							setVoiceSelectedIndex(index);
							setVoiceDraft(String(option?.value ?? ""));
						}}
						onSelect={(index, option) => {
							setVoiceSelectedIndex(index);
							setVoiceDraft(String(option?.value ?? ""));
							void commitEdit("speech.voice");
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
			if (
				e.name === "return" &&
				(editingField() === "speech.maxChars" ||
					editingField() === "retry.maxRetries" ||
					editingField() === "retry.baseDelayMs" ||
					editingField() === "retry.maxDelayMs")
			) {
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
												if (row.kind === "select") {
													setVoiceSelectedIndex(
														resolveVoiceIndex(voiceDraft()),
													);
												}
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
										) : row.kind === "input" ? (
											renderInputValue(row)
										) : (
											renderSelectValue(row)
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
