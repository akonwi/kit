import { readFile } from "node:fs/promises";
import { getPiKitPaths, type PiKitPaths } from "../paths";

export type SettingsSource = "kit" | "pi" | "defaults";

export type LoadedSettings = {
	source: SettingsSource;
	values: Record<string, unknown>;
	paths: PiKitPaths;
};

async function tryReadJson(
	filePath: string,
): Promise<Record<string, unknown> | null> {
	try {
		const content = await readFile(filePath, "utf8");
		const parsed = JSON.parse(content) as unknown;
		return parsed && typeof parsed === "object"
			? (parsed as Record<string, unknown>)
			: {};
	} catch {
		return null;
	}
}

export async function loadSettings(): Promise<LoadedSettings> {
	const paths = getPiKitPaths();

	const kitSettings = await tryReadJson(paths.kitSettingsPath);
	if (kitSettings) {
		return {
			source: "kit",
			values: kitSettings,
			paths,
		};
	}

	const piSettings = await tryReadJson(paths.piSettingsPath);
	if (piSettings) {
		return {
			source: "pi",
			values: piSettings,
			paths,
		};
	}

	return {
		source: "defaults",
		values: {},
		paths,
	};
}
