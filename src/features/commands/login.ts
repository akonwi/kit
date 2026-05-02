import { createComponent } from "solid-js";
import { LoginModal, type LoginOutcome } from "../login/LoginModal";
import type { Command, CommandContext } from "./types";

async function openLoginModal(
	openCustomOverlay: CommandContext["openCustomOverlay"],
): Promise<LoginOutcome> {
	return openCustomOverlay((props) =>
		createComponent(LoginModal, {
			surfaceProps: props.surfaceProps,
			onClose: (result: LoginOutcome) => props.done(result),
		}),
	);
}

export const loginCommand: Command = {
	name: "login",
	description: "Log in to an AI provider",
	async execute({ runtime, openCustomOverlay }) {
		const result = await openLoginModal(openCustomOverlay);
		if (!result.didAuthenticate) return;

		runtime.emitInfo("Login successful", [
			result.providerName
				? `Logged in to ${result.providerName}.`
				: "Credentials saved.",
		]);
	},
};
