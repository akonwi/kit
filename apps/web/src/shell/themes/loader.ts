import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { getKitPaths } from "../../paths";
import type { ThemeDefinition } from "./types";

/**
 * Load a user-defined theme from ~/.kit/themes/{name}.json.
 * Returns null if the file doesn't exist or can't be parsed.
 */
export async function loadUserTheme(
	name: string,
): Promise<ThemeDefinition | null> {
	const themePath = path.join(getKitPaths().themesDir, `${name}.json`);
	try {
		const content = await readFile(themePath, "utf8");
		return JSON.parse(content) as ThemeDefinition;
	} catch {
		return null;
	}
}

/**
 * List user-defined theme names from ~/.kit/themes/.
 * Returns theme names (without .json extension).
 */
export async function listUserThemes(): Promise<string[]> {
	try {
		const entries = await readdir(getKitPaths().themesDir);
		return entries
			.filter((f) => f.endsWith(".json"))
			.map((f) => f.replace(/\.json$/, ""));
	} catch {
		return [];
	}
}
