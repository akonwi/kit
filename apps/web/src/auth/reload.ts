// When the server that served this page is replaced, the session API answers
// with an instance or protocol mismatch, and the page reloads to load the
// replacement's client. It reloads at most once for each server instance, so
// a mismatch that persists cannot loop (ADR 0059).

const KEY = "kit.reloaded-for";

/**
 * Decides whether to reload for the server instance now answering. Returns
 * true, after recording the instance, when the page hasn't reloaded for it.
 */
export function shouldReload(instance: string, storage: Storage = sessionStorage): boolean {
  if (storage.getItem(KEY) === instance) return false;
  storage.setItem(KEY, instance);
  return true;
}
