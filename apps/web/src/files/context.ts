// Opening files from anywhere in a session's page: a tool's path, a
// comment's lines.
import { createContext, useContext } from "solid-js";

export interface Files {
  /** The path in the session's folder a tool's path names, or undefined when it's elsewhere. */
  resolve(path: string): string | undefined;
  /** Shows a file in the session's folder, with the lines to bring into view. */
  open(path: string, line?: number, end?: number): void;
}

export const FilesContext = createContext<Files>();

export function useFiles(): Files | undefined {
  return useContext(FilesContext);
}
