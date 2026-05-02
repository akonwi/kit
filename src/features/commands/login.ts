import { exec } from "node:child_process";
import {
	getOAuthProviders,
	type OAuthAuthInfo,
	type OAuthPrompt,
	type OAuthProviderInterface,
} from "@mariozechner/pi-ai/oauth";
import { readAuthFile, writeAuthFile } from "../../auth";
import type { PaletteContext } from "../../state/palette";
import type { PaletteManager } from "../../state/palette-manager";
import type { Command } from "./types";

export type LoginOutcome = {
	didAuthenticate: boolean;
	providerName?: string;
};

export type LoginNotifier = {
	info?: (title: string, lines: string[]) => void;
	error?: (title: string, lines: string[]) => void;
};

export async function runLoginFlow(
	palette: PaletteManager,
	notify: LoginNotifier = {},
): Promise<LoginOutcome> {
	const providers = getOAuthProviders();

	if (providers.length === 0) {
		const saved = await promptApiKey(palette);
		return {
			didAuthenticate: saved,
			providerName: saved ? "Anthropic" : undefined,
		};
	}

	const provider = await selectProvider(palette, providers);
	if (!provider) return { didAuthenticate: false };

	notify.info?.(`Logging in to ${provider.name}…`, []);

	try {
		const credentials = await provider.login({
			onAuth({ url, instructions: _instructions }: OAuthAuthInfo) {
				console.log("[login] onAuth called, url:", url?.slice(0, 60));
				exec(`open "${url}"`);
				notify.info?.("Browser opened", ["Complete login then return here."]);
			},

			onProgress(message: string) {
				notify.info?.(message, []);
			},

			onPrompt: (prompt: OAuthPrompt) =>
				promptForValue(palette, prompt.message),

			onManualCodeInput: () =>
				promptForValue(
					palette,
					"Paste the redirect URL (or leave blank to wait for browser callback)",
				),
		});

		if (palette.isInputMode) palette.pop();

		const auth = await readAuthFile();
		auth[provider.id] = { ...credentials, type: "oauth" };
		await writeAuthFile(auth);

		return { didAuthenticate: true, providerName: provider.name };
	} catch (error) {
		console.log("[login] error:", error);
		notify.error?.("Login failed", [String(error)]);
		return { didAuthenticate: false };
	}
}

export const loginCommand: Command = {
	name: "login",
	description: "Log in to an AI provider",
	async execute({ palette, runtime }) {
		const result = await runLoginFlow(palette, {
			info: (title, lines) => runtime.emitInfo(title, lines),
			error: (title, lines) => runtime.emitError(title, lines),
		});

		if (!result.didAuthenticate) return;

		runtime.emitInfo("Login successful", [
			result.providerName
				? `Logged in to ${result.providerName}.`
				: "Credentials saved.",
		]);
	},
};

async function selectProvider(
	palette: PaletteManager,
	providers: OAuthProviderInterface[],
): Promise<OAuthProviderInterface | null> {
	return new Promise<OAuthProviderInterface | null>((resolve) => {
		let selected: OAuthProviderInterface | null = null;
		palette.show({
			filterable: false,
			hint: "Select a provider",
			onDismiss: () => resolve(selected),
			options: providers.map((provider) => ({
				name: provider.name,
				description: provider.id,
				value: provider,
				action: (ctx: PaletteContext) => {
					selected = provider;
					ctx.dismiss();
				},
			})),
		});
	});
}

async function promptForValue(
	palette: PaletteManager,
	label: string,
): Promise<string> {
	return new Promise<string>((resolve) => {
		palette.show({
			mode: "input",
			label,
			onDismiss: () => resolve(""),
			onSubmit: (value: string, ctx: PaletteContext) => {
				ctx.dismiss();
				resolve(value);
			},
		});
	});
}

async function promptApiKey(palette: PaletteManager): Promise<boolean> {
	return new Promise<boolean>((resolve) => {
		palette.show({
			mode: "input",
			label: "Enter API key (e.g. ANTHROPIC_API_KEY)",
			onDismiss: () => resolve(false),
			onSubmit: (key: string, ctx: PaletteContext) => {
				const trimmed = key.trim();
				if (trimmed) process.env.ANTHROPIC_API_KEY = trimmed;
				ctx.dismiss();
				resolve(trimmed.length > 0);
			},
		});
	});
}
