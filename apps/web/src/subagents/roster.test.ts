import { describe, expect, test } from "bun:test";
import type { SubagentConversation } from "../api/types";
import type { Row, ToolCall } from "../session/projection";
import {
  agentLabel,
  agentStatus,
  dismissLocked,
  ending,
  modelLocked,
  startOrder,
  stripAgents,
  thinkingLocked,
} from "./roster";

function agent(name: string, extra: Partial<SubagentConversation> = {}): SubagentConversation {
  return {
    id: `c-${name}`,
    agentName: name,
    generation: 1,
    model: "anthropic/claude-sonnet-4-5",
    thinkingLevel: "high",
    queuedTasks: 0,
    state: "idle",
    updatedAt: "2025-01-01T00:00:00Z",
    ...extra,
  };
}

function call(id: string, args: Record<string, unknown>, status: ToolCall["status"] = "done"): ToolCall {
  return {
    id,
    name: "subagent",
    arguments: JSON.stringify(args),
    output: "",
    attachments: [],
    failed: false,
    status,
  };
}

function tools(...calls: ToolCall[]): Row {
  return { kind: "tools", id: `tools-${calls[0]?.id}`, tools: calls };
}

describe("agentStatus", () => {
  test("names each state the strip shows", () => {
    expect(agentStatus(agent("a", { state: "running" }))).toBe("working");
    expect(agentStatus(agent("a", { queuedTasks: 2 }))).toBe("queued");
    expect(agentStatus(agent("a", { state: "failed" }))).toBe("failed");
    expect(agentStatus(agent("a", { state: "interrupted" }))).toBe("interrupted");
    expect(agentStatus(agent("a", { state: "aborted" }))).toBe("stopped");
    expect(agentStatus(agent("a"))).toBe("done");
  });
});

describe("startOrder", () => {
  test("counts only calls that started an agent, in the order they ran", () => {
    const rows = [
      tools(
        call("1", { action: "start", agent: "reviewer" }),
        call("2", { action: "message", agent: "tester" }),
        call("3", { action: "start", agent: "tester" }, "notRun"),
      ),
      tools(
        call("4", { action: "spawn", agent: "tester" }),
        call("5", { action: "start", agent: "reviewer" }),
      ),
    ];
    expect([...startOrder(rows)]).toEqual([
      ["reviewer", 0],
      ["tester", 1],
    ]);
  });
});

describe("stripAgents", () => {
  test("puts agents that need a look first, then working ones, then idle ones, each in start order", () => {
    const rows = [
      tools(
        call("1", { action: "start", agent: "librarian" }),
        call("2", { action: "start", agent: "reviewer" }),
        call("3", { action: "start", agent: "tester" }),
        call("4", { action: "start", agent: "designer" }),
      ),
    ];
    const strip = stripAgents(
      [
        agent("designer", { queuedTasks: 1 }),
        agent("librarian"),
        agent("tester", { state: "failed" }),
        agent("reviewer", { state: "running" }),
        agent("archivist"),
      ],
      rows,
    );
    expect(strip.map((a) => a.agentName)).toEqual([
      "tester",
      "reviewer",
      "designer",
      "librarian",
      "archivist",
    ]);
  });

  test("lists an agent once, and none without an id or name", () => {
    const strip = stripAgents(
      [
        agent("reviewer"),
        agent("reviewer", { id: "c-again" }),
        agent("", { id: "c-x" }),
        agent("x", { id: "" }),
      ],
      [],
    );
    expect(strip.map((a) => a.id)).toEqual(["c-reviewer"]);
  });
});

describe("the agent bar", () => {
  const models = [
    {
      id: "anthropic/claude-sonnet-4-5",
      name: "Claude Sonnet 4.5",
      provider: "anthropic",
      available: true,
      thinkingLevels: ["off", "high"],
    },
  ] as never;

  test("names the agent, its model, and its thinking", () => {
    expect(agentLabel(agent("reviewer"), models)).toBe("reviewer · Claude Sonnet 4.5 · High");
    expect(agentLabel(agent("tester", { thinkingLevel: "off" }), undefined)).toBe(
      "tester · anthropic/claude-sonnet-4-5 · Thinking off",
    );
  });

  test("says how the last task ended when it didn't finish", () => {
    expect(ending(agent("tester", { state: "failed" }))).toEqual({
      text: "tester’s last task failed.",
      tone: "danger",
    });
    expect(ending(agent("tester", { state: "interrupted" }))?.text).toBe(
      "tester’s last task was interrupted.",
    );
    expect(ending(agent("tester", { state: "aborted" }))?.text).toBe("tester’s last task was stopped.");
    expect(ending(agent("tester"))).toBeUndefined();
  });

  test("locks the model while work runs or waits, and every change while offline", () => {
    const working = agent("reviewer", { state: "running" });
    expect(modelLocked(working, true, false)).toBe("Can change once reviewer’s work finishes");
    expect(modelLocked(agent("reviewer", { queuedTasks: 1 }), true, false)).toBe(
      "Can change once reviewer’s work finishes",
    );
    expect(thinkingLocked(working, true, false)).toBeUndefined();
    expect(modelLocked(agent("reviewer"), true, false)).toBeUndefined();
    expect(modelLocked(agent("reviewer"), false, false)).toBe("Reconnect to change");
    expect(thinkingLocked(agent("reviewer"), true, true)).toBe("Changing…");
    expect(dismissLocked(working, true, false)).toBeUndefined();
    expect(dismissLocked(working, false, false)).toBe("Reconnect to dismiss");
    expect(dismissLocked(agent("reviewer", { generation: 0 }), true, false)).toBe("Unavailable");
  });
});
