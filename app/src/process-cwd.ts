import { statSync } from "node:fs";
import { homedir } from "node:os";

function isDirectory(path: string | undefined): path is string {
	if (!path) return false;
	try {
		return statSync(path).isDirectory();
	} catch {
		return false;
	}
}

export function safeProcessCwd(): string {
	try {
		const cwd = process.cwd();
		if (isDirectory(cwd)) return cwd;
	} catch {
		// Fall through to environment/home fallbacks.
	}
	if (isDirectory(process.env.PWD)) return process.env.PWD;
	return homedir();
}

export function chdirIfNeeded(targetCwd: string): void {
	try {
		const cwd = process.cwd();
		if (cwd === targetCwd) {
			process.env.PWD = cwd;
			return;
		}
	} catch {
		// The current process cwd may have been deleted. chdir with an absolute
		// target still lets us recover, so continue to process.chdir below.
	}
	process.chdir(targetCwd);
	process.env.PWD = process.cwd();
}
