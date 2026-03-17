import { copyToClipboard } from "./clipboard";

type Renderer = {
  getSelection(): { getSelectedText(): string } | null;
  clearSelection(): void;
};

/**
 * If there is a text selection, copy it to the clipboard, clear the
 * selection, and return true. Otherwise return false.
 */
export function copySelection(renderer: Renderer): boolean {
  const text = renderer.getSelection()?.getSelectedText();
  if (!text) return false;

  copyToClipboard(text).catch((err) => {
    console.error("clipboard copy failed:", err);
  });

  renderer.clearSelection();
  return true;
}
