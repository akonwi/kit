// What a transcript row needs to open a subagent's conversation, and how a
// subagent's transcript labels the tasks it was given.
import { createContext, useContext } from "solid-js";

export interface AgentLinks {
  /** Whether the session started an agent by this name, so a call naming it can open it. */
  has(name: string): boolean;
  /** Shows the agent's conversation in place of the transcript. */
  open(name: string): void;
}

export const AgentsContext = createContext<AgentLinks>();

export function useAgents(): AgentLinks | undefined {
  return useContext(AgentsContext);
}

/** The caption a transcript puts over its user messages: a subagent's are tasks. */
export const UserCaptionContext = createContext<string>();
