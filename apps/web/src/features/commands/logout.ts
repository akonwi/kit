import { kitCredentialStore } from "../../auth";
import { kitModels, refreshModelAvailability } from "../../runtime/models";
import { resolveDefaultAuthenticatedModel } from "../../runtime/provider-selection";
import type { PickerContext } from "../../state/picker";
import type { Command, CommandContext } from "./types";

export type LogoutProviderOption = {
	id: string;
	name: string;
};

/**
 * Providers with saved credentials, sorted for stable picker ordering.
 * Includes credential entries for providers no longer in the current
 * registry, using their raw id as the display name, so stale entries can
 * still be cleared.
 */
export function authenticatedProviders(
	authenticatedIds: ReadonlySet<string>,
): LogoutProviderOption[] {
	const names = new Map(
		kitModels.getProviders().map((provider) => [provider.id, provider.name]),
	);
	return [...authenticatedIds]
		.map((id) => ({ id, name: names.get(id) ?? id }))
		.sort((a, b) => a.name.localeCompare(b.name));
}

/**
 * When the active model's provider was just logged out, switch to another
 * still-authenticated model if one is available. Returns the model switched
 * to, or undefined if no reconciliation was needed or possible.
 *
 * Takes the caller's own up-to-date authenticated-id set rather than
 * re-reading it, since the model-availability cache can still be stale
 * immediately after a credential change.
 */
export function reconcileActiveModel(
	runtime: Pick<CommandContext["runtime"], "getCurrentModel" | "setModel">,
	remainingAuthenticatedProviderIds: readonly string[],
) {
	const currentModel = runtime.getCurrentModel();
	if (!currentModel) return undefined;

	const fallback = resolveDefaultAuthenticatedModel([
		...remainingAuthenticatedProviderIds,
	]);
	if (!fallback) return undefined;

	runtime.setModel(fallback);
	return fallback;
}

export const logoutCommand: Command = {
	name: "logout",
	description: "Log out of an AI provider",
	async execute({ picker, toast, runtime }) {
		const authenticated = new Set(
			(await kitCredentialStore.list()).map((entry) => entry.providerId),
		);
		if (authenticated.size === 0) {
			toast({
				title: "Not logged in to any providers",
				variant: "info",
			});
			return;
		}

		const providers = authenticatedProviders(authenticated);

		picker.show({
			filterable: true,
			label: "Log out of a provider",
			options: providers.map((provider) => ({
				name: provider.name,
				description: provider.id,
				value: provider.id,
				action: async (ctx: PickerContext) => {
					ctx.dismiss();

					try {
						await kitCredentialStore.delete(provider.id);
						authenticated.delete(provider.id);
					} catch (error) {
						toast({
							title: "Failed to log out",
							subtitle: error instanceof Error ? error.message : String(error),
							variant: "error",
						});
						return;
					}

					toast({
						title: "Logged out",
						subtitle: `Removed credentials for ${provider.name}.`,
						variant: "info",
					});

					const wasActiveProvider =
						runtime.getCurrentModel()?.provider === provider.id;

					let refreshed = true;
					try {
						await refreshModelAvailability();
					} catch (error) {
						refreshed = false;
						toast({
							title: "Could not refresh provider availability",
							subtitle: error instanceof Error ? error.message : String(error),
							variant: "warning",
						});
					}

					if (!wasActiveProvider) return;

					// Availability is stale after a failed refresh, so it may still
					// report the just-removed provider as authenticated. Skip
					// reconciliation rather than risk picking that provider again.
					if (!refreshed) {
						toast({
							title: "Active model may be unavailable",
							subtitle: "Run /model to pick a new one once available.",
							variant: "warning",
						});
						return;
					}

					const fallback = reconcileActiveModel(runtime, [...authenticated]);
					if (fallback) {
						toast({
							title: "Switched active model",
							subtitle: `${fallback.name ?? fallback.id} is now active.`,
							variant: "info",
						});
					} else {
						// No authenticated provider remains to fall back to. The runtime
						// has no supported "unauthenticated" state once a session is
						// running (only at startup), so the active model reference is
						// left as-is; the next send will surface a normal auth error the
						// same way an expired/invalid credential would. The warning below
						// is the actionable signal to re-authenticate first.
						toast({
							title: "No authenticated providers remain",
							subtitle:
								"Run /login, then /model to pick a new active model before sending a message.",
							variant: "warning",
						});
					}
				},
			})),
		});
	},
};
