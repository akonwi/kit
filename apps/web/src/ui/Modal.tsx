// Modal dialogs on Mica's <dialog>: a centered dialog, or a drawer that is a
// bottom sheet on a phone and follows its keyboard (mica/drawer.js).
import "@akonwi/mica/drawer.js";
import { createEffect, type JSX, onCleanup, Show } from "solid-js";
import { Icon } from "./icons";

export interface ModalProps {
  open: boolean;
  /** Called when the dialog closes itself: Esc, the close button, or a drag down. */
  onClose(): void;
  title: string;
  /** A line under the title. */
  detail?: JSX.Element;
  /** A bottom sheet on narrow screens rather than a centered dialog. */
  drawer?: boolean;
  /** Alert dialogs are announced as such and have no corner close button. */
  alert?: boolean;
  class?: string;
  children: JSX.Element;
  footer?: JSX.Element;
}

let ids = 0;

export function Modal(props: ModalProps) {
  let dialog: HTMLDialogElement | undefined;
  const id = `modal-${++ids}`;

  createEffect(() => {
    if (!dialog) return;
    if (props.open && !dialog.open) dialog.showModal();
    else if (!props.open && dialog.open) dialog.close();
  });
  onCleanup(() => dialog?.close());

  return (
    <dialog
      ref={dialog}
      class={props.class}
      data-drawer={props.drawer ? "" : undefined}
      role={props.alert ? "alertdialog" : undefined}
      aria-labelledby={`${id}-title`}
      aria-describedby={props.detail ? `${id}-detail` : undefined}
      onClose={() => {
        if (props.open) props.onClose();
      }}
    >
      <header>
        <h2 id={`${id}-title`}>{props.title}</h2>
        <Show when={props.detail}>
          <p id={`${id}-detail`}>{props.detail}</p>
        </Show>
      </header>
      <Show when={!props.alert}>
        <button type="button" class="close" aria-label="Close" onClick={() => dialog?.close()}>
          <Icon name="close" size={18} />
        </button>
      </Show>
      <div class="modal-body">{props.children}</div>
      <Show when={props.footer}>
        <footer>{props.footer}</footer>
      </Show>
    </dialog>
  );
}
