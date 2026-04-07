import type { AgentTool } from "@mariozechner/pi-agent-core";
import { createBashTool } from "./bash";
import { createReadTool } from "./read";
import { createWriteTool } from "./write";
import { createEditTool } from "./edit";
import { createLsTool } from "./ls";
import { createGrepTool } from "./grep";
import { createFindTool } from "./find";

export { createBashTool } from "./bash";
export { createReadTool } from "./read";
export { createWriteTool } from "./write";
export { createEditTool } from "./edit";
export { createLsTool } from "./ls";
export { createGrepTool } from "./grep";
export { createFindTool } from "./find";

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
