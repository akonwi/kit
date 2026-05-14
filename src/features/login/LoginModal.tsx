import {
	getOAuthProviders,
	type OAuthAuthInfo,
	type OAuthPrompt,
	type OAuthProviderInterface,
} from "@earendil-works/pi-ai/oauth";
import type { KeyEvent } from "@opentui/core";
import { useKeyboard, useRenderer } from "@opentui/solid";
import { createSignal, For, onCleanup, Show } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { readAuthFile, writeAuthFile } from "../../auth";
import { Dialog } from "../../shell/Dialog";
import { type Binding, HintBar } from "../../shell/HintBar";
import { openExternal } from "../../shell/open-external";
import { copySelection } from "../../shell/selection";
import { theme } from "../../shell/theme";

const SELECT_BINDINGS: Binding[] = [
	{ key: "↑/↓", action: "move" },
	{ key: "Enter", action: "select" },
	{ key: "Esc", action: "cancel" },
];

const PROMPT_BINDINGS: Binding[] = [
	{ key: "Enter", action: "submit" },
	{ key: "Esc", action: "cancel" },
];

const WAITING_BINDINGS: Binding[] = [{ key: "Esc", action: "cancel" }];

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
	const providers = getOAuthProviders();
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [step, setStep] = createSignal<LoginStep>(
		providers.length > 0 ? "select" : "apiKey",
	);
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
			setStep(providers.length > 0 ? "select" : "apiKey");
		}
	}

	function submitInput() {
		if (step() === "apiKey") {
			const trimmed = inputValue().trim();
			if (!trimmed) return;
			process.env.ANTHROPIC_API_KEY = trimmed;
			finish({ didAuthenticate: true, providerName: "Anthropic" });
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

	useKeyboard((event: KeyEvent) => {
		if (event.ctrl && event.name === "c") {
			event.preventDefault();
			cancel();
			return;
		}

		if (event.name === "escape") {
			event.preventDefault();
			cancel();
			return;
		}

		if (step() === "select") {
			if (event.name === "up") {
				event.preventDefault();
				setSelectedIndex((index) => Math.max(0, index - 1));
				return;
			}
			if (event.name === "down") {
				event.preventDefault();
				setSelectedIndex((index) => Math.min(providers.length - 1, index + 1));
				return;
			}
			if (event.name === "return" || event.name === "enter") {
				event.preventDefault();
				const provider = providers[selectedIndex()];
				if (provider) void startProviderLogin(provider);
			}
			return;
		}

		if (
			(step() === "prompt" || step() === "apiKey") &&
			(event.name === "return" || event.name === "enter")
		) {
			event.preventDefault();
			submitInput();
		}
	});

	const title = () => {
		if (step() === "apiKey") return "Set API key";
		if (step() === "select") return "Log in to a provider";
		return "Complete login";
	};

	const subtitle = () => {
		if (step() === "apiKey") return "No OAuth providers are available.";
		if (providerName()) return providerName();
		return `${providers.length} providers available`;
	};

	const bindings = () => {
		if (step() === "select") return SELECT_BINDINGS;
		if (step() === "prompt" || step() === "apiKey") return PROMPT_BINDINGS;
		return WAITING_BINDINGS;
	};

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
				<box flexDirection="column" gap={0}>
					<For each={providers}>
						{(provider, index) => {
							const focused = () => index() === selectedIndex();
							return (
								<box
									paddingX={1}
									backgroundColor={
										focused() ? theme.bgMuted : theme.bgTransparent
									}
								>
									<text
										fg={focused() ? theme.textPrimary : theme.textSecondary}
									>
										{provider.name}
									</text>
								</box>
							);
						}}
					</For>
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
							? "Enter API key (e.g. ANTHROPIC_API_KEY)"
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
									? "sk-..."
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
				</box>
			</Show>

			<Dialog.Footer>
				<HintBar borderless bindings={bindings()} />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
