// The connection to the server that served this page: the device credential,
// the handshake, and recovery when the credential or the server changes.
import { createContext, createSignal, type JSX, useContext } from "solid-js";
import { Api, ApiError } from "../api/client";
import { type Health, PROTOCOL_VERSION } from "../api/types";
import { type Credential, clearCredential, loadCredential, saveCredential } from "../auth/credential";
import { shouldReload } from "../auth/reload";
import { forgetDrafts } from "../scratchpad/pad";

/** Why this browser shows the pairing page. */
export type UnpairedReason = "none" | "signedOut" | "revoked";

export type Phase =
  | { kind: "unpaired"; reason: UnpairedReason }
  | { kind: "connecting" }
  | { kind: "ready"; api: Api; credential: Credential; health: Health }
  | { kind: "unreachable"; message: string }
  | { kind: "incompatible"; message: string };

export interface Kit {
  phase: () => Phase;
  /** The server's host, as this browser reaches it. */
  host: string;
  connect(): Promise<void>;
  /** Stores a credential from pairing and connects with it. */
  paired(credential: Credential): Promise<void>;
  signOut(): Promise<void>;
  /** Handles a failure any page saw; returns true when the app took over. */
  fail(error: unknown): boolean;
}

const KitContext = createContext<Kit>();

export function useKit(): Kit {
  const kit = useContext(KitContext);
  if (!kit) throw new Error("useKit outside KitProvider");
  return kit;
}

export function useReady(): Extract<Phase, { kind: "ready" }> {
  const phase = useKit().phase();
  if (phase.kind !== "ready") throw new Error("The connection isn't ready.");
  return phase;
}

export function KitProvider(props: { children: JSX.Element }) {
  const [phase, setPhase] = createSignal<Phase>({ kind: "connecting" });

  const incompatible = () =>
    setPhase({
      kind: "incompatible",
      message:
        "This page and Kit's server don't match, and reloading didn't fix it. Reload the page to try again.",
    });

  /** Reloads once for the server instance now answering, else gives up. */
  const recover = async () => {
    try {
      const health = await new Api(token()).handshake();
      if (shouldReload(health.instanceId)) {
        window.location.reload();
        return;
      }
    } catch {
      // A server that can't be asked keeps the page as it is.
    }
    incompatible();
  };

  let current: Credential | undefined;
  const token = () => current?.token ?? "";
  // Each connection attempt has a generation. Pairing, unpairing, or a newer
  // attempt makes an older one stale, and a stale attempt changes nothing.
  let generation = 0;

  const unpair = (reason: UnpairedReason) => {
    generation += 1;
    clearCredential();
    // Unsaved scratchpad drafts are the session's text; they leave with the pairing.
    forgetDrafts();
    current = undefined;
    setPhase({ kind: "unpaired", reason });
  };

  const kit: Kit = {
    phase,
    host: window.location.host,
    async connect() {
      generation += 1;
      const attempt = generation;
      const credential = loadCredential();
      current = credential;
      if (!credential) {
        setPhase({ kind: "unpaired", reason: "none" });
        return;
      }
      setPhase({ kind: "connecting" });
      const api = new Api(credential.token);
      try {
        const health = await api.handshake();
        if (attempt !== generation) return;
        if (health.protocolVersion !== PROTOCOL_VERSION) {
          if (shouldReload(health.instanceId)) window.location.reload();
          else incompatible();
          return;
        }
        setPhase({ kind: "ready", api, credential, health });
      } catch (error) {
        if (attempt !== generation) return;
        if (!kit.fail(error)) {
          setPhase({
            kind: "unreachable",
            message: error instanceof Error ? error.message : "Kit's server can't be reached.",
          });
        }
      }
    },
    async paired(credential) {
      saveCredential(credential);
      await kit.connect();
    },
    async signOut() {
      const state = phase();
      if (state.kind === "ready") {
        try {
          await state.api.revokeDevice(state.credential.deviceId);
        } catch (error) {
          // A device the server already forgot is signed out all the same.
          if (!(error instanceof ApiError && (error.unauthorized || error.status === 404))) throw error;
        }
      }
      unpair("signedOut");
    },
    fail(error) {
      if (error instanceof ApiError) {
        if (error.unauthorized) {
          unpair("revoked");
          return true;
        }
        if (error.mismatch) {
          void recover();
          return true;
        }
      }
      return false;
    },
  };

  void kit.connect();

  return <KitContext.Provider value={kit}>{props.children}</KitContext.Provider>;
}
