import { createSignal, Show } from "solid-js";
import type { OverlayComponentProps } from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { Dialog } from "../../shell/Dialog";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";

export function SubagentDismissDialog(props: {
	overlay: OverlayComponentProps<boolean>;
	agentName: string;
	running: boolean;
	dismiss: () => Promise<boolean>;
	onConfirmingChange?: (confirming: boolean) => void;
}) {
	const [dismissing, setDismissing] = createSignal(false);
	const [error, setError] = createSignal<string | null>(null);

	async function confirm(): Promise<void> {
		if (dismissing()) return;
		setDismissing(true);
		setError(null);
		props.onConfirmingChange?.(true);
		try {
			const dismissed = await props.dismiss();
			if (!dismissed) {
				setError("Conversation is no longer active.");
				setDismissing(false);
				props.onConfirmingChange?.(false);
				return;
			}
			props.overlay.done(true);
			props.onConfirmingChange?.(false);
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : String(cause));
			setDismissing(false);
			props.onConfirmingChange?.(false);
		}
	}

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => props.overlay.active !== false,
		commands: {
			"subagents.confirm-dismiss": () => void confirm(),
			"subagents.cancel-dismiss": () => {
				if (!dismissing()) props.overlay.done(false);
			},
		},
	}));

	return (
		<Dialog.Root
			maxWidth={80}
			paddingBottom={0}
			surfaceProps={props.overlay.surfaceProps}
		>
			<Dialog.Header>
				<Dialog.Title fg={theme.errorText}>
					Dismiss "{props.agentName}"?
				</Dialog.Title>
			</Dialog.Header>
			<box flexDirection="column">
				<text fg={theme.textPrimary}>
					The transcript and conversation context will be deleted.
				</text>
				<Show when={props.running}>
					<text fg={theme.warningText}>
						The running execution will also be aborted.
					</text>
				</Show>
				<Show when={error()}>
					{(message) => <text fg={theme.errorText}>{message()}</text>}
				</Show>
				<Show when={dismissing()}>
					<text fg={theme.textMuted}>Dismissing…</text>
				</Show>
			</box>
			<Dialog.Footer>
				<KeymapHintBar borderless group="subagent-dismiss" />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
