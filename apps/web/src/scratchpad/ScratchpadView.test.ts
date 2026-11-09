import { expect, test } from "bun:test";
import type { SessionInfo } from "../api/types";
import { familyLabel, padSubtitle } from "./ScratchpadView";

const info: SessionInfo = {
  id: "session_fork",
  name: "Try the cursor",
  cwd: "/work",
  model: "claude",
  thinkingLevel: "high",
  configurationRevision: 1,
  createdAt: "2025-11-06T10:00:00Z",
  updatedAt: "2025-11-06T10:00:00Z",
  parentSessionId: "session_root",
  parentSessionName: "Fix SSE reconnect",
};

test("familyLabel names the session that owns the scratchpad", () => {
  expect(familyLabel({ ...info, id: "session_root" }, "session_root")).toBe(
    "Shared with forks of Try the cursor",
  );
  expect(familyLabel(info, "session_root")).toBe("Shared with Fix SSE reconnect and its forks");
  expect(familyLabel(info, "session_grandparent")).toBe(
    "Shared with the session this came from and its forks",
  );
  expect(familyLabel({ ...info, id: "session_root", name: undefined }, "session_root")).toBe(
    "Shared with forks of this session",
  );
});

test("padSubtitle says why a draft isn't saved, and a save only once it's slow", () => {
  const family = "Shared with forks of Fix SSE reconnect";
  expect(padSubtitle({ kind: "saved" }, true, family)).toEqual({ text: family });
  expect(padSubtitle({ kind: "unsaved" }, true, family)).toEqual({ text: family });
  expect(padSubtitle({ kind: "saving" }, true, family)).toEqual({ text: family });
  expect(padSubtitle({ kind: "saving" }, true, family, true)).toEqual({ text: "Saving…" });
  expect(padSubtitle({ kind: "conflict" }, true, family)).toEqual({
    text: "Not saved · The shared copy changed",
    tone: "warn",
  });
  expect(padSubtitle({ kind: "failed", reason: "Kit can’t be reached" }, true, family)).toEqual({
    text: "Not saved · Kit can’t be reached",
    tone: "danger",
  });
  expect(padSubtitle({ kind: "failed", reason: "Kit can’t be reached" }, false, family)).toEqual({
    text: "Couldn’t load · Kit can’t be reached",
    tone: "danger",
  });
});
