import type { KeyEvent } from "@opentui/core";
import { Show } from "solid-js";
import type { PaletteManager } from "../state/palette-manager";
import { theme } from "./theme";

export type ModalProps = {
	palette: PaletteManager;
};

export function Modal(props: ModalProps) {
	const modal = () => props.palette.current();

	return (
		<Show when={modal().visible && modal().mode === "modal"}>
			<box
				position="absolute"
				left={0}
				top={0}
				width="100%"
				height="100%"
				justifyContent="center"
				alignItems="center"
				zIndex={1000}
				backgroundColor={theme.modalBackdrop}
			>
				<box
					width="70%"
					maxWidth={96}
					minWidth={40}
					flexDirection="column"
					focusable
					focused
					onKeyDown={(e: KeyEvent) => {
						if (e.name === "escape" || e.name === "return") {
							e.preventDefault();
							props.palette.pop();
						}
					}}
				>
					<box
						backgroundColor={theme.bgSurface}
						padding={1}
						flexDirection="column"
						gap={1}
						flexGrow={1}
					>
						<text fg={theme.textPrimary}>{modal().modalTitle}</text>
						<box flexDirection="column" gap={0} width="100%">
							{modal().modalLines.map((line) => (
								<text fg={theme.textSecondary}>{line}</text>
							))}
						</box>
						<text fg={theme.textMuted}>Enter/Esc to close</text>
					</box>
				</box>
			</box>
		</Show>
	);
}
