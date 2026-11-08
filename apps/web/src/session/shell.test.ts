import { describe, expect, test } from "bun:test";
import type { Row } from "./projection";
import {
  placeShellRuns,
  type ShellRun,
  shellDraft,
  shellFromBoundary,
  shellStatus,
  toggleShellContext,
} from "./shell";

function run(id: string, overrides: Partial<ShellRun> = {}): ShellRun {
  return {
    id,
    command: "git status --short",
    status: "completed",
    output: "",
    exitCode: 0,
    excluded: false,
    truncated: false,
    timedOut: false,
    ...overrides,
  };
}

function text(id: string): Row {
  return { kind: "assistant", id, text: id, attachments: [] };
}

describe("shellDraft", () => {
  test("reads ! as a command the agent sees and !! as one it doesn't", () => {
    expect(shellDraft("!git status")).toEqual({ command: "git status", excluded: false });
    expect(shellDraft("!! du -sh node_modules ")).toEqual({ command: "du -sh node_modules", excluded: true });
    expect(shellDraft("!")).toEqual({ command: "", excluded: false });
    expect(shellDraft("Run git status")).toBeUndefined();
  });

  test("toggles the prefix and keeps the command as typed", () => {
    expect(toggleShellContext("!git log")).toBe("!!git log");
    expect(toggleShellContext("!!git log")).toBe("!git log");
    expect(toggleShellContext("git log")).toBe("git log");
  });
});

describe("shellStatus", () => {
  test("names an outcome only when the command didn't simply succeed", () => {
    expect(shellStatus(run("a", { status: "running", exitCode: undefined }))).toBeUndefined();
    expect(shellStatus(run("a"))).toBeUndefined();
    expect(shellStatus(run("a", { exitCode: 1 }))).toBe("Exit 1");
    expect(shellStatus(run("a", { timedOut: true, exitCode: undefined, status: "failed" }))).toBe(
      "Timed out",
    );
    expect(shellStatus(run("a", { status: "aborted", exitCode: undefined }))).toBe("Aborted");
  });
});

describe("shellFromBoundary", () => {
  const details = {
    version: 1,
    command: "go vet ./...",
    status: "completed",
    exitCode: 1,
    truncated: true,
    startedAt: "2025-11-08T10:00:00Z",
    completedAt: "2025-11-08T10:00:02Z",
  };

  test("takes the output from between the command line and the server's notices", () => {
    const content = [
      {
        kind: "text" as const,
        text: "[bash command: go vet ./...] (exit code: 1)\n# kit/internal/session\nwatch.go:212:3: unreachable code\n[output truncated]",
      },
    ];
    expect(shellFromBoundary("bash_1", details, content)).toEqual({
      id: "bash_1",
      command: "go vet ./...",
      status: "completed",
      output: "# kit/internal/session\nwatch.go:212:3: unreachable code",
      exitCode: 1,
      excluded: false,
      truncated: true,
      timedOut: false,
      error: undefined,
    });
  });

  test("drops the framing the server puts first, as its own block or its first line", () => {
    const blocks = [
      { kind: "text" as const, text: "[bash from composer]" },
      {
        kind: "text" as const,
        text: "[bash command: go vet ./...] (exit code: 1)\nvet: ok\n[output truncated]",
      },
    ];
    expect(shellFromBoundary("bash_1", details, blocks, "composer")?.output).toBe("vet: ok");
    const joined = [
      {
        kind: "text" as const,
        text: "[bash from composer]\n[bash command: go vet ./...] (exit code: 1)\nvet: ok",
      },
    ];
    expect(shellFromBoundary("bash_1", { ...details, truncated: false }, joined, "composer")?.output).toBe(
      "vet: ok",
    );
  });

  test("reads a failed command's error and a command with no output", () => {
    const failed = {
      ...details,
      status: "failed",
      exitCode: undefined,
      truncated: false,
      errorMessage: "shell not found",
    };
    const content = [
      { kind: "text" as const, text: "[bash command: go vet ./...]\n[failed: shell not found]" },
    ];
    const value = shellFromBoundary("bash_2", failed, content);
    expect(value?.output).toBe("");
    expect(value?.error).toBe("shell not found");
  });

  test("refuses details of another version", () => {
    expect(shellFromBoundary("bash_3", { ...details, version: 2 }, [])).toBeUndefined();
  });
});

describe("placeShellRuns", () => {
  test("puts a command after the row it followed, or first when there was none", () => {
    const rows = [text("a"), text("b")];
    const anchors = new Map([
      ["bash_1", "a"],
      ["bash_2", ""],
    ]);
    const placed = placeShellRuns(rows, [run("bash_1"), run("bash_2")], anchors);
    expect(placed.map((row) => row.id)).toEqual(["bash_2", "a", "bash_1", "b"]);
  });

  test("shows a recorded command from the transcript, not this device's copy", () => {
    const recorded: Row = { kind: "shell", id: "bash_1", run: run("bash_1", { output: "recorded" }) };
    const placed = placeShellRuns([text("a"), recorded], [run("bash_1")], new Map([["bash_1", "a"]]));
    expect(placed).toEqual([text("a"), recorded]);
  });

  test("keeps a running command whose row is gone at the end, and drops a settled one", () => {
    const anchors = new Map([
      ["bash_1", "gone"],
      ["bash_2", "gone"],
    ]);
    const placed = placeShellRuns(
      [text("a")],
      [run("bash_1", { status: "running" }), run("bash_2")],
      anchors,
    );
    expect(placed.map((row) => row.id)).toEqual(["a", "bash_1"]);
  });
});
