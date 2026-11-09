// The app: pairing until this browser has a credential, then the session
// list and sessions.
import { createMemo, ErrorBoundary, Match, Switch } from "solid-js";
import { NotPaired, PairingPage } from "../pages/Pairing";
import { SessionPage } from "../pages/Session";
import { SessionsPage } from "../pages/Sessions";
import { Logo, Spinner } from "../ui/icons";
import { KitProvider, useKit } from "./kit";
import { route } from "./router";

export function App() {
  return (
    <KitProvider>
      <Routes />
    </KitProvider>
  );
}

function Routes() {
  const kit = useKit();
  const current = route;
  const sessionId = createMemo(() => {
    const value = current();
    return value.page === "session" ? value.id : undefined;
  });
  return (
    <Switch>
      <Match when={current().page === "pair" ? current() : undefined} keyed>
        {(pair) => <PairingPage code={pair.page === "pair" ? pair.code : ""} />}
      </Match>
      <Match when={kit.phase().kind === "unpaired" ? kit.phase() : undefined} keyed>
        {(phase) => <NotPaired reason={phase.kind === "unpaired" ? phase.reason : "none"} />}
      </Match>
      <Match when={kit.phase().kind === "connecting"}>
        <main class="splash" aria-busy="true">
          <Logo height={30} />
          <Spinner size={18} />
        </main>
      </Match>
      <Match when={kit.phase().kind === "unreachable" || kit.phase().kind === "incompatible"}>
        <main class="splash">
          <Logo height={30} />
          <p class="muted">
            {(() => {
              const phase = kit.phase();
              return phase.kind === "unreachable" || phase.kind === "incompatible" ? phase.message : "";
            })()}
          </p>
          <button
            type="button"
            onClick={() => (kit.phase().kind === "incompatible" ? location.reload() : kit.connect())}
          >
            Try again
          </button>
        </main>
      </Match>
      <Match when={kit.phase().kind === "ready"}>
        {/* A page that fails to render says so, rather than leaving the
            splash up with nothing to explain it. */}
        <ErrorBoundary
          fallback={(error) => (
            <main class="splash" role="alert">
              <Logo height={30} />
              <p class="muted">
                This page couldn’t be shown. {error instanceof Error ? error.message : String(error)}
              </p>
              <button type="button" onClick={() => location.reload()}>
                Reload
              </button>
            </main>
          )}
        >
          <Switch>
            {/* Keyed by the session alone: #changes on the same session keeps its page and draft. */}
            <Match when={sessionId()} keyed>
              {(id) => <SessionPage id={id} />}
            </Match>
            <Match when={true}>
              <SessionsPage />
            </Match>
          </Switch>
        </ErrorBoundary>
      </Match>
    </Switch>
  );
}
