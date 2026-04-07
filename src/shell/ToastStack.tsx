import { For } from "solid-js";
import type { Toast } from "../state/app-state";
import { theme } from "./theme";

function ToastItem(props: { toast: Toast }) {
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
		>
			<text fg={color()}>{label()}</text>
		</box>
	);
}

export type ToastStackProps = {
	toasts: Toast[];
	bottom: number;
};

export function ToastStack(props: ToastStackProps) {
	return (
		<box
			position="absolute"
			bottom={props.bottom}
			left={2}
			right={2}
			zIndex={200}
			flexDirection="column"
			gap={0}
		>
			<For each={props.toasts}>{(toast) => <ToastItem toast={toast} />}</For>
		</box>
	);
}
