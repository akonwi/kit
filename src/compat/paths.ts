import { homedir } from "node:os";
import path from "node:path";

/**
 * Resolved storage paths for the pi-kit app.
 *
 * ## Layout
 *
 * ### Pi compatibility root (`~/.pi/agent/`)
 * Read/write for Pi-compatible state:
 * - `sessions/`   — session .jsonl files (shared with Pi)
 * - `auth.json`   — API key credentials (shared with Pi)
 * - `agents/`     — user-level agent definitions
 * - `settings.json` — Pi settings (fallback when pi-kit settings absent)
 *
 * ### Pi-kit app root (`~/.pi-kit/`)
 * App-native config and state:
 * - `settings.json`     — pi-kit settings (takes precedence over Pi settings)
 * - `notifications.json` — bell/speech preferences
 *
 * ### Precedence
 * 1. `~/.pi-kit/settings.json`
 * 2. `~/.pi/agent/settings.json` (fallback)
 * 3. Built-in defaults
 */
export type PiKitPaths = {
  home: string;
  /** Pi compatibility root: `~/.pi/agent` */
  piAgentRoot: string;
  /** Pi-kit app root: `~/.pi-kit` */
  piKitRoot: string;
  /** Pi settings (fallback): `~/.pi/agent/settings.json` */
  piSettingsPath: string;
  /** Pi-kit settings (primary): `~/.pi-kit/settings.json` */
  piKitSettingsPath: string;
  /** Notification config: `~/.pi-kit/notifications.json` */
  notificationConfigPath: string;
};

let _cached: PiKitPaths | null = null;

export function getPiKitPaths(home = homedir()): PiKitPaths {
  if (_cached && _cached.home === home) return _cached;

  const piAgentRoot = path.join(home, ".pi", "agent");
  const piKitRoot = path.join(home, ".pi-kit");

  _cached = {
    home,
    piAgentRoot,
    piKitRoot,
    piSettingsPath: path.join(piAgentRoot, "settings.json"),
    piKitSettingsPath: path.join(piKitRoot, "settings.json"),
    notificationConfigPath: path.join(piKitRoot, "notifications.json"),
  };
  return _cached;
}
