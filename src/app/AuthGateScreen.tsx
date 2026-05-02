import { homedir } from "node:os";
import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import { createSignal } from "solid-js";
import { runLoginFlow } from "../features/commands/login";
import type { Session } from "../session";
import { type Binding, HintBar } from "../shell/HintBar";
import { InlinePicker } from "../shell/InlinePicker";
import { Modal } from "../shell/Modal";
import { ScreenHeader } from "../shell/ScreenHeader";
import { ToastStack } from "../shell/ToastStack";
import { theme } from "../shell/theme";
import { createAppState } from "../state/app-state";
import { createPaletteManager } from "../state/palette-manager";

const AUTH_GATE_BINDINGS: Binding[] = [
	{ key: "Enter", action: "log in" },
	{ key: "L", action: "log in" },
	{ key: "Esc", action: "quit" },
];

function formatCwd(rawCwd: string): string {
	const home = homedir();
	return rawCwd.startsWith(home) ? `~${rawCwd.slice(home.length)}` : rawCwd;
}

export type AuthGateScreenProps = {
	session: Session;
	onAuthenticated: (providerName?: string) => Promise<boolean>;
	onQuit: () => void;
};

export function AuthGateScreen(props: AuthGateScreenProps) {
	const app = createAppState(null);
	const palette = createPaletteManager();
	const [headerHeight, setHeaderHeight] = createSignal(1);
	const [isLoggingIn, setIsLoggingIn] = createSignal(false);

	async function handleLogin() {
		if (isLoggingIn()) return;
		setIsLoggingIn(true);
		try {
			const result = await runLoginFlow(palette, {
				info: (title, lines) =>
					app.showToast({ title, lines, variant: "info" }),
				error: (title, lines) =>
					app.showToast({ title, lines, variant: "error" }),
			});

			if (!result.didAuthenticate) return;

			const didEnterReadyState = await props.onAuthenticated(
				result.providerName,
			);
			if (!didEnterReadyState) {
				app.showToast({
					title: "Login complete",
					lines: ["Credentials were saved, but no model is available yet."],
					variant: "warning",
				});
			}
		} finally {
			setIsLoggingIn(false);
		}
	}

	useKeyboard((event: KeyEvent) => {
		if (event.ctrl && event.name === "c") {
			event.preventDefault();
			if (palette.visible) {
				palette.clear();
				return;
			}
			props.onQuit();
			return;
		}

		if (palette.visible && !palette.isFilterable && !palette.isInputMode) {
			if (event.name === "up") {
				event.preventDefault();
				palette.moveUp();
				return;
			}
			if (event.name === "down") {
				event.preventDefault();
				palette.moveDown();
				return;
			}
			if (event.name === "escape") {
				event.preventDefault();
				palette.pop();
				return;
			}
			if (event.name === "return") {
				event.preventDefault();
				palette.selectCurrent();
				return;
			}
			if (event.ctrl && event.name) {
				const key = `ctrl+${event.name}`;
				if (palette.handleKeyBinding(key)) {
					event.preventDefault();
					return;
				}
			}
			return;
		}

		if (palette.visible) return;
		if (event.name === "return" || event.name === "l") {
			event.preventDefault();
			void handleLogin();
			return;
		}
		if (event.name === "escape" || (event.ctrl && event.name === "d")) {
			event.preventDefault();
			props.onQuit();
		}
	});

	return (
		<box
			width="100%"
			height="100%"
			flexDirection="column"
			backgroundColor={theme.bg}
		>
			<ScreenHeader
				left={<text fg={theme.textPrimary}>Kit</text>}
				right={<text fg={theme.textMuted}>{formatCwd(props.session.cwd)}</text>}
				onHeightChange={setHeaderHeight}
			/>

			<box
				flexGrow={1}
				width="100%"
				paddingX={1}
				justifyContent="center"
				alignItems="center"
			>
				<box flexDirection="column" alignItems="center" gap={1}>
					<box flexDirection="column" alignItems="center" gap={0}>
						<text fg={theme.textPrimary}>k i t</text>
						<text fg={theme.borderAccent}>━━━━━━━━━━━</text>
					</box>
					<text fg={theme.textSecondary}>
						Log in to an AI provider to get started.
					</text>
					<text fg={theme.textPlaceholder}>/login</text>
				</box>
			</box>

			<HintBar bindings={AUTH_GATE_BINDINGS} />
			<InlinePicker palette={palette} bottomOffset={3} />
			<Modal palette={palette} />
			<ToastStack
				toasts={app.state.toasts}
				top={headerHeight()}
				zIndex={200}
				onDismiss={app.dismissToast}
			/>
		</box>
	);
}
