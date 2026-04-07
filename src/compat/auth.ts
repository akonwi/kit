/**
 * Read API credentials from ~/.pi/agent/auth.json.
 *
 * Temporary compatibility shim while we build our own auth system.
 * pi-ai accepts OAuth access tokens directly alongside regular API keys.
 */

import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { getEnvApiKey } from "@mariozechner/pi-ai";

const AUTH_PATH = join(homedir(), ".pi", "agent", "auth.json");

interface AuthEntry {
  type: "api_key" | "oauth";
  key?: string;
  access?: string;
  refresh?: string;
  expires?: number;
}

type AuthFile = Record<string, AuthEntry>;

function readAuthFile(): AuthFile {
  try {
    return JSON.parse(readFileSync(AUTH_PATH, "utf8")) as AuthFile;
  } catch {
    return {};
  }
}

/** Map pi provider names to pi-ai provider identifiers */
const PROVIDER_MAP: Record<string, string> = {
  anthropic: "anthropic",
  openai: "openai",
  google: "google",
};

export function getApiKey(provider: string): string | undefined {
  // Env vars take precedence
  const envKey = getEnvApiKey(provider);
  if (envKey) return envKey;

  // Fall back to ~/.pi/agent/auth.json
  const auth = readAuthFile();
  const piProvider = PROVIDER_MAP[provider] ?? provider;
  const entry = auth[piProvider];
  if (!entry) return undefined;

  if (entry.type === "api_key" && entry.key) return entry.key;
  if (entry.type === "oauth" && entry.access) return entry.access;

  return undefined;
}
