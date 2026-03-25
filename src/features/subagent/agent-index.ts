/**
 * Agent index — discovers subagents and provides suggestions for the @ picker.
 */

import { type AgentScope, discoverAgents, type AgentConfig } from "./agents";
import { scoreMatch } from "../files/score";

export type AgentSuggestion = {
  name: string;
  description: string;
  value: string;
};

export function createAgentIndex(cwd: string, scope: AgentScope = "both") {
  let cachedAgents: AgentConfig[] | null = null;

  function getAgents(): AgentConfig[] {
    if (!cachedAgents) {
      cachedAgents = discoverAgents(cwd, scope).agents;
    }
    return cachedAgents;
  }

  /**
   * Get agent suggestions matching a query.
   * Returns all agents when query is empty, scored/filtered otherwise.
   */
  function suggest(query: string): AgentSuggestion[] {
    const agents = getAgents();
    const norm = query.replace(/^@/, "");

    if (!norm) {
      // No query — return all agents
      return agents.map((a) => ({
        name: `@${a.name}`,
        description: `agent · ${a.description}`,
        value: a.name,
      }));
    }

    return agents
      .map((a) => ({
        agent: a,
        score: Math.max(scoreMatch(a.name, norm), scoreMatch(a.description, norm)),
      }))
      .filter((x) => x.score > 0)
      .sort((a, b) => b.score - a.score)
      .map((x) => ({
        name: `@${x.agent.name}`,
        description: `agent · ${x.agent.description}`,
        value: x.agent.name,
      }));
  }

  /** Check if a name matches a known agent. */
  function isAgent(name: string): boolean {
    return getAgents().some((a) => a.name === name);
  }

  /** Force re-discovery (e.g. after agent files change). */
  function invalidate() {
    cachedAgents = null;
  }

  return { suggest, isAgent, getAgents, invalidate };
}

export type AgentIndex = ReturnType<typeof createAgentIndex>;
