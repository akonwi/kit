// The device credential this browser keeps for the server that served it.
// localStorage is scoped to the origin, which is the server (ADR 0059).

export interface Credential {
  token: string;
  deviceId: string;
  deviceName: string;
  /** When this browser paired, as the server reported it. */
  pairedAt: string;
}

const KEY = "kit.device";

export function loadCredential(storage: Storage = localStorage): Credential | undefined {
  try {
    const value = JSON.parse(storage.getItem(KEY) ?? "null") as Partial<Credential> | null;
    if (
      value &&
      typeof value.token === "string" &&
      value.token &&
      typeof value.deviceId === "string" &&
      typeof value.deviceName === "string" &&
      typeof value.pairedAt === "string"
    ) {
      return value as Credential;
    }
  } catch {
    // A malformed record is no credential.
  }
  return undefined;
}

export function saveCredential(credential: Credential, storage: Storage = localStorage): void {
  storage.setItem(KEY, JSON.stringify(credential));
}

export function clearCredential(storage: Storage = localStorage): void {
  storage.removeItem(KEY);
}

/** A name for this browser that says which browser and system it is. */
export function defaultDeviceName(userAgent: string = navigator.userAgent): string {
  const browser = /Edg\//.test(userAgent)
    ? "Edge"
    : /Firefox\//.test(userAgent)
      ? "Firefox"
      : /SamsungBrowser\//.test(userAgent)
        ? "Samsung Internet"
        : /Chrome\//.test(userAgent)
          ? "Chrome"
          : /Safari\//.test(userAgent)
            ? "Safari"
            : "Browser";
  const system = /Android/.test(userAgent)
    ? "Android"
    : /iPhone|iPad/.test(userAgent)
      ? "iOS"
      : /Mac OS X/.test(userAgent)
        ? "Mac"
        : /Windows/.test(userAgent)
          ? "Windows"
          : /CrOS/.test(userAgent)
            ? "ChromeOS"
            : /Linux/.test(userAgent)
              ? "Linux"
              : "";
  return system ? `${browser} on ${system}` : browser;
}
