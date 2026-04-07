import type { AgentTool } from "@mariozechner/pi-agent-core";
import { createBashTool } from "./bash";
import { createEditTool } from "./edit";
import { createFindTool } from "./find";
import { createGrepTool } from "./grep";
import { createLsTool } from "./ls";
import { createReadTool } from "./read";
import { createWriteTool } from "./write";

export { createBashTool } from "./bash";
export { createEditTool } from "./edit";
export { createFindTool } from "./find";
export { createGrepTool } from "./grep";
export { createLsTool } from "./ls";
export { createReadTool } from "./read";
export { createWriteTool } from "./write";

/** Create the standard coding tool suite for a given working directory. */
export function createDefaultTools(cwd: string): AgentTool[] {
	return [
		createBashTool(cwd),
		createReadTool(cwd),
		createWriteTool(cwd),
		createEditTool(cwd),
		createLsTool(cwd),
		createGrepTool(cwd),
		createFindTool(cwd),
	];
}
