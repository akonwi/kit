import { describe, expect, test } from "bun:test";
import { ApiError, NetworkError } from "../api/client";
import type { SessionInfo } from "../api/types";
import {
  compactionOutcome,
  compactUnavailable,
  createFailure,
  defaultThinking,
  definitelyRejected,
  deleteFailure,
  folderOptions,
  folderPath,
  newId,
  preferredModel,
  recentFolders,
} from "./manage";

const session = (cwd: string, updatedAt: string) => ({ cwd, updatedAt }) as SessionInfo;

test("newId is a prefix and 128 random bits in hex", () => {
  expect(newId("session")).toMatch(/^session_[0-9a-f]{32}$/);
  expect(newId("compact")).not.toBe(newId("compact"));
});

describe("folderPath", () => {
  test("keeps an absolute path", () => {
    expect(folderPath(" /srv/app ", "/Users/ada")).toBe("/srv/app");
  });

  test("starts ~ and relative paths from the server user's home", () => {
    expect(folderPath("~", "/Users/ada")).toBe("/Users/ada");
    expect(folderPath("~/code/kit", "/Users/ada")).toBe("/Users/ada/code/kit");
    expect(folderPath("code/kit", "/Users/ada/")).toBe("/Users/ada/code/kit");
  });

  test("names no folder for a relative path without a known home, or nothing typed", () => {
    expect(folderPath("~/code", undefined)).toBeUndefined();
    expect(folderPath("  ", "/Users/ada")).toBeUndefined();
  });
});

test("recentFolders lists each folder once, newest first, as the user types it", () => {
  const sessions = [
    session("/Users/ada/code/kit", "2025-01-01T00:00:00Z"),
    session("/srv/app", "2025-01-03T00:00:00Z"),
    session("/Users/ada/code/kit", "2025-01-02T00:00:00Z"),
  ];
  expect(recentFolders(sessions, "/Users/ada")).toEqual(["/srv/app", "~/code/kit"]);
});

describe("folderOptions", () => {
  const folders = ["~/code/kit", "~/code/mica", "/srv/app"];

  test("offers every folder while the field shows one from the list", () => {
    expect(folderOptions(folders, "~/code/mica")).toEqual(folders);
  });

  test("offers the folders containing what was typed, ignoring case", () => {
    expect(folderOptions(folders, "KIT")).toEqual(["~/code/kit"]);
    expect(folderOptions(folders, "~/code/")).toEqual(["~/code/kit", "~/code/mica"]);
  });
});

test("preferredModel takes the server's default, then its new-session model, then the first", () => {
  const available = ["a", "b", "c"];
  expect(preferredModel({ defaultModel: "b", newSessionModel: "c" } as never, available)).toBe("b");
  expect(preferredModel({ defaultModel: "gone", newSessionModel: "c" } as never, available)).toBe("c");
  expect(preferredModel(undefined, available)).toBe("a");
});

test("defaultThinking prefers medium", () => {
  expect(defaultThinking(["low", "medium", "high"])).toBe("medium");
  expect(defaultThinking(["off", "high"])).toBe("off");
  expect(defaultThinking([])).toBe("");
});

test("definitelyRejected trusts refusals but not lost answers", () => {
  expect(definitelyRejected(new ApiError(400, "invalid", "bad cwd"))).toBe(true);
  expect(definitelyRejected(new ApiError(503, "unavailable", "later"))).toBe(false);
  expect(definitelyRejected(new NetworkError("offline"))).toBe(false);
});

test("compactUnavailable follows the Mac app: not during a turn or another compaction", () => {
  expect(compactUnavailable({ activeTurnId: "turn_1", compacting: false }, false)).toBe(
    "Context can compact when the turn ends",
  );
  expect(compactUnavailable({ compacting: true }, false)).toBe("Compaction in progress");
  expect(compactUnavailable({ compacting: false }, true)).toBe("Compaction in progress");
  expect(compactUnavailable({ compacting: false }, false)).toBeUndefined();
});

describe("compactionOutcome", () => {
  test("reports a compaction with too little history to compact", () => {
    expect(compactionOutcome({ compacted: false })).toEqual({
      tone: "info",
      title: "Not enough turns to compact",
    });
  });

  test("offers a retry for a busy session", () => {
    expect(compactionOutcome(undefined, new ApiError(409, "busy", "busy"))).toEqual({
      tone: "error",
      title: "Compaction failed",
      detail: "The session is busy. Try again when the turn ends.",
      retry: true,
    });
  });

  test("offers no retry for a failure that would repeat", () => {
    const failure = new ApiError(422, "compaction_failed", "The summary was cut off.", {
      reason: "summary_truncated",
    });
    expect(compactionOutcome(undefined, failure)).toEqual({
      tone: "error",
      title: "Compaction failed",
      detail: "The summary was cut off.",
      retry: false,
    });
  });

  test("offers a retry when the answer was lost", () => {
    expect(compactionOutcome(undefined, new NetworkError("The server can’t be reached."))).toEqual({
      tone: "error",
      title: "Compaction failed",
      detail: "The server can’t be reached.",
      retry: true,
    });
  });
});

test("deleteFailure uses the Mac app's in-use copy when the server refuses", () => {
  expect(deleteFailure(new ApiError(409, "busy", "busy"))).toBe(
    "This session is in use and cannot be deleted. Stop its active work before trying again.",
  );
  expect(deleteFailure(new NetworkError("The server can’t be reached."))).toBe(
    "The server can’t be reached.",
  );
});

test("createFailure names the missing folder on the server", () => {
  expect(createFailure(new ApiError(400, "invalid_request", "invalid request"), " ~/nope ", "macbook")).toBe(
    "No folder at ~/nope on macbook.",
  );
});
