import type { AgentTool } from "../runtime/agent";
import { createBashTool } from "./bash";
import { createEditTool } from "./edit";
import type { FileOperations } from "./file-operations";
import { createFindTool } from "./find";
import { createGrepTool } from "./grep";
import { createLsTool } from "./ls";
import { createReadTool } from "./read";
import { createWriteTool } from "./write";

export { createBashTool } from "./bash";
export { createEditTool } from "./edit";
export {
	defaultFileOperations,
	type FileOperationHandler,
	type FileOperations,
} from "./file-operations";
export { createFindTool } from "./find";
export { createGrepTool } from "./grep";
export { createLsTool } from "./ls";
export { createReadTool } from "./read";
export { createWriteTool } from "./write";

/** Create the standard coding tool suite for a given working directory. */
export function createDefaultTools(
	cwd: string,
	files?: FileOperations,
): AgentTool[] {
	return [
		createBashTool(cwd),
		createReadTool(cwd, files),
		createWriteTool(cwd, files),
		createEditTool(cwd, files),
		createLsTool(cwd),
		createGrepTool(cwd),
		createFindTool(cwd),
	];
}
