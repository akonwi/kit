import { useTimeline } from "@opentui/solid";
import { createSignal, For, onMount } from "solid-js";
import type { Toast } from "../state/toasts";
import { GLYPH_ACTIVE, GLYPH_ERROR } from "./glyphs";
import { theme } from "./theme";

function ToastItem(props: { toast: Toast; onDismiss: () => void }) {
	const [offset, setOffset] = createSignal(20);

	const timeline = useTimeline({ duration: 300 });

	onMount(() => {
		timeline.add(
			{ x: 20 },
			{
				x: 0,
				duration: 300,
				ease: "outCirc",
				onUpdate: (anim) => {
					setOffset(Math.round(anim.targets[0].x));
				},
			},
		);
	});

	const color = () =>
		props.toast.variant === "error"
			? theme.errorText
			: props.toast.variant === "warning"
				? theme.warningText
				: theme.metaText;

	const icon = () =>
		props.toast.variant === "error"
			? GLYPH_ERROR
			: props.toast.variant === "warning"
				? "▲"
				: GLYPH_ACTIVE;

	const label = () =>
		props.toast.lines.length > 0
			? `${props.toast.title}: ${props.toast.lines.join(" ")}`
			: props.toast.title;

	return (
		<box
			position="relative"
			left={offset()}
			border
			borderColor={color()}
			backgroundColor={theme.bg}
			paddingX={1}
			flexDirection="row"
			gap={1}
			maxWidth="50%"
		>
			<text fg={color()}>{icon()}</text>
			<text flexGrow={1} fg={color()}>
				{label()}
			</text>
			<box onMouseUp={props.onDismiss}>
				<text fg={theme.textMuted}>✕</text>
			</box>
		</box>
	);
}

export type ToastStackProps = {
	toasts: Toast[];
	top: number;
	zIndex: number;
	onDismiss: (id: number) => void;
};

export function ToastStack(props: ToastStackProps) {
	return (
		<box
			position="absolute"
			top={props.top}
			left={2}
			right={2}
			zIndex={props.zIndex}
			flexDirection="column"
			alignItems="flex-end"
			gap={0}
		>
			<For each={props.toasts}>
				{(toast) => (
					<ToastItem
						toast={toast}
						onDismiss={() => props.onDismiss(toast.id)}
					/>
				)}
			</For>
		</box>
	);
}
