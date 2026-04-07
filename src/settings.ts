import { readFile } from "node:fs/promises";
import { getKitPaths, type KitPaths } from "./paths";

export type LoadedSettings = {
	values: Record<string, unknown>;
	paths: KitPaths;
};

export async function loadSettings(): Promise<LoadedSettings> {
	const paths = getKitPaths();
	try {
		const content = await readFile(paths.settingsPath, "utf8");
		const parsed = JSON.parse(content) as unknown;
		const values =
			parsed && typeof parsed === "object"
				? (parsed as Record<string, unknown>)
				: {};
		return { values, paths };
	} catch {
		return { values: {}, paths };
	}
}
