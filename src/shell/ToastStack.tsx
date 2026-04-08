import { For } from "solid-js";
import type { Toast } from "../state/app-state";
import { theme } from "./theme";

function ToastItem(props: { toast: Toast; onDismiss: () => void }) {
	const color = () =>
		props.toast.variant === "error" ? theme.errorText : theme.textSecondary;
	const label = () =>
		props.toast.lines.length > 0
			? `${props.toast.title}: ${props.toast.lines.join(" ")}`
			: props.toast.title;

	return (
		<box
			paddingX={2}
			paddingY={0}
			backgroundColor={theme.pickerBg}
			border
			borderColor={color()}
			flexDirection="row"
			gap={1}
		>
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
	onDismiss: (id: number) => void;
};

export function ToastStack(props: ToastStackProps) {
	return (
		<box
			position="absolute"
			top={props.top}
			left={2}
			right={2}
			zIndex={200}
			flexDirection="column"
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
