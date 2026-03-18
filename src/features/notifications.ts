/**
 * Bells and speech notifications on agent turn completion.
 *
 * - Bell: terminal bell character (\u0007) on success, macOS Funk sound on error
 * - Speech (macOS only): reads a shortened version of the assistant's response via `say`
 */

import { spawn } from "node:child_process";
import { platform } from "node:os";

const FUNK_SOUND_PATH = "/System/Library/Sounds/Funk.aiff";

// ── Bell ────────────────────────────────────────────────────────────

function writeBell(): void {
  try {
    process.stdout.write("\u0007");
  } catch {
    // best effort
  }
}

function playErrorSound(): void {
  if (platform() === "darwin") {
    spawn("afplay", [FUNK_SOUND_PATH], { stdio: "ignore" }).unref();
  } else {
    writeBell();
  }
}

export function ringBell(isError: boolean): void {
  if (isError) {
    playErrorSound();
  } else {
    writeBell();
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
