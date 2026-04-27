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

export const loginCommand: Command = {
	name: "login",
	description: "Log in to an AI provider",
	async execute({ palette, runtime }) {
		const providers = getOAuthProviders();

		if (providers.length === 0) {
			await promptApiKey(palette);
			return;
		}

		// Step 1: pick provider
		const provider = await new Promise<OAuthProviderInterface | null>(
			(resolve) => {
				let selected: OAuthProviderInterface | null = null;
				palette.show({
					filterable: false,
					hint: "Select a provider",
					onDismiss: () => resolve(selected),
					options: providers.map((p: OAuthProviderInterface) => ({
						name: p.name,
						description: p.id,
						value: p,
						action: (ctx: PaletteContext) => {
							selected = p;
							ctx.dismiss();
						},
					})),
				});
			},
		);

		console.log("[login] provider selected:", provider?.id);
		if (!provider) return;

		// Step 2: run OAuth flow
		console.log("[login] starting OAuth flow for:", provider.name);
		runtime.emitInfo(`Logging in to ${provider.name}…`, []);
		try {
			const credentials = await provider.login({
				onAuth({ url, instructions: _instructions }: OAuthAuthInfo) {
					console.log("[login] onAuth called, url:", url?.slice(0, 60));
					exec(`open "${url}"`);
					runtime.emitInfo("Browser opened", [
						"Complete login then return here.",
					]);
				},

				onProgress(msg: string) {
					runtime.emitInfo(msg, []);
				},

				onPrompt: (prompt: OAuthPrompt) =>
					new Promise<string>((resolve) => {
						palette.show({
							mode: "input",
							label: prompt.message,
							onDismiss: () => resolve(""),
							onSubmit: (value: string, ctx: PaletteContext) => {
								ctx.dismiss();
								resolve(value);
							},
						});
					}),

				onManualCodeInput: () =>
					new Promise<string>((resolveInput) => {
						palette.show({
							mode: "input",
							label:
								"Paste the redirect URL (or leave blank to wait for browser callback)",
							onDismiss: () => resolveInput(""),
							onSubmit: (value: string, ctx: PaletteContext) => {
								ctx.dismiss();
								resolveInput(value);
							},
						});
					}),
			});

			// Dismiss the manual URL input if still open
			if (palette.isInputMode) palette.pop();

			// Persist credentials with type field so getApiKey can read them
			const auth = await readAuthFile();
			auth[provider.id] = { ...credentials, type: "oauth" };
			await writeAuthFile(auth);

			runtime.emitInfo("Login successful", [`Logged in to ${provider.name}.`]);
		} catch (err) {
			console.log("[login] error:", err);
			runtime.emitError("Login failed", [String(err)]);
		}
	},
};

async function promptApiKey(palette: PaletteManager): Promise<void> {
	await new Promise<void>((resolve) => {
		palette.show({
			mode: "input",
			label: "Enter API key (e.g. ANTHROPIC_API_KEY)",
			onDismiss: resolve,
			onSubmit: (key: string, ctx: PaletteContext) => {
				if (key.trim()) process.env.ANTHROPIC_API_KEY = key.trim();
				ctx.dismiss();
				resolve();
			},
		});
	});
}
