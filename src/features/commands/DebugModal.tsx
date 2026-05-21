import type { Renderable } from "@opentui/core";
import { createSignal } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { Dialog } from "../../shell/Dialog";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";

export type DebugModalProps = {
	title: string;
	lines: string[];
	active?: boolean;
	surfaceProps?: OverlaySurfaceProps;
	onClose: () => void;
};

export function DebugModal(props: DebugModalProps) {
	const [rootTarget, setRootTarget] = createSignal<Renderable | null>(null);

	useKeymapLayer(() => ({
		scope: "modal",
		target: rootTarget,
		targetMode: "focus-within",
		when: () => props.active !== false,
		commands: {
			"debug.close": () => props.onClose(),
		},
	}));

	return (
		<Dialog.Root
			height="70%"
			padding={0}
			surfaceProps={props.surfaceProps}
			rootRef={setRootTarget}
		>
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
				<KeymapHintBar borderless group="debug" />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
