import {
	getOAuthProviders,
	type OAuthAuthInfo,
	type OAuthDeviceCodeInfo,
	type OAuthPrompt,
	type OAuthProviderInterface,
	type OAuthSelectPrompt,
} from "@earendil-works/pi-ai/oauth";
import { builtinProviders } from "@earendil-works/pi-ai/providers/all";
import { useBindings } from "@opentui/keymap/solid";
import { useRenderer } from "@opentui/solid";
import { createSignal, For, onCleanup, Show } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { readAuthFile, readAuthFileSync, writeAuthFile } from "../../auth";
import { withKitKeyAliases } from "../../keymap/bindings";
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

type LoginStep = "select" | "prompt" | "waiting" | "apiKey";

type PromptState = {
	label: string;
	placeholder?: string;
	allowEmpty: boolean;
};

export type LoginModalProps = {
	onClose: (result: LoginOutcome) => void;
	surfaceProps?: OverlaySurfaceProps;
};

/**
 * A row in the provider list — either an OAuth flow or an API-key
 * paste. Both persist credentials to ~/.kit/auth.json; getApiKey()
 * already prefers auth.json entries of either type over env vars.
 */
type ProviderOption =
	| {
			kind: "oauth";
			name: string;
			providerId: string;
			oauth: OAuthProviderInterface;
	  }
	| { kind: "api-key"; name: string; providerId: string };

/**
 * OAuth providers first (preferred flows), then API-key providers A–Z.
 * Both the list and display names come from pi-ai's provider registry;
 * a provider offers an API-key row only when it declares apiKey auth
 * and has selectable models.
 */
function buildProviderOptions(): ProviderOption[] {
	const oauth: ProviderOption[] = getOAuthProviders().map((provider) => ({
		kind: "oauth",
		name: provider.name,
		providerId: provider.id,
		oauth: provider,
	}));
	const apiKey: ProviderOption[] = builtinProviders()
		.filter(
			(provider) => provider.auth.apiKey && provider.getModels().length > 0,
		)
		.map((provider) => ({
			kind: "api-key" as const,
			name: provider.name,
			providerId: provider.id,
		}))
		.sort((a, b) => a.name.localeCompare(b.name));
	return [...oauth, ...apiKey];
}

function splitInstructions(instructions?: string): string[] {
	return (instructions ?? "")
		.split(/\r?\n/)
		.map((line) => line.trim())
		.filter((line) => line.length > 0);
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
	const [apiKeyTarget, setApiKeyTarget] = createSignal<{
		providerId: string;
		name: string;
	} | null>(null);
	const [promptState, setPromptState] = createSignal<PromptState | null>(null);
	const [inputValue, setInputValue] = createSignal("");
	const [authUrl, setAuthUrl] = createSignal<string | null>(null);
	const [authInstructions, setAuthInstructions] = createSignal<string[]>([]);
	const [progressLines, setProgressLines] = createSignal<string[]>([]);
	const [errorLines, setErrorLines] = createSignal<string[]>([]);

	let pendingPromptResolve: ((value: string) => void) | null = null;
	let closed = false;
	const abortController = new AbortController();

	function finish(result: LoginOutcome) {
		if (closed) return;
		closed = true;
		props.onClose(result);
	}

	function resolvePendingPrompt(value: string) {
		if (!pendingPromptResolve) return;
		const resolve = pendingPromptResolve;
		pendingPromptResolve = null;
		resolve(value);
	}

	function cancel() {
		abortController.abort();
		resolvePendingPrompt("");
		finish({ didAuthenticate: false });
	}

	onCleanup(() => {
		closed = true;
		abortController.abort();
		resolvePendingPrompt("");
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

	function requestPrompt(prompt: OAuthPrompt): Promise<string> {
		if (closed) return Promise.resolve("");
		setPromptState({
			label: prompt.message,
			placeholder: prompt.placeholder,
			allowEmpty: prompt.allowEmpty ?? false,
		});
		setInputValue("");
		setStep("prompt");
		return new Promise<string>((resolve) => {
			pendingPromptResolve = resolve;
		});
	}

	async function startProviderLogin(provider: OAuthProviderInterface) {
		clearTransientState(provider.name);
		setStep("waiting");
		appendProgress("Starting login…");

		try {
			const credentials = await provider.login({
				onAuth(info: OAuthAuthInfo) {
					if (closed) return;
					setAuthUrl(info.url);
					setAuthInstructions(splitInstructions(info.instructions));
					setStep("waiting");
					appendProgress("Complete authentication in your browser.");
					void openExternal(info.url)
						.then(() => {
							if (!closed) appendProgress("Browser opened.");
						})
						.catch((error) => {
							if (closed) return;
							setErrorLines([
								`Failed to open browser automatically: ${formatError(error)}`,
							]);
						});
				},
				onProgress(message: string) {
					if (closed) return;
					setStep("waiting");
					appendProgress(message);
				},
				onPrompt: requestPrompt,
				onDeviceCode(info: OAuthDeviceCodeInfo) {
					if (closed) return;
					appendProgress(
						`Enter code ${info.userCode} at ${info.verificationUri}`,
					);
					void openExternal(info.verificationUri).catch(() => {});
				},
				async onSelect(prompt: OAuthSelectPrompt) {
					if (closed) return undefined;
					const result = await requestPrompt({
						message: `${prompt.message}\n${prompt.options.map((o) => `  ${o.id}: ${o.label}`).join("\n")}`,
					});
					const match = prompt.options.find(
						(o) => o.id === result || o.label === result,
					);
					return match?.id;
				},
				onManualCodeInput: () =>
					requestPrompt({
						message:
							"Paste the redirect URL (or leave blank to wait for browser callback)",
						allowEmpty: true,
					}),
				signal: abortController.signal,
			});

			if (closed) return;

			const auth = await readAuthFile();
			auth[provider.id] = { ...credentials, type: "oauth" };
			await writeAuthFile(auth);
			finish({ didAuthenticate: true, providerName: provider.name });
		} catch (error) {
			if (closed || abortController.signal.aborted) return;
			clearTransientState();
			setErrorLines([formatError(error)]);
			setStep("select");
		}
	}

	function startApiKeyEntry(option: { providerId: string; name: string }) {
		clearTransientState(option.name);
		setApiKeyTarget(option);
		setStep("apiKey");
	}

	function backToSelect() {
		clearTransientState();
		setApiKeyTarget(null);
		setStep("select");
	}

	async function saveApiKey() {
		const target = apiKeyTarget();
		const key = inputValue().trim();
		if (!target || !key) return;
		try {
			const auth = await readAuthFile();
			auth[target.providerId] = { type: "api_key", key };
			await writeAuthFile(auth);
			finish({ didAuthenticate: true, providerName: target.name });
		} catch (error) {
			if (closed) return;
			setErrorLines([formatError(error)]);
		}
	}

	function submitInput() {
		if (step() === "apiKey") {
			void saveApiKey();
			return;
		}

		const prompt = promptState();
		if (!prompt) return;
		if (!prompt.allowEmpty && inputValue().trim().length === 0) return;
		const value = inputValue();
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

	// Escape backs out one level: API-key entry returns to the provider
	// list; the OAuth waiting/prompt steps cancel the whole flow.
	useBindings(() =>
		withKitKeyAliases({
			enabled: () => step() === "apiKey",
			priority: 200,
			commands: [
				{
					name: "login.back",
					desc: "Back to provider list",
					group: "login",
					hint: "back",
					run: backToSelect,
				},
			],
			bindings: [
				{
					key: "escape",
					cmd: "login.back",
					desc: "Back to provider list",
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
			enabled: () => step() === "prompt" || step() === "apiKey",
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
		if (step() === "apiKey") return "Set API key";
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
		description: option.kind === "oauth" ? "oauth" : "api key",
		nameColor:
			option.kind === "oauth" ? theme.textPrimary : theme.textSecondary,
		action: () => {
			if (option.kind === "oauth") {
				void startProviderLogin(option.oauth);
			} else {
				startApiKeyEntry(option);
			}
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

			<Show when={step() === "prompt" || step() === "apiKey"}>
				<box flexDirection="column" gap={1}>
					<text fg={theme.textPrimary}>
						{step() === "apiKey"
							? `Paste your ${apiKeyTarget()?.name ?? "provider"} API key`
							: (promptState()?.label ?? "")}
					</text>
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
							placeholder={
								step() === "apiKey"
									? "paste key"
									: (promptState()?.placeholder ?? "")
							}
							placeholderColor={theme.textPlaceholder}
							backgroundColor={theme.bgTransparent}
							focusedBackgroundColor={theme.bgTransparent}
							textColor={theme.textPrimary}
							focusedTextColor={theme.textPrimary}
							cursorColor={theme.cursor}
							onInput={(value: string) => setInputValue(value)}
						/>
					</box>
					<Show when={step() === "apiKey"}>
						<text fg={theme.textMuted}>Saved to ~/.kit/auth.json</text>
					</Show>
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
