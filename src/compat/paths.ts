import { homedir } from "node:os";
import path from "node:path";

export type PiKitPaths = {
  home: string;
  piAgentRoot: string;
  piKitRoot: string;
  piSettingsPath: string;
  piKitSettingsPath: string;
};

export function getPiKitPaths(home = homedir()): PiKitPaths {
  const piAgentRoot = path.join(home, ".pi", "agent");
  const piKitRoot = path.join(home, ".pi-kit");

  return {
    home,
    piAgentRoot,
    piKitRoot,
    piSettingsPath: path.join(piAgentRoot, "settings.json"),
    piKitSettingsPath: path.join(piKitRoot, "settings.json"),
  };
}
