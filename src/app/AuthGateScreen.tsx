import { homedir } from "node:os";
import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import { createSignal, Show } from "solid-js";
import { LoginModal, type LoginOutcome } from "../features/login/LoginModal";
import type { Session } from "../session";
import { GLYPH_HORIZONTAL_HEAVY } from "../shell/glyphs";
import { type Binding, HintBar } from "../shell/HintBar";
import { ScreenHeader } from "../shell/ScreenHeader";
import { ToastStack } from "../shell/ToastStack";
import { theme } from "../shell/theme";
import { createAppState } from "../state/app-state";

const AUTH_GATE_BINDINGS: Binding[] = [
	{ key: "Enter", action: "log in" },
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
	const [headerHeight, setHeaderHeight] = createSignal(1);
	const [loginOpen, setLoginOpen] = createSignal(false);

	async function handleLoginResult(result: LoginOutcome) {
		setLoginOpen(false);
		if (!result.didAuthenticate) return;

		const didEnterReadyState = await props.onAuthenticated(result.providerName);
		if (!didEnterReadyState) {
			app.showToast({
				title: "Login complete",
				lines: ["Credentials were saved, but no model is available yet."],
				variant: "warning",
			});
		}
	}

	useKeyboard((event: KeyEvent) => {
		if (loginOpen()) return;
		if (event.ctrl && event.name === "c") {
			event.preventDefault();
			props.onQuit();
			return;
		}
		if (event.name === "return") {
			event.preventDefault();
			setLoginOpen(true);
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
						<text fg={theme.borderAccent}>
							{GLYPH_HORIZONTAL_HEAVY.repeat(11)}
						</text>
					</box>
					<text fg={theme.textSecondary}>
						Log in to an AI provider to get started.
					</text>
					<text fg={theme.textPlaceholder}>Press Enter to log in</text>
				</box>
			</box>

			<HintBar bindings={AUTH_GATE_BINDINGS} />
			<Show when={loginOpen()}>
				<LoginModal
					surfaceProps={{ zIndex: 300 }}
					onClose={(result) => {
						void handleLoginResult(result);
					}}
				/>
			</Show>
			<ToastStack
				toasts={app.state.toasts}
				top={headerHeight()}
				zIndex={400}
				onDismiss={app.dismissToast}
			/>
		</box>
	);
}
