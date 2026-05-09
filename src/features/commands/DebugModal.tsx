import { useKeyboard } from "@opentui/solid";
import { DialogFrame } from "../../shell/DialogFrame";
import { type Binding, HintBar } from "../../shell/HintBar";
import { theme } from "../../shell/theme";

export type DebugModalProps = {
	title: string;
	lines: string[];
	onClose: () => void;
};

const BINDINGS: Binding[] = [{ key: "Enter/Esc", action: "close" }];

export function DebugModal(props: DebugModalProps) {
	useKeyboard((e) => {
		if (e.name === "escape" || e.name === "return") {
			e.preventDefault();
			props.onClose();
		}
	});

	return (
		<DialogFrame height="70%">
			<text fg={theme.textPrimary}>{props.title}</text>
			<scrollbox flexGrow={1} scrollY focused>
				<box flexDirection="column" gap={0} width="100%">
					{props.lines.map((line) => (
						<text fg={theme.textSecondary}>{line}</text>
					))}
				</box>
			</scrollbox>
			<HintBar bindings={BINDINGS} />
		</DialogFrame>
	);
}
