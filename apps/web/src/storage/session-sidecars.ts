import path from "node:path";
import { getKitPaths } from "../paths";

const SESSIONS_DIR = path.join(getKitPaths().kitRoot, "sessions");

export function scratchpadPath(sessionId: string): string {
	return path.join(SESSIONS_DIR, `${sessionId}.scratchpad.md`);
}
