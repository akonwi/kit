import type {
	AuthEvent,
	AuthInteraction,
	AuthPrompt,
	AuthType,
	Provider,
} from "@earendil-works/pi-ai";
import { useBindings } from "@opentui/keymap/solid";
import { useRenderer } from "@opentui/solid";
import { createSignal, For, onCleanup, Show } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { readAuthFileSync } from "../../auth";
import { withKitKeyAliases } from "../../keymap/bindings";
import { kitModels } from "../../runtime/models";
import { Dialog } from "../../shell/Dialog";
import { CHECK } from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { openExternal } from "../../shell/open-external";
import { Picker } from "../../shell/Picker";
import { copySelection } from "../../shell/selection";
import { theme } from "../../shell/theme";
import type { PickerOption } from "../../state/picker";
import { createPickerManager } from "../../state/picker-manager";

export type LoginOutcome = {
	didAuthenticate: boolean;
	providerName?: string;
};

type LoginStep = "select" | "prompt" | "waiting";

type PromptState = {
	label: string;
	placeholder?: string;
	allowEmpty: boolean;
	options?: readonly { id: string; label: string }[];
};

export type LoginModalProps = {
	onClose: (result: LoginOutcome) => void;
	surfaceProps?: OverlaySurfaceProps;
};

type ProviderOption = {
	name: string;
	method: string;
	providerId: string;
	authType: AuthType;
};

function providerOptions(provider: Provider): ProviderOption[] {
	const options: ProviderOption[] = [];
	if (provider.auth.oauth) {
		options.push({
			name: provider.name,
			method:
				provider.auth.oauth.loginLabel ?? provider.auth.oauth.name ?? "oauth",
			providerId: provider.id,
			authType: "oauth",
		});
	}
	if (provider.auth.apiKey?.login) {
		options.push({
			name: provider.name,
			method: provider.auth.apiKey.name,
			providerId: provider.id,
			authType: "api_key",
		});
	}
	return options;
}

export function buildProviderOptions(): ProviderOption[] {
	return kitModels
		.getProviders()
		.flatMap(providerOptions)
		.sort((a, b) => {
			if (a.authType !== b.authType) return a.authType === "oauth" ? -1 : 1;
			return a.name.localeCompare(b.name);
		});
}

function splitInstructions(instructions?: string): string[] {
	return (instructions ?? "")
		.split(/\r?\n/)
		.map((line) => line.trim())
		.filter((line) => line.length > 0);
}

export function authPromptAllowsEmpty(prompt: AuthPrompt): boolean {
	return prompt.type === "text" || prompt.type === "manual_code";
}

function formatError(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}

export function LoginModal(props: LoginModalProps) {
	const renderer = useRenderer();
	const providerOptions = buildProviderOptions();
	const authenticatedIds = new Set(Object.keys(readAuthFileSync()));
	const [step, setStep] = createSignal<LoginStep>("select");
	const [providerName, setProviderName] = createSignal<string | undefined>(
		undefined,
	);
	const [promptState, setPromptState] = createSignal<PromptState | null>(null);
	const [inputValue, setInputValue] = createSignal("");
	const [authUrl, setAuthUrl] = createSignal<string | null>(null);
	const [authInstructions, setAuthInstructions] = createSignal<string[]>([]);
	const [progressLines, setProgressLines] = createSignal<string[]>([]);
	const [errorLines, setErrorLines] = createSignal<string[]>([]);

	let pendingPromptResolve: ((value: string) => void) | null = null;
	let pendingPromptReject: ((error: Error) => void) | null = null;
	let pendingPromptCleanup: (() => void) | null = null;
	let closed = false;
	const abortController = new AbortController();

	function finish(result: LoginOutcome) {
		if (closed) return;
		closed = true;
		props.onClose(result);
	}

	function clearPendingPrompt() {
		pendingPromptCleanup?.();
		pendingPromptCleanup = null;
		pendingPromptResolve = null;
		pendingPromptReject = null;
	}

	function resolvePendingPrompt(value: string) {
		if (!pendingPromptResolve) return;
		const resolve = pendingPromptResolve;
		clearPendingPrompt();
		resolve(value);
	}

	function rejectPendingPrompt(error: Error) {
		if (!pendingPromptReject) return;
		const reject = pendingPromptReject;
		clearPendingPrompt();
		reject(error);
	}

	function cancel() {
		abortController.abort();
		rejectPendingPrompt(new Error("Login cancelled"));
		finish({ didAuthenticate: false });
	}

	onCleanup(() => {
		closed = true;
		abortController.abort();
		rejectPendingPrompt(new Error("Login cancelled"));
	});

	function clearTransientState(nextProviderName?: string) {
		setProviderName(nextProviderName);
		setAuthUrl(null);
		setAuthInstructions([]);
		setProgressLines([]);
		setErrorLines([]);
		setPromptState(null);
		setInputValue("");
	}

	function appendProgress(message: string) {
		setProgressLines((prev) => [...prev, message]);
	}

	function requestAuthPrompt(prompt: AuthPrompt): Promise<string> {
		if (closed) return Promise.reject(new Error("Login cancelled"));
		const label =
			prompt.type === "select"
				? `${prompt.message}\n${prompt.options
						.map((option) => `  ${option.id}: ${option.label}`)
						.join("\n")}`
				: prompt.message;
		setPromptState({
			label,
			placeholder: "placeholder" in prompt ? prompt.placeholder : undefined,
			allowEmpty: authPromptAllowsEmpty(prompt),
			options: prompt.type === "select" ? prompt.options : undefined,
		});
		setInputValue("");
		setStep("prompt");
		return new Promise<string>((resolve, reject) => {
			const onAbort = () => rejectPendingPrompt(new Error("Login cancelled"));
			if (prompt.signal?.aborted) {
				reject(new Error("Login cancelled"));
				return;
			}
			pendingPromptResolve = resolve;
			pendingPromptReject = reject;
			pendingPromptCleanup = () =>
				prompt.signal?.removeEventListener("abort", onAbort);
			prompt.signal?.addEventListener("abort", onAbort, { once: true });
		});
	}

	function handleAuthEvent(event: AuthEvent) {
		if (closed) return;
		setStep("waiting");
		switch (event.type) {
			case "auth_url":
				setAuthUrl(event.url);
				setAuthInstructions(splitInstructions(event.instructions));
				appendProgress("Complete authentication in your browser.");
				void openExternal(event.url)
					.then(() => {
						if (!closed) appendProgress("Browser opened.");
					})
					.catch((error) => {
						if (!closed) setErrorLines([formatError(error)]);
					});
				break;
			case "device_code":
				setAuthUrl(event.verificationUri);
				setAuthInstructions([`Enter code ${event.userCode}`]);
				appendProgress(`Enter code ${event.userCode} in your browser.`);
				void openExternal(event.verificationUri).catch(() => {});
				break;
			case "progress":
				appendProgress(event.message);
				break;
			case "info":
				appendProgress(event.message);
				setAuthInstructions(
					event.links?.map((link) =>
						link.label ? `${link.label}: ${link.url}` : link.url,
					) ?? [],
				);
				break;
		}
	}

	async function startProviderLogin(option: ProviderOption) {
		clearTransientState(option.name);
		setStep("waiting");
		appendProgress("Starting login…");
		const interaction: AuthInteraction = {
			signal: abortController.signal,
			prompt: requestAuthPrompt,
			notify: handleAuthEvent,
		};

		try {
			await kitModels.login(option.providerId, option.authType, interaction);
			if (!closed) {
				finish({ didAuthenticate: true, providerName: option.name });
			}
		} catch (error) {
			if (closed || abortController.signal.aborted) return;
			clearTransientState();
			setErrorLines([formatError(error)]);
			setStep("select");
		}
	}

	function submitInput() {
		const prompt = promptState();
		if (!prompt) return;
		if (!prompt.allowEmpty && inputValue().trim().length === 0) return;
		const input = inputValue();
		const value =
			prompt.options?.find(
				(option) => option.id === input || option.label === input,
			)?.id ?? input;
		setInputValue("");
		setPromptState(null);
		setStep("waiting");
		resolvePendingPrompt(value);
	}

	// The provider list itself is a Picker (filterable, windowed, with
	// its own escape/enter bindings); the modal only binds keys for the
	// non-select steps plus a global ctrl+c.
	useBindings(() =>
		withKitKeyAliases({
			priority: 200,
			commands: [
				{
					name: "login.cancel",
					desc: "Cancel login",
					group: "login",
					hint: "cancel",
					run: cancel,
				},
			],
			bindings: [
				{
					key: "ctrl+c",
					cmd: "login.cancel",
					desc: "Cancel login",
					group: "login",
				},
			],
		}),
	);

	useBindings(() =>
		withKitKeyAliases({
			enabled: () => step() === "prompt" || step() === "waiting",
			priority: 200,
			bindings: [
				{
					key: "escape",
					cmd: "login.cancel",
					desc: "Cancel login",
					group: "login",
				},
			],
		}),
	);

	useBindings(() =>
		withKitKeyAliases({
			enabled: () => step() === "prompt",
			priority: 200,
			commands: [
				{
					name: "login.submit-input",
					desc: "Submit login input",
					group: "login",
					hint: "submit",
					run: submitInput,
				},
			],
			bindings: [
				{
					key: "return",
					cmd: "login.submit-input",
					desc: "Submit login input",
					group: "login",
				},
			],
		}),
	);

	const title = () => {
		if (step() === "select") return "Log in to a provider";
		return "Complete login";
	};

	const subtitle = () => {
		if (providerName()) return providerName();
		return `${providerOptions.length} login options`;
	};

	// Provider selection is a filterable Picker; escape pops it, which
	// cancels the whole modal via onDismiss.
	const providerPicker = createPickerManager();
	const pickerOptions: PickerOption[] = providerOptions.map((option) => ({
		name: authenticatedIds.has(option.providerId)
			? `${option.name} ${CHECK}`
			: option.name,
		description: option.method,
		nameColor:
			option.authType === "oauth" ? theme.textPrimary : theme.textSecondary,
		action: () => {
			void startProviderLogin(option);
		},
	}));
	providerPicker.show({
		label: "Filter providers",
		options: pickerOptions,
		filterable: true,
		onDismiss: cancel,
	});

	const authDetailsLines = () => {
		const details = [] as string[];
		const url = authUrl();
		if (url) details.push(url);
		for (const line of authInstructions()) details.push(line);
		return details;
	};

	return (
		<Dialog.Root surfaceProps={props.surfaceProps}>
			<Dialog.Header>
				<Dialog.Title>{title()}</Dialog.Title>
				<Dialog.Meta>{subtitle()}</Dialog.Meta>
			</Dialog.Header>

			<Show when={errorLines().length > 0}>
				<box flexDirection="column" gap={0}>
					<For each={errorLines()}>
						{(line) => <text fg={theme.errorText}>{line}</text>}
					</For>
				</box>
			</Show>

			<Show when={step() === "select"}>
				<box height={15} flexDirection="column">
					<Picker.Root
						picker={providerPicker}
						maxVisible={12}
						commandNamespace="login-picker"
					>
						<Picker.Header />
						<Picker.Body />
					</Picker.Root>
				</box>
			</Show>

			<Show when={step() === "waiting"}>
				<box flexDirection="column" gap={1}>
					<text fg={theme.textSecondary}>
						Complete authentication in your browser. If a provider asks for a
						code, use the instructions shown below.
					</text>
					<Show when={authDetailsLines().length > 0}>
						<box
							border
							borderColor={theme.borderAccent}
							paddingX={1}
							flexDirection="column"
							gap={0}
							onMouseUp={() => copySelection(renderer)}
						>
							<For each={authDetailsLines()}>
								{(line, index) => (
									<text
										fg={index() === 0 ? theme.metaText : theme.textPrimary}
										selectable
									>
										{line}
									</text>
								)}
							</For>
						</box>
					</Show>
					<Show when={progressLines().length > 0}>
						<box flexDirection="column" gap={0}>
							<For each={progressLines()}>
								{(line) => <text fg={theme.textMuted}>{line}</text>}
							</For>
						</box>
					</Show>
				</box>
			</Show>

			<Show when={step() === "prompt"}>
				<box flexDirection="column" gap={1}>
					<text fg={theme.textPrimary}>{promptState()?.label ?? ""}</text>
					<box
						border
						borderColor={theme.borderDefault}
						paddingX={1}
						backgroundColor={theme.bgTransparent}
					>
						<input
							focused
							width="100%"
							value={inputValue()}
							placeholder={promptState()?.placeholder ?? ""}
							placeholderColor={theme.textPlaceholder}
							backgroundColor={theme.bgTransparent}
							focusedBackgroundColor={theme.bgTransparent}
							textColor={theme.textPrimary}
							focusedTextColor={theme.textPrimary}
							cursorColor={theme.cursor}
							onInput={(value: string) => setInputValue(value)}
						/>
					</box>
				</box>
			</Show>

			<Dialog.Footer>
				<KeymapHintBar
					borderless
					group={step() === "select" ? "login-picker" : "login"}
				/>
			</Dialog.Footer>
		</Dialog.Root>
	);
}
