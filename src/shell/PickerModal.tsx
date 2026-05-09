import type { KeyEvent } from "@opentui/core";
import { Show } from "solid-js";
import type { PickerManager } from "../state/picker-manager";
import { DialogFrame } from "./DialogFrame";
import { type Binding, HintBar } from "./HintBar";
import { theme } from "./theme";

export type PickerModalProps = {
	picker: PickerManager;
};

const BINDINGS: Binding[] = [{ key: "Enter/Esc", action: "close" }];

export function PickerModal(props: PickerModalProps) {
	const snapshot = () => props.picker.current();

	return (
		<Show when={snapshot().visible && snapshot().mode === "modal"}>
			<DialogFrame>
				<box
					focusable
					focused
					onKeyDown={(e: KeyEvent) => {
						if (e.name === "escape" || e.name === "return") {
							e.preventDefault();
							props.picker.pop();
						}
					}}
				/>
				<text fg={theme.textPrimary}>{snapshot().modalTitle}</text>
				<box flexDirection="column" gap={0} width="100%">
					{snapshot().modalLines.map((line) => (
						<text fg={theme.textSecondary}>{line}</text>
					))}
				</box>
				<HintBar bindings={BINDINGS} />
			</DialogFrame>
		</Show>
	);
}
