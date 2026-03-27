/**
 * Bells and speech notifications on agent turn completion.
 *
 * - Bell: terminal bell character (\u0007) on success, macOS Funk sound on error
 * - Speech (macOS only): reads a shortened version of the assistant's response via `say`
 */

import { spawn } from "node:child_process";
import { writeFileSync } from "node:fs";
import { platform } from "node:os";

const FUNK_SOUND_PATH = "/System/Library/Sounds/Funk.aiff";

// ── Bell ────────────────────────────────────────────────────────────

function writeBell(): void {
  // For terminal-tab bell indicators (Ghostty, iTerm, etc), write BEL directly
  // to the controlling TTY when possible.
  try {
    writeFileSync("/dev/tty", "\u0007");
    return;
  } catch {
    // fall through
  }

  // Fallback paths.
  try {
    process.stderr.write("\u0007");
    return;
  } catch {
    // fall through
  }

  try {
    process.stdout.write("\u0007");
  } catch {
    // best effort
  }
}

function playErrorSound(enabled: boolean): void {
  if (!enabled) return;
  if (platform() === "darwin") {
    const child = spawn("afplay", [FUNK_SOUND_PATH], { stdio: "ignore" });
    child.unref();
  }
}

/**
 * Emit terminal tab notification (BEL) always.
 * Optional sound playback is controlled separately via `soundEnabled`.
 */
export function ringBell(isError: boolean, soundEnabled: boolean): void {
  // Always emit BEL so terminal tab indicators light up.
  writeBell();

  // Sound policy is separate from tab notification.
  if (isError) {
    playErrorSound(soundEnabled);
  }
}

// ── Speech ──────────────────────────────────────────────────────────

function cleanForSpeech(text: string): string {
  return text
    .replace(/```[\s\S]*?```/g, " code block omitted ")
    .replace(/`([^`]+)`/g, "$1")
    .replace(/\[(.*?)\]\((.*?)\)/g, "$1")
    .replace(/[*_~#>]/g, "")
    .replace(/\s+/g, " ")
    .trim();
}

function shortenForSpeech(text: string, maxChars: number): string {
  const cleaned = cleanForSpeech(text);
  if (!cleaned) return "";
  if (cleaned.length <= maxChars) return cleaned;

  const sentence = cleaned.match(/(.+?[.!?])(\s|$)/)?.[1]?.trim();
  if (sentence && sentence.length <= maxChars) return sentence;
  return `${cleaned.slice(0, Math.max(0, maxChars - 3))}...`;
}

const lastSpoken = new Map<string, string>();

export function speak(text: string, sessionId: string, options?: { voice?: string; maxChars?: number }): void {
  if (platform() !== "darwin") return;

  const maxChars = options?.maxChars ?? 220;
  const speech = shortenForSpeech(text, maxChars);
  if (!speech) return;

  // Deduplicate — don't repeat the same text for the same session
  const signature = `${sessionId}:${speech}`;
  if (lastSpoken.get(sessionId) === signature) return;
  lastSpoken.set(sessionId, signature);

  const args: string[] = [];
  if (options?.voice) args.push("-v", options.voice);
  args.push(speech);
  spawn("say", args, { stdio: "ignore" }).unref();
}
