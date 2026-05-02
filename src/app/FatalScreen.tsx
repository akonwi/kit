import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import { type Binding, HintBar } from "../shell/HintBar";
import { ScreenHeader } from "../shell/ScreenHeader";
import { ScreenLayout } from "../shell/ScreenLayout";
import { theme } from "../shell/theme";

const FATAL_BINDINGS: Binding[] = [{ key: "Esc", action: "quit" }];

export type FatalScreenProps = {
	error: string;
	onQuit: () => void;
};

export function FatalScreen(props: FatalScreenProps) {
	useKeyboard((event: KeyEvent) => {
		if (
			event.name === "escape" ||
			(event.ctrl && (event.name === "c" || event.name === "d"))
		) {
			event.preventDefault();
			props.onQuit();
		}
	});

	return (
		<ScreenLayout
			header={
				<ScreenHeader
					left={<text fg={theme.textPrimary}>Kit</text>}
					right={<text fg={theme.errorText}>Startup failed</text>}
				/>
			}
			footer={<HintBar bindings={FATAL_BINDINGS} />}
		>
			<box
				flexGrow={1}
				width="100%"
				paddingX={1}
				justifyContent="center"
				alignItems="center"
			>
				<box flexDirection="column" alignItems="center" gap={1} maxWidth={96}>
					<text fg={theme.errorText}>Kit could not start.</text>
					<text fg={theme.textSecondary}>{props.error}</text>
				</box>
			</box>
		</ScreenLayout>
	);
}
