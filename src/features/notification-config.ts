import { existsSync } from "node:fs";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { getPiKitPaths } from "../compat/paths";

export type NotificationConfig = {
	bells: { enabled: boolean };
	speech: { enabled: boolean; maxChars: number; voice: string | null };
};

const CONFIG_PATH = getPiKitPaths().notificationConfigPath;

const DEFAULTS: NotificationConfig = {
	bells: { enabled: true },
	speech: { enabled: true, maxChars: 220, voice: null },
};

function isRecord(v: unknown): v is Record<string, unknown> {
	return v !== null && typeof v === "object" && !Array.isArray(v);
}

export async function loadNotificationConfig(): Promise<NotificationConfig> {
	try {
		const raw = JSON.parse(await readFile(CONFIG_PATH, "utf8"));
		if (!isRecord(raw)) return { ...DEFAULTS };
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
					typeof speech.voice === "string"
						? speech.voice
						: DEFAULTS.speech.voice,
			},
		};
	} catch {
		return { ...DEFAULTS };
	}
}

export async function saveNotificationConfig(
	config: NotificationConfig,
): Promise<void> {
	const dir = path.dirname(CONFIG_PATH);
	if (!existsSync(dir)) await mkdir(dir, { recursive: true });
	await writeFile(CONFIG_PATH, JSON.stringify(config, null, 2));
}
