import { useKeyboard } from "@opentui/solid";
import { Dialog } from "../../shell/Dialog";
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
		<Dialog.Root height="70%">
			<Dialog.Header>
				<Dialog.Title>{props.title}</Dialog.Title>
			</Dialog.Header>
			<Dialog.Body>
				<scrollbox flexGrow={1} scrollY focused>
					<box flexDirection="column" gap={0} width="100%">
						{props.lines.map((line) => (
							<text fg={theme.textSecondary}>{line}</text>
						))}
					</box>
				</scrollbox>
			</Dialog.Body>
			<Dialog.Footer>
				<HintBar bindings={BINDINGS} />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
