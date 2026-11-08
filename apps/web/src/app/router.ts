// Client-side navigation among the pages the server answers: /, /pair, and
// /sessions/{id}.
import { createSignal } from "solid-js";

export type Route = { page: "sessions" } | { page: "pair"; code: string } | { page: "session"; id: string };

export function parseRoute(pathname: string, hash: string): Route {
  if (pathname === "/pair") return { page: "pair", code: pairingCode(hash) };
  const session = /^\/sessions\/([A-Za-z0-9._-]{1,128})$/.exec(pathname);
  if (session?.[1]) return { page: "session", id: session[1] };
  return { page: "sessions" };
}

/** The fragment's code; a malformed fragment is no code, which the page refuses. */
function pairingCode(hash: string): string {
  try {
    return decodeURIComponent(hash.replace(/^#/, ""));
  } catch {
    return "";
  }
}

// A history entry on the same page, such as an open viewer's, keeps the page.
const [location, setLocation] = createSignal(
  { pathname: window.location.pathname, hash: window.location.hash },
  { equals: (a, b) => a.pathname === b.pathname && a.hash === b.hash },
);

window.addEventListener("popstate", () => {
  setLocation({ pathname: window.location.pathname, hash: window.location.hash });
});

export const route = () => parseRoute(location().pathname, location().hash);

export function navigate(path: string, options: { replace?: boolean } = {}): void {
  if (options.replace) history.replaceState(null, "", path);
  else history.pushState(null, "", path);
  setLocation({ pathname: window.location.pathname, hash: window.location.hash });
  window.scrollTo(0, 0);
}

/** Goes back when the previous page is this client's, else to the list. */
export function back(): void {
  if (history.state?.inApp || document.referrer.startsWith(window.location.origin)) history.back();
  else navigate("/", { replace: true });
}
