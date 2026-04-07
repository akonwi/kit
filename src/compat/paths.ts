import { homedir } from "node:os";
import path from "node:path";

/**
 * Resolved storage paths for the kit app.
 *
 * ## Layout
 *
 * ### Pi compatibility root (`~/.pi/agent/`)
 * Read/write for Pi-compatible state:
 * - `sessions/`   — session .jsonl files (shared with Pi)
 * - `auth.json`   — API key credentials (shared with Pi)
 * - `agents/`     — user-level agent definitions
 * - `settings.json` — Pi settings (fallback when kit settings absent)
 *
 * ### Pi-kit app root (`~/.kit/`)
 * App-native config and state:
 * - `settings.json`     — kit settings (takes precedence over Pi settings)
 * - `notifications.json` — bell/speech preferences
 *
 * ### Precedence
 * 1. `~/.kit/settings.json`
 * 2. `~/.pi/agent/settings.json` (fallback)
 * 3. Built-in defaults
 */
export type PiKitPaths = {
  home: string;
  /** Pi compatibility root: `~/.pi/agent` */
  piAgentRoot: string;
  /** Pi-kit app root: `~/.kit` */
  kitRoot: string;
  /** Pi settings (fallback): `~/.pi/agent/settings.json` */
  piSettingsPath: string;
  /** Pi-kit settings (primary): `~/.kit/settings.json` */
  kitSettingsPath: string;
  /** Notification config: `~/.kit/notifications.json` */
  notificationConfigPath: string;
};

let _cached: PiKitPaths | null = null;

export function getPiKitPaths(home = homedir()): PiKitPaths {
  if (_cached && _cached.home === home) return _cached;

  const piAgentRoot = path.join(home, ".pi", "agent");
  const kitRoot = path.join(home, ".kit");

  _cached = {
    home,
    piAgentRoot,
    kitRoot,
    piSettingsPath: path.join(piAgentRoot, "settings.json"),
    kitSettingsPath: path.join(kitRoot, "settings.json"),
    notificationConfigPath: path.join(kitRoot, "notifications.json"),
  };
  return _cached;
}
