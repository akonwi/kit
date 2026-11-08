// How attachments look: thumbnails and file tiles for what you send, images
// at full width in Kit's replies, and a full-screen viewer for either.
import {
  createContext,
  createResource,
  createSignal,
  For,
  type JSX,
  Match,
  Show,
  Switch,
  useContext,
} from "solid-js";
import { bytes } from "../ui/format";
import { Icon, Spinner } from "../ui/icons";
import { Viewer } from "../ui/Viewer";
import type { AttachmentLibrary } from "./library";
import type { Attachment } from "./projection";

/** An attachment the server has, which the viewer can open. */
export type Openable = Attachment & { id: string };

export interface AttachmentsContextValue {
  library: AttachmentLibrary;
  open(attachment: Openable): void;
}

export const AttachmentsContext = createContext<AttachmentsContextValue>();

function useAttachments(): AttachmentsContextValue {
  const value = useContext(AttachmentsContext);
  if (!value) throw new Error("Attachments need an AttachmentsContext.");
  return value;
}

/** An image's square thumbnail; the image icon while it loads or if it can't. */
export function Thumbnail(props: { url: string | null | undefined; label: string; children?: JSX.Element }) {
  return (
    <span class="thumb">
      <Show when={props.url} fallback={<Icon name="image" size={20} />}>
        {(url) => <img src={url()} alt={props.label} draggable={false} />}
      </Show>
      {props.children}
    </span>
  );
}

/** A file's tile: its icon, name, and a line under it. */
export function FileTile(props: { name: string; detail: JSX.Element; children?: JSX.Element }) {
  return (
    <>
      <Icon name="file" size={20} />
      <span class="file-tile-text">
        <span class="file-tile-name">{props.name}</span>
        <span class="file-tile-detail">{props.detail}</span>
      </span>
      {props.children}
    </>
  );
}

/** Attachments you sent: images as thumbnails, other files as tiles. */
export function SentAttachments(props: { items: Attachment[] }) {
  const context = useAttachments();
  return (
    <Show when={props.items.length > 0}>
      <ul class="attachment-strip" aria-label="Attachments">
        <For each={props.items}>
          {(item) => (
            <li>
              <AttachmentButton item={item} class={item.image ? "thumb-button" : "file-tile"}>
                <Show
                  when={item.image}
                  fallback={<FileTile name={item.filename} detail={<Size id={item.id} />} />}
                >
                  <Thumbnail url={item.id ? context.library.image(item.id) : null} label={item.filename} />
                </Show>
              </AttachmentButton>
            </li>
          )}
        </For>
      </ul>
    </Show>
  );
}

/** Attachments in Kit's replies: images at the column's width, files as tiles. */
export function ReplyAttachments(props: { items: Attachment[] }) {
  const context = useAttachments();
  return (
    <Show when={props.items.length > 0}>
      <div class="reply-attachments">
        <For each={props.items}>
          {(item) => (
            <Show
              when={item.image}
              fallback={
                <AttachmentButton item={item} class="file-tile">
                  <FileTile name={item.filename} detail={<Size id={item.id} />} />
                </AttachmentButton>
              }
            >
              <figure class="reply-image">
                <AttachmentButton item={item} class="reply-image-button">
                  <Show
                    when={item.id ? context.library.image(item.id) : null}
                    fallback={
                      <span class="reply-image-placeholder">
                        <Icon name="image" size={24} />
                      </span>
                    }
                  >
                    {(url) => <img src={url()} alt={item.filename} draggable={false} />}
                  </Show>
                </AttachmentButton>
                <figcaption>
                  <span>{item.filename}</span>
                  <Size id={item.id} />
                </figcaption>
              </figure>
            </Show>
          )}
        </For>
      </div>
    </Show>
  );
}

/** Opens the viewer; an attachment the server never named can't be opened. */
function AttachmentButton(props: { item: Attachment; class: string; children: JSX.Element }) {
  const context = useAttachments();
  const id = props.item.id;
  return (
    <button
      type="button"
      class={props.class}
      disabled={!id}
      aria-label={`Open ${props.item.filename}`}
      onClick={() => id && context.open({ ...props.item, id })}
    >
      {props.children}
    </button>
  );
}

/** The attachment's size once the server says it. */
function Size(props: { id: string | undefined }) {
  const context = useAttachments();
  const info = () => (props.id ? context.library.info(props.id) : null);
  return (
    <span class="mono">
      <Show when={info()} fallback={info() === null ? "Not available" : ""}>
        {(value) => bytes(value().size)}
      </Show>
    </span>
  );
}

/** Saves an attachment under its name. */
export async function download(library: AttachmentLibrary, id: string, filename: string): Promise<void> {
  const url = URL.createObjectURL(await library.blob(id));
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

/**
 * The full-screen viewer. It takes a history entry, so Back closes it as
 * well as × and Escape. An image fits the screen and pinches to zoom; on a
 * laptop a click shows it at full size. A text file shows as plain text.
 */
export function AttachmentViewer(props: { attachment: Openable | undefined; onClose(): void }) {
  return (
    <Viewer
      open={Boolean(props.attachment)}
      label={props.attachment?.filename ?? "Attachment"}
      onClose={() => props.onClose()}
    >
      {(close) => (
        // Keyed: each attachment gets its own body, with nothing of the last one's.
        <Show when={props.attachment} keyed>
          {(attachment) => <ViewerBody attachment={attachment} onClose={close} />}
        </Show>
      )}
    </Viewer>
  );
}

function ViewerBody(props: { attachment: Openable; onClose(): void }) {
  const context = useAttachments();
  const { id, filename, image } = props.attachment;
  const meta = () => {
    const value = context.library.info(id);
    if (!value) return "";
    const size = bytes(value.size);
    return value.width && value.height ? `${size} · ${value.width} × ${value.height}` : size;
  };
  const [text] = createResource(
    () => (image ? undefined : id),
    async (value) => (await context.library.blob(value)).text(),
  );
  return (
    <>
      <header class="viewer-bar">
        <button type="button" class="icon-button" aria-label="Close" onClick={() => props.onClose()}>
          <Icon name="close" />
        </button>
        <div class="viewer-title">
          <h2>{filename}</h2>
          <p class="mono muted">{meta()}</p>
        </div>
        <button
          type="button"
          class="icon-button"
          aria-label="Download"
          onClick={() => void download(context.library, id, filename)}
        >
          <Icon name="download" />
        </button>
      </header>
      <div class="viewer-stage" data-kind={image ? "image" : "text"}>
        <Switch>
          <Match when={image}>
            <ViewerImage url={context.library.image(id)} label={filename} />
          </Match>
          <Match when={text.error}>
            <p class="muted">This file couldn’t be loaded.</p>
          </Match>
          <Match when={text.state === "ready"}>
            <pre class="viewer-text">{text()}</pre>
          </Match>
          <Match when={true}>
            <Spinner size={18} />
          </Match>
        </Switch>
      </div>
    </>
  );
}

function ViewerImage(props: { url: string | null | undefined; label: string }) {
  const [zoomed, setZoomed] = createSignal(false);
  return (
    <Switch fallback={<Spinner size={18} />}>
      <Match when={props.url === null}>
        <p class="muted">This image couldn’t be loaded.</p>
      </Match>
      <Match when={props.url}>
        {(url) => (
          <button
            type="button"
            class="viewer-zoom"
            aria-pressed={zoomed()}
            aria-label="Actual size"
            data-zoomed={zoomed() || undefined}
            onClick={() => setZoomed(!zoomed())}
          >
            <img src={url()} alt={props.label} draggable={false} />
          </button>
        )}
      </Match>
    </Switch>
  );
}
