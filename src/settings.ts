import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { getKitPaths, type KitPaths } from "./paths";

export type ReviewDiffView = "unified" | "split";

export type KeybindingValue = string | string[] | false | null;
export type KeybindingSettings = Record<string, KeybindingValue>;

export type Settings = {
	/** Theme name: "system" (default) or a custom theme from ~/.kit/themes/ */
	theme?: string;
	/** Enable terminal bell on turn complete */
	bells?: boolean;
	/** User keybinding overrides by Kit command id. Use false/null to disable. */
	keybindings?: KeybindingSettings;
	/** Speech settings - can be a boolean or object with options */
	speech?:
		| boolean
		| {
				enabled?: boolean;
				maxChars?: number;
				voice?: string;
		  };
	/** Auto-open the pager for long assistant responses */
	pager?: boolean;
	/** Expose the guided_questions tool to the agent */
	guidedQuestions?: boolean;
	/** Auto-generate a session title after the first couple of turns */
	sessionNaming?: boolean;
	/** Default diff rendering settings */
	diffs?: {
		view?: ReviewDiffView;
	};
	/** Retry settings for transient model/provider errors */
	retry?: {
		enabled?: boolean;
		maxRetries?: number;
		baseDelayMs?: number;
		maxDelayMs?: number;
	};
};

export type ResolvedSpeechSettings = {
	enabled: boolean;
	maxChars: number;
	voice?: string;
};

export type ResolvedRetrySettings = {
	enabled: boolean;
	maxRetries: number;
	baseDelayMs: number;
	maxDelayMs: number;
};

export type ResolvedDiffSettings = {
	view: ReviewDiffView;
};

const DEFAULTS: Settings = {
	bells: true,
	speech: { enabled: true, maxChars: 220 },
	pager: true,
	guidedQuestions: true,
	sessionNaming: true,
	diffs: { view: "unified" },
	retry: {
		enabled: true,
		maxRetries: 3,
		baseDelayMs: 2000,
		maxDelayMs: 60000,
	},
};

function isRecord(v: unknown): v is Record<string, unknown> {
	return v !== null && typeof v === "object" && !Array.isArray(v);
}

function defaultSpeechObject(): ResolvedSpeechSettings {
	return { enabled: true, maxChars: 220 };
}

function defaultRetryObject(): ResolvedRetrySettings {
	return { enabled: true, maxRetries: 3, baseDelayMs: 2000, maxDelayMs: 60000 };
}

function defaultDiffObject(): ResolvedDiffSettings {
	return { view: "unified" };
}

export function resolveSpeechSettings(
	speech: Settings["speech"],
): ResolvedSpeechSettings {
	if (typeof speech === "boolean") {
		return { enabled: speech, maxChars: 220 };
	}
	if (speech && typeof speech === "object") {
		return {
			enabled: speech.enabled ?? true,
			maxChars: speech.maxChars ?? 220,
			...(speech.voice ? { voice: speech.voice } : {}),
		};
	}
	return defaultSpeechObject();
}

export function resolveRetrySettings(
	retry: Settings["retry"],
): ResolvedRetrySettings {
	if (retry && typeof retry === "object") {
		return {
			enabled: retry.enabled ?? true,
			maxRetries: typeof retry.maxRetries === "number" ? retry.maxRetries : 3,
			baseDelayMs:
				typeof retry.baseDelayMs === "number" ? retry.baseDelayMs : 2000,
			maxDelayMs:
				typeof retry.maxDelayMs === "number" ? retry.maxDelayMs : 60000,
		};
	}
	return defaultRetryObject();
}

export function resolveDiffSettings(
	diffs: Settings["diffs"],
): ResolvedDiffSettings {
	if (diffs && typeof diffs === "object") {
		return {
			view: diffs.view === "split" ? "split" : "unified",
		};
	}
	return defaultDiffObject();
}

function sanitizeKeybindings(value: unknown): KeybindingSettings | undefined {
	if (!isRecord(value)) return undefined;
	const keybindings: KeybindingSettings = {};
	for (const [command, binding] of Object.entries(value)) {
		const name = command.trim();
		if (!name) continue;
		if (typeof binding === "string") {
			keybindings[name] = binding;
			continue;
		}
		if (binding === false || binding === null) {
			keybindings[name] = binding;
			continue;
		}
		if (Array.isArray(binding)) {
			const keys = binding.filter(
				(entry): entry is string => typeof entry === "string",
			);
			if (keys.length > 0) keybindings[name] = keys;
		}
	}
	return Object.keys(keybindings).length > 0 ? keybindings : undefined;
}

export function sanitizeSettings(raw: unknown): Settings {
	if (!isRecord(raw)) {
		return { ...DEFAULTS };
	}

	const theme = typeof raw.theme === "string" ? raw.theme : undefined;
	const bells = typeof raw.bells === "boolean" ? raw.bells : DEFAULTS.bells;
	const keybindings = sanitizeKeybindings(raw.keybindings);
	const pager = typeof raw.pager === "boolean" ? raw.pager : DEFAULTS.pager;
	const guidedQuestions =
		typeof raw.guidedQuestions === "boolean"
			? raw.guidedQuestions
			: DEFAULTS.guidedQuestions;
	const sessionNaming =
		typeof raw.sessionNaming === "boolean"
			? raw.sessionNaming
			: DEFAULTS.sessionNaming;
	const diffs: Settings["diffs"] = isRecord(raw.diffs)
		? {
				view: raw.diffs.view === "split" ? "split" : "unified",
			}
		: defaultDiffObject();

	const retry = isRecord(raw.retry)
		? {
				enabled:
					typeof raw.retry.enabled === "boolean" ? raw.retry.enabled : true,
				maxRetries:
					typeof raw.retry.maxRetries === "number" ? raw.retry.maxRetries : 3,
				baseDelayMs:
					typeof raw.retry.baseDelayMs === "number"
						? raw.retry.baseDelayMs
						: 2000,
				maxDelayMs:
					typeof raw.retry.maxDelayMs === "number"
						? raw.retry.maxDelayMs
						: 60000,
			}
		: defaultRetryObject();

	let speech: Settings["speech"];
	if (typeof raw.speech === "boolean") {
		speech = raw.speech;
	} else if (isRecord(raw.speech)) {
		const rawSpeech = raw.speech;
		speech = {
			enabled:
				typeof rawSpeech.enabled === "boolean" ? rawSpeech.enabled : true,
			maxChars:
				typeof rawSpeech.maxChars === "number" ? rawSpeech.maxChars : 220,
			...(typeof rawSpeech.voice === "string"
				? { voice: rawSpeech.voice }
				: {}),
		};
	} else {
		speech = defaultSpeechObject();
	}

	return {
		theme,
		bells,
		...(keybindings ? { keybindings } : {}),
		speech,
		pager,
		guidedQuestions,
		sessionNaming,
		diffs,
		retry,
	};
}

export type LoadedSettings = {
	settings: Settings;
	paths: KitPaths;
};

export async function loadSettings(): Promise<LoadedSettings> {
	const paths = getKitPaths();
	try {
		const content = await readFile(paths.settingsPath, "utf8");
		const parsed = JSON.parse(content) as unknown;
		const settings = sanitizeSettings(parsed);
		return { settings, paths };
	} catch {
		return { settings: { ...DEFAULTS }, paths };
	}
}

export function loadSettingsSync(): LoadedSettings {
	const paths = getKitPaths();
	try {
		const content = readFileSync(paths.settingsPath, "utf8");
		const parsed = JSON.parse(content) as unknown;
		const settings = sanitizeSettings(parsed);
		return { settings, paths };
	} catch {
		return { settings: { ...DEFAULTS }, paths };
	}
}

export async function saveSettings(settings: Settings): Promise<void> {
	const paths = getKitPaths();
	const dir = path.dirname(paths.settingsPath);
	if (!existsSync(dir)) await mkdir(dir, { recursive: true });
	await writeFile(paths.settingsPath, JSON.stringify(settings, null, 2));
}

export function saveSettingsSync(settings: Settings): void {
	const paths = getKitPaths();
	const dir = path.dirname(paths.settingsPath);
	if (!existsSync(dir)) mkdirSync(dir, { recursive: true });
	writeFileSync(paths.settingsPath, JSON.stringify(settings, null, 2));
}
