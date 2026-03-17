import { spawn } from "node:child_process";

export type GitInfo = {
  branch: string | null;
  dirty: boolean;
};

/**
 * Get current git branch and dirty state for a given directory.
 */
export function getGitInfo(cwd: string): GitInfo {
  // Get branch name
  const branch = getGitBranch(cwd);
  if (!branch) {
    return { branch: null, dirty: false };
  }

  // Check for uncommitted changes
  const dirty = isGitDirty(cwd);

  return { branch, dirty };
}

function getGitBranch(cwd: string): string | null {
  try {
    const result = spawnSync("git", ["rev-parse", "--abbrev-ref", "HEAD"], { cwd, encoding: "utf-8" });
    if (result.error || result.status !== 0) return null;
    return result.stdout.trim() || null;
  } catch {
    return null;
  }
}

function isGitDirty(cwd: string): boolean {
  try {
    const result = spawnSync("git", ["status", "--porcelain"], { cwd, encoding: "utf-8" });
    if (result.error || result.status !== 0) return false;
    return result.stdout.trim().length > 0;
  } catch {
    return false;
  }
}

function spawnSync(cmd: string, args: string[], options: { cwd: string; encoding: string }) {
  const { spawnSync } = require("node:child_process");
  return spawnSync(cmd, args, options);
}
