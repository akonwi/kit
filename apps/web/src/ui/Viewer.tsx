// The full-screen viewer: a modal dialog over the session that Back closes,
// as an attachment or a diagram opens.
import { createEffect, type JSX, onCleanup, Show } from "solid-js";

export function Viewer(props: {
  open: boolean;
  label: string;
  onClose(): void;
  /** The viewer's content, given the close that also takes its history entry. */
  children: (close: () => void) => JSX.Element;
}) {
  let dialog: HTMLDialogElement | undefined;
  // The history entry the viewer pushed, until Back takes it.
  let entry = false;
  // Back was asked for; its popstate closes the viewer.
  let closing = false;

  const popstate = () => {
    if (!entry) return;
    entry = false;
    closing = false;
    props.onClose();
  };
  window.addEventListener("popstate", popstate);
  onCleanup(() => window.removeEventListener("popstate", popstate));

  const close = () => {
    if (closing) return;
    if (entry && history.state?.viewer) {
      closing = true;
      history.back();
    } else {
      entry = false;
      props.onClose();
    }
  };

  createEffect(() => {
    if (!dialog) return;
    if (props.open && !dialog.open) {
      dialog.showModal();
      history.pushState({ inApp: true, viewer: true }, "");
      entry = true;
    } else if (!props.open && dialog.open) {
      dialog.close();
    }
  });
  onCleanup(() => {
    if (entry && !closing && history.state?.viewer) history.back();
  });

  return (
    <dialog
      ref={dialog}
      class="viewer"
      aria-label={props.label}
      onCancel={(event) => {
        event.preventDefault();
        close();
      }}
    >
      <Show when={props.open}>{props.children(close)}</Show>
    </dialog>
  );
}
