import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { SESSIONS_DIR } from "../../session";
import { scratchpadPath } from "../../storage/session-sidecars";

export { scratchpadPath };

export function readScratchpad(sessionId: string): string {
	const filePath = scratchpadPath(sessionId);
	if (!existsSync(filePath)) return "";
	try {
		return readFileSync(filePath, "utf8");
	} catch {
		return "";
	}
}

export function writeScratchpad(sessionId: string, content: string): void {
	mkdirSync(SESSIONS_DIR, { recursive: true });
	writeFileSync(scratchpadPath(sessionId), content, "utf8");
}
