import { existsSync, realpathSync } from "node:fs";
import path from "node:path";

export function getInstalledRuntimeDir(): string | null {
	try {
		const execDir = path.dirname(realpathSync(process.execPath));
		const runtimeDir = path.join(execDir, "runtime");
		return existsSync(runtimeDir) ? runtimeDir : null;
	} catch {
		return null;
	}
}
