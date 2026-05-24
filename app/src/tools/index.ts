import type { AgentTool } from "../runtime/agent";
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
// biome-ignore lint/suspicious/noExplicitAny: heterogeneous tool collection, matches pi-core convention
export function createDefaultTools(cwd: string): AgentTool<any>[] {
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
