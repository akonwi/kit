// Pairing: redeeming the link a paired device or the server made, or saying
// how to get one.
import { createSignal, type JSX, Match, Show, Switch } from "solid-js";
import { Api, ApiError } from "../api/client";
import { type UnpairedReason, useKit } from "../app/kit";
import { navigate } from "../app/router";
import { defaultDeviceName } from "../auth/credential";
import { Icon, Logo } from "../ui/icons";

function HostLine() {
  return (
    <div class="host-line">
      <Show when={window.location.protocol === "https:"}>
        <Icon name="lock" size={14} class="ok" />
      </Show>
      <span>{useKit().host}</span>
    </div>
  );
}

function Notice(props: {
  icon: "alert" | "signout";
  tone: "danger" | "muted";
  title: string;
  children: JSX.Element;
}) {
  return (
    <div class="notice" role="status">
      <Icon name={props.icon} size={16} class={props.tone} />
      <div>
        <strong>{props.title}</strong>
        <p>{props.children}</p>
      </div>
    </div>
  );
}

/** How to pair, with a notice saying why this browser isn't paired. */
export function NotPaired(props: { reason?: UnpairedReason; refused?: boolean }) {
  return (
    <main class="pairing">
      <div class="pairing-title">
        <Logo height={30} />
        <h1>Pair this browser</h1>
        <p class="muted">
          Get a pairing link from the server, or from a device that’s already paired with it.
        </p>
        <HostLine />
      </div>
      <Switch>
        <Match when={props.refused}>
          <Notice icon="alert" tone="danger" title="That pairing link didn’t work">
            It was already used or has expired. Links last 5 minutes and work once. Make a new one.
          </Notice>
        </Match>
        <Match when={props.reason === "revoked"}>
          <Notice icon="signout" tone="muted" title="This browser was signed out">
            Its device was revoked or expired. Pair it again to continue.
          </Notice>
        </Match>
        <Match when={props.reason === "signedOut"}>
          <Notice icon="signout" tone="muted" title="Signed out">
            Signing out revoked this browser’s device. Pair it again to continue.
          </Notice>
        </Match>
      </Switch>
      <ol class="steps">
        <li>
          <span>
            Run <code>kit server pair</code> on the server, or choose Pair a Device in Kit’s server settings.
          </span>
        </li>
        <li>
          <span>Scan its QR code with this phone, or open the link here.</span>
        </li>
        <li>
          <span>Name this browser and choose Pair.</span>
        </li>
      </ol>
    </main>
  );
}

/** The page a pairing link opens: name this browser, then redeem the code. */
export function PairingPage(props: { code: string }) {
  const kit = useKit();
  // The code works once; keep it out of the address bar and history.
  const code = props.code;
  history.replaceState(null, "", "/pair");
  const [name, setName] = createSignal(defaultDeviceName());
  const [pending, setPending] = createSignal(false);
  const [refused, setRefused] = createSignal(!code);
  const [error, setError] = createSignal("");

  const pair = async (event: SubmitEvent) => {
    event.preventDefault();
    const deviceName = name().trim();
    if (!deviceName || pending()) return;
    if (new TextEncoder().encode(deviceName).length > 64) {
      setError("That name is too long. Use a shorter one.");
      return;
    }
    setPending(true);
    setError("");
    try {
      const result = await Api.redeemPairing(code, deviceName);
      await kit.paired({
        token: result.token,
        deviceId: result.device.id,
        deviceName: result.device.name,
        pairedAt: result.device.createdAt,
      });
      navigate("/", { replace: true });
    } catch (failure) {
      if (failure instanceof ApiError && failure.status === 400 && /device name/i.test(failure.message)) {
        setError("The server can’t use that name. Try another.");
      } else if (failure instanceof ApiError && [400, 401, 403, 404, 410].includes(failure.status)) {
        setRefused(true);
      } else {
        setError(failure instanceof Error ? failure.message : "Pairing failed.");
      }
    } finally {
      setPending(false);
    }
  };

  return (
    <Show when={!refused()} fallback={<NotPaired refused={Boolean(code)} />}>
      <main class="pairing">
        <div class="pairing-title">
          <Logo height={30} />
          <h1>Pair with this server</h1>
          <HostLine />
        </div>
        <form class="pairing-form" onSubmit={pair}>
          <div class="field">
            <label for="device-name">Name this browser</label>
            <input
              id="device-name"
              name="device-name"
              value={name()}
              onInput={(event) => setName(event.currentTarget.value)}
              maxLength={64}
              autocomplete="off"
              required
            />
            <p class="hint">
              The server lists this browser under that name in <code>kit server devices</code>, where you can
              revoke it.
            </p>
          </div>
          <Show when={error()}>
            <p class="error" role="alert">
              {error()}
            </p>
          </Show>
          <div class="actions">
            <button type="submit" data-variant="primary" disabled={pending() || !name().trim()}>
              {pending() ? "Pairing…" : "Pair"}
            </button>
            <p class="hint center">
              The browser keeps its key in this site’s storage. Signing out revokes it.
            </p>
          </div>
        </form>
      </main>
    </Show>
  );
}
