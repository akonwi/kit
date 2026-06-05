import type { Accessor, JSX } from "solid-js";
import { createContext, createMemo, createSignal, useContext } from "solid-js";
import {
	type ReviewDiffView,
	resolveDiffSettings,
	resolveRetrySettings,
	resolveSpeechSettings,
	type Settings,
} from "../../settings";
import type { SpeechVoiceDiscovery } from "../notifications/voices";
import {
	type BooleanSettingsRowData,
	type EditableField,
	type InputSettingsRowData,
	REVIEW_DIFF_VIEW_OPTIONS,
	type SelectSettingsRowData,
	type SettingsRowData,
	type SettingsSelectOption,
	type SettingsTabId,
	TABS,
} from "./SettingsTypes";

export type SettingsContextValue = {
	activeTab: Accessor<SettingsTabId>;
	focusedRowIndex: Accessor<number>;
	editingField: Accessor<EditableField>;
	error: Accessor<string | null>;
	rows: Accessor<SettingsRowData[]>;
	isRowFocused: (index: number) => boolean;
	isEditing: (field: Exclude<EditableField, null>) => boolean;
	inputDraft: (field: InputSettingsRowData["id"]) => string;
	setInputDraft: (field: InputSettingsRowData["id"], value: string) => void;
	selectOptions: (field: SelectSettingsRowData["id"]) => SettingsSelectOption[];
	selectSelectedIndex: (field: SelectSettingsRowData["id"]) => number;
	selectHeight: (field: SelectSettingsRowData["id"]) => number;
	selectMinWidth: (field: SelectSettingsRowData["id"]) => number;
	showSelectDescription: (field: SelectSettingsRowData["id"]) => boolean;
	setSelectDraft: (
		field: SelectSettingsRowData["id"],
		index: number,
		value: unknown,
	) => void;
	commitSelect: (
		field: SelectSettingsRowData["id"],
		index: number,
		value: unknown,
	) => void;
	actions: {
		toggleBoolean: (field: BooleanSettingsRowData["id"]) => Promise<void>;
		commitEdit: (field?: EditableField) => Promise<boolean>;
		cancelEdit: () => void;
		focusRow: (index: number) => void;
		activateRow: (index?: number) => Promise<void>;
		runAfterPendingEdit: (action: () => void | Promise<void>) => Promise<void>;
		switchTab: (nextTab: SettingsTabId) => Promise<void>;
	};
};

type SettingsProviderProps = {
	initialSettings: Settings;
	speechVoices: SpeechVoiceDiscovery;
	onSave: (settings: Settings) => Promise<void>;
	children: JSX.Element;
};

const SettingsContext = createContext<SettingsContextValue>();

export function useSettingsContext(): SettingsContextValue {
	const ctx = useContext(SettingsContext);
	if (!ctx)
		throw new Error("Settings components must be used inside SettingsProvider");
	return ctx;
}

function cloneSettings(settings: Settings): Settings {
	return {
		...settings,
		speech:
			typeof settings.speech === "object" && settings.speech !== null
				? { ...settings.speech }
				: settings.speech,
		diffs:
			typeof settings.diffs === "object" && settings.diffs !== null
				? { ...settings.diffs }
				: settings.diffs,
		retry:
			typeof settings.retry === "object" && settings.retry !== null
				? { ...settings.retry }
				: settings.retry,
	};
}

export function SettingsProvider(props: SettingsProviderProps) {
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
	const [reviewDiffViewDraft, setReviewDiffViewDraft] =
		createSignal<ReviewDiffView>(
			resolveDiffSettings(props.initialSettings.diffs).view,
		);
	const [reviewDiffViewSelectedIndex, setReviewDiffViewSelectedIndex] =
		createSignal(0);
	const [voiceDraft, setVoiceDraft] = createSignal(
		resolveSpeechSettings(props.initialSettings.speech).voice ?? "",
	);
	const [voiceSelectedIndex, setVoiceSelectedIndex] = createSignal(0);

	const voiceOptions = createMemo<SettingsSelectOption[]>(() => [
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

	function resolveReviewDiffViewIndex(value: ReviewDiffView): number {
		const index = REVIEW_DIFF_VIEW_OPTIONS.findIndex(
			(option) => option.value === value,
		);
		return index >= 0 ? index : 0;
	}

	function resolveVoiceIndex(value: string): number {
		const index = voiceOptions().findIndex((option) => option.value === value);
		return index >= 0 ? index : 0;
	}

	const rows = createMemo<SettingsRowData[]>(() => {
		const currentSettings = settings();
		const speech = resolveSpeechSettings(currentSettings.speech);
		const diffs = resolveDiffSettings(currentSettings.diffs);
		const retry = resolveRetrySettings(currentSettings.retry);

		if (activeTab() === "general") {
			return [
				{
					id: "diffs.view",
					kind: "select",
					label: "Code Review Diff View",
					help: "Default view for /code-review.",
					value: diffs.view,
				},
				{
					id: "guidedQuestions",
					kind: "boolean",
					label: "Guided Questions",
					help: "The agent uses a form when it needs several answers.",
					checked: currentSettings.guidedQuestions !== false,
				},
				{
					id: "sessionNaming",
					kind: "boolean",
					label: "Auto-name Sessions",
					help: "Generated after the first couple of turns.",
					checked: currentSettings.sessionNaming !== false,
				},
				{
					id: "pager",
					kind: "boolean",
					label: "Auto-open Pager",
					help: "A paged modal UX for long agent responses",
					checked: currentSettings.pager !== false,
				},
				{
					id: "retry.enabled",
					kind: "boolean",
					label: "Auto-retry Errors",
					help: "Retry transient provider and server failures.",
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
					help: "Base delay in milliseconds.",
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
				: "Which macOS voice to use for speech notifications.";

		return [
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

	function syncDrafts(nextSettings: Settings) {
		const speech = resolveSpeechSettings(nextSettings.speech);
		const diffs = resolveDiffSettings(nextSettings.diffs);
		const retry = resolveRetrySettings(nextSettings.retry);
		setReviewDiffViewDraft(diffs.view);
		setReviewDiffViewSelectedIndex(resolveReviewDiffViewIndex(diffs.view));
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

	async function toggleBoolean(rowId: BooleanSettingsRowData["id"]) {
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

	function inputDraft(field: InputSettingsRowData["id"]): string {
		switch (field) {
			case "speech.maxChars":
				return maxCharsDraft();
			case "retry.maxRetries":
				return retryMaxRetriesDraft();
			case "retry.baseDelayMs":
				return retryBaseDelayDraft();
			case "retry.maxDelayMs":
				return retryMaxDelayDraft();
		}
	}

	function setInputDraft(field: InputSettingsRowData["id"], value: string) {
		if (!/^\d*$/.test(value)) return;
		switch (field) {
			case "speech.maxChars":
				setMaxCharsDraft(value);
				return;
			case "retry.maxRetries":
				setRetryMaxRetriesDraft(value);
				return;
			case "retry.baseDelayMs":
				setRetryBaseDelayDraft(value);
				return;
			case "retry.maxDelayMs":
				setRetryMaxDelayDraft(value);
				return;
		}
	}

	function selectOptions(
		field: SelectSettingsRowData["id"],
	): SettingsSelectOption[] {
		switch (field) {
			case "diffs.view":
				return REVIEW_DIFF_VIEW_OPTIONS;
			case "speech.voice":
				return voiceOptions();
		}
	}

	function selectSelectedIndex(field: SelectSettingsRowData["id"]): number {
		switch (field) {
			case "diffs.view":
				return reviewDiffViewSelectedIndex();
			case "speech.voice":
				return voiceSelectedIndex();
		}
	}

	function setSelectDraft(
		field: SelectSettingsRowData["id"],
		index: number,
		value: unknown,
	) {
		switch (field) {
			case "diffs.view":
				setReviewDiffViewSelectedIndex(index);
				setReviewDiffViewDraft(value === "split" ? "split" : "unified");
				return;
			case "speech.voice":
				setVoiceSelectedIndex(index);
				setVoiceDraft(String(value ?? ""));
				return;
		}
	}

	function commitSelect(
		field: SelectSettingsRowData["id"],
		index: number,
		value: unknown,
	) {
		setSelectDraft(field, index, value);
		void commitEdit(field);
	}

	function selectHeight(field: SelectSettingsRowData["id"]): number {
		const options = selectOptions(field);
		return Math.min(6, Math.max(2, options.length));
	}

	function selectMinWidth(field: SelectSettingsRowData["id"]): number {
		return field === "diffs.view" ? 16 : 28;
	}

	function showSelectDescription(field: SelectSettingsRowData["id"]): boolean {
		return field !== "diffs.view";
	}

	async function commitEdit(field = editingField()): Promise<boolean> {
		if (!field) return true;
		if (field === "diffs.view") {
			const ok = await persist({
				...cloneSettings(settings()),
				diffs: { view: reviewDiffViewDraft() },
			});
			if (ok) setEditingField(null);
			return ok;
		}
		if (
			field === "speech.maxChars" ||
			field === "retry.maxRetries" ||
			field === "retry.baseDelayMs" ||
			field === "retry.maxDelayMs"
		) {
			const draft = inputDraft(field).trim();
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
			if (row.id === "diffs.view") {
				setReviewDiffViewSelectedIndex(
					resolveReviewDiffViewIndex(reviewDiffViewDraft()),
				);
			} else {
				setVoiceSelectedIndex(resolveVoiceIndex(voiceDraft()));
			}
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

	const value: SettingsContextValue = {
		activeTab,
		focusedRowIndex,
		editingField,
		error,
		rows,
		isRowFocused: (index) => index === focusedRowIndex(),
		isEditing: (field) => editingField() === field,
		inputDraft,
		setInputDraft,
		selectOptions,
		selectSelectedIndex,
		selectHeight,
		selectMinWidth,
		showSelectDescription,
		setSelectDraft,
		commitSelect,
		actions: {
			toggleBoolean,
			commitEdit,
			cancelEdit,
			focusRow,
			activateRow,
			runAfterPendingEdit,
			switchTab,
		},
	};

	return (
		<SettingsContext.Provider value={value}>
			{props.children}
		</SettingsContext.Provider>
	);
}

export function nextSettingsTab(current: SettingsTabId, direction: -1 | 1) {
	const currentIndex = TABS.findIndex((tab) => tab.id === current);
	const nextIndex =
		direction < 0
			? currentIndex <= 0
				? TABS.length - 1
				: currentIndex - 1
			: currentIndex >= TABS.length - 1
				? 0
				: currentIndex + 1;
	return TABS[nextIndex]?.id ?? current;
}
