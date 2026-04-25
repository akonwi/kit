import { homedir } from "node:os";
import path from "node:path";

export type KitPaths = {
	home: string;
	kitRoot: string;
	settingsPath: string;
	notificationConfigPath: string;
	themesDir: string;
};

let _cached: KitPaths | null = null;

export function getKitPaths(home = homedir()): KitPaths {
	if (_cached && _cached.home === home) return _cached;
	const kitRoot = path.join(home, ".kit");
	_cached = {
		home,
		kitRoot,
		settingsPath: path.join(kitRoot, "settings.json"),
		notificationConfigPath: path.join(kitRoot, "notifications.json"),
		themesDir: path.join(kitRoot, "themes"),
	};
	return _cached;
}
