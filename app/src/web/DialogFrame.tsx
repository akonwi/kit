/** @jsxImportSource solid-js */
import { createEffect, type JSX } from "solid-js";

export function DialogFrame(props: {
	open: boolean;
	id: string;
	class?: string;
	labelledBy: string;
	focusKey?: unknown;
	children: JSX.Element;
	onCancel: () => void;
	onAfterOpen?: (dialog: HTMLDialogElement) => void;
	restoreFocus?: () => boolean;
}): JSX.Element {
	let dialog: HTMLDialogElement | undefined;
	let returnFocus: HTMLElement | null = null;

	createEffect(() => {
		const open = props.open;
		const focusKey = props.focusKey;
		if (!dialog) return;
		if (open) {
			if (!dialog.open) {
				returnFocus =
					document.activeElement instanceof HTMLElement
						? document.activeElement
						: null;
				dialog.showModal();
			}
			const currentDialog = dialog;
			void focusKey;
			queueMicrotask(() => {
				if (currentDialog.open) props.onAfterOpen?.(currentDialog);
			});
			return;
		}
		if (!dialog.open) return;
		dialog.close();
		const target = returnFocus;
		returnFocus = null;
		queueMicrotask(() => {
			if (props.restoreFocus?.()) return;
			if (target?.isConnected) target.focus();
			if (!target || document.activeElement !== target) {
				document.querySelector<HTMLElement>("#transcript")?.focus();
			}
		});
	});

	return (
		<dialog
			ref={dialog}
			id={props.id}
			class={props.class}
			aria-labelledby={props.labelledBy}
			onClick={(event) => {
				if (event.target !== event.currentTarget) return;
				const bounds = event.currentTarget.getBoundingClientRect();
				const outside =
					event.clientX < bounds.left ||
					event.clientX > bounds.right ||
					event.clientY < bounds.top ||
					event.clientY > bounds.bottom;
				if (outside) props.onCancel();
			}}
			onCancel={(event) => {
				event.preventDefault();
				props.onCancel();
			}}
		>
			{props.children}
		</dialog>
	);
}
