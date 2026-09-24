import { isRecord } from "@akonwi/kit-session-client";
import { type ToastVariant, toastForRuntimeRecord } from "../state/toasts";

export type WebToastInput = {
	title: string;
	description?: string;
	variant: ToastVariant;
	duration?: number;
};

export type WebToastSink = (input: WebToastInput) => void;

export const DEFAULT_WEB_TOAST_DURATION_MS = 10_000;

export function toastForProtocolRecord(record: unknown): WebToastInput | null {
	if (
		isRecord(record) &&
		record.type === "ui.toast.requested" &&
		isRecord(record.toast) &&
		typeof record.toast.title === "string" &&
		record.toast.title.length > 0 &&
		(record.toast.subtitle === undefined ||
			typeof record.toast.subtitle === "string") &&
		(record.toast.variant === "error" ||
			record.toast.variant === "info" ||
			record.toast.variant === "warning") &&
		(record.toast.persistent === undefined ||
			typeof record.toast.persistent === "boolean")
	) {
		return {
			title: record.toast.title,
			description: record.toast.subtitle,
			variant: record.toast.variant,
			duration: record.toast.persistent ? 0 : DEFAULT_WEB_TOAST_DURATION_MS,
		};
	}
	const toast = toastForRuntimeRecord(record);
	return toast
		? {
				title: toast.title,
				description: toast.subtitle,
				variant: toast.variant,
				duration: DEFAULT_WEB_TOAST_DURATION_MS,
			}
		: null;
}
