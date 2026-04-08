import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { getKitPaths } from "../paths";

export type NotificationConfig = {
	bells: { enabled: boolean };
	speech: { enabled: boolean; maxChars: number; voice: string | null };
};

const CONFIG_PATH = getKitPaths().notificationConfigPath;

const DEFAULTS: NotificationConfig = {
	bells: { enabled: true },
	speech: { enabled: true, maxChars: 220, voice: null },
};

function isRecord(v: unknown): v is Record<string, unknown> {
	return v !== null && typeof v === "object" && !Array.isArray(v);
}

function sanitizeNotificationConfig(raw: unknown): NotificationConfig {
	if (!isRecord(raw)) {
		return {
			bells: { ...DEFAULTS.bells },
			speech: { ...DEFAULTS.speech },
		};
	}
	const bells = isRecord(raw.bells) ? raw.bells : {};
	const speech = isRecord(raw.speech) ? raw.speech : {};
	return {
		bells: {
			enabled:
				typeof bells.enabled === "boolean"
					? bells.enabled
					: DEFAULTS.bells.enabled,
		},
		speech: {
			enabled:
				typeof speech.enabled === "boolean"
					? speech.enabled
					: DEFAULTS.speech.enabled,
			maxChars:
				typeof speech.maxChars === "number"
					? speech.maxChars
					: DEFAULTS.speech.maxChars,
			voice:
				typeof speech.voice === "string" ? speech.voice : DEFAULTS.speech.voice,
		},
	};
}

export async function loadNotificationConfig(): Promise<NotificationConfig> {
	try {
		return sanitizeNotificationConfig(
			JSON.parse(await readFile(CONFIG_PATH, "utf8")),
		);
	} catch {
		return sanitizeNotificationConfig(DEFAULTS);
	}
}

export function loadNotificationConfigSync(): NotificationConfig {
	try {
		return sanitizeNotificationConfig(
			JSON.parse(readFileSync(CONFIG_PATH, "utf8")),
		);
	} catch {
		return sanitizeNotificationConfig(DEFAULTS);
	}
}

export async function saveNotificationConfig(
	config: NotificationConfig,
): Promise<void> {
	const dir = path.dirname(CONFIG_PATH);
	if (!existsSync(dir)) await mkdir(dir, { recursive: true });
	await writeFile(
		CONFIG_PATH,
		JSON.stringify(sanitizeNotificationConfig(config), null, 2),
	);
}

export function saveNotificationConfigSync(config: NotificationConfig): void {
	const dir = path.dirname(CONFIG_PATH);
	if (!existsSync(dir)) mkdirSync(dir, { recursive: true });
	writeFileSync(
		CONFIG_PATH,
		JSON.stringify(sanitizeNotificationConfig(config), null, 2),
	);
}
