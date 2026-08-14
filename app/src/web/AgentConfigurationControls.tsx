/** @jsxImportSource solid-js */
import {
	type Accessor,
	createContext,
	createEffect,
	createMemo,
	createSignal,
	type JSX,
	Show,
	useContext,
} from "solid-js";
import { Portal } from "solid-js/web";
import { MIDDLE_DOT } from "../shell/glyphs";
import { isRecord } from "./client-state";
import { PickerDialog, type PickerDialogOption } from "./PickerDialog";
import type { RemoteModel } from "./remote-services";
import { useWebClient } from "./WebClientContext";

type ActivePicker = "model" | "thinking" | null;

type AgentConfigurationContextValue = {
	modelLabel: Accessor<string>;
	thinkingLevel: Accessor<string>;
	disabled: Accessor<boolean>;
	openModelPicker: () => Promise<void>;
	openThinkingPicker: () => Promise<void>;
};

const AgentConfigurationContext =
	createContext<AgentConfigurationContextValue>();

export function useAgentConfiguration(): AgentConfigurationContextValue {
	const value = useContext(AgentConfigurationContext);
	if (!value) {
		throw new Error(
			"useAgentConfiguration must be used within AgentConfigurationProvider",
		);
	}
	return value;
}

export function AgentConfigurationProvider(props: {
	children: JSX.Element;
}): JSX.Element {
	const { snapshot, controller } = useWebClient();
	let requestGeneration = 0;
	let observedStreamId: string | null = null;
	let observedSessionId: string | null = null;
	let pickerSessionId: string | null = null;
	let pickerModelId: string | undefined;
	const [activePicker, setActivePicker] = createSignal<ActivePicker>(null);
	const [models, setModels] = createSignal<RemoteModel[]>([]);
	const [thinkingLevels, setThinkingLevels] = createSignal<string[]>([]);
	const [loading, setLoading] = createSignal(false);
	const [mutating, setMutating] = createSignal(false);
	const [error, setError] = createSignal("");
	const protocol = createMemo(() => snapshot().protocol);
	const currentModel = createMemo<RemoteModel | null>(() => {
		const value = protocol().serverState.model;
		if (
			!isRecord(value) ||
			typeof value.id !== "string" ||
			typeof value.provider !== "string"
		) {
			return null;
		}
		return {
			id: value.id,
			provider: value.provider,
			...(typeof value.name === "string" ? { name: value.name } : {}),
		};
	});
	const modelLabel = createMemo(() => {
		const model = currentModel();
		return model ? (model.name ?? model.id) : "";
	});
	const thinkingLevel = createMemo(() => {
		const value = protocol().serverState.thinkingLevel;
		return typeof value === "string" ? value : "";
	});
	const sessionId = createMemo(() => {
		const value = protocol().serverState.sessionId;
		return typeof value === "string" ? value : null;
	});
	const disabled = createMemo(
		() =>
			protocol().phase !== "live" ||
			protocol().serverState.isStreaming === true ||
			snapshot().submitting ||
			mutating(),
	);
	const currentModelId = createMemo(() => {
		const model = currentModel();
		return model ? `${model.provider}\u0000${model.id}` : undefined;
	});
	const modelOptions = createMemo<PickerDialogOption<RemoteModel>[]>(() =>
		models().map((model) => ({
			id: `${model.provider}\u0000${model.id}`,
			name: model.name ?? model.id,
			description: model.provider,
			value: model,
		})),
	);
	const thinkingOptions = createMemo<PickerDialogOption<string>[]>(() =>
		thinkingLevels().map((level) => ({
			id: level,
			name: level,
			value: level,
		})),
	);

	function closePicker(): void {
		requestGeneration += 1;
		setActivePicker(null);
		setLoading(false);
		setError("");
		pickerSessionId = null;
		pickerModelId = undefined;
	}

	function canOpenPicker(): boolean {
		if (disabled()) return false;
		return !document.querySelector<HTMLDialogElement>("dialog:modal");
	}

	async function openModelPicker(): Promise<void> {
		if (!canOpenPicker()) return;
		const generation = ++requestGeneration;
		const streamId = protocol().streamId;
		pickerSessionId = sessionId();
		pickerModelId = currentModelId();
		setModels([]);
		setError("");
		setLoading(true);
		setActivePicker("model");
		try {
			const next = await controller.listModels();
			if (
				generation !== requestGeneration ||
				activePicker() !== "model" ||
				protocol().phase !== "live" ||
				protocol().streamId !== streamId ||
				sessionId() !== pickerSessionId
			) {
				return;
			}
			setModels(next);
		} catch (cause) {
			if (generation !== requestGeneration || activePicker() !== "model")
				return;
			setError(cause instanceof Error ? cause.message : String(cause));
		} finally {
			if (generation === requestGeneration) setLoading(false);
		}
	}

	async function openThinkingPicker(): Promise<void> {
		if (!canOpenPicker()) return;
		const generation = ++requestGeneration;
		const streamId = protocol().streamId;
		pickerSessionId = sessionId();
		pickerModelId = currentModelId();
		setThinkingLevels([]);
		setError("");
		setLoading(true);
		setActivePicker("thinking");
		try {
			const next = await controller.listThinkingLevels();
			if (
				generation !== requestGeneration ||
				activePicker() !== "thinking" ||
				protocol().phase !== "live" ||
				protocol().streamId !== streamId ||
				sessionId() !== pickerSessionId ||
				currentModelId() !== pickerModelId
			) {
				return;
			}
			setThinkingLevels(next);
		} catch (cause) {
			if (generation !== requestGeneration || activePicker() !== "thinking") {
				return;
			}
			setError(cause instanceof Error ? cause.message : String(cause));
		} finally {
			if (generation === requestGeneration) setLoading(false);
		}
	}

	async function selectModel(model: RemoteModel): Promise<void> {
		const expectedSessionId = pickerSessionId;
		if (
			activePicker() !== "model" ||
			disabled() ||
			expectedSessionId !== sessionId()
		) {
			return;
		}
		closePicker();
		if (`${model.provider}\u0000${model.id}` === currentModelId()) return;
		setMutating(true);
		try {
			await controller.setModel(model);
		} finally {
			setMutating(false);
		}
	}

	async function selectThinkingLevel(level: string): Promise<void> {
		const expectedSessionId = pickerSessionId;
		const expectedModelId = pickerModelId;
		if (
			activePicker() !== "thinking" ||
			disabled() ||
			expectedSessionId !== sessionId() ||
			expectedModelId !== currentModelId()
		) {
			return;
		}
		closePicker();
		if (level === thinkingLevel()) return;
		setMutating(true);
		try {
			await controller.setThinkingLevel(level);
		} finally {
			setMutating(false);
		}
	}

	createEffect(() => {
		const state = protocol();
		const nextSessionId = sessionId();
		const nextModelId = currentModelId();
		if (
			activePicker() &&
			(state.phase !== "live" ||
				state.serverState.isStreaming === true ||
				(observedStreamId !== null && state.streamId !== observedStreamId) ||
				(observedSessionId !== null && nextSessionId !== observedSessionId) ||
				(activePicker() === "thinking" && pickerModelId !== nextModelId))
		) {
			closePicker();
		}
		observedStreamId = state.streamId;
		observedSessionId = nextSessionId;
	});

	const value: AgentConfigurationContextValue = {
		modelLabel,
		thinkingLevel,
		disabled,
		openModelPicker,
		openThinkingPicker,
	};

	return (
		<AgentConfigurationContext.Provider value={value}>
			{props.children}
			<Portal>
				<PickerDialog
					open={activePicker() === "model"}
					id="model-picker"
					title="Model"
					options={modelOptions()}
					currentId={currentModelId()}
					filter="fuzzy"
					layout="detail"
					loading={loading()}
					error={error()}
					onCancel={closePicker}
					onSelect={(model) => void selectModel(model)}
				/>
				<PickerDialog
					open={activePicker() === "thinking"}
					id="thinking-picker"
					title="Thinking level"
					options={thinkingOptions()}
					currentId={thinkingLevel()}
					filter="none"
					layout="single"
					loading={loading()}
					error={error()}
					onCancel={closePicker}
					onSelect={(level) => void selectThinkingLevel(level)}
				/>
			</Portal>
		</AgentConfigurationContext.Provider>
	);
}

export function AgentConfigurationControls(props: {
	showModel?: boolean;
	showThinking?: boolean;
}): JSX.Element {
	const {
		modelLabel,
		thinkingLevel,
		disabled,
		openModelPicker,
		openThinkingPicker,
	} = useAgentConfiguration();

	return (
		<div class="header-meta" role="group" aria-label="Agent configuration">
			<Show when={props.showModel !== false && modelLabel()}>
				{(label) => (
					<button
						class="header-meta-action"
						type="button"
						data-variant="ghost"
						aria-disabled={disabled()}
						aria-label={`Choose model, current ${label()}`}
						title={`Model: ${label()}`}
						onClick={() => void openModelPicker()}
					>
						{label()}
					</button>
				)}
			</Show>
			<Show
				when={
					props.showModel !== false &&
					props.showThinking !== false &&
					modelLabel() &&
					thinkingLevel()
				}
			>
				<span class="chrome-separator" aria-hidden="true">
					{MIDDLE_DOT}
				</span>
			</Show>
			<Show when={props.showThinking !== false && thinkingLevel()}>
				{(level) => (
					<button
						class="header-meta-action"
						type="button"
						data-variant="ghost"
						aria-disabled={disabled()}
						aria-label={`Choose thinking level, current ${level()}`}
						title={`Thinking level: ${level()}`}
						onClick={() => void openThinkingPicker()}
					>
						{level()}
					</button>
				)}
			</Show>
		</div>
	);
}
